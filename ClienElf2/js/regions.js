let regionsData = [];
let regionModalInstance = null;
let regionsDT = null;

document.addEventListener('DOMContentLoaded', async () => {
    regionModalInstance = new bootstrap.Modal(document.getElementById('regionModal'));
    await loadRegions();
});

async function loadRegions() {
    const tbody = document.getElementById('regionsTable');
    if (!tbody) return;
    tbody.innerHTML = '<tr><td colspan="4" class="loading">Загрузка данных...</td></tr>';

    try {
        const data = await API.get('/api/region?columns=RegionID,Code,Name');
        if (data && data.length > 0) {
            regionsData = data;
            tbody.innerHTML = data.map(region => {
                const id = region.RegionID || region.region_id || '-';
                const code = region.Code || region.code || '-';
                const name = region.Name || region.name || '-';
                const capital = region.Capital || '-';
                return `<tr>
                    <td>${escapeHtml(code)}</td>
                    <td>${escapeHtml(name)}</td>
                    <td>${escapeHtml(capital)}</td>
                    <td><button class="btn btn-outline-secondary btn-sm" onclick="viewRegion('${id}')"><i class="bi bi-eye"></i></button></td>
                </tr>`;
            }).join('');
        } else {
            tbody.innerHTML = '<tr><td colspan="4" class="empty-row">Нет данных</td></tr>';
        }
        if (regionsDT) ElfTable.destroy(regionsDT);
        regionsDT = ElfTable.init('tblRegions', { sortColumn: 1, sortDir: 'asc', unsortable: [3], exportName: 'Регионы' });
        TableFilters.setup('tblRegions');
    } catch (error) {
        console.error('Ошибка загрузки регионов:', error);
        tbody.innerHTML = '<tr><td colspan="4" class="empty-row">Ошибка загрузки данных</td></tr>';
    }
}

function viewRegion(id) {
    const region = regionsData.find(r => (r.RegionID || r.region_id) === id);
    if (!region) { Toast.error('Ошибка', 'Регион не найден'); return; }

    document.getElementById('modalCode').textContent = region.Code || '-';
    document.getElementById('modalName').textContent = region.Name || '-';
    document.getElementById('modalCapital').textContent = region.Capital || '-';
    document.getElementById('modalDistrict').textContent = region.FederalDistrict || '-';
    document.getElementById('modalStatus').textContent = region.IsActive ? 'Активен' : 'Неактивен';
    regionModalInstance.show();
}
