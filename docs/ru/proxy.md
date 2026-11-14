# Обратный прокси для админки Linager

В режиме «Свой обратный прокси» админка Linager слушает обычный HTTP на выбранном порту. Прокси должен передавать WebSocket и заголовки `X-Forwarded-For`, `X-Forwarded-Proto`, `Host`. Порт клиентов (агентов) через прокси **не проходит**: клиенты подключаются к нему напрямую.

## Caddy

```caddy
linager.example.com {
	encode zstd gzip
	reverse_proxy 10.0.0.5:8080
}
```

Для закрытой сети без публичного домена: `tls internal` внутри блока сайта.

## nginx

```nginx
server {
    listen 443 ssl;
    server_name linager.example.com;
    ssl_certificate     /etc/ssl/linager.crt;
    ssl_certificate_key /etc/ssl/linager.key;

    location / {
        proxy_pass http://10.0.0.5:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 3600s;
    }
}
```

## Nginx Proxy Manager

В карточке Proxy Host:

- **Details:** Scheme `http`, Forward Hostname / IP: адрес сервера Linager (например `10.0.0.5`), Forward Port: порт админки (например `8080`). Включите **Websockets Support**: он нужен терминалу и живому обновлению дашборда.
- **SSL:** выберите сертификат, включите Force SSL.
- **Advanced**, поле Custom Nginx Configuration:

```nginx
proxy_buffering off;
proxy_read_timeout 3600s;
```

`Host`, `X-Forwarded-For` и `X-Forwarded-Proto` Nginx Proxy Manager передаёт сам.

## Живое обновление дашборда (SSE)
Дашборд получает данные одним долгим запросом `GET /api/live`: сервер время от времени дописывает в него новые данные. Обычно nginx копит ответ в буфере и отдаёт его пачками, из-за чего обновления приходят с задержкой или не приходят совсем. Поэтому для этого запроса буферизацию нужно выключить: в nginx добавьте `proxy_buffering off;` в `location /` (или отдельно в `location /api/live`). Сервер дополнительно шлёт заголовок `X-Accel-Buffering: no`. В Caddy ничего менять не нужно.

Второе требование: прокси не должен обрывать молчащее соединение раньше времени. Сервер отправляет сигнал «я жив» каждые 15 секунд, поэтому таймаут чтения (`proxy_read_timeout`) должен быть больше 15 секунд (по умолчанию в nginx 60, этого достаточно).

## Безопасность
Так как порт админки по умолчанию принимает заголовки `X-Forwarded-*` от любого адреса, ограничьте доступ к нему файрволом (только прокси) или сузьте `admin.trusted_proxies` и `admin.listen` в `/etc/linager/server.yaml`.
