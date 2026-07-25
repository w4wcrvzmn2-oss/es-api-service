let accessPointsDT = null;

document.addEventListener('DOMContentLoaded', async () => {
    await loadAccessPoints();
});

async function loadAccessPoints(opts = {}) {
    const tbody = document.getElementById('accessPointsTable');
    if (!tbody) return;

    const silent = !!opts.silent;
    if (!silent) {
        tbody.innerHTML = '<tr><td colspan="5" class="loading">Загрузка данных...</td></tr>';
    }

    try {
        const data = await API.get('/api/import-points');
        if (data && data.length > 0) {
            tbody.innerHTML = data.map(point => {
                const sourceType = point.source_type || 'local';
                let typeLabel, pathInfo;

                if (sourceType === 'ftp') {
                    typeLabel = 'FTP';
                    pathInfo = point.ftp_host ? `${point.ftp_host}${point.ftp_remote_path || '/'}` : '-';
                } else {
                    typeLabel = 'Локальная';
                    pathInfo = point.source_file_path || point.dbf_file_path || '-';
                }

                return `
                <tr data-id="${point.import_point_id}">
                    <td style="text-align: center;">
                        <input type="checkbox" ${point.is_active ? 'checked' : ''} 
                               onchange="togglePointActive('${point.import_point_id}', this.checked)"
                               title="Активность точки">
                    </td>
                    <td><strong>${escapeHtml(point.name || '-')}</strong></td>
                    <td>${escapeHtml(typeLabel)}</td>
                    <td>${escapeHtml(pathInfo)}</td>
                    <td style="text-align: center;">
                        <a href="access-point-edit.html?id=${point.import_point_id}" class="btn-icon" title="Редактировать">✏️</a>
                        <button class="btn-icon btn-danger-icon" onclick="deleteAccessPoint('${point.import_point_id}', this)" title="Удалить" style="margin-left: 8px;">🗑️</button>
                    </td>
                </tr>
                `;
            }).join('');
        } else {
            tbody.innerHTML = '<tr><td colspan="5" class="empty-row">Нет данных</td></tr>';
        }
        if (accessPointsDT) ElfTable.destroy(accessPointsDT);
        accessPointsDT = ElfTable.init('tblAccessPoints', { sortColumn: 1, sortDir: 'asc', unsortable: [0, 4], exportName: 'Точки_доступа' });
        TableFilters.setup('tblAccessPoints');
    } catch (error) {
        console.error('Ошибка загрузки точек доступа:', error);
        if (!silent) {
            tbody.innerHTML = '<tr><td colspan="5" class="empty-row">Ошибка загрузки данных</td></tr>';
        }
    }
}

async function togglePointActive(pointId, isActive) {
    console.log('Toggle point active:', pointId, isActive);
}

async function deleteAccessPoint(pointId, btn) {
    if (!confirm('Вы уверены, что хотите удалить эту точку доступа?')) {
        return;
    }

    const row = btn ? btn.closest('tr') : document.querySelector(`tr[data-id="${pointId}"]`);
    if (row) {
        row.style.opacity = '0.35';
        row.style.pointerEvents = 'none';
    }

    try {
        const result = await API.delete(`/api/import-points/${pointId}`);
        if (!result) throw new Error('Ошибка удаления');
        Toast.success('Удалено', 'Точка доступа удалена');
        if (accessPointsDT) {
            ElfTable.destroy(accessPointsDT);
            accessPointsDT = null;
        }
        if (row) row.remove();
        const tbody = document.getElementById('accessPointsTable');
        if (tbody && !tbody.querySelector('tr[data-id]')) {
            tbody.innerHTML = '<tr><td colspan="5" class="empty-row">Нет данных</td></tr>';
        } else if (tbody && tbody.querySelector('tr[data-id]')) {
            accessPointsDT = ElfTable.init('tblAccessPoints', { sortColumn: 1, sortDir: 'asc', unsortable: [0, 4], exportName: 'Точки_доступа' });
            TableFilters.setup('tblAccessPoints');
        }
    } catch (error) {
        console.error('Ошибка удаления:', error);
        if (row) {
            row.style.opacity = '';
            row.style.pointerEvents = '';
        }
        Toast.error('Ошибка удаления', error.message);
    }
}

function escapeHtml(text) {
    if (text == null) return '';
    const div = document.createElement('div');
    div.textContent = String(text);
    return div.innerHTML;
}
