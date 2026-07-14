// Управление страницей аудит-логов

let currentOffset = 0;
const pageSize = 50;
let currentFilters = {};

document.addEventListener('DOMContentLoaded', async () => {
    // Проверка авторизации
    if (!AuthManager.isAuthenticated()) {
        window.location.href = 'login.html';
        return;
    }
    
    // Обработчики событий
    const applyFiltersBtn = document.getElementById('applyFiltersBtn');
    const clearFiltersBtn = document.getElementById('clearFiltersBtn');
    const refreshBtn = document.getElementById('refreshBtn');
    
    applyFiltersBtn?.addEventListener('click', () => {
        if (applyFiltersBtn) showButtonLoading(applyFiltersBtn, 'Применение...');
        applyFilters();
        setTimeout(() => {
            if (applyFiltersBtn) hideButtonLoading(applyFiltersBtn);
        }, 500);
    });
    clearFiltersBtn?.addEventListener('click', () => {
        if (clearFiltersBtn) showButtonLoading(clearFiltersBtn, 'Очистка...');
        clearFilters();
        setTimeout(() => {
            if (clearFiltersBtn) hideButtonLoading(clearFiltersBtn);
        }, 500);
    });
    refreshBtn?.addEventListener('click', () => {
        if (refreshBtn) showButtonLoading(refreshBtn, 'Обновление...');
        loadLogs(currentOffset).finally(() => {
            if (refreshBtn) hideButtonLoading(refreshBtn);
        });
    });
    document.getElementById('prevPageBtn')?.addEventListener('click', () => {
        if (currentOffset > 0) {
            currentOffset = Math.max(0, currentOffset - pageSize);
            loadLogs(currentOffset);
        }
    });
    document.getElementById('nextPageBtn')?.addEventListener('click', () => {
        currentOffset += pageSize;
        loadLogs(currentOffset);
    });
    
    // Загружаем логи и статистику
    await loadStats();
    await loadLogs(0);
});

function getFilters() {
    const userID = document.getElementById('filterUserID')?.value.trim() || '';
    const logLevel = document.getElementById('filterLogLevel')?.value || '';
    const category = document.getElementById('filterCategory')?.value.trim() || '';
    const action = document.getElementById('filterAction')?.value.trim() || '';
    const startDate = document.getElementById('filterStartDate')?.value || '';
    const endDate = document.getElementById('filterEndDate')?.value || '';
    
    const filters = {};
    if (userID) filters.user_id = userID;
    if (logLevel) filters.log_level = logLevel;
    if (category) filters.category = category;
    if (action) filters.action = action;
    if (startDate) {
        // Преобразуем datetime-local в формат для SQL Server (YYYY-MM-DDTHH:mm:ss)
        const date = new Date(startDate);
        filters.start_date = date.toISOString().slice(0, 19);
    }
    if (endDate) {
        const date = new Date(endDate);
        filters.end_date = date.toISOString().slice(0, 19);
    }
    
    console.log('Фильтры:', filters);
    return filters;
}

function applyFilters() {
    currentFilters = getFilters();
    currentOffset = 0;
    loadLogs(0);
}

function clearFilters() {
    document.getElementById('filterUserID').value = '';
    document.getElementById('filterLogLevel').value = '';
    document.getElementById('filterCategory').value = '';
    document.getElementById('filterAction').value = '';
    document.getElementById('filterStartDate').value = '';
    document.getElementById('filterEndDate').value = '';
    currentFilters = {};
    currentOffset = 0;
    loadLogs(0);
}

async function loadLogs(offset = 0) {
    const tbody = document.getElementById('logsTableBody');
    if (!tbody) return;
    
    showElementLoading(tbody, 'Загрузка логов...');
    
    try {
        const params = new URLSearchParams({
            limit: pageSize.toString(),
            offset: offset.toString(),
            ...Object.fromEntries(
                Object.entries(currentFilters).filter(([_, v]) => v !== undefined && v !== '')
            )
        });
        
        const url = `/api/audit-logs?${params}`;
        console.log('Запрос логов:', url);
        
        const response = await AuthManager.authenticatedFetch(url);
        
        console.log('Ответ сервера:', response.status, response.statusText);
        
        if (!response.ok) {
            let errorText = '';
            try {
                const errorData = await response.json();
                errorText = errorData.error || errorData.message || '';
                console.error('Ошибка API:', errorData);
            } catch (e) {
                errorText = await response.text();
                console.error('Текст ошибки:', errorText);
            }
            throw new Error(`Ошибка загрузки логов: ${response.status} ${response.statusText}${errorText ? ' - ' + errorText : ''}`);
        }
        
        const data = await response.json();
        console.log('Данные логов:', data);
        
        if (data.logs && data.logs.length > 0) {
            displayLogs(data.logs);
            updatePagination(data.total || data.totalCount || 0, data.offset || offset, data.limit || pageSize, data.has_more || false);
        } else {
            console.log('Логи не найдены. Данные:', data);
            tbody.innerHTML = '<tr><td colspan="11" style="padding: 20px; text-align: center; color: #666;">Логи не найдены</td></tr>';
            updatePagination(0, offset, pageSize, false);
        }
    } catch (error) {
        console.error('Ошибка загрузки логов:', error);
        showError(`Ошибка загрузки логов: ${error.message}`);
        tbody.innerHTML = '<tr><td colspan="11" style="padding: 20px; text-align: center; color: #dc3545;">Ошибка загрузки</td></tr>';
    } finally {
        hideElementLoading(tbody);
    }
}

function displayLogs(logs) {
    const tbody = document.getElementById('logsTableBody');
    if (!tbody) return;
    
    tbody.innerHTML = '';
    
    logs.forEach(log => {
        const row = document.createElement('tr');
        row.style.borderBottom = '1px solid #ddd';
        
        const levelClass = getLevelClass(log.log_level);
        const date = new Date(log.created_at).toLocaleString('ru-RU');
        
        row.innerHTML = `
            <td style="padding: 8px; border: 1px solid #ddd; font-size: 0.9em;">${escapeHtml(date)}</td>
            <td style="padding: 8px; border: 1px solid #ddd;">
                <span class="log-level-badge ${levelClass}" style="padding: 4px 8px; border-radius: 4px; font-size: 0.85em; font-weight: bold;">
                    ${escapeHtml(log.log_level)}
                </span>
            </td>
            <td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(log.username || log.user_id || '-')}</td>
            <td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(log.category || '-')}</td>
            <td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(log.action || '-')}</td>
            <td style="padding: 8px; border: 1px solid #ddd; max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;" title="${escapeHtml(log.message)}">${escapeHtml(log.message)}</td>
            <td style="padding: 8px; border: 1px solid #ddd; font-size: 0.85em;">${escapeHtml(log.ip_address || '-')}</td>
            <td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(log.request_method || '-')}</td>
            <td style="padding: 8px; border: 1px solid #ddd; font-size: 0.85em; max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;" title="${escapeHtml(log.request_path || '')}">${escapeHtml(log.request_path || '-')}</td>
            <td style="padding: 8px; border: 1px solid #ddd;">
                ${log.response_status ? `<span class="status-badge status-${getStatusClass(log.response_status)}">${log.response_status}</span>` : '-'}
            </td>
            <td style="padding: 8px; border: 1px solid #ddd;">${log.execution_time_ms || '-'}${log.execution_time_ms ? ' мс' : ''}</td>
        `;
        
        // Добавляем обработчик клика для показа деталей
        row.addEventListener('click', () => showLogDetails(log));
        row.style.cursor = 'pointer';
        
        tbody.appendChild(row);
    });
}

function getLevelClass(level) {
    switch (level) {
        case 'ERROR': return 'log-level-error';
        case 'WARN': return 'log-level-warn';
        case 'INFO': return 'log-level-info';
        case 'DEBUG': return 'log-level-debug';
        default: return '';
    }
}

function getStatusClass(status) {
    if (status >= 200 && status < 300) return 'success';
    if (status >= 300 && status < 400) return 'redirect';
    if (status >= 400 && status < 500) return 'client-error';
    if (status >= 500) return 'server-error';
    return 'unknown';
}

function updatePagination(total, offset, limit, hasMore) {
    const info = document.getElementById('logsInfo');
    const prevBtn = document.getElementById('prevPageBtn');
    const nextBtn = document.getElementById('nextPageBtn');
    
    if (info) {
        info.textContent = `Показано ${offset + 1}-${Math.min(offset + limit, total)} из ${total}`;
    }
    
    if (prevBtn) {
        prevBtn.disabled = offset === 0;
    }
    
    if (nextBtn) {
        nextBtn.disabled = !hasMore;
    }
}

async function loadStats() {
    const statsSection = document.getElementById('statsSection');
    const statsContent = document.getElementById('statsContent');
    
    try {
        console.log('Загрузка статистики...');
        if (statsContent) showElementLoading(statsContent, 'Загрузка статистики...');
        const response = await AuthManager.authenticatedFetch('/api/audit-logs/stats');
        
        console.log('Статистика: статус ответа', response.status, response.statusText);
        
        if (response.ok) {
            const data = await response.json();
            console.log('Данные статистики:', data);
            displayStats(data);
        } else {
            console.error('Ошибка загрузки статистики:', response.status, response.statusText);
            try {
                const errorData = await response.json();
                console.error('Детали ошибки:', errorData);
            } catch (e) {
                console.error('Не удалось получить детали ошибки');
            }
        }
    } catch (error) {
        console.error('Исключение при загрузке статистики:', error);
    } finally {
        if (statsContent) hideElementLoading(statsContent);
    }
}

function displayStats(stats) {
    const statsSection = document.getElementById('statsSection');
    const statsContent = document.getElementById('statsContent');
    
    if (!statsSection || !statsContent) return;
    
    let html = '<div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 15px;">';
    
    // Статистика по уровням
    if (stats.by_level && Object.keys(stats.by_level).length > 0) {
        html += '<div><strong>По уровням:</strong><ul style="margin: 10px 0; padding-left: 20px;">';
        for (const [level, count] of Object.entries(stats.by_level)) {
            html += `<li>${escapeHtml(level)}: <strong>${count}</strong></li>`;
        }
        html += '</ul></div>';
    } else {
        html += '<div><strong>По уровням:</strong><p style="color: #666; margin: 10px 0;">Нет данных</p></div>';
    }
    
    // Статистика по пользователям
    if (stats.by_user && stats.by_user.length > 0) {
        html += '<div><strong>По пользователям:</strong><ul style="margin: 10px 0; padding-left: 20px;">';
        stats.by_user.forEach(user => {
            html += `<li>${escapeHtml(user.username)}: <strong>${user.count}</strong></li>`;
        });
        html += '</ul></div>';
    } else {
        html += '<div><strong>По пользователям:</strong><p style="color: #666; margin: 10px 0;">Нет данных</p></div>';
    }
    
    html += '</div>';
    statsContent.innerHTML = html;
    statsSection.style.display = 'block';
}

function showLogDetails(log) {
    let details = `Дата/Время: ${new Date(log.created_at).toLocaleString('ru-RU')}\n`;
    details += `Уровень: ${log.log_level}\n`;
    details += `Пользователь: ${log.username || log.user_id || 'Система'}\n`;
    details += `Категория: ${log.category || '-'}\n`;
    details += `Действие: ${log.action || '-'}\n`;
    details += `Сообщение: ${log.message}\n`;
    if (log.ip_address) details += `IP адрес: ${log.ip_address}\n`;
    if (log.user_agent) details += `User-Agent: ${log.user_agent}\n`;
    if (log.request_method) details += `Метод: ${log.request_method}\n`;
    if (log.request_path) details += `Путь: ${log.request_path}\n`;
    if (log.response_status) details += `Статус: ${log.response_status}\n`;
    if (log.execution_time_ms) details += `Время выполнения: ${log.execution_time_ms} мс\n`;
    if (log.error_message) details += `Ошибка: ${log.error_message}\n`;
    if (log.details) {
        try {
            const detailsObj = JSON.parse(log.details);
            details += `Детали:\n${JSON.stringify(detailsObj, null, 2)}`;
        } catch {
            details += `Детали: ${log.details}`;
        }
    }
    
    alert(details);
}

function escapeHtml(text) {
    if (text == null) return '';
    const div = document.createElement('div');
    div.textContent = String(text);
    return div.innerHTML;
}

