package httpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"es_api_service/internal/config"
	"es_api_service/internal/db"
	"es_api_service/internal/logger"
	"es_api_service/internal/models"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type contextKey string

const claimsContextKey contextKey = "jwt_claims"

// ClaimsFromContext извлекает JWT claims из контекста запроса
func ClaimsFromContext(ctx context.Context) *Claims {
	if c, ok := ctx.Value(claimsContextKey).(*Claims); ok {
		return c
	}
	return nil
}

// LoginRequest представляет запрос на авторизацию
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse представляет ответ с токеном
type LoginResponse struct {
	Token       string `json:"token"`
	Role        string `json:"role"`
	RedirectURL string `json:"redirect_url,omitempty"`
}

// ErrorResponse представляет ответ с ошибкой
type ErrorResponse struct {
	Error string `json:"error"`
}

// Claims представляет JWT claims
type Claims struct {
	Username    string `json:"username"`
	Role        string `json:"role"`
	SupplierID  string `json:"supplier_id,omitempty"`
	BuyerUserID string `json:"buyer_user_id,omitempty"`
	jwt.RegisteredClaims
}

// AuthService предоставляет методы для авторизации
type AuthService struct {
	config   *config.Config
	database *db.Database
	logger   *logger.Logger
	limiter  *SecurityLimiter
}

// NewAuthService создаёт новый AuthService
func NewAuthService(cfg *config.Config, database *db.Database, fileLogger *logger.Logger, limiter *SecurityLimiter) *AuthService {
	return &AuthService{
		config:   cfg,
		database: database,
		logger:   fileLogger,
		limiter:  limiter,
	}
}

// Login обрабатывает запрос на авторизацию
// @Summary Авторизация пользователя
// @Description Выполняет авторизацию пользователя и возвращает JWT токен
// @Tags auth
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Данные для авторизации"
// @Success 200 {object} LoginResponse "Успешная авторизация"
// @Failure 400 {object} ErrorResponse "Некорректный запрос"
// @Failure 401 {object} ErrorResponse "Неверные учетные данные"
// @Failure 500 {object} ErrorResponse "Внутренняя ошибка сервера"
// @Router /auth/login [post]
func (a *AuthService) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	trustProxy := true
	if a.limiter != nil {
		trustProxy = a.limiter.lim.TrustProxyHeaders
	}
	ip := clientIP(r, trustProxy)

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if a.logger != nil {
			a.logger.Warn("Неверный формат JSON при авторизации от %s: %v", ip, err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Неверный формат JSON"})
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if a.limiter != nil {
		if locked, retry := a.limiter.isLoginLocked(ip, req.Username); locked {
			if a.logger != nil {
				a.logger.Warn("Login lockout username=%s ip=%s retry=%v", req.Username, ip, retry)
			}
			writeRateLimited(w, retry)
			return
		}
	}

	if a.logger != nil {
		a.logger.Info("Попытка авторизации пользователя '%s' от %s", req.Username, ip)
	}

	var role, supplierID, buyerUserID, redirectURL string

	// Фаза 1: проверка в конфиге (admin)
	user := a.config.FindUser(req.Username)
	if user != nil && verifyPassword(user.Password, req.Password) {
		role = "admin"
		redirectURL = "index.html"
	} else if sid, err := a.findSupplierByCredentials(r.Context(), req.Username, req.Password); err == nil && sid != "" {
		// Фаза 2: проверка в таблице Supplier (поставщик)
		role = "supplier"
		supplierID = sid
		redirectURL = "/supplier/index.html"
	} else if buid, err := a.findBuyerUserByCredentials(r.Context(), req.Username, req.Password); err == nil && buid != "" {
		// Фаза 3: проверка в таблице BuyerUser (покупатель)
		role = "buyer"
		buyerUserID = buid
		redirectURL = "/buyer/index.html"
	} else {
		if a.limiter != nil {
			a.limiter.recordLoginFailure(ip, req.Username)
		}
		if a.logger != nil {
			a.logger.Warn("Неудачная попытка авторизации для пользователя '%s' от %s", req.Username, ip)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Неверные учётные данные"})
		return
	}

	if a.limiter != nil {
		a.limiter.clearLoginFailures(ip, req.Username)
	}

	token, err := a.generateToken(req.Username, role, supplierID, buyerUserID)
	if err != nil {
		if a.logger != nil {
			a.logger.Error("Ошибка создания токена для пользователя '%s': %v", req.Username, err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Ошибка создания токена"})
		return
	}

	if a.logger != nil {
		a.logger.Info("Успешная авторизация пользователя '%s' (role=%s) от %s", req.Username, role, ip)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(LoginResponse{Token: token, Role: role, RedirectURL: redirectURL})
}

type authCredentialRow struct {
	ID       string  `gorm:"column:id"`
	Password *string `gorm:"column:password"`
}

// findSupplierByCredentials ищет поставщика по Login/Password.
// Возвращает SupplierID или "" если не найдено / пароль не совпал.
func (a *AuthService) findSupplierByCredentials(ctx context.Context, login, password string) (string, error) {
	if a.database == nil {
		return "", fmt.Errorf("database not available")
	}
	var row authCredentialRow
	err := a.database.GORMWith(ctx).Raw(`
		SELECT CAST("SupplierID" AS TEXT) AS id, "Password" AS password
		FROM "Supplier"
		WHERE "Login" = ? AND "IsActive" = TRUE
		LIMIT 1
	`, login).Scan(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		if a.logger != nil {
			a.logger.Warn("findSupplierByCredentials: query error for '%s': %v", login, err)
		}
		return "", err
	}
	if row.ID == "" {
		if a.logger != nil {
			a.logger.Info("findSupplierByCredentials: supplier '%s' not found or inactive", login)
		}
		return "", nil
	}
	stored := ""
	if row.Password != nil {
		stored = *row.Password
	}
	if a.logger != nil {
		a.logger.Info("findSupplierByCredentials: supplier '%s' found id=%s pwdLen=%d bcrypt=%v", login, row.ID, len(stored), isBcryptHash(stored))
	}
	if !verifyPassword(stored, password) {
		if a.logger != nil {
			a.logger.Warn("findSupplierByCredentials: password mismatch for supplier '%s' id=%s", login, row.ID)
		}
		return "", nil
	}
	if !isBcryptHash(stored) {
		if err := a.upgradeSupplierPasswordHash(ctx, row.ID, password); err != nil && a.logger != nil {
			a.logger.Warn("Не удалось обновить пароль поставщика %s на bcrypt-хэш: %v", row.ID, err)
		}
	}
	return row.ID, nil
}

// findBuyerUserByCredentials ищет BuyerUser по Email/Password.
// Возвращает BuyerUserID или "" если не найдено / пароль не совпал.
func (a *AuthService) findBuyerUserByCredentials(ctx context.Context, login, password string) (string, error) {
	if a.database == nil {
		return "", fmt.Errorf("database not available")
	}
	var row authCredentialRow
	err := a.database.GORMWith(ctx).Raw(`
		SELECT CAST("BuyerUserID" AS TEXT) AS id, "Password" AS password
		FROM "BuyerUser"
		WHERE lower("Email") = lower(?) AND "IsActive" = TRUE
		ORDER BY "CreatedAt" DESC
		LIMIT 1
	`, login).Scan(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		if a.logger != nil {
			a.logger.Warn("findBuyerUserByCredentials: query error for '%s': %v", login, err)
		}
		return "", err
	}
	if row.ID == "" {
		if a.logger != nil {
			a.logger.Info("findBuyerUserByCredentials: buyer '%s' not found or inactive", login)
		}
		return "", nil
	}
	if row.Password == nil || *row.Password == "" {
		if a.logger != nil {
			a.logger.Warn("findBuyerUserByCredentials: buyer '%s' id=%s has empty password", login, row.ID)
		}
		return "", nil
	}
	stored := *row.Password
	if a.logger != nil {
		a.logger.Info("findBuyerUserByCredentials: buyer '%s' found id=%s pwdLen=%d bcrypt=%v", login, row.ID, len(stored), isBcryptHash(stored))
	}
	if !verifyPassword(stored, password) {
		if a.logger != nil {
			a.logger.Warn("findBuyerUserByCredentials: password mismatch for buyer '%s' id=%s", login, row.ID)
		}
		return "", nil
	}
	if !isBcryptHash(stored) {
		if err := a.upgradeBuyerUserPasswordHash(ctx, row.ID, password); err != nil && a.logger != nil {
			a.logger.Warn("Не удалось обновить пароль BuyerUser %s на bcrypt-хэш: %v", row.ID, err)
		}
	}
	return row.ID, nil
}

func (a *AuthService) upgradeBuyerUserPasswordHash(ctx context.Context, buyerUserID, password string) error {
	hashed, err := hashPassword(password)
	if err != nil {
		return err
	}
	return a.database.GORMWith(ctx).
		Model(&models.BuyerUser{}).
		Where("BuyerUserID = ?", db.UUIDParam(buyerUserID)).
		Update("Password", hashed).Error
}

func (a *AuthService) upgradeSupplierPasswordHash(ctx context.Context, supplierID, password string) error {
	hashed, err := hashPassword(password)
	if err != nil {
		return err
	}
	return a.database.GORMWith(ctx).
		Model(&models.Supplier{}).
		Where("SupplierID = ?", db.UUIDParam(supplierID)).
		Updates(map[string]interface{}{
			"Password":  hashed,
			"UpdatedAt": gorm.Expr("(NOW() AT TIME ZONE 'utc')"),
		}).Error
}

func verifyPassword(storedPassword, password string) bool {
	if isBcryptHash(storedPassword) {
		return bcrypt.CompareHashAndPassword([]byte(storedPassword), []byte(password)) == nil
	}
	return subtle.ConstantTimeCompare([]byte(storedPassword), []byte(password)) == 1
}

func hashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("ошибка хэширования пароля: %w", err)
	}
	return string(hashed), nil
}

func isBcryptHash(password string) bool {
	return strings.HasPrefix(password, "$2a$") || strings.HasPrefix(password, "$2b$") || strings.HasPrefix(password, "$2y$")
}

// generateToken создаёт JWT токен
func (a *AuthService) generateToken(username, role, supplierID, buyerUserID string) (string, error) {
	claims := Claims{
		Username:    username,
		Role:        role,
		SupplierID:  supplierID,
		BuyerUserID: buyerUserID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    a.config.Auth.Issuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(a.config.GetTokenTTL())),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(a.config.Auth.JWTSecret))
}

// RequireRole создаёт middleware, пропускающий только указанные роли
func (a *AuthService) RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := ClaimsFromContext(r.Context())
			if claims == nil || !allowed[claims.Role] {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(ErrorResponse{Error: "Доступ запрещён"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ValidateToken проверяет JWT токен
func (a *AuthService) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(a.config.Auth.JWTSecret), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("неверный токен")
}

// JWTMiddleware создаёт middleware для проверки JWT токенов
func (a *AuthService) JWTMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if a.logger != nil {
					a.logger.Error("Паника в JWTMiddleware: %v", rec)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(ErrorResponse{Error: "Внутренняя ошибка"})
			}
		}()

		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Отсутствует заголовок Authorization"})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Неверный формат заголовка Authorization"})
			return
		}

		claims, err := a.ValidateToken(parts[1])
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Неверный токен"})
			return
		}

		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
