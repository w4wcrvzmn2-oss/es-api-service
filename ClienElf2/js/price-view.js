let allItems = [];
let priceListId = null;
let currentMatchFilter = 'all';
let _loadGen = 0;
const FIRST_BATCH = 200;
let _pvPageSize = 50;
let _pvPage = 0;
let _pvSearch = '';
let _pvSortKey = null;
let _pvSortDir = 'asc';
let _pvColFilters = {};

document.addEventListener('DOMContentLoaded', async () => {
    const params = new URLSearchParams(window.location.search);
    priceListId = params.get('id');
    if (!priceListId) {
        Toast.error('Ошибка', 'Не указан ID прайса');
        return;
    }
    document.getElementById('btnEdit').href = `price-edit.html?id=${priceListId}`;

    document.querySelectorAll('input[name="filterMatch"]').forEach(r => {
        r.addEventListener('change', () => {
            currentMatchFilter = r.value;
            loadPriceItems();
        });
    });

    document.getElementById('tblPriceItems')?.querySelector('thead')?.addEventListener('click', (e) => {
        const th = e.target.closest('th[data-sort-key]');
        if (!th) return;
        const key = th.dataset.sortKey;
        if (_pvSortKey === key) { _pvSortDir = _pvSortDir === 'asc' ? 'desc' : 'asc'; }
        else { _pvSortKey = key; _pvSortDir = 'asc'; }
        th.closest('tr').querySelectorAll('th').forEach(h => h.classList.remove('sort-asc', 'sort-desc'));
        th.classList.add(_pvSortDir === 'asc' ? 'sort-asc' : 'sort-desc');
        _pvPage = 0;
        renderPvPage();
    });

    TableFilters.setup('tblPriceItems', {
        onFilter: (filters) => { _pvColFilters = filters; _pvPage = 0; renderPvPage(); },
        getValues: (colIdx) => {
            const field = _pvColMap[parseInt(colIdx)];
            if (!field) return [];
            const vals = new Set();
            allItems.forEach(it => {
                let v;
                if (field === '_status') v = (it.guid_es && it.guid_es.trim()) ? 'Сопоставлен' : 'Не сопоставлен';
                else if (field === 'price' || field === 'final_price') v = it[field] != null ? Number(it[field]).toFixed(2) : '';
                else if (field === 'quantity') v = it[field] != null ? String(it[field]) : '';
                else v = it[field] || '';
                if (v) vals.add(v);
            });
            return Array.from(vals).sort((a, b) => a.localeCompare(b, 'ru', { numeric: true, sensitivity: 'base' }));
        }
    });

    await loadPriceItems();
});

function buildUrl(extra) {
    const p = new URLSearchParams();
    if (currentMatchFilter && currentMatchFilter !== 'all') p.set('match_status', currentMatchFilter);
    if (extra) Object.entries(extra).forEach(([k, v]) => p.set(k, v));
    const qs = p.toString();
    return `/api/price-lists/${priceListId}/items` + (qs ? '?' + qs : '');
}

async function loadPriceItems() {
    const gen = ++_loadGen;
    const tbody = document.getElementById('priceItemsBody');
    if (!tbody) return;
    tbody.innerHTML = '<tr><td colspan="11" class="text-center text-muted py-4"><span class="spinner-border spinner-border-sm me-2"></span>Загрузка позиций...</td></tr>';

    try {
        const data = await API.get(buildUrl({ limit: FIRST_BATCH }));
        if (gen !== _loadGen) return;

        const pl = data.price_list || {};
        const stats = data.stats || {};
        allItems = data.items || [];

        document.getElementById('pageTitle').textContent = pl.name || 'Просмотр прайса';
        document.title = (pl.name || 'Прайс') + ' - ЭльФиСА';

        document.getElementById('infoName').textContent = pl.name || '-';
        document.getElementById('infoSupplier').textContent = pl.supplier_name || '-';
        document.getElementById('infoLastUpdate').textContent = pl.last_update_at || '-';
        document.getElementById('infoDescription').textContent = pl.description || '—';

        if (pl.supplier_id) {
            document.getElementById('btnLinks').href = `links.html?supplier=${pl.supplier_id}`;
            window._supplierId = pl.supplier_id;
        }

        const total = stats.total || 0;
        const matched = stats.matched || 0;
        const unmatched = stats.unmatched || 0;
        document.getElementById('statTotal').textContent = total.toLocaleString('ru-RU');
        document.getElementById('statMatched').textContent = matched.toLocaleString('ru-RU');
        document.getElementById('statUnmatched').textContent = unmatched.toLocaleString('ru-RU');

        const pctMatched = total > 0 ? (matched / total * 100) : 0;
        document.getElementById('progressMatched').style.width = pctMatched + '%';
        document.getElementById('progressUnmatched').style.width = (100 - pctMatched) + '%';
        document.getElementById('statPercent').textContent = total > 0
            ? `${pctMatched.toFixed(1)}% сопоставлено`
            : '';

        renderItems(allItems);
        updateFilterInfo(allItems.length, stats);

        const filterTotal = currentMatchFilter === 'matched' ? matched
            : currentMatchFilter === 'unmatched' ? unmatched : total;

        if (allItems.length < filterTotal) {
            loadRemaining(gen, allItems.length, stats);
        }
    } catch (e) {
        if (gen !== _loadGen) return;
        console.error('Ошибка загрузки позиций:', e);
        tbody.innerHTML = `<tr><td colspan="11" class="text-center text-danger py-4">Ошибка: ${escapeHtml(e.message)}</td></tr>`;
    }
}

async function loadRemaining(gen, loaded, stats) {
    const info = document.getElementById('filterInfo');
    const BATCH = 2000;
    let offset = loaded;
    try {
        while (true) {
            const data = await API.get(buildUrl({ limit: BATCH, offset: offset }));
            if (gen !== _loadGen) return;
            if (!data) throw new Error('Нет ответа от сервера');

            const moreItems = data.items || [];
            if (moreItems.length === 0) break;
            allItems = allItems.concat(moreItems);
            offset += moreItems.length;

            if (info) {
                const filterTotal = currentMatchFilter === 'matched' ? (stats.matched || 0)
                    : currentMatchFilter === 'unmatched' ? (stats.unmatched || 0) : (stats.total || 0);
                info.innerHTML = `Загружено ${allItems.length.toLocaleString('ru-RU')} из ${filterTotal.toLocaleString('ru-RU')} <span class="spinner-border spinner-border-sm ms-1"></span>`;
            }

            if (moreItems.length < BATCH) break;
        }
        if (gen !== _loadGen) return;
        renderItems(allItems);
        updateFilterInfo(allItems.length, stats);
    } catch (e) {
        if (gen !== _loadGen) return;
        console.error('Ошибка фоновой дозагрузки:', e);
        if (info) info.textContent = `Загружено ${allItems.length.toLocaleString('ru-RU')} (ошибка загрузки)`;
    }
}

function updateFilterInfo(returned, stats) {
    const info = document.getElementById('filterInfo');
    const total = stats.total || 0;
    const matched = stats.matched || 0;
    const unmatched = stats.unmatched || 0;
    const filterTotal = currentMatchFilter === 'matched' ? matched
        : currentMatchFilter === 'unmatched' ? unmatched : total;

    if (returned < filterTotal) {
        const label = currentMatchFilter === 'matched' ? 'Сопоставленные'
            : currentMatchFilter === 'unmatched' ? 'Не сопоставленные' : '';
        info.innerHTML = `${label ? label + ': ' : ''}Загружено ${returned.toLocaleString('ru-RU')} из ${filterTotal.toLocaleString('ru-RU')} <span class="spinner-border spinner-border-sm ms-1"></span>`;
    } else if (currentMatchFilter === 'matched') {
        info.textContent = `Сопоставленные: ${returned.toLocaleString('ru-RU')}`;
    } else if (currentMatchFilter === 'unmatched') {
        info.textContent = `Не сопоставленные: ${returned.toLocaleString('ru-RU')}`;
    } else {
        info.textContent = '';
    }
}

const _pvColMap = ['item_code','item_name','drug_name','producer','inn','price','final_price','quantity','batch_number','expiry_date','_status'];

function getPvFiltered() {
    let arr = allItems;
    for (const colIdx in _pvColFilters) {
        const f = _pvColFilters[colIdx];
        const field = _pvColMap[parseInt(colIdx)];
        if (!field) continue;
        arr = arr.filter(it => {
            let v;
            if (field === '_status') v = (it.guid_es && it.guid_es.trim()) ? 'Сопоставлен' : 'Не сопоставлен';
            else if (field === 'price' || field === 'final_price') v = it[field] != null ? Number(it[field]).toFixed(2) : '';
            else if (field === 'quantity') v = it[field] != null ? String(it[field]) : '';
            else v = (it[field] || '');
            const vl = v.toLowerCase();
            if (f.partial) return vl.includes(f.q);
            return f.values ? f.values.has(vl) : true;
        });
    }
    if (_pvSearch) {
        const q = _pvSearch.toLowerCase();
        arr = arr.filter(it =>
            (it.item_name || '').toLowerCase().includes(q) ||
            (it.item_code || '').toLowerCase().includes(q) ||
            (it.drug_name || '').toLowerCase().includes(q) ||
            (it.inn || '').toLowerCase().includes(q) ||
            (it.producer || '').toLowerCase().includes(q)
        );
    }
    if (_pvSortKey) {
        const numKeys = ['price', 'final_price', 'quantity'];
        arr = [...arr].sort((a, b) => {
            let va, vb;
            if (_pvSortKey === 'status') {
                va = (a.guid_es && a.guid_es.trim()) ? 1 : 0;
                vb = (b.guid_es && b.guid_es.trim()) ? 1 : 0;
            } else if (numKeys.includes(_pvSortKey)) {
                va = parseFloat(a[_pvSortKey]) || 0;
                vb = parseFloat(b[_pvSortKey]) || 0;
            } else {
                va = (a[_pvSortKey] || '').toLowerCase();
                vb = (b[_pvSortKey] || '').toLowerCase();
            }
            if (va < vb) return _pvSortDir === 'asc' ? -1 : 1;
            if (va > vb) return _pvSortDir === 'asc' ? 1 : -1;
            return 0;
        });
    }
    return arr;
}

function renderItems(items) {
    _pvPage = 0;
    _pvSearch = '';
    const si = document.getElementById('pvSearchInput');
    if (si) si.value = '';
    renderPvPage();
    setupPvPagination();
}

function setupPvPagination() {
    if (document.getElementById('pvPagBar')) return;
    const tbl = document.getElementById('tblPriceItems');
    if (!tbl || !tbl.parentNode) return;

    const bar = document.createElement('div');
    bar.id = 'pvPagBar';
    bar.className = 'd-flex align-items-center justify-content-between flex-wrap gap-2 mb-2 px-2 py-2';
    bar.innerHTML = `
        <div class="d-flex align-items-center gap-2 flex-grow-1" style="max-width:400px">
            <input type="search" id="pvSearchInput" class="form-control form-control-sm" placeholder="Поиск…">
        </div>
        <div class="d-flex align-items-center gap-2 flex-wrap">
            <div class="d-flex gap-1">
                <button id="pvExpCsv" class="btn btn-sm btn-outline-secondary" title="CSV"><i class="bi bi-filetype-csv"></i> CSV</button>
                <button id="pvExpXls" class="btn btn-sm btn-outline-secondary" title="Excel"><i class="bi bi-file-earmark-spreadsheet"></i> Excel</button>
                <button id="pvExpPdf" class="btn btn-sm btn-outline-secondary" title="PDF"><i class="bi bi-filetype-pdf"></i> PDF</button>
            </div>
            <span id="pvPagInfo" class="text-muted small"></span>
            <button id="pvPagPrev" class="btn btn-sm btn-outline-secondary">&laquo;</button>
            <button id="pvPagNext" class="btn btn-sm btn-outline-secondary">&raquo;</button>
            <select id="pvPageSize" class="form-select form-select-sm" style="width:auto">
                <option value="25">25</option>
                <option value="50" selected>50</option>
                <option value="100">100</option>
            </select>
            <span class="text-muted small text-nowrap">на стр.</span>
        </div>`;
    tbl.parentNode.insertBefore(bar, tbl);

    document.getElementById('pvPagPrev').onclick = () => { if (_pvPage > 0) { _pvPage--; renderPvPage(); } };
    document.getElementById('pvPagNext').onclick = () => {
        if ((_pvPage + 1) * _pvPageSize < getPvFiltered().length) { _pvPage++; renderPvPage(); }
    };
    document.getElementById('pvSearchInput').oninput = (e) => {
        _pvSearch = e.target.value.trim();
        _pvPage = 0;
        renderPvPage();
    };
    document.getElementById('pvPageSize').onchange = (e) => {
        _pvPageSize = parseInt(e.target.value, 10);
        _pvPage = 0;
        renderPvPage();
    };
    document.getElementById('pvExpCsv').onclick = () => pvExport('csv');
    document.getElementById('pvExpXls').onclick = () => pvExport('excel');
    document.getElementById('pvExpPdf').onclick = () => pvExport('pdf');
}

function renderPvPage() {
    const tbody = document.getElementById('priceItemsBody');
    if (!tbody) return;
    const filtered = getPvFiltered();
    if (filtered.length === 0) {
        tbody.innerHTML = '<tr><td colspan="11" class="text-center text-muted py-4">Нет позиций</td></tr>';
        updatePvPagInfo(0, 0, 0);
        return;
    }
    const start = _pvPage * _pvPageSize;
    const end = Math.min(start + _pvPageSize, filtered.length);
    const page = filtered.slice(start, end);

    tbody.innerHTML = page.map(it => {
        const isMatched = it.guid_es && it.guid_es.trim() !== '';
        const statusBadge = isMatched
            ? '<span class="badge bg-success">Сопоставлен</span>'
            : `<button class="btn btn-outline-danger btn-sm" onclick="openMatchModal('${it.id}')" title="Сопоставить позицию"><i class="bi bi-link-45deg"></i> Связать</button>`;
        const drugNameHtml = it.drug_name
            ? `<span class="text-success">${escapeHtml(it.drug_name)}</span>`
            : '<span class="text-muted fst-italic">—</span>';
        const price = it.price != null ? formatPrice(it.price) : '-';
        const finalPrice = it.final_price != null ? formatPrice(it.final_price) : '-';
        const expiry = it.expiry_date || '-';

        return `<tr class="pv-row" data-matched="${isMatched ? '1' : '0'}">
            <td class="small">${escapeHtml(it.item_code || '-')}</td>
            <td>${escapeHtml(it.item_name || '-')}</td>
            <td>${drugNameHtml}</td>
            <td class="small">${escapeHtml(it.producer || '-')}</td>
            <td class="small">${escapeHtml(it.inn || '-')}</td>
            <td class="text-end">${price}</td>
            <td class="text-end fw-semibold">${finalPrice}</td>
            <td class="text-center">${it.quantity != null ? it.quantity : '-'}</td>
            <td class="small">${escapeHtml(it.batch_number || '-')}</td>
            <td class="small">${expiry}</td>
            <td class="text-center">${statusBadge}</td>
        </tr>`;
    }).join('');

    updatePvPagInfo(start + 1, end, filtered.length);
}

function updatePvPagInfo(from, to, total) {
    const el = document.getElementById('pvPagInfo');
    if (el) el.textContent = total > 0 ? `${from}–${to} из ${total.toLocaleString('ru-RU')}` : '';
    const prev = document.getElementById('pvPagPrev');
    const next = document.getElementById('pvPagNext');
    if (prev) prev.disabled = _pvPage === 0;
    if (next) next.disabled = to >= total;
}


// --- Сопоставление ---
let _editItem = null;
let _selectedDrug = null;
let _matchModal = null;

function openMatchModal(itemId) {
    const item = allItems.find(i => i.id === itemId);
    if (!item) return;
    _editItem = item;
    _selectedDrug = null;

    document.getElementById('pvModalItem').innerHTML = `
        <strong>${escapeHtml(item.item_name || '-')}</strong>
        <div class="small text-muted mt-1">Код: ${escapeHtml(item.item_code || '-')}
        ${item.price != null ? ' &bull; Цена: ' + formatPrice(item.price) + ' ₽' : ''}</div>`;

    document.getElementById('pvCurrentMatch').style.display = 'none';
    document.getElementById('pvDrugSearch').value = item.item_name || '';
    document.getElementById('pvSearchResults').style.display = 'none';

    if (!_matchModal) _matchModal = new bootstrap.Modal(document.getElementById('matchModal'));
    _matchModal.show();
}

document.addEventListener('DOMContentLoaded', () => {
    document.getElementById('pvSearchBtn')?.addEventListener('click', pvSearchDrugs);
    document.getElementById('pvDrugSearch')?.addEventListener('keypress', e => { if (e.key === 'Enter') pvSearchDrugs(); });
    document.getElementById('pvSaveBtn')?.addEventListener('click', pvSaveMatch);
});

async function pvSearchDrugs() {
    const q = document.getElementById('pvDrugSearch')?.value.trim();
    if (!q || q.length < 2) { Toast.warning('Внимание', 'Минимум 2 символа'); return; }

    const div = document.getElementById('pvSearchResults');
    const list = document.getElementById('pvSearchList');
    div.style.display = 'block';
    list.innerHTML = '<div class="p-3 text-center"><span class="spinner-border spinner-border-sm"></span> Поиск...</div>';

    try {
        const resp = await API.get('/api/drugs/search?q=' + encodeURIComponent(q));
        const drugs = resp?.drugs || [];
        if (!drugs.length) { list.innerHTML = '<div class="p-3 text-center text-muted">Не найдено</div>'; return; }

        window._pvDrugs = drugs;
        list.innerHTML = drugs.map((d, i) => `
            <div class="p-2 border-bottom" style="cursor:pointer" onclick="pvSelectDrug(${i})">
                <div class="d-flex justify-content-between align-items-start">
                    <div>
                        <strong>${escapeHtml(d.name)}</strong><br>
                        ${d.inn ? '<small class="text-muted">МНН: ' + escapeHtml(d.inn) + '</small><br>' : ''}
                        ${d.producer_name ? '<small class="text-muted">' + escapeHtml(d.producer_name) + '</small>' : ''}
                    </div>
                    <button class="btn btn-sm btn-success" onclick="event.stopPropagation();pvSelectDrug(${i})">Выбрать</button>
                </div>
            </div>`).join('');
    } catch (e) {
        list.innerHTML = '<div class="p-3 text-center text-danger">Ошибка поиска</div>';
    }
}

function pvSelectDrug(idx) {
    const drug = window._pvDrugs?.[idx];
    if (!drug) return;
    _selectedDrug = drug;

    const el = document.getElementById('pvCurrentMatch');
    el.style.display = 'block';
    document.getElementById('pvCurrentMatchDetails').innerHTML = `
        <strong class="text-success">${escapeHtml(drug.name)}</strong>
        ${drug.inn ? '<div class="small">МНН: ' + escapeHtml(drug.inn) + '</div>' : ''}
        ${drug.cure_form ? '<div class="small">Форма: ' + escapeHtml(drug.cure_form) + '</div>' : ''}`;

    document.getElementById('pvSearchResults').style.display = 'none';
    document.getElementById('pvDrugSearch').value = drug.name;
}

async function pvSaveMatch() {
    if (!_editItem || !_selectedDrug?.guid_es) { Toast.warning('Внимание', 'Выберите препарат'); return; }

    const btn = document.getElementById('pvSaveBtn');
    btn.disabled = true;
    btn.innerHTML = '<span class="spinner-border spinner-border-sm"></span> Сохранение...';

    try {
        await API.put('/api/supplier-prices/' + _editItem.id + '/match', {
            guid_es: _selectedDrug.guid_es,
            match_method: 'MANUAL',
            match_confidence: 100
        });
        Toast.success('Сохранено', 'Позиция сопоставлена');
        _matchModal.hide();
        await loadPriceItems();
    } catch (e) {
        Toast.error('Ошибка', e.message);
    } finally {
        btn.disabled = false;
        btn.innerHTML = '<i class="bi bi-check-lg"></i> Сохранить';
    }
}

function pvExport(fmt) {
    const items = getPvFiltered();
    const headers = ['Код', 'Наименование', 'Номенклатура ЕС', 'Производитель', 'МНН', 'Цена', 'Цена+%', 'Кол-во', 'Серия', 'Годен до', 'Статус'];
    const rows = items.map(it => [
        it.item_code || '', it.item_name || '', it.drug_name || '', it.producer || '',
        it.inn || '', it.price != null ? Number(it.price).toFixed(2) : '',
        it.final_price != null ? Number(it.final_price).toFixed(2) : '',
        it.quantity != null ? String(it.quantity) : '', it.batch_number || '',
        it.expiry_date || '', (it.guid_es && it.guid_es.trim()) ? 'Сопоставлен' : 'Не сопоставлен'
    ]);
    const name = 'Прайс';
    if (fmt === 'csv') _dlCsv(headers, rows, name);
    else if (fmt === 'excel') _dlXlsx(headers, rows, name);
    else _dlPdf(headers, rows, name);
}

function _dlCsv(headers, rows, name) {
    const bom = '\uFEFF';
    const csv = bom + headers.join(';') + '\n' + rows.map(r => r.map(c => '"' + String(c).replace(/"/g, '""') + '"').join(';')).join('\n');
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
    const a = document.createElement('a'); a.href = URL.createObjectURL(blob); a.download = name + '.csv';
    document.body.appendChild(a); a.click(); document.body.removeChild(a); URL.revokeObjectURL(a.href);
}

function _dlXlsx(headers, rows, name) {
    if (typeof XLSX === 'undefined') { Toast.error('Ошибка', 'Библиотека SheetJS не загружена'); return; }
    const ws = XLSX.utils.aoa_to_sheet([headers, ...rows]);
    const wb = XLSX.utils.book_new(); XLSX.utils.book_append_sheet(wb, ws, 'Данные');
    XLSX.writeFile(wb, name + '.xlsx');
}

function _dlPdf(headers, rows, name) {
    let html = '<!DOCTYPE html><html><head><meta charset="utf-8"><title>' + escapeHtml(name) + '</title>' +
        '<style>@page{size:landscape A4;margin:12mm}body{font-family:Arial,sans-serif;font-size:10px}' +
        'h2{font-size:14px;margin:0 0 6px}small{color:#888}' +
        'table{border-collapse:collapse;width:100%}th{background:#31528f;color:#fff;padding:4px 6px;text-align:left;font-size:9px}' +
        'td{border-bottom:1px solid #ddd;padding:3px 6px;font-size:9px}tr:nth-child(even){background:#f8f9fa}</style></head><body>' +
        '<h2>' + escapeHtml(name) + '</h2><small>' + new Date().toLocaleDateString('ru-RU') + '</small><table><thead><tr>';
    headers.forEach(h => { html += '<th>' + escapeHtml(h) + '</th>'; });
    html += '</tr></thead><tbody>';
    rows.forEach(r => { html += '<tr>'; r.forEach(c => { html += '<td>' + escapeHtml(c) + '</td>'; }); html += '</tr>'; });
    html += '</tbody></table></body></html>';
    const w = window.open('', '_blank'); w.document.write(html); w.document.close(); w.onload = () => w.print();
}

function formatPrice(val) {
    if (val == null) return '-';
    return Number(val).toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

function escapeHtml(text) {
    if (text == null) return '';
    const div = document.createElement('div');
    div.textContent = String(text);
    return div.innerHTML;
}
