// Управление страницей сопоставления прайсов

let currentPrices = [];
let filteredPrices = [];
let currentFilters = {
    supplier_id: '',
    match_status: '',
    search: ''
};
let currentEditingPrice = null;
let currentPage = 1;
let itemsPerPage = 50;
let _matchLoadGen = 0;
const MATCH_FIRST_BATCH = 200;

document.addEventListener('DOMContentLoaded', async () => {
    // Проверка авторизации
    if (!AuthManager.isAuthenticated()) {
        window.location.href = 'login.html';
        return;
    }
    
    // Загружаем данные
    await loadSuppliers();
    
    // Обработчики событий
    document.getElementById('applyFiltersBtn')?.addEventListener('click', applyFilters);
    document.getElementById('clearFiltersBtn')?.addEventListener('click', clearFilters);
    document.getElementById('refreshBtn')?.addEventListener('click', () => loadPrices());
    document.getElementById('saveMatchBtn')?.addEventListener('click', saveMatch);
    document.getElementById('clearMatchBtn')?.addEventListener('click', clearMatch);
    document.getElementById('searchDrugBtn')?.addEventListener('click', searchDrugs);
    document.getElementById('modalDrugSearch')?.addEventListener('keypress', (e) => {
        if (e.key === 'Enter') {
            searchDrugs();
        }
    });
    
    // Пагинация
    const itemsPerPageSelect = document.getElementById('itemsPerPageSelect');
    if (itemsPerPageSelect) {
        itemsPerPageSelect.addEventListener('change', (e) => {
            itemsPerPage = parseInt(e.target.value) || 0;
            currentPage = 1;
            displayFilteredPrices();
        });
    }
    
    // Обработчик для кнопок редактирования (делегирование событий)
    document.addEventListener('click', (e) => {
        if (e.target && e.target.classList.contains('edit-match-btn')) {
            // Получаем priceID из dataset (более надежно) или из атрибута
            let priceID = e.target.dataset?.priceId;
            if (!priceID) {
                priceID = e.target.getAttribute('data-price-id');
            }
            // Принудительно преобразуем в строку
            priceID = String(priceID || '').trim();
            
            console.log('Клик по кнопке редактирования:', {
                priceID: priceID,
                тип: typeof priceID,
                длина: priceID.length,
                элемент: e.target,
                атрибут: e.target.getAttribute('data-price-id'),
                dataset: e.target.dataset?.priceId,
                весьDataset: e.target.dataset
            });
            
            if (priceID && priceID !== '' && priceID !== 'undefined' && priceID !== 'null') {
                editMatch(priceID);
            } else {
                console.error('priceID не найден или пустой:', {
                    priceID: priceID,
                    dataset: e.target.dataset,
                    attribute: e.target.getAttribute('data-price-id')
                });
                showError('Не удалось получить ID прайса');
            }
        }
    });
    
    // Загружаем прайсы
    await loadPrices();
});

async function loadSuppliers() {
    const select = document.getElementById('filterSupplier');
    if (!select) return;
    
    try {
        const response = await AuthManager.authenticatedFetch('/api/suppliers');
        if (response.ok) {
            const data = await response.json();
            const suppliers = Array.isArray(data) ? data : [];
            
            select.innerHTML = '<option value="">Все поставщики</option>';
            suppliers.forEach(supplier => {
                const option = document.createElement('option');
                option.value = supplier.supplier_id;
                option.textContent = supplier.name;
                select.appendChild(option);
            });
        }
    } catch (error) {
        console.error('Ошибка загрузки поставщиков:', error);
    }
}


function applyFilters() {
    currentFilters = {
        supplier_id: document.getElementById('filterSupplier')?.value || '',
        match_status: document.getElementById('filterMatchStatus')?.value || '',
        search: document.getElementById('filterSearch')?.value.trim() || ''
    };
    currentPage = 1; // Сбрасываем страницу при изменении фильтров
    loadPrices();
}

function clearFilters() {
    document.getElementById('filterSupplier').value = '';
    document.getElementById('filterMatchStatus').value = '';
    document.getElementById('filterSearch').value = '';
    currentFilters = {
        supplier_id: '',
        match_status: '',
        search: ''
    };
    currentPage = 1; // Сбрасываем страницу при очистке фильтров
    loadPrices();
}

function applyClientFilters() {
    filteredPrices = currentPrices;
    if (currentFilters.match_status === 'matched') {
        filteredPrices = filteredPrices.filter(p => p.guid_es && p.guid_es.trim() !== '');
    } else if (currentFilters.match_status === 'unmatched') {
        filteredPrices = filteredPrices.filter(p => !p.guid_es || p.guid_es.trim() === '');
    }
    if (currentFilters.search) {
        const searchLower = currentFilters.search.toLowerCase();
        filteredPrices = filteredPrices.filter(p =>
            (p.supplier_item_name && p.supplier_item_name.toLowerCase().includes(searchLower)) ||
            (p.supplier_item_code && p.supplier_item_code.toLowerCase().includes(searchLower)) ||
            (p.drug_name && p.drug_name.toLowerCase().includes(searchLower))
        );
    }
}

async function loadPrices() {
    const gen = ++_matchLoadGen;
    const tbody = document.getElementById('pricesTableBody');
    const info = document.getElementById('pricesInfo');
    if (!tbody) return;

    if (!currentFilters.supplier_id) {
        tbody.innerHTML = '<tr><td colspan="9" style="padding: 20px; text-align: center; color: #666;">Выберите поставщика для просмотра прайсов</td></tr>';
        if (info) info.textContent = '';
        return;
    }

    showElementLoading(tbody, 'Загрузка прайсов...');

    try {
        const params = new URLSearchParams({
            supplier_id: currentFilters.supplier_id,
            include_inactive: 'true',
            limit: MATCH_FIRST_BATCH
        });

        const response = await AuthManager.authenticatedFetch(`/api/supplier-prices?${params}`);
        if (gen !== _matchLoadGen) return;
        if (!response.ok) throw new Error(`Ошибка загрузки прайсов: ${response.status}`);

        const data = await response.json();
        currentPrices = data.prices || [];
        const stats = data.stats || {};
        const totalDB = stats.total_in_db || currentPrices.length;

        applyClientFilters();
        displayFilteredPrices();

        if (info) {
            const matchedInDB = stats.matched_final || 0;
            const unmatchedInDB = stats.unmatched_final || 0;
            if (currentPrices.length < totalDB) {
                info.innerHTML = `Загружено ${currentPrices.length} из ${totalDB} (сопоставлено: ${matchedInDB}, не сопоставлено: ${unmatchedInDB}) <span class="spinner-border spinner-border-sm" style="width:1em;height:1em;"></span>`;
            } else {
                info.textContent = `Показано ${filteredPrices.length} из ${currentPrices.length} (всего в БД: ${totalDB}, сопоставлено: ${matchedInDB}, не сопоставлено: ${unmatchedInDB})`;
            }
        }

        if (currentPrices.length < totalDB) {
            loadMatchPricesRemaining(gen, currentFilters.supplier_id, currentPrices.length, totalDB, stats);
        }
    } catch (error) {
        if (gen !== _matchLoadGen) return;
        console.error('Ошибка загрузки прайсов:', error);
        showError(`Ошибка загрузки прайсов: ${error.message}`);
        tbody.innerHTML = '<tr><td colspan="9" style="padding: 20px; text-align: center; color: #dc3545;">Ошибка загрузки</td></tr>';
    } finally {
        hideElementLoading(tbody);
    }
}

async function loadMatchPricesRemaining(gen, supplierId, loaded, totalDB, stats) {
    const info = document.getElementById('pricesInfo');
    try {
        const params = new URLSearchParams({
            supplier_id: supplierId,
            include_inactive: 'true',
            offset: loaded
        });
        const response = await AuthManager.authenticatedFetch(`/api/supplier-prices?${params}`);
        if (gen !== _matchLoadGen) return;
        if (!response.ok) throw new Error(`HTTP ${response.status}`);

        const data = await response.json();
        const more = data.prices || [];
        if (more.length === 0) return;
        currentPrices = currentPrices.concat(more);

        applyClientFilters();
        displayFilteredPrices();

        const matchedInDB = stats.matched_final || 0;
        const unmatchedInDB = stats.unmatched_final || 0;
        if (info) info.textContent = `Показано ${filteredPrices.length} из ${currentPrices.length} (всего в БД: ${totalDB}, сопоставлено: ${matchedInDB}, не сопоставлено: ${unmatchedInDB})`;
    } catch (e) {
        if (gen !== _matchLoadGen) return;
        console.error('Ошибка фоновой дозагрузки:', e);
        if (info) info.textContent += ' (ошибка дозагрузки)';
    }
}

function displayFilteredPrices() {
    if (!filteredPrices || filteredPrices.length === 0) {
        const tbody = document.getElementById('pricesTableBody');
        if (tbody) {
            tbody.innerHTML = '<tr><td colspan="9" style="padding: 20px; text-align: center; color: #666;">Прайсы не найдены</td></tr>';
        }
        const paginationContainer = document.getElementById('paginationContainer');
        if (paginationContainer) paginationContainer.style.display = 'none';
        return;
    }
    
    // Применяем пагинацию
    let displayData = filteredPrices;
    let totalPages = 1;
    
    if (itemsPerPage > 0) {
        totalPages = Math.ceil(filteredPrices.length / itemsPerPage);
        const start = (currentPage - 1) * itemsPerPage;
        const end = start + itemsPerPage;
        displayData = filteredPrices.slice(start, end);
    }
    
    // Проверяем, что текущая страница не выходит за границы
    if (currentPage > totalPages && totalPages > 0) {
        currentPage = totalPages;
        displayData = filteredPrices.slice((currentPage - 1) * itemsPerPage, currentPage * itemsPerPage);
    }
    
    displayPrices(displayData);
    
    // Отображаем пагинацию
    if (itemsPerPage > 0 && totalPages > 1) {
        displayPagination(totalPages);
    } else {
        const paginationContainer = document.getElementById('paginationContainer');
        if (paginationContainer) paginationContainer.style.display = 'none';
    }
}

function displayPrices(prices) {
    const tbody = document.getElementById('pricesTableBody');
    if (!tbody) return;
    
    tbody.innerHTML = '';
    
    if (prices.length === 0) {
        tbody.innerHTML = '<tr><td colspan="9" style="padding: 20px; text-align: center; color: #666;">Прайсы не найдены</td></tr>';
        return;
    }
    
    prices.forEach(price => {
        const row = document.createElement('tr');
        row.style.borderBottom = '1px solid #ddd';
        
        const matchStatus = price.guid_es ? '✅ Сопоставлено' : '❌ Не сопоставлено';
        const matchMethod = price.match_method || '-';
        const matchConfidence = price.match_confidence ? `${price.match_confidence.toFixed(1)}%` : '-';
        
        // Убеждаемся, что supplier_price_id - строка
        // Проверяем, что это действительно строка, а не объект
        let priceID = price.supplier_price_id;
        if (typeof priceID !== 'string') {
            // Если это объект, пытаемся преобразовать в строку
            if (priceID && typeof priceID === 'object') {
                console.warn('supplier_price_id пришел как объект:', priceID, 'для прайса:', price.supplier_item_name);
                // Пытаемся получить строковое представление
                priceID = JSON.stringify(priceID);
            } else {
                priceID = String(priceID || '');
            }
        }
        priceID = priceID.trim();
        
        // Проверяем, что это валидный UUID
        if (!priceID || priceID.length < 30) {
            console.error('Некорректный priceID:', priceID, 'для прайса:', price.supplier_item_name);
        }
        
        row.innerHTML = `
            <td style="padding: 8px; border: 1px solid #ddd;">
                <strong>${escapeHtml(price.supplier_item_name || '-')}</strong>
            </td>
            <td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(price.supplier_item_code || '-')}</td>
            <td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(price.barcode || '-')}</td>
            <td style="padding: 8px; border: 1px solid #ddd; text-align: right;">
                ${price.price ? price.price.toFixed(2) : '-'} ₽
            </td>
            <td style="padding: 8px; border: 1px solid #ddd;">
                <span class="${price.guid_es ? 'log-level-info' : 'log-level-error'}" style="padding: 4px 8px; border-radius: 4px; font-size: 0.85em;">
                    ${matchStatus}
                </span>
            </td>
            <td style="padding: 8px; border: 1px solid #ddd;">
                ${price.drug_name ? escapeHtml(price.drug_name) : '-'}
                ${price.inn ? `<br><small style="color: #666;">МНН: ${escapeHtml(price.inn)}</small>` : ''}
            </td>
            <td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(matchMethod)}</td>
            <td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(matchConfidence)}</td>
            <td style="padding: 8px; border: 1px solid #ddd;">
                <button data-price-id="${escapeHtml(priceID)}" class="btn btn-sm btn-primary edit-match-btn" style="padding: 5px 10px; font-size: 0.85em;">
                    ✏️ Редактировать
                </button>
            </td>
        `;
        
        // Также сохраняем priceID в dataset элемента напрямую для надежности
        const btn = row.querySelector('.edit-match-btn');
        if (btn) {
            btn.dataset.priceId = priceID;
        }
        
        tbody.appendChild(row);
    });
}

function editMatch(priceID) {
    // Принудительно преобразуем в строку, если это не строка
    if (priceID == null) {
        console.error('priceID is null or undefined');
        showError('Некорректный ID прайса');
        return;
    }
    
    // Преобразуем в строку, даже если это объект
    let priceIDStr = String(priceID);
    
    // Валидация и очистка priceID
    console.log('editMatch вызван с priceID:', {
        priceID: priceID,
        priceIDStr: priceIDStr,
        тип: typeof priceID,
        длина: priceIDStr.length,
        hex: Array.from(priceIDStr).map(c => c.charCodeAt(0).toString(16).padStart(2, '0')).join(' ')
    });
    
    if (!priceIDStr || priceIDStr.trim() === '') {
        console.error('Некорректный priceID (пустая строка):', priceID, 'тип:', typeof priceID);
        showError('Некорректный ID прайса');
        return;
    }
    
    // Убираем возможные пробелы и спецсимволы
    const cleanPriceID = priceIDStr.trim();
    
    console.log('Очищенный priceID:', {
        исходный: priceID,
        очищенный: cleanPriceID,
        длина: cleanPriceID.length,
        hex: Array.from(cleanPriceID).map(c => c.charCodeAt(0).toString(16).padStart(2, '0')).join(' ')
    });
    
    // Проверяем, что это UUID (36 символов с дефисами)
    const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
    if (!uuidPattern.test(cleanPriceID)) {
        console.error('Некорректный формат UUID:', cleanPriceID, 'hex:', Array.from(cleanPriceID).map(c => c.charCodeAt(0).toString(16).padStart(2, '0')).join(' '));
        showError('Некорректный формат ID прайса');
        return;
    }
    
    const price = currentPrices.find(p => p.supplier_price_id === cleanPriceID);
    if (!price) {
        console.error('Прайс не найден для ID:', cleanPriceID);
        console.log('Доступные ID:', currentPrices.map(p => p.supplier_price_id));
        showError('Прайс не найден');
        return;
    }
    
    currentEditingPrice = price;
    // Убеждаемся, что currentEditingPrice содержит правильный ID
    currentEditingPrice.supplier_price_id = cleanPriceID;
    
    // Заполняем модальное окно
    const modalSupplierItem = document.getElementById('modalSupplierItem');
    modalSupplierItem.innerHTML = `
        <div style="margin-bottom: 10px;">
            <strong style="font-size: 1.1em; color: #333;">${escapeHtml(price.supplier_item_name || '-')}</strong>
        </div>
        <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin-top: 10px;">
            <div style="padding: 8px; background: #f8f9fa; border-radius: 4px;">
                <strong style="color: #666; font-size: 0.9em;">Код товара:</strong><br>
                <span style="font-size: 1em; color: #333;">${escapeHtml(price.supplier_item_code || '-')}</span>
            </div>
            <div style="padding: 8px; background: #f8f9fa; border-radius: 4px;">
                <strong style="color: #666; font-size: 0.9em;">📊 Штрихкод:</strong><br>
                <span style="font-size: 1em; color: #333; font-family: monospace;">${escapeHtml(price.barcode || 'не указан')}</span>
            </div>
        </div>
        ${price.price ? `
        <div style="margin-top: 10px; padding: 8px; background: #e7f3ff; border-radius: 4px; border-left: 3px solid #007bff;">
            <strong style="color: #666; font-size: 0.9em;">Цена:</strong>
            <span style="font-size: 1.2em; color: #007bff; font-weight: bold; margin-left: 10px;">${price.price.toFixed(2)} ₽</span>
        </div>
        ` : ''}
    `;
    
    // Текущее сопоставление
    const currentMatchInfo = document.getElementById('currentMatchInfo');
    const currentMatchDetails = document.getElementById('currentMatchDetails');
    
    if (price.guid_es) {
        currentMatchInfo.style.display = 'block';
        currentMatchDetails.innerHTML = `
            <div style="margin-bottom: 10px;">
                <strong style="font-size: 1.1em; color: #28a745;">${escapeHtml(price.drug_name || '-')}</strong>
            </div>
            <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin-top: 10px;">
                ${price.inn ? `
                <div style="padding: 8px; background: #f8f9fa; border-radius: 4px;">
                    <strong style="color: #666; font-size: 0.9em;">МНН:</strong><br>
                    <span style="font-size: 1em; color: #333;">${escapeHtml(price.inn)}</span>
                </div>
                ` : ''}
                ${price.cure_form ? `
                <div style="padding: 8px; background: #f8f9fa; border-radius: 4px;">
                    <strong style="color: #666; font-size: 0.9em;">Форма выпуска:</strong><br>
                    <span style="font-size: 1em; color: #333;">${escapeHtml(price.cure_form)}</span>
                </div>
                ` : ''}
            </div>
            ${price.producer_name ? `
            <div style="margin-top: 10px; padding: 8px; background: #fff3cd; border-radius: 4px; border-left: 3px solid #ffc107;">
                <strong style="color: #666; font-size: 0.9em;">🏭 Производитель:</strong><br>
                <span style="font-size: 1em; color: #333;">${escapeHtml(price.producer_name)}</span>
            </div>
            ` : ''}
            ${price.barcode ? `
            <div style="margin-top: 10px; padding: 8px; background: #f8f9fa; border-radius: 4px;">
                <strong style="color: #666; font-size: 0.9em;">📊 Штрихкод:</strong><br>
                <span style="font-size: 1em; color: #333; font-family: monospace;">${escapeHtml(price.barcode)}</span>
            </div>
            ` : ''}
            <div style="margin-top: 10px; padding: 8px; background: #e9ecef; border-radius: 4px;">
                <strong style="color: #666; font-size: 0.9em;">GUID:</strong><br>
                <span style="font-size: 0.85em; color: #666; font-family: monospace; word-break: break-all;">${escapeHtml(price.guid_es)}</span>
            </div>
        `;
    } else {
        currentMatchInfo.style.display = 'none';
    }
    
    // Заполняем поля формы
    const modalMatchMethod = document.getElementById('modalMatchMethod');
    const modalMatchConfidence = document.getElementById('modalMatchConfidence');
    const modalIsConfirmed = document.getElementById('modalIsConfirmed');
    
    if (modalMatchMethod) {
        modalMatchMethod.value = price.match_method || 'MANUAL';
    }
    if (modalMatchConfidence) {
        modalMatchConfidence.value = price.match_confidence || 100;
    }
    if (modalIsConfirmed) {
        modalIsConfirmed.checked = price.is_confirmed || false;
    }
    
    // Очищаем поиск
    document.getElementById('modalDrugSearch').value = '';
    document.getElementById('drugSearchResults').style.display = 'none';
    
    // Показываем модальное окно
    document.getElementById('matchModal').style.display = 'block';
}

function closeMatchModal() {
    document.getElementById('matchModal').style.display = 'none';
    currentEditingPrice = null;
    document.getElementById('modalDrugSearch').value = '';
    document.getElementById('drugSearchResults').style.display = 'none';
}

async function searchDrugs() {
    const query = document.getElementById('modalDrugSearch')?.value.trim();
    if (!query) {
        showError('Введите поисковый запрос');
        return;
    }
    
    const resultsDiv = document.getElementById('drugSearchResults');
    const resultsList = document.getElementById('drugSearchResultsList');
    
    resultsDiv.style.display = 'block';
    resultsList.innerHTML = '<div style="padding: 20px; text-align: center;">Поиск...</div>';
    
    try {
        const response = await AuthManager.authenticatedFetch(`/api/drugs/search?q=${encodeURIComponent(query)}`);
        
        if (!response.ok) {
            throw new Error(`Ошибка поиска: ${response.status}`);
        }
        
        const data = await response.json();
        const drugs = data.drugs || [];
        
        if (drugs.length === 0) {
            resultsList.innerHTML = '<div style="padding: 20px; text-align: center; color: #666;">Препараты не найдены</div>';
            return;
        }
        
        resultsList.innerHTML = '';
            drugs.forEach((drug, index) => {
                const drugDiv = document.createElement('div');
                drugDiv.style.padding = '15px';
                drugDiv.style.borderBottom = '1px solid #eee';
                drugDiv.style.cursor = 'pointer';
                drugDiv.onmouseover = () => drugDiv.style.background = '#f9f9f9';
                drugDiv.onmouseout = () => drugDiv.style.background = '';
                drugDiv.onclick = () => selectDrug(drug);
                
                drugDiv.innerHTML = `
                    <div style="display: flex; justify-content: space-between; align-items: start;">
                        <div style="flex: 1;">
                            <strong>${escapeHtml(drug.name)}</strong><br>
                            ${drug.inn ? `<small>МНН: ${escapeHtml(drug.inn)}</small><br>` : ''}
                            ${drug.cure_form ? `<small>Форма: ${escapeHtml(drug.cure_form)}</small><br>` : ''}
                            ${drug.trade_name ? `<small>Торговое название: ${escapeHtml(drug.trade_name)}</small><br>` : ''}
                            ${drug.producer_name ? `<small>Производитель: ${escapeHtml(drug.producer_name)}</small><br>` : ''}
                            ${drug.barcode ? `<small>Штрихкод: ${escapeHtml(drug.barcode)}</small>` : ''}
                        </div>
                        <button onclick="event.stopPropagation(); window.selectDrugFromList(${index})" class="btn btn-sm btn-success" style="padding: 5px 10px;">
                            Выбрать
                        </button>
                    </div>
                `;
                
                resultsList.appendChild(drugDiv);
            });
            
            // Сохраняем список препаратов для выбора по индексу
            window.searchResultsDrugs = drugs;
    } catch (error) {
        console.error('Ошибка поиска препаратов:', error);
        showError(`Ошибка поиска: ${error.message}`);
        resultsList.innerHTML = '<div style="padding: 20px; text-align: center; color: #dc3545;">Ошибка поиска</div>';
    }
}

function selectDrug(drug) {
    // Устанавливаем выбранный препарат
    const selectedDrug = {
        guid_es: drug.guid_es,
        name: drug.name,
        inn: drug.inn,
        cure_form: drug.cure_form,
        barcode: drug.barcode
    };
    
    // Обновляем текущее сопоставление
    const currentMatchInfo = document.getElementById('currentMatchInfo');
    const currentMatchDetails = document.getElementById('currentMatchDetails');
    
    currentMatchInfo.style.display = 'block';
    currentMatchDetails.innerHTML = `
        <strong>${escapeHtml(drug.name)}</strong><br>
        ${drug.inn ? `<small>МНН: ${escapeHtml(drug.inn)}</small><br>` : ''}
        ${drug.cure_form ? `<small>Форма: ${escapeHtml(drug.cure_form)}</small><br>` : ''}
        <small>GUID: ${escapeHtml(drug.guid_es)}</small>
    `;
    
    // Скрываем результаты поиска
    document.getElementById('drugSearchResults').style.display = 'none';
    document.getElementById('modalDrugSearch').value = drug.name;
    
    // Сохраняем выбранный препарат для сохранения
    if (currentEditingPrice) {
        currentEditingPrice.selectedDrug = selectedDrug;
    }
}

// Глобальная функция для выбора препарата по индексу из результатов поиска
window.selectDrugFromList = function(index) {
    if (window.searchResultsDrugs && window.searchResultsDrugs[index]) {
        selectDrug(window.searchResultsDrugs[index]);
    }
};

async function saveMatch() {
    if (!currentEditingPrice) {
        showError('Не выбран прайс для редактирования');
        return;
    }
    
    // Валидация supplier_price_id
    const priceID = currentEditingPrice.supplier_price_id;
    if (!priceID || typeof priceID !== 'string') {
        console.error('Некорректный supplier_price_id:', priceID);
        showError('Некорректный ID прайса');
        return;
    }
    
    const cleanPriceID = priceID.trim();
    const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
    if (!uuidPattern.test(cleanPriceID)) {
        console.error('Некорректный формат UUID supplier_price_id:', cleanPriceID);
        showError('Некорректный формат ID прайса');
        return;
    }
    
    const guidES = currentEditingPrice.selectedDrug?.guid_es || 
                   (currentEditingPrice.guid_es || null);
    const matchMethod = document.getElementById('modalMatchMethod').value;
    const matchConfidence = parseFloat(document.getElementById('modalMatchConfidence').value);
    
    const saveBtn = document.getElementById('saveMatchBtn');
    
    try {
        if (saveBtn) showButtonLoading(saveBtn, 'Сохранение...');
        // Формируем payload, проверяя, что все поля заполнены
        const payload = {};
        
        // guid_es может быть null (для очистки сопоставления)
        if (guidES !== null && guidES !== undefined) {
            payload.guid_es = guidES;
        }
        
        // Остальные поля всегда должны быть заполнены
        if (matchMethod) {
            payload.match_method = matchMethod;
        }
        if (matchConfidence !== null && matchConfidence !== undefined && !isNaN(matchConfidence)) {
            payload.match_confidence = matchConfidence;
        }
        
        // Проверяем, что хотя бы одно поле указано
        if (Object.keys(payload).length === 0) {
            showError('Не указаны поля для обновления. Выберите лекарство или укажите метод сопоставления.');
            return;
        }
        
        console.log('Сохранение сопоставления:', {
            priceID: cleanPriceID,
            guidES: guidES,
            matchMethod: matchMethod,
            matchConfidence: matchConfidence,
        });
        
        // Формируем URL - используем прямой UUID без дополнительного кодирования
        // Go HTTP сервер автоматически декодирует URL, но лучше использовать прямой путь
        const requestURL = `/api/supplier-prices/${cleanPriceID}/match`;
        
        console.log('Отправка запроса на сопоставление:', {
            priceID: priceID,
            cleanPriceID: cleanPriceID,
            cleanPriceIDLength: cleanPriceID.length,
            cleanPriceIDHex: Array.from(cleanPriceID).map(c => c.charCodeAt(0).toString(16).padStart(2, '0')).join(' '),
            requestURL: requestURL,
            payload: payload
        });
        
        const response = await AuthManager.authenticatedFetch(
            requestURL,
            {
                method: 'PUT',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(payload)
            }
        );
        
        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Ошибка сохранения');
        }
        
        showSuccess('Сопоставление успешно сохранено');
        closeMatchModal();
        await loadPrices();
    } catch (error) {
        console.error('Ошибка сохранения сопоставления:', error);
        showError(`Ошибка сохранения: ${error.message}`);
    } finally {
        if (saveBtn) hideButtonLoading(saveBtn);
    }
}

async function clearMatch() {
    if (!currentEditingPrice) {
        showError('Не выбран прайс для редактирования');
        return;
    }
    
    if (!confirm('Вы уверены, что хотите очистить сопоставление?')) {
        return;
    }
    
    try {
        // Валидация supplier_price_id
        const priceID = currentEditingPrice.supplier_price_id;
        if (!priceID || typeof priceID !== 'string') {
            console.error('Некорректный supplier_price_id:', priceID);
            showError('Некорректный ID прайса');
            return;
        }
        
        const cleanPriceID = priceID.trim();
        const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
        if (!uuidPattern.test(cleanPriceID)) {
            console.error('Некорректный формат UUID supplier_price_id:', cleanPriceID);
            showError('Некорректный формат ID прайса');
            return;
        }
        
        const response = await AuthManager.authenticatedFetch(
            `/api/supplier-prices/${encodeURIComponent(cleanPriceID)}/match`,
            {
                method: 'PUT',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({
                    guid_es: '',
                    match_method: null,
                    match_confidence: null,
                })
            }
        );
        
        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Ошибка очистки');
        }
        
        showSuccess('Сопоставление очищено');
        closeMatchModal();
        await loadPrices();
    } catch (error) {
        console.error('Ошибка очистки сопоставления:', error);
        showError(`Ошибка очистки: ${error.message}`);
    }
}

function escapeHtml(text) {
    if (text == null) return '';
    const div = document.createElement('div');
    div.textContent = String(text);
    return div.innerHTML;
}

function displayPagination(totalPages) {
    const container = document.getElementById('paginationContainer');
    const info = document.getElementById('paginationInfo');
    const controls = document.getElementById('paginationControls');
    if (!container || !info || !controls) return;
    container.style.display = 'flex';
    const start = (currentPage - 1) * itemsPerPage + 1;
    const end = Math.min(currentPage * itemsPerPage, filteredPrices.length);
    info.textContent = `Показано ${start}-${end} из ${filteredPrices.length} позиций`;
    let html = `<button class="pagination-btn" onclick="goToPage(${currentPage - 1})" ${currentPage === 1 ? 'disabled' : ''}>‹ Назад</button>`;
    const maxPages = 7;
    let startPage = Math.max(1, currentPage - Math.floor(maxPages / 2));
    let endPage = Math.min(totalPages, startPage + maxPages - 1);
    if (startPage > 1) {
        html += `<button class="pagination-btn" onclick="goToPage(1)">1</button>`;
        if (startPage > 2) html += `<span style="padding: 8px;">...</span>`;
    }
    for (let i = startPage; i <= endPage; i++) {
        html += `<button class="pagination-btn ${i === currentPage ? 'active' : ''}" onclick="goToPage(${i})">${i}</button>`;
    }
    if (endPage < totalPages) {
        if (endPage < totalPages - 1) html += `<span style="padding: 8px;">...</span>`;
        html += `<button class="pagination-btn" onclick="goToPage(${totalPages})">${totalPages}</button>`;
    }
    html += `<button class="pagination-btn" onclick="goToPage(${currentPage + 1})" ${currentPage === totalPages ? 'disabled' : ''}>Вперед ›</button>`;
    controls.innerHTML = html;
}

function goToPage(page) {
    if (page < 1) return;
    if (!filteredPrices || filteredPrices.length === 0) return;
    const totalPages = itemsPerPage > 0 ? Math.ceil(filteredPrices.length / itemsPerPage) : 1;
    if (page > totalPages) return;
    currentPage = page;
    displayFilteredPrices();
    window.scrollTo({ top: 0, behavior: 'smooth' });
}

// Экспортируем функции для использования в onclick
window.goToPage = goToPage;
