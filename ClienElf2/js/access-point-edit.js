// Редактирование точки доступа
let currentAccessPointId = null;

document.addEventListener('DOMContentLoaded', async () => {
    const urlParams = new URLSearchParams(window.location.search);
    currentAccessPointId = urlParams.get('id');
    
    if (currentAccessPointId) {
        document.getElementById('pageTitle').textContent = 'Редактирование точки доступа';
        await loadAccessPoint(currentAccessPointId);
    }
    
    document.getElementById('accessPointForm').addEventListener('submit', handleSubmit);
});

function toggleSourceType() {
    const sourceType = document.querySelector('input[name="sourceType"]:checked').value;
    document.getElementById('localSourceBlock').style.display = sourceType === 'local' ? '' : 'none';
    document.getElementById('ftpSourceBlock').style.display = sourceType === 'ftp' ? '' : 'none';
}

async function testFtpConnection() {
    const btn = document.getElementById('btnFtpTest');
    const status = document.getElementById('ftpTestStatus');
    const host = (document.getElementById('ftpHost').value || '').trim();
    if (!host) {
        status.className = 'small text-danger';
        status.textContent = 'Укажите FTP-хост';
        return;
    }
    const port = parseInt(document.getElementById('ftpPort').value, 10);
    btn.disabled = true;
    status.className = 'small text-muted';
    status.innerHTML = '<span class="spinner-border spinner-border-sm me-1"></span>Проверка…';
    try {
        const res = await API.post('/api/ftp/test', {
            host,
            port: isNaN(port) || port <= 0 ? 21 : port,
            user: document.getElementById('ftpUser').value || '',
            password: document.getElementById('ftpPassword').value || '',
            path: document.getElementById('ftpRemotePath').value || ''
        });
        if (res && res.ok) {
            status.className = 'small text-success fw-semibold';
            status.textContent = '✓ ' + (res.message || 'Соединение успешно');
            if (window.Toast) Toast.success('FTP', res.message || 'Соединение успешно');
        } else {
            const detail = (res && (res.error || res.message)) || 'Не удалось проверить FTP';
            status.className = 'small text-danger';
            status.textContent = '✗ ' + detail;
            if (window.Toast) Toast.error('FTP', detail);
        }
    } catch (error) {
        status.className = 'small text-danger';
        status.textContent = '✗ ' + (error.message || 'Ошибка проверки');
        if (window.Toast) Toast.error('FTP', error.message || 'Ошибка проверки');
    } finally {
        btn.disabled = false;
    }
}

async function loadAccessPoint(id) {
    try {
        const data = await API.get('/api/import-points');
        if (data && data.length > 0) {
            const point = data.find(p => p.import_point_id === id);
            if (point) {
                document.getElementById('name').value = point.name || '';
                document.getElementById('description').value = point.description || '';
                document.getElementById('isActive').checked = point.is_active !== false;

                const sourceType = point.source_type || 'local';
                const radio = document.querySelector(`input[name="sourceType"][value="${sourceType}"]`);
                if (radio) radio.checked = true;
                toggleSourceType();

                document.getElementById('sourceFilePath').value = point.source_file_path || '';
                document.getElementById('ftpHost').value = point.ftp_host || '';
                document.getElementById('ftpPort').value = point.ftp_port || 21;
                document.getElementById('ftpUser').value = point.ftp_user || '';
                document.getElementById('ftpPassword').value = point.ftp_password || '';
                document.getElementById('ftpRemotePath').value = point.ftp_remote_path || '';
            }
        }
    } catch (error) {
        console.error('Ошибка загрузки точки доступа:', error);
    }
}

async function handleSubmit(e) {
    e.preventDefault();
    
    const sourceType = document.querySelector('input[name="sourceType"]:checked').value;
    
    const formData = {
        name: document.getElementById('name').value,
        description: document.getElementById('description').value || null,
        source_type: sourceType,
        is_active: document.getElementById('isActive').checked
    };

    if (sourceType === 'local') {
        formData.source_file_path = document.getElementById('sourceFilePath').value || null;
    } else {
        formData.ftp_host = document.getElementById('ftpHost').value || null;
        const port = parseInt(document.getElementById('ftpPort').value);
        formData.ftp_port = isNaN(port) ? 21 : port;
        formData.ftp_user = document.getElementById('ftpUser').value || null;
        formData.ftp_password = document.getElementById('ftpPassword').value || null;
        formData.ftp_remote_path = document.getElementById('ftpRemotePath').value || null;
    }
    
    try {
        if (currentAccessPointId) {
            await API.put(`/api/import-points/${currentAccessPointId}`, formData);
        } else {
            await API.post('/api/import-points/create', formData);
        }
        window.location.href = 'access-points.html';
    } catch (error) {
        Toast.error('Ошибка сохранения', error.message);
    }
}
