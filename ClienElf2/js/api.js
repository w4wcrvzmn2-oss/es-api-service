// API базовый класс
// Автоматически определяем API URL из текущего адреса страницы
function getApiBase() {
    if (typeof window !== 'undefined' && window.location && window.location.origin) {
        const origin = window.location.origin;
        if (origin && origin !== 'null' && !origin.startsWith('file://')) {
            return origin;
        }
    }
    return 'http://localhost:8082';
}

const API_BASE = getApiBase();

class API {
    static async request(endpoint, options = {}) {
        const token = localStorage.getItem('authToken');
        const headers = {
            'Content-Type': 'application/json',
            'Accept': 'application/json',
            ...options.headers
        };
        
        if (token) {
            headers['Authorization'] = `Bearer ${token}`;
        }
        
        try {
            console.log(`API Request: ${options.method || 'GET'} ${API_BASE}${endpoint}`);
            
            const response = await fetch(`${API_BASE}${endpoint}`, {
                ...options,
                headers,
                mode: 'cors',
                credentials: 'omit'
            });
            
            console.log(`API Response: ${response.status} ${response.statusText}`);
            
            if (response.status === 401) {
                console.warn('401 Unauthorized - redirecting to login');
                localStorage.removeItem('authToken');
                // Определяем правильный путь к login.html
                const isInPages = window.location.pathname.includes('/pages/');
                window.location.href = isInPages ? '../login.html' : 'login.html';
                return null;
            }
            
            return response;
        } catch (error) {
            console.error('API Error:', error);
            throw error;
        }
    }
    
    static async get(endpoint) {
        try {
            const response = await this.request(endpoint, { method: 'GET' });
            if (response && response.ok) {
                const data = await response.json();
                return data;
            } else if (response) {
                console.error(`API Error ${response.status}: ${endpoint}`);
                const errorText = await response.text();
                console.error('Error response:', errorText);
            }
            return null;
        } catch (error) {
            console.error(`API GET ${endpoint} failed:`, error);
            return null;
        }
    }
    
    static async post(endpoint, data) {
        try {
            const response = await this.request(endpoint, {
                method: 'POST',
                body: JSON.stringify(data)
            });
            if (response && response.ok) {
                return response.json();
            } else if (response) {
                const errorText = await response.text();
                console.error(`API POST Error ${response.status}: ${endpoint}`, errorText);
                let msg = `HTTP ${response.status}`;
                try { msg = JSON.parse(errorText).error || msg; } catch(_) {}
                throw new Error(msg);
            }
            throw new Error('Нет ответа от сервера');
        } catch (error) {
            console.error(`API POST ${endpoint} failed:`, error);
            throw error;
        }
    }
    
    static async put(endpoint, data) {
        try {
            console.log(`API PUT Request: ${endpoint}`, data);
            const response = await this.request(endpoint, {
                method: 'PUT',
                body: JSON.stringify(data)
            });
            if (response && response.ok) {
                const result = await response.json();
                console.log(`API PUT Success: ${endpoint}`, result);
                return result;
            } else if (response) {
                const errorText = await response.text();
                console.error(`API PUT Error ${response.status}: ${endpoint}`);
                console.error('Error response:', errorText);
                throw new Error(errorText || `HTTP ${response.status}`);
            }
            return null;
        } catch (error) {
            console.error(`API PUT ${endpoint} failed:`, error);
            throw error;
        }
    }
    
    static async upload(endpoint, formData) {
        const token = localStorage.getItem('authToken');
        const headers = { 'Accept': 'application/json' };
        if (token) headers['Authorization'] = `Bearer ${token}`;

        const response = await fetch(`${API_BASE}${endpoint}`, {
            method: 'POST',
            headers,
            body: formData,
            mode: 'cors',
            credentials: 'omit'
        });

        if (response.status === 401) {
            localStorage.removeItem('authToken');
            const isInPages = window.location.pathname.includes('/pages/');
            window.location.href = isInPages ? '../login.html' : 'login.html';
            return null;
        }
        if (!response.ok) {
            const errorText = await response.text();
            let msg = `HTTP ${response.status}`;
            try { msg = JSON.parse(errorText).error || msg; } catch(_) {}
            throw new Error(msg);
        }
        return response.json();
    }

    static async delete(endpoint) {
        try {
            const response = await this.request(endpoint, { method: 'DELETE' });
            if (response && response.ok) {
                return await response.json();
            } else if (response) {
                const errorText = await response.text();
                console.error(`API DELETE Error ${response.status}: ${endpoint}`, errorText);
                let msg = `HTTP ${response.status}`;
                try { msg = JSON.parse(errorText).error || msg; } catch(_) {}
                throw new Error(msg);
            }
            throw new Error('Нет ответа от сервера');
        } catch (error) {
            console.error(`API DELETE ${endpoint} failed:`, error);
            throw error;
        }
    }
}

function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = String(text);
    return div.innerHTML;
}
