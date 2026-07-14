// Общий модуль для всплывающих уведомлений (toast)
// Функция для экранирования HTML
function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Функция для создания всплывающего уведомления (toast)
function showToast(message, type = 'info', duration = 5000) {
    // Ищем или создаем контейнер для уведомлений
    let container = document.getElementById('toastContainer');
    
    if (!container) {
        // Создаем контейнер если его нет
        container = document.createElement('div');
        container.id = 'toastContainer';
        container.style.cssText = 'position: fixed; top: 20px; right: 20px; z-index: 10000; display: flex; flex-direction: column; gap: 10px; max-width: 400px; pointer-events: none;';
        document.body.appendChild(container);
    }
    
    // Создаем элемент уведомления
    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    toast.style.cssText = 'pointer-events: auto;';
    
    // Иконки для разных типов уведомлений
    const icons = {
        error: '❌',
        success: '✅',
        info: 'ℹ️',
        warning: '⚠️'
    };
    
    // Обрабатываем многострочные сообщения
    const formattedMessage = escapeHtml(message).replace(/\n/g, '<br>');
    
    toast.innerHTML = `
        <div class="toast-content">
            <span class="toast-icon">${icons[type] || icons.info}</span>
            <span style="white-space: pre-line; word-wrap: break-word;">${formattedMessage}</span>
        </div>
        <button class="toast-close" onclick="this.parentElement.remove()" aria-label="Закрыть">×</button>
    `;
    
    // Добавляем в контейнер
    container.appendChild(toast);
    
    let timeout;
    let remainingTime = duration;
    let startTime = Date.now();
    
    // Функция для запуска таймера
    const startTimer = () => {
        startTime = Date.now();
        timeout = setTimeout(() => {
            removeToast(toast);
        }, remainingTime);
    };
    
    // Запускаем таймер
    startTimer();
    
    // Останавливаем таймер при наведении
    toast.addEventListener('mouseenter', () => {
        if (timeout) {
            clearTimeout(timeout);
            const elapsed = Date.now() - startTime;
            remainingTime = Math.max(0, remainingTime - elapsed);
        }
    });
    
    // Возобновляем таймер при уходе мыши
    toast.addEventListener('mouseleave', () => {
        if (remainingTime > 0) {
            startTimer();
        }
    });
}

function removeToast(toast) {
    if (!toast || !toast.parentElement) return;
    
    toast.classList.add('hiding');
    setTimeout(() => {
        if (toast.parentElement) {
            toast.parentElement.removeChild(toast);
        }
    }, 300); // Время анимации
}

// Обертки для совместимости со старым кодом
function showError(message) {
    showToast(message, 'error', 5000);
}

function showSuccess(message) {
    showToast(message, 'success', 3000);
}

function showInfo(message) {
    showToast(message, 'info', 4000);
}

function showWarning(message) {
    showToast(message, 'warning', 4000);
}

