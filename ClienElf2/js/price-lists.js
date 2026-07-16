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
                        <a href="price-edit.html?id=${price.price_list_id}" class="btn btn-outline-secondary btn-sm me-1" title="Редактировать"><i class="bi bi-pencil"></i></a>
                        <button class="btn btn-outline-info btn-sm" onclick="showPriceListBuyers('${price.price_list_id}')" title="Подключённые клиенты"><i class="bi bi-people"></i></button>
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

function ensureBuyersModal() {
    let el = document.getElementById('plBuyersModal');
    if (el) return el;
    document.body.insertAdjacentHTML('beforeend', `
    <div class="modal fade" id="plBuyersModal" tabindex="-1" aria-hidden="true">
      <div class="modal-dialog modal-dialog-scrollable">
        <div class="modal-content">
          <div class="modal-header">
            <h5 class="modal-title"><i class="bi bi-people"></i> Клиенты прайса</h5>
            <button type="button" class="btn-close" data-bs-dismiss="modal" aria-label="Закрыть"></button>
          </div>
          <div class="modal-body" id="plBuyersBody"><div class="text-muted">Загрузка...</div></div>
          <div class="modal-footer"><button type="button" class="btn btn-secondary" data-bs-dismiss="modal">Закрыть</button></div>
        </div>
      </div>
    </div>`);
    return document.getElementById('plBuyersModal');
}

async function showPriceListBuyers(priceListId) {
    const modalEl = ensureBuyersModal();
    const body = document.getElementById('plBuyersBody');
    const pl = allPriceLists.find(p => p.price_list_id === priceListId);
    modalEl.querySelector('.modal-title').innerHTML =
        `<i class="bi bi-people"></i> Клиенты прайса${pl ? ': ' + escapeHtml(pl.name || '') : ''}`;
    body.innerHTML = '<div class="text-muted">Загрузка...</div>';
    const modal = bootstrap.Modal.getOrCreateInstance(modalEl);
    modal.show();
    try {
        const buyers = await API.get(`/api/price-lists/${priceListId}/buyers`) || [];
        if (!Array.isArray(buyers) || buyers.length === 0) {
            body.innerHTML = '<div class="text-muted">К этому прайсу пока не подключён ни один клиент. Подключение задаётся в карточке покупателя.</div>';
            return;
        }
        body.innerHTML = `<div class="mb-2 text-muted small">Подключено клиентов: ${buyers.length}</div>
            <table class="table table-sm table-striped align-middle mb-0">
              <thead><tr><th>Клиент</th><th>ИНН</th><th>Регион</th></tr></thead>
              <tbody>${buyers.map(b => `<tr>
                <td>${escapeHtml(b.name || '-')}</td>
                <td>${escapeHtml(b.inn || '')}</td>
                <td>${escapeHtml(b.region_name || '')}</td>
              </tr>`).join('')}</tbody>
            </table>`;
    } catch (err) {
        console.error('Ошибка загрузки клиентов прайса:', err);
        body.innerHTML = '<div class="alert alert-danger mb-0">Не удалось загрузить клиентов</div>';
    }
}

function escapeHtml(text) {
    if (text == null) return '';
    const div = document.createElement('div');
    div.textContent = String(text);
    return div.innerHTML;
}
