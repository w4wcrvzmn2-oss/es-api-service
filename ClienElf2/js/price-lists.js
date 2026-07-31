let allPriceLists = [];
let priceListsDT = null;

document.addEventListener('DOMContentLoaded', async () => {
    await loadPriceLists();
});


async function loadPriceLists(opts = {}) {
    const tbody = document.getElementById('priceListsTable');
    if (!tbody) return;

    const silent = !!opts.silent;
    if (!silent) {
        tbody.innerHTML = '<tr><td colspan="8" class="text-center text-muted py-3">Загрузка данных...</td></tr>';
    }

    try {
        const response = await API.get('/api/price-lists');
        allPriceLists = response?.price_lists || [];

        if (allPriceLists.length > 0) {
            tbody.innerHTML = allPriceLists.map(price => {
                const lastUpdate = formatDateTime(price.last_update_at);
                const unlinkedCount = price.unmatched_count ?? price.prices_count ?? 0;
                const cnt = price.prices_count ?? 0;

                return `
                <tr data-id="${price.price_list_id}">
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
                        <button class="btn btn-outline-success btn-sm me-1" onclick="forceFetchPriceList('${price.price_list_id}', this)" title="Обновить сейчас"><i class="bi bi-arrow-repeat"></i></button>
                        <button class="btn btn-outline-info btn-sm" onclick="showPriceListBuyers('${price.price_list_id}')" title="Подключённые клиенты"><i class="bi bi-people"></i></button>
                    </td>
                    <td class="text-center">
                        <button class="btn btn-outline-danger btn-sm" onclick="deletePriceList('${price.price_list_id}', this)" title="Удалить"><i class="bi bi-trash"></i></button>
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
        if (!silent) {
            tbody.innerHTML = '<tr><td colspan="8" class="text-center text-danger py-3">Ошибка загрузки данных</td></tr>';
        }
    }
}

function formatDateTime(value) {
    if (!value) return '-';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return escapeHtml(String(value));
    return date.toLocaleString('ru-RU', {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit'
    });
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

async function deletePriceList(priceId, btn) {
    if (!confirm('Вы уверены, что хотите удалить этот прайс и все его позиции?')) return;

    const row = btn ? btn.closest('tr') : document.querySelector(`tr[data-id="${priceId}"]`);
    if (row) {
        row.style.opacity = '0.35';
        row.style.pointerEvents = 'none';
    }

    try {
        const result = await API.delete(`/api/price-lists/${priceId}`);
        if (!result) throw new Error('Ошибка удаления');
        Toast.success('Удалено', 'Прайс удалён');
        allPriceLists = allPriceLists.filter(p => p.price_list_id !== priceId);
        if (priceListsDT) {
            ElfTable.destroy(priceListsDT);
            priceListsDT = null;
        }
        if (row) row.remove();
        const tbody = document.getElementById('priceListsTable');
        if (tbody && !tbody.querySelector('tr[data-id]')) {
            tbody.innerHTML = '<tr><td colspan="8" class="text-center text-muted py-3">Нет данных</td></tr>';
        } else if (tbody && tbody.querySelector('tr[data-id]')) {
            priceListsDT = ElfTable.init('tblPriceLists', { sortColumn: 2, sortDir: 'asc', unsortable: [0, 5, 6, 7], exportName: 'Прайсы' });
            TableFilters.setup('tblPriceLists');
        }
    } catch (error) {
        console.error('Ошибка удаления прайса:', error);
        if (row) {
            row.style.opacity = '';
            row.style.pointerEvents = '';
        }
        Toast.error('Ошибка удаления', error.message);
    }
}

async function forceFetchPriceList(priceId, btn) {
    const originalHtml = btn ? btn.innerHTML : '';
    if (btn) {
        btn.disabled = true;
        btn.innerHTML = '<span class="spinner-border spinner-border-sm"></span>';
    }
    try {
        const result = await API.post(`/api/price-lists/${priceId}/fetch`, {});
        if (!result) throw new Error('Не удалось запустить обновление прайса');
        Toast.success('Обновление запущено', result.message || 'Ручной забор прайса стартовал');
        setTimeout(() => { loadPriceLists({ silent: true }); }, 1500);
    } catch (error) {
        console.error('Ошибка ручного обновления прайса:', error);
        Toast.error('Ошибка', error.message || 'Не удалось запустить обновление прайса');
    } finally {
        if (btn) {
            btn.disabled = false;
            btn.innerHTML = originalHtml;
        }
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
