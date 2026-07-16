let ordersDT = null;
let allOrders = [];
let _ordersLoadGen = 0;
const ORDERS_FIRST_BATCH = 200;

document.addEventListener('DOMContentLoaded', async () => {
    await loadOrders();
});

function renderOrderRows(orders) {
    return orders.map(order => {
        const orderDate = order.created_at ? new Date(order.created_at).toLocaleDateString('ru') : '-';
        const amount = order.total_amount ? parseFloat(order.total_amount).toFixed(2) : '0.00';
        return `
        <tr>
            <td style="text-align: center;">
                <input type="checkbox" checked disabled title="Активность заказа">
            </td>
            <td><strong>${escapeHtml(order.buyer_name || order.buyer_user_name || '-')}</strong></td>
            <td data-sort="${order.created_at || ''}">${orderDate}</td>
            <td style="text-align: right; font-weight: bold;" data-sort="${amount}">${amount}</td>
            <td>
                <span class="badge bg-secondary">${escapeHtml(order.order_status_name || 'Новый')}</span>
            </td>
            <td style="text-align: center;">
                <button class="btn btn-sm btn-outline-secondary" onclick="reexportOrder('${order.order_id}')" title="Выгрузить повторно">
                    <i class="bi bi-arrow-repeat"></i> Выгрузить
                </button>
            </td>
            <td style="text-align: center;">
                <button class="btn-icon" onclick="viewOrder('${order.order_id}')" title="Просмотр"><i class="bi bi-eye"></i></button>
            </td>
        </tr>`;
    }).join('');
}

function rebuildOrdersTable() {
    const tbody = document.getElementById('ordersTable');
    if (!tbody) return;
    if (allOrders.length === 0) {
        tbody.innerHTML = '<tr><td colspan="7" class="empty-row">Нет данных</td></tr>';
        if (ordersDT) { ElfTable.destroy(ordersDT); ordersDT = null; }
        return;
    }
    tbody.innerHTML = renderOrderRows(allOrders);
    if (ordersDT) ElfTable.destroy(ordersDT);
    ordersDT = ElfTable.init('tblOrders', { sortColumn: 2, sortDir: 'desc', unsortable: [0, 5, 6], exportName: 'Заказы' });
    TableFilters.setup('tblOrders');
}

async function loadOrders() {
    const gen = ++_ordersLoadGen;
    const tbody = document.getElementById('ordersTable');
    if (!tbody) return;
    tbody.innerHTML = '<tr><td colspan="7" class="loading">Загрузка данных...</td></tr>';

    try {
        const data = await API.get(`/api/orders?limit=${ORDERS_FIRST_BATCH}`);
        if (gen !== _ordersLoadGen) return;
        allOrders = Array.isArray(data) ? data : (data?.orders || []);
        rebuildOrdersTable();

        const total = data?.total_count || allOrders.length;
        if (allOrders.length < total) {
            loadOrdersRemaining(gen, allOrders.length, total);
        }
    } catch (error) {
        if (gen !== _ordersLoadGen) return;
        console.error('Ошибка загрузки заказов:', error);
        tbody.innerHTML = '<tr><td colspan="7" class="empty-row">Ошибка загрузки данных</td></tr>';
    }
}

async function loadOrdersRemaining(gen, loaded, total) {
    try {
        const data = await API.get(`/api/orders?limit=${total}&offset=${loaded}`);
        if (gen !== _ordersLoadGen) return;
        const more = Array.isArray(data) ? data : (data?.orders || []);
        if (more.length === 0) return;
        allOrders = allOrders.concat(more);
        rebuildOrdersTable();
    } catch (e) {
        if (gen !== _ordersLoadGen) return;
        console.error('Ошибка фоновой дозагрузки заказов:', e);
    }
}

function reexportOrder(orderId) {
    Toast.info('В разработке', 'Функция повторной выгрузки ещё не реализована');
}

function fmtMoney(v) {
    const n = parseFloat(v);
    if (!isFinite(n)) return '0.00';
    return n.toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

function ensureOrderModal() {
    let el = document.getElementById('orderViewModal');
    if (el) return el;
    document.body.insertAdjacentHTML('beforeend', `
    <div class="modal fade" id="orderViewModal" tabindex="-1" aria-hidden="true">
        <div class="modal-dialog modal-lg modal-dialog-scrollable">
            <div class="modal-content">
                <div class="modal-header">
                    <h5 class="modal-title"><i class="bi bi-receipt"></i> Заказ</h5>
                    <button type="button" class="btn-close" data-bs-dismiss="modal" aria-label="Закрыть"></button>
                </div>
                <div class="modal-body" id="orderViewBody">
                    <div class="text-center text-muted py-4">Загрузка данных...</div>
                </div>
                <div class="modal-footer">
                    <button type="button" class="btn btn-secondary" data-bs-dismiss="modal">Закрыть</button>
                </div>
            </div>
        </div>
    </div>`);
    return document.getElementById('orderViewModal');
}

function renderOrderView(order) {
    const items = Array.isArray(order.items) ? order.items : [];
    const orderDate = order.created_at ? new Date(order.created_at).toLocaleString('ru') : '-';
    const buyer = order.buyer_name || order.buyer_user_name || '-';
    const status = order.order_status_name || 'Новый';
    const address = order.location_address || '-';
    const comment = order.comment || '';

    let itemsSum = 0;
    const itemRows = items.map((it, idx) => {
        const line = (parseFloat(it.qty) || 0) * (parseFloat(it.unit_price) || 0);
        itemsSum += line;
        const name = it.product_name || it.supplier_item_name || '—';
        return `
        <tr>
            <td class="text-muted">${idx + 1}</td>
            <td>${escapeHtml(name)}</td>
            <td>${escapeHtml(it.supplier_name || '-')}</td>
            <td class="text-end">${escapeHtml(String(it.qty ?? 0))}</td>
            <td class="text-end">${fmtMoney(it.unit_price)}</td>
            <td class="text-end fw-semibold">${fmtMoney(line)}</td>
        </tr>`;
    }).join('');

    const total = order.total_amount != null ? order.total_amount : itemsSum;

    const itemsBlock = items.length
        ? `<div class="table-responsive">
            <table class="table table-sm table-striped align-middle mb-0">
                <thead>
                    <tr>
                        <th style="width:40px">№</th>
                        <th>Препарат</th>
                        <th>Поставщик</th>
                        <th class="text-end" style="width:90px">Кол-во</th>
                        <th class="text-end" style="width:110px">Цена</th>
                        <th class="text-end" style="width:120px">Сумма</th>
                    </tr>
                </thead>
                <tbody>${itemRows}</tbody>
                <tfoot>
                    <tr>
                        <th colspan="5" class="text-end">Итого:</th>
                        <th class="text-end">${fmtMoney(total)}</th>
                    </tr>
                </tfoot>
            </table>
        </div>`
        : `<div class="text-center text-muted py-3">Позиции по заказу отсутствуют</div>`;

    return `
    <div class="row g-3 mb-3">
        <div class="col-md-6">
            <div class="text-muted small">Покупатель</div>
            <div class="fw-semibold">${escapeHtml(buyer)}</div>
        </div>
        <div class="col-md-3">
            <div class="text-muted small">Дата</div>
            <div class="fw-semibold">${escapeHtml(orderDate)}</div>
        </div>
        <div class="col-md-3">
            <div class="text-muted small">Статус</div>
            <div><span class="badge bg-secondary">${escapeHtml(status)}</span></div>
        </div>
        <div class="col-md-9">
            <div class="text-muted small">Адрес доставки</div>
            <div class="fw-semibold">${escapeHtml(address)}</div>
        </div>
        <div class="col-md-3">
            <div class="text-muted small">Сумма заказа</div>
            <div class="fw-bold fs-5">${fmtMoney(total)} ₽</div>
        </div>
        ${comment ? `<div class="col-12">
            <div class="text-muted small">Комментарий</div>
            <div>${escapeHtml(comment)}</div>
        </div>` : ''}
    </div>
    <h6 class="mb-2">Позиции заказа</h6>
    ${itemsBlock}`;
}

async function viewOrder(orderId) {
    const modalEl = ensureOrderModal();
    const body = document.getElementById('orderViewBody');
    body.innerHTML = '<div class="text-center text-muted py-4">Загрузка данных...</div>';
    const modal = bootstrap.Modal.getOrCreateInstance(modalEl);
    modal.show();

    try {
        const order = await API.get(`/api/orders/${orderId}`);
        body.innerHTML = renderOrderView(order || {});
    } catch (error) {
        console.error('Ошибка загрузки заказа:', error);
        body.innerHTML = '<div class="alert alert-danger mb-0">Не удалось загрузить заказ</div>';
        if (typeof Toast !== 'undefined' && Toast.error) {
            Toast.error('Ошибка', 'Не удалось загрузить заказ');
        }
    }
}

function escapeHtml(text) {
    if (text == null) return '';
    const div = document.createElement('div');
    div.textContent = String(text);
    return div.innerHTML;
}
