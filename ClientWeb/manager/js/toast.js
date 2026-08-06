const Toast = (() => {
    let container = null;

    function getContainer() {
        if (!container) {
            container = document.createElement('div');
            container.className = 'elf-toast-container';
            document.body.appendChild(container);
        }
        return container;
    }

    const ICONS = { success: '\u2705', error: '\u274c', warning: '\u26a0\ufe0f', info: '\u2139\ufe0f' };

    function show({ type = 'info', title = '', message = '', html = '', duration = 5000 } = {}) {
        const el = document.createElement('div');
        el.className = `elf-toast elf-toast-${type}`;

        let body = '';
        if (title) body += `<div class="elf-toast-title">${esc(title)}</div>`;
        if (html)  body += `<div class="elf-toast-message">${html}</div>`;
        else if (message) body += `<div class="elf-toast-message">${esc(message)}</div>`;

        el.innerHTML =
            `<span class="elf-toast-icon">${ICONS[type]||ICONS.info}</span>` +
            `<div class="elf-toast-body">${body}</div>` +
            `<button class="elf-toast-close">&times;</button>` +
            (duration ? `<div class="elf-toast-progress" style="animation-duration:${duration}ms"></div>` : '');

        el.querySelector('.elf-toast-close').onclick = () => dismiss(el);
        getContainer().appendChild(el);
        if (duration) setTimeout(() => dismiss(el), duration);
        return el;
    }

    function dismiss(el) {
        if (!el || el.classList.contains('elf-toast-out')) return;
        el.classList.add('elf-toast-out');
        el.addEventListener('animationend', () => el.remove());
    }

    function esc(t) { const d = document.createElement('div'); d.textContent = t; return d.innerHTML; }

    function success(title, message, o) { return show({ type:'success', title, message, ...o }); }
    function error(title, message, o)   { return show({ type:'error',   title, message, ...o }); }
    function warning(title, message, o) { return show({ type:'warning', title, message, ...o }); }
    function info(title, message, o)    { return show({ type:'info',    title, message, ...o }); }

    function regionCleanup(cleanedRegions) {
        const grouped = {};
        cleanedRegions.forEach(i => { if (!grouped[i.price_name]) grouped[i.price_name] = []; grouped[i.price_name].push(i.region_name); });
        let h = '';
        for (const [price, regions] of Object.entries(grouped)) {
            h += `<div class="elf-toast-price-group">${esc(price)}</div><ul class="elf-toast-detail-list">`;
            regions.forEach(r => { h += `<li>${esc(r)}</li>`; });
            h += '</ul>';
        }
        return show({ type:'warning', title:'\u0420\u0435\u0433\u0438\u043e\u043d\u044b \u0443\u0434\u0430\u043b\u0435\u043d\u044b \u0438\u0437 \u043f\u0440\u0430\u0439\u0441\u043e\u0432', html:h, duration:10000 });
    }

    function confirm(title, message) {
        return new Promise(resolve => {
            const html = `<div>${esc(message)}</div>
                <div class="d-flex gap-2 mt-2">
                    <button class="btn btn-success btn-sm elf-toast-yes">Да</button>
                    <button class="btn btn-outline-secondary btn-sm elf-toast-no">Нет</button>
                </div>`;
            const el = show({ type: 'warning', title, html, duration: 0 });
            el.querySelector('.elf-toast-yes').onclick = () => { dismiss(el); resolve(true); };
            el.querySelector('.elf-toast-no').onclick = () => { dismiss(el); resolve(false); };
            el.querySelector('.elf-toast-close').onclick = () => { dismiss(el); resolve(false); };
        });
    }

    return { show, success, error, warning, info, confirm, regionCleanup };
})();
