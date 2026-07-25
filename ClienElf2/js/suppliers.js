let suppliersDT = null;

document.addEventListener('DOMContentLoaded', async () => {
    await loadSuppliers();
});

async function loadSuppliers(opts = {}) {
    const tbody = document.getElementById('suppliersTable');
    if (!tbody) return;

    const silent = !!opts.silent;
    if (!silent) {
        tbody.innerHTML = '<tr><td colspan="7" class="loading">Загрузка данных...</td></tr>';
    }

    try {
        const data = await API.get('/api/suppliers');
        if (data && data.length > 0) {
            tbody.innerHTML = data.map(supplier => `
                <tr data-id="${supplier.supplier_id}">
                    <td style="text-align: center;">
                        <input type="checkbox" ${supplier.is_active ? 'checked' : ''} 
                               onchange="toggleSupplierActive('${supplier.supplier_id}', this.checked)"
                               title="Активность поставщика">
                    </td>
                    <td><strong>${escapeHtml(supplier.name || '-')}</strong></td>
                    <td>${escapeHtml(supplier.inn || '-')}</td>
                    <td>${escapeHtml(supplier.address || '-')}</td>
                    <td>${escapeHtml(supplier.contacts || '-')}</td>
                    <td style="text-align: center;">
                        <a href="supplier-edit.html?id=${supplier.supplier_id}" class="btn-icon" title="Редактировать">✏️</a>
                    </td>
                    <td style="text-align: center;">
                        <button class="btn-icon btn-danger-icon" onclick="deleteSupplier('${supplier.supplier_id}', this)" title="Удалить">🗑️</button>
                    </td>
                </tr>
            `).join('');
        } else {
            tbody.innerHTML = '<tr><td colspan="7" class="empty-row">Нет данных</td></tr>';
        }
        if (suppliersDT) ElfTable.destroy(suppliersDT);
        suppliersDT = ElfTable.init('tblSuppliers', { sortColumn: 1, sortDir: 'asc', unsortable: [0, 5, 6], exportName: 'Поставщики' });
        TableFilters.setup('tblSuppliers');
    } catch (error) {
        console.error('Ошибка загрузки поставщиков:', error);
        if (!silent) {
            tbody.innerHTML = '<tr><td colspan="7" class="empty-row">Ошибка загрузки данных</td></tr>';
        }
    }
}

async function toggleSupplierActive(supplierId, isActive) {
    console.log('Toggle supplier active:', supplierId, isActive);
}

async function deleteSupplier(supplierId, btn) {
    if (!confirm('Вы уверены, что хотите удалить этого поставщика?')) {
        return;
    }

    const row = btn ? btn.closest('tr') : document.querySelector(`tr[data-id="${supplierId}"]`);
    if (row) {
        row.style.opacity = '0.35';
        row.style.pointerEvents = 'none';
    }

    try {
        const result = await API.delete(`/api/suppliers/${supplierId}`);
        if (!result) throw new Error('Ошибка удаления');
        Toast.success('Удалено', 'Поставщик удалён');
        if (suppliersDT) {
            ElfTable.destroy(suppliersDT);
            suppliersDT = null;
        }
        if (row) row.remove();
        const tbody = document.getElementById('suppliersTable');
        if (tbody && !tbody.querySelector('tr[data-id]')) {
            tbody.innerHTML = '<tr><td colspan="7" class="empty-row">Нет данных</td></tr>';
        } else if (tbody && tbody.querySelector('tr[data-id]')) {
            suppliersDT = ElfTable.init('tblSuppliers', { sortColumn: 1, sortDir: 'asc', unsortable: [0, 5, 6], exportName: 'Поставщики' });
            TableFilters.setup('tblSuppliers');
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
