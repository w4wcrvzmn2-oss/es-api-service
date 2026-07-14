// Универсальный компонент индикатора загрузки

// Создаем глобальный overlay для загрузки
let loadingOverlay = null;

function initLoadingOverlay() {
    if (loadingOverlay) return;
    
    loadingOverlay = document.createElement('div');
    loadingOverlay.id = 'globalLoadingOverlay';
    loadingOverlay.innerHTML = `
        <div class="loading-overlay-content">
            <div class="loading-spinner-large"></div>
            <div class="loading-text">Загрузка...</div>
        </div>
    `;
    document.body.appendChild(loadingOverlay);
}

// Показать глобальный индикатор загрузки
function showLoading(message = 'Загрузка...') {
    if (!loadingOverlay) {
        initLoadingOverlay();
    }
    
    const textElement = loadingOverlay.querySelector('.loading-text');
    if (textElement) {
        textElement.textContent = message;
    }
    
    loadingOverlay.classList.add('active');
    document.body.style.overflow = 'hidden'; // Блокируем прокрутку
}

// Скрыть глобальный индикатор загрузки
function hideLoading() {
    if (loadingOverlay) {
        loadingOverlay.classList.remove('active');
        document.body.style.overflow = ''; // Восстанавливаем прокрутку
    }
}

// Показать индикатор загрузки для конкретного элемента
function showElementLoading(element, message = 'Загрузка...') {
    if (!element) return;
    
    // Сохраняем исходное содержимое
    if (!element.dataset.originalContent) {
        element.dataset.originalContent = element.innerHTML;
    }
    
    // Создаем индикатор загрузки
    const loadingHTML = `
        <div class="element-loading">
            <div class="loading-spinner-small"></div>
            <span class="loading-text-small">${escapeHtml(message)}</span>
        </div>
    `;
    
    element.innerHTML = loadingHTML;
    element.classList.add('loading-state');
    element.disabled = true;
}

// Скрыть индикатор загрузки для конкретного элемента
function hideElementLoading(element) {
    if (!element) return;
    
    // Восстанавливаем исходное содержимое
    if (element.dataset.originalContent) {
        element.innerHTML = element.dataset.originalContent;
        delete element.dataset.originalContent;
    }
    
    element.classList.remove('loading-state');
    element.disabled = false;
}

// Показать индикатор загрузки для кнопки
function showButtonLoading(button, message = 'Загрузка...') {
    if (!button) return;
    
    // Сохраняем исходное содержимое
    if (!button.dataset.originalContent) {
        button.dataset.originalContent = button.innerHTML;
    }
    
    // Создаем индикатор загрузки для кнопки
    const loadingHTML = `
        <span class="button-loading-spinner"></span>
        <span>${escapeHtml(message)}</span>
    `;
    
    button.innerHTML = loadingHTML;
    button.disabled = true;
    button.classList.add('loading');
}

// Скрыть индикатор загрузки для кнопки
function hideButtonLoading(button) {
    if (!button) return;
    
    // Восстанавливаем исходное содержимое
    if (button.dataset.originalContent) {
        button.innerHTML = button.dataset.originalContent;
        delete button.dataset.originalContent;
    }
    
    button.disabled = false;
    button.classList.remove('loading');
}

// Обертка для async функций с автоматическим показом загрузки
async function withLoading(asyncFn, options = {}) {
    const {
        global = true,
        element = null,
        button = null,
        message = 'Загрузка...',
        errorMessage = 'Произошла ошибка'
    } = options;
    
    try {
        if (global) {
            showLoading(message);
        } else if (button) {
            showButtonLoading(button, message);
        } else if (element) {
            showElementLoading(element, message);
        }
        
        const result = await asyncFn();
        return result;
    } catch (error) {
        console.error('Ошибка в withLoading:', error);
        if (typeof showError === 'function') {
            showError(errorMessage + ': ' + (error.message || error));
        }
        throw error;
    } finally {
        if (global) {
            hideLoading();
        } else if (button) {
            hideButtonLoading(button);
        } else if (element) {
            hideElementLoading(element);
        }
    }
}

// Вспомогательная функция для экранирования HTML
function escapeHtml(text) {
    if (text == null) return '';
    const div = document.createElement('div');
    div.textContent = String(text);
    return div.innerHTML;
}

// Инициализация при загрузке DOM
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initLoadingOverlay);
} else {
    initLoadingOverlay();
}

// Экспортируем функции для глобального использования
window.showLoading = showLoading;
window.hideLoading = hideLoading;
window.showElementLoading = showElementLoading;
window.hideElementLoading = hideElementLoading;
window.showButtonLoading = showButtonLoading;
window.hideButtonLoading = hideButtonLoading;
window.withLoading = withLoading;

