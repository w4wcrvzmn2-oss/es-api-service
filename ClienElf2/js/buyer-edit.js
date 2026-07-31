// Редактирование покупателя
let currentBuyerId = null;
let accountUsers = [];   // существующие логины (BuyerUser) покупателя

document.addEventListener('DOMContentLoaded', async () => {
    const urlParams = new URLSearchParams(window.location.search);
    currentBuyerId = urlParams.get('id');

    await loadRegions();

    if (currentBuyerId) {
        document.getElementById('pageTitle').textContent = 'Редактирование покупателя';
        document.getElementById('deleteBtn').style.display = 'inline-block';
        await loadBuyer(currentBuyerId);
        // Кабинет и точки доставки доступны только у существующего покупателя.
        document.getElementById('locationsCard').style.display = '';
        await loadLocations(currentBuyerId);
        await loadAccount(currentBuyerId);
    }

    // Подключённые прайсы грузим и при создании, и при редактировании.
    await loadPriceListAssignments(currentBuyerId);

    document.getElementById('buyerForm').addEventListener('submit', handleSubmit);

    // Маска телефона: форматируем по мере ввода
    const phoneEl = document.getElementById('phone');
    if (phoneEl) {
        phoneEl.addEventListener('input', () => { phoneEl.value = formatPhone(phoneEl.value); });
    }
});

// Форматирует номер в вид +7 (999) 123-45-67
function formatPhone(value) {
    let d = (value || '').replace(/\D/g, '');
    if (d.startsWith('8')) d = '7' + d.slice(1);
    if (d.startsWith('7')) d = d.slice(1);
    d = d.slice(0, 10);
    if (d.length === 0) return '';
    let res = '+7';
    if (d.length > 0) res += ' (' + d.slice(0, 3);
    if (d.length >= 3) res += ')';
    if (d.length > 3) res += ' ' + d.slice(3, 6);
    if (d.length >= 6) res += '-' + d.slice(6, 8);
    if (d.length >= 8) res += '-' + d.slice(8, 10);
    return res;
}

async function loadBuyer(id) {
    try {
        const data = await API.get(`/api/buyers/${id}`);
        if (data) {
            document.getElementById('name').value = data.name || '';
            document.getElementById('inn').value = data.inn || '';
            document.getElementById('code').value = data.code || '';
            document.getElementById('phone').value = data.phone ? formatPhone(data.phone) : '';
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
    const locSelect = document.getElementById('locRegion');
    try {
        const data = await API.get('/api/region?columns=RegionID,Name');
        const rows = API.unwrapList(data);
        if (rows.length > 0) {
            const opts = rows.map(region =>
                `<option value="${(region.RegionID || region.region_id || '').toLowerCase()}">${escapeHtml(region.Name || region.name || '-')}</option>`).join('');
            if (select) select.innerHTML = '<option value="">Выберите регион</option>' + opts;
            if (locSelect) locSelect.innerHTML = '<option value="">—</option>' + opts;
        } else {
            if (select) select.innerHTML = '<option value="">Нет регионов</option>';
        }
    } catch (error) {
        if (select) select.innerHTML = '<option value="">Ошибка загрузки</option>';
    }
}

async function handleSubmit(e) {
    e.preventDefault();

    const regionId = document.getElementById('regionSelect').value;
    const address = document.getElementById('address').value || null;

    const formData = {
        name: document.getElementById('name').value,
        inn: document.getElementById('inn').value || null,
        region_id: regionId || null,
        code: document.getElementById('code').value || null,
        phone: document.getElementById('phone').value || null,
        address: address,
        email: document.getElementById('email').value || null,
        is_active: document.getElementById('isActive').checked
    };

    const isCreate = !currentBuyerId;

    try {
        let buyerId = currentBuyerId;
        if (isCreate) {
            const created = await API.post('/api/buyers', formData);
            buyerId = (created && (created.buyer_id || created.BuyerID)) || null;
        } else {
            await API.put(`/api/buyers/${currentBuyerId}`, formData);
        }

        if (!buyerId) {
            // Не смогли определить ID нового покупателя — уходим в список.
            window.location.href = 'buyers.html';
            return;
        }

        // Личный кабинет (логин + пароль), если заполнены.
        await ensureAccount(buyerId);

        // Назначенные прайсы + индивидуальная наценка (и при создании, и при редактировании).
        try {
            await API.put(`/api/buyers/${buyerId}/price-lists`, { items: collectPriceListItems() });
        } catch (err) {
            console.error('Не удалось сохранить прайсы покупателя:', err);
        }

        // При создании — заводим точку доставки из адреса, чтобы сразу был Location ID.
        if (isCreate && address) {
            try {
                await API.post('/api/buyer-locations', {
                    buyer_id: buyerId,
                    address: address,
                    region_id: regionId || null,
                    is_default: true
                });
            } catch (err) {
                console.error('Не удалось создать точку доставки:', err);
            }
        }

        if (isCreate) {
            // Переходим в режим редактирования нового покупателя — там виден Location ID.
            window.location.href = `buyer-edit.html?id=${buyerId}`;
        } else {
            window.location.href = 'buyers.html';
        }
    } catch (error) {
        Toast.error('Ошибка сохранения', error.message);
    }
}

// Создаёт логин покупателя или задаёт пароль существующему, если поля заполнены.
async function ensureAccount(buyerId) {
    const email = (document.getElementById('loginEmail').value || '').trim();
    const password = document.getElementById('loginPassword').value || '';

    if (!email && !password) return; // кабинет не запрашивали

    if (!email || !password) {
        Toast.error('Кабинет не создан', 'Укажите и логин (e-mail), и пароль');
        return;
    }

    // Уже есть пользователь с таким email — просто задаём пароль.
    const existing = accountUsers.find(u => (u.email || u.Email || '').toLowerCase() === email.toLowerCase());
    try {
        if (existing) {
            const uid = existing.buyer_user_id || existing.BuyerUserID;
            await API.post(`/api/buyer-users/${uid}/password`, { password });
            Toast.success('Пароль обновлён', email);
        } else {
            await API.post('/api/buyer-users', {
                buyer_id: buyerId,
                full_name: document.getElementById('name').value || email,
                email: email,
                role: 'buyer',
                is_active: true,
                password: password
            });
            Toast.success('Кабинет создан', email);
        }
        document.getElementById('loginPassword').value = '';
    } catch (err) {
        Toast.error('Кабинет не создан', err.message || 'Ошибка');
    }
}

async function loadAccount(buyerId) {
    const box = document.getElementById('accountExisting');
    try {
        accountUsers = await API.get(`/api/buyer-users?buyer_id=${buyerId}`) || [];
    } catch (e) {
        accountUsers = [];
    }
    if (!Array.isArray(accountUsers) || accountUsers.length === 0) {
        box.style.display = 'none';
        // Подставим e-mail покупателя как логин по умолчанию.
        const email = document.getElementById('email').value;
        if (email && !document.getElementById('loginEmail').value) {
            document.getElementById('loginEmail').value = email;
        }
        document.getElementById('accountHint').textContent =
            'Заполните логин и пароль — при сохранении будет создан личный кабинет покупателя. Вход по этому e-mail и паролю.';
        return;
    }

    box.style.display = '';
    box.innerHTML = '<div class="fw-semibold mb-2">Существующие входы:</div>' +
        accountUsers.map(u => {
            const uid = u.buyer_user_id || u.BuyerUserID;
            const em = u.email || u.Email || '';
            const active = (u.is_active ?? u.IsActive) !== false;
            return `<div class="d-flex align-items-center gap-2 mb-1">
                <span class="badge ${active ? 'bg-success' : 'bg-secondary'}">${active ? 'активен' : 'выкл'}</span>
                <code>${escapeHtml(em)}</code>
                <button type="button" class="btn btn-sm btn-outline-secondary" onclick="resetPassword('${uid}','${escapeHtml(em)}')">
                    <i class="bi bi-key"></i> Сбросить пароль
                </button>
            </div>`;
        }).join('');
    document.getElementById('accountHint').textContent =
        'Чтобы добавить ещё один вход — впишите новый e-mail и пароль. Чтобы сменить пароль существующему — впишите его e-mail и новый пароль.';
}

async function resetPassword(userId, email) {
    const pwd = prompt(`Новый пароль для ${email}:`);
    if (!pwd) return;
    try {
        await API.post(`/api/buyer-users/${userId}/password`, { password: pwd });
        Toast.success('Пароль обновлён', email);
    } catch (err) {
        Toast.error('Не удалось сменить пароль', err.message || 'Ошибка (нужны права администратора)');
    }
}

async function loadLocations(buyerId) {
    const list = document.getElementById('locationsList');
    try {
        const locs = await API.get(`/api/buyer-locations?buyer_id=${buyerId}`) || [];
        if (!Array.isArray(locs) || locs.length === 0) {
            list.innerHTML = '<div class="text-muted">Точек доставки нет. Добавьте ниже — появится Location ID.</div>';
            return;
        }
        list.innerHTML = locs.map(l => {
            const id = l.buyer_location_id || l.BuyerLocationID || '';
            const addr = l.address || l.Address || '';
            const isDef = (l.is_default ?? l.IsDefault) === true;
            const region = l.RegionName || l.region_name || '';
            return `<div class="d-flex align-items-center gap-2 mb-2 flex-wrap">
                ${isDef ? '<span class="badge bg-primary">по умолчанию</span>' : ''}
                <code class="user-select-all">${escapeHtml(id)}</code>
                <button type="button" class="btn btn-sm btn-outline-secondary" onclick="copyText('${id}')" title="Копировать Location ID"><i class="bi bi-clipboard"></i></button>
                <span class="text-muted">${escapeHtml(addr)}${region ? ' · ' + escapeHtml(region) : ''}</span>
                <button type="button" class="btn btn-sm btn-outline-danger ms-auto" onclick="deleteLocation('${id}')" title="Удалить"><i class="bi bi-trash"></i></button>
            </div>`;
        }).join('');
    } catch (err) {
        list.innerHTML = '<div class="text-danger">Ошибка загрузки точек доставки</div>';
    }
}

async function addLocation() {
    if (!currentBuyerId) return;
    const addr = document.getElementById('locAddress').value.trim();
    if (!addr) { Toast.error('Адрес обязателен', 'Укажите адрес точки'); return; }
    const region = document.getElementById('locRegion').value || null;
    const isDefault = document.getElementById('locDefault').checked;
    try {
        await API.post('/api/buyer-locations', {
            buyer_id: currentBuyerId,
            address: addr,
            region_id: region,
            is_default: isDefault
        });
        document.getElementById('locAddress').value = '';
        document.getElementById('locDefault').checked = false;
        await loadLocations(currentBuyerId);
        Toast.success('Точка добавлена', 'Location ID создан');
    } catch (err) {
        Toast.error('Ошибка', err.message || 'Не удалось добавить точку');
    }
}

async function deleteLocation(id) {
    if (!confirm('Удалить эту точку доставки?')) return;
    const list = document.getElementById('locationsList');
    const btn = list ? list.querySelector(`button[onclick="deleteLocation('${id}')"]`) : null;
    const row = btn ? btn.closest('div') : null;
    if (row) {
        row.style.opacity = '0.35';
        row.style.pointerEvents = 'none';
    }
    try {
        await API.delete(`/api/buyer-locations/${id}`);
        if (row) row.remove();
        if (list && !list.querySelector('button[onclick^="deleteLocation"]')) {
            list.innerHTML = '<div class="text-muted">Точек доставки нет. Добавьте ниже — появится Location ID.</div>';
        }
        Toast.success('Удалено', 'Точка доставки удалена');
    } catch (err) {
        if (row) {
            row.style.opacity = '';
            row.style.pointerEvents = '';
        }
        Toast.error('Ошибка', err.message || 'Не удалось удалить точку');
    }
}

function copyText(text) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(
            () => Toast.success('Скопировано', text),
            () => Toast.info('Location ID', text)
        );
    } else {
        Toast.info('Location ID', text);
    }
}

function genPassword() {
    const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789';
    let p = '';
    const arr = new Uint32Array(12);
    (window.crypto || {}).getRandomValues ? window.crypto.getRandomValues(arr) : arr.forEach((_, i) => arr[i] = Math.floor(Math.random() * 1e9));
    for (let i = 0; i < 12; i++) p += chars[arr[i] % chars.length];
    const el = document.getElementById('loginPassword');
    el.type = 'text';
    el.value = p;
    document.getElementById('togglePwdIcon').className = 'bi bi-eye-slash';
}

function togglePwd() {
    const el = document.getElementById('loginPassword');
    const icon = document.getElementById('togglePwdIcon');
    if (el.type === 'password') { el.type = 'text'; icon.className = 'bi bi-eye-slash'; }
    else { el.type = 'password'; icon.className = 'bi bi-eye'; }
}

let allPriceListsForAssign = [];

async function loadPriceListAssignments(buyerId) {
    const box = document.getElementById('priceListsAssign');
    if (!box) return;
    try {
        const resp = await API.get('/api/price-lists');
        allPriceListsForAssign = (resp && resp.price_lists) || [];
    } catch (e) {
        box.innerHTML = '<div class="text-danger">Не удалось загрузить список прайсов</div>';
        return;
    }

    // Назначенные прайсы + индивидуальная наценка клиента.
    const assignedMarkup = {}; // price_list_id(lower) -> markup_pct
    if (buyerId) {
        try {
            const assigned = await API.get(`/api/buyers/${buyerId}/price-lists`) || [];
            assigned.forEach(a => { assignedMarkup[(a.price_list_id || '').toLowerCase()] = a.markup_pct || 0; });
        } catch (e) { /* назначений нет — ок */ }
    }

    if (allPriceListsForAssign.length === 0) {
        box.innerHTML = '<div class="text-muted">Прайсов пока нет.</div>';
        return;
    }

    box.innerHTML = allPriceListsForAssign.map(pl => {
        const id = pl.price_list_id;
        const key = (id || '').toLowerCase();
        const isAssigned = Object.prototype.hasOwnProperty.call(assignedMarkup, key);
        const checked = isAssigned ? 'checked' : '';
        const markupVal = isAssigned ? assignedMarkup[key] : '';
        const plMarkup = (pl.default_markup_pct != null) ? pl.default_markup_pct : 0;
        const search = ((pl.name || '') + ' ' + (pl.supplier_name || '')).toLowerCase();
        return `<div class="pl-row border-bottom py-2" data-search="${escapeHtml(search)}">
            <div class="d-flex align-items-center gap-2 flex-wrap">
                <div class="form-check mb-0 flex-grow-1">
                    <input class="form-check-input pl-assign" type="checkbox" value="${id}" id="pl_${id}" ${checked}>
                    <label class="form-check-label" for="pl_${id}">
                        ${escapeHtml(pl.name || '-')}${pl.supplier_name ? ` · <span class="text-muted">${escapeHtml(pl.supplier_name)}</span>` : ''}
                    </label>
                </div>
                <div class="input-group input-group-sm" style="width:200px">
                    <span class="input-group-text">Наценка клиента</span>
                    <input type="number" step="0.01" class="form-control pl-markup" data-pl="${id}" value="${markupVal}" placeholder="0">
                    <span class="input-group-text">%</span>
                </div>
                <span class="text-muted small" title="Наценка самого прайса" style="min-width:78px">прайс: ${plMarkup}%</span>
            </div>
        </div>`;
    }).join('');

    // Поиск/фильтр по прайсам.
    const searchEl = document.getElementById('plSearch');
    if (searchEl && !searchEl._bound) {
        searchEl._bound = true;
        searchEl.addEventListener('input', () => {
            const q = searchEl.value.trim().toLowerCase();
            document.querySelectorAll('#priceListsAssign .pl-row').forEach(row => {
                row.style.display = (!q || (row.getAttribute('data-search') || '').includes(q)) ? '' : 'none';
            });
        });
    }
}

// Собирает отмеченные прайсы с индивидуальной наценкой: [{price_list_id, markup_pct}].
function collectPriceListItems() {
    return Array.from(document.querySelectorAll('.pl-assign:checked')).map(el => {
        const id = el.value;
        const markupEl = document.querySelector(`.pl-markup[data-pl="${id}"]`);
        const markup = markupEl ? parseFloat(markupEl.value) : 0;
        return { price_list_id: id, markup_pct: isFinite(markup) ? markup : 0 };
    });
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
