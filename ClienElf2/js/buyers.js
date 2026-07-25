let buyersDT = null;

document.addEventListener('DOMContentLoaded', async () => {
    await loadBuyers();
});

async function loadBuyers(opts = {}) {
    const tbody = document.getElementById('buyersTable');
    if (!tbody) return;

    const silent = !!opts.silent;
    if (!silent) {
        tbody.innerHTML = '<tr><td colspan="6" class="loading">Загрузка данных...</td></tr>';
    }

    try {
        const data = await API.get('/api/buyers');
        if (data && data.length > 0) {
            tbody.innerHTML = data.map(buyer => `
                <tr data-id="${buyer.buyer_id}">
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
                        <button class="btn-icon btn-danger-icon" onclick="deleteBuyer('${buyer.buyer_id}', this)" title="Удалить">🗑️</button>
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
        if (!silent) {
            tbody.innerHTML = '<tr><td colspan="6" class="empty-row">Ошибка загрузки данных</td></tr>';
        }
    }
}

async function toggleBuyerActive(buyerId, isActive) {
    console.log('Toggle buyer active:', buyerId, isActive);
}

async function deleteBuyer(buyerId, btn) {
    if (!confirm('Вы уверены, что хотите удалить этого покупателя?')) {
        return;
    }

    const row = btn ? btn.closest('tr') : document.querySelector(`tr[data-id="${buyerId}"]`);
    if (row) {
        row.style.opacity = '0.35';
        row.style.pointerEvents = 'none';
    }

    try {
        const result = await API.delete(`/api/buyers/${buyerId}`);
        if (!result) throw new Error('Ошибка удаления');
        Toast.success('Удалено', 'Покупатель удалён');
        if (buyersDT) {
            ElfTable.destroy(buyersDT);
            buyersDT = null;
        }
        if (row) row.remove();
        const tbody = document.getElementById('buyersTable');
        if (tbody && !tbody.querySelector('tr[data-id]')) {
            tbody.innerHTML = '<tr><td colspan="6" class="empty-row">Нет данных</td></tr>';
        } else if (tbody && tbody.querySelector('tr[data-id]')) {
            buyersDT = ElfTable.init('tblBuyers', { sortColumn: 1, sortDir: 'asc', unsortable: [0, 4, 5], exportName: 'Покупатели' });
            TableFilters.setup('tblBuyers');
        }
    } catch (error) {
        console.error('Ошибка удаления:', error);
        if (row) {
            row.style.opacity = '';
            row.style.pointerEvents = '';
        }
        Toast.error('Ошибка удаления', error.message);
    }
}

function escapeHtml(text) {
    if (text == null) return '';
    const div = document.createElement('div');
    div.textContent = String(text);
    return div.innerHTML;
}
