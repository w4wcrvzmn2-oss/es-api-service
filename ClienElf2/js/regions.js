let regionsData = [];
let regionEditModal = null;
let regionsDT = null;

document.addEventListener('DOMContentLoaded', async () => {
    regionEditModal = new bootstrap.Modal(document.getElementById('regionEditModal'));
    document.getElementById('btnAddRegion')?.addEventListener('click', openCreateRegion);
    document.getElementById('btnSaveRegion')?.addEventListener('click', saveRegion);
    await loadRegions();
});

async function loadRegions() {
    const tbody = document.getElementById('regionsTable');
    if (!tbody) return;
    tbody.innerHTML = '<tr><td colspan="6" class="loading">Загрузка данных...</td></tr>';

    try {
        const data = await API.get('/api/region?columns=RegionID,Code,Name,Capital,FederalDistrict,IsActive');
        const rows = API.unwrapList(data);
        if (rows.length > 0) {
            regionsData = rows;
            tbody.innerHTML = rows.map(region => {
                const id = region.RegionID || region.region_id || '';
                const code = region.Code || region.code || '-';
                const name = region.Name || region.name || '-';
                const capital = region.Capital || region.capital || '-';
                const district = region.FederalDistrict || region.federal_district || '-';
                const active = region.IsActive === true || region.IsActive === 1 || region.is_active === true;
                return `<tr data-id="${escapeHtml(id)}">
                    <td>${escapeHtml(code)}</td>
                    <td><strong>${escapeHtml(name)}</strong></td>
                    <td>${escapeHtml(capital)}</td>
                    <td>${escapeHtml(district)}</td>
                    <td class="text-center">${active ? '<span class="badge bg-success">да</span>' : '<span class="badge bg-secondary">нет</span>'}</td>
                    <td class="text-nowrap">
                        <button class="btn btn-outline-primary btn-sm" onclick="openEditRegion('${id}')" title="Редактировать"><i class="bi bi-pencil"></i></button>
                    </td>
                </tr>`;
            }).join('');
        } else {
            regionsData = [];
            tbody.innerHTML = '<tr><td colspan="6" class="empty-row">Нет данных. Нажмите «Добавить».</td></tr>';
        }
        if (regionsDT) ElfTable.destroy(regionsDT);
        regionsDT = ElfTable.init('tblRegions', { sortColumn: 1, sortDir: 'asc', unsortable: [4, 5], exportName: 'Регионы' });
        TableFilters.setup('tblRegions');
    } catch (error) {
        console.error('Ошибка загрузки регионов:', error);
        tbody.innerHTML = '<tr><td colspan="6" class="empty-row">Ошибка загрузки данных</td></tr>';
    }
}

function openCreateRegion() {
    document.getElementById('regionEditTitle').textContent = 'Новый регион';
    document.getElementById('editRegionId').value = '';
    document.getElementById('editCode').value = '';
    document.getElementById('editName').value = '';
    document.getElementById('editCapital').value = '';
    document.getElementById('editDistrict').value = '';
    document.getElementById('editActive').checked = true;
    regionEditModal.show();
    setTimeout(() => document.getElementById('editCode')?.focus(), 200);
}

function openEditRegion(id) {
    const region = regionsData.find(r => (r.RegionID || r.region_id) === id);
    if (!region) {
        Toast.error('Ошибка', 'Регион не найден');
        return;
    }
    document.getElementById('regionEditTitle').textContent = 'Редактирование региона';
    document.getElementById('editRegionId').value = id;
    document.getElementById('editCode').value = region.Code || region.code || '';
    document.getElementById('editName').value = region.Name || region.name || '';
    document.getElementById('editCapital').value = region.Capital || region.capital || '';
    document.getElementById('editDistrict').value = region.FederalDistrict || region.federal_district || '';
    document.getElementById('editActive').checked = region.IsActive === true || region.IsActive === 1 || region.is_active === true;
    regionEditModal.show();
}

async function saveRegion() {
    const id = document.getElementById('editRegionId').value.trim();
    const code = document.getElementById('editCode').value.trim();
    const name = document.getElementById('editName').value.trim();
    const capital = document.getElementById('editCapital').value.trim();
    const district = document.getElementById('editDistrict').value.trim();
    const isActive = document.getElementById('editActive').checked;

    if (!code || !name) {
        Toast.error('Проверка', 'Код и название обязательны');
        return;
    }

    const body = {
        code,
        name,
        capital: capital || '-',
        federal_district: district || '-',
        is_active: isActive
    };

    const btn = document.getElementById('btnSaveRegion');
    btn.disabled = true;
    try {
        let result;
        if (id) {
            result = await API.put(`/api/region/${id}`, body);
        } else {
            result = await API.post('/api/region/create', body);
        }
        if (!result) throw new Error('Сервер не вернул ответ');
        regionEditModal.hide();
        Toast.success(id ? 'Сохранено' : 'Создано', id ? 'Регион обновлён' : 'Регион добавлен');
        if (regionsDT) {
            ElfTable.destroy(regionsDT);
            regionsDT = null;
        }
        await loadRegions();
    } catch (error) {
        console.error(error);
        Toast.error('Ошибка', error.message || 'Не удалось сохранить регион');
    } finally {
        btn.disabled = false;
    }
}

function escapeHtml(text) {
    if (text == null) return '';
    const div = document.createElement('div');
    div.textContent = String(text);
    return div.innerHTML;
}

window.openEditRegion = openEditRegion;
