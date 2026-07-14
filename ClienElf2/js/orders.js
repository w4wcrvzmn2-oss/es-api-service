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

function viewOrder(orderId) {
    Toast.info('В разработке', 'Функция просмотра заказа ещё не реализована');
}

function escapeHtml(text) {
    if (text == null) return '';
    const div = document.createElement('div');
    div.textContent = String(text);
    return div.innerHTML;
}
