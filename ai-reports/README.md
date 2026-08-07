# ИИ-отчёты (AI reports)

Локальный ИИ-сервис на **Mac mini (M4, 16 ГБ)**, который превращает
свободный запрос покупателя («закупки по поставщикам за июль»,
«топ товаров за 3 месяца», «заказы у Farmsnab») в готовый **Word/Excel**.

## Принцип
LLM **только распознаёт** запрос → JSON-интент (тип отчёта, период, фильтры).
Данные и цифры считает **детерминированный код** по платформенному
`GET /api/buyer/report`, который скоупит их по JWT покупателя — клиент видит
**только свои** заказы. Так отчёт всегда точный, а данные — только клиента.

```
десктоп (промт) → Caddy /ai/* → Mac:8808 (FastAPI)
   → Ollama qwen2.5:7b (интент) → платформа /api/buyer/report (данные клиента)
   → сборка Word/Excel → назад клиенту
```

## Компоненты на Маке (папка ~/ElfisaAI, ярлык на рабочем столе)
- `Ollama.app` (standalone) + модель `qwen2.5:7b-instruct` в `~/ElfisaAI/models`
- `server.py` — FastAPI-сервис (`/generate`, `/health`) на порту **8808**
- `venv/` — Python 3.9 + fastapi/uvicorn/python-docx/openpyxl/requests
- LaunchAgents (автозапуск, KeepAlive): `com.elfisa.ollama`, `com.elfisa.aireports`

> ⚠️ Файлы лежат в `~/ElfisaAI`, а не в `~/Desktop`, потому что macOS (TCC)
> блокирует запуск фоновых агентов launchd из защищённой папки Desktop.
> На рабочем столе — симлинк `ElfisaAI → ~/ElfisaAI` для удобства.

## Установка (кратко)
```bash
# 1. Ollama (без Homebrew)
curl -fL https://ollama.com/download/Ollama-darwin.zip -o Ollama.zip
unzip Ollama.zip -d ~/ElfisaAI
OLLAMA_MODELS=~/ElfisaAI/models ~/ElfisaAI/Ollama.app/Contents/Resources/ollama serve &
~/ElfisaAI/Ollama.app/Contents/Resources/ollama pull qwen2.5:7b-instruct

# 2. Python-сервис
cd ~/ElfisaAI && /usr/bin/python3 -m venv venv
./venv/bin/pip install -r requirements.txt

# 3. Автозапуск
cp com.elfisa.*.plist ~/Library/LaunchAgents/
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.elfisa.ollama.plist
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.elfisa.aireports.plist
```

## Публичный доступ (Caddy на Windows-сервере)
В блок `24pharmdata.ru` добавлено:
```caddy
handle_path /ai/* {
    reverse_proxy 192.168.95.116:8808   # Mac mini
}
```
Проверка: `https://24pharmdata.ru/ai/health`.

## Конфиг сервиса (env)
| Переменная | Значение по умолчанию |
|---|---|
| `OLLAMA_URL` | `http://127.0.0.1:11434` |
| `OLLAMA_MODEL` | `qwen2.5:7b-instruct` |
| `ELF_API_BASE` | `https://24pharmdata.ru` |

Десктоп ходит на `AppConfig.AiBaseUrl` (`https://24pharmdata.ru/ai`,
override — `ELF_AI_BASE_URL`).
