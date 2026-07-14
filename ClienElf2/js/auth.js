// Менеджер авторизации
class AuthManager {
    static STORAGE_TOKEN_KEY = 'authToken';
    static STORAGE_USER_INFO_KEY = 'userInfo';

    static isAuthenticated() {
        const token = localStorage.getItem(this.STORAGE_TOKEN_KEY);
        if (!token) return false;
        
        // Проверяем формат JWT (3 части разделенные точками)
        const parts = token.split('.');
        if (parts.length !== 3) {
            console.warn('Некорректный формат токена, очищаем');
            this.logout();
            return false;
        }
        
        return true;
    }

    static getToken() {
        return localStorage.getItem(this.STORAGE_TOKEN_KEY);
    }

    static setToken(token) {
        localStorage.setItem(this.STORAGE_TOKEN_KEY, token);
    }

    static logout() {
        localStorage.removeItem(this.STORAGE_TOKEN_KEY);
        localStorage.removeItem(this.STORAGE_USER_INFO_KEY);
        // Определяем правильный путь к login.html
        const isInPages = window.location.pathname.includes('/pages/');
        window.location.href = isInPages ? '../login.html' : 'login.html';
    }

    static getUserInfo() {
        const userInfo = localStorage.getItem(this.STORAGE_USER_INFO_KEY);
        return userInfo ? JSON.parse(userInfo) : null;
    }

    static getUsername() {
        const info = this.getUserInfo();
        return info ? info.username : 'Пользователь';
    }
}

// Экспорт для глобального использования
if (typeof window !== 'undefined') {
    window.AuthManager = AuthManager;
}
