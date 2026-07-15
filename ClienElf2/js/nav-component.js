(function() {
    const inPages = location.pathname.replace(/\\/g,'/').includes('/pages/');
    const r = inPages ? '../' : '';
    const p = inPages ? '' : 'pages/';
    const cur = location.pathname.split(/[/\\]/).pop() || 'index.html';

    const items = [
        { href: r + 'index.html',         icon: 'bi-house-door',   text: '\u0413\u043b\u0430\u0432\u043d\u0430\u044f',       file: 'index.html' },
        { href: p + 'regions.html',        icon: 'bi-geo-alt',      text: '\u0420\u0435\u0433\u0438\u043e\u043d\u044b',       file: 'regions.html' },
        { href: p + 'access-points.html',  icon: 'bi-plug',         text: '\u0422\u043e\u0447\u043a\u0438 \u0434\u043e\u0441\u0442\u0443\u043f\u0430', file: 'access-points.html' },
        { href: p + 'suppliers.html',      icon: 'bi-building',     text: '\u041f\u043e\u0441\u0442\u0430\u0432\u0449\u0438\u043a\u0438',   file: 'suppliers.html' },
        { href: p + 'price-lists.html',    icon: 'bi-list-check',   text: '\u041f\u0440\u0430\u0439\u0441\u044b',       file: 'price-lists.html' },
        { href: p + 'orders.html',         icon: 'bi-cart3',        text: '\u0417\u0430\u043a\u0430\u0437\u044b',       file: 'orders.html' },
        { href: p + 'links.html',          icon: 'bi-link-45deg',   text: '\u0421\u0432\u044f\u0437\u043a\u0438',       file: 'links.html' },
        { href: p + 'buyers.html',         icon: 'bi-people',       text: '\u041f\u043e\u043a\u0443\u043f\u0430\u0442\u0435\u043b\u0438',   file: 'buyers.html' }
    ];

    const parentMap = {
        'supplier-edit.html': 'suppliers.html',
        'buyer-edit.html': 'buyers.html',
        'price-edit.html': 'price-lists.html',
        'price-view.html': 'price-lists.html',
        'access-point-edit.html': 'access-points.html'
    };
    const activeFile = parentMap[cur] || cur;

    const links = items.map(i =>
        `<li class="nav-item"><a class="nav-link${i.file===activeFile?' active':''}" href="${i.href}"><i class="bi ${i.icon}"></i> ${i.text}</a></li>`
    ).join('');

    document.body.insertAdjacentHTML('afterbegin',
    `<nav class="navbar navbar-expand-lg navbar-light bg-white">
        <div class="container-fluid px-3">
            <a class="navbar-brand d-flex align-items-center gap-2" href="${r}index.html">
                <span style="font-size:1.3rem">\ud83d\udc8a</span><span class="fw-semibold">ЭльФиСА</span>
            </a>
            <button class="navbar-toggler" type="button" data-bs-toggle="collapse" data-bs-target="#mainNav">
                <span class="navbar-toggler-icon"></span>
            </button>
            <div class="collapse navbar-collapse" id="mainNav">
                <ul class="navbar-nav me-auto mb-2 mb-lg-0">${links}</ul>
                <button class="btn btn-outline-secondary btn-sm" onclick="AuthManager.logout()">
                    <i class="bi bi-box-arrow-right"></i> \u0412\u044b\u0439\u0442\u0438
                </button>
            </div>
        </div>
    </nav>`);
})();
