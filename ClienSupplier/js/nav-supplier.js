(function() {
    const inPages = location.pathname.replace(/\\/g,'/').includes('/pages/');
    const r = inPages ? '../' : '';
    const p = inPages ? '' : 'pages/';
    const cur = location.pathname.split(/[/\\]/).pop() || 'index.html';

    const items = [
        { href: r + 'index.html',              icon: 'bi-house-door',   text: 'Главная',            file: 'index.html' },
        { text: 'Прайс-листы', icon: 'bi-list-check', dropdown: [
            { href: p + 'price-lists.html',     icon: 'bi-list-check',   text: 'Прайс-листы',       file: 'price-lists.html' },
            { href: p + 'price-upload-log.html', icon: 'bi-clock-history',text: 'Статистика загрузок',file: 'price-upload-log.html' }
        ]},
        { href: p + 'regions.html',            icon: 'bi-geo-alt',      text: 'Регионы',            file: 'regions.html' },
        { href: p + 'clients.html',            icon: 'bi-people',       text: 'Клиенты',            file: 'clients.html' },
        { text: 'Заказы', icon: 'bi-cart3', dropdown: [
            { href: p + 'orders.html',          icon: 'bi-cart3',        text: 'Статистика заказов', file: 'orders.html' },
            { href: p + 'order-delivery.html',   icon: 'bi-truck',       text: 'Доставка заказов',  file: 'order-delivery.html' },
            { href: p + 'order-service.html',    icon: 'bi-gear',        text: 'Обслуживание',      file: 'order-service.html' }
        ]},
        { href: p + 'profile.html',            icon: 'bi-person',       text: 'Профиль',            file: 'profile.html' },
        { href: p + 'change-password.html',     icon: 'bi-key',          text: 'Сменить пароль',     file: 'change-password.html' }
    ];

    const parentMap = {};
    items.forEach(i => {
        if (i.dropdown) i.dropdown.forEach(d => parentMap[d.file] = i.dropdown[0].file);
    });
    const activeFile = parentMap[cur] || cur;

    let links = '';
    items.forEach(i => {
        if (i.dropdown) {
            const isActive = i.dropdown.some(d => d.file === activeFile);
            const ddItems = i.dropdown.map(d =>
                `<li><a class="dropdown-item${d.file===activeFile?' active':''}" href="${d.href}"><i class="bi ${d.icon}"></i> ${d.text}</a></li>`
            ).join('');
            links += `<li class="nav-item dropdown">
                <a class="nav-link dropdown-toggle${isActive?' active':''}" href="#" role="button" data-bs-toggle="dropdown">
                    <i class="bi ${i.icon}"></i> ${i.text}
                </a>
                <ul class="dropdown-menu">${ddItems}</ul>
            </li>`;
        } else {
            links += `<li class="nav-item"><a class="nav-link${i.file===activeFile?' active':''}" href="${i.href}"><i class="bi ${i.icon}"></i> ${i.text}</a></li>`;
        }
    });

    let userInfo = {};
    try {
        userInfo = JSON.parse(localStorage.getItem('userInfo') || '{}');
    } catch (_) {
        localStorage.removeItem('userInfo');
    }
    const supplierName = userInfo.username || 'Поставщик';
    const safeSupplierName = escapeHtml(supplierName);

    document.body.insertAdjacentHTML('afterbegin',
    `<nav class="navbar navbar-expand-lg navbar-light supplier-navbar bg-white">
        <div class="container-fluid px-3">
            <a class="navbar-brand d-flex align-items-center gap-2" href="${r}index.html">
                <i class="bi bi-building" style="font-size:1.3rem;color:var(--accent)"></i><span class="fw-semibold">Кабинет поставщика</span>
            </a>
            <button class="navbar-toggler" type="button" data-bs-toggle="collapse" data-bs-target="#mainNav">
                <span class="navbar-toggler-icon"></span>
            </button>
            <div class="collapse navbar-collapse" id="mainNav">
                <ul class="navbar-nav me-auto mb-2 mb-lg-0">${links}</ul>
                <span class="navbar-text me-3"><i class="bi bi-person-circle"></i> ${safeSupplierName}</span>
                <button class="btn btn-outline-secondary btn-sm" onclick="AuthManager.logout()">
                    <i class="bi bi-box-arrow-right"></i> Выйти
                </button>
            </div>
        </div>
    </nav>`);

    function escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = String(text == null ? '' : text);
        return div.innerHTML;
    }
})();
