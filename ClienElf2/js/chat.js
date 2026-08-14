// Чат с ExestAI — раздел кабинета phd. Ходит через same-origin /api/ai/chat
// (Go-бэкенд проксирует в ИИ-сервис на Маке — так надёжнее, чем кросс-домен).
let history = [];         // [{role:'user'|'assistant', content}]
let busy = false;

const log = () => document.getElementById('chatLog');
const input = () => document.getElementById('chatInput');

function escapeHtml(t) {
    const d = document.createElement('div');
    d.textContent = t == null ? '' : String(t);
    return d.innerHTML;
}

function addBubble(role, text, cls = '') {
    const wrap = document.createElement('div');
    wrap.className = 'msg ' + (role === 'user' ? 'user' : 'ai') + (cls ? ' ' + cls : '');
    wrap.innerHTML = `<div class="bubble">${escapeHtml(text)}</div>`;
    log().appendChild(wrap);
    log().scrollTop = log().scrollHeight;
    return wrap;
}

async function send() {
    const text = input().value.trim();
    if (!text || busy) return;
    busy = true;
    input().value = '';
    autoGrow();
    addBubble('user', text);
    history.push({ role: 'user', content: text });

    const typing = addBubble('ai', 'ExestAI печатает…', 'typing');
    try {
        const data = await API.post('/api/ai/chat', {
            message: text,
            history: history.slice(0, -1).slice(-10)
        });
        typing.remove();
        const reply = (data && data.reply) ? data.reply : 'Пустой ответ.';
        addBubble('ai', reply);
        history.push({ role: 'assistant', content: reply });
    } catch (e) {
        typing.remove();
        addBubble('ai', 'Не удалось получить ответ (ИИ-сервис недоступен). Попробуйте позже.', 'err');
        console.error('chat error:', e);
    } finally {
        busy = false;
        input().focus();
    }
}

function autoGrow() {
    const el = input();
    el.style.height = 'auto';
    el.style.height = Math.min(el.scrollHeight, 140) + 'px';
}

document.addEventListener('DOMContentLoaded', () => {
    addBubble('ai', 'Привет! Я ExestAI — ассистент ЭльФиСА. Помогу разобраться с прайсами, заказами, поставщиками и выгрузками. Чем помочь?');
    document.getElementById('btnSend').addEventListener('click', send);
    document.getElementById('btnClear').addEventListener('click', () => {
        history = [];
        log().innerHTML = '';
        addBubble('ai', 'Начнём заново. О чём поговорим?');
    });
    input().addEventListener('input', autoGrow);
    input().addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); }
    });
    input().focus();
});
