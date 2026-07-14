// Загрузка навигации на всех страницах
(function() {
    'use strict';
    
    // Определяем текущую страницу по имени файла
    function getCurrentPage() {
        const path = window.location.pathname;
        const filename = path.split('/').pop() || 'index.html';
        const pageName = filename.replace('.html', '');
        // Если это index или пустая строка, возвращаем 'index'
        return pageName || 'index';
    }
    
    // Загружает навигацию из файла
    async function loadNavigation() {
        try {
            const response = await fetch('navigation.html');
            if (!response.ok) {
                throw new Error('Не удалось загрузить навигацию');
            }
            const html = await response.text();
            
            // Создаем временный контейнер для парсинга HTML
            const tempDiv = document.createElement('div');
            tempDiv.innerHTML = html;
            
            // Находим контейнер для навигации (dashboard)
            const dashboard = document.querySelector('.dashboard');
            if (!dashboard) {
                console.error('Контейнер .dashboard не найден');
                return;
            }
            
            // Проверяем, не загружена ли уже навигация
            const existingTopNav = dashboard.querySelector('.top-nav');
            const existingSidebar = dashboard.querySelector('.sidebar');
            if (existingTopNav || existingSidebar) {
                console.warn('Навигация уже загружена, пропускаем повторную загрузку');
                return;
            }
            
            // Получаем элементы навигации (клонируем для получения живых DOM элементов)
            const topNavTemplate = tempDiv.querySelector('.top-nav');
            const sidebarTemplate = tempDiv.querySelector('.sidebar');
            
            if (!topNavTemplate || !sidebarTemplate) {
                throw new Error('Элементы навигации не найдены в navigation.html');
            }
            
            // Клонируем элементы для получения живых DOM узлов
            const topNav = topNavTemplate.cloneNode(true);
            const sidebar = sidebarTemplate.cloneNode(true);
            
            // Вставляем навигацию в начало dashboard
            dashboard.insertBefore(topNav, dashboard.firstChild);
            
            // Вставляем sidebar после top-nav
            topNav.parentNode.insertBefore(sidebar, topNav.nextSibling);
            
            // Устанавливаем активный пункт меню
            const currentPage = getCurrentPage();
            const activeLinks = document.querySelectorAll(`[data-page="${currentPage}"]`);
            activeLinks.forEach(link => {
                link.classList.add('active');
            });
            
            console.log('Навигация загружена, текущая страница:', currentPage);
        } catch (error) {
            console.error('Ошибка загрузки навигации:', error);
            // Fallback: показываем сообщение об ошибке
            const dashboard = document.querySelector('.dashboard');
            if (dashboard) {
                dashboard.insertAdjacentHTML('afterbegin', 
                    '<div style="padding: 20px; background: #fee; color: #c33; border-radius: 8px; margin-bottom: 20px;">⚠️ Ошибка загрузки навигации: ' + error.message + '</div>'
                );
            }
        }
    }
    
    // Загружаем навигацию при готовности DOM
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', loadNavigation);
    } else {
        // DOM уже загружен, но даем немного времени для других скриптов
        setTimeout(loadNavigation, 10);
    }
    
    // Экспортируем функцию для ручного вызова, если нужно
    window.loadNavigation = loadNavigation;
})();

