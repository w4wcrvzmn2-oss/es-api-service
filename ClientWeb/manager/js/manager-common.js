function mgrEsc(t) {
    const d = document.createElement('div');
    d.textContent = t == null ? '' : String(t);
    return d.innerHTML;
}
function mgrMoney(v) {
    return (Number(v) || 0).toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}
function mgrNum(v) {
    return (Number(v) || 0).toLocaleString('ru-RU');
}
function mgrShortId(id) {
    if (!id) return '—';
    return String(id).slice(0, 8) + '…';
}
function mgrToast(message, type) {
    if (typeof Toast !== 'undefined') {
        const map = { error: 'error', success: 'success', warning: 'warning', info: 'info' };
        const fn = Toast[map[type] || 'info'];
        if (typeof fn === 'function') return fn(type === 'error' ? 'Ошибка' : '', message);
        return Toast.show({ type: map[type] || 'info', message });
    }
    alert(message);
}
async function mgrApi(path, options) {
    const res = await AuthManager.authenticatedFetch(path, options || {});
    if (!res.ok) {
        let msg = res.statusText;
        try {
            const j = await res.json();
            msg = j.error || j.message || msg;
        } catch (_) {}
        throw new Error(msg || 'Ошибка запроса');
    }
    const ct = res.headers.get('content-type') || '';
    if (ct.includes('application/json')) return res.json();
    return res;
}
