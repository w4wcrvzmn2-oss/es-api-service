// Скрипт для управления прайсами

let allRegions = [];
let allImportPoints = {};

// Инициализация после загрузки DOM
document.addEventListener('DOMContentLoaded', async () => {
    // Загружаем поставщиков и регионы
    await loadSuppliers();
    await loadRegions();
    
    // Проверяем наличие элементов перед добавлением обработчиков
    const loadBtn = document.getElementById('loadPriceListsBtn');
    if (loadBtn) {
        loadBtn.addEventListener('click', (e) => {
            e.preventDefault();
            loadPriceLists(true); // Показываем уведомление при явной загрузке
        });
        console.log('Обработчик кнопки "Загрузить прайсы" добавлен');
    } else {
        console.error('Кнопка loadPriceListsBtn не найдена!');
    }
    
    const addBtn = document.getElementById('addPriceListBtn');
    if (addBtn) {
        addBtn.addEventListener('click', showAddPriceListForm);
    }
    
    const cancelBtn = document.getElementById('cancelPriceListFormBtn');
    if (cancelBtn) {
        cancelBtn.addEventListener('click', hidePriceListForm);
    }
    
    const form = document.getElementById('priceListForm');
    if (form) {
        form.addEventListener('submit', handlePriceListFormSubmit);
    }
    
    const formSupplier = document.getElementById('formSupplierId');
    if (formSupplier) {
        formSupplier.addEventListener('change', onSupplierChange);
    }
    
    const supplierFilter = document.getElementById('supplierFilter');
    if (supplierFilter) {
        supplierFilter.addEventListener('change', (e) => {
            // Автоматически загружаем прайсы при выборе поставщика (без уведомления)
            loadPriceLists(false);
        });
        console.log('Обработчик фильтра поставщиков добавлен');
    }
    
    // Обработчик для расписания (дублируем на случай если onchange в HTML не сработает)
    const schedulePreset = document.getElementById('formSchedulePreset');
    if (schedulePreset) {
        schedulePreset.addEventListener('change', updateScheduleCron);
        console.log('Обработчик расписания добавлен через addEventListener');
    } else {
        console.error('Элемент formSchedulePreset не найден при инициализации!');
    }
    
    // Инициализация обработчиков для импорта
    // Делаем небольшую задержку, чтобы убедиться что все элементы DOM загружены
    console.log('Планируем вызов initImportHandlers через 500мс...');
    setTimeout(() => {
        console.log('Вызываем initImportHandlers...');
        try {
            initImportHandlers();
        } catch (error) {
            console.error('Ошибка в initImportHandlers:', error);
        }
    }, 500);
    
    // Автоматически загружаем прайсы при загрузке страницы (без уведомления)
    console.log('Автоматическая загрузка прайсов...');
    loadPriceLists(false);
    
    // Загружаем поставщиков для фильтра
    loadSuppliers(true);
});

async function loadSuppliers(forFilter = false) {
    try {
        const response = await AuthManager.authenticatedFetch('/api/suppliers');
        if (!response.ok) {
            if (forFilter) {
                const select = document.getElementById('supplierFilter');
                if (select) select.innerHTML = '<option value="">-- Все поставщики --</option>';
            }
            return;
        }
        
        const suppliers = await response.json();
        const suppliersList = Array.isArray(suppliers) ? suppliers : [];
        
        // Обновляем фильтр (если forFilter = true или всегда)
        const filterSelect = document.getElementById('supplierFilter');
        if (filterSelect) {
            filterSelect.innerHTML = '<option value="">-- Все поставщики --</option>';
            suppliersList.forEach(s => {
                const option = document.createElement('option');
                option.value = s.supplier_id;
                option.textContent = s.name;
                filterSelect.appendChild(option);
            });
        }
        
        // Всегда обновляем список в форме создания прайса
        const formSelect = document.getElementById('formSupplierId');
        if (formSelect) {
            formSelect.innerHTML = '<option value="">-- Выберите поставщика --</option>';
            suppliersList.forEach(s => {
                const option = document.createElement('option');
                option.value = s.supplier_id;
                option.textContent = s.name;
                formSelect.appendChild(option);
            });
        }
        
        console.log(`Загружено поставщиков: ${suppliersList.length}`);
    } catch (error) {
        console.error('Ошибка загрузки поставщиков:', error);
        showError(`Ошибка загрузки поставщиков: ${error.message}`);
    }
}

async function loadRegions() {
    try {
        const response = await AuthManager.authenticatedFetch('/api/region?limit=1000');
        if (response.ok) {
            const regions = await response.json();
            allRegions = Array.isArray(regions) ? regions : [];
            populateRegionsCheckboxes();
        }
    } catch (error) {
        console.warn('Ошибка загрузки регионов:', error);
    }
}

function populateRegionsCheckboxes() {
    const container = document.getElementById('regionsCheckboxContainer');
    if (allRegions.length === 0) {
        container.innerHTML = '<div style="color: #666; font-style: italic;">Регионы не загружены</div>';
        return;
    }
    
    // Сортируем регионы по алфавиту по названию
    const sortedRegions = [...allRegions].sort((a, b) => {
        const nameA = (a.Name || a.name || a.Code || a.code || a.RegionID || a.region_id || '').toLowerCase();
        const nameB = (b.Name || b.name || b.Code || b.code || b.RegionID || b.region_id || '').toLowerCase();
        return nameA.localeCompare(nameB, 'ru');
    });
    
    container.innerHTML = '';
    sortedRegions.forEach(region => {
        const regionId = region.RegionID || region.region_id;
        const regionName = region.Name || region.name || region.Code || region.code || regionId;
        
        const div = document.createElement('div');
        div.className = 'region-checkbox';
        div.innerHTML = `
            <label>
                <input type="checkbox" value="${regionId}" class="region-checkbox-input">
                <span>${escapeHtml(regionName)}</span>
            </label>
        `;
        container.appendChild(div);
    });
}

async function loadImportPoints(supplierID) {
    if (!supplierID) {
        const select = document.getElementById('formImportPointId');
        select.innerHTML = '<option value="">-- Не указана --</option>';
        return;
    }
    
    try {
        const response = await AuthManager.authenticatedFetch(`/api/import-points?supplier_id=${supplierID}`);
        if (response.ok) {
            const importPoints = await response.json();
            allImportPoints[supplierID] = Array.isArray(importPoints) ? importPoints : [];
            
            const select = document.getElementById('formImportPointId');
            select.innerHTML = '<option value="">-- Не указана --</option>';
            allImportPoints[supplierID].forEach(ip => {
                const option = document.createElement('option');
                option.value = ip.import_point_id;
                option.textContent = ip.name;
                select.appendChild(option);
            });
        }
    } catch (error) {
        console.warn('Ошибка загрузки точек импорта:', error);
    }
}

async function loadPriceLists(showNotification = false) {
    // Параметр showNotification по умолчанию false (не показывать уведомление)
    // Используем значение по умолчанию в параметре функции для надежности
    
    const btn = document.getElementById('loadPriceListsBtn');
    
    try {
        // Показываем индикатор загрузки
        if (btn) {
            showButtonLoading(btn, 'Загрузка...');
        }
        
        const supplierID = document.getElementById('supplierFilter')?.value || '';
        let url = '/api/price-lists';
        if (supplierID) {
            url += `?supplier_id=${supplierID}`;
        }
        
        console.log('Загрузка прайсов, URL:', url);
        const response = await AuthManager.authenticatedFetch(url);
        
        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Ошибка загрузки');
        }
        
        const data = await response.json();
        console.log('Получены данные:', data);
        displayPriceLists(data.price_lists || []);
        
        // Показываем уведомление только если явно запрошено (при нажатии кнопки)
        if (showNotification) {
            if (data.price_lists && data.price_lists.length > 0) {
                showSuccess(`Загружено прайсов: ${data.price_lists.length}`);
            } else {
                showSuccess('Прайсы не найдены');
            }
        }
    } catch (error) {
        console.error('Ошибка загрузки прайсов:', error);
        showError(`Ошибка загрузки прайсов: ${error.message}`);
    } finally {
        // Восстанавливаем кнопку
        if (btn) {
            hideButtonLoading(btn);
        }
    }
}

function displayPriceLists(priceLists) {
    const table = document.getElementById('priceListsTable');
    const tbody = document.getElementById('priceListsTableBody');
    
    if (!table || !tbody) {
        console.error('Элементы таблицы не найдены!');
        return;
    }
    
    tbody.innerHTML = '';
    
    if (!priceLists || priceLists.length === 0) {
        table.style.display = 'table';
        const row = document.createElement('tr');
        row.innerHTML = '<td colspan="9" style="text-align: center; padding: 20px; color: #666;">Прайсы не найдены</td>';
        tbody.appendChild(row);
        return;
    }
    
    table.style.display = 'table';
    
    priceLists.forEach(pl => {
        const row = document.createElement('tr');
        const status = pl.is_active ? '✅ Активен' : '❌ Неактивен';
        const markupPct = pl.default_markup_pct ? pl.default_markup_pct.toFixed(2) + '%' : '0%';
        const schedule = pl.schedule_cron || '-';
        
        row.innerHTML = `
            <td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(pl.supplier_name || '-')}</td>
            <td style="padding: 8px; border: 1px solid #ddd;"><strong>${escapeHtml(pl.name)}</strong></td>
            <td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(pl.import_point_name || '-')}</td>
            <td style="padding: 8px; border: 1px solid #ddd; text-align: right;">${markupPct}</td>
            <td style="padding: 8px; border: 1px solid #ddd; text-align: center;">${pl.regions_count || 0}</td>
            <td style="padding: 8px; border: 1px solid #ddd; text-align: center;">${pl.prices_count || 0}</td>
            <td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(schedule)}</td>
            <td style="padding: 8px; border: 1px solid #ddd;">${status}</td>
            <td style="padding: 8px; border: 1px solid #ddd;">
                <button onclick="editPriceList('${pl.price_list_id}')" class="btn btn-sm" style="padding: 4px 8px; margin: 2px;">✏️</button>
                <button onclick="togglePriceList('${pl.price_list_id}')" class="btn btn-sm" style="padding: 4px 8px; margin: 2px;">${pl.is_active ? '⏸️' : '▶️'}</button>
                <button onclick="deletePriceList('${pl.price_list_id}')" class="btn btn-sm" style="padding: 4px 8px; margin: 2px; background: #dc3545;">🗑️</button>
            </td>
        `;
        row.dataset.priceListId = pl.price_list_id;
        tbody.appendChild(row);
    });
}

function showAddPriceListForm() {
    document.getElementById('priceListFormTitle').textContent = 'Создание прайса';
    document.getElementById('priceListId').value = '';
    document.getElementById('priceListForm').reset();
    document.getElementById('formIsActive').checked = true;
    document.getElementById('formSchedulePreset').value = '';
    document.getElementById('formScheduleCron').value = '';
    document.getElementById('cronInputContainer').style.display = 'none';
    updateScheduleDescription();
    document.getElementById('priceListFormSection').style.display = 'block';
    populateRegionsCheckboxes();
}

function hidePriceListForm() {
    document.getElementById('priceListFormSection').style.display = 'none';
}

function onSupplierChange() {
    const supplierID = document.getElementById('formSupplierId').value;
    loadImportPoints(supplierID);
}

async function handlePriceListFormSubmit(e) {
    e.preventDefault();
    
    const priceListId = document.getElementById('priceListId').value;
    const supplierID = document.getElementById('formSupplierId').value;
    const name = document.getElementById('formName').value.trim();
    const description = document.getElementById('formDescription').value.trim();
    const importPointID = document.getElementById('formImportPointId').value;
    
    // Получаем наценку - всегда отправляем значение, даже если 0
    const markupInput = document.getElementById('formDefaultMarkupPct');
    let defaultMarkupPct = 0;
    if (markupInput) {
        const value = markupInput.value.trim();
        if (value !== '' && value !== null && value !== undefined) {
            const parsed = parseFloat(value);
            if (!isNaN(parsed)) {
                defaultMarkupPct = parsed;
            } else {
                console.warn('Не удалось распарсить значение наценки:', value);
            }
        }
        // Если поле пустое, оставляем 0 - это валидное значение
    }
    console.log('Наценка из формы:', { 
        raw: markupInput?.value, 
        parsed: defaultMarkupPct, 
        type: typeof defaultMarkupPct,
        isNegative: defaultMarkupPct < 0
    });
    
    // Получаем расписание из пресета или ручного ввода
    const schedulePreset = document.getElementById('formSchedulePreset').value;
    const scheduleCronManual = document.getElementById('formScheduleCron').value.trim();
    const scheduleCron = (schedulePreset && schedulePreset !== 'custom') ? schedulePreset : (scheduleCronManual || null);
    
    const isActive = document.getElementById('formIsActive').checked;
    
    // Собираем выбранные регионы
    const regionCheckboxes = document.querySelectorAll('.region-checkbox-input:checked');
    const regionIDs = Array.from(regionCheckboxes).map(cb => cb.value);
    
    if (!supplierID || !name) {
        showError('Заполните все обязательные поля');
        return;
    }
    
    console.log('Отправка прайса:', { 
        priceListId, 
        defaultMarkupPct, 
        defaultMarkupPctType: typeof defaultMarkupPct,
        supplierID, 
        name,
        payload: priceListId ? 'UPDATE' : 'CREATE'
    });
    
    try {
        // Для обновления отправляем только измененные поля, для создания - все
        let payload;
        if (priceListId) {
            // Обновление - отправляем все поля, включая наценку (даже если 0)
            payload = {
                default_markup_pct: defaultMarkupPct,
                name: name,
                description: description || null,
                import_point_id: importPointID || null,
                schedule_cron: scheduleCron || null,
                region_ids: regionIDs,
                is_active: isActive
            };
        } else {
            // Создание
            payload = {
                supplier_id: supplierID,
                name: name,
                description: description || null,
                import_point_id: importPointID || null,
                default_markup_pct: defaultMarkupPct,
                schedule_cron: scheduleCron || null,
                region_ids: regionIDs,
                is_active: isActive
            };
        }
        
        console.log('Payload для отправки:', JSON.stringify(payload, null, 2));
        
        if (priceListId) {
            // Обновление
            const response = await AuthManager.authenticatedFetch(`/api/price-lists/${priceListId}`, {
                method: 'PUT',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(payload)
            });
            
            if (!response.ok) {
                const contentType = response.headers.get('content-type');
                let errorMsg = 'Ошибка обновления';
                if (contentType && contentType.includes('application/json')) {
                    const error = await response.json();
                    errorMsg = error.error || errorMsg;
                } else {
                    const text = await response.text();
                    console.error('Неправильный формат ответа:', text.substring(0, 200));
                    errorMsg = `Ошибка ${response.status}: ${response.statusText}`;
                }
                throw new Error(errorMsg);
            }
            
            const contentType = response.headers.get('content-type');
            let result = null;
            if (contentType && contentType.includes('application/json')) {
                result = await response.json();
                console.log('Прайс обновлен, ответ сервера:', result);
            }
            
            showSuccess('Прайс успешно обновлен');
            
            // Обновляем поле наценки в форме из ответа сервера (если есть)
            if (result && result.default_markup_pct !== undefined && result.default_markup_pct !== null) {
                const markupInput = document.getElementById('formDefaultMarkupPct');
                if (markupInput) {
                    markupInput.value = result.default_markup_pct;
                    console.log('Наценка в форме обновлена из ответа PUT:', {
                        sent: defaultMarkupPct,
                        received: result.default_markup_pct,
                        set: result.default_markup_pct
                    });
                }
            } else {
                // Если сервер не вернул значение, делаем дополнительный запрос
                console.warn('Сервер не вернул default_markup_pct в ответе, загружаем данные отдельно');
                try {
                    const refreshResponse = await AuthManager.authenticatedFetch(`/api/price-lists`);
                    if (refreshResponse.ok) {
                        const refreshData = await refreshResponse.json();
                        const updatedPriceList = refreshData.price_lists?.find(pl => pl.price_list_id === priceListId);
                        if (updatedPriceList) {
                            const markupInput = document.getElementById('formDefaultMarkupPct');
                            if (markupInput) {
                                const newValue = updatedPriceList.default_markup_pct !== null && updatedPriceList.default_markup_pct !== undefined 
                                    ? updatedPriceList.default_markup_pct 
                                    : 0;
                                markupInput.value = newValue;
                                console.log('Наценка в форме обновлена из GET запроса:', {
                                    sent: defaultMarkupPct,
                                    received: updatedPriceList.default_markup_pct,
                                    set: newValue
                                });
                            }
                        }
                    }
                } catch (refreshError) {
                    console.warn('Не удалось обновить форму после сохранения:', refreshError);
                }
            }
            
            // Перезагружаем список прайсов, чтобы увидеть обновленные данные
            await loadPriceLists(false); // Без уведомления
        } else {
            // Создание
            const response = await AuthManager.authenticatedFetch('/api/price-lists/create', {
                method: 'POST',
                body: JSON.stringify(payload)
            });
            
            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || 'Ошибка создания');
            }
            
            showSuccess('Прайс успешно создан');
        }
        
        hidePriceListForm();
        
        // Перезагружаем список прайсов (для создания - всегда, для обновления уже сделано выше)
        if (!priceListId) {
            await loadPriceLists(false); // Без уведомления
        }
    } catch (error) {
        showError(`Ошибка сохранения прайса: ${error.message}`);
    } finally {
        if (submitBtn) hideButtonLoading(submitBtn);
    }
}

async function editPriceList(priceListId) {
    try {
        console.log('Редактирование прайса:', priceListId);
        
        const response = await AuthManager.authenticatedFetch(`/api/price-lists`);
        if (!response.ok) {
            const contentType = response.headers.get('content-type');
            let errorMsg = 'Ошибка загрузки';
            if (contentType && contentType.includes('application/json')) {
                const error = await response.json();
                errorMsg = error.error || errorMsg;
            } else {
                const text = await response.text();
                console.error('Неправильный формат ответа (ожидался JSON):', text.substring(0, 200));
            }
            throw new Error(errorMsg);
        }
        
        const contentType = response.headers.get('content-type');
        if (!contentType || !contentType.includes('application/json')) {
            const text = await response.text();
            console.error('Сервер вернул не JSON:', text.substring(0, 200));
            throw new Error('Сервер вернул некорректный формат ответа');
        }
        
        const data = await response.json();
        console.log('Получены данные прайсов:', data);
        
        const priceList = data.price_lists.find(pl => pl.price_list_id === priceListId);
        if (!priceList) {
            showError('Прайс не найден');
            return;
        }
        
        // Загружаем регионы прайса
        let selectedRegionIDs = [];
        try {
            const regionsResponse = await AuthManager.authenticatedFetch(`/api/price-lists/${priceListId}/regions`);
            if (regionsResponse.ok) {
                const contentType = regionsResponse.headers.get('content-type');
                if (contentType && contentType.includes('application/json')) {
                    const regionsData = await regionsResponse.json();
                    selectedRegionIDs = regionsData.regions ? regionsData.regions.map(r => r.region_id) : [];
                }
            }
        } catch (err) {
            console.warn('Ошибка загрузки регионов прайса:', err);
            // Продолжаем без регионов
        }
        
        document.getElementById('priceListFormTitle').textContent = 'Редактирование прайса';
        document.getElementById('priceListId').value = priceListId;
        document.getElementById('formSupplierId').value = priceList.supplier_id;
        document.getElementById('formName').value = priceList.name || '';
        document.getElementById('formDescription').value = priceList.description || '';
        document.getElementById('formImportPointId').value = priceList.import_point_id || '';
        document.getElementById('formDefaultMarkupPct').value = priceList.default_markup_pct || 0;
        
        // Устанавливаем расписание
        const scheduleValue = priceList.schedule_cron || '';
        if (scheduleValue) {
            const presetSelect = document.getElementById('formSchedulePreset');
            const cronInput = document.getElementById('formScheduleCron');
            const cronContainer = document.getElementById('cronInputContainer');
            // Проверяем, есть ли такое значение в пресетах
            let found = false;
            for (let i = 0; i < presetSelect.options.length; i++) {
                if (presetSelect.options[i].value === scheduleValue) {
                    presetSelect.value = scheduleValue;
                    cronContainer.style.display = 'none';
                    found = true;
                    break;
                }
            }
            if (!found) {
                presetSelect.value = 'custom';
                cronInput.value = scheduleValue;
                cronContainer.style.display = 'block';
            }
            updateScheduleDescription();
        } else {
            document.getElementById('formSchedulePreset').value = '';
            document.getElementById('formScheduleCron').value = '';
            document.getElementById('cronInputContainer').style.display = 'none';
        }
        
        document.getElementById('formIsActive').checked = priceList.is_active !== false;
        
        await loadImportPoints(priceList.supplier_id);
        populateRegionsCheckboxes();
        
        // Отмечаем выбранные регионы
        setTimeout(() => {
            selectedRegionIDs.forEach(regionID => {
                const checkbox = document.querySelector(`.region-checkbox-input[value="${regionID}"]`);
                if (checkbox) checkbox.checked = true;
            });
        }, 100);
        
        document.getElementById('priceListFormSection').style.display = 'block';
    } catch (error) {
        console.error('Ошибка редактирования:', error);
        showError(`Ошибка редактирования: ${error.message}`);
    }
}

async function togglePriceList(priceListId) {
    try {
        // Сначала получаем текущее состояние
        const response = await AuthManager.authenticatedFetch(`/api/price-lists`);
        if (!response.ok) throw new Error('Ошибка загрузки');
        
        const data = await response.json();
        const priceList = data.price_lists.find(pl => pl.price_list_id === priceListId);
        if (!priceList) {
            showError('Прайс не найден');
            return;
        }
        
        const newState = !priceList.is_active;
        const updateResponse = await AuthManager.authenticatedFetch(`/api/price-lists/${priceListId}`, {
            method: 'PUT',
            body: JSON.stringify({ is_active: newState })
        });
        
        if (!updateResponse.ok) {
            const error = await updateResponse.json();
            throw new Error(error.error || 'Ошибка переключения');
        }
        
        showSuccess(`Прайс ${newState ? 'включен' : 'выключен'}`);
        loadPriceLists(false); // Без уведомления
    } catch (error) {
        showError(`Ошибка переключения прайса: ${error.message}`);
    }
}

async function deletePriceList(priceListId) {
    if (!confirm('Вы уверены, что хотите удалить этот прайс? Все связанные цены будут деактивированы.')) {
        return;
    }
    
    try {
        const response = await AuthManager.authenticatedFetch(`/api/price-lists/${priceListId}`, {
            method: 'DELETE'
        });
        
        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Ошибка удаления');
        }
        
        showSuccess('Прайс успешно удален');
        loadPriceLists(false); // Без уведомления
    } catch (error) {
        showError(`Ошибка удаления прайса: ${error.message}`);
    }
}

function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Функции showError, showSuccess, showInfo теперь определены в toast.js
// Если toast.js не загружен, используем fallback
if (typeof showToast === 'undefined') {
    function showError(message) {
        console.error('Toast not loaded:', message);
    }
    function showSuccess(message) {
        console.log('Toast not loaded:', message);
    }
    function showInfo(message) {
        console.log('Toast not loaded:', message);
    }
}

// Функция для обновления поля CRON при выборе пресета
function updateScheduleCron() {
    const presetSelect = document.getElementById('formSchedulePreset');
    const cronInput = document.getElementById('formScheduleCron');
    const cronContainer = document.getElementById('cronInputContainer');
    const description = document.getElementById('scheduleDescription');
    
    console.log('updateScheduleCron вызвана, значение:', presetSelect ? presetSelect.value : 'элемент не найден');
    
    if (!presetSelect) {
        console.error('Элемент formSchedulePreset не найден!');
        return;
    }
    if (!cronInput) {
        console.error('Элемент formScheduleCron не найден!');
        return;
    }
    if (!cronContainer) {
        console.error('Элемент cronInputContainer не найден!');
        return;
    }
    if (!description) {
        console.error('Элемент scheduleDescription не найден!');
        return;
    }
    
    if (presetSelect.value === 'custom') {
        // Показываем контейнер с полем ввода CRON
        console.log('Показываем поле для ввода CRON');
        cronContainer.setAttribute('style', 'display: block !important; margin-top: 10px; padding: 10px; background: #f8f9fa; border: 1px solid #ddd; border-radius: 4px;');
        cronInput.value = cronInput.value || ''; // Сохраняем текущее значение, если есть
        cronInput.placeholder = 'Например: 0 0 * * * (каждый день в полночь)';
        description.textContent = 'Введите CRON выражение в поле выше';
    } else if (presetSelect.value) {
        // Скрываем контейнер и устанавливаем значение из пресета
        cronContainer.setAttribute('style', 'display: none !important; margin-top: 10px; padding: 10px; background: #f8f9fa; border: 1px solid #ddd; border-radius: 4px;');
        cronInput.value = presetSelect.value;
        const selectedOption = presetSelect.options[presetSelect.selectedIndex];
        description.textContent = 'Выбрано: ' + selectedOption.text;
    } else {
        // Ничего не выбрано
        cronContainer.setAttribute('style', 'display: none !important; margin-top: 10px; padding: 10px; background: #f8f9fa; border: 1px solid #ddd; border-radius: 4px;');
        cronInput.value = '';
        description.textContent = 'Выберите готовое расписание из списка выше или настройте вручную';
    }
}

function updateScheduleDescription() {
    const presetSelect = document.getElementById('formSchedulePreset');
    const description = document.getElementById('scheduleDescription');
    
    if (!presetSelect || !description) return;
    
    if (presetSelect.value && presetSelect.value !== 'custom') {
        const selectedOption = presetSelect.options[presetSelect.selectedIndex];
        description.textContent = 'Выбрано: ' + selectedOption.text;
    } else if (presetSelect.value === 'custom') {
        description.textContent = 'Формат CRON: минута (0-59) час (0-23) день (1-31) месяц (1-12) день_недели (0-7, 0 и 7 = воскресенье). Примеры: "0 0 * * *" - каждый день в полночь, "0 */6 * * *" - каждые 6 часов';
    } else {
        description.textContent = 'Выберите готовое расписание из списка выше или настройте вручную';
    }
}

// ========== Функции для управления поставщиками и импортом ==========

let targetFields = []; // Для маппинга DBF полей

// Инициализация обработчиков для импорта (вызывается в DOMContentLoaded)
function initImportHandlers() {
    console.log('=== Инициализация обработчиков импорта ===');
    console.log('Текущий URL:', window.location.href);
    console.log('Всего элементов на странице:', document.querySelectorAll('*').length);
    
    // Поставщики
    const refreshSuppliersBtn = document.getElementById('refreshSuppliersBtn');
    const addSupplierBtn = document.getElementById('addSupplierBtn');
    const saveSupplierBtn = document.getElementById('saveSupplierBtn');
    const cancelSupplierBtn = document.getElementById('cancelSupplierBtn');
    
    console.log('Элементы кнопок:', {
        refreshSuppliersBtn: !!refreshSuppliersBtn,
        addSupplierBtn: !!addSupplierBtn,
        saveSupplierBtn: !!saveSupplierBtn,
        cancelSupplierBtn: !!cancelSupplierBtn
    });
    
    // Дополнительная проверка: ищем по тексту кнопки
    const allButtons = Array.from(document.querySelectorAll('button'));
    console.log('Всего кнопок на странице:', allButtons.length);
    const importButtons = allButtons.filter(btn => 
        btn.textContent.includes('Добавить поставщика') || 
        btn.textContent.includes('Обновить список') ||
        btn.textContent.includes('Добавить точку импорта')
    );
    console.log('Кнопки импорта найдены по тексту:', importButtons.map(b => ({
        id: b.id,
        text: b.textContent.trim()
    })));
    
    // Fallback: если не нашли по ID, пытаемся найти по тексту
    const refreshBtn = refreshSuppliersBtn || importButtons.find(b => b.textContent.includes('Обновить список'));
    const addBtn = addSupplierBtn || importButtons.find(b => b.textContent.includes('Добавить поставщика'));
    
    if (refreshBtn && !refreshSuppliersBtn) {
        console.log('Нашли кнопку "Обновить список" по тексту, ID:', refreshBtn.id);
    }
    if (addBtn && !addSupplierBtn) {
        console.log('Нашли кнопку "Добавить поставщика" по тексту, ID:', addBtn.id);
    }
    
    if (refreshBtn) {
        refreshBtn.addEventListener('click', async () => {
            console.log('Кнопка "Обновить список поставщиков" нажата');
            try {
                await loadSuppliers(false); // Для формы добавления поставщика
                // Также обновляем фильтр
                const filter = document.getElementById('supplierFilter');
                if (filter) {
                    await loadSuppliers(true); // true - для фильтра
                }
                showSuccess('Список поставщиков обновлен');
            } catch (error) {
                console.error('Ошибка обновления списка поставщиков:', error);
                showError('Ошибка обновления списка поставщиков');
            }
        });
        console.log('Обработчик для refreshSuppliersBtn добавлен');
    } else {
        console.error('Кнопка refreshSuppliersBtn не найдена!');
    }
    
    if (addBtn) {
        addBtn.addEventListener('click', () => {
            console.log('Кнопка "Добавить поставщика" нажата');
            const form = document.getElementById('supplierForm');
            if (form) {
                form.style.display = 'block';
            } else {
                console.error('Форма supplierForm не найдена!');
            }
        });
        console.log('Обработчик для addSupplierBtn добавлен');
    } else {
        console.error('Кнопка addSupplierBtn не найдена!');
    }
    
    if (saveSupplierBtn) {
        saveSupplierBtn.addEventListener('click', saveSupplierForImport);
        console.log('Обработчик для saveSupplierBtn добавлен');
    } else {
        console.error('Кнопка saveSupplierBtn не найдена!');
    }
    
    // Инициализация обработчиков для наценок по регионам
    const addRegionMarkupBtn = document.getElementById('addRegionMarkupBtn');
    const saveDefaultMarkupBtn = document.getElementById('saveDefaultMarkupBtn');
    
    if (addRegionMarkupBtn) {
        addRegionMarkupBtn.addEventListener('click', showAddRegionMarkupForm);
    }
    
    if (saveDefaultMarkupBtn) {
        saveDefaultMarkupBtn.addEventListener('click', saveDefaultMarkup);
    }
    
    const saveNewRegionMarkupBtn = document.getElementById('saveNewRegionMarkupBtn');
    const cancelNewRegionMarkupBtn = document.getElementById('cancelNewRegionMarkupBtn');
    
    if (saveNewRegionMarkupBtn) {
        saveNewRegionMarkupBtn.addEventListener('click', saveNewRegionMarkup);
    }
    
    if (cancelNewRegionMarkupBtn) {
        cancelNewRegionMarkupBtn.addEventListener('click', () => {
            const form = document.getElementById('addRegionMarkupForm');
            if (form) form.style.display = 'none';
        });
    }
    
    // При изменении поставщика в фильтре показываем/скрываем секцию наценок
    if (supplierFilter) {
        supplierFilter.addEventListener('change', () => {
            const supplierID = supplierFilter.value;
            const markupSection = document.getElementById('supplierMarkupSection');
            if (markupSection) {
                if (supplierID) {
                    markupSection.style.display = 'block';
                    loadSupplierMarkupPolicies(supplierID);
                } else {
                    markupSection.style.display = 'none';
                }
            }
        });
    }
    
    if (cancelSupplierBtn) {
        cancelSupplierBtn.addEventListener('click', hideSupplierForm);
        console.log('Обработчик для cancelSupplierBtn добавлен');
    } else {
        console.error('Кнопка cancelSupplierBtn не найдена!');
    }
    
    // Точки импорта
    const addImportPointBtn = document.getElementById('addImportPointBtn');
    const saveImportPointBtn = document.getElementById('saveImportPointBtn');
    const cancelImportPointBtn = document.getElementById('cancelImportPointBtn');
    const importPointSelectForImport = document.getElementById('importPointSelectForImport');
    
    // Fallback для кнопки добавления точки импорта (используем importButtons, определенный выше)
    const allButtonsForImport = Array.from(document.querySelectorAll('button'));
    const importButtonsForImport = allButtonsForImport.filter(btn => 
        btn.textContent.includes('Добавить точку импорта')
    );
    const addImportPointBtnFallback = addImportPointBtn || importButtonsForImport.find(b => b.textContent.includes('Добавить точку импорта'));
    
    console.log('Элементы точек импорта:', {
        addImportPointBtn: !!addImportPointBtn,
        addImportPointBtnFallback: !!addImportPointBtnFallback,
        saveImportPointBtn: !!saveImportPointBtn,
        cancelImportPointBtn: !!cancelImportPointBtn,
        importPointSelectForImport: !!importPointSelectForImport
    });
    
    if (addImportPointBtnFallback) {
        addImportPointBtnFallback.addEventListener('click', () => {
            console.log('Кнопка "Добавить точку импорта" нажата');
            showImportPointForm();
        });
        console.log('Обработчик для addImportPointBtn добавлен, ID элемента:', addImportPointBtnFallback.id);
    } else {
        console.error('Кнопка addImportPointBtn не найдена ни по ID, ни по тексту!');
    }
    
    if (saveImportPointBtn) {
        saveImportPointBtn.addEventListener('click', () => {
            console.log('Кнопка "Сохранить точку импорта" нажата');
            saveImportPointForImport();
        });
        console.log('Обработчик для saveImportPointBtn добавлен');
    } else {
        console.error('Кнопка saveImportPointBtn не найдена!');
    }
    
    if (cancelImportPointBtn) {
        cancelImportPointBtn.addEventListener('click', () => {
            console.log('Кнопка "Отмена" для точки импорта нажата');
            hideImportPointForm();
        });
        console.log('Обработчик для cancelImportPointBtn добавлен');
    } else {
        console.error('Кнопка cancelImportPointBtn не найдена!');
    }
    
    if (importPointSelectForImport) {
        importPointSelectForImport.addEventListener('change', () => {
            console.log('Изменен выбор точки импорта:', importPointSelectForImport.value);
            onImportPointChangeForImport();
        });
        console.log('Обработчик для importPointSelectForImport добавлен');
    } else {
        console.error('Элемент importPointSelectForImport не найден!');
    }
    
    // Обновление списка точек импорта при изменении фильтра поставщиков
    // НЕ добавляем обработчик здесь, т.к. он уже добавлен в DOMContentLoaded
    // Просто убедимся, что обработчик вызывается при изменении фильтра
    const supplierFilterForImport = document.getElementById('supplierFilter');
    if (supplierFilterForImport) {
        // Добавляем дополнительный обработчик для обновления точек импорта
        supplierFilterForImport.addEventListener('change', async () => {
            const supplierID = supplierFilterForImport.value;
            if (supplierID) {
                await loadImportPointsForImport(supplierID);
            } else {
                const select = document.getElementById('importPointSelectForImport');
                if (select) {
                    select.innerHTML = '<option value="">-- Сначала выберите поставщика в фильтре выше --</option>';
                }
            }
        });
        console.log('Обработчик фильтра поставщиков для импорта добавлен');
    }
    
    // DBF файлы
    const selectDBFFileBtn = document.getElementById('selectDBFFileBtn');
    const dbfFileInput = document.getElementById('dbfFileInput');
    const analyzeDBFBtn = document.getElementById('analyzeDBFBtn');
    const saveMappingBtn = document.getElementById('saveMappingBtn');
    const selectImportFileBtn = document.getElementById('selectImportFileBtn');
    const importFileInput = document.getElementById('importFileInput');
    const importFileBtn = document.getElementById('importFileBtn');
    
    console.log('Элементы DBF/импорт:', {
        selectDBFFileBtn: !!selectDBFFileBtn,
        dbfFileInput: !!dbfFileInput,
        analyzeDBFBtn: !!analyzeDBFBtn,
        saveMappingBtn: !!saveMappingBtn,
        selectImportFileBtn: !!selectImportFileBtn,
        importFileInput: !!importFileInput,
        importFileBtn: !!importFileBtn
    });
    
    if (selectDBFFileBtn) {
        selectDBFFileBtn.addEventListener('click', () => {
            if (dbfFileInput) dbfFileInput.click();
        });
        console.log('Обработчик для selectDBFFileBtn добавлен');
    }
    if (dbfFileInput) {
        dbfFileInput.addEventListener('change', handleDBFFileSelected);
        console.log('Обработчик для dbfFileInput добавлен');
    }
    if (analyzeDBFBtn) {
        analyzeDBFBtn.addEventListener('click', analyzeDBFFile);
        console.log('Обработчик для analyzeDBFBtn добавлен');
    }
    if (saveMappingBtn) {
        saveMappingBtn.addEventListener('click', saveMapping);
        console.log('Обработчик для saveMappingBtn добавлен');
    }
    if (selectImportFileBtn) {
        selectImportFileBtn.addEventListener('click', () => {
            if (importFileInput) importFileInput.click();
        });
        console.log('Обработчик для selectImportFileBtn добавлен');
    }
    if (importFileInput) {
        importFileInput.addEventListener('change', handleImportFileSelected);
        console.log('Обработчик для importFileInput добавлен');
    }
    if (importFileBtn) {
        importFileBtn.addEventListener('click', importFile);
        console.log('Обработчик для importFileBtn добавлен');
    } else {
        console.error('Кнопка importFileBtn не найдена!');
    }
    
    console.log('=== Инициализация обработчиков импорта завершена ===');
}

// Функции для работы с поставщиками (для секции импорта)
function hideSupplierForm() {
    const form = document.getElementById('supplierForm');
    if (form) form.style.display = 'none';
    const nameInput = document.getElementById('supplierName');
    const innInput = document.getElementById('supplierINN');
    const addressInput = document.getElementById('supplierAddress');
    const contactsInput = document.getElementById('supplierContacts');
    if (nameInput) nameInput.value = '';
    if (innInput) innInput.value = '';
    if (addressInput) addressInput.value = '';
    if (contactsInput) contactsInput.value = '';
}

async function saveSupplierForImport() {
    const nameInput = document.getElementById('supplierName');
    if (!nameInput || !nameInput.value.trim()) {
        showError('Введите название поставщика');
        return;
    }
    
    const supplier = {
        name: nameInput.value.trim(),
        inn: document.getElementById('supplierINN')?.value.trim() || null,
        address: document.getElementById('supplierAddress')?.value.trim() || null,
        contacts: document.getElementById('supplierContacts')?.value.trim() || null,
        is_active: true
    };
    
    try {
        const response = await AuthManager.authenticatedFetch('/api/suppliers/create', {
            method: 'POST',
            body: JSON.stringify(supplier)
        });
        
        if (response.ok) {
            showSuccess('Поставщик успешно создан');
            hideSupplierForm();
            await loadSuppliers();
            await loadSuppliers(true); // Обновляем фильтр
        } else {
            const error = await response.json();
            showError(`Ошибка: ${error.error || 'Неизвестная ошибка'}`);
        }
    } catch (error) {
        showError(`Ошибка создания поставщика: ${error.message}`);
    }
}

// Функции для работы с точками импорта
function showImportPointForm() {
    console.log('showImportPointForm вызвана');
    const supplierFilter = document.getElementById('supplierFilter');
    const supplierID = supplierFilter?.value;
    
    console.log('supplierFilter:', supplierFilter, 'supplierID:', supplierID);
    
    if (!supplierID) {
        showError('Сначала выберите поставщика в фильтре выше');
        return;
    }
    
    const form = document.getElementById('importPointForm');
    console.log('Форма importPointForm:', form);
    if (form) {
        form.style.display = 'block';
        console.log('Форма отображена');
    } else {
        console.error('Форма importPointForm не найдена!');
        showError('Ошибка: форма не найдена');
    }
}

function hideImportPointForm() {
    console.log('hideImportPointForm вызвана');
    const form = document.getElementById('importPointForm');
    if (form) {
        form.style.display = 'none';
        console.log('Форма скрыта');
    }
    const nameInput = document.getElementById('importPointName');
    const descInput = document.getElementById('importPointDescription');
    const sourceFilePathInput = document.getElementById('importPointSourceFilePath');
    if (nameInput) nameInput.value = '';
    if (descInput) descInput.value = '';
    if (sourceFilePathInput) sourceFilePathInput.value = '';
}

async function saveImportPointForImport() {
    console.log('saveImportPointForImport вызвана');
    const supplierFilter = document.getElementById('supplierFilter');
    const supplierID = supplierFilter?.value?.trim();
    const nameInput = document.getElementById('importPointName');
    const name = nameInput?.value?.trim();
    const sourceFilePathInput = document.getElementById('importPointSourceFilePath');
    const sourceFilePath = sourceFilePathInput?.value?.trim() || null;
    
    console.log('Данные для сохранения:', { supplierID, name, sourceFilePath });
    
    if (!supplierID || !name) {
        showError('Заполните все обязательные поля. Убедитесь, что выбран поставщик в фильтре.');
        return;
    }
    
    const importPoint = {
        supplier_id: supplierID,
        name: name,
        description: document.getElementById('importPointDescription')?.value.trim() || null,
        source_file_path: sourceFilePath,
        is_active: true
    };
    
    try {
        const response = await AuthManager.authenticatedFetch('/api/import-points/create', {
            method: 'POST',
            body: JSON.stringify(importPoint)
        });
        
        if (response.ok) {
            showSuccess('Точка импорта успешно создана');
            hideImportPointForm();
            await loadImportPointsForImport(supplierID);
            // Также обновляем список в форме создания прайса
            await loadImportPoints(supplierID);
        } else {
            const error = await response.json();
            showError(`Ошибка: ${error.error || 'Неизвестная ошибка'}`);
        }
    } catch (error) {
        showError(`Ошибка создания точки импорта: ${error.message}`);
    }
}

async function loadImportPointsForImport(supplierID) {
    const select = document.getElementById('importPointSelectForImport');
    if (!select) return;
    
    if (!supplierID) {
        select.innerHTML = '<option value="">-- Сначала выберите поставщика в фильтре выше --</option>';
        return;
    }
    
    try {
        const response = await AuthManager.authenticatedFetch(`/api/import-points?supplier_id=${supplierID}`);
        
        if (!response.ok) {
            select.innerHTML = '<option value="">-- Ошибка загрузки --</option>';
            return;
        }
        
        const data = await response.json();
        const importPoints = Array.isArray(data) ? data : [];
        
        select.innerHTML = '<option value="">-- Выберите точку импорта --</option>';
        importPoints.forEach(ip => {
            const option = document.createElement('option');
            option.value = ip.import_point_id;
            option.textContent = ip.name;
            select.appendChild(option);
        });
    } catch (error) {
        console.error('Ошибка загрузки точек импорта:', error);
        select.innerHTML = '<option value="">-- Ошибка загрузки --</option>';
    }
}

async function onImportPointChangeForImport() {
    const importPointID = document.getElementById('importPointSelectForImport')?.value;
    const mappingSection = document.getElementById('mappingSection');
    const importSection = document.getElementById('importSection');
    
    if (importPointID) {
        if (mappingSection) mappingSection.style.display = 'block';
        if (importSection) importSection.style.display = 'block';
        
        // Загружаем маппинг, если таблица уже отображена
        const tbody = document.getElementById('mappingTableBody');
        if (tbody && tbody.querySelectorAll('tr').length > 0) {
            await applyExistingMapping(importPointID);
        } else {
            await loadFieldMappings(importPointID);
        }
    } else {
        if (mappingSection) mappingSection.style.display = 'none';
        if (importSection) importSection.style.display = 'none';
    }
}

// Функции для работы с DBF файлами
async function handleDBFFileSelected(event) {
    const file = event.target.files[0];
    if (!file) return;
    
    const fileNameSpan = document.getElementById('dbfFileName');
    if (fileNameSpan) {
        fileNameSpan.textContent = `Выбран: ${file.name}`;
    }
    
    const formData = new FormData();
    formData.append('file', file);
    
    try {
        showSuccess('Загрузка файла...');
        const response = await AuthManager.authenticatedFetch('/api/dbf/upload', {
            method: 'POST',
            body: formData
        });
        
        if (response.ok) {
            const data = await response.json();
            const pathInput = document.getElementById('dbfFilePathInput');
            
            // Заполняем путь к файлу, если он есть в ответе
            if (pathInput && data.file_path) {
                pathInput.value = data.file_path;
                showSuccess(`Файл загружен. Путь к файлу заполнен: ${data.file_path}`);
                // Автоматически анализируем загруженный файл
                setTimeout(() => analyzeDBFFile(), 500);
            } else if (pathInput) {
                showSuccess('Файл загружен. Укажите путь к файлу для анализа.');
            }
        } else {
            const error = await response.json();
            showError(`Ошибка загрузки файла: ${error.error || 'Неизвестная ошибка'}. Укажите путь к файлу вручную.`);
        }
    } catch (error) {
        console.warn('Загрузка файла не поддерживается:', error);
        showError('Загрузка файла через браузер не поддерживается. Укажите путь к файлу вручную.');
    }
}

async function handleImportFileSelected(event) {
    const file = event.target.files[0];
    if (!file) return;
    
    const fileNameSpan = document.getElementById('importFileName');
    if (fileNameSpan) {
        fileNameSpan.textContent = `Выбран: ${file.name}`;
    }
    
    const formData = new FormData();
    formData.append('file', file);
    
    try {
        showSuccess('Загрузка файла...');
        const response = await AuthManager.authenticatedFetch('/api/dbf/upload', {
            method: 'POST',
            body: formData
        });
        
        if (response.ok) {
            const data = await response.json();
            console.log('Ответ сервера при загрузке файла:', data);
            
            const pathInput = document.getElementById('importFilePath');
            
            // Заполняем путь к файлу, если он есть в ответе
            if (pathInput && data.file_path) {
                pathInput.value = data.file_path;
                showSuccess(`Файл загружен. Путь к файлу заполнен: ${data.file_path}`);
            } else if (pathInput) {
                // Если путь не вернулся, но файл выбран, предлагаем ввести путь вручную
                showSuccess('Файл загружен. Укажите путь к файлу для импорта.');
                console.warn('Путь к файлу не возвращен сервером. Ответ:', data);
            }
        } else {
            let error;
            try {
                error = await response.json();
            } catch (e) {
                error = { error: `HTTP ${response.status}: ${response.statusText}` };
            }
            console.error('Ошибка загрузки файла:', error);
            showError(`Ошибка загрузки файла: ${error.error || 'Неизвестная ошибка'}. Укажите путь к файлу вручную.`);
        }
    } catch (error) {
        console.error('Исключение при загрузке файла:', error);
        showError(`Ошибка загрузки файла: ${error.message || 'Неизвестная ошибка'}. Укажите путь к файлу вручную.`);
    }
}

async function analyzeDBFFile() {
    const pathInput = document.getElementById('dbfFilePathInput');
    const filePath = pathInput?.value.trim();
    if (!filePath) {
        showError('Введите путь к DBF файлу или выберите файл');
        return;
    }
    
    try {
        const response = await AuthManager.authenticatedFetch('/api/dbf/analyze', {
            method: 'POST',
            body: JSON.stringify({ file_path: filePath })
        });
        
        if (response.ok) {
            const data = await response.json();
            const fileInfo = document.getElementById('dbfFileInfo');
            if (fileInfo) {
                fileInfo.innerHTML = `
                    <strong>Файл:</strong> ${escapeHtml(data.file_path || '-')}<br>
                    <strong>Количество записей:</strong> ${escapeHtml(String(data.records_count || 0))}<br>
                    <strong>Количество полей:</strong> ${escapeHtml(String(data.field_count || 0))}
                `;
            }
            await loadTargetFields();
            displayMappingTable(data.fields);
            
            await new Promise(resolve => setTimeout(resolve, 100));
            
            const importPointID = document.getElementById('importPointSelectForImport')?.value;
            if (importPointID) {
                await applyExistingMapping(importPointID);
            }
            
            const resultDiv = document.getElementById('dbfAnalysisResult');
            if (resultDiv) resultDiv.style.display = 'block';
        } else {
            const error = await response.json();
            showError(`Ошибка анализа файла: ${error.error || 'Неизвестная ошибка'}`);
        }
    } catch (error) {
        showError(`Ошибка анализа файла: ${error.message}`);
    }
}

async function loadTargetFields() {
    try {
        const response = await AuthManager.authenticatedFetch('/api/target-fields');
        const data = await response.json();
        targetFields = data.target_fields || [];
    } catch (error) {
        targetFields = [
            {name: "invoice_number", type: "NVARCHAR", description: "Номер прайса"},
            {name: "invoice_date", type: "DATETIME", description: "Дата прайса"},
            {name: "item_code", type: "NVARCHAR", description: "Код товара"},
            {name: "item_name", type: "NVARCHAR", description: "Наименование товара"},
            {name: "quantity", type: "DECIMAL", description: "Количество"},
            {name: "price", type: "DECIMAL", description: "Цена"},
            {name: "amount", type: "DECIMAL", description: "Сумма"},
            {name: "barcode", type: "NVARCHAR", description: "Штрихкод"}
        ];
    }
}

function displayMappingTable(dbfFields) {
    const tbody = document.getElementById('mappingTableBody');
    if (!tbody) return;
    
    tbody.innerHTML = '';
    
    dbfFields.forEach((field, index) => {
        const row = document.createElement('tr');
        
        const dbfCell = document.createElement('td');
        dbfCell.style.padding = '10px';
        dbfCell.style.border = '1px solid #ddd';
        dbfCell.innerHTML = `<strong>${escapeHtml(field.name || '-')}</strong><br><small>${escapeHtml(field.type || '-')}(${escapeHtml(String(field.length || 0))})</small>`;
        
        const typeDbfCell = document.createElement('td');
        typeDbfCell.style.padding = '10px';
        typeDbfCell.style.border = '1px solid #ddd';
        typeDbfCell.textContent = field.type;
        
        const arrowCell = document.createElement('td');
        arrowCell.style.padding = '10px';
        arrowCell.style.border = '1px solid #ddd';
        arrowCell.style.textAlign = 'center';
        arrowCell.textContent = '→';
        
        const targetCell = document.createElement('td');
        targetCell.style.padding = '10px';
        targetCell.style.border = '1px solid #ddd';
        const targetSelect = document.createElement('select');
        targetSelect.style.width = '100%';
        targetSelect.id = `target_${index}`;
        targetSelect.innerHTML = '<option value="">-- Не сопоставлять --</option>';
        targetFields.forEach(tf => {
            const option = document.createElement('option');
            option.value = tf.name;
            option.textContent = `${tf.name} (${tf.description})`;
            targetSelect.appendChild(option);
        });
        targetCell.appendChild(targetSelect);
        
        const typeCell = document.createElement('td');
        typeCell.style.padding = '10px';
        typeCell.style.border = '1px solid #ddd';
        const typeSelect = document.createElement('select');
        typeSelect.style.width = '100%';
        typeSelect.id = `type_${index}`;
        ['NVARCHAR', 'DECIMAL', 'INT', 'DATETIME', 'DATE'].forEach(t => {
            const option = document.createElement('option');
            option.value = t;
            option.textContent = t;
            if ((field.type === 'N' || field.type === 'F') && field.decimals > 0 && t === 'DECIMAL') option.selected = true;
            else if ((field.type === 'N' || field.type === 'F') && field.decimals === 0 && t === 'INT') option.selected = true;
            else if (field.type === 'D' && t === 'DATE') option.selected = true;
            else if (t === 'NVARCHAR') option.selected = true;
            typeSelect.appendChild(option);
        });
        typeCell.appendChild(typeSelect);
        
        const requiredCell = document.createElement('td');
        requiredCell.style.padding = '10px';
        requiredCell.style.border = '1px solid #ddd';
        requiredCell.style.textAlign = 'center';
        const requiredCheck = document.createElement('input');
        requiredCheck.type = 'checkbox';
        requiredCheck.id = `required_${index}`;
        requiredCell.appendChild(requiredCheck);
        
        row.appendChild(dbfCell);
        row.appendChild(typeDbfCell);
        row.appendChild(arrowCell);
        row.appendChild(targetCell);
        row.appendChild(typeCell);
        row.appendChild(requiredCell);
        tbody.appendChild(row);
    });
}

async function loadFieldMappings(importPointID) {
    try {
        const response = await AuthManager.authenticatedFetch(`/api/field-mappings?import_point_id=${importPointID}`);
        const mappings = await response.json();
        return mappings;
    } catch (error) {
        console.error('Ошибка загрузки маппинга:', error);
        return null;
    }
}

async function applyExistingMapping(importPointID) {
    try {
        const mappingsData = await loadFieldMappings(importPointID);
        if (!mappingsData) {
            return;
        }
        
        let mappings = [];
        if (Array.isArray(mappingsData)) {
            mappings = mappingsData;
        } else if (mappingsData && mappingsData.mappings && Array.isArray(mappingsData.mappings)) {
            mappings = mappingsData.mappings;
        } else if (mappingsData && mappingsData.field_mappings && Array.isArray(mappingsData.field_mappings)) {
            mappings = mappingsData.field_mappings;
        }
        
        if (mappings.length === 0) {
            return;
        }
        
        const tbody = document.getElementById('mappingTableBody');
        if (!tbody) return;
        
        let appliedCount = 0;
        const rows = tbody.querySelectorAll('tr');
        rows.forEach((row, index) => {
            const dbfFieldName = row.cells[0]?.querySelector('strong')?.textContent.trim();
            if (!dbfFieldName) return;
            
            const mapping = mappings.find(m => {
                const dbfName = (m.dbf_field_name || m.DBFFieldName || m.dbfFieldName || '').toString().trim();
                return dbfName.toLowerCase() === dbfFieldName.toLowerCase();
            });
            
            if (mapping) {
                const targetFieldName = (mapping.target_field_name || mapping.TargetFieldName || mapping.targetFieldName || '').toString().trim();
                if (targetFieldName) {
                    const selects = row.querySelectorAll('select');
                    const targetSelect = selects[0];
                    if (targetSelect && Array.from(targetSelect.options).some(opt => opt.value === targetFieldName)) {
                        targetSelect.value = targetFieldName;
                        appliedCount++;
                    }
                    
                    const typeSelect = selects[1];
                    if (typeSelect) {
                        const dataType = (mapping.data_type || mapping.DataType || '').toString().trim();
                        if (dataType && Array.from(typeSelect.options).some(opt => opt.value === dataType)) {
                            typeSelect.value = dataType;
                        }
                    }
                    
                    const requiredCheckbox = row.querySelector('input[type="checkbox"]');
                    if (requiredCheckbox) {
                        const isRequired = mapping.is_required === true || 
                                         mapping.is_required === 1 || 
                                         mapping.IsRequired === true ||
                                         mapping.IsRequired === 1 ||
                                         mapping.isRequired === true;
                        requiredCheckbox.checked = isRequired;
                    }
                }
            }
        });
        
        if (appliedCount > 0) {
            showSuccess(`Загружен существующий маппинг: ${appliedCount} полей применено`);
        }
    } catch (error) {
        console.error('Ошибка применения маппинга:', error);
    }
}

async function saveMapping() {
    const importPointID = document.getElementById('importPointSelectForImport')?.value;
    if (!importPointID) {
        showError('Сначала выберите точку импорта');
        return;
    }
    
    const tbody = document.getElementById('mappingTableBody');
    if (!tbody) {
        showError('Таблица маппинга не найдена. Сначала проанализируйте DBF файл.');
        return;
    }
    
    const saveBtn = document.getElementById('saveMappingBtn');
    const originalBtnText = saveBtn ? saveBtn.textContent : 'Сохранить маппинг';
    
    // Показываем индикатор загрузки
    if (saveBtn) {
        saveBtn.disabled = true;
        saveBtn.textContent = '⏳ Сохранение...';
    }
    
    try {
        const rows = tbody.querySelectorAll('tr');
        const mappings = [];
        
        rows.forEach((row, index) => {
            const targetSelect = document.getElementById(`target_${index}`);
            const typeSelect = document.getElementById(`type_${index}`);
            const requiredCheck = document.getElementById(`required_${index}`);
            
            if (!targetSelect || !targetSelect.value) return;
            
            const dbfFieldName = row.cells[0]?.querySelector('strong')?.textContent.trim();
            if (!dbfFieldName) return;
            
            mappings.push({
                dbf_field_name: dbfFieldName,
                target_field_name: targetSelect.value,
                data_type: typeSelect?.value || 'NVARCHAR',
                is_required: requiredCheck?.checked || false,
                display_order: index
            });
        });
        
        if (mappings.length === 0) {
            showError('Сопоставьте хотя бы одно поле');
            return;
        }
        
        // Проверка обязательных полей перед сохранением
        const requiredFields = ['item_code', 'item_name', 'barcode', 'price'];
        const mappedFields = new Set(mappings.map(m => m.target_field_name));
        const missingRequired = requiredFields.filter(field => !mappedFields.has(field));
        
        if (missingRequired.length > 0) {
            showError(`Не заполнены обязательные поля: ${missingRequired.join(', ')}`);
            return;
        }
        
        // Показываем уведомление о начале сохранения
        showInfo(`Сохранение маппинга: ${mappings.length} полей...`);
        
        let savedCount = 0;
        let errorCount = 0;
        const errors = [];
        
        for (const mapping of mappings) {
            try {
                const response = await AuthManager.authenticatedFetch(`/api/field-mappings/save?import_point_id=${importPointID}`, {
                    method: 'POST',
                    body: JSON.stringify(mapping)
                });
                
                if (response.ok) {
                    savedCount++;
                } else {
                    errorCount++;
                    const errorData = await response.json().catch(() => ({error: 'Неизвестная ошибка'}));
                    const errorMsg = errorData.error || errorData.message || 'Ошибка сохранения';
                    errors.push(`${mapping.dbf_field_name}: ${errorMsg}`);
                    console.error(`Ошибка сохранения маппинга для ${mapping.dbf_field_name}:`, errorMsg);
                }
            } catch (err) {
                errorCount++;
                errors.push(`${mapping.dbf_field_name}: ${err.message || 'Ошибка сети'}`);
                console.error(`Исключение при сохранении ${mapping.dbf_field_name}:`, err);
            }
        }
        
        // Показываем результат
        if (savedCount === mappings.length) {
            showSuccess(`✅ Маппинг успешно сохранен: ${savedCount} полей`);
            console.log('Маппинг успешно сохранен:', savedCount, 'полей');
        } else if (errorCount > 0) {
            const errorMsg = `Сохранено ${savedCount} из ${mappings.length} маппингов. Ошибки:\n${errors.join('\n')}`;
            showError(errorMsg);
            console.error('Ошибки при сохранении маппинга:', errors);
        } else {
            showError(`Сохранено ${savedCount} из ${mappings.length} маппингов`);
        }
    } catch (error) {
        const errorMsg = `Ошибка сохранения маппинга: ${error.message}`;
        showError(errorMsg);
        console.error('Критическая ошибка при сохранении маппинга:', error);
    } finally {
        // Восстанавливаем кнопку
        if (saveBtn) {
            saveBtn.disabled = false;
            saveBtn.textContent = originalBtnText;
        }
    }
}

async function importFile() {
    const importPointID = document.getElementById('importPointSelectForImport')?.value.trim();
    const pathInput = document.getElementById('importFilePath');
    const filePath = pathInput?.value.trim();
    
    if (!importPointID || !filePath) {
        showError('Заполните все поля: выберите точку импорта и укажите путь к файлу');
        return;
    }
    
    const statusDiv = document.getElementById('importStatus');
    if (statusDiv) {
        statusDiv.innerHTML = '<div style="color: #007bff;">Запуск импорта...</div>';
    }
    
    try {
        const response = await AuthManager.authenticatedFetch('/api/import/file', {
            method: 'POST',
            body: JSON.stringify({
                import_point_id: importPointID,
                file_path: filePath
            })
        });
        
        if (response.ok) {
            const result = await response.json();
            if (statusDiv) {
                statusDiv.innerHTML = `<div style="color: #28a745; font-weight: bold;">✅ ${result.message || 'Импорт запущен'}</div>`;
            }
            showSuccess(`Импорт запущен в фоновом режиме`);
            if (pathInput) pathInput.value = '';
        } else {
            const error = await response.json();
            if (statusDiv) {
                statusDiv.innerHTML = `<div style="color: #dc3545; font-weight: bold;">❌ ${error.error || 'Ошибка'}</div>`;
            }
            showError(`Ошибка запуска импорта: ${error.error || 'Неизвестная ошибка'}`);
        }
    } catch (error) {
        if (statusDiv) {
            statusDiv.innerHTML = `<div style="color: #dc3545; font-weight: bold;">❌ Ошибка: ${error.message}</div>`;
        }
        showError(`Ошибка импорта: ${error.message}`);
    }
}


// Функции для работы с наценками по регионам
let currentMarkupSupplierID = null;

async function loadSupplierMarkupPolicies(supplierID) {
    if (!supplierID) return;
    
    currentMarkupSupplierID = supplierID;
    
    try {
        const response = await AuthManager.authenticatedFetch(`/api/supplier-markup-policies?supplier_id=${supplierID}`);
        if (!response.ok) {
            console.error('Ошибка загрузки политик наценок');
            return;
        }
        
        const data = await response.json();
        const policies = data.policies || [];
        
        // Находим общую наценку (без RegionID)
        const defaultPolicy = policies.find(p => !p.region_id);
        const defaultMarkupInput = document.getElementById('defaultMarkupPct');
        if (defaultMarkupInput) {
            defaultMarkupInput.value = defaultPolicy ? defaultPolicy.markup_pct : '';
        }
        
        // Отображаем региональные наценки
        displayRegionMarkupList(policies.filter(p => p.region_id));
    } catch (error) {
        console.error('Ошибка загрузки политик наценок:', error);
        showError(`Ошибка загрузки наценок: ${error.message}`);
    }
}

function displayRegionMarkupList(policies) {
    const container = document.getElementById('regionMarkupList');
    if (!container) return;
    
    if (policies.length === 0) {
        container.innerHTML = '<div style="color: #666; font-style: italic; padding: 10px;">Нет региональных наценок. Добавьте наценку для региона, если нужно.</div>';
        return;
    }
    
    container.innerHTML = '';
    
    policies.forEach(policy => {
        const div = document.createElement('div');
        div.className = 'region-markup-item';
        div.style.cssText = 'display: flex; align-items: center; gap: 10px; padding: 10px; margin-bottom: 10px; border: 1px solid #ddd; border-radius: 5px; background: #f9f9f9;';
        div.innerHTML = `
            <div style="flex: 1;">
                <strong>${escapeHtml(policy.region_name || 'Неизвестный регион')}</strong>
                <div style="color: #666; font-size: 0.9em;">
                    Наценка: <strong>${policy.markup_pct}%</strong>
                    ${policy.rounding_step ? ` | Округление: ${policy.rounding_step}` : ''}
                </div>
            </div>
            <div style="display: flex; gap: 5px;">
                <button onclick="editRegionMarkup('${policy.policy_id}', '${policy.region_id}', '${policy.markup_pct}', ${policy.rounding_step || 'null'})" 
                        class="btn btn-sm" style="padding: 5px 10px; background: #007bff; color: white; border: none; border-radius: 3px; cursor: pointer;">✏️</button>
                <button onclick="deleteRegionMarkup('${policy.policy_id}', '${policy.region_name || ''}')" 
                        class="btn btn-sm" style="padding: 5px 10px; background: #dc3545; color: white; border: none; border-radius: 3px; cursor: pointer;">🗑️</button>
            </div>
        `;
        container.appendChild(div);
    });
}

function showAddRegionMarkupForm() {
    if (!currentMarkupSupplierID) {
        showError('Сначала выберите поставщика');
        return;
    }
    
    // Загружаем список регионов в выпадающий список
    const regionSelect = document.getElementById('newRegionMarkupRegion');
    if (!regionSelect) return;
    
    regionSelect.innerHTML = '<option value="">-- Выберите регион --</option>';
    allRegions.forEach(r => {
        const regionId = r.RegionID || r.region_id;
        const regionName = r.Name || r.name || r.Code || r.code || regionId;
        const option = document.createElement('option');
        option.value = regionId;
        option.textContent = regionName;
        regionSelect.appendChild(option);
    });
    
    // Очищаем поля
    document.getElementById('newRegionMarkupPct').value = '';
    document.getElementById('newRegionMarkupRounding').value = '';
    
    // Показываем форму
    const form = document.getElementById('addRegionMarkupForm');
    if (form) {
        form.style.display = 'block';
    }
}

async function createRegionMarkup(regionID, markupPct, roundingStep = null) {
    if (!currentMarkupSupplierID) {
        showError('Сначала выберите поставщика');
        return;
    }
    
    try {
        const response = await AuthManager.authenticatedFetch('/api/supplier-markup-policies', {
            method: 'POST',
            body: JSON.stringify({
                supplier_id: currentMarkupSupplierID,
                region_id: regionID,
                markup_pct: markupPct,
                rounding_step: roundingStep,
                is_active: true
            })
        });
        
        if (response.ok) {
            showSuccess('Наценка для региона добавлена');
            await loadSupplierMarkupPolicies(currentMarkupSupplierID);
            // Скрываем форму
            const form = document.getElementById('addRegionMarkupForm');
            if (form) form.style.display = 'none';
        } else {
            const error = await response.json();
            showError(`Ошибка: ${error.error || 'Неизвестная ошибка'}`);
        }
    } catch (error) {
        showError(`Ошибка создания наценки: ${error.message}`);
    }
}

async function saveNewRegionMarkup() {
    const regionSelect = document.getElementById('newRegionMarkupRegion');
    const markupInput = document.getElementById('newRegionMarkupPct');
    const roundingInput = document.getElementById('newRegionMarkupRounding');
    
    if (!regionSelect || !markupInput) return;
    
    const regionID = regionSelect.value;
    if (!regionID) {
        showError('Выберите регион');
        return;
    }
    
    const markupPct = parseFloat(markupInput.value);
    if (isNaN(markupPct)) {
        showError('Наценка должна быть числом');
        return;
    }
    
    const roundingStep = roundingInput && roundingInput.value ? parseFloat(roundingInput.value) : null;
    if (roundingStep !== null && isNaN(roundingStep)) {
        showError('Шаг округления должен быть числом');
        return;
    }
    
    await createRegionMarkup(regionID, markupPct, roundingStep);
}

async function editRegionMarkup(policyID, regionID, currentMarkup, currentRounding) {
    const newMarkup = prompt(`Редактировать наценку:\n\nТекущая наценка: ${currentMarkup}%\nТекущее округление: ${currentRounding || 'не указано'}\n\nВведите новую наценку (в %):`, currentMarkup);
    if (newMarkup === null) return;
    
    const markupPct = parseFloat(newMarkup);
    if (isNaN(markupPct)) {
        showError('Наценка должна быть числом');
        return;
    }
    
    try {
        const response = await AuthManager.authenticatedFetch(`/api/supplier-markup-policies/${policyID}`, {
            method: 'PUT',
            body: JSON.stringify({
                region_id: regionID,
                markup_pct: markupPct,
                rounding_step: currentRounding
            })
        });
        
        if (response.ok) {
            showSuccess('Наценка обновлена');
            await loadSupplierMarkupPolicies(currentMarkupSupplierID);
        } else {
            const error = await response.json();
            showError(`Ошибка: ${error.error || 'Неизвестная ошибка'}`);
        }
    } catch (error) {
        showError(`Ошибка обновления наценки: ${error.message}`);
    }
}

async function deleteRegionMarkup(policyID, regionName) {
    if (!confirm(`Удалить наценку для региона "${regionName}"?`)) return;
    
    try {
        const response = await AuthManager.authenticatedFetch(`/api/supplier-markup-policies/${policyID}`, {
            method: 'DELETE'
        });
        
        if (response.ok) {
            showSuccess('Наценка удалена');
            await loadSupplierMarkupPolicies(currentMarkupSupplierID);
        } else {
            const error = await response.json();
            showError(`Ошибка: ${error.error || 'Неизвестная ошибка'}`);
        }
    } catch (error) {
        showError(`Ошибка удаления наценки: ${error.message}`);
    }
}

async function saveDefaultMarkup() {
    if (!currentMarkupSupplierID) {
        showError('Сначала выберите поставщика');
        return;
    }
    
    const defaultMarkupInput = document.getElementById('defaultMarkupPct');
    if (!defaultMarkupInput) return;
    
    const markupPct = parseFloat(defaultMarkupInput.value);
    if (isNaN(markupPct)) {
        showError('Наценка должна быть числом');
        return;
    }
    
    try {
        // Сначала загружаем существующие политики, чтобы найти общую
        const response = await AuthManager.authenticatedFetch(`/api/supplier-markup-policies?supplier_id=${currentMarkupSupplierID}`);
        if (!response.ok) {
            showError('Ошибка загрузки политик наценок');
            return;
        }
        
        const data = await response.json();
        const policies = data.policies || [];
        const defaultPolicy = policies.find(p => !p.region_id);
        
        if (defaultPolicy) {
            // Обновляем существующую общую наценку
            const updateResponse = await AuthManager.authenticatedFetch(`/api/supplier-markup-policies/${defaultPolicy.policy_id}`, {
                method: 'PUT',
                body: JSON.stringify({
                    markup_pct: markupPct
                })
            });
            
            if (updateResponse.ok) {
                showSuccess('Общая наценка обновлена');
                await loadSupplierMarkupPolicies(currentMarkupSupplierID);
            } else {
                const error = await updateResponse.json();
                showError(`Ошибка: ${error.error || 'Неизвестная ошибка'}`);
            }
        } else {
            // Создаем новую общую наценку
            const createResponse = await AuthManager.authenticatedFetch('/api/supplier-markup-policies', {
                method: 'POST',
                body: JSON.stringify({
                    supplier_id: currentMarkupSupplierID,
                    markup_pct: markupPct,
                    is_active: true
                })
            });
            
            if (createResponse.ok) {
                showSuccess('Общая наценка сохранена');
                await loadSupplierMarkupPolicies(currentMarkupSupplierID);
            } else {
                const error = await createResponse.json();
                showError(`Ошибка: ${error.error || 'Неизвестная ошибка'}`);
            }
        }
    } catch (error) {
        showError(`Ошибка сохранения общей наценки: ${error.message}`);
    }
}

// Экспорт функций для использования в onclick
window.editPriceList = editPriceList;
window.togglePriceList = togglePriceList;
window.deletePriceList = deletePriceList;
window.updateScheduleCron = updateScheduleCron;
window.editRegionMarkup = editRegionMarkup;
window.deleteRegionMarkup = deleteRegionMarkup;

