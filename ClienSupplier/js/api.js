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
            const response = await fetch(`${API_BASE}${endpoint}`, {
                ...options,
                headers,
                mode: 'cors',
                credentials: 'omit'
            });
            if (response.status === 401) {
                localStorage.removeItem('authToken');
                window.location.href = window.AuthManager ? AuthManager.getLoginPath() : 'index.html';
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
                return await response.json();
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
                let msg = `HTTP ${response.status}`;
                try { msg = JSON.parse(errorText).error || msg; } catch(_) {}
                throw new Error(msg);
            }
            throw new Error('Нет ответа от сервера');
        } catch (error) {
            throw error;
        }
    }

    static async put(endpoint, data) {
        try {
            const response = await this.request(endpoint, {
                method: 'PUT',
                body: JSON.stringify(data)
            });
            if (response && response.ok) {
                return await response.json();
            } else if (response) {
                const errorText = await response.text();
                throw new Error(errorText || `HTTP ${response.status}`);
            }
            return null;
        } catch (error) {
            throw error;
        }
    }

    static async patch(endpoint, data) {
        try {
            const response = await this.request(endpoint, {
                method: 'PATCH',
                body: JSON.stringify(data)
            });
            if (response && response.ok) {
                return await response.json();
            } else if (response) {
                const errorText = await response.text();
                throw new Error(errorText || `HTTP ${response.status}`);
            }
            return null;
        } catch (error) {
            throw error;
        }
    }

    static async delete(endpoint) {
        try {
            const response = await this.request(endpoint, { method: 'DELETE' });
            if (response && response.ok) {
                return await response.json();
            } else if (response) {
                const errorText = await response.text();
                let msg = `HTTP ${response.status}`;
                try { msg = JSON.parse(errorText).error || msg; } catch(_) {}
                throw new Error(msg);
            }
            throw new Error('Нет ответа от сервера');
        } catch (error) {
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
