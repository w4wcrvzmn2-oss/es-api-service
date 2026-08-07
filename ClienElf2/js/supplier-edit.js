// Редактирование поставщика
let currentSupplierId = null;

document.addEventListener('DOMContentLoaded', async () => {
    const urlParams = new URLSearchParams(window.location.search);
    currentSupplierId = urlParams.get('id');
    
    await loadRegionsCheckboxes();
    
    if (currentSupplierId) {
        document.getElementById('pageTitle').textContent = 'Редактирование поставщика';
        await loadSupplier(currentSupplierId);
        await loadSupplierPriceLists(currentSupplierId);
    }
    
    document.getElementById('supplierForm').addEventListener('submit', handleSubmit);

    const toggleBtn = document.getElementById('togglePassword');
    const pwdField = document.getElementById('password');
    if (toggleBtn && pwdField) {
        toggleBtn.addEventListener('click', () => {
            const isPassword = pwdField.type === 'password';
            pwdField.type = isPassword ? 'text' : 'password';
            toggleBtn.querySelector('i').className = isPassword ? 'bi bi-eye-slash' : 'bi bi-eye';
        });
    }
});

async function loadSupplier(id) {
    try {
        const data = await API.get('/api/suppliers');
        if (data && data.length > 0) {
            const supplier = data.find(s => s.supplier_id === id);
            if (supplier) {
                document.getElementById('name').value = supplier.name || '';
                document.getElementById('code').value = supplier.code || '';
                document.getElementById('inn').value = supplier.inn || '';
                document.getElementById('contacts').value = supplier.contacts || '';
                document.getElementById('address').value = supplier.address || '';
                document.getElementById('contractNumber').value = supplier.contract_number || '';
                document.getElementById('login').value = supplier.login || '';
                document.getElementById('password').value = supplier.password || '';
                document.getElementById('password').placeholder = 'Не задан';
            }
        }

        const regionsResp = await API.get(`/api/suppliers/${id}/regions`);
        const regions = regionsResp?.regions || [];
        if (regions.length > 0) {
            const regionIds = new Set(regions.map(r => (r.region_id || '').toUpperCase()));
            document.querySelectorAll('#regionsCheckboxes input[type="checkbox"]').forEach(cb => {
                if (regionIds.has((cb.value || '').toUpperCase())) cb.checked = true;
            });
        }
        sortRegionCheckboxes();
    } catch (error) {
        console.error('Ошибка загрузки поставщика:', error);
    }
}

async function loadSupplierPriceLists(supplierId) {
    const tbody = document.getElementById('priceListsTable');
    if (!tbody) return;
    
    try {
        const response = await API.get(`/api/price-lists?supplier_id=${supplierId}`);
        // API возвращает { price_lists: [...], total: N }
        const priceLists = response?.price_lists || [];
        
        if (priceLists.length > 0) {
            tbody.innerHTML = priceLists.map(price => `
                <tr>
                    <td>${escapeHtml(price.name || '-')}</td>
                    <td>${escapeHtml(price.regions_count || 0)} регион(ов)</td>
                    <td>${escapeHtml(price.last_update_at ? new Date(price.last_update_at).toLocaleDateString('ru') : '-')}</td>
                    <td>${price.is_active ? 'Активен' : 'Неактивен'}</td>
                    <td>
                        <a href="price-edit.html?id=${price.price_list_id}" class="btn btn-secondary btn-sm">Редактировать</a>
                    </td>
                </tr>
            `).join('');
        } else {
            tbody.innerHTML = '<tr><td colspan="5" class="empty-row">Нет прайсов</td></tr>';
        }
    } catch (error) {
        tbody.innerHTML = '<tr><td colspan="5" class="empty-row">Ошибка загрузки</td></tr>';
    }
}

async function loadRegionsCheckboxes() {
    const container = document.getElementById('regionsCheckboxes');
    if (!container) return;
    
    try {
        const data = await API.get('/api/region?columns=RegionID,Name');
        const rows = API.unwrapList(data);
        if (rows.length > 0) {
            const sorted = rows.sort((a, b) => 
                (a.Name || a.name || '').localeCompare(b.Name || b.name || '', 'ru')
            );
            container.innerHTML = sorted.map(region => `
                <label class="checkbox-label">
                    <input type="checkbox" name="regions" value="${region.RegionID || region.region_id || ''}">
                    <span>${escapeHtml(region.Name || region.name || '-')}</span>
                </label>
            `).join('');
        } else {
            container.innerHTML = '<p>Нет регионов</p>';
        }
    } catch (error) {
        container.innerHTML = '<p>Ошибка загрузки регионов</p>';
    }
}

function sortRegionCheckboxes() {
    const container = document.getElementById('regionsCheckboxes');
    if (!container) return;
    const labels = Array.from(container.querySelectorAll('.checkbox-label'));
    if (labels.length === 0) return;

    labels.sort((a, b) => {
        const aChecked = a.querySelector('input').checked ? 0 : 1;
        const bChecked = b.querySelector('input').checked ? 0 : 1;
        if (aChecked !== bChecked) return aChecked - bChecked;
        return a.textContent.trim().localeCompare(b.textContent.trim(), 'ru');
    });

    labels.forEach(l => container.appendChild(l));
}

function selectAllRegions() {
    document.querySelectorAll('#regionsCheckboxes input[type="checkbox"]').forEach(cb => cb.checked = true);
    sortRegionCheckboxes();
}

function addPriceList() {
    if (currentSupplierId) {
        window.location.href = `price-edit.html?supplier_id=${currentSupplierId}`;
    } else {
        Toast.warning('Внимание', 'Сначала сохраните поставщика');
    }
}

async function handleSubmit(e) {
    e.preventDefault();
    
    const regionCheckboxes = document.querySelectorAll('#regionsCheckboxes input[type="checkbox"]:checked');
    const regionIds = Array.from(regionCheckboxes).map(cb => cb.value).filter(v => v);

    const code = (document.getElementById('code').value || '').trim();
    if (code && !/^\d+$/.test(code)) {
        Toast.error('Ошибка', 'Код поставщика должен содержать только цифры');
        return;
    }

    const formData = {
        name: document.getElementById('name').value,
        code: code || null,
        inn: document.getElementById('inn').value || null,
        contacts: document.getElementById('contacts').value || null,
        address: document.getElementById('address').value || null,
        contract_number: document.getElementById('contractNumber').value || null,
        login: document.getElementById('login').value || null,
        password: document.getElementById('password').value || null,
        is_active: true,
        region_ids: regionIds
    };
    
    try {
        let result;
        if (currentSupplierId) {
            result = await API.put(`/api/suppliers/${currentSupplierId}`, formData);
        } else {
            result = await API.post('/api/suppliers/create', formData);
        }

        if (result?.cleaned_regions?.length > 0) {
            Toast.regionCleanup(result.cleaned_regions);
            setTimeout(() => { window.location.href = 'suppliers.html'; }, 3000);
        } else {
            Toast.success('Сохранено', currentSupplierId ? 'Поставщик обновлён' : 'Поставщик создан');
            setTimeout(() => { window.location.href = 'suppliers.html'; }, 800);
        }
    } catch (error) {
        Toast.error('Ошибка сохранения', error.message);
    }
}
