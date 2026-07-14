/**
 * TableFilters — Excel-style column filters with checkboxes + partial text search.
 *
 * TableFilters.setup(tableId, opts)
 *   opts.onFilter(filters)   — callback, filters = { colIdx: { q, partial, values } }
 *   opts.getValues(colIdx)   — returns sorted unique values for column
 */
var TableFilters = (function () {
    'use strict';

    var _openDropdown = null;
    var _openCtx = null;

    document.addEventListener('click', function (e) {
        if (_openDropdown && !_openDropdown.contains(e.target) && !e.target.closest('.elf-cf-icon')) {
            _closeDropdown();
        }
    });

    function _closeDropdown() {
        if (_openDropdown) { _openDropdown.remove(); _openDropdown = null; _openCtx = null; }
    }

    function setup(tableId, opts) {
        opts = opts || {};
        var tbl = document.getElementById(tableId);
        if (!tbl) return null;
        var thead = tbl.querySelector('thead');
        if (!thead) return null;
        var headerRow = thead.querySelector('tr');
        if (!headerRow) return null;

        /*  activeFilters[colIdx] = { partial: bool, q: string, values: Set }
         *  partial=true  → text search (q contains search text)
         *  partial=false → checkbox multi-select (values is Set of selected vals)
         */
        var activeFilters = {};
        var ths = headerRow.querySelectorAll('th');
        var icons = [];

        ths.forEach(function (th, idx) {
            if (th.hasAttribute('data-no-filter')) return;
            th.style.position = 'relative';
            var icon = document.createElement('span');
            icon.className = 'elf-cf-icon';
            icon.innerHTML = ' &#9660;';
            icon.dataset.colIdx = idx;
            icon.title = 'Фильтр';
            th.appendChild(icon);
            icons.push(icon);

            icon.addEventListener('click', function (e) {
                e.stopPropagation();
                e.preventDefault();
                if (_openDropdown && _openCtx && _openCtx.colIdx === idx && _openCtx.tableId === tableId) {
                    _closeDropdown();
                    return;
                }
                _closeDropdown();
                _showDropdown(icon, idx);
            });
        });

        var toggleBtn = document.createElement('button');
        toggleBtn.type = 'button';
        toggleBtn.className = 'btn btn-sm btn-outline-secondary elf-filter-toggle';
        toggleBtn.innerHTML = '<i class="bi bi-funnel"></i>';
        toggleBtn.title = 'Сбросить все фильтры';
        toggleBtn.style.display = 'none';
        toggleBtn.addEventListener('click', function () {
            activeFilters = {};
            icons.forEach(function (ic) { ic.classList.remove('active'); });
            toggleBtn.style.display = 'none';
            _fireFilters();
        });

        _insertToggle(tbl, toggleBtn);

        function _getValuesFromDom(colIdx) {
            var vals = new Set();
            var tbody = tbl.querySelector('tbody');
            if (!tbody) return [];
            tbody.querySelectorAll('tr').forEach(function (tr) {
                var cells = tr.querySelectorAll('td');
                if (cells[colIdx]) {
                    var t = cells[colIdx].textContent.trim();
                    if (t) vals.add(t);
                }
            });
            return Array.from(vals).sort(function (a, b) {
                return a.localeCompare(b, 'ru', { numeric: true, sensitivity: 'base' });
            });
        }

        function _showDropdown(icon, colIdx) {
            var allValues = opts.getValues ? opts.getValues(colIdx) : _getValuesFromDom(colIdx);
            var cur = activeFilters[colIdx];

            var dd = document.createElement('div');
            dd.className = 'elf-cf-dropdown';
            dd.dataset.colIdx = colIdx;

            // --- search + apply partial ---
            var searchWrap = document.createElement('div');
            searchWrap.className = 'elf-cf-search-wrap';
            var search = document.createElement('input');
            search.type = 'search';
            search.className = 'form-control form-control-sm elf-cf-search';
            search.placeholder = 'Текст фильтра…';
            if (cur && cur.partial) search.value = cur.q || '';
            var applyBtn = document.createElement('button');
            applyBtn.type = 'button';
            applyBtn.className = 'btn btn-sm btn-primary elf-cf-apply';
            applyBtn.innerHTML = '<i class="bi bi-search"></i>';
            applyBtn.title = 'Фильтровать по тексту';
            searchWrap.appendChild(search);
            searchWrap.appendChild(applyBtn);
            dd.appendChild(searchWrap);

            var sep = document.createElement('div');
            sep.className = 'elf-cf-sep';
            dd.appendChild(sep);

            // --- checkbox list ---
            var list = document.createElement('div');
            list.className = 'elf-cf-list';

            // "(Все)" row
            var selAll = !cur || (cur && !cur.partial && cur.values && cur.values.size === allValues.length);
            var allRow = _makeCheckRow('(Все)', selAll, true);
            list.appendChild(allRow.el);

            var checkRows = [];
            allValues.forEach(function (val) {
                var checked = selAll || (cur && !cur.partial && cur.values && cur.values.has(val));
                var row = _makeCheckRow(val, checked, false);
                checkRows.push(row);
                list.appendChild(row.el);
            });

            // "(Все)" toggle logic
            allRow.cb.addEventListener('change', function () {
                var on = allRow.cb.checked;
                checkRows.forEach(function (r) { r.cb.checked = on; });
            });

            dd.appendChild(list);

            // --- footer buttons ---
            var footer = document.createElement('div');
            footer.className = 'elf-cf-footer';
            var resetBtn = document.createElement('button');
            resetBtn.type = 'button';
            resetBtn.className = 'btn btn-sm btn-outline-danger elf-cf-reset';
            resetBtn.textContent = 'Сбросить';
            var spacer = document.createElement('span');
            spacer.style.flex = '1';
            var okBtn = document.createElement('button');
            okBtn.type = 'button';
            okBtn.className = 'btn btn-sm btn-primary';
            okBtn.textContent = 'OK';
            var cancelBtn = document.createElement('button');
            cancelBtn.type = 'button';
            cancelBtn.className = 'btn btn-sm btn-outline-secondary';
            cancelBtn.textContent = 'Отмена';
            footer.appendChild(resetBtn);
            footer.appendChild(spacer);
            footer.appendChild(okBtn);
            footer.appendChild(cancelBtn);
            dd.appendChild(footer);

            // --- actions ---
            function applyPartial() {
                var q = search.value.trim();
                if (!q) { _clearCol(colIdx, icon); return; }
                activeFilters[colIdx] = { partial: true, q: q };
                icon.classList.add('active');
                _updateToggle();
                _closeDropdown();
                _fireFilters();
            }

            applyBtn.addEventListener('click', applyPartial);
            search.addEventListener('keydown', function (e) {
                if (e.key === 'Enter') { e.preventDefault(); applyPartial(); }
            });

            search.addEventListener('input', function () {
                var q = search.value.trim().toLowerCase();
                checkRows.forEach(function (r) {
                    r.el.style.display = r.val.toLowerCase().indexOf(q) !== -1 ? '' : 'none';
                });
            });

            okBtn.addEventListener('click', function () {
                var selected = new Set();
                checkRows.forEach(function (r) { if (r.cb.checked) selected.add(r.val); });
                if (selected.size === 0 || selected.size === allValues.length) {
                    _clearCol(colIdx, icon);
                } else {
                    activeFilters[colIdx] = { partial: false, values: selected };
                    icon.classList.add('active');
                    _updateToggle();
                    _closeDropdown();
                    _fireFilters();
                }
            });

            cancelBtn.addEventListener('click', function () { _closeDropdown(); });

            resetBtn.addEventListener('click', function () {
                _clearCol(colIdx, icon);
            });

            // --- position ---
            var rect = icon.getBoundingClientRect();
            dd.style.top = (rect.bottom + window.scrollY + 2) + 'px';
            dd.style.left = Math.max(0, rect.left + window.scrollX - 120) + 'px';

            document.body.appendChild(dd);
            _openDropdown = dd;
            _openCtx = { colIdx: colIdx, tableId: tableId };
            search.focus();
        }

        function _makeCheckRow(label, checked, isAll) {
            var el = document.createElement('label');
            el.className = 'elf-cf-item' + (isAll ? ' elf-cf-item-all' : '');
            var cb = document.createElement('input');
            cb.type = 'checkbox';
            cb.checked = !!checked;
            cb.className = 'form-check-input elf-cf-cb';
            var span = document.createElement('span');
            span.textContent = label;
            el.appendChild(cb);
            el.appendChild(span);
            return { el: el, cb: cb, val: label };
        }

        function _clearCol(colIdx, icon) {
            delete activeFilters[colIdx];
            icon.classList.remove('active');
            _updateToggle();
            _closeDropdown();
            _fireFilters();
        }

        function _updateToggle() {
            var hasActive = Object.keys(activeFilters).length > 0;
            toggleBtn.style.display = hasActive ? '' : 'none';
            toggleBtn.classList.toggle('active', hasActive);
        }

        function _fireFilters() {
            var filters = {};
            for (var k in activeFilters) {
                var f = activeFilters[k];
                if (f.partial) {
                    filters[k] = { q: f.q.toLowerCase(), partial: true };
                } else {
                    var lowSet = new Set();
                    f.values.forEach(function (v) { lowSet.add(v.toLowerCase()); });
                    filters[k] = { values: lowSet, partial: false };
                }
            }
            if (opts.onFilter) {
                opts.onFilter(filters);
                return;
            }
            // DOM filtering for ElfTable pages
            var tbody = tbl.querySelector('tbody');
            if (!tbody) return;
            tbody.querySelectorAll('tr').forEach(function (tr) {
                var cells = tr.querySelectorAll('td');
                if (cells.length === 0) return;
                var show = true;
                for (var colIdx in activeFilters) {
                    var cell = cells[parseInt(colIdx)];
                    if (!cell) { show = false; break; }
                    var ct = cell.textContent.trim().toLowerCase();
                    var af = activeFilters[colIdx];
                    if (af.partial) {
                        if (ct.indexOf(af.q.toLowerCase()) === -1) { show = false; break; }
                    } else {
                        var match = false;
                        af.values.forEach(function (v) { if (v.toLowerCase() === ct) match = true; });
                        if (!match) { show = false; break; }
                    }
                }
                tr.style.display = show ? '' : 'none';
            });
        }

        return { getFilters: function () { return activeFilters; }, applyFilters: _fireFilters };
    }

    function _insertToggle(tbl, btn) {
        var exportBar = null;
        var pagBar = tbl.parentNode.querySelector('[id$="PagBar"]');
        if (pagBar) {
            exportBar = pagBar.querySelector('.d-flex.gap-1');
        }
        if (!exportBar) {
            var dtWrapper = tbl.closest('.datatable-wrapper');
            if (dtWrapper) exportBar = dtWrapper.querySelector('.elf-export-bar');
        }
        if (exportBar) {
            exportBar.insertBefore(btn, exportBar.firstChild);
        } else if (pagBar) {
            var rightGroup = pagBar.querySelector('.d-flex.align-items-center.gap-2.flex-wrap');
            if (rightGroup) rightGroup.insertBefore(btn, rightGroup.firstChild);
            else pagBar.insertBefore(btn, pagBar.firstChild);
        } else {
            var dtw = tbl.closest('.datatable-wrapper');
            var dtTop = dtw ? dtw.querySelector('.datatable-top') : null;
            if (dtTop) {
                dtTop.insertBefore(btn, dtTop.firstChild);
            } else {
                tbl.parentNode.insertBefore(btn, tbl);
            }
        }
    }

    return { setup: setup };
})();
