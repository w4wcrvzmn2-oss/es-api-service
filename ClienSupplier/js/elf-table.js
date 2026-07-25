/**
 * ElfTable — обёртка над Simple-DataTables с экспортом CSV / Excel / PDF.
 * Использование:  const dt = elfTable('myTableId', { sortColumn: 1, sortDir: 'asc', ... });
 */
var ElfTable = (function () {
    'use strict';

    const LABELS = {
        placeholder: 'Поиск…',
        searchTitle: 'Поиск в таблице',
        perPage: 'записей на странице',
        noRows: 'Нет данных',
        noResults: 'Ничего не найдено',
        info: 'Записи {start}–{end} из {rows}',
    };

    function _tpl(options, dom) {
        var top = "<div class='" + options.classes.top + "'>";
        if (options.searchable) {
            top += "<div class='" + options.classes.search + "'>" +
                "<input class='" + options.classes.input + "' placeholder='" + options.labels.placeholder +
                "' type='search' title='" + options.labels.searchTitle + "'" +
                (dom.id ? " aria-controls='" + dom.id + "'" : "") + ">" +
                "</div>";
        }
        top += "</div>";

        var container = "<div class='" + options.classes.container + "'" +
            (options.scrollY && options.scrollY.length ? " style='height:" + options.scrollY + ";overflow-Y:auto;'" : "") +
            "></div>";

        var bottom = "<div class='" + options.classes.bottom + "'>";
        if (options.paging) {
            bottom += "<div class='" + options.classes.info + "'></div>";
        }
        bottom += "<nav class='" + options.classes.pagination + "'></nav>";
        if (options.paging && options.perPageSelect) {
            bottom += "<div class='" + options.classes.dropdown + "'>" +
                "<label><select class='" + options.classes.selector + "'></select> " +
                options.labels.perPage + "</label></div>";
        }
        bottom += "</div>";

        return top + container + bottom;
    }

    function init(tableId, opts) {
        opts = opts || {};
        var el = document.getElementById(tableId);
        if (!el) { console.warn('ElfTable: таблица #' + tableId + ' не найдена'); return null; }

        var unsortable = (opts.unsortable || []).map(function (i) {
            return { select: i, sortable: false };
        });

        var dtOpts = {
            labels: LABELS,
            perPage: opts.perPage || 25,
            perPageSelect: [10, 25, 50, 100],
            searchable: opts.searchable !== false,
            sortable: opts.sortable !== false,
            header: true,
            columns: unsortable,
            template: _tpl,
            classes: {
                active: 'datatable-active',
                bottom: 'datatable-bottom',
                container: 'datatable-container table-responsive',
                cursor: 'datatable-cursor',
                dropdown: 'datatable-dropdown',
                ellipsis: 'datatable-ellipsis',
                empty: 'datatable-empty',
                headercontainer: 'datatable-headercontainer',
                info: 'datatable-info',
                input: 'datatable-input form-control form-control-sm',
                loading: 'datatable-loading',
                pagination: 'datatable-pagination',
                paginationList: 'datatable-pagination-list pagination pagination-sm mb-0',
                paginationListItem: 'datatable-pagination-list-item page-item',
                paginationListItemLink: 'datatable-pagination-list-item-link page-link',
                search: 'datatable-search',
                selector: 'datatable-selector form-select form-select-sm',
                sorter: 'datatable-sorter',
                table: 'datatable-table ' + (el.className || ''),
                top: 'datatable-top',
                wrapper: 'datatable-wrapper',
            },
        };

        var dt = new simpleDatatables.DataTable('#' + tableId, dtOpts);

        dt.on('datatable.init', function () {
            if (opts.sortColumn !== undefined) {
                dt.columns.sort(opts.sortColumn, opts.sortDir || 'asc');
            }
        });

        _addExportBar(dt, el, opts.exportName || tableId);

        return dt;
    }

    function _addExportBar(dt, tableEl, name) {
        var wrapper = tableEl.closest('.datatable-wrapper');
        if (!wrapper) {
            dt.on('datatable.init', function () {
                wrapper = tableEl.closest('.datatable-wrapper');
                if (wrapper) _insertBar(wrapper, dt, tableEl, name);
            });
        } else {
            _insertBar(wrapper, dt, tableEl, name);
        }
    }

    function _insertBar(wrapper, dt, tableEl, name) {
        var top = wrapper.querySelector('.datatable-top');
        if (!top) return;
        var bar = document.createElement('div');
        bar.className = 'elf-export-bar';
        bar.innerHTML =
            '<button type="button" class="btn btn-outline-secondary btn-sm" data-export="csv"><i class="bi bi-filetype-csv"></i> CSV</button>' +
            '<button type="button" class="btn btn-outline-secondary btn-sm" data-export="excel"><i class="bi bi-file-earmark-spreadsheet"></i> Excel</button>' +
            '<button type="button" class="btn btn-outline-secondary btn-sm" data-export="pdf"><i class="bi bi-filetype-pdf"></i> PDF</button>';
        top.appendChild(bar);

        bar.querySelector('[data-export="csv"]').onclick = function () { _exportCSV(tableEl, name); };
        bar.querySelector('[data-export="excel"]').onclick = function () { _exportExcel(tableEl, name); };
        bar.querySelector('[data-export="pdf"]').onclick = function () { _exportPDF(tableEl, name); };
    }

    function _getVisibleData(tableEl) {
        var thead = tableEl.querySelector('thead');
        var headers = [];
        if (thead) {
            thead.querySelectorAll('th').forEach(function (th) { headers.push(th.textContent.trim()); });
        }
        var rows = [];
        tableEl.querySelectorAll('tbody tr').forEach(function (tr) {
            if (tr.querySelector('.datatable-empty')) return;
            var row = [];
            tr.querySelectorAll('td').forEach(function (td) { row.push(td.textContent.trim()); });
            rows.push(row);
        });
        return { headers: headers, rows: rows };
    }

    function _exportCSV(tableEl, name) {
        var d = _getVisibleData(tableEl);
        var bom = '\uFEFF';
        var csv = bom + d.headers.join(';') + '\n' + d.rows.map(function (r) {
            return r.map(function (c) { return '"' + c.replace(/"/g, '""') + '"'; }).join(';');
        }).join('\n');
        _download(csv, name + '.csv', 'text/csv;charset=utf-8;');
    }

    function _xlsxUrl() {
        var path = (window.location && window.location.pathname) || '';
        if (path.indexOf('/pages/') !== -1) return '../vendor/xlsx.full.min.js';
        return 'vendor/xlsx.full.min.js';
    }

    var _xlsxPromise = null;
    function _ensureXlsx() {
        if (typeof XLSX !== 'undefined') return Promise.resolve();
        if (_xlsxPromise) return _xlsxPromise;
        _xlsxPromise = new Promise(function (resolve, reject) {
            var s = document.createElement('script');
            s.src = _xlsxUrl();
            s.async = true;
            s.onload = function () { resolve(); };
            s.onerror = function () {
                _xlsxPromise = null;
                reject(new Error('Не удалось загрузить SheetJS'));
            };
            document.head.appendChild(s);
        });
        return _xlsxPromise;
    }

    function _exportExcel(tableEl, name) {
        _ensureXlsx().then(function () {
            var wb = XLSX.utils.table_to_book(tableEl, { sheet: 'Данные', raw: false });
            XLSX.writeFile(wb, name + '.xlsx');
        }).catch(function (err) {
            if (typeof Toast !== 'undefined') Toast.error('Ошибка', err.message || 'Библиотека SheetJS не загружена');
            else alert(err.message || 'Библиотека SheetJS не загружена');
        });
    }

    function _exportPDF(tableEl, name) {
        var d = _getVisibleData(tableEl);
        var html = '<!DOCTYPE html><html><head><meta charset="utf-8"><title>' + _escapeHtml(name) + '</title>' +
            '<style>' +
            '@page{size:landscape A4;margin:12mm}' +
            'body{font-family:Arial,sans-serif;font-size:11px;color:#222}' +
            'h2{font-size:16px;margin:0 0 8px}' +
            'small{color:#888}' +
            'table{border-collapse:collapse;width:100%}' +
            'th{background:#31528f;color:#fff;font-weight:600;padding:6px 8px;text-align:left;font-size:10px}' +
            'td{border-bottom:1px solid #ddd;padding:5px 8px;font-size:10px}' +
            'tr:nth-child(even){background:#f8f9fa}' +
            '</style></head><body>' +
            '<h2>' + _escapeHtml(name) + '</h2>' +
            '<small>' + new Date().toLocaleDateString('ru-RU') + '</small>' +
            '<table><thead><tr>';
        d.headers.forEach(function (h) { html += '<th>' + _escapeHtml(h) + '</th>'; });
        html += '</tr></thead><tbody>';
        d.rows.forEach(function (row) {
            html += '<tr>';
            row.forEach(function (c) { html += '<td>' + _escapeHtml(c) + '</td>'; });
            html += '</tr>';
        });
        html += '</tbody></table></body></html>';

        var w = window.open('', '_blank');
        w.document.write(html);
        w.document.close();
        w.onload = function () { w.print(); };
    }

    function _escapeHtml(value) {
        var div = document.createElement('div');
        div.textContent = String(value == null ? '' : value);
        return div.innerHTML;
    }

    function _download(content, fileName, mimeType) {
        var blob = new Blob([content], { type: mimeType });
        var url = URL.createObjectURL(blob);
        var a = document.createElement('a');
        a.href = url; a.download = fileName;
        document.body.appendChild(a); a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
    }

    function destroy(dt) {
        if (dt && dt.destroy) dt.destroy();
    }

    return { init: init, destroy: destroy };
})();
