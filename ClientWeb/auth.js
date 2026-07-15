// Модуль управления авторизацией и навигацией
class AuthManager {
    static STORAGE_TOKEN_KEY = 'es_api_token';
    static STORAGE_USERNAME_KEY = 'es_api_username';
    static STORAGE_API_URL_KEY = 'es_api_url';
    static STORAGE_USER_INFO_KEY = 'es_api_user_info';

    // Проверка авторизации
    static isAuthenticated() {
        const token = localStorage.getItem(this.STORAGE_TOKEN_KEY);
        if (!token) return false;
        
        // Проверяем формат токена (JWT должен содержать 3 части)
        const parts = token.split('.');
        if (parts.length !== 3) {
            console.warn('Токен имеет некорректный формат, очищаем');
            this.logout();
            return false;
        }
        
        return true;
    }

    // Получение токена
    static getToken() {
        return localStorage.getItem(this.STORAGE_TOKEN_KEY);
    }

    // Получение URL API
    static getApiUrl() {
        // Сначала проверяем localStorage, затем используем текущий адрес страницы
        const savedUrl = localStorage.getItem(this.STORAGE_API_URL_KEY);
        if (savedUrl) {
            return savedUrl;
        }
        // Если ничего не сохранено, используем текущий адрес страницы (origin)
        if (typeof window !== 'undefined' && window.location) {
            return window.location.origin;
        }
        // Фолбэк для серверного окружения
        return 'http://localhost:8080';
    }

    // Получение имени пользователя
    static getUsername() {
        return localStorage.getItem(this.STORAGE_USERNAME_KEY) || 'Пользователь';
    }

    // Получение информации о пользователе
    static getUserInfo() {
        const info = localStorage.getItem(this.STORAGE_USER_INFO_KEY);
        return info ? JSON.parse(info) : { username: this.getUsername() };
    }

    // Авторизация
    static async login(username, password, apiUrl) {
        try {
            // Нормализуем URL (убираем слэш в конце)
            const normalizedUrl = apiUrl.replace(/\/+$/, '');
            
            console.log('Попытка авторизации:', { username, apiUrl: normalizedUrl });
            
            const response = await fetch(`${normalizedUrl}/auth/login`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({ username, password }),
                mode: 'cors'
            });

            if (!response.ok) {
                let errorData;
                try {
                    errorData = await response.json();
                } catch (e) {
                    errorData = { error: `HTTP ${response.status}: ${response.statusText}` };
                }
                return { success: false, error: errorData.error || 'Неверные учетные данные' };
            }

            const data = await response.json();
            console.log('Ответ от сервера получен, проверяем токен...');

            if (data.token && typeof data.token === 'string' && data.token.trim().length > 0) {
                const token = data.token.trim();
                
                // Проверяем формат JWT токена (должен содержать 3 части, разделенные точками)
                const tokenParts = token.split('.');
                if (tokenParts.length !== 3) {
                    console.error('Некорректный формат токена от сервера:', token.substring(0, 50));
                    return { success: false, error: 'Сервер вернул некорректный токен' };
                }
                
                // Сохраняем токен и данные
                try {
                    localStorage.setItem(this.STORAGE_TOKEN_KEY, token);
                    localStorage.setItem(this.STORAGE_USERNAME_KEY, username);
                    localStorage.setItem(this.STORAGE_API_URL_KEY, normalizedUrl);
                    
                    // Сохраняем информацию о пользователе
                    localStorage.setItem(this.STORAGE_USER_INFO_KEY, JSON.stringify({
                        username: username,
                        loginTime: new Date().toISOString()
                    }));

                    // Кросс-кабинетная сессия: кабинеты поставщика и управления
                    // читают токен из общих ключей authToken/userInfo.
                    localStorage.setItem('authToken', token);
                    localStorage.setItem('userInfo', JSON.stringify({ username: username }));

                    // Проверяем, что токен действительно сохранился
                    const savedToken = localStorage.getItem(this.STORAGE_TOKEN_KEY);
                    if (!savedToken || savedToken !== token) {
                        console.error('Токен не сохранился в localStorage');
                        return { success: false, error: 'Ошибка сохранения токена. Проверьте настройки браузера.' };
                    }
                    
                    console.log('Токен успешно сохранен, длина:', token.length, 'частей:', tokenParts.length);
                    return { success: true, token: token, role: data.role, redirectUrl: data.redirect_url };
                } catch (storageError) {
                    console.error('Ошибка сохранения в localStorage:', storageError);
                    return { success: false, error: 'Ошибка сохранения данных. Возможно, localStorage недоступен.' };
                }
            } else {
                console.error('Сервер не вернул токен или токен пустой');
                return { success: false, error: 'Сервер не вернул токен авторизации' };
            }
        } catch (error) {
            console.error('Ошибка при авторизации:', error);
            return { success: false, error: `Ошибка подключения: ${error.message}` };
        }
    }

    // Выход
    static logout() {
        localStorage.removeItem(this.STORAGE_TOKEN_KEY);
        localStorage.removeItem(this.STORAGE_USERNAME_KEY);
        localStorage.removeItem(this.STORAGE_USER_INFO_KEY);
        // чистим и общие кросс-кабинетные ключи, иначе после выхода
        // /login.html увидит сессию и снова уведёт в панель.
        localStorage.removeItem('authToken');
        localStorage.removeItem('userInfo');
        window.location.href = 'login.html';
    }

    // Проверка здоровья сервиса
    static async healthCheck(apiUrl) {
        try {
            const response = await fetch(`${apiUrl}/health`);
            const data = await response.json();
            return { success: response.ok && data.ok, data };
        } catch (error) {
            return { success: false, error: error.message };
        }
    }

    // Получение заголовков для авторизованных запросов
    static getAuthHeaders() {
        const token = this.getToken();
        return {
            'Authorization': `Bearer ${token}`,
            'Content-Type': 'application/json',
            'Accept': 'application/json'
        };
    }

    // Выполнение авторизованного запроса
    static async authenticatedFetch(url, options = {}) {
        if (!this.isAuthenticated()) {
            this.logout();
            throw new Error('Требуется авторизация');
        }

        const token = this.getToken();
        
        // Проверяем формат токена (JWT должен содержать 3 части, разделенные точками)
        if (!token || !token.includes('.') || token.split('.').length !== 3) {
            console.error('Некорректный формат токена:', token ? token.substring(0, 20) + '...' : 'токен отсутствует');
            this.logout();
            throw new Error('Токен авторизации поврежден. Пожалуйста, войдите заново.');
        }

        const headers = this.getAuthHeaders();
        if (options.headers) {
            Object.assign(headers, options.headers);
        }

        const apiUrl = this.getApiUrl();
        const fullUrl = url.startsWith('http') ? url : `${apiUrl}${url}`;

        console.log('Выполнение запроса:', {
            url: fullUrl,
            method: options.method || 'GET',
            hasAuth: !!headers.Authorization,
            tokenLength: token ? token.length : 0,
            tokenPreview: token ? token.substring(0, 20) + '...' : 'нет'
        });

        try {
            // Создаем AbortController для таймаута
            const controller = new AbortController();
            const timeoutId = setTimeout(() => controller.abort(), 30000); // 30 секунд таймаут
            
            let response;
            try {
                // Если body является FormData, не добавляем Content-Type заголовок (браузер сделает это сам)
                const isFormData = options.body instanceof FormData;
                const fetchHeaders = isFormData ? { Authorization: headers.Authorization } : headers;
                
                response = await fetch(fullUrl, {
                    ...options,
                    headers: fetchHeaders,
                    mode: 'cors',
                    credentials: 'omit',
                    signal: controller.signal
                });
            } finally {
                clearTimeout(timeoutId);
            }

            console.log('Ответ получен:', {
                status: response.status,
                statusText: response.statusText,
                ok: response.ok
            });

            if (response.status === 401) {
                console.warn('Получен 401, выполняем logout');
                this.logout();
                throw new Error('Сессия истекла. Требуется повторная авторизация');
            }

            return response;
        } catch (error) {
            console.error('Ошибка выполнения запроса:', error);
            
            // Обработка различных типов ошибок
            if (error.name === 'AbortError') {
                throw new Error(`Таймаут запроса к серверу: ${apiUrl}. Проверьте соединение с сервером.`);
            }
            
            // Проверяем различные варианты ошибок сети
            const errorMessage = error.message || error.toString() || '';
            const isNetworkError = 
                errorMessage.includes('Failed to fetch') || 
                errorMessage.includes('NetworkError') || 
                errorMessage.includes('ERR_EMPTY_RESPONSE') ||
                errorMessage.includes('ERR_CONNECTION_REFUSED') ||
                errorMessage.includes('ERR_CONNECTION_RESET') ||
                errorMessage.includes('ERR_NAME_NOT_RESOLVED') ||
                errorMessage.includes('ERR_INTERNET_DISCONNECTED') ||
                error.name === 'TypeError' && errorMessage.includes('fetch');
            
            if (isNetworkError) {
                const networkError = new Error(`Ошибка подключения: Не удалось подключиться к серверу ${apiUrl}.\n\nВозможные причины:\n• Сервер не запущен\n• Неправильный адрес сервера\n• Проблемы с сетью\n\nПроверьте настройки подключения в форме входа.`);
                networkError.originalError = error;
                networkError.apiUrl = apiUrl;
                throw networkError;
            }
            
            throw error;
        }
    }
}

// Экспорт для использования в других модулях
if (typeof window !== 'undefined') {
    window.AuthManager = AuthManager;
}

