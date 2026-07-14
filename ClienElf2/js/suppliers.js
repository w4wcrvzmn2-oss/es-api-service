let suppliersDT = null;

document.addEventListener('DOMContentLoaded', async () => {
    await loadSuppliers();
});

async function loadSuppliers() {
    const tbody = document.getElementById('suppliersTable');
    if (!tbody) return;
    
    tbody.innerHTML = '<tr><td colspan="7" class="loading">Загрузка данных...</td></tr>';
    
    try {
        const data = await API.get('/api/suppliers');
        if (data && data.length > 0) {
            tbody.innerHTML = data.map(supplier => `
                <tr>
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
                        <button class="btn-icon btn-danger-icon" onclick="deleteSupplier('${supplier.supplier_id}')" title="Удалить">🗑️</button>
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
        tbody.innerHTML = '<tr><td colspan="7" class="empty-row">Ошибка загрузки данных</td></tr>';
    }
}

async function toggleSupplierActive(supplierId, isActive) {
    console.log('Toggle supplier active:', supplierId, isActive);
    // TODO: API для изменения статуса
}

async function deleteSupplier(supplierId) {
    if (!confirm('Вы уверены, что хотите удалить этого поставщика?')) {
        return;
    }
    
    try {
        const result = await API.delete(`/api/suppliers/${supplierId}`);
        if (result) {
            Toast.success('Удалено', 'Поставщик удалён');
            await loadSuppliers();
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
