document.addEventListener('DOMContentLoaded', function() {
    loadStatistics();
});

async function loadStatistics() {
    const matchedEl = document.getElementById('unlinkedProducts');
    const unmatchedEl = document.getElementById('unlinkedCount');

    if (matchedEl) matchedEl.innerHTML = '<span class="spinner-border spinner-border-sm"></span>';
    if (unmatchedEl) unmatchedEl.innerHTML = '<span class="spinner-border spinner-border-sm"></span>';

    try {
        const data = await API.get('/api/stats/global');
        if (matchedEl) matchedEl.textContent = (data.matched ?? 0).toLocaleString('ru-RU');
        if (unmatchedEl) unmatchedEl.textContent = (data.unmatched ?? 0).toLocaleString('ru-RU');
    } catch (error) {
        console.error('Ошибка загрузки статистики:', error);
        if (matchedEl) matchedEl.textContent = '-';
        if (unmatchedEl) unmatchedEl.textContent = '-';
    }
}
