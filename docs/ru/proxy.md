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

## Потоки в реальном времени (SSE)
Дашборд получает данные через `GET /api/live` (Server-Sent Events). Прокси не должен буферизовать этот поток: в nginx добавьте для `location /` (или отдельно для `/api/live`) `proxy_buffering off;` (сервер также шлёт `X-Accel-Buffering: no`), в Caddy ничего менять не нужно. Таймаут чтения должен быть больше 15 секунд (сервер шлёт keep-alive каждые 15 с).

## Безопасность
Так как порт админки по умолчанию принимает заголовки `X-Forwarded-*` от любого адреса, ограничьте доступ к нему файрволом (только прокси) или сузьте `admin.trusted_proxies` и `admin.listen` в `/etc/linager/server.yaml`.
