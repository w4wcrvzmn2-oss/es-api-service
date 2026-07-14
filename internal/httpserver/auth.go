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
}

// NewAuthService создаёт новый AuthService
func NewAuthService(cfg *config.Config, database *db.Database, fileLogger *logger.Logger) *AuthService {
	return &AuthService{
		config:   cfg,
		database: database,
		logger:   fileLogger,
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

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if a.logger != nil {
			a.logger.Warn("Неверный формат JSON при авторизации от %s: %v", r.RemoteAddr, err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Неверный формат JSON"})
		return
	}

	if a.logger != nil {
		a.logger.Info("Попытка авторизации пользователя '%s' от %s", req.Username, r.RemoteAddr)
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
		if a.logger != nil {
			a.logger.Warn("Неудачная попытка авторизации для пользователя '%s' от %s", req.Username, r.RemoteAddr)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Неверные учётные данные"})
		return
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
		a.logger.Info("Успешная авторизация пользователя '%s' (role=%s) от %s", req.Username, role, r.RemoteAddr)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(LoginResponse{Token: token, Role: role, RedirectURL: redirectURL})
}

// findSupplierByCredentials ищет поставщика по Login/Password через GORM.
// Возвращает SupplierID или "" если не найдено / пароль не совпал.
func (a *AuthService) findSupplierByCredentials(ctx context.Context, login, password string) (string, error) {
	if a.database == nil {
		return "", fmt.Errorf("database not available")
	}
	// CAST UUID в NVARCHAR, чтобы GORM Scan заполнил string-поле,
	// а не 16 raw bytes из uniqueidentifier (это попадёт в JWT).
	var supplier struct {
		SupplierID string
		Password   *string
	}
	err := a.database.GORMWith(ctx).
		Table("Supplier").
		Select("CAST(SupplierID AS NVARCHAR(50)) AS SupplierID, Password").
		Where("Login = ? AND IsActive = ?", login, true).
		Take(&supplier).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	stored := ""
	if supplier.Password != nil {
		stored = *supplier.Password
	}
	if !verifyPassword(stored, password) {
		return "", nil
	}
	if !isBcryptHash(stored) {
		if err := a.upgradeSupplierPasswordHash(ctx, supplier.SupplierID, password); err != nil && a.logger != nil {
			a.logger.Warn("Не удалось обновить пароль поставщика %s на bcrypt-хэш: %v", supplier.SupplierID, err)
		}
	}
	return supplier.SupplierID, nil
}

// findBuyerUserByCredentials ищет BuyerUser по Email/Password через GORM.
// Возвращает BuyerUserID или "" если не найдено / пароль не совпал.
func (a *AuthService) findBuyerUserByCredentials(ctx context.Context, login, password string) (string, error) {
	if a.database == nil {
		return "", fmt.Errorf("database not available")
	}
	// CAST UUID в NVARCHAR — иначе uniqueidentifier попадёт в string как 16 raw bytes.
	var bu struct {
		BuyerUserID string
		Password    *string
	}
	err := a.database.GORMWith(ctx).
		Table("BuyerUser").
		Select("CAST(BuyerUserID AS NVARCHAR(50)) AS BuyerUserID, Password").
		Where("Email = ? AND IsActive = ?", login, true).
		Take(&bu).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if bu.Password == nil || *bu.Password == "" {
		return "", nil
	}
	stored := *bu.Password
	if !verifyPassword(stored, password) {
		return "", nil
	}
	if !isBcryptHash(stored) {
		if err := a.upgradeBuyerUserPasswordHash(ctx, bu.BuyerUserID, password); err != nil && a.logger != nil {
			a.logger.Warn("Не удалось обновить пароль BuyerUser %s на bcrypt-хэш: %v", bu.BuyerUserID, err)
		}
	}
	return bu.BuyerUserID, nil
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
			"UpdatedAt": gorm.Expr("GETUTCDATE()"),
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
		// Обработка паник в middleware
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

		// Пропускаем OPTIONS запросы (preflight для CORS)
		if r.Method == http.MethodOptions {
			if a.logger != nil {
				a.logger.Info("JWTMiddleware: пропускаем OPTIONS запрос для %s", r.URL.Path)
			}
			next.ServeHTTP(w, r)
			return
		}

		if a.logger != nil {
			a.logger.Info("JWTMiddleware: начало обработки запроса %s %s от %s", r.Method, r.URL.Path, r.RemoteAddr)
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			if a.logger != nil {
				a.logger.Warn("Запрос без заголовка Authorization от %s к %s", r.RemoteAddr, r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Отсутствует заголовок Authorization"})
			return
		}

		if a.logger != nil {
			a.logger.Info("JWTMiddleware: заголовок Authorization найден, длина: %d", len(authHeader))
		}

		// Проверка формата "Bearer <token>"
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			if a.logger != nil {
				a.logger.Warn("Неверный формат заголовка Authorization от %s к %s", r.RemoteAddr, r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Неверный формат заголовка Authorization"})
			return
		}

		tokenString := parts[1]
		if a.logger != nil {
			a.logger.Info("JWTMiddleware: начинаем валидацию токена, длина: %d", len(tokenString))
		}

		// Валидация токена
		claims, err := a.ValidateToken(tokenString)
		if err != nil {
			if a.logger != nil {
				a.logger.Warn("Неверный токен от %s к %s: %v", r.RemoteAddr, r.URL.Path, err)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Неверный токен"})
			return
		}

		if a.logger != nil {
			a.logger.Info("JWTMiddleware: токен валиден, пользователь: %s, передаем управление handler", claims.Username)
		}

		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		r = r.WithContext(ctx)

		if a.logger != nil {
			a.logger.Info("JWTMiddleware: вызываем следующий handler для %s", r.URL.Path)
		}

		next.ServeHTTP(w, r)

		if a.logger != nil {
			a.logger.Info("JWTMiddleware: обработка запроса завершена для %s", r.URL.Path)
		}
	})
}
