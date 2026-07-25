// Редактирование прайса
let currentPriceId = null;
let targetFieldsList = [];
let currentImportPointId = null;
let importPointsCache = [];
let lastUploadedFilePath = null;

document.addEventListener('DOMContentLoaded', async () => {
    const urlParams = new URLSearchParams(window.location.search);
    currentPriceId = urlParams.get('id');
    const supplierId = urlParams.get('supplier_id');
    
    await loadSuppliers(supplierId);
    await loadAccessPoints();
    await loadTargetFields();
    initScheduleBuilder();

    const supplierSelect = document.getElementById('supplier');
    supplierSelect.addEventListener('change', async () => {
        await loadRegionsForSupplier(supplierSelect.value);
    });

    const accessPointSelect = document.getElementById('accessPoint');
    accessPointSelect.addEventListener('change', () => {
        onImportPointChange(accessPointSelect.value);
    });

    if (currentPriceId) {
        document.getElementById('pageTitle').textContent = 'Редактирование прайса';
        await loadPriceList(currentPriceId);
    } else if (supplierId) {
        await loadRegionsForSupplier(supplierId);
    } else {
        showRegionsPlaceholder();
    }
    
    document.getElementById('priceForm').addEventListener('submit', handleSubmit);
});

// ── Schedule Builder ──

function initScheduleBuilder() {
    const hourSel = document.getElementById('schedHour');
    const minSel = document.getElementById('schedMinute');
    for (let h = 0; h < 24; h++) hourSel.add(new Option(String(h).padStart(2, '0'), h));
    for (let m = 0; m < 60; m += 5) minSel.add(new Option(String(m).padStart(2, '0'), m));
    hourSel.value = 10;
    minSel.value = 0;

    const freq = document.getElementById('schedFrequency');
    freq.addEventListener('change', onScheduleChange);
    hourSel.addEventListener('change', onScheduleChange);
    minSel.addEventListener('change', onScheduleChange);
    document.getElementById('schedInterval').addEventListener('change', onScheduleChange);
    document.querySelectorAll('#schedDaysBlock input').forEach(cb => cb.addEventListener('change', onScheduleChange));

    onScheduleChange();
}

function onScheduleChange() {
    const freq = document.getElementById('schedFrequency').value;
    const timeBlock = document.getElementById('schedTimeBlock');
    const intervalBlock = document.getElementById('schedIntervalBlock');
    const daysBlock = document.getElementById('schedDaysBlock');
    const preview = document.getElementById('schedPreview');

    timeBlock.style.display = (freq === 'daily' || freq === 'weekdays' || freq === 'custom_days') ? 'flex' : 'none';
    intervalBlock.style.display = (freq === 'interval') ? 'flex' : 'none';
    daysBlock.style.display = (freq === 'custom_days') ? 'flex' : 'none';

    const cron = buildCron();
    document.getElementById('updateSchedule').value = cron;
    preview.textContent = cron ? describeCron(cron) : '';
}

function buildCron() {
    const freq = document.getElementById('schedFrequency').value;
    if (!freq) return '';

    const h = document.getElementById('schedHour').value;
    const m = document.getElementById('schedMinute').value;

    switch (freq) {
        case 'daily':
            return `${m} ${h} * * *`;
        case 'weekdays':
            return `${m} ${h} * * 1-5`;
        case 'custom_days': {
            const days = Array.from(document.querySelectorAll('#schedDaysBlock input:checked')).map(c => c.value);
            if (days.length === 0) return '';
            return `${m} ${h} * * ${days.join(',')}`;
        }
        case 'interval': {
            const iv = document.getElementById('schedInterval').value;
            if (iv === '5m') return '*/5 * * * *';
            if (iv === '90m') return '@every 1h30m';
            return `0 */${iv} * * *`;
        }
        default:
            return '';
    }
}

function describeCron(cron) {
    if (!cron) return '';
    if (cron === '@every 1h30m') return 'Каждые 1 ч 30 мин';
    if (cron.startsWith('@every ')) {
        return 'Каждые ' + cron.slice(7);
    }

    const parts = cron.split(' ');
    if (parts.length !== 5) return cron;
    const [min, hour, , , dow] = parts;

    if (min.startsWith('*/') && hour === '*') return `Каждые ${min.slice(2)} мин.`;
    if (hour.startsWith('*/')) return `Каждые ${hour.slice(2)} ч.`;

    const time = `${String(hour).padStart(2, '0')}:${String(min).padStart(2, '0')}`;
    if (dow === '*') return `Ежедневно в ${time}`;
    if (dow === '1-5') return `Пн-Пт в ${time}`;

    const dayNames = {'0':'Вс','1':'Пн','2':'Вт','3':'Ср','4':'Чт','5':'Пт','6':'Сб'};
    const dayList = dow.split(',').map(d => dayNames[d] || d).join(', ');
    return `${dayList} в ${time}`;
}

function setCronToUI(cron) {
    if (!cron) {
        document.getElementById('schedFrequency').value = '';
        onScheduleChange();
        return;
    }

    if (cron === '@every 1h30m') {
        document.getElementById('schedFrequency').value = 'interval';
        document.getElementById('schedInterval').value = '90m';
        onScheduleChange();
        return;
    }

    const parts = cron.split(' ');
    if (parts.length !== 5) {
        document.getElementById('schedFrequency').value = '';
        onScheduleChange();
        return;
    }

    const [min, hour, , , dow] = parts;

    if (min.startsWith('*/') && hour === '*') {
        document.getElementById('schedFrequency').value = 'interval';
        document.getElementById('schedInterval').value = min.slice(2) + 'm';
    } else if (hour.startsWith('*/')) {
        document.getElementById('schedFrequency').value = 'interval';
        document.getElementById('schedInterval').value = hour.slice(2);
    } else {
        document.getElementById('schedHour').value = parseInt(hour, 10);
        document.getElementById('schedMinute').value = parseInt(min, 10);

        if (dow === '*') {
            document.getElementById('schedFrequency').value = 'daily';
        } else if (dow === '1-5') {
            document.getElementById('schedFrequency').value = 'weekdays';
        } else {
            document.getElementById('schedFrequency').value = 'custom_days';
            const days = dow.split(',');
            document.querySelectorAll('#schedDaysBlock input').forEach(cb => {
                cb.checked = days.includes(cb.value);
            });
        }
    }

    onScheduleChange();
}

// ── Data Loading ──

function showRegionsPlaceholder() {
    const container = document.getElementById('regionsCheckboxes');
    if (container) container.innerHTML = '<p>Сначала выберите поставщика</p>';
}

async function loadRegionsForSupplier(supplierId) {
    const container = document.getElementById('regionsCheckboxes');
    if (!container) return;

    if (!supplierId) {
        showRegionsPlaceholder();
        return;
    }

    try {
        const resp = await API.get(`/api/suppliers/${supplierId}/regions`);
        const regions = resp?.regions || [];
        if (regions.length > 0) {
            const sorted = regions.sort((a, b) =>
                (a.region_name || '').localeCompare(b.region_name || '', 'ru')
            );
            container.innerHTML = sorted.map(region => `
                <label class="checkbox-label">
                    <input type="checkbox" name="regions" value="${region.region_id || ''}">
                    <span>${escapeHtml(region.region_name || '-')}</span>
                </label>
            `).join('');
        } else {
            container.innerHTML = '<p>У поставщика нет назначенных регионов</p>';
        }
    } catch (error) {
        container.innerHTML = '<p>Ошибка загрузки регионов поставщика</p>';
    }
}

function sortRegionCheckboxes() {
    const container = document.getElementById('regionsCheckboxes');
    if (!container) return;
    const labels = Array.from(container.querySelectorAll('.checkbox-label'));
    if (labels.length === 0) return;

    labels.sort((a, b) => {
        const aChecked = a.querySelector('input').checked ? 0 : 1;
        const bChecked = b.querySelector('input').checked ? 0 : 1;
        if (aChecked !== bChecked) return aChecked - bChecked;
        return a.textContent.trim().localeCompare(b.textContent.trim(), 'ru');
    });

    labels.forEach(l => container.appendChild(l));
}

async function loadPriceList(id) {
    try {
        const response = await API.get('/api/price-lists');
        const priceLists = response?.price_lists || [];
        
        if (priceLists.length > 0) {
            const price = priceLists.find(p => p.price_list_id === id);
            if (price) {
                document.getElementById('name').value = price.name || '';
                document.getElementById('supplier').value = price.supplier_id || '';
                document.getElementById('accessPoint').value = price.import_point_id || '';
                document.getElementById('markup').value = price.default_markup_pct || '';
                setCronToUI(price.schedule_cron || '');

                await loadRegionsForSupplier(price.supplier_id);

                if (price.import_point_id) {
                    onImportPointChange(price.import_point_id);
                }

                loadLinksStats(id, price.supplier_id);
            }
        }

        const regionsResp = await API.get(`/api/price-lists/${id}/regions`);
        const regions = regionsResp?.regions || [];
        if (regions.length > 0) {
            const regionIds = new Set(regions.map(r => (r.region_id || '').toUpperCase()));
            document.querySelectorAll('#regionsCheckboxes input[type="checkbox"]').forEach(cb => {
                if (regionIds.has((cb.value || '').toUpperCase())) cb.checked = true;
            });
        }
        sortRegionCheckboxes();
    } catch (error) {
        console.error('Ошибка загрузки прайса:', error);
    }
}

async function loadSuppliers(selectedId) {
    const select = document.getElementById('supplier');
    try {
        const data = await API.get('/api/suppliers');
        if (data && data.length > 0) {
            select.innerHTML = '<option value="">Выберите поставщика</option>' +
                data.map(s => `<option value="${s.supplier_id}" ${s.supplier_id == selectedId ? 'selected' : ''}>${escapeHtml(s.name)}</option>`).join('');
        }
    } catch (error) {
        console.error('Ошибка загрузки поставщиков:', error);
    }
}

async function loadAccessPoints() {
    const select = document.getElementById('accessPoint');
    if (!select) return;
    
    try {
        const data = await API.get('/api/import-points');
        importPointsCache = data || [];
        if (data && data.length > 0) {
            select.innerHTML = '<option value="">Выберите точку доступа</option>' +
                data.map(p => `<option value="${p.import_point_id}">${escapeHtml(p.name)}</option>`).join('');
        }
    } catch (error) {
        console.error('Ошибка загрузки точек доступа:', error);
    }
}

function selectAllRegions() {
    document.querySelectorAll('#regionsCheckboxes input[type="checkbox"]').forEach(cb => cb.checked = true);
    sortRegionCheckboxes();
}

async function handleSubmit(e) {
    e.preventDefault();
    
    const regionCheckboxes = document.querySelectorAll('#regionsCheckboxes input[type="checkbox"]:checked');
    const regionIds = Array.from(regionCheckboxes).map(cb => cb.value).filter(v => v);

    const formData = {
        name: document.getElementById('name').value,
        supplier_id: document.getElementById('supplier').value,
        import_point_id: document.getElementById('accessPoint').value || null,
        default_markup_pct: parseFloat(document.getElementById('markup').value) || 0,
        schedule_cron: document.getElementById('updateSchedule').value || null,
        region_ids: regionIds,
        is_active: true
    };
    
    try {
        if (currentPriceId) {
            await API.put(`/api/price-lists/${currentPriceId}`, formData);
        } else {
            await API.post('/api/price-lists/create', formData);
        }
        
        // Сохраняем маппинг, если секция видна и есть строки
        if (currentImportPointId && document.getElementById('mappingSection').style.display !== 'none') {
            await saveAllMappings(true);
        }

        Toast.success('Сохранено', currentPriceId ? 'Прайс обновлён' : 'Прайс создан');
        setTimeout(() => { window.location.href = 'price-lists.html'; }, 800);
    } catch (error) {
        Toast.error('Ошибка сохранения', error.message);
    }
}

// ── Mapping ──

async function loadTargetFields() {
    try {
        const resp = await API.get('/api/target-fields');
        targetFieldsList = resp?.target_fields || [];
    } catch (e) {
        console.error('Ошибка загрузки целевых полей:', e);
    }
}

function onImportPointChange(importPointId) {
    currentImportPointId = importPointId;
    const mappingSection = document.getElementById('mappingSection');
    const importSection = document.getElementById('importSection');
    if (!importPointId) {
        mappingSection.style.display = 'none';
        if (importSection) importSection.style.display = 'none';
        return;
    }
    mappingSection.style.display = '';
    if (importSection) importSection.style.display = '';
    loadMappings(importPointId);
    loadImportHistory(importPointId);
}

async function loadImportHistory(importPointId) {
    const container = document.getElementById('importHistory');
    if (!container) return;

    try {
        const data = await API.get(`/api/invoice-imports?import_point_id=${importPointId}`);
        const imports = data?.imports || [];
        if (imports.length === 0) {
            container.innerHTML = '<p class="text-muted small mb-0">Импорты ещё не выполнялись</p>';
            return;
        }
        const last5 = imports.slice(0, 5);
        container.innerHTML = '<h6 class="mt-2 mb-2">Последние импорты</h6>' +
            '<div class="table-responsive"><table class="table table-sm table-hover mb-0">' +
            '<thead><tr><th>Дата</th><th>Файл</th><th>Статус</th><th>Записей</th><th>Обработано</th><th>Ошибок</th></tr></thead><tbody>' +
            last5.map(imp => {
                const date = imp.started_at || imp.created_at || '-';
                const statusBadge = imp.import_status === 'COMPLETED' ? 'bg-success' :
                    imp.import_status === 'PROCESSING' ? 'bg-warning' : 'bg-danger';
                const statusText = imp.import_status === 'COMPLETED' ? 'Готово' :
                    imp.import_status === 'PROCESSING' ? 'В процессе' : 'Ошибка';
                return `<tr>
                    <td class="small">${date}</td>
                    <td class="small">${escapeHtml(imp.file_name || '-')}</td>
                    <td><span class="badge ${statusBadge}">${statusText}</span></td>
                    <td>${imp.records_total || 0}</td>
                    <td>${imp.records_processed || 0}</td>
                    <td>${imp.records_error || 0}</td>
                </tr>`;
            }).join('') +
            '</tbody></table></div>';
    } catch (e) {
        container.innerHTML = '<p class="text-muted small mb-0">Не удалось загрузить историю импортов</p>';
    }
}

function runImportNow() {
    if (!currentImportPointId) {
        Toast.warning('Внимание', 'Сначала выберите точку доступа');
        return;
    }

    if (lastUploadedFilePath) {
        doImport(lastUploadedFilePath);
    } else {
        const input = document.getElementById('importFileInput');
        input.click();
    }
}

async function uploadAndImport(input) {
    const file = input.files[0];
    if (!file) return;
    input.value = '';

    if (!currentImportPointId) {
        Toast.warning('Внимание', 'Сначала выберите точку доступа');
        return;
    }

    const btn = document.getElementById('btnImportNow');
    btn.disabled = true;
    btn.innerHTML = '<span class="spinner-border spinner-border-sm"></span> Загрузка...';

    try {
        const formData = new FormData();
        formData.append('file', file);
        const uploadResult = await API.upload('/api/dbf/upload', formData);
        if (!uploadResult?.file_path) {
            Toast.error('Ошибка', 'Не удалось загрузить файл');
            return;
        }
        lastUploadedFilePath = uploadResult.file_path;
        await doImport(uploadResult.file_path);
    } catch (e) {
        Toast.error('Ошибка', e.message);
    } finally {
        btn.disabled = false;
        btn.innerHTML = '<i class="bi bi-play-fill"></i> Импортировать сейчас';
    }
}

async function doImport(filePath) {
    const hasUnsavedMapping = document.querySelectorAll('#mappingTableBody tr .mapping-target').length > 0;
    const savedMappings = await API.get(`/api/field-mappings?import_point_id=${currentImportPointId}`).catch(() => []);
    const hasSaved = (savedMappings?.length || 0) > 0;

    if (hasUnsavedMapping) {
        const saveFirst = await Toast.confirm('Маппинг', 'Сохранить текущий маппинг перед импортом?');
        if (saveFirst) {
            await saveAllMappings(true);
        } else if (!hasSaved) {
            Toast.error('Импорт невозможен', 'Маппинг полей не сохранён. Сначала сохраните маппинг.');
            return;
        }
    } else if (!hasSaved) {
        Toast.error('Импорт невозможен', 'Маппинг полей не настроен. Сначала загрузите образец и сохраните маппинг.');
        return;
    }

    const btn = document.getElementById('btnImportNow');
    btn.disabled = true;
    btn.innerHTML = '<span class="spinner-border spinner-border-sm"></span> Импорт...';

    try {
        const result = await API.post('/api/import/file', {
            import_point_id: currentImportPointId,
            file_path: filePath
        });

        if (result?.status === 'import_started') {
            Toast.success('Импорт запущен', 'Файл принят, импорт выполняется в фоновом режиме. Обновите страницу через несколько секунд.');
        } else {
            Toast.success('Импорт завершён', result?.message || 'Готово');
        }
        if (currentPriceId) {
            setTimeout(() => loadLinksStats(currentPriceId, document.getElementById('supplier')?.value), 3000);
        }
    } catch (e) {
        Toast.error('Ошибка импорта', e.message);
    } finally {
        btn.disabled = false;
        btn.innerHTML = '<i class="bi bi-play-fill"></i> Импортировать сейчас';
    }
}

async function loadMappings(importPointId) {
    const tbody = document.getElementById('mappingTableBody');
    try {
        const data = await API.get(`/api/field-mappings?import_point_id=${importPointId}`);
        const mappings = data || [];
        if (mappings.length === 0) {
            tbody.innerHTML = '<tr><td colspan="5" class="empty-row">Нажмите «Загрузить образец» для считывания полей из DBF</td></tr>';
            return;
        }
        tbody.innerHTML = '';
        mappings.forEach(m => {
            addMappingRowWithData(m.dbf_field_name, m.target_field_name, m.data_type, m.is_required);
        });
    } catch (e) {
        tbody.innerHTML = '<tr><td colspan="5" class="empty-row">Ошибка загрузки маппинга</td></tr>';
    }
}

async function analyzeFile() {
    if (!currentImportPointId) {
        Toast.warning('Внимание', 'Сначала выберите точку доступа');
        return;
    }

    const btn = document.getElementById('btnAnalyze');
    btn.disabled = true;
    btn.innerHTML = '<i class="bi bi-hourglass-split"></i> Анализ...';

    try {
        const result = await API.post(`/api/import-points/${currentImportPointId}/analyze`, {});
        populateFieldsFromAnalysis(result);
    } catch (e) {
        Toast.error('Ошибка анализа', e.message);
    } finally {
        btn.disabled = false;
        btn.innerHTML = '<i class="bi bi-folder2-open"></i> Считать с сервера';
    }
}

async function uploadAndAnalyze(input) {
    const file = input.files[0];
    if (!file) return;
    input.value = '';

    if (!currentImportPointId) {
        Toast.warning('Внимание', 'Сначала выберите точку доступа');
        return;
    }

    const btn = document.getElementById('btnUpload');
    btn.disabled = true;
    btn.innerHTML = '<i class="bi bi-hourglass-split"></i> Загрузка...';

    try {
        const formData = new FormData();
        formData.append('file', file);

        const uploadResult = await API.upload('/api/dbf/upload', formData);
        if (!uploadResult?.file_path) {
            Toast.error('Ошибка', 'Не удалось загрузить файл');
            return;
        }

        lastUploadedFilePath = uploadResult.file_path;
        Toast.info('Файл загружен', file.name);

        const result = await API.post('/api/dbf/analyze', { file_path: uploadResult.file_path });
        populateFieldsFromAnalysis(result);
    } catch (e) {
        Toast.error('Ошибка', e.message);
    } finally {
        btn.disabled = false;
        btn.innerHTML = '<i class="bi bi-upload"></i> Загрузить образец';
    }
}

function populateFieldsFromAnalysis(result) {
    const fields = result?.fields || [];
    if (fields.length === 0) {
        Toast.warning('Результат', 'Файл не содержит полей');
        return;
    }

    const existingMappings = collectCurrentMappings();
    const tbody = document.getElementById('mappingTableBody');
    tbody.innerHTML = '';

    const usedTargets = new Set();

    fields.forEach(f => {
        const existing = existingMappings.find(m => m.dbf === f.name);
        let guessedTarget = existing ? existing.target : guessTargetField(f.name);

        if (guessedTarget && usedTargets.has(guessedTarget)) {
            guessedTarget = '';
        }
        if (guessedTarget) usedTargets.add(guessedTarget);

        const guessedType = existing ? existing.type : dbfTypeToDataType(f.type);
        const required = existing ? existing.required : isFieldRequired(guessedTarget);
        addMappingRowWithData(f.name, guessedTarget, guessedType, required);
    });

    Toast.success('Готово', `Считано ${fields.length} полей из файла`);
}

function collectCurrentMappings() {
    const rows = document.querySelectorAll('#mappingTableBody tr');
    const result = [];
    rows.forEach(row => {
        const dbfInput = row.querySelector('.mapping-dbf');
        const targetSelect = row.querySelector('.mapping-target');
        const typeSelect = row.querySelector('.mapping-type');
        const reqCb = row.querySelector('.mapping-required');
        if (dbfInput && targetSelect) {
            result.push({
                dbf: dbfInput.value,
                target: targetSelect.value,
                type: typeSelect?.value || 'NVARCHAR',
                required: reqCb?.checked || false
            });
        }
    });
    return result;
}

function guessTargetField(dbfName) {
    const n = dbfName.toUpperCase();
    const guesses = {
        'NAME': 'item_name', 'TOVAR': 'item_name', 'NAIMEN': 'item_name', 'NAZV': 'item_name',
        'NAIMTOV': 'item_name', 'NAIM': 'item_name', 'PRODUCT': 'item_name', 'NAMETOV': 'item_name',
        'CODE': 'item_code', 'KOD': 'item_code', 'KODTOV': 'item_code', 'CODETOV': 'item_code',
        'ARTICUL': 'item_code', 'ART': 'item_code',
        'BARCODE': 'barcode', 'EAN': 'barcode', 'SHTRIX': 'barcode', 'SHTRIH': 'barcode', 'BAR': 'barcode',
        'PRICE': 'price', 'CENA': 'price', 'COST': 'price', 'TSENA': 'price', 'SUMMA': 'price',
        'QTY': 'quantity', 'QUANTITY': 'quantity', 'KOL': 'quantity', 'KOLVO': 'quantity',
        'OSTATOK': 'quantity', 'OST': 'quantity',
        'SROK': 'expiry_date', 'GODEN': 'expiry_date', 'EXPIRY': 'expiry_date', 'SROKGOD': 'expiry_date',
        'SERIA': 'batch_number', 'BATCH': 'batch_number', 'SERIYA': 'batch_number', 'SERIANUM': 'batch_number',
        'MAKER': 'manufacturer', 'PROIZVOD': 'manufacturer', 'FIRM': 'manufacturer', 'PRODUC': 'manufacturer',
        'IZGTOV': 'manufacturer', 'MANUF': 'manufacturer',
        'COUNTRY': 'country', 'STRANA': 'country',
        'DOCNUM': 'invoice_number', 'NUMDOC': 'invoice_number', 'NUMNAK': 'invoice_number',
        'DOCDATE': 'invoice_date', 'DATDOC': 'invoice_date', 'DATNAK': 'invoice_date',
    };
    for (const [key, val] of Object.entries(guesses)) {
        if (n === key || n.includes(key)) return val;
    }
    return '';
}

function dbfTypeToDataType(dbfType) {
    const map = { 'N': 'DECIMAL', 'F': 'FLOAT', 'D': 'DATE', 'L': 'INT' };
    return map[dbfType] || 'NVARCHAR';
}

function isFieldRequired(targetField) {
    return ['item_code', 'item_name', 'barcode', 'price'].includes(targetField);
}

function buildTargetSelect(selectedValue) {
    let html = '<option value="">-- не маппить --</option>';
    targetFieldsList.forEach(f => {
        const sel = f.name === selectedValue ? ' selected' : '';
        html += `<option value="${f.name}"${sel}>${escapeHtml(f.description)} (${f.name})</option>`;
    });
    return html;
}

function addMappingRow() {
    addMappingRowWithData('', '', 'NVARCHAR', false);
}

function getUsedTargetFields(excludeRow) {
    const used = new Set();
    document.querySelectorAll('#mappingTableBody tr').forEach(row => {
        if (row === excludeRow) return;
        const sel = row.querySelector('.mapping-target');
        if (sel && sel.value) used.add(sel.value);
    });
    return used;
}

function addMappingRowWithData(dbfField, targetField, dataType, isRequired) {
    const tbody = document.getElementById('mappingTableBody');
    const emptyRow = tbody.querySelector('.empty-row');
    if (emptyRow) emptyRow.closest('tr').remove();

    const tr = document.createElement('tr');
    tr.innerHTML = `
        <td><input type="text" class="form-control form-control-sm mapping-dbf" value="${escapeHtml(dbfField)}" placeholder="Имя поля в файле"${dbfField ? ' readonly' : ''}></td>
        <td><select class="form-select form-select-sm mapping-target">${buildTargetSelect(targetField)}</select></td>
        <td>
            <select class="form-select form-select-sm mapping-type">
                <option value="NVARCHAR"${dataType === 'NVARCHAR' ? ' selected' : ''}>Текст</option>
                <option value="DECIMAL"${dataType === 'DECIMAL' ? ' selected' : ''}>Число</option>
                <option value="FLOAT"${dataType === 'FLOAT' ? ' selected' : ''}>Дробное</option>
                <option value="INT"${dataType === 'INT' ? ' selected' : ''}>Целое</option>
                <option value="DATE"${dataType === 'DATE' ? ' selected' : ''}>Дата</option>
                <option value="DATETIME"${dataType === 'DATETIME' ? ' selected' : ''}>Дата+время</option>
            </select>
        </td>
        <td style="text-align:center"><input type="checkbox" class="form-check-input mapping-required"${isRequired ? ' checked' : ''}></td>
        <td style="text-align:center"><button type="button" class="btn btn-outline-danger btn-sm" onclick="removeMappingRow(this)" title="Удалить"><i class="bi bi-trash"></i></button></td>
    `;

    const targetSelect = tr.querySelector('.mapping-target');
    const typeSelect = tr.querySelector('.mapping-type');
    const reqCb = tr.querySelector('.mapping-required');

    targetSelect.addEventListener('change', () => {
        const newVal = targetSelect.value;
        if (newVal) {
            const used = getUsedTargetFields(tr);
            if (used.has(newVal)) {
                Toast.warning('Дубликат', `Поле «${newVal}» уже сопоставлено с другой колонкой`);
                targetSelect.value = '';
                return;
            }
        }
        const tf = targetFieldsList.find(f => f.name === newVal);
        if (tf) typeSelect.value = tf.type;
        reqCb.checked = isFieldRequired(newVal);
    });

    tbody.appendChild(tr);
}

function removeMappingRow(btn) {
    btn.closest('tr').remove();
    const tbody = document.getElementById('mappingTableBody');
    if (tbody.children.length === 0) {
        tbody.innerHTML = '<tr><td colspan="5" class="empty-row">Нет маппинга</td></tr>';
    }
}

async function loadLinksStats(priceListId, supplierId) {
    const section = document.getElementById('linksSection');
    if (!section || !priceListId) return;
    section.style.display = '';

    const btnView = document.getElementById('btnViewPrice');
    const btnLinks = document.getElementById('btnGoLinks');
    if (btnView) btnView.href = `price-view.html?id=${priceListId}`;
    if (btnLinks && supplierId) btnLinks.href = `links.html?supplier=${supplierId}`;

    const container = document.getElementById('linksStats');
    try {
        const data = await API.get(`/api/price-lists/${priceListId}/items?stats_only=true`);
        if (!data || !data.stats) {
            container.innerHTML = '<p class="text-muted small mb-0">Нет данных о сопоставлении. Импортируйте прайс, чтобы увидеть статистику.</p>';
            return;
        }
        const stats = data.stats;
        const total = stats.total || 0;
        const matched = stats.matched || 0;
        const unmatched = stats.unmatched || 0;
        const pct = total > 0 ? (matched / total * 100).toFixed(1) : 0;

        if (total === 0) {
            container.innerHTML = '<p class="text-muted small mb-0">Позиции прайса ещё не импортированы. Используйте кнопку «Импортировать сейчас» выше.</p>';
            return;
        }

        container.innerHTML = `
            <div class="row text-center g-2 mb-2">
                <div class="col-4"><div class="border rounded p-2"><div class="fs-4 fw-bold text-primary">${total.toLocaleString('ru-RU')}</div><small class="text-muted">Всего позиций</small></div></div>
                <div class="col-4"><div class="border rounded p-2"><div class="fs-4 fw-bold text-success">${matched.toLocaleString('ru-RU')}</div><small class="text-muted">Сопоставлено</small></div></div>
                <div class="col-4"><div class="border rounded p-2"><div class="fs-4 fw-bold text-danger">${unmatched.toLocaleString('ru-RU')}</div><small class="text-muted">Не сопоставлено</small></div></div>
            </div>
            <div class="progress" style="height:6px">
                <div class="progress-bar bg-success" style="width:${pct}%"></div>
                <div class="progress-bar bg-danger" style="width:${100 - pct}%"></div>
            </div>
            <div class="text-center mt-1"><small class="text-muted">${pct}% сопоставлено</small></div>`;
    } catch (e) {
        container.innerHTML = '<p class="text-muted small mb-0">Не удалось загрузить статистику связок</p>';
    }
}

async function saveAllMappings(silent) {
    if (!currentImportPointId) {
        if (!silent) Toast.warning('Внимание', 'Сначала выберите точку доступа');
        return;
    }

    const rows = document.querySelectorAll('#mappingTableBody tr');
    const mappingsToSave = [];
    const usedTargets = new Set();

    for (const row of rows) {
        const dbfInput = row.querySelector('.mapping-dbf');
        const targetSelect = row.querySelector('.mapping-target');
        const typeSelect = row.querySelector('.mapping-type');
        const reqCb = row.querySelector('.mapping-required');
        if (!dbfInput || !targetSelect) continue;

        const dbfField = dbfInput.value.trim();
        const targetField = targetSelect.value;
        if (!dbfField) continue;
        if (!targetField) continue;

        if (usedTargets.has(targetField)) {
            Toast.error('Дубликат', `Поле «${targetField}» назначено несколько раз. Исправьте маппинг.`);
            return;
        }
        usedTargets.add(targetField);

        mappingsToSave.push({
            dbf_field_name: dbfField,
            target_field_name: targetField,
            data_type: typeSelect?.value || 'NVARCHAR',
            is_required: reqCb?.checked || false,
            display_order: mappingsToSave.length
        });
    }

    if (mappingsToSave.length === 0) {
        if (!silent) Toast.info('Маппинг', 'Нечего сохранять — не выбрано ни одного целевого поля');
        return;
    }

    try {
        await API.post(`/api/field-mappings/save-all?import_point_id=${currentImportPointId}`, {
            mappings: mappingsToSave
        });
        if (!silent) Toast.success('Маппинг сохранён', `${mappingsToSave.length} полей`);
    } catch (e) {
        if (!silent) Toast.error('Ошибка сохранения маппинга', e.message);
        console.error('Ошибка сохранения маппинга:', e);
    }
}
