let currentPrices = [];
let currentEditingPrice = null;
let _linksLoadGen = 0;
const LINKS_FIRST_BATCH = 200;
let _linksPageSize = 50;
let _linksPage = 0;
let _linksSearch = '';
let _linksMatchFilter = 'all';
let _linksSortKey = null;
let _linksSortDir = 'asc';
let _linksColFilters = {};

document.addEventListener('DOMContentLoaded', async () => {
    await loadSuppliers();

    document.getElementById('filterSupplier')?.addEventListener('change', () => loadPrices());
    document.getElementById('refreshBtn')?.addEventListener('click', () => loadPrices());
    document.getElementById('saveMatchBtn')?.addEventListener('click', saveMatch);
    document.getElementById('clearMatchBtn')?.addEventListener('click', clearMatch);
    document.getElementById('searchDrugBtn')?.addEventListener('click', searchDrugs);

    document.getElementById('modalDrugSearch')?.addEventListener('keypress', (e) => {
        if (e.key === 'Enter') searchDrugs();
    });

    document.querySelectorAll('input[name="filterMatch"]').forEach(r => {
        r.addEventListener('change', () => {
            _linksMatchFilter = r.value;
            _linksPage = 0;
            renderLinksPage();
        });
    });

    document.getElementById('tblLinks')?.querySelector('thead')?.addEventListener('click', (e) => {
        const th = e.target.closest('th[data-sort-key]');
        if (!th) return;
        const key = th.dataset.sortKey;
        if (_linksSortKey === key) { _linksSortDir = _linksSortDir === 'asc' ? 'desc' : 'asc'; }
        else { _linksSortKey = key; _linksSortDir = 'asc'; }
        th.closest('tr').querySelectorAll('th').forEach(h => h.classList.remove('sort-asc', 'sort-desc'));
        th.classList.add(_linksSortDir === 'asc' ? 'sort-asc' : 'sort-desc');
        _linksPage = 0;
        renderLinksPage();
    });

    document.addEventListener('click', (e) => {
        const btn = e.target.closest('.edit-match-btn');
        if (btn) {
            const priceID = btn.dataset.priceId;
            if (priceID) editMatch(priceID);
        }
    });

    TableFilters.setup('tblLinks', {
        onFilter: (filters) => { _linksColFilters = filters; _linksPage = 0; renderLinksPage(); },
        getValues: (colIdx) => {
            const field = _linksColMap[parseInt(colIdx)];
            if (!field) return [];
            const vals = new Set();
            currentPrices.forEach(p => {
                let v;
                if (field === '_status') v = (p.guid_es && p.guid_es.trim()) ? 'Сопоставлено' : 'Не сопоставлено';
                else if (field === 'price') v = p.price ? parseFloat(p.price).toFixed(2) + ' \u20BD' : '';
                else v = p[field] || '';
                if (v) vals.add(v);
            });
            return Array.from(vals).sort((a, b) => a.localeCompare(b, 'ru', { numeric: true, sensitivity: 'base' }));
        }
    });
});

async function loadSuppliers() {
    const select = document.getElementById('filterSupplier');
    if (!select) return;

    try {
        const suppliers = await API.get('/api/suppliers');
        if (suppliers && suppliers.length > 0) {
            select.innerHTML = '<option value="">Выберите поставщика</option>' +
                suppliers.map(s => `<option value="${s.supplier_id}">${escapeHtml(s.name)}</option>`).join('');

            const urlParams = new URLSearchParams(window.location.search);
            const urlSupplier = urlParams.get('supplier');
            if (urlSupplier) {
                select.value = urlSupplier;
                await loadPrices();
                return;
            }

            for (const supplier of suppliers) {
                try {
                    const testResponse = await API.get(`/api/supplier-prices?supplier_id=${supplier.supplier_id}&limit=1`);
                    const totalPrices = testResponse?.stats?.total_in_db
                        ?? testResponse?.total_prices
                        ?? (Array.isArray(testResponse?.prices) ? testResponse.prices.length : 0);
                    if (totalPrices > 0) {
                        select.value = supplier.supplier_id;
                        await loadPrices();
                        break;
                    }
                } catch (_) {}
            }
        }
    } catch (error) {
        console.error('Ошибка загрузки поставщиков:', error);
    }
}

function renderPriceRows(prices) {
    return prices.map(price => {
        const isMatched = price.guid_es && price.guid_es.trim() !== '';
        const priceValue = price.price ? parseFloat(price.price).toFixed(2) + ' \u20BD' : '-';
        return `
        <tr>
            <td><strong>${escapeHtml(price.supplier_item_name || '-')}</strong></td>
            <td>${escapeHtml(price.supplier_item_code || '-')}</td>
            <td>${escapeHtml(price.barcode || '-')}</td>
            <td style="text-align:right" data-sort="${price.price || 0}">${priceValue}</td>
            <td>
                <span class="badge ${isMatched ? 'bg-success' : 'bg-danger'}">
                    ${isMatched ? 'Сопоставлено' : 'Не сопоставлено'}
                </span>
            </td>
            <td>
                ${isMatched ? escapeHtml(price.drug_name || '-') : '-'}
                ${price.inn ? '<br><small class="text-muted">МНН: ' + escapeHtml(price.inn) + '</small>' : ''}
            </td>
            <td>${escapeHtml(price.match_method || '-')}</td>
            <td>
                <button data-price-id="${price.supplier_price_id}" class="btn btn-primary btn-sm edit-match-btn">
                    <i class="bi bi-pencil"></i>
                </button>
            </td>
        </tr>`;
    }).join('');
}

const _linksColMap = ['supplier_item_name','supplier_item_code','barcode','price','_status','drug_name','match_method'];

function getFilteredPrices() {
    let arr = currentPrices;
    if (_linksMatchFilter === 'matched') {
        arr = arr.filter(p => p.guid_es && p.guid_es.trim() !== '');
    } else if (_linksMatchFilter === 'unmatched') {
        arr = arr.filter(p => !p.guid_es || p.guid_es.trim() === '');
    }
    for (const colIdx in _linksColFilters) {
        const f = _linksColFilters[colIdx];
        const field = _linksColMap[parseInt(colIdx)];
        if (!field) continue;
        arr = arr.filter(p => {
            let v;
            if (field === '_status') v = (p.guid_es && p.guid_es.trim()) ? 'Сопоставлено' : 'Не сопоставлено';
            else if (field === 'price') v = p.price ? parseFloat(p.price).toFixed(2) + ' ₽' : '';
            else v = (p[field] || '');
            const vl = v.toLowerCase();
            if (f.partial) return vl.includes(f.q);
            return f.values ? f.values.has(vl) : true;
        });
    }
    if (_linksSearch) {
        const q = _linksSearch.toLowerCase();
        arr = arr.filter(p =>
            (p.supplier_item_name || '').toLowerCase().includes(q) ||
            (p.supplier_item_code || '').toLowerCase().includes(q) ||
            (p.barcode || '').toLowerCase().includes(q) ||
            (p.drug_name || '').toLowerCase().includes(q) ||
            (p.inn || '').toLowerCase().includes(q)
        );
    }
    if (_linksSortKey) {
        arr = [...arr].sort((a, b) => {
            let va, vb;
            if (_linksSortKey === 'price') {
                va = parseFloat(a.price) || 0; vb = parseFloat(b.price) || 0;
            } else if (_linksSortKey === 'status') {
                va = (a.guid_es && a.guid_es.trim()) ? 1 : 0;
                vb = (b.guid_es && b.guid_es.trim()) ? 1 : 0;
            } else {
                va = (a[_linksSortKey] || '').toLowerCase();
                vb = (b[_linksSortKey] || '').toLowerCase();
            }
            if (va < vb) return _linksSortDir === 'asc' ? -1 : 1;
            if (va > vb) return _linksSortDir === 'asc' ? 1 : -1;
            return 0;
        });
    }
    return arr;
}

function rebuildLinksTable() {
    _linksPage = 0;
    _linksSearch = '';
    const si = document.getElementById('linksSearchInput');
    if (si) si.value = '';
    renderLinksPage();
    setupLinksPagination();
}

function setupLinksPagination() {
    if (document.getElementById('linksPagBar')) return;
    const tbl = document.getElementById('tblLinks');
    if (!tbl || !tbl.parentNode) return;

    const bar = document.createElement('div');
    bar.id = 'linksPagBar';
    bar.className = 'd-flex align-items-center justify-content-between flex-wrap gap-2 mb-2 px-2 py-2';
    bar.innerHTML = `
        <div class="d-flex align-items-center gap-2 flex-grow-1" style="max-width:400px">
            <input type="search" id="linksSearchInput" class="form-control form-control-sm" placeholder="Поиск…">
        </div>
        <div class="d-flex align-items-center gap-2 flex-wrap">
            <div class="d-flex gap-1">
                <button id="linksExpCsv" class="btn btn-sm btn-outline-secondary" title="CSV"><i class="bi bi-filetype-csv"></i> CSV</button>
                <button id="linksExpXls" class="btn btn-sm btn-outline-secondary" title="Excel"><i class="bi bi-file-earmark-spreadsheet"></i> Excel</button>
                <button id="linksExpPdf" class="btn btn-sm btn-outline-secondary" title="PDF"><i class="bi bi-filetype-pdf"></i> PDF</button>
            </div>
            <span id="linksPagInfo" class="text-muted small"></span>
            <button id="linksPagPrev" class="btn btn-sm btn-outline-secondary">&laquo;</button>
            <button id="linksPagNext" class="btn btn-sm btn-outline-secondary">&raquo;</button>
            <select id="linksPageSize" class="form-select form-select-sm" style="width:auto">
                <option value="25">25</option>
                <option value="50" selected>50</option>
                <option value="100">100</option>
            </select>
            <span class="text-muted small text-nowrap">на стр.</span>
        </div>`;
    tbl.parentNode.insertBefore(bar, tbl);

    document.getElementById('linksPagPrev').onclick = () => { if (_linksPage > 0) { _linksPage--; renderLinksPage(); } };
    document.getElementById('linksPagNext').onclick = () => {
        const total = getFilteredPrices().length;
        if ((_linksPage + 1) * _linksPageSize < total) { _linksPage++; renderLinksPage(); }
    };
    document.getElementById('linksSearchInput').oninput = (e) => {
        _linksSearch = e.target.value.trim();
        _linksPage = 0;
        renderLinksPage();
    };
    document.getElementById('linksPageSize').onchange = (e) => {
        _linksPageSize = parseInt(e.target.value, 10);
        _linksPage = 0;
        renderLinksPage();
    };
    document.getElementById('linksExpCsv').onclick = () => linksExport('csv');
    document.getElementById('linksExpXls').onclick = () => linksExport('excel');
    document.getElementById('linksExpPdf').onclick = () => linksExport('pdf');
}

function renderLinksPage() {
    const tbody = document.getElementById('pricesTableBody');
    if (!tbody) return;
    const filtered = getFilteredPrices();
    if (filtered.length === 0) {
        tbody.innerHTML = '<tr><td colspan="8" class="empty-row">Нет данных</td></tr>';
        updateLinksPagInfo(0, 0, 0);
        return;
    }
    const start = _linksPage * _linksPageSize;
    const end = Math.min(start + _linksPageSize, filtered.length);
    tbody.innerHTML = renderPriceRows(filtered.slice(start, end));
    updateLinksPagInfo(start + 1, end, filtered.length);
}

function updateLinksPagInfo(from, to, total) {
    const el = document.getElementById('linksPagInfo');
    if (el) el.textContent = total > 0 ? `${from}–${to} из ${total.toLocaleString('ru-RU')}` : '';
    const prev = document.getElementById('linksPagPrev');
    const next = document.getElementById('linksPagNext');
    if (prev) prev.disabled = _linksPage === 0;
    if (next) next.disabled = to >= total;
}

async function loadPrices() {
    const gen = ++_linksLoadGen;
    const supplierId = document.getElementById('filterSupplier')?.value || '';
    const tbody = document.getElementById('pricesTableBody');
    const matchedEl = document.getElementById('matchedCount');
    const unmatchedEl = document.getElementById('unmatchedCount');
    const totalEl = document.getElementById('totalCount');
    const info = document.getElementById('pricesInfo');

    if (!supplierId) {
        if (tbody) tbody.innerHTML = '<tr><td colspan="8" class="empty-row">Выберите поставщика</td></tr>';
        [matchedEl, unmatchedEl, totalEl].forEach(el => { if (el) el.textContent = '-'; });
        if (info) info.textContent = '';
        return;
    }

    if (tbody) tbody.innerHTML = '<tr><td colspan="8" class="loading">Загрузка прайсов...</td></tr>';
    [matchedEl, unmatchedEl, totalEl].forEach(el => {
        if (el) el.innerHTML = '<span class="spinner-border spinner-border-sm"></span>';
    });

    try {
        const response = await API.get(`/api/supplier-prices?supplier_id=${supplierId}&include_inactive=true&limit=${LINKS_FIRST_BATCH}`);
        if (gen !== _linksLoadGen) return;
        if (!response) throw new Error('Нет ответа от сервера');

        currentPrices = response.prices || [];
        const stats = response.stats || {};
        const totalDB = stats.total_in_db || 0;

        if (matchedEl) matchedEl.textContent = stats.matched_final || 0;
        if (unmatchedEl) unmatchedEl.textContent = stats.unmatched_final || 0;
        if (totalEl) totalEl.textContent = totalDB || currentPrices.length;

        rebuildLinksTable();

        if (info) {
            if (currentPrices.length < totalDB) {
                info.innerHTML = `Загружено ${currentPrices.length.toLocaleString('ru-RU')} из ${totalDB.toLocaleString('ru-RU')} <span class="spinner-border spinner-border-sm ms-1"></span>`;
            } else {
                info.textContent = `Всего ${totalDB.toLocaleString('ru-RU')} позиций`;
            }
        }

        if (currentPrices.length < totalDB) {
            loadPricesRemaining(gen, supplierId, currentPrices.length, totalDB);
        }
    } catch (error) {
        if (gen !== _linksLoadGen) return;
        console.error('Ошибка загрузки прайсов:', error);
        if (tbody) tbody.innerHTML = '<tr><td colspan="8" class="empty-row" style="color:red">Ошибка загрузки</td></tr>';
        [matchedEl, unmatchedEl, totalEl].forEach(el => { if (el) el.textContent = '-'; });
    }
}

async function loadPricesRemaining(gen, supplierId, loaded, totalDB) {
    const info = document.getElementById('pricesInfo');
    const BATCH = 2000;
    let offset = loaded;
    try {
        while (true) {
            const response = await API.get(`/api/supplier-prices?supplier_id=${supplierId}&include_inactive=true&limit=${BATCH}&offset=${offset}`);
            if (gen !== _linksLoadGen) return;
            if (!response) throw new Error('Нет ответа от сервера');
            const more = response.prices || [];
            if (more.length === 0) break;
            currentPrices = currentPrices.concat(more);
            offset += more.length;
            if (info) {
                info.innerHTML = `Загружено ${currentPrices.length.toLocaleString('ru-RU')} из ${totalDB.toLocaleString('ru-RU')} <span class="spinner-border spinner-border-sm ms-1"></span>`;
            }
            if (more.length < BATCH) break;
        }
        if (gen !== _linksLoadGen) return;
        rebuildLinksTable();
        if (info) info.textContent = `Всего ${currentPrices.length.toLocaleString('ru-RU')} позиций`;
    } catch (e) {
        if (gen !== _linksLoadGen) return;
        console.error('Ошибка фоновой дозагрузки:', e);
        if (info) info.textContent = `Загружено ${currentPrices.length.toLocaleString('ru-RU')} позиций (ошибка загрузки)`;
    }
}

function editMatch(priceID) {
    const price = currentPrices.find(p => p.supplier_price_id === priceID);
    if (!price) { Toast.error('Ошибка', 'Прайс не найден'); return; }

    currentEditingPrice = { ...price };

    document.getElementById('modalSupplierItem').innerHTML = `
        <div class="mb-2"><strong class="fs-6">${escapeHtml(price.supplier_item_name || '-')}</strong></div>
        <div class="row g-2">
            <div class="col-6"><small class="text-muted">Код:</small><br>${escapeHtml(price.supplier_item_code || '-')}</div>
            <div class="col-6"><small class="text-muted">Штрихкод:</small><br>${escapeHtml(price.barcode || '-')}</div>
        </div>
        ${price.price ? '<div class="mt-2"><small class="text-muted">Цена:</small> <strong class="text-primary">' + parseFloat(price.price).toFixed(2) + ' \u20BD</strong></div>' : ''}
    `;

    const currentMatchInfo = document.getElementById('currentMatchInfo');
    const currentMatchDetails = document.getElementById('currentMatchDetails');
    if (price.guid_es) {
        currentMatchInfo.style.display = 'block';
        currentMatchDetails.innerHTML = `
            <div><strong class="text-success">${escapeHtml(price.drug_name || '-')}</strong></div>
            ${price.inn ? '<div class="mt-1"><small>МНН:</small> ' + escapeHtml(price.inn) + '</div>' : ''}
            ${price.cure_form ? '<div><small>Форма:</small> ' + escapeHtml(price.cure_form) + '</div>' : ''}
            <div class="mt-2 small text-muted">GUID: ${price.guid_es}</div>
        `;
    } else {
        currentMatchInfo.style.display = 'none';
    }

    document.getElementById('modalMatchMethod').value = price.match_method || 'MANUAL';
    document.getElementById('modalMatchConfidence').value = price.match_confidence || 100;
    document.getElementById('modalDrugSearch').value = '';
    document.getElementById('drugSearchResults').style.display = 'none';

    if (!window._matchModal) window._matchModal = new bootstrap.Modal(document.getElementById('matchModal'));
    window._matchModal.show();
}

function closeMatchModal() {
    if (window._matchModal) window._matchModal.hide();
    currentEditingPrice = null;
}

async function searchDrugs() {
    const query = document.getElementById('modalDrugSearch')?.value.trim();
    if (!query || query.length < 2) { Toast.warning('Внимание', 'Введите минимум 2 символа'); return; }

    const resultsDiv = document.getElementById('drugSearchResults');
    const resultsList = document.getElementById('drugSearchResultsList');
    resultsDiv.style.display = 'block';
    resultsList.innerHTML = '<div class="p-3 text-center">Поиск...</div>';

    try {
        const response = await API.get(`/api/drugs/search?q=${encodeURIComponent(query)}`);
        const drugs = response?.drugs || [];

        if (drugs.length === 0) {
            resultsList.innerHTML = '<div class="p-3 text-center text-muted">Препараты не найдены</div>';
            return;
        }

        window.searchResultsDrugs = drugs;
        resultsList.innerHTML = drugs.map((drug, idx) => `
            <div class="search-result-item p-2 border-bottom" onclick="selectDrug(${idx})" style="cursor:pointer">
                <div class="d-flex justify-content-between align-items-start">
                    <div>
                        <strong>${escapeHtml(drug.name)}</strong><br>
                        ${drug.inn ? '<small class="text-muted">МНН: ' + escapeHtml(drug.inn) + '</small><br>' : ''}
                        ${drug.cure_form ? '<small class="text-muted">Форма: ' + escapeHtml(drug.cure_form) + '</small><br>' : ''}
                        ${drug.producer_name ? '<small class="text-muted">Производитель: ' + escapeHtml(drug.producer_name) + '</small><br>' : ''}
                        ${drug.barcode ? '<small class="text-muted">Штрихкод: ' + drug.barcode + '</small>' : ''}
                    </div>
                    <button class="btn btn-sm btn-success" onclick="event.stopPropagation(); selectDrug(${idx})">Выбрать</button>
                </div>
            </div>
        `).join('');
    } catch (error) {
        console.error('Ошибка поиска:', error);
        resultsList.innerHTML = '<div class="p-3 text-center text-danger">Ошибка поиска</div>';
    }
}

function selectDrug(index) {
    const drug = window.searchResultsDrugs?.[index];
    if (!drug || !currentEditingPrice) return;

    currentEditingPrice.selectedDrug = { guid_es: drug.guid_es, name: drug.name, inn: drug.inn, cure_form: drug.cure_form };

    const currentMatchInfo = document.getElementById('currentMatchInfo');
    const currentMatchDetails = document.getElementById('currentMatchDetails');
    currentMatchInfo.style.display = 'block';
    currentMatchDetails.innerHTML = `
        <div><strong class="text-success">${escapeHtml(drug.name)}</strong></div>
        ${drug.inn ? '<div class="mt-1"><small>МНН:</small> ' + escapeHtml(drug.inn) + '</div>' : ''}
        ${drug.cure_form ? '<div><small>Форма:</small> ' + escapeHtml(drug.cure_form) + '</div>' : ''}
        <div class="mt-2 small text-muted">GUID: ${drug.guid_es}</div>
    `;

    document.getElementById('drugSearchResults').style.display = 'none';
    document.getElementById('modalDrugSearch').value = drug.name;
}

async function saveMatch() {
    if (!currentEditingPrice) { Toast.error('Ошибка', 'Не выбран прайс'); return; }

    const priceID = currentEditingPrice.supplier_price_id;
    if (!priceID) { Toast.error('Ошибка', 'Некорректный ID прайса'); return; }

    const guidES = currentEditingPrice.selectedDrug?.guid_es || currentEditingPrice.guid_es;
    const matchMethod = document.getElementById('modalMatchMethod').value;
    const matchConfidence = parseFloat(document.getElementById('modalMatchConfidence').value);

    if (!guidES) { Toast.warning('Внимание', 'Выберите препарат из справочника'); return; }

    const saveBtn = document.getElementById('saveMatchBtn');
    const originalHTML = saveBtn.innerHTML;
    saveBtn.innerHTML = '<span class="spinner-border spinner-border-sm"></span> Сохранение...';
    saveBtn.disabled = true;

    try {
        const response = await API.put(`/api/supplier-prices/${priceID}/match`, {
            guid_es: guidES,
            match_method: matchMethod,
            match_confidence: matchConfidence
        });

        if (response) {
            Toast.success('Сохранено', 'Сопоставление сохранено');
            closeMatchModal();
            await loadPrices();
        } else {
            throw new Error('Ошибка сохранения');
        }
    } catch (error) {
        console.error('Ошибка сохранения:', error);
        Toast.error('Ошибка сохранения', error.message);
    } finally {
        saveBtn.innerHTML = originalHTML;
        saveBtn.disabled = false;
    }
}

async function clearMatch() {
    if (!currentEditingPrice) { Toast.error('Ошибка', 'Не выбран прайс'); return; }
    if (!confirm('Очистить сопоставление?')) return;

    const priceID = currentEditingPrice.supplier_price_id;
    try {
        const response = await API.put(`/api/supplier-prices/${priceID}/match`, {
            guid_es: '', match_method: null, match_confidence: null
        });
        if (response) {
            Toast.success('Очищено', 'Сопоставление очищено');
            closeMatchModal();
            await loadPrices();
        }
    } catch (error) {
        console.error('Ошибка очистки:', error);
        Toast.error('Ошибка', error.message);
    }
}

function linksExport(fmt) {
    const items = getFilteredPrices();
    const headers = ['Товар поставщика', 'Код', 'Штрихкод', 'Цена', 'Статус', 'Препарат из справочника', 'МНН', 'Метод'];
    const rows = items.map(p => [
        p.supplier_item_name || '', p.supplier_item_code || '', p.barcode || '',
        p.price ? Number(p.price).toFixed(2) : '',
        (p.guid_es && p.guid_es.trim()) ? 'Сопоставлено' : 'Не сопоставлено',
        p.drug_name || '', p.inn || '', p.match_method || ''
    ]);
    const name = 'Связки';
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

function escapeHtml(text) {
    if (text == null) return '';
    const div = document.createElement('div');
    div.textContent = String(text);
    return div.innerHTML;
}

window.selectDrug = selectDrug;
window.closeMatchModal = closeMatchModal;
