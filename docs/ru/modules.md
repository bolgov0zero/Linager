# Модули агента и конфигурация

Конфигурация агента: `/etc/linager/agent.yaml` (создаётся установщиком, права 600). Каждый модуль можно отключить; после изменения: `sudo systemctl restart linager-agent`. Админка показывает вкладки только включённых модулей.

```yaml
modules:
  metrics: true      # метрики (всегда нужны дашборду)
  terminal: true     # веб-терминал (оболочка root)
  services: true     # службы systemd
  apt: true          # обновления apt
  processes: true    # процессы
  logs: true         # журнал journald
  files: true        # файловый менеджер
  network: true      # сеть (ifupdown; netplan только просмотр)
  firewall: true     # nftables (таблица inet filter)
  docker: true       # Docker (включается, только если есть сокет)
```

Перезагрузка (`reboot`) доступна всегда и требует подтверждения.

## Файлы и каталоги агента
| Путь | Назначение |
|---|---|
| `/usr/local/bin/linager` | бинарник (сервер и агент) |
| `/etc/linager/agent.yaml` | включённые модули |
| `/var/lib/linager-agent/` | ключ, сертификат, состояние, журнал отката (`pending.json`), `linager.prev` |
| `/etc/systemd/system/linager-agent.service` | служба агента |

## Команды клиента
```bash
linager agent status          # версия, сервер, состояние
sudo linager agent update     # обновить с сервера
sudo linager agent uninstall --purge   # удалить с хоста (ключи и конфиг)
```

## Удаление клиента из админки
Карточка сервера, «Удалить сервер»: введите имя сервера, отметьте «Также удалить агента с самого сервера» (если он онлайн, агент удалит свои ключи и службу и остановится). Запись и история удаляются, сертификат агента перестаёт работать сразу.
