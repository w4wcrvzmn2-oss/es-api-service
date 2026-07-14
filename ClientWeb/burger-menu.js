// Универсальный скрипт для бургер-меню
(function() {
    'use strict';
    
    function toggleSidebar() {
        console.log('toggleSidebar вызвана');
        const sidebar = document.getElementById('sidebar');
        const overlay = document.getElementById('sidebarOverlay');
        const burger = document.getElementById('burgerMenu');
        
        console.log('Элементы:', { sidebar: !!sidebar, overlay: !!overlay, burger: !!burger });
        
        if (sidebar && overlay && burger) {
            sidebar.classList.toggle('active');
            overlay.classList.toggle('active');
            burger.classList.toggle('active');
            console.log('Классы применены:', {
                sidebar: sidebar.classList.contains('active'),
                overlay: overlay.classList.contains('active'),
                burger: burger.classList.contains('active')
            });
        } else {
            console.error('Не найдены элементы:', { sidebar, overlay, burger });
        }
    }
    
    function closeSidebar() {
        const sidebar = document.getElementById('sidebar');
        const overlay = document.getElementById('sidebarOverlay');
        const burger = document.getElementById('burgerMenu');
        
        if (sidebar && overlay && burger) {
            sidebar.classList.remove('active');
            overlay.classList.remove('active');
            burger.classList.remove('active');
        }
    }
    
    // Экспорт функций для глобального использования сразу
    window.toggleSidebar = toggleSidebar;
    window.closeSidebar = closeSidebar;
    
    // Инициализация после загрузки DOM
    function initBurgerMenu() {
        // Добавляем обработчики событий для всех кнопок бургер-меню
        const burgerButtons = document.querySelectorAll('#burgerMenu, .burger-menu');
        burgerButtons.forEach(btn => {
            // Удаляем атрибут onclick, если он есть
            btn.removeAttribute('onclick');
            // Удаляем старые обработчики и добавляем новый
            const newBtn = btn.cloneNode(true);
            btn.parentNode.replaceChild(newBtn, btn);
            newBtn.addEventListener('click', function(e) {
                e.preventDefault();
                e.stopPropagation();
                console.log('Клик по бургер-меню');
                toggleSidebar();
            });
        });
        
        // Добавляем обработчики для overlay
        const overlays = document.querySelectorAll('#sidebarOverlay, .sidebar-overlay');
        overlays.forEach(overlay => {
            overlay.removeAttribute('onclick');
            const newOverlay = overlay.cloneNode(true);
            overlay.parentNode.replaceChild(newOverlay, overlay);
            newOverlay.addEventListener('click', function(e) {
                e.preventDefault();
                e.stopPropagation();
                console.log('Клик по overlay');
                closeSidebar();
            });
        });
        
        console.log('Бургер-меню инициализировано:', {
            buttons: burgerButtons.length,
            overlays: overlays.length,
            windowWidth: window.innerWidth
        });
    }
    
    // Инициализация при загрузке DOM
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', initBurgerMenu);
    } else {
        // DOM уже загружен
        initBurgerMenu();
    }
    
    // Закрытие меню при изменении размера окна (если перешли на десктоп)
    window.addEventListener('resize', () => {
        if (window.innerWidth > 768) {
            closeSidebar();
        }
    });
    
    // Закрытие меню при нажатии Escape
    document.addEventListener('keydown', (e) => {
        if (e.key === 'Escape') {
            closeSidebar();
        }
    });
})();

