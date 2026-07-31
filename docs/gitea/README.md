# Внутренний GitHub (Gitea) для десктопа

Поддомен: `git.24pharmdata.ru` → сервер `WIN-HBHPS40FQTP` → `127.0.0.1:3000`.

## Быстрый старт на сервере

1. DNS: `git.24pharmdata.ru` A-запись на IP сервера.
2. Скопировать папку `docs/gitea/` на сервер в `D:\gitea\setup\` (или распаковать с шары).
3. Запустить от администратора:

```bat
D:\gitea\setup\install-gitea.ps1
```

4. Добавить в `D:\Caddy\Caddyfile` блок из `caddy-git.snippet` и перезапустить Caddy.
5. Открыть `https://git.24pharmdata.ru`, завершить initial setup (создать admin).
6. Создать организацию `pharmdata` и private repo `elfisa-pharmacy`.
7. Импорт:

```bat
D:\gitea\setup\import-elfisa-pharmacy.ps1 -GiteaUrl https://git.24pharmdata.ru -Owner pharmdata -Token <TOKEN>
```

Или с машины разработчика:

```bat
cd elfisa-pharmacy
git remote add gitea https://git.24pharmdata.ru/pharmdata/elfisa-pharmacy.git
git push gitea master --tags
```

## Регламент релизов для desktop-разработчика

1. Поднять версию в `AssemblyInfo.cs` (`1.0.X.0`).
2. `git tag v1.0.X && git push gitea master --tags`
3. `powershell -File dist\build-release.ps1`
4. В Gitea: **Releases → New Release** → tag `v1.0.X` → прикрепить ZIP.

Или одной командой после установки Gitea:

```bat
powershell -File docs\gitea\push-and-release.ps1 -Token <PAT> -ZipPath path\to.zip
```

Клиент при старте дергает:

`GET /api/v1/repos/pharmdata/elfisa-pharmacy/releases/latest`

Если tag новее локальной версии — блокирует работу и предлагает скачать.

## Проверка

```bat
powershell -File docs\gitea\verify-update-check.ps1
powershell -File docs\gitea\verify-forced-update.ps1
```

Второй скрипт поднимает mock Gitea и подтверждает, что локальная `1.0.3` блокируется релизом `v1.0.10`.

## Права

- Разработчик: Write на репозиторий.
- Клиентское приложение: чтение Releases. Для private repo — read-only token в настройках `UpdateApiToken` (или env `ELF_UPDATE_API_TOKEN`).
