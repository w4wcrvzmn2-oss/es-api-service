class AuthManager {
    static STORAGE_TOKEN_KEY = 'authToken';
    static STORAGE_USER_INFO_KEY = 'userInfo';

    static isAuthenticated() {
        const token = localStorage.getItem(this.STORAGE_TOKEN_KEY);
        if (!token) return false;
        const parts = token.split('.');
        if (parts.length !== 3) {
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
        window.location.replace(this.getLoginPath());
    }

    static getUserInfo() {
        const userInfo = localStorage.getItem(this.STORAGE_USER_INFO_KEY);
        try {
            return userInfo ? JSON.parse(userInfo) : null;
        } catch (_) {
            localStorage.removeItem(this.STORAGE_USER_INFO_KEY);
            return null;
        }
    }

    static getUsername() {
        const info = this.getUserInfo();
        return info ? info.username : 'Поставщик';
    }

    static getLoginPath() {
        return '/login.html';
    }
}

if (typeof window !== 'undefined') {
    window.AuthManager = AuthManager;
}
