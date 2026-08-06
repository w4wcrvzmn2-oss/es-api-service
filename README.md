# es-api-service

Внутренний backend и веб-кабинеты для проекта Elfisa / 24PharmData.

## Что в репозитории

- `cmd/service` — Go-сервис `es_api_service`
- `ClientWeb` — основной web UI + кабинет менеджера
- `ClienElf2` — кабинет покупателя / админские страницы
- `ClienSupplier` — кабинет поставщика
- `internal/httpserver` — HTTP API, auth, buyer/manager/supplier handlers
- `internal/dbfimport` — импорт накладных и связанный matching
- `internal/orderexport` — DBF-выгрузка заказов для поставщика
- `sql/pg` — PostgreSQL schema, Caddy и deploy-примеры

## Текущее состояние

- backend работает с PostgreSQL
- добавлен кабинет менеджера в `ClientWeb/manager`
- покупательские заказы создаются сразу в статусе `Placed`
- для заказов введён глобальный номер `EX-0000001`
- в выгрузке поставщика DBF появилась колонка `GLOBAL_SIGN`
- добавлены антибрутфорс и rate-limit настройки в конфиг

## Быстрый старт

1. Скопировать пример конфига:
   - `sql/pg/es_api_service.cfg.example` -> `es_api_service.cfg`
2. Заполнить параметры PostgreSQL и секреты.
3. Запустить сервис:

```powershell
go run ./cmd/service
```

Или собрать бинарник:

```powershell
go build -o es_api_service.exe ./cmd/service
```

## Deploy notes

- пример reverse proxy: `sql/pg/Caddyfile`
- пример server config: `sql/pg/es_api_service.cfg.example`
- миграция глобальных номеров заказов: `sql/pg/008_order_global_sign.sql`
- CDN / setup notes: `sql/pg/cdn-setup.md`

## Важно

В репозитории могут быть локальные рабочие артефакты рядом с кодом. Перед публикацией не включайте в commit большие дампы, временные txt-файлы и локальные бинарники.
