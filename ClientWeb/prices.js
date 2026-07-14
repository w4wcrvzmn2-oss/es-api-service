// Скрипт для страницы прайсов

let allRegions = [];
let currentMarkupSupplierID = null;
let _pricesLoadGen = 0;
const PRICES_FIRST_BATCH = 200;

// Инициализация после загрузки DOM
document.addEventListener('DOMContentLoaded', () => {
    // Загрузка поставщиков и регионов при инициализации
    loadSuppliers();
    loadRegions();
    
    // Кнопки загрузки
    document.getElementById('loadPricesBtn').addEventListener('click', loadSupplierPrices);
    document.getElementById('addPriceBtn').addEventListener('click', showAddPriceForm);
    document.getElementById('cancelPriceFormBtn').addEventListener('click', hidePriceForm);
    
    // Форма прайса
    document.getElementById('priceForm').addEventListener('submit', handlePriceFormSubmit);
    
    // Обработчики для региональных наценок
    const addRegionMarkupBtn = document.getElementById('addRegionMarkupBtn');
    const saveNewRegionMarkupBtn = document.getElementById('saveNewRegionMarkupBtn');
    const cancelNewRegionMarkupBtn = document.getElementById('cancelNewRegionMarkupBtn');
    
    if (addRegionMarkupBtn) {
        addRegionMarkupBtn.addEventListener('click', showAddRegionMarkupForm);
    }
    if (saveNewRegionMarkupBtn) {
        saveNewRegionMarkupBtn.addEventListener('click', saveNewRegionMarkup);
    }
    if (cancelNewRegionMarkupBtn) {
        cancelNewRegionMarkupBtn.addEventListener('click', () => {
            const form = document.getElementById('addRegionMarkupForm');
            if (form) form.style.display = 'none';
        });
    }
    
    // Отслеживание изменения поставщика в форме
    const formSupplierId = document.getElementById('formSupplierId');
    if (formSupplierId) {
        formSupplierId.addEventListener('change', (e) => {
            const supplierID = e.target.value;
            if (supplierID) {
                currentMarkupSupplierID = supplierID;
                loadSupplierMarkupPolicies(supplierID);
            } else {
                currentMarkupSupplierID = null;
                const container = document.getElementById('regionMarkupList');
                if (container) container.innerHTML = '';
            }
        });
    }
    
    // Вычисления цен выполняются только на сервере для безопасности
});

async function loadSuppliers() {
    const select = document.getElementById('supplierSelect');
    
    if (!select) {
        console.error('Элемент supplierSelect не найден');
        return;
    }
    
    // Проверяем авторизацию перед запросом
    if (!AuthManager.isAuthenticated()) {
        showError('Требуется авторизация. Пожалуйста, войдите в систему.');
        setTimeout(() => {
            window.location.href = 'login.html';
        }, 2000);
        return;
    }
    
    try {
        select.innerHTML = '<option value="">-- Загрузка... --</option>';
        select.disabled = true;
        
        const response = await AuthManager.authenticatedFetch('/api/suppliers');
        
        if (!response.ok) {
            let errorData;
            try {
                errorData = await response.json();
            } catch (e) {
                errorData = { error: `HTTP ${response.status}: ${response.statusText}` };
            }
            
            if (response.status === 401) {
                throw new Error('Сессия истекла или токен недействителен');
            }
            
            throw new Error(errorData.error || `HTTP ${response.status}`);
        }
        
        const suppliers = await response.json();
        
        select.innerHTML = '<option value="">-- Все поставщики --</option>';
        
        if (Array.isArray(suppliers) && suppliers.length > 0) {
            suppliers.forEach(s => {
                const option = document.createElement('option');
                option.value = s.supplier_id;
                option.textContent = s.name;
                select.appendChild(option);
            });
        }
        
        select.disabled = false;
    } catch (error) {
        console.error('Ошибка загрузки поставщиков:', error);
        select.innerHTML = '<option value="">-- Ошибка загрузки --</option>';
        select.disabled = false;
        
        let errorMessage = error.message;
        if (errorMessage.includes('Failed to fetch') || errorMessage.includes('NetworkError')) {
            const apiUrl = AuthManager.getApiUrl();
            errorMessage = `Не удалось подключиться к серверу API: ${apiUrl}`;
        } else if (errorMessage.includes('Сессия истекла')) {
            setTimeout(() => AuthManager.logout(), 2000);
        }
        
        showError(`Ошибка загрузки поставщиков: ${errorMessage}`);
    }
}

async function loadRegions() {
    try {
        const response = await AuthManager.authenticatedFetch('/api/region?limit=1000');
        if (response.ok) {
            const data = await response.json();
            const regions = data.regions || data || [];
            allRegions = regions;
            const select = document.getElementById('regionSelect');
            const formSelect = document.getElementById('formRegionId');
            
            const populateSelect = (sel) => {
                if (sel && Array.isArray(regions)) {
                    regions.forEach(r => {
                        const option = document.createElement('option');
                        option.value = r.RegionID || r.region_id;
                        option.textContent = r.Name || r.name || r.Code || r.code || option.value;
                        sel.appendChild(option);
                    });
                }
            };
            
            populateSelect(select);
            populateSelect(formSelect);
        }
    } catch (error) {
        console.warn('Ошибка загрузки регионов:', error);
    }
}

function renderPriceRow(price) {
    const basePrice = price.price ? price.price.toFixed(2) : '-';
    const markupPct = price.markup_pct !== undefined ? price.markup_pct.toFixed(2) + '%' : '0%';
    const finalPrice = price.final_price ? price.final_price.toFixed(2) : basePrice;
    const regionName = price.region_name || '-';
    const status = price.is_active ? 'Активен' : 'Неактивен';
    return `
        <td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(price.supplier_item_name || price.drug_name || price.item_name || '-')}</td>
        <td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(price.supplier_item_code || price.item_code || '-')}</td>
        <td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(price.inn || '-')}</td>
        <td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(price.cure_form || '-')}</td>
        <td style="padding: 8px; border: 1px solid #ddd; text-align: right;">${basePrice}</td>
        <td style="padding: 8px; border: 1px solid #ddd; text-align: right;">${markupPct}</td>
        <td style="padding: 8px; border: 1px solid #ddd; text-align: right; font-weight: bold;">${finalPrice}</td>
        <td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(regionName)}</td>
        <td style="padding: 8px; border: 1px solid #ddd;">${price.invoice_date ? new Date(price.invoice_date).toLocaleDateString('ru-RU') : '-'}</td>
        <td style="padding: 8px; border: 1px solid #ddd;">${status}</td>
        <td style="padding: 8px; border: 1px solid #ddd;">
            <button onclick="editPrice('${price.supplier_price_id}')" class="btn btn-sm" style="padding: 4px 8px; margin: 2px;">Edit</button>
            <button onclick="togglePrice('${price.supplier_price_id}')" class="btn btn-sm" style="padding: 4px 8px; margin: 2px;">${price.is_active ? 'Pause' : 'Play'}</button>
            <button onclick="deletePrice('${price.supplier_price_id}')" class="btn btn-sm" style="padding: 4px 8px; margin: 2px; background: #dc3545;">Del</button>
        </td>`;
}

function rebuildPricesTable(prices) {
    const table = document.getElementById('pricesTable');
    const tbody = document.getElementById('pricesTableBody');
    const summaryDiv = document.getElementById('pricesSummary');
    if (!tbody) return;
    tbody.innerHTML = '';
    summaryDiv.style.display = 'none';
    if (prices.length === 0) {
        table.style.display = 'none';
        return;
    }
    table.style.display = 'table';
    prices.forEach(price => {
        const row = document.createElement('tr');
        row.innerHTML = renderPriceRow(price);
        row.dataset.priceId = price.supplier_price_id;
        tbody.appendChild(row);
    });
}

async function loadSupplierPrices() {
    const gen = ++_pricesLoadGen;
    const supplierID = document.getElementById('supplierSelect').value;
    if (!supplierID) {
        showError('Сначала выберите поставщика');
        return;
    }

    const loadBtn = document.getElementById('loadPricesBtn');
    const tbody = document.getElementById('pricesTableBody');
    const regionID = document.getElementById('regionSelect').value;

    let baseUrl = `/api/supplier-prices?supplier_id=${supplierID}`;
    if (regionID) baseUrl += `&region_id=${regionID}`;

    try {
        if (loadBtn) showButtonLoading(loadBtn, 'Загрузка...');
        if (tbody) showElementLoading(tbody, 'Загрузка прайсов...');

        const response = await AuthManager.authenticatedFetch(baseUrl + `&limit=${PRICES_FIRST_BATCH}`);
        if (gen !== _pricesLoadGen) return;
        if (!response.ok) { const error = await response.json(); throw new Error(error.error || 'Ошибка загрузки'); }

        const data = await response.json();
        let allPrices = data.prices || [];
        const totalDB = data.stats?.total_in_db || allPrices.length;

        rebuildPricesTable(allPrices);
        showSuccess(`Загружено ${allPrices.length} из ${totalDB} прайсов...`);

        if (allPrices.length < totalDB) {
            const resp2 = await AuthManager.authenticatedFetch(baseUrl + `&offset=${allPrices.length}`);
            if (gen !== _pricesLoadGen) return;
            if (resp2.ok) {
                const data2 = await resp2.json();
                const more = data2.prices || [];
                if (more.length > 0) {
                    allPrices = allPrices.concat(more);
                    rebuildPricesTable(allPrices);
                }
            }
        }
        showSuccess(`Загружено прайсов: ${allPrices.length}`);
    } catch (error) {
        if (gen !== _pricesLoadGen) return;
        showError(`Ошибка загрузки прайсов: ${error.message}`);
    } finally {
        if (loadBtn) hideButtonLoading(loadBtn);
        if (tbody) hideElementLoading(tbody);
    }
}

async function loadPriceSummary() {
    const supplierID = document.getElementById('supplierSelect').value;
    const regionID = document.getElementById('regionSelect').value;
    
    try {
        let url = '/api/supplier-prices/summary';
        const params = new URLSearchParams();
        if (supplierID) {
            params.append('supplier_id', supplierID);
        }
        if (regionID) {
            params.append('region_id', regionID);
        }
        if (params.toString()) {
            url += '?' + params.toString();
        }
        
        const response = await AuthManager.authenticatedFetch(url);
        
        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Ошибка загрузки');
        }
        
        const data = await response.json();
        const summaryDiv = document.getElementById('pricesSummary');
        const table = document.getElementById('pricesTable');
        const tbody = document.getElementById('pricesTableBody');
        
        table.style.display = 'none';
        tbody.innerHTML = '';
        
        if (data.summary && data.summary.length > 0) {
            const hasSupplierInfo = data.summary.some(p => p.supplier_name);
            const hasRegionInfo = data.summary.some(p => p.region_name);
            const hasMarkupInfo = data.summary.some(p => p.markup_pct && p.markup_pct > 0);
            
            let html = `<h5>Сводный прайс (финальные цены с учетом наценок): ${data.summary.length} ${hasSupplierInfo ? 'позиций' : 'препаратов'}</h5>`;
            html += '<table style="width: 100%; border-collapse: collapse; margin-top: 10px;">';
            html += '<thead><tr style="background: #e0e0e0;">';
            html += '<th style="padding: 10px; border: 1px solid #ddd;">Препарат</th>';
            if (hasSupplierInfo) {
                html += '<th style="padding: 10px; border: 1px solid #ddd;">Поставщик</th>';
            }
            html += '<th style="padding: 10px; border: 1px solid #ddd;">МНН</th>';
            html += '<th style="padding: 10px; border: 1px solid #ddd;">Форма</th>';
            if (hasMarkupInfo) {
                html += '<th style="padding: 10px; border: 1px solid #ddd;">Базовая цена</th>';
                html += '<th style="padding: 10px; border: 1px solid #ddd;">Наценка</th>';
            }
            html += '<th style="padding: 10px; border: 1px solid #ddd;">Финальная цена</th>';
            if (hasRegionInfo) {
                html += '<th style="padding: 10px; border: 1px solid #ddd;">Регион</th>';
            }
            html += '<th style="padding: 10px; border: 1px solid #ddd;">Дата</th>';
            html += '</tr></thead><tbody>';
            
            data.summary.forEach(price => {
                const date = price.last_price_date ? new Date(price.last_price_date).toLocaleDateString('ru-RU') : '-';
                const basePrice = price.base_price ? price.base_price.toFixed(2) : (price.price ? price.price.toFixed(2) : '-');
                const markupPct = price.markup_pct ? price.markup_pct.toFixed(2) + '%' : '0%';
                const finalPrice = price.price ? price.price.toFixed(2) : '-';
                
                html += `<tr>
                    <td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(price.drug_name || '-')}</td>`;
                if (hasSupplierInfo) {
                    html += `<td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(price.supplier_name || '-')}</td>`;
                }
                html += `<td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(price.inn || '-')}</td>
                    <td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(price.cure_form || '-')}</td>`;
                
                if (hasMarkupInfo) {
                    html += `<td style="padding: 8px; border: 1px solid #ddd; text-align: right;">${basePrice}</td>
                        <td style="padding: 8px; border: 1px solid #ddd; text-align: right;">${markupPct}</td>`;
                }
                
                html += `<td style="padding: 8px; border: 1px solid #ddd; text-align: right; font-weight: bold; color: #28a745;">${finalPrice}</td>`;
                
                if (hasRegionInfo) {
                    html += `<td style="padding: 8px; border: 1px solid #ddd;">${escapeHtml(price.region_name || '-')}</td>`;
                }
                
                html += `<td style="padding: 8px; border: 1px solid #ddd;">${date}</td>
                </tr>`;
            });
            
            html += '</tbody></table>';
            summaryDiv.innerHTML = html;
            summaryDiv.style.display = 'block';
            
            let message = supplierID 
                ? `Загружен сводный прайс поставщика: ${data.summary.length} позиций`
                : `Загружен сводный прайс всех поставщиков: ${data.summary.length} позиций`;
            
            if (regionID) {
                const regionName = document.getElementById('regionSelect').options[document.getElementById('regionSelect').selectedIndex].text;
                message += ` (фильтр: ${regionName})`;
            }
            
            showSuccess(message);
        } else {
            summaryDiv.innerHTML = '<div style="color: #dc3545;">Прайсы не найдены. Сначала запустите сопоставление или добавьте прайсы.</div>';
            summaryDiv.style.display = 'block';
        }
    } catch (error) {
        showError(`Ошибка загрузки сводного прайса: ${error.message}`);
    }
}

function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Функции showError и showSuccess теперь определены в toast.js

// Управление формой прайса
function showAddPriceForm() {
    document.getElementById('priceFormTitle').textContent = 'Добавление прайса';
    document.getElementById('priceId').value = '';
    document.getElementById('priceForm').reset();
    document.getElementById('formIsActive').checked = true;
    currentMarkupSupplierID = null;
    
    // Скрываем секцию наценок при добавлении нового прайса
    const markupSection = document.getElementById('priceFormMarkupSection');
    if (markupSection) {
        markupSection.style.display = 'none';
    }
    
    // Вычисления выполняются на сервере
    document.getElementById('priceFormSection').style.display = 'block';
    document.getElementById('formSupplierId').innerHTML = document.getElementById('supplierSelect').innerHTML;
}

function hidePriceForm() {
    document.getElementById('priceFormSection').style.display = 'none';
}

// Функция удалена - все вычисления выполняются на сервере для безопасности

async function handlePriceFormSubmit(e) {
    e.preventDefault();
    
    const priceId = document.getElementById('priceId').value;
    const supplierID = document.getElementById('formSupplierId').value;
    const guidES = document.getElementById('formGuidES').value.trim();
    const price = parseFloat(document.getElementById('formPrice').value);
    const markupPct = parseFloat(document.getElementById('formMarkupPct').value) || 0;
    const regionID = document.getElementById('formRegionId').value;
    const itemCode = document.getElementById('formItemCode').value.trim();
    const itemName = document.getElementById('formItemName').value.trim();
    const isActive = document.getElementById('formIsActive').checked;
    
    if (!supplierID || !itemCode || !itemName || !price) {
        showError('Заполните все обязательные поля (поставщик, код товара, наименование, цена)');
        return;
    }
    
    try {
        if (priceId) {
            // Обновление существующего прайса
            const response = await AuthManager.authenticatedFetch(`/api/supplier-prices/${priceId}`, {
                method: 'PUT',
                body: JSON.stringify({
                    price: price,
                    markup_pct: markupPct,
                    region_id: regionID || null,
                    is_active: isActive
                })
            });
            
            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || 'Ошибка обновления');
            }
            
            showSuccess('Прайс успешно обновлен');
        } else {
            // Создание нового прайса
            const response = await AuthManager.authenticatedFetch('/api/supplier-prices/create', {
                method: 'POST',
                body: JSON.stringify({
                    supplier_id: supplierID,
                    guid_es: guidES || null, // Опционально - только если товар является препаратом
                    price: price,
                    markup_pct: markupPct,
                    region_id: regionID || null,
                    item_code: itemCode,
                    item_name: itemName,
                    is_active: isActive
                })
            });
            
            if (!response.ok) {
                const error = await response.json();
                throw new Error(error.error || 'Ошибка создания');
            }
            
            showSuccess('Прайс успешно создан');
        }
        
        hidePriceForm();
        loadSupplierPrices();
    } catch (error) {
        showError(`Ошибка сохранения прайса: ${error.message}`);
    }
}

async function editPrice(priceId) {
    try {
        const supplierID = document.getElementById('supplierSelect').value;
        if (!supplierID) {
            showError('Сначала выберите поставщика');
            return;
        }
        
        const response = await AuthManager.authenticatedFetch(`/api/supplier-prices?supplier_id=${supplierID}`);
        if (!response.ok) throw new Error('Ошибка загрузки');
        
        const data = await response.json();
        const price = data.prices.find(p => p.supplier_price_id === priceId);
        if (!price) {
            showError('Прайс не найден');
            return;
        }
        
        document.getElementById('priceFormTitle').textContent = 'Редактирование прайса';
        document.getElementById('priceId').value = priceId;
        document.getElementById('formSupplierId').value = price.supplier_id;
        document.getElementById('formGuidES').value = price.guid_es || '';
        document.getElementById('formPrice').value = price.price || '';
        document.getElementById('formMarkupPct').value = price.markup_pct || 0;
        document.getElementById('formRegionId').value = price.region_id || '';
        document.getElementById('formItemCode').value = price.supplier_item_code || '';
        document.getElementById('formItemName').value = price.supplier_item_name || '';
        document.getElementById('formIsActive').checked = price.is_active !== false;
        
        // Загружаем региональные наценки для поставщика
        if (price.supplier_id) {
            currentMarkupSupplierID = price.supplier_id;
            loadSupplierMarkupPolicies(price.supplier_id);
        }
        
        calculateFinalPrice();
        document.getElementById('priceFormSection').style.display = 'block';
        
        // Показываем секцию наценок
        const markupSection = document.getElementById('priceFormMarkupSection');
        if (markupSection) {
            markupSection.style.display = 'block';
        }
    } catch (error) {
        showError(`Ошибка редактирования: ${error.message}`);
    }
}

async function togglePrice(priceId) {
    try {
        const response = await AuthManager.authenticatedFetch(`/api/supplier-prices/${priceId}/toggle`, {
            method: 'PUT'
        });
        
        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Ошибка переключения');
        }
        
        const result = await response.json();
        showSuccess(result.message || 'Статус прайса изменен');
        loadSupplierPrices();
    } catch (error) {
        showError(`Ошибка переключения прайса: ${error.message}`);
    }
}

async function deletePrice(priceId) {
    if (!confirm('Вы уверены, что хотите удалить этот прайс?')) {
        return;
    }
    
    try {
        const response = await AuthManager.authenticatedFetch(`/api/supplier-prices/${priceId}`, {
            method: 'DELETE'
        });
        
        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Ошибка удаления');
        }
        
        showSuccess('Прайс успешно удален');
        loadSupplierPrices();
    } catch (error) {
        showError(`Ошибка удаления прайса: ${error.message}`);
    }
}

// Экспорт функций для использования в onclick
// Функции для работы с наценками по регионам
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
        
        // Отображаем региональные наценки (только с region_id)
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

function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

window.editPrice = editPrice;
window.togglePrice = togglePrice;
window.deletePrice = deletePrice;
window.editRegionMarkup = editRegionMarkup;
window.deleteRegionMarkup = deleteRegionMarkup;

