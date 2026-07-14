// ES API Service Test Client
class ESAPIClient {
    constructor() {
        this.token = null;
        this.apiUrl = 'http://localhost:8080';
        this.initializeEventListeners();
        this.loadSavedSettings();
    }

    initializeEventListeners() {
        // Авторизация
        document.getElementById('loginBtn').addEventListener('click', () => this.login());
        document.getElementById('logoutBtn').addEventListener('click', () => this.logout());
        
        // Запросы данных
        document.getElementById('fetchDataBtn').addEventListener('click', () => this.fetchData());
        document.getElementById('clearDataBtn').addEventListener('click', () => this.clearResults());
        document.getElementById('exportJsonBtn').addEventListener('click', () => this.exportJson());
        
        // Мониторинг
        const healthCheckBtn = document.getElementById('healthCheckBtn');
        if (healthCheckBtn) {
            healthCheckBtn.addEventListener('click', () => {
                console.log('Кнопка "Проверить состояние сервиса" нажата');
                this.healthCheck();
            });
        } else {
            console.error('Кнопка healthCheckBtn не найдена!');
        }
        
        // Сохранение настроек при изменении
        document.getElementById('apiUrl').addEventListener('change', () => this.saveSettings());
        document.getElementById('username').addEventListener('change', () => this.saveSettings());
    }

    loadSavedSettings() {
        const savedApiUrl = localStorage.getItem('es_api_url');
        const savedUsername = localStorage.getItem('es_api_username');
        
        if (savedApiUrl) {
            document.getElementById('apiUrl').value = savedApiUrl;
            this.apiUrl = savedApiUrl;
        }
        if (savedUsername) {
            document.getElementById('username').value = savedUsername;
        }
    }

    saveSettings() {
        this.apiUrl = document.getElementById('apiUrl').value;
        localStorage.setItem('es_api_url', this.apiUrl);
        localStorage.setItem('es_api_username', document.getElementById('username').value);
    }

    async login() {
        const username = document.getElementById('username').value;
        const password = document.getElementById('password').value;
        this.apiUrl = document.getElementById('apiUrl').value;

        if (!username || !password) {
            this.showError('Введите имя пользователя и пароль');
            return;
        }

        try {
            this.showLoading(true);
            this.clearMessages();

            const response = await fetch(`${this.apiUrl}/auth/login`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({ username, password })
            });

            const data = await response.json();

            if (response.ok) {
                this.token = data.token;
                this.showSuccess('Успешная авторизация!');
                this.updateUIAfterLogin();
                this.saveSettings();
            } else {
                this.showError(`Ошибка авторизации: ${data.error || 'Неизвестная ошибка'}`);
            }
        } catch (error) {
            this.showError(`Ошибка подключения: ${error.message}`);
        } finally {
            this.showLoading(false);
        }
    }

    logout() {
        this.token = null;
        this.updateUIAfterLogout();
        this.showSuccess('Вы вышли из системы');
        this.clearResults();
    }

    updateUIAfterLogin() {
        document.getElementById('loginBtn').style.display = 'none';
        document.getElementById('logoutBtn').style.display = 'inline-block';
        const dataPanel = document.querySelector('.data-panel');
        if (dataPanel) {
            dataPanel.style.display = 'block';
        }
        const importPanel = document.querySelector('.import-panel');
        if (importPanel) {
            importPanel.style.display = 'block';
        }
        document.getElementById('authStatus').innerHTML = '<span class="success">Авторизован</span>';
        this.initializeImportHandlers();
        this.loadSuppliers();
    }

    updateUIAfterLogout() {
        document.getElementById('loginBtn').style.display = 'inline-block';
        document.getElementById('logoutBtn').style.display = 'none';
        const dataPanel = document.querySelector('.data-panel');
        if (dataPanel) {
            dataPanel.style.display = 'none';
        }
        const importPanel = document.querySelector('.import-panel');
        if (importPanel) {
            importPanel.style.display = 'none';
        }
        document.getElementById('authStatus').innerHTML = '';
    }

    async fetchData() {
        if (!this.token) {
            this.showError('Сначала выполните авторизацию');
            return;
        }

        const tableInput = document.getElementById('tableInput');
        const table = tableInput ? tableInput.value.trim() : '';
        
        if (!table) {
            this.showError('Введите имя таблицы');
            return;
        }

        const limit = document.getElementById('limit').value;
        const offset = document.getElementById('offset').value;
        const updatedAfter = document.getElementById('updatedAfter').value;
        const idGt = document.getElementById('idGt').value;
        const columns = document.getElementById('columns').value;

        // Построение URL с параметрами
        const params = new URLSearchParams();
        if (limit) params.append('limit', limit);
        if (offset) params.append('offset', offset);
        if (updatedAfter) {
            // Конвертируем datetime-local в RFC3339
            const date = new Date(updatedAfter);
            params.append('updated_after', date.toISOString());
        }
        if (idGt) params.append('id_gt', idGt);
        if (columns) params.append('columns', columns);

        // Преобразуем название таблицы в нижний регистр для API endpoint
        const tableEndpoint = table.toLowerCase();
        const url = `${this.apiUrl}/api/${tableEndpoint}?${params.toString()}`;

        try {
            this.showLoading(true);
            this.clearMessages();

            // Показываем информацию о запросе
            this.showRequestInfo(url, params);

            const response = await fetch(url, {
                method: 'GET',
                headers: {
                    'Authorization': `Bearer ${this.token}`,
                    'Accept-Encoding': 'gzip'
                }
            });

            if (!response.ok) {
                const errorData = await response.json();
                throw new Error(errorData.error || `HTTP ${response.status}: ${response.statusText}`);
            }

            const data = await response.json();
            this.displayData(data);
            this.showSuccess(`Получено ${Array.isArray(data) ? data.length : 0} записей`);
            
            // Показываем кнопку экспорта
            document.getElementById('exportJsonBtn').style.display = 'inline-block';
            this.currentData = data;

        } catch (error) {
            this.showError(`Ошибка получения данных: ${error.message}`);
        } finally {
            this.showLoading(false);
        }
    }

    showRequestInfo(url, params) {
        const requestInfo = document.getElementById('requestInfo');
        requestInfo.innerHTML = `
            <strong>Запрос:</strong> ${url}<br>
            <strong>Параметры:</strong> ${params.toString() || 'Нет'}
        `;
    }

    displayData(data) {
        const jsonData = document.getElementById('jsonData');
        jsonData.textContent = JSON.stringify(data, null, 2);
    }

    clearResults() {
        document.getElementById('jsonData').textContent = '';
        document.getElementById('requestInfo').innerHTML = '';
        document.getElementById('exportJsonBtn').style.display = 'none';
        this.currentData = null;
        this.clearMessages();
    }

    exportJson() {
        if (!this.currentData) {
            this.showError('Нет данных для экспорта');
            return;
        }

        const dataStr = JSON.stringify(this.currentData, null, 2);
        const dataBlob = new Blob([dataStr], { type: 'application/json' });
        const url = URL.createObjectURL(dataBlob);
        
        const link = document.createElement('a');
        link.href = url;
        link.download = `es_api_data_${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.json`;
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
        URL.revokeObjectURL(url);
        
        this.showSuccess('Данные экспортированы в JSON файл');
    }

    async healthCheck() {
        try {
            console.log('Начинаем проверку состояния сервиса...');
            this.clearMessages();
            this.showLoading(true);
            
            const url = `${this.apiUrl}/health`;
            console.log('Отправляем запрос на:', url);
            
            const response = await fetch(url);
            console.log('Получен ответ:', response.status, response.statusText);
            
            const data = await response.json();
            console.log('Данные ответа:', data);
            
            if (response.ok && data.ok) {
                document.getElementById('healthStatus').innerHTML = 
                    '<span class="success">✅ Сервис работает нормально</span>';
                this.showSuccess('Сервис работает нормально');
            } else {
                document.getElementById('healthStatus').innerHTML = 
                    '<span class="error">❌ Проблемы с сервисом: ' + (data.error || 'Неизвестная ошибка') + '</span>';
                this.showError('Проблемы с сервисом: ' + (data.error || 'Неизвестная ошибка'));
            }
        } catch (error) {
            console.error('Ошибка при проверке состояния:', error);
            document.getElementById('healthStatus').innerHTML = 
                '<span class="error">❌ Ошибка подключения: ' + error.message + '</span>';
            this.showError('Ошибка подключения: ' + error.message);
        } finally {
            this.showLoading(false);
        }
    }

    showLoading(show) {
        const loadingDiv = document.getElementById('loadingIndicator');
        if (loadingDiv) {
            loadingDiv.style.display = show ? 'block' : 'none';
        }
    }

    showError(message) {
        // Показываем в основной панели результатов
        const errorDiv = document.getElementById('errorMessage');
        if (errorDiv) {
            errorDiv.textContent = message;
            errorDiv.style.display = 'block';
            setTimeout(() => {
                errorDiv.style.display = 'none';
            }, 5000);
        }
        // Также показываем в панели импорта, если она видна
        const importErrorDiv = document.getElementById('importErrorMessage');
        if (importErrorDiv) {
            importErrorDiv.textContent = message;
            importErrorDiv.style.display = 'block';
            setTimeout(() => {
                importErrorDiv.style.display = 'none';
            }, 5000);
        }
        console.error('Error:', message);
    }

    showSuccess(message) {
        // Показываем в основной панели результатов
        const successDiv = document.getElementById('successMessage');
        if (successDiv) {
            successDiv.textContent = message;
            successDiv.style.display = 'block';
            setTimeout(() => {
                successDiv.style.display = 'none';
            }, 3000);
        }
        // Также показываем в панели импорта, если она видна
        const importSuccessDiv = document.getElementById('importSuccessMessage');
        if (importSuccessDiv) {
            importSuccessDiv.textContent = message;
            importSuccessDiv.style.display = 'block';
            setTimeout(() => {
                importSuccessDiv.style.display = 'none';
            }, 3000);
        }
        console.log('Success:', message);
    }

    clearMessages() {
        const errorDiv = document.getElementById('errorMessage');
        const successDiv = document.getElementById('successMessage');
        if (errorDiv) errorDiv.style.display = 'none';
        if (successDiv) successDiv.style.display = 'none';
    }
}

// Инициализация клиента при загрузке страницы
document.addEventListener('DOMContentLoaded', () => {
    window.esApiClient = new ESAPIClient();
    
    // Автоматическая проверка состояния сервиса при загрузке
    setTimeout(() => {
        window.esApiClient.healthCheck();
    }, 1000);
});

// Утилиты для работы с датами
function setCurrentDateTime() {
    const now = new Date();
    const year = now.getFullYear();
    const month = String(now.getMonth() + 1).padStart(2, '0');
    const day = String(now.getDate()).padStart(2, '0');
    const hours = String(now.getHours()).padStart(2, '0');
    const minutes = String(now.getMinutes()).padStart(2, '0');
    
    const dateTimeString = `${year}-${month}-${day}T${hours}:${minutes}`;
    document.getElementById('updatedAfter').value = dateTimeString;
}

// Добавляем кнопку для установки текущего времени
document.addEventListener('DOMContentLoaded', () => {
    const updatedAfterGroup = document.querySelector('input[type="datetime-local"]').parentElement;
    const setNowBtn = document.createElement('button');
    setNowBtn.type = 'button';
    setNowBtn.className = 'btn btn-secondary';
    setNowBtn.textContent = 'Сейчас';
    setNowBtn.style.marginTop = '5px';
    setNowBtn.onclick = setCurrentDateTime;
    updatedAfterGroup.appendChild(setNowBtn);
});

// Обработка ошибок CORS
window.addEventListener('error', (event) => {
    if (event.message.includes('CORS')) {
        console.error('CORS ошибка. Убедитесь, что сервер поддерживает CORS или используйте браузер с отключенной проверкой CORS.');
    }
});

// Функции для импорта прайсов
ESAPIClient.prototype.initializeImportHandlers = function() {
    document.getElementById('refreshSuppliersBtn').addEventListener('click', () => this.loadSuppliers());
    document.getElementById('addSupplierBtn').addEventListener('click', () => this.showSupplierForm());
    document.getElementById('saveSupplierBtn').addEventListener('click', () => this.saveSupplier());
    document.getElementById('cancelSupplierBtn').addEventListener('click', () => this.hideSupplierForm());
    
    document.getElementById('supplierSelect').addEventListener('change', () => {
        this.onSupplierChange();
        // Автоматически загружаем импорты при выборе поставщика
        this.loadInvoiceImports();
    });
    document.getElementById('addImportPointBtn').addEventListener('click', () => this.showImportPointForm());
    document.getElementById('saveImportPointBtn').addEventListener('click', () => this.saveImportPoint());
    document.getElementById('cancelImportPointBtn').addEventListener('click', () => this.hideImportPointForm());
    
    document.getElementById('importPointSelect').addEventListener('change', () => this.onImportPointChange());
    document.getElementById('analyzeDBFBtn').addEventListener('click', () => this.analyzeDBFFile());
    document.getElementById('saveMappingBtn').addEventListener('click', () => this.saveMapping());
    
    document.getElementById('importFileBtn').addEventListener('click', () => this.importFile());
    
    // Сопоставление прайсов
    document.getElementById('refreshImportsBtn').addEventListener('click', () => this.loadInvoiceImports());
    document.getElementById('startMatchingBtn').addEventListener('click', () => this.startMatching());
    document.getElementById('loadPricesBtn').addEventListener('click', () => this.loadSupplierPrices());
    document.getElementById('loadPriceSummaryBtn').addEventListener('click', () => this.loadPriceSummary());
};

ESAPIClient.prototype.loadSuppliers = async function() {
    try {
        const response = await fetch(`${this.apiUrl}/api/suppliers`, {
            headers: { 'Authorization': `Bearer ${this.token}` }
        });
        const suppliers = await response.json();
        
        const select = document.getElementById('supplierSelect');
        select.innerHTML = '<option value="">-- Выберите поставщика --</option>';
        suppliers.forEach(s => {
            const option = document.createElement('option');
            option.value = s.supplier_id;
            option.textContent = s.name;
            select.appendChild(option);
        });
    } catch (error) {
        this.showError(`Ошибка загрузки поставщиков: ${error.message}`);
    }
};

ESAPIClient.prototype.onSupplierChange = async function() {
    const supplierID = document.getElementById('supplierSelect').value;
    if (!supplierID) {
        const select = document.getElementById('importPointSelect');
        select.innerHTML = '<option value="">-- Выберите точку импорта --</option>';
        return;
    }
    
    console.log('Выбран поставщик:', supplierID);
    await this.loadImportPoints(supplierID);
};

ESAPIClient.prototype.loadImportPoints = async function(supplierID) {
    try {
        const response = await fetch(`${this.apiUrl}/api/import-points?supplier_id=${supplierID}`, {
            headers: { 'Authorization': `Bearer ${this.token}` }
        });
        
        if (!response.ok) {
            let errorText = `HTTP ${response.status}: ${response.statusText}`;
            try {
                const error = await response.json();
                errorText = error.error || errorText;
            } catch (e) {
                const text = await response.text();
                if (text) errorText = text;
            }
            console.error('Ошибка загрузки точек импорта:', errorText);
            // Не показываем ошибку, просто очищаем список
            const select = document.getElementById('importPointSelect');
            select.innerHTML = '<option value="">-- Выберите точку импорта --</option>';
            return;
        }
        
        const data = await response.json();
        console.log('Загружены точки импорта:', data);
        
        // Проверяем, что это массив
        const importPoints = Array.isArray(data) ? data : [];
        
        const select = document.getElementById('importPointSelect');
        select.innerHTML = '<option value="">-- Выберите точку импорта --</option>';
        importPoints.forEach(ip => {
            const option = document.createElement('option');
            option.value = ip.import_point_id;
            option.textContent = ip.name;
            select.appendChild(option);
        });
    } catch (error) {
        console.error('Исключение при загрузке точек импорта:', error);
        // Не показываем ошибку, просто очищаем список
        const select = document.getElementById('importPointSelect');
        select.innerHTML = '<option value="">-- Выберите точку импорта --</option>';
    }
};

ESAPIClient.prototype.onImportPointChange = function() {
    const importPointID = document.getElementById('importPointSelect').value;
    if (importPointID) {
        document.getElementById('mappingSection').style.display = 'block';
        document.getElementById('importSection').style.display = 'block';
        this.loadFieldMappings(importPointID);
    } else {
        document.getElementById('mappingSection').style.display = 'none';
        document.getElementById('importSection').style.display = 'none';
    }
};

ESAPIClient.prototype.showSupplierForm = function() {
    document.getElementById('supplierForm').style.display = 'block';
};

ESAPIClient.prototype.hideSupplierForm = function() {
    document.getElementById('supplierForm').style.display = 'none';
    document.getElementById('supplierName').value = '';
    document.getElementById('supplierINN').value = '';
    document.getElementById('supplierAddress').value = '';
    document.getElementById('supplierContacts').value = '';
};

ESAPIClient.prototype.saveSupplier = async function() {
    const name = document.getElementById('supplierName').value.trim();
    
    if (!name) {
        this.showError('Введите название поставщика');
        return;
    }
    
    const supplier = {
        name: name,
        inn: document.getElementById('supplierINN').value.trim() || null,
        address: document.getElementById('supplierAddress').value.trim() || null,
        contacts: document.getElementById('supplierContacts').value.trim() || null,
        is_active: true
    };
    
    console.log('Сохранение поставщика:', supplier);
    
    try {
        this.showLoading(true);
        
        if (!this.token) {
            this.showError('Токен авторизации отсутствует. Выполните вход заново.');
            return;
        }
        
        console.log('Отправка запроса на:', `${this.apiUrl}/api/suppliers/create`);
        console.log('Токен:', this.token.substring(0, 20) + '...');
        console.log('Данные:', supplier);
        
        const response = await fetch(`${this.apiUrl}/api/suppliers/create`, {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${this.token}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify(supplier)
        });
        
        console.log('Ответ сервера:', response.status, response.statusText);
        console.log('Заголовки ответа:', Object.fromEntries(response.headers.entries()));
        
        if (response.ok) {
            const result = await response.json();
            console.log('Результат:', result);
            this.showSuccess('Поставщик успешно создан');
            this.hideSupplierForm();
            await this.loadSuppliers();
        } else {
            let errorText = `HTTP ${response.status}: ${response.statusText}`;
            try {
                const error = await response.json();
                errorText = error.error || errorText;
                console.error('Ошибка JSON:', error);
            } catch (e) {
                const text = await response.text();
                if (text) {
                    errorText = text;
                    console.error('Ошибка текста:', text);
                }
            }
            console.error('Ошибка создания поставщика:', errorText);
            this.showError(`Ошибка: ${errorText}`);
        }
    } catch (error) {
        console.error('Исключение при создании поставщика:', error);
        console.error('Тип ошибки:', error.name);
        console.error('Стек ошибки:', error.stack);
        
        let errorMessage = error.message;
        if (error.message === 'Failed to fetch') {
            errorMessage = 'Не удалось подключиться к серверу. Проверьте, что сервер запущен и доступен по адресу ' + this.apiUrl;
        }
        
        this.showError(`Ошибка создания поставщика: ${errorMessage}`);
    } finally {
        this.showLoading(false);
    }
};

ESAPIClient.prototype.showImportPointForm = function() {
    const supplierID = document.getElementById('supplierSelect').value;
    if (!supplierID) {
        this.showError('Сначала выберите поставщика');
        return;
    }
    document.getElementById('importPointForm').style.display = 'block';
};

ESAPIClient.prototype.hideImportPointForm = function() {
    document.getElementById('importPointForm').style.display = 'none';
    document.getElementById('importPointName').value = '';
    document.getElementById('importPointDescription').value = '';
};

ESAPIClient.prototype.saveImportPoint = async function() {
    const supplierID = document.getElementById('supplierSelect').value.trim();
    const name = document.getElementById('importPointName').value.trim();
    
    if (!supplierID) {
        this.showError('Сначала выберите поставщика');
        return;
    }
    
    if (!name) {
        this.showError('Введите название точки импорта');
        return;
    }
    
    // Проверяем формат UUID
    const uuidRegex = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
    if (!uuidRegex.test(supplierID)) {
        this.showError(`Неверный формат ID поставщика: ${supplierID}`);
        console.error('Неверный формат SupplierID:', supplierID);
        return;
    }
    
    const importPoint = {
        supplier_id: supplierID,
        name: name,
        description: document.getElementById('importPointDescription').value.trim() || null,
        is_active: true
    };
    
    console.log('Создание точки импорта:', importPoint);
    
    try {
        this.showLoading(true);
        const response = await fetch(`${this.apiUrl}/api/import-points/create`, {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${this.token}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify(importPoint)
        });
        
        console.log('Ответ сервера:', response.status, response.statusText);
        
        if (response.ok) {
            const result = await response.json();
            console.log('Результат:', result);
            this.showSuccess('Точка импорта успешно создана');
            this.hideImportPointForm();
            await this.loadImportPoints(supplierID);
        } else {
            let errorText = `HTTP ${response.status}: ${response.statusText}`;
            try {
                const error = await response.json();
                errorText = error.error || errorText;
                console.error('Ошибка JSON:', error);
            } catch (e) {
                const text = await response.text();
                if (text) {
                    errorText = text;
                    console.error('Ошибка текста:', text);
                }
            }
            console.error('Ошибка создания точки импорта:', errorText);
            this.showError(`Ошибка: ${errorText}`);
        }
    } catch (error) {
        console.error('Исключение при создании точки импорта:', error);
        this.showError(`Ошибка создания точки импорта: ${error.message}`);
    } finally {
        this.showLoading(false);
    }
};

ESAPIClient.prototype.loadFieldMappings = async function(importPointID) {
    try {
        const response = await fetch(`${this.apiUrl}/api/field-mappings?import_point_id=${importPointID}`, {
            headers: { 'Authorization': `Bearer ${this.token}` }
        });
        const mappings = await response.json();
        
        // Отображение маппинга будет добавлено позже
        console.log('Mappings loaded:', mappings);
    } catch (error) {
        this.showError(`Ошибка загрузки маппинга: ${error.message}`);
    }
};

ESAPIClient.prototype.analyzeDBFFile = async function() {
    const filePath = document.getElementById('dbfFilePathInput').value.trim();
    
    if (!filePath) {
        this.showError('Введите путь к DBF файлу');
        return;
    }
    
    try {
        this.showLoading(true);
        const response = await fetch(`${this.apiUrl}/api/dbf/analyze`, {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${this.token}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ file_path: filePath })
        });
        
        if (response.ok) {
            const data = await response.json();
            console.log('Результат анализа DBF:', data);
            
            // Показываем информацию о файле
            const fileInfo = document.getElementById('dbfFileInfo');
            fileInfo.innerHTML = `
                <strong>Файл:</strong> ${data.file_path}<br>
                <strong>Количество записей:</strong> ${data.records_count}<br>
                <strong>Количество полей:</strong> ${data.field_count}
            `;
            
            // Загружаем список целевых полей
            await this.loadTargetFields();
            
            // Отображаем таблицу маппинга
            this.displayMappingTable(data.fields);
            document.getElementById('dbfAnalysisResult').style.display = 'block';
            
        } else {
            const error = await response.json();
            this.showError(`Ошибка анализа файла: ${error.error || 'Неизвестная ошибка'}`);
        }
    } catch (error) {
        console.error('Ошибка анализа DBF:', error);
        this.showError(`Ошибка анализа файла: ${error.message}`);
    } finally {
        this.showLoading(false);
    }
};

ESAPIClient.prototype.loadTargetFields = async function() {
    try {
        const response = await fetch(`${this.apiUrl}/api/target-fields`, {
            headers: { 'Authorization': `Bearer ${this.token}` }
        });
        const data = await response.json();
        this.targetFields = data.target_fields;
    } catch (error) {
        console.error('Ошибка загрузки целевых полей:', error);
        // Используем список по умолчанию
        this.targetFields = [
            {name: "invoice_number", type: "NVARCHAR", description: "Номер прайса"},
            {name: "invoice_date", type: "DATETIME", description: "Дата прайса"},
            {name: "item_code", type: "NVARCHAR", description: "Код товара"},
            {name: "item_name", type: "NVARCHAR", description: "Наименование товара"},
            {name: "quantity", type: "DECIMAL", description: "Количество"},
            {name: "price", type: "DECIMAL", description: "Цена"},
            {name: "amount", type: "DECIMAL", description: "Сумма"},
            {name: "batch_number", type: "NVARCHAR", description: "Номер партии"},
            {name: "expiry_date", type: "DATETIME", description: "Срок годности"},
            {name: "barcode", type: "NVARCHAR", description: "Штрихкод"}
        ];
    }
};

ESAPIClient.prototype.displayMappingTable = function(dbfFields) {
    const tbody = document.getElementById('mappingTableBody');
    tbody.innerHTML = '';
    
    dbfFields.forEach((field, index) => {
        const row = document.createElement('tr');
        
        // Поле DBF
        const dbfCell = document.createElement('td');
        dbfCell.style.padding = '10px';
        dbfCell.style.border = '1px solid #ddd';
        dbfCell.innerHTML = `<strong>${field.name}</strong><br><small>${field.type}(${field.length})</small>`;
        
        // Стрелка
        const arrowCell = document.createElement('td');
        arrowCell.style.padding = '10px';
        arrowCell.style.border = '1px solid #ddd';
        arrowCell.style.textAlign = 'center';
        arrowCell.textContent = '→';
        
        // Целевое поле
        const targetCell = document.createElement('td');
        targetCell.style.padding = '10px';
        targetCell.style.border = '1px solid #ddd';
        const targetSelect = document.createElement('select');
        targetSelect.className = 'form-control';
        targetSelect.style.width = '100%';
        targetSelect.id = `target_${index}`;
        targetSelect.innerHTML = '<option value="">-- Не сопоставлять --</option>';
        if (this.targetFields) {
            this.targetFields.forEach(tf => {
                const option = document.createElement('option');
                option.value = tf.name;
                option.textContent = `${tf.name} (${tf.description})`;
                targetSelect.appendChild(option);
            });
        }
        targetCell.appendChild(targetSelect);
        
        // Тип данных
        const typeCell = document.createElement('td');
        typeCell.style.padding = '10px';
        typeCell.style.border = '1px solid #ddd';
        const typeSelect = document.createElement('select');
        typeSelect.className = 'form-control';
        typeSelect.style.width = '100%';
        typeSelect.id = `type_${index}`;
        ['NVARCHAR', 'DECIMAL', 'INT', 'DATETIME', 'DATE'].forEach(t => {
            const option = document.createElement('option');
            option.value = t;
            option.textContent = t;
            if (field.type === 'N' || field.type === 'F') {
                if (field.decimals > 0 && t === 'DECIMAL') option.selected = true;
                else if (field.decimals === 0 && t === 'INT') option.selected = true;
            } else if (field.type === 'D' && t === 'DATE') option.selected = true;
            else if (t === 'NVARCHAR') option.selected = true;
            typeSelect.appendChild(option);
        });
        typeCell.appendChild(typeSelect);
        
        // Обязательное
        const requiredCell = document.createElement('td');
        requiredCell.style.padding = '10px';
        requiredCell.style.border = '1px solid #ddd';
        requiredCell.style.textAlign = 'center';
        const requiredCheck = document.createElement('input');
        requiredCheck.type = 'checkbox';
        requiredCheck.id = `required_${index}`;
        requiredCell.appendChild(requiredCheck);
        
        // Тип DBF
        const typeDbfCell = document.createElement('td');
        typeDbfCell.style.padding = '10px';
        typeDbfCell.style.border = '1px solid #ddd';
        typeDbfCell.textContent = field.type;
        
        row.appendChild(dbfCell);
        row.appendChild(typeDbfCell);
        row.appendChild(arrowCell);
        row.appendChild(targetCell);
        row.appendChild(typeCell);
        row.appendChild(requiredCell);
        
        tbody.appendChild(row);
    });
};

ESAPIClient.prototype.saveMapping = async function() {
    const importPointID = document.getElementById('importPointSelect').value;
    if (!importPointID) {
        this.showError('Сначала выберите точку импорта');
        return;
    }
    
    const tbody = document.getElementById('mappingTableBody');
    const rows = tbody.querySelectorAll('tr');
    const mappings = [];
    
    rows.forEach((row, index) => {
        const targetSelect = document.getElementById(`target_${index}`);
        const typeSelect = document.getElementById(`type_${index}`);
        const requiredCheck = document.getElementById(`required_${index}`);
        
        if (!targetSelect || !targetSelect.value) {
            return; // Пропускаем не сопоставленные поля
        }
        
        // Получаем имя DBF поля из первой ячейки
        const dbfFieldName = row.cells[0].querySelector('strong').textContent.trim();
        
        mappings.push({
            dbf_field_name: dbfFieldName,
            target_field_name: targetSelect.value,
            data_type: typeSelect.value,
            is_required: requiredCheck.checked,
            display_order: index
        });
    });
    
    if (mappings.length === 0) {
        this.showError('Сопоставьте хотя бы одно поле');
        return;
    }
    
    try {
        this.showLoading(true);
        
        // Сохраняем каждый маппинг
        let savedCount = 0;
        for (const mapping of mappings) {
            const response = await fetch(`${this.apiUrl}/api/field-mappings/save?import_point_id=${importPointID}`, {
                method: 'POST',
                headers: {
                    'Authorization': `Bearer ${this.token}`,
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(mapping)
            });
            
            if (response.ok) {
                savedCount++;
            } else {
                const error = await response.json();
                console.error('Ошибка сохранения маппинга:', error);
            }
        }
        
        if (savedCount === mappings.length) {
            this.showSuccess(`Маппинг сохранен: ${savedCount} полей`);
            await this.loadFieldMappings(importPointID);
        } else {
            this.showError(`Сохранено ${savedCount} из ${mappings.length} маппингов`);
        }
    } catch (error) {
        console.error('Ошибка сохранения маппинга:', error);
        this.showError(`Ошибка сохранения маппинга: ${error.message}`);
    } finally {
        this.showLoading(false);
    }
};

ESAPIClient.prototype.importFile = async function() {
    const importPointID = document.getElementById('importPointSelect').value.trim();
    const filePath = document.getElementById('importFilePath').value.trim();
    
    if (!importPointID) {
        this.showError('Сначала выберите точку импорта');
        return;
    }
    
    if (!filePath) {
        this.showError('Введите путь к DBF файлу');
        return;
    }
    
    // Проверяем формат UUID
    const uuidRegex = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
    if (!uuidRegex.test(importPointID)) {
        this.showError(`Неверный формат ID точки импорта: ${importPointID}`);
        console.error('Неверный формат ImportPointID:', importPointID);
        return;
    }
    
    const statusDiv = document.getElementById('importStatus');
    statusDiv.innerHTML = '<div style="color: #007bff;">Запуск импорта...</div>';
    
    try {
        this.showLoading(true);
        console.log('Запуск импорта:', { import_point_id: importPointID, file_path: filePath });
        
        const response = await fetch(`${this.apiUrl}/api/import/file`, {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${this.token}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({
                import_point_id: importPointID,
                file_path: filePath
            })
        });
        
        console.log('Ответ сервера на импорт:', response.status, response.statusText);
        
        if (response.ok) {
            const result = await response.json();
            console.log('Результат импорта:', result);
            
            statusDiv.innerHTML = `<div style="color: #28a745; font-weight: bold;">✅ ${result.message || 'Импорт запущен'}</div>`;
            this.showSuccess(`Импорт запущен в фоновом режиме: ${result.file_path || filePath}`);
            
            // Очищаем поле после успешного запуска
            document.getElementById('importFilePath').value = '';
            
            // Автоматически обновляем список импортов через 3 секунды
            setTimeout(() => {
                this.loadInvoiceImports();
            }, 3000);
        } else {
            let errorText = `HTTP ${response.status}: ${response.statusText}`;
            try {
                const error = await response.json();
                errorText = error.error || errorText;
                console.error('Ошибка JSON:', error);
            } catch (e) {
                const text = await response.text();
                if (text) {
                    errorText = text;
                    console.error('Ошибка текста:', text);
                }
            }
            statusDiv.innerHTML = `<div style="color: #dc3545; font-weight: bold;">❌ ${errorText}</div>`;
            this.showError(`Ошибка запуска импорта: ${errorText}`);
        }
    } catch (error) {
        console.error('Исключение при импорте:', error);
        statusDiv.innerHTML = `<div style="color: #dc3545; font-weight: bold;">❌ Ошибка: ${error.message}</div>`;
        this.showError(`Ошибка импорта: ${error.message}`);
    } finally {
        this.showLoading(false);
    }
};

// ===== Функции сопоставления прайсов =====

ESAPIClient.prototype.loadInvoiceImports = async function() {
    const supplierID = document.getElementById('supplierSelect').value;
    const select = document.getElementById('invoiceImportSelect');
    
    if (!supplierID) {
        select.innerHTML = '<option value="">-- Сначала выберите поставщика --</option>';
        return;
    }
    
    if (!this.token) {
        select.innerHTML = '<option value="">-- Необходима авторизация --</option>';
        this.showError('Необходима авторизация. Пожалуйста, войдите в систему.');
        return;
    }
    
    try {
        select.innerHTML = '<option value="">-- Загрузка... --</option>';
        select.disabled = true;
        
        const url = `${this.apiUrl}/api/invoice-imports?supplier_id=${supplierID}`;
        console.log('Загрузка импортов:', url);
        console.log('Token:', this.token ? this.token.substring(0, 20) + '...' : 'отсутствует');
        
        // Используем supplier_id для получения всех импортов поставщика
        const response = await fetch(url, {
            method: 'GET',
            headers: { 
                'Authorization': `Bearer ${this.token}`,
                'Accept': 'application/json'
            },
            mode: 'cors',
            credentials: 'omit'
        });
        
        console.log('Ответ сервера:', response.status, response.statusText);
        
        if (!response.ok) {
            let errorText = `HTTP ${response.status}: ${response.statusText}`;
            try {
                const error = await response.json();
                errorText = error.error || errorText;
            } catch (e) {
                const text = await response.text();
                if (text) errorText = text;
            }
            select.innerHTML = `<option value="">-- Ошибка загрузки: ${errorText} --</option>`;
            console.error('Ошибка загрузки импортов:', errorText);
            this.showError(`Ошибка загрузки импортов: ${errorText}`);
            select.disabled = false;
            return;
        }
        
        const data = await response.json();
        console.log('Импорты загружены:', data);
        
        select.innerHTML = '<option value="">-- Выберите импорт --</option>';
        
        if (data.imports && Array.isArray(data.imports) && data.imports.length > 0) {
            data.imports.forEach(imp => {
                const option = document.createElement('option');
                option.value = imp.invoice_import_id;
                const statusText = imp.import_status || 'UNKNOWN';
                const processed = imp.records_processed || 0;
                const total = imp.records_total || 0;
                const date = imp.created_at ? new Date(imp.created_at).toLocaleDateString('ru-RU') : '';
                option.textContent = `${imp.file_name} [${statusText}] ${processed}/${total} ${date ? '(' + date + ')' : ''}`;
                select.appendChild(option);
            });
            console.log(`Загружено импортов: ${data.imports.length}`);
            this.showSuccess(`Загружено импортов: ${data.imports.length}`);
        } else {
            select.innerHTML = '<option value="">-- Нет импортов для этого поставщика --</option>';
            console.log('Импорты не найдены');
        }
        
        select.disabled = false;
    } catch (error) {
        console.error('Исключение при загрузке импортов:', error);
        let errorMessage = error.message;
        
        if (errorMessage === 'Failed to fetch') {
            errorMessage = 'Не удалось подключиться к серверу. Проверьте, что сервер запущен и доступен по адресу ' + this.apiUrl;
        }
        
        select.innerHTML = `<option value="">-- Ошибка: ${errorMessage} --</option>`;
        this.showError(`Ошибка загрузки импортов: ${errorMessage}`);
        select.disabled = false;
    }
};

ESAPIClient.prototype.startMatching = async function() {
    const invoiceImportID = document.getElementById('invoiceImportSelect').value.trim();
    
    if (!invoiceImportID) {
        this.showError('Выберите импорт для сопоставления');
        return;
    }
    
    const statusDiv = document.getElementById('matchingStatus');
    statusDiv.innerHTML = '<div style="color: #007bff;">⏳ Запуск сопоставления...</div>';
    
    try {
        this.showLoading(true);
        const response = await fetch(`${this.apiUrl}/api/match/invoice-data`, {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${this.token}`,
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({
                invoice_import_id: invoiceImportID
            })
        });
        
        console.log('Ответ сервера на сопоставление:', response.status, response.statusText);
        
        if (response.ok) {
            const result = await response.json();
            console.log('Результат сопоставления:', result);
            
            statusDiv.innerHTML = `<div style="color: #28a745; font-weight: bold;">✅ ${result.message || 'Сопоставление запущено'}</div>`;
            this.showSuccess(`Сопоставление запущено в фоновом режиме`);
            
            // Предлагаем загрузить прайсы через некоторое время
            setTimeout(() => {
                statusDiv.innerHTML += '<div style="color: #007bff; margin-top: 10px;">💡 Можно загрузить прайсы через несколько секунд</div>';
            }, 2000);
        } else {
            let errorText = `HTTP ${response.status}: ${response.statusText}`;
            try {
                const error = await response.json();
                errorText = error.error || errorText;
                console.error('Ошибка JSON:', error);
            } catch (e) {
                const text = await response.text();
                if (text) {
                    errorText = text;
                    console.error('Ошибка текста:', text);
                }
            }
            statusDiv.innerHTML = `<div style="color: #dc3545; font-weight: bold;">❌ ${errorText}</div>`;
            this.showError(`Ошибка запуска сопоставления: ${errorText}`);
        }
    } catch (error) {
        console.error('Исключение при сопоставлении:', error);
        statusDiv.innerHTML = `<div style="color: #dc3545; font-weight: bold;">❌ Ошибка: ${error.message}</div>`;
        this.showError(`Ошибка сопоставления: ${error.message}`);
    } finally {
        this.showLoading(false);
    }
};

ESAPIClient.prototype.loadSupplierPrices = async function() {
    const supplierID = document.getElementById('supplierSelect').value;
    if (!supplierID) {
        this.showError('Сначала выберите поставщика');
        return;
    }
    
    if (!this.token) {
        this.showError('Необходима авторизация. Пожалуйста, войдите в систему.');
        return;
    }
    
    try {
        this.showLoading(true);
        console.log('Загрузка прайсов для поставщика:', supplierID);
        console.log('URL:', `${this.apiUrl}/api/supplier-prices?supplier_id=${supplierID}`);
        console.log('Token:', this.token ? this.token.substring(0, 20) + '...' : 'отсутствует');
        
        const url = `${this.apiUrl}/api/supplier-prices?supplier_id=${supplierID}`;
        console.log('Полный URL запроса:', url);
        
        const response = await fetch(url, {
            method: 'GET',
            headers: { 
                'Authorization': `Bearer ${this.token}`,
                'Accept': 'application/json'
            },
            mode: 'cors',
            credentials: 'omit'
        });
        
        console.log('Ответ сервера:', response.status, response.statusText);
        
        if (!response.ok) {
            let errorText = `HTTP ${response.status}: ${response.statusText}`;
            try {
                const error = await response.json();
                errorText = error.error || errorText;
                console.error('Ошибка JSON:', error);
            } catch (e) {
                const text = await response.text();
                if (text) {
                    errorText = text;
                    console.error('Ошибка текста:', text);
                }
            }
            throw new Error(errorText);
        }
        
        const data = await response.json();
        console.log('Прайсы загружены:', data);
        
        const table = document.getElementById('pricesTable');
        const tbody = document.getElementById('pricesTableBody');
        const summaryDiv = document.getElementById('pricesSummary');
        tbody.innerHTML = '';
        
        // Скрываем сводный прайс при загрузке полного списка
        if (summaryDiv) {
            summaryDiv.style.display = 'none';
        }
        
        if (data.prices && data.prices.length > 0) {
            table.style.display = 'table';
            
            data.prices.forEach(price => {
                const row = document.createElement('tr');
                
                const matchMethod = price.match_method || '-';
                const confidence = price.match_confidence ? price.match_confidence.toFixed(1) + '%' : '-';
                const confirmed = price.is_confirmed ? '✅' : '⏳';
                
                row.innerHTML = `
                    <td style="padding: 8px; border: 1px solid #ddd;">${this.escapeHtml(price.drug_name || price.supplier_item_name || '-')}</td>
                    <td style="padding: 8px; border: 1px solid #ddd;">${this.escapeHtml(price.inn || '-')}</td>
                    <td style="padding: 8px; border: 1px solid #ddd;">${this.escapeHtml(price.cure_form || '-')}</td>
                    <td style="padding: 8px; border: 1px solid #ddd; text-align: right;">${price.price ? price.price.toFixed(2) : '-'}</td>
                    <td style="padding: 8px; border: 1px solid #ddd;">${price.invoice_date ? new Date(price.invoice_date).toLocaleDateString('ru-RU') : '-'}</td>
                    <td style="padding: 8px; border: 1px solid #ddd;">${matchMethod}</td>
                    <td style="padding: 8px; border: 1px solid #ddd;">${confidence}</td>
                    <td style="padding: 8px; border: 1px solid #ddd;">${confirmed}</td>
                `;
                tbody.appendChild(row);
            });
            
            this.showSuccess(`Загружено прайсов: ${data.total_prices}`);
        } else {
            table.style.display = 'none';
            this.showError('Прайсы не найдены. Сначала запустите сопоставление для этого поставщика.');
        }
    } catch (error) {
        console.error('Ошибка загрузки прайсов:', error);
        let errorMessage = error.message;
        
        if (errorMessage === 'Failed to fetch') {
            errorMessage = 'Не удалось подключиться к серверу. Проверьте, что сервер запущен и доступен по адресу ' + this.apiUrl;
        }
        
        this.showError(`Ошибка загрузки прайсов: ${errorMessage}`);
    } finally {
        this.showLoading(false);
    }
};

ESAPIClient.prototype.loadPriceSummary = async function() {
    // supplier_id опционален - если не выбран, загружаем сводный прайс всех поставщиков
    const supplierSelect = document.getElementById('supplierSelect');
    const supplierID = supplierSelect ? supplierSelect.value : '';
    
    if (!this.token) {
        this.showError('Необходима авторизация. Пожалуйста, войдите в систему.');
        return;
    }
    
    try {
        this.showLoading(true);
        
        let url = `${this.apiUrl}/api/supplier-prices/summary`;
        if (supplierID) {
            url += `?supplier_id=${supplierID}`;
            console.log('Загрузка сводного прайса для поставщика:', supplierID);
        } else {
            console.log('Загрузка сводного прайса всех поставщиков');
        }
        console.log('Полный URL запроса:', url);
        
        const response = await fetch(url, {
            method: 'GET',
            headers: { 
                'Authorization': `Bearer ${this.token}`,
                'Accept': 'application/json'
            },
            mode: 'cors',
            credentials: 'omit'
        });
        
        console.log('Ответ сервера:', response.status, response.statusText);
        
        if (!response.ok) {
            let errorText = `HTTP ${response.status}: ${response.statusText}`;
            try {
                const error = await response.json();
                errorText = error.error || errorText;
            } catch (e) {
                const text = await response.text();
                if (text) errorText = text;
            }
            throw new Error(errorText);
        }
        
        const data = await response.json();
        console.log('Сводный прайс загружен:', data);
        
        const summaryDiv = document.getElementById('pricesSummary');
        const table = document.getElementById('pricesTable');
        const tbody = document.getElementById('pricesTableBody');
        
        // Скрываем полную таблицу
        table.style.display = 'none';
        tbody.innerHTML = '';
        
        if (data.summary && data.summary.length > 0) {
            const hasSupplierInfo = data.summary.some(p => p.supplier_name);
            let html = `<h5>Сводный прайс (последние цены): ${data.summary.length} ${hasSupplierInfo ? 'позиций' : 'препаратов'}</h5>`;
            html += '<table style="width: 100%; border-collapse: collapse; margin-top: 10px;">';
            html += '<thead><tr style="background: #e0e0e0;">';
            html += '<th style="padding: 10px; border: 1px solid #ddd;">Препарат</th>';
            if (hasSupplierInfo) {
                html += '<th style="padding: 10px; border: 1px solid #ddd;">Поставщик</th>';
            }
            html += '<th style="padding: 10px; border: 1px solid #ddd;">МНН</th>';
            html += '<th style="padding: 10px; border: 1px solid #ddd;">Форма</th>';
            html += '<th style="padding: 10px; border: 1px solid #ddd;">Цена</th>';
            html += '<th style="padding: 10px; border: 1px solid #ddd;">Дата</th>';
            html += '<th style="padding: 10px; border: 1px solid #ddd;">Метод</th>';
            html += '</tr></thead><tbody>';
            
            data.summary.forEach(price => {
                const matchMethod = price.match_method || '-';
                const date = price.last_price_date ? new Date(price.last_price_date).toLocaleDateString('ru-RU') : '-';
                html += `<tr>
                    <td style="padding: 8px; border: 1px solid #ddd;">${this.escapeHtml(price.drug_name || '-')}</td>`;
                if (hasSupplierInfo) {
                    html += `<td style="padding: 8px; border: 1px solid #ddd;">${this.escapeHtml(price.supplier_name || '-')}</td>`;
                }
                html += `<td style="padding: 8px; border: 1px solid #ddd;">${this.escapeHtml(price.inn || '-')}</td>
                    <td style="padding: 8px; border: 1px solid #ddd;">${this.escapeHtml(price.cure_form || '-')}</td>
                    <td style="padding: 8px; border: 1px solid #ddd; text-align: right; font-weight: bold;">${price.price ? price.price.toFixed(2) : '-'}</td>
                    <td style="padding: 8px; border: 1px solid #ddd;">${date}</td>
                    <td style="padding: 8px; border: 1px solid #ddd;">${matchMethod}</td>
                </tr>`;
            });
            
            html += '</tbody></table>';
            summaryDiv.innerHTML = html;
            summaryDiv.style.display = 'block';
            
            const message = supplierID 
                ? `Загружен сводный прайс поставщика: ${data.summary.length} позиций`
                : `Загружен сводный прайс всех поставщиков: ${data.summary.length} позиций`;
            this.showSuccess(message);
        } else {
            summaryDiv.innerHTML = '<div style="color: #dc3545;">Прайсы не найдены. Сначала запустите сопоставление.</div>';
            summaryDiv.style.display = 'block';
        }
    } catch (error) {
        console.error('Ошибка загрузки сводного прайса:', error);
        let errorMessage = error.message;
        
        if (errorMessage === 'Failed to fetch') {
            errorMessage = 'Не удалось подключиться к серверу. Проверьте, что сервер запущен и доступен по адресу ' + this.apiUrl;
        }
        
        this.showError(`Ошибка загрузки сводного прайса: ${errorMessage}`);
    } finally {
        this.showLoading(false);
    }
};

// escapeHtml экранирует HTML символы для безопасного отображения
ESAPIClient.prototype.escapeHtml = function(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
};
