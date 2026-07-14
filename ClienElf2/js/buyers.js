let buyersDT = null;

document.addEventListener('DOMContentLoaded', async () => {
    await loadBuyers();
});

async function loadBuyers() {
    const tbody = document.getElementById('buyersTable');
    if (!tbody) return;
    
    tbody.innerHTML = '<tr><td colspan="6" class="loading">Загрузка данных...</td></tr>';
    
    try {
        const data = await API.get('/api/buyers');
        if (data && data.length > 0) {
            tbody.innerHTML = data.map(buyer => `
                <tr>
                    <td style="text-align: center;">
                        <input type="checkbox" ${buyer.is_active ? 'checked' : ''} 
                               onchange="toggleBuyerActive('${buyer.buyer_id}', this.checked)"
                               title="Активность покупателя">
                    </td>
                    <td><strong>${escapeHtml(buyer.name || '-')}</strong></td>
                    <td>${escapeHtml(buyer.inn || '-')}</td>
                    <td>${escapeHtml(buyer.region_name || '-')}</td>
                    <td style="text-align: center;">
                        <a href="buyer-edit.html?id=${buyer.buyer_id}" class="btn-icon" title="Редактировать">✏️</a>
                    </td>
                    <td style="text-align: center;">
                        <button class="btn-icon btn-danger-icon" onclick="deleteBuyer('${buyer.buyer_id}')" title="Удалить">🗑️</button>
                    </td>
                </tr>
            `).join('');
        } else {
            tbody.innerHTML = '<tr><td colspan="6" class="empty-row">Нет данных</td></tr>';
        }
        if (buyersDT) ElfTable.destroy(buyersDT);
        buyersDT = ElfTable.init('tblBuyers', { sortColumn: 1, sortDir: 'asc', unsortable: [0, 4, 5], exportName: 'Покупатели' });
        TableFilters.setup('tblBuyers');
    } catch (error) {
        console.error('Ошибка загрузки покупателей:', error);
        tbody.innerHTML = '<tr><td colspan="6" class="empty-row">Ошибка загрузки данных</td></tr>';
    }
}

async function toggleBuyerActive(buyerId, isActive) {
    console.log('Toggle buyer active:', buyerId, isActive);
    // TODO: API для изменения статуса
}

async function deleteBuyer(buyerId) {
    if (!confirm('Вы уверены, что хотите удалить этого покупателя?')) {
        return;
    }
    
    try {
        const result = await API.delete(`/api/buyers/${buyerId}`);
        if (result) {
            Toast.success('Удалено', 'Покупатель удалён');
            await loadBuyers();
        } else {
            throw new Error('Ошибка удаления');
        }
    } catch (error) {
        console.error('Ошибка удаления:', error);
        Toast.error('Ошибка удаления', error.message);
    }
}

function escapeHtml(text) {
    if (text == null) return '';
    const div = document.createElement('div');
    div.textContent = String(text);
    return div.innerHTML;
}
