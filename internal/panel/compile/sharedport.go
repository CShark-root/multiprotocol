package compile

import (
	"encoding/json"
	"net"
	"strconv"

	"panel/internal/agent/sniproxy"
	"panel/internal/panel/store"
)

// SharePorts — один порт (обычно 443) на Reality Xray и Reality sing-box.
// Если подключение vless-sb стоит на том же порту, что VLESS/Trojan Reality
// Xray, оба уходят на 127.0.0.1, а порт слушает распределитель по SNI: имя
// сайта vless-sb — в sing-box, всё остальное (свои, чужие, сканеры) — в Xray
// с настоящим адресом клиента (PROXY protocol). Возвращает подключения для
// сборки ядер и конфиг распределителя (nil — делить нечего).
func SharePorts(list []store.Inbound) ([]store.Inbound, []byte) {
	out := make([]store.Inbound, len(list))
	copy(out, list)
	xrayAt := map[int]int{} // порт → индекс Reality-подключения Xray
	for i, ib := range out {
		if ib.Enabled && (ib.Protocol == "vless" || ib.Protocol == "trojan") && ib.Settings.Reality != nil {
			if _, ok := xrayAt[ib.Port]; !ok {
				xrayAt[ib.Port] = i
			}
		}
	}
	var cfg sniproxy.Config
	byPort := map[int]int{} // порт → индекс слушателя в cfg.Listeners
	for i, ib := range out {
		if !ib.Enabled || ib.Protocol != "vless-sb" || ib.Settings.Reality == nil || len(ib.Settings.Reality.ServerNames) == 0 {
			continue
		}
		xi, ok := xrayAt[ib.Port]
		if !ok {
			continue // своего порта у Xray нет — sing-box слушает сам
		}
		li, ok := byPort[ib.Port]
		if !ok {
			x := &out[xi]
			xport := internalPort(x.ID)
			so := store.SockoptSettings{}
			if x.Settings.Sockopt != nil {
				so = *x.Settings.Sockopt
			}
			so.AcceptProxyProtocol = true
			x.Settings.Sockopt = &so
			x.LocalOnly = true
			cfg.Listeners = append(cfg.Listeners, sniproxy.Listener{
				Listen:  net.JoinHostPort("", strconv.Itoa(ib.Port)),
				Default: sniproxy.Route{To: loopback(xport), ProxyProtocol: true},
			})
			x.Port = xport
			li = len(cfg.Listeners) - 1
			byPort[ib.Port] = li
		}
		sport := internalPort(ib.ID)
		cfg.Listeners[li].Routes = append(cfg.Listeners[li].Routes, sniproxy.Route{SNI: ib.Settings.Reality.ServerNames[0], To: loopback(sport)})
		out[i].Port, out[i].LocalOnly = sport, true
	}
	if len(cfg.Listeners) == 0 {
		return out, nil
	}
	b, _ := json.Marshal(cfg)
	return out, b
}

// internalPort — внутренний порт ядра за распределителем (только 127.0.0.1).
func internalPort(id int64) int { return 61000 + int(id%4000) }

func loopback(port int) string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) }
