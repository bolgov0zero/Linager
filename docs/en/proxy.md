# Reverse proxy for the Linager admin panel

In "Own reverse proxy" mode the Linager admin panel listens for plain HTTP on the selected port. The proxy must pass WebSocket traffic and the `X-Forwarded-For`, `X-Forwarded-Proto`, and `Host` headers. The client (agent) port **cannot** be passed through the proxy: clients connect to it directly.

## Caddy

```caddy
linager.example.com {
	encode zstd gzip
	reverse_proxy 10.0.0.5:8080
}
```

For a closed network without a public domain: `tls internal` inside the site block.

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

## Real-time streams (SSE)
The dashboard receives data via `GET /api/live` (Server-Sent Events). The proxy must not buffer this stream: in nginx, add `proxy_buffering off;` for `location /` (or separately for `/api/live`); the server also sends `X-Accel-Buffering: no`. Caddy needs no changes. The read timeout must be longer than 15 seconds (the server sends a keep-alive every 15 s).

## Security
Because the admin panel port accepts `X-Forwarded-*` headers from any address by default, restrict access to it with a firewall (proxy only), or narrow `admin.trusted_proxies` and `admin.listen` in `/etc/linager/server.yaml`.
