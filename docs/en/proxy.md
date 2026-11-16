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

## Nginx Proxy Manager

In the Proxy Host card:

- **Details:** Scheme `http`, Forward Hostname / IP: the address of the Linager server (for example `10.0.0.5`), Forward Port: the admin panel port (for example `8080`). Turn on **Websockets Support**: the terminal and the live dashboard need it.
- **SSL:** pick a certificate and turn on Force SSL.
- **Advanced**, Custom Nginx Configuration field:

```nginx
proxy_buffering off;
proxy_read_timeout 3600s;
```

Nginx Proxy Manager sets `Host`, `X-Forwarded-For` and `X-Forwarded-Proto` by itself. If NPM sits behind Cloudflare or another proxy, turn on passing the real visitor address in NPM (otherwise you will see the address of Cloudflare). If NPM runs in Docker on the same host, Linager may see it from the gateway address of the Docker network (for example `172.17.0.1`) and not from the container address: add exactly the address the interface shows to "Trusted proxies".

## Live dashboard updates (SSE)
The dashboard receives data through one long request, `GET /api/live`: the server keeps appending new data to it. By default nginx collects the response in a buffer and sends it in batches, so updates arrive late or not at all. For this request buffering must be off: in nginx add `proxy_buffering off;` to `location /` (or separately to `location /api/live`). The server also sends the `X-Accel-Buffering: no` header. Caddy needs no changes.

The second requirement: the proxy must not drop a quiet connection too early. The server sends a "still alive" signal every 15 seconds, so the read timeout (`proxy_read_timeout`) must be longer than 15 seconds (nginx defaults to 60, which is enough).

## Security
Because the admin panel port accepts `X-Forwarded-*` headers from any address by default, open Settings → Admin panel access and add the address of the proxy to "Trusted proxies" (the button "Add … as a trusted proxy" fills it in when you came through the proxy). After that you can turn on a white or black list by the real address of the visitor and read the access journal. The local network can still connect directly: such a connection is judged by its own address, and `X-Forwarded-For` from an address that is not trusted is ignored. If direct access is not needed, turn on "Only through trusted proxies". You can also restrict the admin panel port with a firewall (proxy only) and narrow `admin.listen`.

Note: if the proxy is not trusted and the white list holds the whole local network, the server sees every visitor from the address of the proxy (a local one) and lets the whole internet in. The interface warns about it and refuses to save such settings.
