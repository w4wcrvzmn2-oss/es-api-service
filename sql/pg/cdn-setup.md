# CDN установщика: cdn.24pharmdata.ru

## Симптом
`ERR_SSL_PROTOCOL_ERROR` / «подключение не защищено» — для хоста
`cdn.24pharmdata.ru` в Caddy не было сайта, поэтому HTTPS (443) не отдавал
валидный TLS. По HTTP открывался основной сайт, не установщик.

## На сервере (один раз)

1. Создать папку и положить Setup:
```bat
mkdir D:\cdn\elfisa
copy /Y "\\путь\к\ElfisaPharmacy-Setup.exe" D:\cdn\elfisa\ElfisaPharmacy-Setup.exe
```

2. Обновить `D:\Caddy\Caddyfile` из `sql/pg/Caddyfile` (блок `cdn.24pharmdata.ru`).

3. Перезапустить Caddy:
```bat
net stop caddy
net start caddy
```
или как у вас запущена служба.

4. Дождаться выпуска сертификата Let's Encrypt (обычно 10–60 сек).
   Проверка:
```bat
curl -I https://cdn.24pharmdata.ru/
```
Ожидается `HTTP/1.1 200` и `Content-Disposition: attachment`.

## DNS
A-запись `cdn.24pharmdata.ru` → `195.14.114.171` уже верная.
