let allPriceLists = [];
let priceListsDT = null;

document.addEventListener('DOMContentLoaded', async () => {
    await loadPriceLists();
});


async function loadPriceLists() {
    const tbody = document.getElementById('priceListsTable');
    if (!tbody) return;

    tbody.innerHTML = '<tr><td colspan="8" class="text-center text-muted py-3">Загрузка данных...</td></tr>';

    try {
        const response = await API.get('/api/price-lists');
        allPriceLists = response?.price_lists || [];

        if (allPriceLists.length > 0) {
            tbody.innerHTML = allPriceLists.map(price => {
                const lastUpdate = price.last_update_at ? new Date(price.last_update_at).toLocaleDateString('ru') : '-';
                const unlinkedCount = price.unmatched_count ?? price.prices_count ?? 0;
                const cnt = price.prices_count ?? 0;
                const linksUrl = `links.html?supplier=${price.supplier_id}`;

                return `
                <tr>
                    <td class="text-center">
                        <input type="checkbox" ${price.is_active ? 'checked' : ''}
                               onchange="togglePriceActive('${price.price_list_id}', this.checked)"
                               title="Активность прайса">
                    </td>
                    <td><strong>${escapeHtml(price.name || '-')}</strong></td>
                    <td>${escapeHtml(price.supplier_name || '-')}</td>
                    <td data-sort="${price.last_update_at || ''}">${lastUpdate}</td>
                    <td class="text-center">${cnt > 0 ? `<a href="price-view.html?id=${price.price_list_id}" class="fw-semibold text-decoration-none" title="Просмотр позиций">${cnt.toLocaleString('ru-RU')}</a>` : '-'}</td>
                    <td class="text-center text-nowrap">
                        <a href="price-view.html?id=${price.price_list_id}" class="btn btn-outline-primary btn-sm me-1" title="Просмотр позиций"><i class="bi bi-eye"></i></a>
                        <a href="price-edit.html?id=${price.price_list_id}" class="btn btn-outline-secondary btn-sm" title="Редактировать"><i class="bi bi-pencil"></i></a>
                    </td>
                    <td class="text-center">
                        <button class="btn btn-outline-danger btn-sm" onclick="deletePriceList('${price.price_list_id}')" title="Удалить"><i class="bi bi-trash"></i></button>
                    </td>
                    <td class="text-center">
                        ${unlinkedCount > 0
                            ? `<button class="btn btn-outline-danger btn-sm" onclick="goLinks('${price.supplier_id}')" title="Перейти к связкам">${unlinkedCount} <i class="bi bi-link-45deg"></i></button>`
                            : '<span class="badge bg-success">0</span>'}
                    </td>
                </tr>
                `;
            }).join('');
        } else {
            tbody.innerHTML = '<tr><td colspan="8" class="text-center text-muted py-3">Нет данных</td></tr>';
        }
        if (priceListsDT) ElfTable.destroy(priceListsDT);
        priceListsDT = ElfTable.init('tblPriceLists', { sortColumn: 2, sortDir: 'asc', unsortable: [0, 5, 6, 7], exportName: 'Прайсы' });
        TableFilters.setup('tblPriceLists');
    } catch (error) {
        console.error('Ошибка загрузки прайсов:', error);
        tbody.innerHTML = '<tr><td colspan="8" class="text-center text-danger py-3">Ошибка загрузки данных</td></tr>';
    }
}

async function togglePriceActive(priceId, isActive) {
    try {
        console.log('Toggle active:', priceId, isActive);
    } catch (error) {
        console.error('Ошибка изменения статуса:', error);
        Toast.error('Ошибка', 'Не удалось изменить статус');
        await loadPriceLists();
    }
}

async function deletePriceList(priceId) {
    if (!confirm('Вы уверены, что хотите удалить этот прайс и все его позиции?')) return;

    try {
        const result = await API.delete(`/api/price-lists/${priceId}`);
        if (result) {
            Toast.success('Удалено', 'Прайс удалён');
            await loadPriceLists();
        } else {
            throw new Error('Ошибка удаления');
        }
    } catch (error) {
        console.error('Ошибка удаления прайса:', error);
        Toast.error('Ошибка удаления', error.message);
    }
}

function goLinks(supplierId) {
    window.location.href = 'links.html?supplier=' + supplierId;
}

function escapeHtml(text) {
    if (text == null) return '';
    const div = document.createElement('div');
    div.textContent = String(text);
    return div.innerHTML;
}
