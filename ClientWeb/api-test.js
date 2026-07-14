// Скрипт для страницы проверки API
let currentData = null;

// Проверка здоровья сервиса
document.getElementById('healthCheckBtn').addEventListener('click', async () => {
    const statusDiv = document.getElementById('healthStatus');
    statusDiv.innerHTML = '<span class="info">Проверка...</span>';
    
    try {
        const result = await AuthManager.healthCheck(AuthManager.getApiUrl());
        if (result.success) {
            statusDiv.innerHTML = '<span class="success">✅ Сервис работает нормально</span>';
            showSuccess('Сервис работает нормально');
        } else {
            statusDiv.innerHTML = `<span class="error">❌ Проблемы с сервисом: ${result.error || 'Неизвестная ошибка'}</span>`;
            showError(`Проблемы с сервисом: ${result.error || 'Неизвестная ошибка'}`);
        }
    } catch (error) {
        statusDiv.innerHTML = `<span class="error">❌ Ошибка подключения: ${error.message}</span>`;
        showError(`Ошибка подключения: ${error.message}`);
    }
});

// Получение данных
document.getElementById('fetchDataBtn').addEventListener('click', fetchData);
document.getElementById('clearDataBtn').addEventListener('click', clearResults);
document.getElementById('exportJsonBtn').addEventListener('click', exportJson);

async function fetchData() {
    const table = document.getElementById('tableInput').value.trim();
    if (!table) {
        showError('Введите имя таблицы');
        return;
    }

    const limit = document.getElementById('limit').value;
    const offset = document.getElementById('offset').value;
    const updatedAfter = document.getElementById('updatedAfter').value;
    const idGt = document.getElementById('idGt').value;
    const columns = document.getElementById('columns').value;

    const params = new URLSearchParams();
    if (limit) params.append('limit', limit);
    if (offset) params.append('offset', offset);
    if (updatedAfter) {
        const date = new Date(updatedAfter);
        params.append('updated_after', date.toISOString());
    }
    if (idGt) params.append('id_gt', idGt);
    if (columns) params.append('columns', columns);

    const tableEndpoint = table.toLowerCase();
    const url = `/api/${tableEndpoint}?${params.toString()}`;

    try {
        showLoading(true);
        clearMessages();
        showRequestInfo(url, params);

        const response = await AuthManager.authenticatedFetch(url, {
            headers: { 'Accept-Encoding': 'gzip' }
        });

        if (!response.ok) {
            const errorData = await response.json();
            throw new Error(errorData.error || `HTTP ${response.status}: ${response.statusText}`);
        }

        const data = await response.json();
        displayData(data);
        showSuccess(`Получено ${Array.isArray(data) ? data.length : 0} записей`);
        
        document.getElementById('exportJsonBtn').style.display = 'inline-block';
        currentData = data;
    } catch (error) {
        showError(`Ошибка получения данных: ${error.message}`);
    } finally {
        showLoading(false);
    }
}

function showRequestInfo(url, params) {
    const requestInfo = document.getElementById('requestInfo');
    const apiUrl = AuthManager.getApiUrl();
    requestInfo.innerHTML = `
        <strong>Запрос:</strong> ${apiUrl}${url}<br>
        <strong>Параметры:</strong> ${params.toString() || 'Нет'}
    `;
}

function displayData(data) {
    const jsonData = document.getElementById('jsonData');
    jsonData.textContent = JSON.stringify(data, null, 2);
}

function clearResults() {
    document.getElementById('jsonData').textContent = '';
    document.getElementById('requestInfo').innerHTML = '';
    document.getElementById('exportJsonBtn').style.display = 'none';
    currentData = null;
    clearMessages();
}

function exportJson() {
    if (!currentData) {
        showError('Нет данных для экспорта');
        return;
    }

    const dataStr = JSON.stringify(currentData, null, 2);
    const dataBlob = new Blob([dataStr], { type: 'application/json' });
    const url = URL.createObjectURL(dataBlob);
    
    const link = document.createElement('a');
    link.href = url;
    link.download = `es_api_data_${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.json`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
    
    showSuccess('Данные экспортированы в JSON файл');
}

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

function showLoading(show) {
    document.getElementById('loadingIndicator').style.display = show ? 'block' : 'none';
}

// Функции showError и showSuccess теперь определены в toast.js

function clearMessages() {
    // Toast уведомления закрываются автоматически или через кнопку ×
    // Эта функция оставлена для совместимости, но не нужна
}

