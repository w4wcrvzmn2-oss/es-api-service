// Скрипт для страницы сводного прайса

// Глобальные переменные для улучшенной функциональности
let filteredData = null;
let currentSort = { column: null, direction: 'asc' };
let currentPage = 1;
let itemsPerPage = 50;
let isCardsView = false;
let searchTimeout = null;
let autoRefreshInterval = null;
let currentSelectedIndex = -1; // Индекс выбранной позиции для навигации с клавиатуры

// Инициализация после загрузки DOM
document.addEventListener('DOMContentLoaded', () => {
    // Загрузка данных при инициализации
    loadSuppliers();
    // Регионы не загружаются для сводного прайса - все данные о препарате из ЕС
    
    // Восстановление состояния
    restoreState();
    
    // Кнопки
    const loadBtn = document.getElementById('loadSummaryBtn');
    const exportBtn = document.getElementById('exportBtn');
    const exportExcelBtn = document.getElementById('exportExcelBtn');
    const clearFiltersBtn = document.getElementById('clearFiltersBtn');
    const toggleViewBtn = document.getElementById('toggleViewBtn');
    
    if (loadBtn) loadBtn.addEventListener('click', loadPriceSummary);
    if (exportBtn) exportBtn.addEventListener('click', exportToJSON);
    if (exportExcelBtn) exportExcelBtn.addEventListener('click', exportToExcel);
    if (clearFiltersBtn) clearFiltersBtn.addEventListener('click', clearFilters);
    if (toggleViewBtn) toggleViewBtn.addEventListener('click', toggleView);
    
    // Кнопка показа/скрытия фильтров
    const toggleFiltersBtn = document.getElementById('toggleFiltersBtn');
    const advancedFilters = document.getElementById('advancedFilters');
    const filtersIcon = document.getElementById('filtersIcon');
    const filtersText = document.getElementById('filtersText');
    
    console.log('Инициализация кнопки фильтров:', {
        toggleFiltersBtn: !!toggleFiltersBtn,
        advancedFilters: !!advancedFilters,
        filtersIcon: !!filtersIcon,
        filtersText: !!filtersText
    });
    
    if (toggleFiltersBtn && advancedFilters) {
        toggleFiltersBtn.addEventListener('click', function(e) {
            e.preventDefault();
            e.stopPropagation();
            
            console.log('Клик по кнопке фильтров');
            
            // Получаем текущее состояние через computed style
            const computedStyle = window.getComputedStyle(advancedFilters);
            const inlineDisplay = advancedFilters.style.display;
            const currentDisplay = inlineDisplay || computedStyle.display;
            const isCurrentlyVisible = currentDisplay !== 'none';
            
            console.log('Состояние фильтров:', {
                inlineDisplay: inlineDisplay,
                computedDisplay: computedStyle.display,
                currentDisplay: currentDisplay,
                isCurrentlyVisible: isCurrentlyVisible
            });
            
            // Переключаем видимость
            if (isCurrentlyVisible) {
                // Скрываем
                advancedFilters.style.display = 'none';
                if (filtersIcon) filtersIcon.textContent = '⚙️';
                if (filtersText) filtersText.textContent = 'Фильтры';
                console.log('Фильтры скрыты');
            } else {
                // Показываем
                advancedFilters.style.display = 'block';
                if (filtersIcon) filtersIcon.textContent = '🔽';
                if (filtersText) filtersText.textContent = 'Скрыть';
                console.log('Фильтры показаны');
            }
        }, true); // Используем capture phase для надежности
    } else {
        console.error('Не найдены элементы для переключения фильтров:', {
            toggleFiltersBtn: !!toggleFiltersBtn,
            advancedFilters: !!advancedFilters,
            filtersIcon: !!filtersIcon,
            filtersText: !!filtersText
        });
    }
    
    // Также добавляем обработчик через onclick для надежности
    if (toggleFiltersBtn) {
        toggleFiltersBtn.onclick = function(e) {
            e.preventDefault();
            e.stopPropagation();
            
            const advancedFilters = document.getElementById('advancedFilters');
            const filtersIcon = document.getElementById('filtersIcon');
            const filtersText = document.getElementById('filtersText');
            
            if (!advancedFilters) {
                console.error('advancedFilters не найден');
                return false;
            }
            
            const computedStyle = window.getComputedStyle(advancedFilters);
            const inlineDisplay = advancedFilters.style.display;
            const currentDisplay = inlineDisplay || computedStyle.display;
            const isCurrentlyVisible = currentDisplay !== 'none';
            
            if (isCurrentlyVisible) {
                advancedFilters.style.display = 'none';
                if (filtersIcon) filtersIcon.textContent = '⚙️';
                if (filtersText) filtersText.textContent = 'Фильтры';
            } else {
                advancedFilters.style.display = 'block';
                if (filtersIcon) filtersIcon.textContent = '🔽';
                if (filtersText) filtersText.textContent = 'Скрыть';
            }
            
            return false;
        };
    }
    
    // Поиск в реальном времени
    const searchInput = document.getElementById('searchInput');
    const searchClearBtn = document.getElementById('searchClearBtn');
    if (searchInput) {
        searchInput.addEventListener('input', (e) => {
            // Показываем/скрываем кнопку очистки
            if (searchClearBtn) {
                searchClearBtn.style.display = e.target.value ? 'block' : 'none';
            }
            
            clearTimeout(searchTimeout);
            searchTimeout = setTimeout(() => {
                if (currentSummaryData) {
                    applyFilters();
                    updateSearchResultsInfo();
                    // Сбрасываем выделение при новом поиске
                    currentSelectedIndex = -1;
                    updateSelection();
                }
            }, 200); // Уменьшена задержка для более быстрого отклика
        });
        
        // Обработка Enter - применение фильтров и переход на таблицу
        searchInput.addEventListener('keydown', (e) => {
            if (e.key === 'Enter') {
                e.preventDefault();
                // Применяем фильтры
                if (currentSummaryData) {
                    applyFilters();
                    updateSearchResultsInfo();
                    currentSelectedIndex = -1;
                    updateSelection();
                }
                // Переключаемся на таблицу для навигации
                if (isCardsView) {
                    isCardsView = false;
                    updateViewToggleButton();
                    saveState();
                    if (currentSummaryData) applyFilters();
                }
                // Убираем фокус с поля поиска
                searchInput.blur();
            } else if (e.key === 'Escape') {
                searchInput.value = '';
                if (searchClearBtn) searchClearBtn.style.display = 'none';
                if (currentSummaryData) {
                    applyFilters();
                    updateSearchResultsInfo();
                }
            }
        });
    }
    
    // Обработчик клавиатуры для навигации по позициям
    document.addEventListener('keydown', handleKeyboardNavigation);
    
    // Фильтры
    const supplierSelect = document.getElementById('supplierSelect');
    const regionSelect = document.getElementById('regionSelect');
    const priceMin = document.getElementById('priceMin');
    const priceMax = document.getElementById('priceMax');
    
    if (supplierSelect) {
        supplierSelect.addEventListener('change', () => {
            if (currentSummaryData) applyFilters();
            else if (supplierSelect.value) {
                loadPriceSummary();
            }
        });
    }
    // Регионы не используются в сводном прайсе
    if (priceMin) {
        priceMin.addEventListener('input', () => {
            clearTimeout(searchTimeout);
            searchTimeout = setTimeout(() => {
                if (currentSummaryData) applyFilters();
            }, 300);
        });
    }
    if (priceMax) {
        priceMax.addEventListener('input', () => {
            clearTimeout(searchTimeout);
            searchTimeout = setTimeout(() => {
                if (currentSummaryData) applyFilters();
            }, 300);
        });
    }
    
    // Пагинация
    const itemsPerPageSelect = document.getElementById('itemsPerPageSelect');
    if (itemsPerPageSelect) {
        itemsPerPageSelect.addEventListener('change', (e) => {
            itemsPerPage = parseInt(e.target.value) || 0;
            currentPage = 1;
            saveState();
            if (currentSummaryData) applyFilters();
        });
    }
    
    // Автообновление
    const autoRefreshCheck = document.getElementById('autoRefreshCheck');
    if (autoRefreshCheck) {
        autoRefreshCheck.addEventListener('change', (e) => {
            if (e.target.checked) {
                startAutoRefresh();
            } else {
                stopAutoRefresh();
            }
        });
    }
    
    // Автозагрузка при открытии страницы
    // Ждем загрузки поставщиков и регионов, затем загружаем прайс
    setTimeout(async () => {
        try {
            await loadSuppliers();
            // Небольшая задержка для завершения загрузки списков
            await new Promise(resolve => setTimeout(resolve, 300));
            loadPriceSummary();
        } catch (error) {
            console.warn('Ошибка автозагрузки:', error);
        }
    }, 500);
});

let currentSummaryData = null;

async function loadSuppliers() {
    const select = document.getElementById('supplierSelect');
    
    if (!select) return Promise.resolve();
    
    if (!AuthManager.isAuthenticated()) {
        console.warn('Требуется авторизация для загрузки поставщиков');
        return Promise.resolve(); // Не блокируем автозагрузку
    }
    
    try {
        const response = await AuthManager.authenticatedFetch('/api/suppliers');
        
        if (!response.ok) {
            throw new Error('Ошибка загрузки поставщиков');
        }
        
        const suppliers = await response.json();
        
        // Очищаем список перед добавлением новых элементов (кроме первого опционального)
        const firstOption = select.options[0];
        select.innerHTML = '';
        if (firstOption && firstOption.value === '') {
            select.appendChild(firstOption);
        } else {
            // Если первого опционального нет, добавляем его
            const defaultOption = document.createElement('option');
            defaultOption.value = '';
            defaultOption.textContent = '-- Все поставщики --';
            select.appendChild(defaultOption);
        }
        
        if (Array.isArray(suppliers) && suppliers.length > 0) {
            // Используем Set для отслеживания уже добавленных поставщиков (по ID)
            const addedSupplierIds = new Set();
            suppliers.forEach(s => {
                // Пропускаем дубликаты по supplier_id
                if (!addedSupplierIds.has(s.supplier_id)) {
                    addedSupplierIds.add(s.supplier_id);
                    const option = document.createElement('option');
                    option.value = s.supplier_id;
                    option.textContent = s.name;
                    select.appendChild(option);
                }
            });
        }
        return Promise.resolve();
    } catch (error) {
        console.warn('Ошибка загрузки поставщиков:', error);
        return Promise.resolve(); // Продолжаем даже при ошибке
    }
}

async function loadRegions() {
    try {
        const response = await AuthManager.authenticatedFetch('/api/region?limit=1000');
        if (response.ok) {
            const regions = await response.json();
            const select = document.getElementById('regionSelect');
            
            if (Array.isArray(regions) && select) {
                // Очищаем список перед добавлением новых элементов (кроме первого опционального)
                const firstOption = select.options[0];
                select.innerHTML = '';
                if (firstOption && firstOption.value === '') {
                    select.appendChild(firstOption);
                } else {
                    // Если первого опционального нет, добавляем его
                    const defaultOption = document.createElement('option');
                    defaultOption.value = '';
                    defaultOption.textContent = '-- Все регионы --';
                    select.appendChild(defaultOption);
                }
                
                // Используем Set для отслеживания уже добавленных регионов (по ID)
                const addedRegionIds = new Set();
                regions.forEach(r => {
                    const regionId = r.RegionID || r.region_id;
                    // Пропускаем дубликаты по region_id
                    if (regionId && !addedRegionIds.has(regionId)) {
                        addedRegionIds.add(regionId);
                        const option = document.createElement('option');
                        option.value = regionId;
                        option.textContent = r.Name || r.name || r.Code || r.code || regionId;
                        select.appendChild(option);
                    }
                });
            }
        }
        return Promise.resolve();
    } catch (error) {
        console.warn('Ошибка загрузки регионов:', error);
        return Promise.resolve(); // Продолжаем даже при ошибке
    }
}

async function loadPriceSummary() {
    const supplierID = document.getElementById('supplierSelect').value;
    // Регионы не используются в сводном прайсе
    
    const loadBtn = document.getElementById('loadSummaryBtn');
    const container = document.getElementById('summaryTableContainer');
    
    try {
        showButtonLoading(loadBtn, 'Загрузка прайса...');
        if (container) {
            showElementLoading(container, 'Загрузка сводного прайса...');
        }
        let url = '/api/supplier-prices/summary';
        const params = new URLSearchParams();
        if (supplierID) {
            params.append('supplier_id', supplierID);
        }
        // region_id не передается - регионы не используются в сводном прайсе
        if (params.toString()) {
            url += '?' + params.toString();
        }
        
        const response = await AuthManager.authenticatedFetch(url);
        
        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Ошибка загрузки');
        }
        
        const data = await response.json();
        currentSummaryData = data;
        
        // Показываем улучшенный заголовок вместо старого
        const summaryHeader = document.getElementById('summaryHeader');
        const oldSummaryStats = document.getElementById('summaryStats');
        if (summaryHeader) summaryHeader.style.display = 'block';
        if (oldSummaryStats) oldSummaryStats.style.display = 'none';
        
        const exportBtn = document.getElementById('exportBtn');
        if (exportBtn) exportBtn.style.display = 'inline-block';
        const exportExcelBtn = document.getElementById('exportExcelBtn');
        if (exportExcelBtn) exportExcelBtn.style.display = 'inline-block';
        
        updateStats(data);
        saveState();
        applyFilters();
        
        let message = supplierID 
            ? `Загружен сводный прайс поставщика: ${data.summary.length} позиций`
            : `Загружен сводный прайс всех поставщиков: ${data.summary.length} позиций`;
        
        showSuccess(message);
    } catch (error) {
        let errorMessage = error.message || 'Неизвестная ошибка';
        
        // Улучшаем сообщение для ошибок сети
        if (errorMessage.includes('Failed to fetch') || 
            errorMessage.includes('NetworkError') ||
            errorMessage.includes('ERR_CONNECTION_REFUSED')) {
            const apiUrl = AuthManager.getApiUrl();
            errorMessage = `Ошибка подключения: Не удалось подключиться к серверу ${apiUrl}.\n\nВозможные причины:\n• Сервер не запущен\n• Неправильный адрес сервера\n• Проблемы с сетью`;
        }
        
        showError(`Ошибка загрузки сводного прайса: ${errorMessage}`);
        if (container) {
            container.innerHTML = '';
            hideElementLoading(container);
        }
        const summaryStats = document.getElementById('summaryStats');
        if (summaryStats) summaryStats.style.display = 'none';
        const summaryHeader = document.getElementById('summaryHeader');
        if (summaryHeader) summaryHeader.style.display = 'none';
    } finally {
        hideButtonLoading(loadBtn);
        if (container) {
            hideElementLoading(container);
        }
    }
}

function displaySummary(data) {
    const container = document.getElementById('summaryTableContainer');
    
    if (!data.summary || data.summary.length === 0) {
        container.innerHTML = '<div style="color: #dc3545; padding: 20px; text-align: center;">Прайсы не найдены. Сначала добавьте прайсы или запустите сопоставление.</div>';
        return;
    }
    
    const hasSupplierInfo = data.summary.some(p => p.supplier_name);
    // Регионы не отображаются в сводном прайсе - все данные о препарате из ЕС
    // Наценки не передаются клиенту для безопасности - расчеты только на сервере
    
    let html = '<table id="summaryTable" class="summary-table">';
    html += '<thead><tr>';
    html += `<th class="sortable" onclick="sortByColumn('drug_name')">Препарат</th>`;
    if (hasSupplierInfo) {
        html += `<th class="sortable" onclick="sortByColumn('supplier_name')">Поставщик</th>`;
    }
    html += `<th class="sortable" onclick="sortByColumn('inn')">МНН</th>`;
    html += '<th>Форма</th>';
    // Базовая цена и наценка не отображаются - расчеты только на сервере для безопасности
    html += `<th class="sortable" onclick="sortByColumn('price')" style="text-align: right;">Цена</th>`;
    html += `<th class="sortable" onclick="sortByColumn('quantity')" style="text-align: right;">Количество</th>`;
    html += '<th>Срок годности</th>';
    // Страна и Регион не отображаются - все данные о препарате из ЕС
    const hasProducerInfo = data.summary.some(p => p.producer_name);
    const hasRegistryPrice = data.summary.some(p => p.registry_price);
    
    if (hasProducerInfo) {
        html += '<th>Производитель</th>';
    }
    if (hasRegistryPrice) {
        html += '<th style="text-align: right;">Цена реестра</th>';
    }
    html += '<th>Действия</th>';
    html += '</tr></thead><tbody>';
    setTimeout(() => {
        document.querySelectorAll('.summary-table th.sortable').forEach(th => {
            th.classList.remove('sort-asc', 'sort-desc');
            const text = th.textContent.trim();
            const columnNames = {
                'Препарат': 'drug_name',
                'Поставщик': 'supplier_name',
                'МНН': 'inn',
                'Финальная цена': 'price',
                'Регион': 'region_name',
                'Количество': 'quantity'
            };
            const column = columnNames[text];
            if (column === currentSort.column) {
                th.classList.add(currentSort.direction === 'asc' ? 'sort-asc' : 'sort-desc');
            }
        });
    }, 100);
    
    const startIndex = (currentPage - 1) * itemsPerPage;
    data.summary.forEach((price, pageIndex) => {
        const globalIndex = startIndex + pageIndex;
        // Только финальная цена передается клиенту (рассчитана на сервере с учетом наценки)
        const finalPrice = formatPrice(price.price);
        const registryPrice = formatPrice(price.registry_price);
        
        // Добавляем класс для выделения клавиатурой
        const selectedClass = globalIndex === currentSelectedIndex ? ' keyboard-selected' : '';
        html += `<tr class="price-row${selectedClass}" data-index="${globalIndex}">`;
        html += `<td><strong>${escapeHtml(price.drug_name || '-')}</strong></td>`;
        
        if (hasSupplierInfo) {
            html += `<td>${escapeHtml(price.supplier_name || '-')}</td>`;
        }
        
        html += `<td>${escapeHtml(price.inn || '-')}</td>`;
        html += `<td>${escapeHtml(price.cure_form || '-')}</td>`;
        
        // Базовая цена и наценка не отображаются - расчеты только на сервере
        html += `<td class="price-final" style="text-align: right; font-weight: bold; color: #28a745;">${finalPrice}</td>`;
        
        // Количество всегда отображается
        const quantity = price.quantity != null && price.quantity !== undefined ? price.quantity.toFixed(3) : '-';
        html += `<td style="text-align: right;">${quantity}</td>`;
        
        // Срок годности
        const expiryDate = price.expiry_date ? new Date(price.expiry_date).toLocaleDateString('ru-RU') : '-';
        html += `<td>${expiryDate}</td>`;
        
        // Страна и Регион не отображаются - все данные о препарате из ЕС
        
        if (hasProducerInfo) {
            html += `<td>${escapeHtml(price.producer_name || '-')}</td>`;
        }
        
        if (hasRegistryPrice) {
            html += `<td style="text-align: right;">${registryPrice}</td>`;
        }
        
        html += `<td><button onclick="showDrugCard(${globalIndex})" class="btn btn-sm" style="padding: 4px 8px; background: #17a2b8; color: white; cursor: pointer;">📋 Карточка</button></td>`;
        html += '</tr>';
    });
    
    // Не перезаписываем window.summaryData здесь, так как он уже содержит все отфильтрованные данные
    // window.summaryData устанавливается в displayFilteredData()
    
    html += '</tbody></table>';
    container.innerHTML = html;
    
    // Обновляем выделение после отрисовки
    setTimeout(() => {
        updateSelection();
    }, 100);
}

function updateStats(data) {
    // Используем новый заголовок summaryHeader, а не старый summaryStats
    const statsDiv = document.getElementById('summaryHeader');
    const statsText = document.getElementById('statsText');
    
    if (!statsDiv || !statsText) return;
    
    if (!data.summary || data.summary.length === 0) {
        statsDiv.style.display = 'none';
        return;
    }
    
    const totalItems = data.summary.length;
    const suppliersCount = new Set(data.summary.filter(p => p.supplier_id).map(p => p.supplier_id)).size;
    const regionsCount = new Set(data.summary.filter(p => p.region_id).map(p => p.region_id)).size;
    // Наценки не передаются клиенту - статистика не доступна
    
    const avgPrice = data.summary.reduce((sum, p) => sum + (p.price || 0), 0) / totalItems;
    const minPrice = Math.min(...data.summary.map(p => p.price || Infinity));
    const maxPrice = Math.max(...data.summary.map(p => p.price || 0));
    
    let stats = `Всего позиций: ${totalItems}`;
    if (suppliersCount > 0) {
        stats += ` | Поставщиков: ${suppliersCount}`;
    }
    if (regionsCount > 0) {
        stats += ` | Регионов: ${regionsCount}`;
    }
    // Статистика по наценкам недоступна - наценки не передаются клиенту для безопасности
    stats += ` | Средняя цена: ${formatPrice(avgPrice)}`;
    stats += ` | Диапазон: ${formatPrice(minPrice)} - ${formatPrice(maxPrice)}`;
    
    statsText.textContent = stats;
    statsDiv.style.display = 'block';
}

// Функции для работы с состоянием и улучшенной функциональностью
function saveState() {
    try {
        const state = {
            supplierID: document.getElementById('supplierSelect')?.value || '',
            regionID: document.getElementById('regionSelect')?.value || '',
            searchText: document.getElementById('searchInput')?.value || '',
            priceMin: document.getElementById('priceMin')?.value || '',
            priceMax: document.getElementById('priceMax')?.value || '',
            itemsPerPage: itemsPerPage,
            currentPage: currentPage,
            isCardsView: isCardsView,
            sort: currentSort
        };
        localStorage.setItem('priceSummaryState', JSON.stringify(state));
    } catch (e) {
        console.warn('Не удалось сохранить состояние:', e);
    }
}

function restoreState() {
    try {
        const state = JSON.parse(localStorage.getItem('priceSummaryState'));
        if (state) {
            if (state.supplierID && document.getElementById('supplierSelect')) {
                document.getElementById('supplierSelect').value = state.supplierID;
            }
            if (state.regionID && document.getElementById('regionSelect')) {
                document.getElementById('regionSelect').value = state.regionID;
            }
            if (state.searchText && document.getElementById('searchInput')) {
                const searchInput = document.getElementById('searchInput');
                searchInput.value = state.searchText;
                const searchClearBtn = document.getElementById('searchClearBtn');
                if (searchClearBtn) searchClearBtn.style.display = 'block';
            }
            if (state.priceMin && document.getElementById('priceMin')) {
                document.getElementById('priceMin').value = state.priceMin;
            }
            if (state.priceMax && document.getElementById('priceMax')) {
                document.getElementById('priceMax').value = state.priceMax;
            }
            if (state.itemsPerPage) {
                itemsPerPage = state.itemsPerPage;
                const select = document.getElementById('itemsPerPageSelect');
                if (select) select.value = state.itemsPerPage;
            }
            if (state.currentPage) currentPage = state.currentPage;
            if (state.isCardsView !== undefined) {
                isCardsView = state.isCardsView;
                updateViewToggleButton();
            }
            if (state.sort) currentSort = state.sort;
        }
    } catch (e) {
        console.warn('Не удалось восстановить состояние:', e);
    }
}

function clearFilters() {
    if (document.getElementById('supplierSelect')) document.getElementById('supplierSelect').value = '';
    if (document.getElementById('regionSelect')) document.getElementById('regionSelect').value = '';
    const searchInput = document.getElementById('searchInput');
    if (searchInput) {
        searchInput.value = '';
        const searchClearBtn = document.getElementById('searchClearBtn');
        if (searchClearBtn) searchClearBtn.style.display = 'none';
    }
    if (document.getElementById('priceMin')) document.getElementById('priceMin').value = '';
    if (document.getElementById('priceMax')) document.getElementById('priceMax').value = '';
    currentPage = 1;
    saveState();
    if (currentSummaryData) {
        applyFilters();
        updateSearchResultsInfo();
    }
}

function clearSearch() {
    const searchInput = document.getElementById('searchInput');
    if (searchInput) {
        searchInput.value = '';
        const searchClearBtn = document.getElementById('searchClearBtn');
        if (searchClearBtn) searchClearBtn.style.display = 'none';
        if (currentSummaryData) {
            applyFilters();
            updateSearchResultsInfo();
        }
    }
}

function updateSearchResultsInfo() {
    const searchInput = document.getElementById('searchInput');
    const searchResultsInfo = document.getElementById('searchResultsInfo');
    if (!searchInput || !searchResultsInfo) return;
    
    const searchText = searchInput.value.trim();
    if (searchText && filteredData !== null) {
        const total = currentSummaryData ? currentSummaryData.summary.length : 0;
        const found = filteredData.length;
        searchResultsInfo.textContent = `Найдено: ${found} из ${total} позиций`;
        searchResultsInfo.style.display = 'block';
    } else {
        searchResultsInfo.style.display = 'none';
    }
}

function toggleView() {
    isCardsView = !isCardsView;
    currentSelectedIndex = -1; // Сбрасываем выделение при переключении вида
    updateViewToggleButton();
    saveState();
    if (currentSummaryData) applyFilters();
}

function updateViewToggleButton() {
    const icon = document.getElementById('viewModeIcon');
    const text = document.getElementById('viewModeText');
    if (icon && text) {
        if (isCardsView) {
            icon.textContent = '📋';
            text.textContent = 'Таблица';
        } else {
            icon.textContent = '🎴';
            text.textContent = 'Карточки';
        }
    }
}

// Обработка навигации с клавиатуры
function handleKeyboardNavigation(e) {
    // Игнорируем, если пользователь вводит текст в поле ввода
    if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA' || e.target.tagName === 'SELECT') {
        // ESC - переход на поиск и выделение текста
        if (e.key === 'Escape') {
            e.preventDefault();
            const searchInput = document.getElementById('searchInput');
            if (searchInput) {
                searchInput.focus();
                searchInput.select();
            }
            return;
        }
        // Если пользователь вводит текст, не обрабатываем стрелки
        return;
    }
    
    // ESC - переход на поле поиска и выделение текста
    if (e.key === 'Escape') {
        e.preventDefault();
        const searchInput = document.getElementById('searchInput');
        if (searchInput) {
            searchInput.focus();
            searchInput.select();
        }
        return;
    }
    
    // Стрелки вверх/вниз - навигация между позициями
    if (e.key === 'ArrowUp' || e.key === 'ArrowDown') {
        e.preventDefault();
        
        // Переключаемся на таблицу, если сейчас карточки
        if (isCardsView) {
            isCardsView = false;
            updateViewToggleButton();
            saveState();
            if (currentSummaryData) {
                applyFilters();
                // Небольшая задержка для отрисовки таблицы перед навигацией
                setTimeout(() => {
                    handleArrowNavigation(e.key);
                }, 100);
                return;
            }
        }
        
        handleArrowNavigation(e.key);
    }
}

// Обработка навигации стрелками
function handleArrowNavigation(key) {
    if (!filteredData || filteredData.length === 0) {
        return;
    }
    
    // Вычисляем индекс с учетом пагинации
    const startIndex = (currentPage - 1) * itemsPerPage;
    const endIndex = Math.min(startIndex + itemsPerPage, filteredData.length);
    const pageItems = filteredData.slice(startIndex, endIndex);
    const maxPageIndex = pageItems.length - 1;
    
    // Если ничего не выделено, начинаем с первой позиции на странице
    if (currentSelectedIndex < startIndex || currentSelectedIndex >= endIndex) {
        currentSelectedIndex = key === 'ArrowDown' ? startIndex : endIndex - 1;
    } else {
        // Находим индекс на текущей странице
        const pageRelativeIndex = currentSelectedIndex - startIndex;
        
        if (key === 'ArrowUp') {
            if (pageRelativeIndex > 0) {
                currentSelectedIndex--;
            } else {
                // Переходим на предыдущую страницу
                if (currentPage > 1) {
                    currentPage--;
                    const newStartIndex = (currentPage - 1) * itemsPerPage;
                    const newEndIndex = Math.min(newStartIndex + itemsPerPage, filteredData.length);
                    currentSelectedIndex = newEndIndex - 1;
                    applyFilters(); // Перерисовываем с новой страницей
                    return;
                } else {
                    // Циклическая навигация - переходим на последнюю страницу
                    const totalPages = Math.ceil(filteredData.length / itemsPerPage);
                    currentPage = totalPages;
                    const newStartIndex = (currentPage - 1) * itemsPerPage;
                    const newEndIndex = Math.min(newStartIndex + itemsPerPage, filteredData.length);
                    currentSelectedIndex = newEndIndex - 1;
                    applyFilters();
                    return;
                }
            }
        } else if (key === 'ArrowDown') {
            if (pageRelativeIndex < maxPageIndex) {
                currentSelectedIndex++;
            } else {
                // Переходим на следующую страницу
                const totalPages = Math.ceil(filteredData.length / itemsPerPage);
                if (currentPage < totalPages) {
                    currentPage++;
                    currentSelectedIndex = (currentPage - 1) * itemsPerPage;
                    applyFilters(); // Перерисовываем с новой страницей
                    return;
                } else {
                    // Циклическая навигация - переходим на первую страницу
                    currentPage = 1;
                    currentSelectedIndex = 0;
                    applyFilters();
                    return;
                }
            }
        }
    }
    
    updateSelection();
    scrollToSelected();
}

// Обновление визуального выделения позиции
function updateSelection() {
    // Убираем выделение со всех элементов
    document.querySelectorAll('.keyboard-selected').forEach(el => {
        el.classList.remove('keyboard-selected');
    });
    
    if (currentSelectedIndex < 0 || !filteredData || currentSelectedIndex >= filteredData.length) {
        return;
    }
    
    // Вычисляем индекс на текущей странице
    const startIndex = (currentPage - 1) * itemsPerPage;
    const endIndex = Math.min(startIndex + itemsPerPage, filteredData.length);
    
    // Проверяем, что выделенная позиция на текущей странице
    if (currentSelectedIndex < startIndex || currentSelectedIndex >= endIndex) {
        return;
    }
    
    const pageRelativeIndex = currentSelectedIndex - startIndex;
    
    // Выделяем текущую позицию
    if (isCardsView) {
        // В режиме карточек
        const cards = document.querySelectorAll('.price-card');
        if (cards[pageRelativeIndex]) {
            cards[pageRelativeIndex].classList.add('keyboard-selected');
        }
    } else {
        // В режиме таблицы
        const rows = document.querySelectorAll('#summaryTable tbody tr');
        if (rows[pageRelativeIndex]) {
            rows[pageRelativeIndex].classList.add('keyboard-selected');
        }
    }
}

// Прокрутка к выбранной позиции
function scrollToSelected() {
    if (currentSelectedIndex < 0 || !filteredData || currentSelectedIndex >= filteredData.length) {
        return;
    }
    
    // Вычисляем индекс на текущей странице
    const startIndex = (currentPage - 1) * itemsPerPage;
    const endIndex = Math.min(startIndex + itemsPerPage, filteredData.length);
    
    // Проверяем, что выделенная позиция на текущей странице
    if (currentSelectedIndex < startIndex || currentSelectedIndex >= endIndex) {
        return;
    }
    
    const pageRelativeIndex = currentSelectedIndex - startIndex;
    
    let element = null;
    
    if (isCardsView) {
        const cards = document.querySelectorAll('.price-card');
        element = cards[pageRelativeIndex];
    } else {
        const rows = document.querySelectorAll('#summaryTable tbody tr');
        element = rows[pageRelativeIndex];
    }
    
    if (element) {
        element.scrollIntoView({ 
            behavior: 'smooth', 
            block: 'center',
            inline: 'nearest'
        });
    }
}

function startAutoRefresh() {
    if (autoRefreshInterval) clearInterval(autoRefreshInterval);
    autoRefreshInterval = setInterval(() => {
        loadPriceSummary();
    }, 60000);
}

function stopAutoRefresh() {
    if (autoRefreshInterval) {
        clearInterval(autoRefreshInterval);
        autoRefreshInterval = null;
    }
}

function applyFilters(resetPage = true) {
    if (!currentSummaryData || !currentSummaryData.summary) return;
    
    let filtered = [...currentSummaryData.summary];
    
    const searchInput = document.getElementById('searchInput');
    if (searchInput) {
        const searchText = searchInput.value.toLowerCase().trim();
        if (searchText) {
            filtered = filtered.filter(p => 
                (p.drug_name && p.drug_name.toLowerCase().includes(searchText)) ||
                (p.inn && p.inn.toLowerCase().includes(searchText)) ||
                (p.trade_name && p.trade_name.toLowerCase().includes(searchText))
            );
        }
    }
    
    const priceMinInput = document.getElementById('priceMin');
    const priceMaxInput = document.getElementById('priceMax');
    if (priceMinInput) {
        const priceMin = parseFloat(priceMinInput.value);
        if (!isNaN(priceMin) && priceMin > 0) {
            filtered = filtered.filter(p => (p.price || 0) >= priceMin);
        }
    }
    if (priceMaxInput) {
        const priceMax = parseFloat(priceMaxInput.value);
        if (!isNaN(priceMax) && priceMax > 0) {
            filtered = filtered.filter(p => (p.price || 0) <= priceMax);
        }
    }
    
    if (currentSort.column) {
        filtered.sort((a, b) => {
            let aVal = getSortValue(a, currentSort.column);
            let bVal = getSortValue(b, currentSort.column);
            if (aVal < bVal) return currentSort.direction === 'asc' ? -1 : 1;
            if (aVal > bVal) return currentSort.direction === 'asc' ? 1 : -1;
            return 0;
        });
    }
    
    filteredData = filtered;
    
    // Проверяем, что текущая страница не выходит за границы после фильтрации
    const totalPages = itemsPerPage > 0 ? Math.ceil(filtered.length / itemsPerPage) : 1;
    if (currentPage > totalPages && totalPages > 0) {
        currentPage = totalPages;
    }
    
    // Сбрасываем страницу только если resetPage = true (при изменении фильтров)
    if (resetPage) {
        currentPage = 1;
        currentSelectedIndex = -1; // Сбрасываем выделение при фильтрации
    }
    
    saveState();
    displayFilteredData();
    updateSearchResultsInfo();
}

function getSortValue(item, column) {
    switch(column) {
        case 'drug_name': return (item.drug_name || '').toLowerCase();
        case 'price': return item.price || 0;
        case 'quantity': return item.quantity != null && item.quantity !== undefined ? item.quantity : 0;
        case 'supplier_name': return (item.supplier_name || '').toLowerCase();
        case 'inn': return (item.inn || '').toLowerCase();
        default: return '';
    }
}

function sortByColumn(column) {
    if (currentSort.column === column) {
        currentSort.direction = currentSort.direction === 'asc' ? 'desc' : 'asc';
    } else {
        currentSort.column = column;
        currentSort.direction = 'asc';
    }
    saveState();
    applyFilters();
}

function displayFilteredData() {
    const container = document.getElementById('summaryTableContainer');
    if (!container) return;
    
    if (!filteredData || filteredData.length === 0) {
        const searchInput = document.getElementById('searchInput');
        const hasSearch = searchInput && searchInput.value.trim();
        container.innerHTML = `<div style="color: ${hasSearch ? '#dc3545' : '#6c757d'}; padding: 40px; text-align: center; font-size: 1.2em;">
            ${hasSearch ? '🔍 Ничего не найдено по запросу "' + escapeHtml(searchInput.value) + '"' : '📋 Нет данных для отображения. Нажмите "Загрузить прайс" для получения данных.'}
        </div>`;
        const paginationContainer = document.getElementById('paginationContainer');
        if (paginationContainer) paginationContainer.style.display = 'none';
        return;
    }
    
    let displayData = filteredData;
    let totalPages = 1;
    if (itemsPerPage > 0) {
        totalPages = Math.ceil(filteredData.length / itemsPerPage);
        const start = (currentPage - 1) * itemsPerPage;
        const end = start + itemsPerPage;
        displayData = filteredData.slice(start, end);
    }
    
    // Сохраняем все отфильтрованные данные для доступа из карточки препарата
    window.summaryData = { summary: filteredData };
    
    if (isCardsView) {
        displayCards(displayData);
    } else {
        displaySummary({ summary: displayData });
    }
    
    if (itemsPerPage > 0 && totalPages > 1) {
        displayPagination(totalPages);
    } else {
        const paginationContainer = document.getElementById('paginationContainer');
        if (paginationContainer) paginationContainer.style.display = 'none';
    }
}

function displayCards(data) {
    const container = document.getElementById('summaryTableContainer');
    let html = '<div class="cards-view">';
    const startIndex = (currentPage - 1) * itemsPerPage;
    data.forEach((price, pageIndex) => {
        const globalIndex = startIndex + pageIndex;
        const originalIndex = currentSummaryData.summary.findIndex(p => 
            p.supplier_price_id === price.supplier_price_id ||
            (p.drug_name === price.drug_name && p.price === price.price)
        );
        const cardIndex = originalIndex !== -1 ? originalIndex : globalIndex;
        // Добавляем класс для выделения клавиатурой
        const selectedClass = globalIndex === currentSelectedIndex ? ' keyboard-selected' : '';
        html += `<div class="price-card${selectedClass}" data-index="${globalIndex}">
            <div class="price-card-header">
                <div class="price-card-title">${escapeHtml(price.drug_name || '-')}</div>
                <div class="price-card-price">${formatPrice(price.price)}</div>
            </div>
            <div class="price-card-info">
                ${price.inn ? `<div class="price-card-info-item">💊 МНН: ${escapeHtml(price.inn)}</div>` : ''}
                ${price.cure_form ? `<div class="price-card-info-item">📦 Форма: ${escapeHtml(price.cure_form)}</div>` : ''}
                ${price.supplier_name ? `<div class="price-card-info-item">🏢 Поставщик: ${escapeHtml(price.supplier_name)}</div>` : ''}
                ${price.quantity != null && price.quantity !== undefined ? `<div class="price-card-info-item">📊 Количество: ${price.quantity.toFixed(3)}</div>` : ''}
                ${price.expiry_date ? `<div class="price-card-info-item">📅 Срок годности: ${new Date(price.expiry_date).toLocaleDateString('ru-RU')}</div>` : ''}
                ${price.producer_name ? `<div class="price-card-info-item">🏭 Производитель: ${escapeHtml(price.producer_name)}</div>` : ''}
                ${/* Базовая цена и наценка не передаются клиенту - расчеты только на сервере */''}
            </div>
            <div style="margin-top: 15px; text-align: center;">
                <button onclick="showDrugCard(${globalIndex})" class="drug-card-button">📋 Карточка препарата</button>
            </div>
        </div>`;
    });
    html += '</div>';
    container.innerHTML = html;
    
    // Обновляем выделение после отрисовки
    setTimeout(() => {
        updateSelection();
    }, 100);
}

function displayPagination(totalPages) {
    const container = document.getElementById('paginationContainer');
    const info = document.getElementById('paginationInfo');
    const controls = document.getElementById('paginationControls');
    if (!container || !info || !controls) return;
    container.style.display = 'flex';
    const start = (currentPage - 1) * itemsPerPage + 1;
    const end = Math.min(currentPage * itemsPerPage, filteredData.length);
    info.textContent = `Показано ${start}-${end} из ${filteredData.length} позиций`;
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
    if (!filteredData || filteredData.length === 0) return;
    const totalPages = itemsPerPage > 0 ? Math.ceil(filteredData.length / itemsPerPage) : 1;
    if (page > totalPages) return;
    currentPage = page;
    saveState();
    // Не сбрасываем страницу при переходе - передаем resetPage = false
    applyFilters(false);
    window.scrollTo({ top: 0, behavior: 'smooth' });
}

function exportToExcel() {
    if (!filteredData || filteredData.length === 0) {
        showError('Нет данных для экспорта');
        return;
    }
    const headers = ['Препарат', 'МНН', 'Форма', 'Поставщик', 'Цена', 'Количество', 'Срок годности', 'Производитель', 'Цена реестра'];
    const rows = filteredData.map(p => [
        p.drug_name || '', 
        p.inn || '', 
        p.cure_form || '', 
        p.supplier_name || '',
        formatPrice(p.price), 
        p.quantity != null && p.quantity !== undefined ? p.quantity.toFixed(3) : '',
        p.expiry_date ? new Date(p.expiry_date).toLocaleDateString('ru-RU') : '',
        p.producer_name || '', 
        formatPrice(p.registry_price)
    ]);
    const csv = [headers.join(';'), ...rows.map(r => r.map(cell => `"${String(cell).replace(/"/g, '""')}"`).join(';'))].join('\n');
    const BOM = '\uFEFF';
    const dataBlob = new Blob([BOM + csv], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(dataBlob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `price-summary-${new Date().toISOString().split('T')[0]}.csv`;
    link.click();
    URL.revokeObjectURL(url);
    showSuccess('Экспорт в Excel (CSV) выполнен успешно');
}

function exportToJSON() {
    if (!filteredData || filteredData.length === 0) {
        if (!currentSummaryData) {
            showError('Сначала загрузите сводный прайс');
            return;
        }
        showError('Нет данных для экспорта');
        return;
    }
    
    const dataStr = JSON.stringify(filteredData, null, 2);
    const dataBlob = new Blob([dataStr], { type: 'application/json' });
    const url = URL.createObjectURL(dataBlob);
    
    const link = document.createElement('a');
    link.href = url;
    link.download = `price-summary-${new Date().toISOString().split('T')[0]}.json`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
    
    showSuccess('Сводный прайс экспортирован');
}

function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Форматирование даты в читаемый формат
function formatDate(dateValue) {
    if (!dateValue) return '-';
    
    try {
        const date = new Date(dateValue);
        if (isNaN(date.getTime())) {
            // Если не удалось распарсить как дату, возвращаем как есть
            return String(dateValue);
        }
        return date.toLocaleDateString('ru-RU', {
            year: 'numeric',
            month: '2-digit',
            day: '2-digit'
        });
    } catch (error) {
        // В случае ошибки возвращаем исходное значение
        return String(dateValue);
    }
}

// Форматирование цены с разделителями тысяч (без символа валюты)
function formatPrice(priceValue) {
    if (priceValue == null || priceValue === undefined || priceValue === '') return '-';
    
    try {
        const price = typeof priceValue === 'number' ? priceValue : parseFloat(priceValue);
        if (isNaN(price)) return '-';
        
        // Форматируем с разделителями тысяч и 2 знаками после запятой
        return price.toLocaleString('ru-RU', {
            minimumFractionDigits: 2,
            maximumFractionDigits: 2
        });
    } catch (error) {
        return String(priceValue);
    }
}

// Функции showError и showSuccess теперь определены в toast.js

function showDrugCard(index) {
    if (!window.summaryData || !window.summaryData.summary || !window.summaryData.summary[index]) {
        showError('Данные не найдены');
        return;
    }
    
    const price = window.summaryData.summary[index];
    
    // Сначала показываем карточку сразу, без ожидания загрузки аналогов/синонимов
    let cardHtml = `
        <div id="drugCardModal">
            <div class="drug-card-wrapper">
                <!-- Фиксированный заголовок -->
                <div class="drug-card-header-fixed">
                    <h2>💊 ${escapeHtml(price.drug_name || 'Препарат')}</h2>
                    <button onclick="closeDrugCard()" title="Закрыть">✕</button>
                </div>
                <!-- Скроллируемая область контента -->
                <div class="drug-card-content">
                    <!-- Табы для навигации -->
                    <div class="drug-card-tabs">
                        <button class="drug-card-tab active" onclick="switchDrugTab('main')">📋 Основное</button>
                        <button class="drug-card-tab" onclick="switchDrugTab('price')">💰 Цены</button>
                        <button class="drug-card-tab" onclick="switchDrugTab('additional')">ℹ️ Дополнительно</button>
                        <button class="drug-card-tab" onclick="switchDrugTab('supplier')">🏢 Поставщик</button>
                        <button class="drug-card-tab" onclick="switchDrugTab('related')">🔗 Связанные</button>
                    </div>
                    
                    <!-- Вкладка: Основное -->
                    <div id="drugTabMain" class="drug-card-tab-content active">
                        <div class="drug-info-grid">
                            ${price.trade_name ? `<div class="drug-info-item">
                                <span class="drug-info-icon">🏷️</span>
                                <div class="drug-info-content">
                                    <div class="drug-info-label">Торговое наименование</div>
                                    <div class="drug-info-value">${escapeHtml(price.trade_name)}</div>
                                </div>
                            </div>` : ''}
                            ${price.inn ? `<div class="drug-info-item">
                                <span class="drug-info-icon">🧬</span>
                                <div class="drug-info-content">
                                    <div class="drug-info-label">МНН</div>
                                    <div class="drug-info-value">${escapeHtml(price.inn)}</div>
                                </div>
                            </div>` : ''}
                            ${price.cure_form ? `<div class="drug-info-item">
                                <span class="drug-info-icon">📦</span>
                                <div class="drug-info-content">
                                    <div class="drug-info-label">Лекарственная форма</div>
                                    <div class="drug-info-value">${escapeHtml(price.cure_form)}</div>
                                </div>
                            </div>` : ''}
                            ${price.dosage ? `<div class="drug-info-item">
                                <span class="drug-info-icon">💉</span>
                                <div class="drug-info-content">
                                    <div class="drug-info-label">Дозировка</div>
                                    <div class="drug-info-value">${escapeHtml(price.dosage)}</div>
                                </div>
                            </div>` : ''}
                            ${price.producer_name ? `<div class="drug-info-item">
                                <span class="drug-info-icon">🏭</span>
                                <div class="drug-info-content">
                                    <div class="drug-info-label">Производитель</div>
                                    <div class="drug-info-value">${escapeHtml(price.producer_name)}</div>
                                </div>
                            </div>` : ''}
                            ${price.barcode ? `<div class="drug-info-item">
                                <span class="drug-info-icon">📊</span>
                                <div class="drug-info-content">
                                    <div class="drug-info-label">Штрихкод</div>
                                    <div class="drug-info-value">${escapeHtml(price.barcode)}</div>
                                </div>
                            </div>` : ''}
                        </div>
                    </div>
                    
                    <!-- Вкладка: Цены -->
                    <div id="drugTabPrice" class="drug-card-tab-content">
                        ${price.price || price.registry_price ? `<div class="drug-price-grid">
                            ${price.price ? `<div class="drug-card-price-item">
                                <div class="drug-card-price-label">Цена</div>
                                <div class="drug-card-price-value">${formatPrice(price.price)}</div>
                            </div>` : ''}
                            ${price.registry_price ? `<div class="drug-card-price-item" style="background: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%); box-shadow: 0 8px 24px rgba(59, 130, 246, 0.3);">
                                <div class="drug-card-price-label">Цена реестра</div>
                                <div class="drug-card-price-value">${formatPrice(price.registry_price)}</div>
                            </div>` : ''}
                        </div>` : '<div class="drug-empty-state">Информация о ценах отсутствует</div>'}
                    </div>
                    
                    <!-- Вкладка: Дополнительно -->
                    <div id="drugTabAdditional" class="drug-card-tab-content">
                        ${price.description || price.storing_condition || price.expiry_period || price.instruction_guid || price.registry_date || price.registry_status ? `<div class="drug-info-grid">
                            ${price.description ? `<div class="drug-info-item full-width">
                                <span class="drug-info-icon">📝</span>
                                <div class="drug-info-content">
                                    <div class="drug-info-label">Описание</div>
                                    <div class="drug-info-value">${escapeHtml(price.description)}</div>
                                </div>
                            </div>` : ''}
                            ${price.storing_condition ? `<div class="drug-info-item">
                                <span class="drug-info-icon">❄️</span>
                                <div class="drug-info-content">
                                    <div class="drug-info-label">Условия хранения</div>
                                    <div class="drug-info-value">${escapeHtml(price.storing_condition)}</div>
                                </div>
                            </div>` : ''}
                            ${price.expiry_period ? `<div class="drug-info-item">
                                <span class="drug-info-icon">⏰</span>
                                <div class="drug-info-content">
                                    <div class="drug-info-label">Срок годности</div>
                                    <div class="drug-info-value">${escapeHtml(price.expiry_period)}</div>
                                </div>
                            </div>` : ''}
                            ${price.instruction_guid ? `<div class="drug-info-item full-width">
                                <span class="drug-info-icon">📄</span>
                                <div class="drug-info-content">
                                    <div class="drug-info-label">Инструкция</div>
                                    <button onclick="showInstruction('${escapeHtml(price.instruction_guid)}')" class="btn btn-sm" style="margin-top: 8px; background: #28a745; color: white; padding: 8px 16px;">
                                        📄 Показать инструкцию
                                    </button>
                                </div>
                            </div>` : ''}
                            ${price.registry_date ? `<div class="drug-info-item">
                                <span class="drug-info-icon">📅</span>
                                <div class="drug-info-content">
                                    <div class="drug-info-label">Дата регистрации</div>
                                    <div class="drug-info-value">${formatDate(price.registry_date)}</div>
                                </div>
                            </div>` : ''}
                            ${price.registry_status ? `<div class="drug-info-item">
                                <span class="drug-info-icon">✅</span>
                                <div class="drug-info-content">
                                    <div class="drug-info-label">Статус регистрации</div>
                                    <div class="drug-info-value">${escapeHtml(price.registry_status)}</div>
                                </div>
                            </div>` : ''}
                        </div>` : '<div class="drug-empty-state">Дополнительная информация отсутствует</div>'}
                    </div>
                    
                    <!-- Вкладка: Поставщик -->
                    <div id="drugTabSupplier" class="drug-card-tab-content">
                        ${(() => {
                            // Проверяем наличие информации о поставщике: имя поставщика, количество, срок годности, цена
                            const hasSupplierInfo = price.supplier_name || 
                                (price.quantity != null && price.quantity !== undefined) || 
                                price.expiry_date || 
                                price.price || 
                                price.batch_number;
                            
                            if (!hasSupplierInfo) {
                                return '<div class="drug-empty-state">Информация о поставщике отсутствует</div>';
                            }
                            
                            return `<div class="drug-info-grid">
                                ${price.supplier_name ? `<div class="drug-info-item">
                                    <span class="drug-info-icon">🏢</span>
                                    <div class="drug-info-content">
                                        <div class="drug-info-label">Поставщик</div>
                                        <div class="drug-info-value">${escapeHtml(price.supplier_name)}</div>
                                    </div>
                                </div>` : ''}
                                ${price.quantity != null && price.quantity !== undefined ? `<div class="drug-info-item">
                                    <span class="drug-info-icon">📊</span>
                                    <div class="drug-info-content">
                                        <div class="drug-info-label">Количество</div>
                                        <div class="drug-info-value">${price.quantity.toFixed(3)}</div>
                                    </div>
                                </div>` : ''}
                                ${price.expiry_date ? `<div class="drug-info-item">
                                    <span class="drug-info-icon">📅</span>
                                    <div class="drug-info-content">
                                        <div class="drug-info-label">Срок годности</div>
                                        <div class="drug-info-value">${new Date(price.expiry_date).toLocaleDateString('ru-RU')}</div>
                                    </div>
                                </div>` : ''}
                                ${price.batch_number ? `<div class="drug-info-item">
                                    <span class="drug-info-icon">🏷️</span>
                                    <div class="drug-info-content">
                                        <div class="drug-info-label">Номер партии</div>
                                        <div class="drug-info-value">${escapeHtml(price.batch_number)}</div>
                                    </div>
                                </div>` : ''}
                                ${price.price ? `<div class="drug-info-item">
                                    <span class="drug-info-icon">💰</span>
                                    <div class="drug-info-content">
                                        <div class="drug-info-label">Цена</div>
                                        <div class="drug-info-value">${formatPrice(price.price)}</div>
                                    </div>
                                </div>` : ''}
                            </div>`;
                        })()}
                    </div>
                    
                    <!-- Вкладка: Связанные -->
                    <div id="drugTabRelated" class="drug-card-tab-content">
                        ${(() => {
                            const guidES = price.guid_es || price.GUID_ES || '';
                            return `<div class="drug-related-section">
                                <div class="drug-related-item">
                                    <div class="drug-related-header">
                                        <span class="drug-related-icon">💊</span>
                                        <strong>Аналоги</strong>
                                        <span id="analogsLoading" style="color: var(--color-primary); font-size: 0.85em; font-weight: normal; margin-left: 8px;"></span>
                                    </div>
                                    <div id="analogsContent" style="padding: 20px; text-align: center;">
                                        <button id="loadAnalogsBtn" onclick="loadDrugAnalogs('${guidES}')" class="drug-card-button">
                                            🔍 Загрузить аналоги
                                        </button>
                                    </div>
                                </div>
                                <div class="drug-related-item">
                                    <div class="drug-related-header">
                                        <span class="drug-related-icon">📝</span>
                                        <strong>Синонимы</strong>
                                        <span id="synonymsLoading" style="color: var(--color-primary); font-size: 0.85em; font-weight: normal; margin-left: 8px;"></span>
                                    </div>
                                    <div id="synonymsContent" style="padding: 20px; text-align: center;">
                                        <button id="loadSynonymsBtn" onclick="loadDrugSynonyms('${guidES}')" class="drug-card-button">
                                            🔍 Загрузить синонимы
                                        </button>
                                    </div>
                                </div>
                            </div>`;
                        })()}
                    </div>
                </div>
            </div>
        </div>
    `;
    
    // Удаляем предыдущее модальное окно, если есть
    const existingModal = document.getElementById('drugCardModal');
    if (existingModal) {
        existingModal.remove();
    }
    
    document.body.insertAdjacentHTML('beforeend', cardHtml);
    
    // Добавляем индикатор прокрутки и улучшаем UX скролла
    const contentDiv = document.querySelector('.drug-card-content');
    if (contentDiv) {
        let scrollTimeout;
        contentDiv.addEventListener('scroll', () => {
            contentDiv.classList.add('scrolling');
            clearTimeout(scrollTimeout);
            scrollTimeout = setTimeout(() => {
                contentDiv.classList.remove('scrolling');
            }, 500);
        });
        
        // Плавная прокрутка при загрузке
        setTimeout(() => {
            contentDiv.scrollTop = 0;
        }, 100);
    }
    
    // Функция переключения табов
    window.switchDrugTab = function(tabName) {
        // Скрываем все табы
        document.querySelectorAll('.drug-card-tab-content').forEach(tab => {
            tab.classList.remove('active');
        });
        document.querySelectorAll('.drug-card-tab').forEach(tab => {
            tab.classList.remove('active');
        });
        
        // Маппинг названий табов
        const tabMap = {
            'main': 'Main',
            'price': 'Price',
            'additional': 'Additional',
            'supplier': 'Supplier',
            'related': 'Related'
        };
        
        // Показываем выбранный таб
        const tabId = `drugTab${tabMap[tabName] || tabName.charAt(0).toUpperCase() + tabName.slice(1)}`;
        const selectedTab = document.getElementById(tabId);
        if (selectedTab) {
            selectedTab.classList.add('active');
        }
        
        // Активируем кнопку таба
        const tabButtons = document.querySelectorAll('.drug-card-tab');
        const tabs = ['main', 'price', 'additional', 'supplier', 'related'];
        tabButtons.forEach((btn, index) => {
            if (tabs[index] === tabName) {
                btn.classList.add('active');
            }
        });
    };
    
    // Аналоги и синонимы теперь загружаются только по клику на кнопки
}

// Загрузка аналогов по запросу (вызывается по клику на кнопку)
// Делаем функцию глобальной для вызова через onclick
window.loadDrugAnalogs = async function(guidES) {
    if (!guidES) {
        console.warn('GUID_ES не указан для загрузки аналогов');
        updateAnalogsSection([], 'GUID_ES не указан');
        return;
    }
    
    const loadingSpan = document.getElementById('analogsLoading');
    const contentDiv = document.getElementById('analogsContent');
    const loadBtn = document.getElementById('loadAnalogsBtn');
    
    if (!loadingSpan || !contentDiv) {
        console.error('Элементы для отображения аналогов не найдены');
        return;
    }
    
    // Показываем индикатор загрузки и скрываем кнопку
    if (loadBtn) {
        loadBtn.style.display = 'none';
    }
    loadingSpan.textContent = '⏳ Загрузка...';
    contentDiv.innerHTML = '<div style="padding: 15px; color: #666; font-style: italic; text-align: center; margin-top: 10px;">Загрузка данных...</div>';
    
    try {
        const response = await AuthManager.authenticatedFetch(`/api/drug/analogs-synonyms?guid_es=${encodeURIComponent(guidES)}`);
        if (response.ok) {
            const data = await response.json();
            const analogs = data.analogs || [];
            
            console.log('Загружено аналогов:', analogs.length);
            
            // Обновляем секцию аналогов
            updateAnalogsSection(analogs);
        } else {
            const errorText = await response.text();
            console.error('Ошибка API аналогов:', response.status, errorText);
            updateAnalogsSection([], 'Ошибка загрузки данных');
        }
    } catch (error) {
        console.error('Ошибка загрузки аналогов:', error);
        updateAnalogsSection([], 'Ошибка загрузки данных');
    }
};

// Загрузка синонимов по запросу (вызывается по клику на кнопку)
// Делаем функцию глобальной для вызова через onclick
window.loadDrugSynonyms = async function(guidES) {
    if (!guidES) {
        console.warn('GUID_ES не указан для загрузки синонимов');
        updateSynonymsSection([], 'GUID_ES не указан');
        return;
    }
    
    const loadingSpan = document.getElementById('synonymsLoading');
    const contentDiv = document.getElementById('synonymsContent');
    const loadBtn = document.getElementById('loadSynonymsBtn');
    
    if (!loadingSpan || !contentDiv) {
        console.error('Элементы для отображения синонимов не найдены');
        return;
    }
    
    // Показываем индикатор загрузки и скрываем кнопку
    if (loadBtn) {
        loadBtn.style.display = 'none';
    }
    loadingSpan.textContent = '⏳ Загрузка...';
    contentDiv.innerHTML = '<div style="padding: 15px; color: #666; font-style: italic; text-align: center; margin-top: 10px;">Загрузка данных...</div>';
    
    try {
        const response = await AuthManager.authenticatedFetch(`/api/drug/analogs-synonyms?guid_es=${encodeURIComponent(guidES)}`);
        if (response.ok) {
            const data = await response.json();
            const synonyms = data.synonyms || [];
            
            console.log('Загружено синонимов:', synonyms.length);
            
            // Обновляем секцию синонимов
            updateSynonymsSection(synonyms);
        } else {
            const errorText = await response.text();
            console.error('Ошибка API синонимов:', response.status, errorText);
            updateSynonymsSection([], 'Ошибка загрузки данных');
        }
    } catch (error) {
        console.error('Ошибка загрузки синонимов:', error);
        updateSynonymsSection([], 'Ошибка загрузки данных');
    }
};

// Обновление секции аналогов
function updateAnalogsSection(analogs, errorMessage = null) {
    const loadingSpan = document.getElementById('analogsLoading');
    const contentDiv = document.getElementById('analogsContent');
    
    if (!loadingSpan || !contentDiv) return;
    
    if (errorMessage) {
        loadingSpan.textContent = '';
        contentDiv.innerHTML = `<div style="padding: 10px; color: #dc3545; font-style: italic;">${escapeHtml(errorMessage)}</div>`;
        return;
    }
    
    loadingSpan.textContent = analogs.length > 0 ? `(${analogs.length})` : '(не найдено)';
    
    // Скрываем кнопку загрузки после получения данных
    const loadBtn = document.getElementById('loadAnalogsBtn');
    if (loadBtn) {
        loadBtn.style.display = 'none';
    }
    
    if (analogs.length > 0) {
        let html = `<div style="max-height: 250px; overflow-y: auto; border: 1px solid #e0e0e0; border-radius: 8px; padding: 10px; background: #f8f9fa;" class="analogs-scroll-container">`;
        analogs.forEach(drug => {
            const priceBadge = drug.has_price ? '<span style="background: #28a745; color: white; padding: 2px 6px; border-radius: 3px; font-size: 0.8em; margin-left: 5px;">✓ В прайсе</span>' : '<span style="background: #6c757d; color: white; padding: 2px 6px; border-radius: 3px; font-size: 0.8em; margin-left: 5px;">Нет в прайсе</span>';
            const clickableStyle = drug.has_price ? 'cursor: pointer; background: #f8f9fa; border-left: 3px solid #28a745;' : '';
            const onClick = drug.has_price ? `onclick="navigateToDrugInTable('${drug.guid_es}')"` : '';
            const hoverStyle = drug.has_price ? 'onmouseover="this.style.background=\'#e9ecef\'" onmouseout="this.style.background=\'#f8f9fa\'"' : '';
            html += `<div style="padding: 8px; margin-bottom: 5px; border-bottom: 1px solid #f0f0f0; ${clickableStyle}" ${onClick} ${hoverStyle}>`;
            html += `<strong>${escapeHtml(drug.name || '-')}</strong>${priceBadge}`;
            if (drug.has_price) {
                html += ` <span style="color: #007bff; font-size: 0.9em; margin-left: 5px;">🔗 Перейти</span>`;
            }
            html += `<br>`;
            if (drug.trade_name) {
                html += `<small>Торг. название: ${escapeHtml(drug.trade_name)}</small><br>`;
            }
            if (drug.producer_name) {
                html += `<small>Производитель: ${escapeHtml(drug.producer_name)}</small><br>`;
            }
            if (drug.cure_form) {
                html += `<small>Форма: ${escapeHtml(drug.cure_form)}</small>`;
            }
            if (drug.registry_price) {
                html += `<small style="color: #007bff;"> | Реестр: ${formatPrice(drug.registry_price)}</small>`;
            }
            html += `</div>`;
        });
        html += `</div>`;
        contentDiv.innerHTML = html;
    } else {
        contentDiv.innerHTML = `<div style="padding: 10px; color: #666; font-style: italic;">Аналоги не найдены (препараты с таким же МНН)</div>`;
    }
}

// Обновление секции синонимов
function updateSynonymsSection(synonyms, errorMessage = null) {
    const loadingSpan = document.getElementById('synonymsLoading');
    const contentDiv = document.getElementById('synonymsContent');
    
    if (!loadingSpan || !contentDiv) return;
    
    if (errorMessage) {
        loadingSpan.textContent = '';
        contentDiv.innerHTML = `<div style="padding: 10px; color: #dc3545; font-style: italic;">${escapeHtml(errorMessage)}</div>`;
        return;
    }
    
    loadingSpan.textContent = synonyms.length > 0 ? `(${synonyms.length})` : '(не найдено)';
    
    // Скрываем кнопку загрузки после получения данных
    const loadBtn = document.getElementById('loadSynonymsBtn');
    if (loadBtn) {
        loadBtn.style.display = 'none';
    }
    
    if (synonyms.length > 0) {
        let html = `<div style="max-height: 250px; overflow-y: auto; border: 1px solid #e0e0e0; border-radius: 8px; padding: 10px; background: #f8f9fa;" class="synonyms-scroll-container">`;
        synonyms.forEach(drug => {
            const priceBadge = drug.has_price ? '<span style="background: #28a745; color: white; padding: 2px 6px; border-radius: 3px; font-size: 0.8em; margin-left: 5px;">✓ В прайсе</span>' : '<span style="background: #6c757d; color: white; padding: 2px 6px; border-radius: 3px; font-size: 0.8em; margin-left: 5px;">Нет в прайсе</span>';
            const clickableStyle = drug.has_price ? 'cursor: pointer; background: #f8f9fa; border-left: 3px solid #28a745;' : '';
            const onClick = drug.has_price ? `onclick="navigateToDrugInTable('${drug.guid_es}')"` : '';
            const hoverStyle = drug.has_price ? 'onmouseover="this.style.background=\'#e9ecef\'" onmouseout="this.style.background=\'#f8f9fa\'"' : '';
            html += `<div style="padding: 8px; margin-bottom: 5px; border-bottom: 1px solid #f0f0f0; ${clickableStyle}" ${onClick} ${hoverStyle}>`;
            html += `<strong>${escapeHtml(drug.name || '-')}</strong>${priceBadge}`;
            if (drug.has_price) {
                html += ` <span style="color: #007bff; font-size: 0.9em; margin-left: 5px;">🔗 Перейти</span>`;
            }
            html += `<br>`;
            if (drug.trade_name) {
                html += `<small>Торг. название: ${escapeHtml(drug.trade_name)}</small><br>`;
            }
            if (drug.producer_name) {
                html += `<small>Производитель: ${escapeHtml(drug.producer_name)}</small><br>`;
            }
            if (drug.cure_form) {
                html += `<small>Форма: ${escapeHtml(drug.cure_form)}</small>`;
            }
            if (drug.registry_price) {
                html += `<small style="color: #007bff;"> | Реестр: ${formatPrice(drug.registry_price)}</small>`;
            }
            html += `</div>`;
        });
        html += `</div>`;
        contentDiv.innerHTML = html;
    } else {
        contentDiv.innerHTML = `<div style="padding: 10px; color: #666; font-style: italic;">Синонимы не найдены (похожие торговые наименования)</div>`;
    }
}

function closeDrugCard() {
    const modal = document.getElementById('drugCardModal');
    if (modal) {
        modal.remove();
    }
}

// Переход к позиции в таблице по GUID_ES
function navigateToDrugInTable(guidES) {
    // Закрываем карточку
    closeDrugCard();
    
    // Находим таблицу
    const table = document.querySelector('#summaryTable tbody');
    if (!table) {
        showError('Таблица не найдена');
        return;
    }
    
    // Ищем строку с нужным GUID_ES в данных
    if (!window.summaryData || !window.summaryData.summary) {
        showError('Данные не загружены');
        return;
    }
    
    // Находим индекс позиции в данных
    const index = window.summaryData.summary.findIndex(p => 
        (p.guid_es && p.guid_es.toLowerCase() === guidES.toLowerCase()) ||
        (p.GUID_ES && p.GUID_ES.toLowerCase() === guidES.toLowerCase())
    );
    
    if (index === -1) {
        showError('Позиция не найдена в таблице');
        return;
    }
    
    // Находим строку в таблице (строки начинаются с индекса 0)
    const rows = table.querySelectorAll('tr');
    if (index >= rows.length) {
        showError('Строка не найдена');
        return;
    }
    
    const targetRow = rows[index];
    
    // Подсвечиваем строку
    targetRow.style.background = '#fff3cd';
    targetRow.style.transition = 'background 0.3s';
    
    // Прокручиваем к строке
    targetRow.scrollIntoView({ behavior: 'smooth', block: 'center' });
    
    // Убираем подсветку через 3 секунды
    setTimeout(() => {
        targetRow.style.background = '';
        setTimeout(() => {
            targetRow.style.transition = '';
        }, 300);
    }, 3000);
    
    // Показываем сообщение
    showSuccess(`Переход к позиции: ${window.summaryData.summary[index].drug_name || 'Неизвестно'}`);
}

// Закрытие по клику вне модального окна
window.addEventListener('click', function(event) {
    const modal = document.getElementById('drugCardModal');
    if (modal && event.target === modal) {
        closeDrugCard();
    }
});

// Показать инструкцию по применению
async function showInstruction(instructionGUID) {
    if (!instructionGUID) {
        showError('GUID инструкции не указан');
        return;
    }

    showLoading('Загрузка инструкции...');

    try {
        const response = await AuthManager.authenticatedFetch(`/api/instruction?guid=${encodeURIComponent(instructionGUID)}`);
        
        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Ошибка загрузки инструкции');
        }

        const instruction = await response.json();
        displayInstructionModal(instruction);
    } catch (error) {
        showError(`Ошибка загрузки инструкции: ${error.message}`);
    } finally {
        hideLoading();
    }
}

// Отображение модального окна с инструкцией
function displayInstructionModal(instruction) {
    const modalHTML = `
        <div id="instructionModal" style="position: fixed; top: 0; left: 0; width: 100%; height: 100%; background: rgba(0,0,0,0.7); z-index: 10000; overflow-y: auto;">
            <div style="max-width: 900px; margin: 20px auto; background: white; border-radius: 12px; box-shadow: 0 10px 40px rgba(0,0,0,0.3); position: relative;">
                <!-- Заголовок -->
                <div style="position: sticky; top: 0; background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); color: white; padding: 20px 30px; border-radius: 12px 12px 0 0; display: flex; justify-content: space-between; align-items: center; z-index: 10001;">
                    <h2 style="margin: 0; font-size: 1.5em;">📄 Инструкция по применению</h2>
                    <div>
                        <button onclick="printInstruction()" class="btn" style="background: rgba(255,255,255,0.2); color: white; border: 1px solid rgba(255,255,255,0.3); margin-right: 10px; padding: 8px 16px;">
                            🖨️ Печать
                        </button>
                        <button onclick="closeInstructionModal()" style="background: rgba(255,255,255,0.2); color: white; border: 1px solid rgba(255,255,255,0.3); padding: 8px 16px; border-radius: 6px; cursor: pointer; font-size: 1.2em;">
                            ✕
                        </button>
                    </div>
                </div>
                
                <!-- Контент инструкции -->
                <div id="instructionContent" style="padding: 30px; line-height: 1.8; color: #333;">
                    ${instruction.composition ? `
                        <div class="instruction-section">
                            <h3 class="instruction-section-title">📋 Состав</h3>
                            <div class="instruction-section-content">${formatInstructionText(instruction.composition)}</div>
                        </div>
                    ` : ''}
                    
                    ${instruction.goods_desc ? `
                        <div class="instruction-section">
                            <h3 class="instruction-section-title">📦 Описание</h3>
                            <div class="instruction-section-content">${formatInstructionText(instruction.goods_desc)}</div>
                        </div>
                    ` : ''}
                    
                    ${instruction.pharm_action ? `
                        <div class="instruction-section">
                            <h3 class="instruction-section-title">💊 Фармакологическое действие</h3>
                            <div class="instruction-section-content">${formatInstructionText(instruction.pharm_action)}</div>
                        </div>
                    ` : ''}
                    
                    ${instruction.indication ? `
                        <div class="instruction-section">
                            <h3 class="instruction-section-title">🎯 Показания к применению</h3>
                            <div class="instruction-section-content">${formatInstructionText(instruction.indication)}</div>
                        </div>
                    ` : ''}
                    
                    ${instruction.contra_indication ? `
                        <div class="instruction-section">
                            <h3 class="instruction-section-title">⚠️ Противопоказания</h3>
                            <div class="instruction-section-content">${formatInstructionText(instruction.contra_indication)}</div>
                        </div>
                    ` : ''}
                    
                    ${instruction.dosage ? `
                        <div class="instruction-section">
                            <h3 class="instruction-section-title">💉 Способ применения и дозы</h3>
                            <div class="instruction-section-content">${formatInstructionText(instruction.dosage)}</div>
                        </div>
                    ` : ''}
                    
                    ${instruction.side_effect ? `
                        <div class="instruction-section">
                            <h3 class="instruction-section-title">⚡ Побочные действия</h3>
                            <div class="instruction-section-content">${formatInstructionText(instruction.side_effect)}</div>
                        </div>
                    ` : ''}
                    
                    ${instruction.interaction ? `
                        <div class="instruction-section">
                            <h3 class="instruction-section-title">🔗 Взаимодействие с другими лекарственными средствами</h3>
                            <div class="instruction-section-content">${formatInstructionText(instruction.interaction)}</div>
                        </div>
                    ` : ''}
                    
                    ${instruction.over_dosage ? `
                        <div class="instruction-section">
                            <h3 class="instruction-section-title">🚨 Передозировка</h3>
                            <div class="instruction-section-content">${formatInstructionText(instruction.over_dosage)}</div>
                        </div>
                    ` : ''}
                    
                    ${instruction.special_instruction ? `
                        <div class="instruction-section">
                            <h3 class="instruction-section-title">ℹ️ Особые указания</h3>
                            <div class="instruction-section-content">${formatInstructionText(instruction.special_instruction)}</div>
                        </div>
                    ` : ''}
                    
                    ${instruction.lactation ? `
                        <div class="instruction-section">
                            <h3 class="instruction-section-title">🤱 Применение при беременности и в период грудного вскармливания</h3>
                            <div class="instruction-section-content">${formatInstructionText(instruction.lactation)}</div>
                        </div>
                    ` : ''}
                    
                    ${instruction.storing_condition ? `
                        <div class="instruction-section">
                            <h3 class="instruction-section-title">❄️ Условия хранения</h3>
                            <div class="instruction-section-content">${formatInstructionText(instruction.storing_condition)}</div>
                        </div>
                    ` : ''}
                    
                    ${instruction.instruction ? `
                        <div class="instruction-section">
                            <h3 class="instruction-section-title">📄 Полная инструкция</h3>
                            <div class="instruction-section-content">${formatInstructionText(instruction.instruction)}</div>
                        </div>
                    ` : ''}
                    
                    ${!instruction.composition && !instruction.goods_desc && !instruction.pharm_action && 
                      !instruction.indication && !instruction.dosage && !instruction.contra_indication && 
                      !instruction.side_effect && !instruction.interaction && !instruction.over_dosage && 
                      !instruction.special_instruction && !instruction.lactation && !instruction.storing_condition && 
                      !instruction.instruction ? `
                        <div style="text-align: center; padding: 40px; color: #666;">
                            <p>Информация в инструкции отсутствует</p>
                        </div>
                    ` : ''}
                </div>
            </div>
        </div>
    `;

    // Удаляем существующее модальное окно, если есть
    const existingModal = document.getElementById('instructionModal');
    if (existingModal) {
        existingModal.remove();
    }

    document.body.insertAdjacentHTML('beforeend', modalHTML);
    
    // Сохраняем данные инструкции для печати
    window.currentInstruction = instruction;
}

// Форматирование текста инструкции (обработка HTML и переносов строк)
function formatInstructionText(text) {
    if (!text) return '';
    
    // Проверяем, содержит ли текст HTML-теги
    const hasHTML = /<[a-z][\s\S]*>/i.test(text);
    
    if (hasHTML) {
        // Удаляем опасные теги и атрибуты для безопасности
        let cleanHTML = text
            // Удаляем script, iframe, object, embed
            .replace(/<script[\s\S]*?<\/script>/gi, '')
            .replace(/<iframe[\s\S]*?<\/iframe>/gi, '')
            .replace(/<object[\s\S]*?<\/object>/gi, '')
            .replace(/<embed[\s\S]*?>/gi, '')
            // Удаляем опасные атрибуты (onclick, onerror, etc.)
            .replace(/\s*on\w+\s*=\s*["'][^"']*["']/gi, '')
            .replace(/\s*on\w+\s*=\s*[^\s>]*/gi, '')
            // Нормализуем некоторые теги
            .replace(/<p[^>]*>/gi, '<p style="margin: 10px 0;">')
            .replace(/<br\s*\/?>/gi, '<br>')
            .replace(/<strong[^>]*>/gi, '<strong>')
            .replace(/<b[^>]*>/gi, '<strong>')
            .replace(/<\/b>/gi, '</strong>')
            .replace(/<em[^>]*>/gi, '<em>')
            .replace(/<i[^>]*>/gi, '<em>')
            .replace(/<\/i>/gi, '</em>')
            .replace(/<u[^>]*>/gi, '<u>')
            .replace(/<h[1-6][^>]*>/gi, '<h4 style="margin: 15px 0 10px 0; font-weight: bold;">')
            .replace(/<\/h[1-6]>/gi, '</h4>')
            .replace(/<ul[^>]*>/gi, '<ul style="margin: 10px 0; padding-left: 25px;">')
            .replace(/<ol[^>]*>/gi, '<ol style="margin: 10px 0; padding-left: 25px;">')
            .replace(/<li[^>]*>/gi, '<li style="margin: 5px 0;">')
            .replace(/<table[^>]*>/gi, '<table style="border-collapse: collapse; width: 100%; margin: 10px 0;">')
            .replace(/<td[^>]*>/gi, '<td style="padding: 5px; border: 1px solid #ddd;">')
            .replace(/<th[^>]*>/gi, '<th style="padding: 5px; border: 1px solid #ddd; background: #f0f0f0;">');
        
        // Если после очистки остался валидный HTML, возвращаем его
        // Иначе извлекаем только текст
        const tempDiv = document.createElement('div');
        tempDiv.innerHTML = cleanHTML;
        const textContent = tempDiv.textContent || tempDiv.innerText || '';
        
        // Если извлеченный текст существенно отличается от исходного (много HTML-тегов),
        // значит HTML был валидным и его нужно отобразить
        if (textContent.length < text.length * 0.7) {
            // HTML содержит много разметки, отображаем его
            return cleanHTML;
        } else {
            // В основном текст, отображаем только текст с форматированием
            return textContent.replace(/\r\n/g, '<br>').replace(/\r/g, '<br>').replace(/\n/g, '<br>');
        }
    } else {
        // Если нет HTML, просто экранируем и сохраняем переносы
        const escaped = escapeHtml(text);
        return escaped.replace(/\r\n/g, '<br>').replace(/\r/g, '<br>').replace(/\n/g, '<br>');
    }
}

// Закрыть модальное окно инструкции
function closeInstructionModal() {
    const modal = document.getElementById('instructionModal');
    if (modal) {
        modal.remove();
    }
    window.currentInstruction = null;
}

// Печать инструкции
function printInstruction() {
    const content = document.getElementById('instructionContent');
    if (!content) return;

    const printWindow = window.open('', '_blank');
    printWindow.document.write(`
        <!DOCTYPE html>
        <html lang="ru">
        <head>
            <meta charset="UTF-8">
            <title>Инструкция по применению</title>
            <style>
                @media print {
                    body { margin: 0; padding: 20px; }
                    .no-print { display: none; }
                }
                body {
                    font-family: 'Times New Roman', serif;
                    line-height: 1.6;
                    color: #000;
                    max-width: 800px;
                    margin: 0 auto;
                    padding: 20px;
                }
                .instruction-section {
                    margin-bottom: 25px;
                    page-break-inside: avoid;
                }
                .instruction-section-title {
                    font-size: 1.2em;
                    font-weight: bold;
                    margin-bottom: 10px;
                    color: #333;
                    border-bottom: 2px solid #667eea;
                    padding-bottom: 5px;
                }
                .instruction-section-content {
                    text-align: justify;
                    font-size: 1em;
                }
                h1 {
                    text-align: center;
                    color: #667eea;
                    margin-bottom: 30px;
                }
            </style>
        </head>
        <body>
            <h1>📄 Инструкция по применению</h1>
            ${content.innerHTML}
        </body>
        </html>
    `);
    printWindow.document.close();
    printWindow.focus();
    setTimeout(() => {
        printWindow.print();
        printWindow.close();
    }, 250);
}

// Экспорт функций
window.showDrugCard = showDrugCard;
window.closeDrugCard = closeDrugCard;
window.navigateToDrugInTable = navigateToDrugInTable;
window.sortByColumn = sortByColumn;
window.goToPage = goToPage;
window.clearSearch = clearSearch;
window.showInstruction = showInstruction;
window.closeInstructionModal = closeInstructionModal;
window.printInstruction = printInstruction;

