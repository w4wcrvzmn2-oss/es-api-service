// Редактирование покупателя
let currentBuyerId = null;

document.addEventListener('DOMContentLoaded', async () => {
    const urlParams = new URLSearchParams(window.location.search);
    currentBuyerId = urlParams.get('id');
    
    await loadRegions();
    
    if (currentBuyerId) {
        document.getElementById('pageTitle').textContent = 'Редактирование покупателя';
        document.getElementById('deleteBtn').style.display = 'inline-block';
        await loadBuyer(currentBuyerId);
    }
    
    document.getElementById('buyerForm').addEventListener('submit', handleSubmit);
});

async function loadBuyer(id) {
    try {
        const data = await API.get(`/api/buyers/${id}`);
        if (data) {
            document.getElementById('name').value = data.name || '';
            document.getElementById('inn').value = data.inn || '';
            document.getElementById('code').value = data.code || '';
            document.getElementById('phone').value = data.phone || '';
            document.getElementById('address').value = data.address || '';
            document.getElementById('email').value = data.email || '';
            document.getElementById('isActive').checked = data.is_active !== false;
            
            if (data.region_id) {
                document.getElementById('regionSelect').value = data.region_id.toLowerCase();
            }
        }
    } catch (error) {
        console.error('Ошибка загрузки покупателя:', error);
    }
}

async function loadRegions() {
    const select = document.getElementById('regionSelect');
    if (!select) return;
    
    try {
        const data = await API.get('/api/region');
        if (data && data.length > 0) {
            select.innerHTML = '<option value="">Выберите регион</option>' +
                data.map(region => `<option value="${(region.RegionID || region.region_id || '').toLowerCase()}">${escapeHtml(region.Name || region.name || '-')}</option>`).join('');
        } else {
            select.innerHTML = '<option value="">Нет регионов</option>';
        }
    } catch (error) {
        select.innerHTML = '<option value="">Ошибка загрузки</option>';
    }
}

function selectAllRegions() {
    // Для покупателя выбирается только один регион
    Toast.info('Внимание', 'Выбирается только один регион для покупателя');
}

async function handleSubmit(e) {
    e.preventDefault();
    
    const regionId = document.getElementById('regionSelect').value;
    
    const formData = {
        name: document.getElementById('name').value,
        inn: document.getElementById('inn').value || null,
        region_id: regionId || null,
        code: document.getElementById('code').value || null,
        phone: document.getElementById('phone').value || null,
        address: document.getElementById('address').value || null,
        email: document.getElementById('email').value || null,
        is_active: document.getElementById('isActive').checked
    };
    
    try {
        if (currentBuyerId) {
            await API.put(`/api/buyers/${currentBuyerId}`, formData);
        } else {
            await API.post('/api/buyers', formData);
        }
        
        window.location.href = 'buyers.html';
    } catch (error) {
        Toast.error('Ошибка сохранения', error.message);
    }
}

async function deleteBuyer() {
    if (!confirm('Вы уверены, что хотите удалить этого покупателя?')) {
        return;
    }
    
    try {
        await API.delete(`/api/buyers/${currentBuyerId}`);
        window.location.href = 'buyers.html';
    } catch (error) {
        Toast.error('Ошибка удаления', error.message);
    }
}
