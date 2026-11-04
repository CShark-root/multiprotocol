# Тестовая виртуалка (Windows 11 Pro)

Виртуалка повторяет VPS: Ubuntu 22.04, 1 vCPU, 2 ГБ RAM, 25 ГБ диска. Сеть идёт мостом в домашнюю сеть, поэтому телефон по Wi-Fi видит её как обычный сервер.

Основной способ — **Multipass** (Canonical) поверх Hyper-V: готовый образ Ubuntu, без установщика, всё командами. Ручной вариант через Hyper-V Manager описан в конце.

## 1. Включить Hyper-V (один раз)

PowerShell **от имени администратора**, затем перезагрузка:

```powershell
Enable-WindowsOptionalFeature -Online -FeatureName Microsoft-Hyper-V -All
```

Если команда пишет, что виртуализация выключена, включить Intel VT-x / AMD-V (SVM) в BIOS/UEFI.

## 2. Поставить Multipass (один раз)

```powershell
winget install --id Canonical.Multipass -e
```

## 3. Создать виртуалку

Из папки проекта, обычный PowerShell:

```powershell
.\scripts\vm\create-vm-multipass.ps1
```

Скрипт:
- создаёт внешний коммутатор `panel-ext` на адаптере `Ethernet` (один запрос UAC, интернет пропадёт на несколько секунд). Для Wi-Fi: `-Adapter "Wi-Fi"`, имена адаптеров показывает `multipass networks`;
- запускает Ubuntu 22.04 `panel-vm` (1 vCPU, 2 ГБ, 25 ГБ) с мостом в домашнюю сеть и ставит `curl`, `unzip`;
- делает снимок `clean`.

Откат к чистой системе:

```powershell
multipass stop panel-vm; multipass restore panel-vm.clean --destructive; multipass start panel-vm
```

Консоль виртуалки: `multipass shell panel-vm`. Её адреса: `multipass info panel-vm` (нужен `192.168.x.y`, а не `172.x` от служебной сети Multipass).

## 4. Поставить или обновить панель

```powershell
.\scripts\vm\deploy.ps1
```

Скрипт собирает панель и устанавливает на VM:
- пользователь `panel`, бинарь `/usr/local/bin/panel`, сервис `panel.service`, данные в `/var/lib/panel`;
- Xray из официального релиза с проверкой контрольной суммы (только в первый раз).

В конце печатается **секретный адрес панели** и отпечаток сертификата. Повторный запуск обновляет панель, данные сохраняются. `-SkipBuild` пропускает сборку.

Полезное:

```powershell
multipass exec panel-vm -- sudo journalctl -u panel -f
```

```powershell
multipass exec panel-vm -- sudo -u panel panel show-url -data-dir /var/lib/panel
```

## 5. Проверить

1. Открыть адрес панели (с IP `192.168.x.y`) в браузере. Сертификат самоподписанный: сверить отпечаток и продолжить.
2. Создать администратора, на обзоре ядро **xray** «ждёт первого подключения».
3. «Подключения» → «Новое подключение» → XHTTP + Reality, порт 443. Через несколько секунд баннер станет зелёным: «Xray … работает».
4. «Пользователи» → добавить → «Подключить устройство».
5. На телефоне в той же Wi-Fi в **v2rayNG** или **Hiddify** отсканировать QR **отдельного способа** (`vless://`). Ссылка-подписка с самоподписанным сертификатом может не загрузиться — приложения проверяют сертификат; на VM используем QR отдельных способов.
6. Открыть пару сайтов; через минуту-две у пользователя появится трафик.
7. Добавить TCP + Vision и gRPC на других портах и **Hysteria2 на UDP 443** (рядом с XHTTP на TCP 443) и сравнить. Hysteria2 в v2rayNG/Hiddify подключается по QR `hysteria2://`; сертификат самоподписанный и закреплён в ссылке.

Что прислать, если что-то не так: текст баннера на странице «Подключения» и вывод
`multipass exec panel-vm -- sudo journalctl -u panel -n 100 --no-pager`.

## Ручной вариант: Hyper-V Manager

Если Multipass не подходит: скачать `ubuntu-22.04.x-live-server-amd64.iso` с <https://releases.ubuntu.com/22.04/>, затем в PowerShell от администратора `.\scripts\vm\create-vm.ps1 -IsoPath <путь к iso>`. Пройти установщик (обязательно **Install OpenSSH server**), настроить вход по ключу и ставить панель через `.\scripts\vm\deploy.ps1 -Target user@IP`.
