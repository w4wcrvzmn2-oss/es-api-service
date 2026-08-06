(function() {
    const cur = location.pathname.split(/[/\\]/).pop() || 'index.html';
    const items = [
        { href: 'index.html',   icon: 'bi-house-door',     text: 'Дашборд',   file: 'index.html' },
        { href: 'orders.html',  icon: 'bi-cart3',          text: 'Заказы',    file: 'orders.html' },
        { href: 'imports.html', icon: 'bi-cloud-upload',   text: 'Импорты',   file: 'imports.html' },
        { href: 'drugs.html',   icon: 'bi-capsule',        text: 'Препараты', file: 'drugs.html' }
    ];
    const links = items.map(i =>
        `<li class="nav-item"><a class="nav-link${i.file === cur ? ' active' : ''}" href="${i.href}"><i class="bi ${i.icon}"></i> ${i.text}</a></li>`
    ).join('');

    document.body.insertAdjacentHTML('afterbegin',
    `<nav class="navbar navbar-expand navbar-light bg-white sticky-top border-bottom mgr-topnav">
        <div class="container-fluid px-3 py-1 flex-wrap gap-2">
            <a class="navbar-brand d-flex align-items-center gap-2 me-0" href="index.html">
                <span style="display:inline-flex;width:26px;height:26px;border-radius:8px;background:#2b6cab;position:relative"><span style="position:absolute;left:11px;top:5px;width:4px;height:16px;background:#fff;border-radius:1px"></span><span style="position:absolute;left:5px;top:11px;width:16px;height:4px;background:#fff;border-radius:1px"></span></span>
                <span class="fw-semibold">PharmData</span>
                <span class="badge rounded-pill ms-1" style="background:var(--elf-primary-soft);color:var(--elf-primary-dark);font-weight:600;font-size:11px">Менеджер</span>
            </a>
            <ul class="navbar-nav flex-row flex-wrap gap-1 me-auto">${links}</ul>
            <div class="d-flex align-items-center gap-2 ms-auto">
                <span class="navbar-text d-none d-md-inline" style="font-size:13px;color:var(--ef-muted)">Операционный кабинет</span>
                <button class="btn btn-outline-secondary btn-sm" onclick="AuthManager.logout()">
                    <i class="bi bi-box-arrow-right"></i> Выйти
                </button>
            </div>
        </div>
    </nav>`);
})();
