# MultiProtocol

Панель для своего VPN: Xray, Hysteria2 и sing-box на одном сервере или на нескольких, плюс управление клиентами Amnezia, установленной штатным приложением. Делалась для себя, семьи и друзей, поэтому здесь нет биллинга, реселлеров и продажи подписок.

Технические имена остались старыми: бинарь `panel`, служба `panel.service`, Go-модуль `panel`. Планы и принятые решения лежат в [PLAN.md](PLAN.md).

Работает на Ubuntu 22.04+ и Debian 12+, x86-64 и ARM64. Релизы выходят часто, обновление ставится кнопкой в панели.

## Установка

От root:

```bash
curl -fsSL https://raw.githubusercontent.com/CShark-root/multiprotocol/main/install.sh | sudo bash
```

Установщик скачивает последний релиз, сверяет контрольную сумму, ставит панель и три ядра, включает BBR и печатает адрес панели вида `https://IP:ПОРТ/<секрет>/` вместе с отпечатком сертификата. Администратора создаёте при первом входе. Docker, контейнеры Amnezia и их правила iptables установщик не трогает.

Переменные для установщика, если нужны:

| Переменная | Что делает |
|---|---|
| `MP_VERSION=v0.1.37` | поставить конкретную версию вместо последней (так же делается откат) |
| `UPDATE_CORES=1` | заодно переустановить ядра на последние версии |
| `PUBLIC_HOST=example.org` | адрес для ссылок клиентов, если автоопределение ошиблось |
| `NODE_JOIN=код` | поставить не панель, а агента для вкладки «Серверы» (код даёт сама панель) |

Пример: `curl -fsSL …/install.sh | sudo MP_VERSION=v0.1.32 bash`.

Перед установкой можно посмотреть сервер. Скрипт только читает и ничего не меняет:

```bash
curl -fsSL https://raw.githubusercontent.com/CShark-root/multiprotocol/main/scripts/vps-check.sh | sudo bash
```

А этот показывает, что сервер отдаёт наружу: открытые порты, ответ панели и Reality постороннему, права на файлы. Его полезно запускать время от времени:

```bash
curl -fsSL https://raw.githubusercontent.com/CShark-root/multiprotocol/main/scripts/exposure-check.sh | sudo bash
```

Переезд с 3x-ui, на новый сервер и перенос Amnezia описаны в [docs/migration.md](docs/migration.md).

## Что умеет

**Подключения**
- Xray: VLESS и Trojan с Reality (XHTTP, TCP/Vision, gRPC), VMess (WS, TCP), Shadowsocks-2022.
- Hysteria2 со своим ядром: обфускация Salamander, port hopping через отдельную таблицу nftables, сертификат домена или самоподписанный с закреплением.
- sing-box: TUIC, AnyTLS, ShadowTLS и VLESS + Reality для Hiddify. Новый Xray (26.9.8+) пускает в Reality только клиентов с ключом ML-KEM, а sing-box его пока не умеет. VLESS на ядре sing-box можно поставить на тот же 443, что и основной VLESS: встроенный распределитель разводит их по имени сайта.
- Экспертный режим: все параметры XHTTP и Reality, sniffing, sockopt. Подбор сайта для маскировки и «Тестовый набор» всех протоколов одной кнопкой.

**Пользователи и подписки**
- Лимит трафика, срок, сброс по дням, неделям или месяцам, продление в один клик, график трафика, кто онлайн.
- Одна ссылка подписки на все приложения. Формат выбирается по приложению: обычные ссылки для v2RayTun и v2rayNG, Mihomo для FlClash и Clash Verge, sing-box для Hiddify. Для каждого пользователя можно выбрать, какие подключения входят в подписку.
- В приложениях видно имя профиля, остаток трафика и срок, а ещё ваше объявление и ссылка на помощь (если приложение это показывает).
- Журнал обновлений подписки: когда и каким приложением человек последний раз её обновлял.
- Правила на устройстве для Mihomo и sing-box: всё через VPN, российское напрямую или через VPN только заблокированное.

**Маршрутизация на сервере**
- Гео-базы (в том числе runetfreedom) с автообновлением. Правила «напрямую», «блок», «WARP», «свой выход».
- WARP регистрируется одной кнопкой. Сторож проверяет его каждые 5 минут, при сбое меняет порт и сообщает, когда WARP снова заработал.
- Свои выходы по ссылке (vless, trojan, ss, vmess, socks, http) и выход через другой ваш сервер (каскад).
- DNS по сайтам, стратегия «только IPv4» для серверов со сломанным IPv6, история изменений с откатом.

**Серверы**
- Главная панель и агенты на других VPS. Агент ставится одной командой и сам ходит к панели за настройками, так что на узле не нужно открывать служебных портов. Если панель недоступна, VPN на узле продолжает работать.
- Пользователи и подписки общие: подписка сразу ведёт на все серверы.
- Нагрузка, ядра и онлайн по каждому серверу, дата оплаты VPS с напоминанием в боте.

**Amnezia**
- Контейнеры, которые поставило приложение Amnezia: состояние, нагрузка, логи, клиенты и их трафик. Версия AmneziaWG определяется как в приложении (1.5, 2, 3.1).
- Клиентов AmneziaWG можно добавлять, переименовывать и удалять из панели. Панель правит те же файлы, что и приложение, поэтому оба видят одних клиентов. Для каждого клиента есть ключ `vpn://` для AmneziaVPN и `.conf` для AmneziaWG.
- Клиента можно привязать к пользователю панели: тогда его срок и лимит трафика действуют и на AmneziaWG, а трафик идёт в его статистику.
- Перед каждой правкой файлы сохраняются в `/var/lib/panel-amnezia/backups`, незнакомый формат панель не трогает, при ошибке всё откатывается. Контейнеры она не перезапускает.

**Обслуживание**
- Обновление панели кнопкой: видны шаги, таймер и журнал.
- Ядра обновляются сами по ночам (время выбирается), с проверкой конфига и откатом, если новое ядро не запустилось. Можно поставить любую из последних версий ядра и закрепить её.
- Зашифрованная резервная копия в Telegram раз в сутки.
- Telegram-бот: уведомления и меню управления (сервер, пользователи, перезапуск ядер, копия, перезагрузка).
- Журнал: живые логи панели и ядер, события, история действий администратора.

**Безопасность**
- Панель спрятана: случайный порт, секретный путь, только HTTPS. Посторонние получают пустой ответ или обрыв.
- Пароли на argon2id, ограничение попыток входа, 2FA (TOTP), сессия в cookie `HttpOnly; Secure; SameSite=Strict`.
- Панель работает от отдельного пользователя без root. Права root есть только у трёх маленьких помощников с узким набором действий: Docker для Amnezia, nftables для port hopping и установка обновлений.
- Параметризованный SQL, строгий разбор JSON, проверка Origin, строгий CSP.

Интерфейс на русском и английском, светлая и тёмная тема, работает с телефона. Быстрый поиск по Ctrl+K.

## Команды на сервере

Почти все команды запускаются от пользователя панели: `sudo -u panel panel …`. Каталог данных на сервере всегда `/var/lib/panel`.

| Команда | Что делает |
|---|---|
| `panel version` | версия панели |
| `panel show-url -data-dir /var/lib/panel` | адрес панели (порт и секретный путь) |
| `panel set-host -data-dir /var/lib/panel АДРЕС` | адрес сервера в ссылках клиентов (IP или домен) |
| `panel admin -data-dir /var/lib/panel -username ЛОГИН` | задать новый пароль, он читается со stdin |
| `panel admin … -disable-2fa` | то же и выключить 2FA, если потеряли телефон |
| `panel admin -data-dir /var/lib/panel -check` | есть ли администратор (код выхода 0 или 3) |
| `panel backup -data-dir /var/lib/panel -out ФАЙЛ` | резервная копия базы и настроек |
| `panel restore -data-dir /var/lib/panel ФАЙЛ` | восстановление, в том числе из `.mpbk` от бота (спросит пароль). Панель перед этим остановить |
| `panel agent-join -data-dir /var/lib/panel КОД` | подключить этот сервер узлом к главной панели |

Забыли пароль:

```bash
echo 'новый-пароль' | sudo -u panel panel admin -data-dir /var/lib/panel -username ЛОГИН
```

Службы и журналы:

| Служба | Зачем |
|---|---|
| `panel` | панель и ядра (Xray, Hysteria2, sing-box запускаются как её дочерние процессы) |
| `panel-amnezia` | помощник для вкладки Amnezia (работает с Docker) |
| `panel-net` | помощник для port hopping (nftables) |
| `panel-update.path` / `panel-update.service` | обновление панели и ядер по кнопке и по ночам |
| `panel-agent` | только на узлах: агент вместо панели |

```bash
sudo systemctl status panel
```

```bash
sudo journalctl -u panel -f
```

```bash
sudo systemctl restart panel
```

Ядра можно поставить вручную, это делают те же скрипты, что использует панель. Без аргумента ставится последняя версия:

```bash
sudo bash /usr/local/lib/panel/install-xray.sh v26.10.31
```

```bash
sudo bash /usr/local/lib/panel/install-hysteria.sh app/v2.13.0
```

```bash
sudo bash /usr/local/lib/panel/install-singbox.sh v1.14.4
```

После ручной установки перезапустите панель.

Где что лежит:

| Путь | Что там |
|---|---|
| `/var/lib/panel` | база `panel.db`, настройки `panel.json`, ключи, конфиги ядер, гео-базы, журнал обновлений |
| `/usr/local/bin/panel` | сам бинарь |
| `/usr/local/lib/panel` | ядра и скрипты обновления |
| `/var/lib/panel-amnezia/backups` | копии файлов Amnezia перед каждой правкой из панели |

## Удаление

Сначала сделайте резервную копию, если панель ещё может пригодиться:

```bash
sudo -u panel panel backup -data-dir /var/lib/panel -out /root/mp-backup.tar.gz
```

Остановить и убрать службы:

```bash
sudo systemctl disable --now panel panel-amnezia panel-net panel-update.path panel-agent 2>/dev/null; sudo nft delete table inet panel_hop 2>/dev/null; true
```

```bash
sudo rm -f /etc/systemd/system/panel.service /etc/systemd/system/panel-amnezia.service /etc/systemd/system/panel-net.service /etc/systemd/system/panel-update.service /etc/systemd/system/panel-update.path /etc/systemd/system/panel-agent.service && sudo systemctl daemon-reload
```

Удалить программу, ядра и системные настройки панели (BBR и право на перезагрузку):

```bash
sudo rm -rf /usr/local/bin/panel /usr/local/lib/panel /etc/sysctl.d/99-zz-panel.conf /etc/sysctl.d/90-panel.conf /etc/modules-load.d/panel-bbr.conf /etc/polkit-1/localauthority/50-local.d/50-panel-reboot.pkla /etc/polkit-1/rules.d/50-panel-reboot.rules
```

Удалить данные: пользователей, ключи, копии файлов Amnezia. Это необратимо:

```bash
sudo rm -rf /var/lib/panel /var/lib/panel-amnezia && sudo userdel panel
```

Docker и контейнеры Amnezia остаются на месте и работают дальше. Их удаляют через приложение Amnezia.

## Сборка

Нужны Go 1.27+ и Node.js 22+.

```bash
cd web && npm ci && npm run build && cd ..
go build -o bin/panel ./cmd/panel
```

На Windows `scripts\build.ps1` собирает версию для разработки и Linux-бинарь для сервера. Релизы собирает GitHub Actions по тегу `vX.Y.Z`.

## Разработка

```bash
go run ./cmd/panel -dev -data-dir data-dev   # http://127.0.0.1:8080/dev/
cd web && npm run dev                        # http://localhost:5173/dev/ с hot reload
```

`-dev` слушает только `127.0.0.1` и работает без TLS. Ещё флаги для разработки: `-no-local-agent` запускает панель без ядер, `-xray-bin`, `-hysteria-bin` и `-singbox-bin` задают пути к ядрам. Тесты: `go test ./...`.

Проверка с настоящими ядрами, Amnezia и телефоном идёт на виртуалке: [docs/vm-setup.md](docs/vm-setup.md).

## Лицензия

MIT
