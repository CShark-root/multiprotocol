#!/usr/bin/env bash
# Обновляет ядра (Xray, Hysteria2, sing-box) до последних стабильных версий.
# Запускается от root из panel-update.sh, когда панель просит "cores-only=1"
# (ночная автопроверка нашла новую версию).
#
# Для каждого ядра: копия старого бинаря (.prev), установка новой версии
# с проверкой контрольной суммы, проверка текущего конфига новым бинарём.
# Затем перезапуск панели (ядра — её дочерние процессы) и проверка, что
# обновлённые ядра поднялись. Если нет — возвращается старый бинарь.
# Итог пишется в /var/lib/panel/cores-update.json, панель сообщит о нём.
#
# Аргументы «ядро@версия» (xray@26.7.28, sing-box@latest) — поставить только
# эти ядра и эти версии (выбор во вкладке «Обслуживание»). Без аргументов —
# все ядра до последних версий.
set -uo pipefail
LIB=/usr/local/lib/panel
DATA=/var/lib/panel
RESULT=$DATA/cores-update.json

version() { # $1 = ядро
  local bin=$LIB/$1/$1
  case $1 in
    xray) "$bin" version 2>/dev/null | head -n1 | awk '{print $2}' ;;
    hysteria) "$bin" version 2>/dev/null | awk 'tolower($1)=="version:"{print $2; exit}' ;;
    sing-box) "$bin" version 2>/dev/null | head -n1 | awk '{print $3}' ;;
  esac
}

config_ok() { # новый бинарь принимает текущий конфиг?
  local bin=$LIB/$1/$1
  case $1 in
    # Проверка от пользователя panel, как ядро потом и работает: под root Xray
    # пробует поднять настоящий интерфейс WireGuard (WARP) и падает на этом.
    xray) [[ ! -f $DATA/xray/config.json ]] || runuser -u panel -- env XRAY_LOCATION_ASSET=$DATA/geo "$bin" run -test -c "$DATA/xray/config.json" >/tmp/core-check.log 2>&1 ;;
    sing-box) [[ ! -f $DATA/sing-box/config.json ]] || runuser -u panel -- "$bin" check -c "$DATA/sing-box/config.json" >/tmp/core-check.log 2>&1 ;;
    hysteria) [[ -n $(version hysteria) ]] ;; # у hysteria нет проверки конфига
  esac
}

running() { pgrep -f "^$LIB/$1/$1 " >/dev/null; }

# latest — версия последнего стабильного релиза без префикса (пусто, если GitHub молчит).
latest() {
  local repo prefix tag
  case $1 in
    xray) repo=XTLS/Xray-core prefix=v ;;
    hysteria) repo=apernet/hysteria prefix=app/v ;;
    sing-box) repo=SagerNet/sing-box prefix=v ;;
  esac
  tag=$(curl -fsSL -m 20 "https://api.github.com/repos/$repo/releases/latest" 2>/dev/null | python3 -c 'import json,sys; print(json.load(sys.stdin)["tag_name"])' 2>/dev/null)
  [[ $tag == "$prefix"* ]] && echo "${tag#"$prefix"}"
}

restore() { # $1 = ядро
  mv -f "$LIB/$1/$1.prev" "$LIB/$1/$1"
}

# Что ставить: ядро → версия (latest или N.N.N). Чужие строки отбрасываются.
declare -A target
if (($#)); then
  for a in "$@"; do
    if [[ $a =~ ^(xray|hysteria|sing-box)@(latest|[0-9]{1,4}\.[0-9]{1,4}\.[0-9]{1,4})$ ]]; then
      target[${BASH_REMATCH[1]}]=${BASH_REMATCH[2]}
    else
      echo "пропускаю непонятный запрос: $a"
    fi
  done
else
  target=([xray]=latest [hysteria]=latest [sing-box]=latest)
fi

# tagOf — тег релиза для версии: у Hysteria2 теги вида app/v2.13.0.
tagOf() { [[ $1 == hysteria ]] && echo "app/v$2" || echo "v$2"; }

updated=() failed=() rolled=()
declare -A was_running from to
for core in xray hysteria sing-box; do
  [[ -n ${target[$core]:-} ]] || continue
  bin=$LIB/$core/$core
  script=$LIB/install-${core//-/}.sh
  [[ -x $bin && -f $script ]] || continue
  running "$core" && was_running[$core]=1
  old=$(version "$core")
  want=${target[$core]}
  [[ $want == latest ]] && want=$(latest "$core")
  if [[ -n $want && ${old#v} == "$want" ]]; then
    echo "$core $old — эта версия уже стоит"
    continue
  fi
  echo "==> $core (сейчас $old, ставлю ${want:-?})"
  cp -a "$bin" "$bin.prev"
  if [[ -z $want ]]; then
    echo "не узнать последнюю версию $core — GitHub не ответил"
    rm -f "$bin.prev"
    failed+=("$core")
    continue
  fi
  if ! bash "$script" "$(tagOf "$core" "$want")"; then
    echo "установка $core не удалась, оставляю $old"
    restore "$core"
    failed+=("$core")
    continue
  fi
  new=$(version "$core")
  if [[ -z $new ]]; then
    echo "новый $core не отвечает на version, возвращаю $old"
    restore "$core"
    failed+=("$core")
    continue
  fi
  if [[ $new == "$old" ]]; then
    rm -f "$bin.prev"
    continue
  fi
  if ! config_ok "$core"; then
    echo "$core $new не принимает текущий конфиг, возвращаю $old:"
    grep -viE "^(xray [0-9]|a unified)" /tmp/core-check.log | tail -n 5
    restore "$core"
    failed+=("$core")
    continue
  fi
  from[$core]=$old to[$core]=$new
  updated+=("$core")
done

if ((${#updated[@]})); then
  echo "==> restart"
  systemctl restart panel
  sleep 20
  again=0
  for core in "${updated[@]}"; do
    [[ -n ${was_running[$core]:-} ]] || continue
    if ! running "$core"; then
      echo "$core ${to[$core]} не запустился, возвращаю ${from[$core]}"
      restore "$core"
      rolled+=("$core")
      again=1
    fi
  done
  ((again)) && systemctl restart panel
fi
for core in "${updated[@]}"; do rm -f "$LIB/$core/$core.prev"; done

# Итог для панели: {"updated":[{"core","from","to"}],"rolledBack":[...],"failed":[...]}
{
  printf '{"at":"%s","updated":[' "$(date -Is)"
  sep=
  for core in "${updated[@]}"; do
    [[ " ${rolled[*]} " == *" $core "* ]] && continue
    printf '%s{"core":"%s","from":"%s","to":"%s"}' "$sep" "$core" "${from[$core]}" "${to[$core]}"
    sep=,
  done
  printf '],"rolledBack":['
  sep=
  for core in "${rolled[@]}"; do printf '%s{"core":"%s","from":"%s","to":"%s"}' "$sep" "$core" "${from[$core]}" "${to[$core]}"; sep=,; done
  printf '],"failed":['
  sep=
  for core in "${failed[@]}"; do printf '%s"%s"' "$sep" "$core"; sep=,; done
  printf ']}\n'
} >"$RESULT.new"
chown panel:panel "$RESULT.new"
chmod 0600 "$RESULT.new"
mv -f "$RESULT.new" "$RESULT"
echo "==> done"
