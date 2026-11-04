# MultiProtocol

Open-source VPN-панель для личного использования: Xray, Hysteria2 и sing-box в одном понятном интерфейсе, рядом со штатной Amnezia (только просмотр). Подробный план — в [PLAN.md](PLAN.md).

Технические имена (бинарь `panel`, сервис `panel.service`, Go-модуль `panel`) пока прежние.

> Статус: в разработке, проверяется на тестовой виртуалке. Что осталось до переезда на VPS — в PLAN.md, раздел «Готовность к переезду».

## Что уже есть

- **11 протоколов на трёх ядрах:** Xray (VLESS и Trojan с Reality на XHTTP/TCP/gRPC, Shadowsocks-2022, VMess), Hysteria2, sing-box (TUIC, AnyTLS, ShadowTLS); «Тестовый набор» одной кнопкой; режим «Эксперт» со всеми параметрами XHTTP, Reality (включая ML-DSA-65), sniffing и sockopt; подбор сайта для маскировки.
- **Подписки** для приложений на Xray (Happ, v2RayTun, v2rayNG…) и на sing-box (Hiddify, sing-box…), QR-коды, учёт трафика и лимиты.
- **Журнал:** живые логи панели и ядер, события, история действий.
- **Скрытая панель:** случайный порт, секретный путь (`https://IP:PORT/<секрет>/`), только HTTPS. Любой другой адрес отвечает пустой 404 (или рвёт соединение), обычный HTTP не получает ответа вообще.
- **Защита входа:** argon2id, ограничение попыток, сессия в `HttpOnly; Secure; SameSite=Strict` cookie, ограниченной секретным путём.
- **Защита от инъекций:** параметризованный SQL, строгий JSON, проверка Origin, строгий CSP, `Referrer-Policy: no-referrer`.
- **Интерфейс:** русский и английский, светлая и тёмная тема, скандинавский стиль.

## Сборка

Нужны Go 1.27+ и Node.js 22+.

```bash
cd web && npm ci && npm run build && cd ..
go build -o bin/panel ./cmd/panel
```

На Windows: `scripts\build.ps1` (собирает и Windows-версию для разработки, и Linux-бинарь для сервера).

## Запуск

```bash
./bin/panel -data-dir /var/lib/panel
```

При первом запуске панель создаёт `panel.json` со случайным портом и секретным путём, самоподписанный сертификат и печатает адрес. Потом адрес можно посмотреть так:

```bash
./bin/panel show-url -data-dir /var/lib/panel
```

## Разработка

```bash
go run ./cmd/panel -dev -data-dir data-dev   # http://127.0.0.1:8080/dev/
cd web && npm run dev                        # http://localhost:5173/dev/ с hot reload
```

`-dev` работает только на `127.0.0.1` и без TLS. Тесты: `go test ./...`.

Проверка с настоящим Xray и телефоном — на виртуалке Hyper-V: [docs/vm-setup.md](docs/vm-setup.md).

## Лицензия

MIT
