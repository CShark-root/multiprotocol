// Package compile turns the panel's stored intent (inbounds, users, later
// routing and DNS) into the final config of each core. It is pure: same input,
// byte-identical output, so the agent can skip restarts when nothing changed.
package compile

import (
	"encoding/json"
	"strconv"

	"panel/internal/panel/store"
)

// Email is the per-user tag cores report traffic under. It uses the stable
// id, not the editable name.
func Email(userID int64) string { return "u" + strconv.FormatInt(userID, 10) }

// UserIDFromEmail reverses Email.
func UserIDFromEmail(email string) (int64, bool) {
	if len(email) < 2 || email[0] != 'u' {
		return 0, false
	}
	id, err := strconv.ParseInt(email[1:], 10, 64)
	return id, err == nil && id > 0
}

// Private and special ranges that clients must never reach through the
// VPN (the provider's internal network, the server itself, metadata APIs).
var privateCIDRs = []string{
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
	"172.16.0.0/12", "192.168.0.0/16", "198.18.0.0/15", "224.0.0.0/4", "240.0.0.0/4",
	"::1/128", "fc00::/7", "fe80::/10", "ff00::/8",
}

// xrayProtocols are the inbound protocols served by Xray.
var xrayProtocols = map[string]bool{"vless": true, "trojan": true, "vmess": true, "shadowsocks": true}

// Xray builds the Xray config for the enabled inbounds and active users.
// It returns nil when there is nothing to serve, meaning "stop the core".
func Xray(inbounds []store.Inbound, users []store.User, apiAddr string) ([]byte, error) {
	var ins []any
	for _, ib := range inbounds {
		if ib.Enabled && xrayProtocols[ib.Protocol] {
			ins = append(ins, xrayInbound(ib, users))
		}
	}
	if len(ins) == 0 {
		return nil, nil
	}
	cfg := map[string]any{
		"log": map[string]any{"loglevel": "warning", "access": "none"},
		"api": map[string]any{
			"tag":      "api",
			"listen":   apiAddr,
			"services": []string{"StatsService"},
		},
		"stats": map[string]any{},
		"policy": map[string]any{
			"levels": map[string]any{
				"0": map[string]any{"statsUserUplink": true, "statsUserDownlink": true},
			},
		},
		"inbounds": ins,
		"outbounds": []any{
			map[string]any{"tag": "direct", "protocol": "freedom"},
			map[string]any{"tag": "block", "protocol": "blackhole"},
		},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules": []any{
				map[string]any{"ip": privateCIDRs, "outboundTag": "block"},
				map[string]any{"protocol": []string{"bittorrent"}, "outboundTag": "block"},
				map[string]any{"port": "25", "network": "tcp", "outboundTag": "block"},
			},
		},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func xrayInbound(ib store.Inbound, users []store.User) map[string]any {
	sniffing := map[string]any{"enabled": true, "destOverride": []string{"http", "tls", "quic"}, "routeOnly": true}
	if s := ib.Settings.Sniffing; s != nil {
		sniffing = map[string]any{
			"enabled": s.Enabled, "destOverride": s.DestOverride,
			"metadataOnly": s.MetadataOnly, "routeOnly": s.RouteOnly,
		}
	}
	in := map[string]any{
		"tag":      "in-" + strconv.FormatInt(ib.ID, 10),
		"listen":   "0.0.0.0",
		"port":     ib.Port,
		"sniffing": sniffing,
	}
	defer func() {
		// Socket options apply to every stream an inbound has.
		if so := ib.Settings.Sockopt; so != nil {
			stream, _ := in["streamSettings"].(map[string]any)
			if stream == nil {
				stream = map[string]any{}
				in["streamSettings"] = stream
			}
			stream["sockopt"] = sockopt(*so)
		}
	}()
	clients := []any{}
	switch ib.Protocol {
	case "vless":
		for _, u := range users {
			c := map[string]any{"id": u.UUID, "email": Email(u.ID)}
			if ib.Transport == "tcp" {
				c["flow"] = "xtls-rprx-vision"
			}
			clients = append(clients, c)
		}
		in["protocol"] = "vless"
		in["settings"] = map[string]any{"clients": clients, "decryption": "none"}
		in["streamSettings"] = realityStream(ib)
	case "trojan":
		for _, u := range users {
			clients = append(clients, map[string]any{"password": u.UUID, "email": Email(u.ID)})
		}
		in["protocol"] = "trojan"
		in["settings"] = map[string]any{"clients": clients}
		in["streamSettings"] = realityStream(ib)
	case "vmess":
		for _, u := range users {
			clients = append(clients, map[string]any{"id": u.UUID, "email": Email(u.ID)})
		}
		in["protocol"] = "vmess"
		in["settings"] = map[string]any{"clients": clients}
		stream := map[string]any{"network": "raw", "security": "none"}
		if ib.Transport == "ws" && ib.Settings.WS != nil {
			stream["network"] = "ws"
			stream["wsSettings"] = map[string]any{"path": ib.Settings.WS.Path}
		}
		in["streamSettings"] = stream
	case "shadowsocks":
		for _, u := range users {
			clients = append(clients, map[string]any{"password": SSUserKey(u.UUID), "email": Email(u.ID)})
		}
		ss := ib.Settings.Shadowsocks
		in["protocol"] = "shadowsocks"
		in["settings"] = map[string]any{
			"method": ss.Method, "password": ss.ServerPSK, "clients": clients, "network": "tcp,udp",
		}
	}
	return in
}

func sockopt(so store.SockoptSettings) map[string]any {
	m := map[string]any{}
	if so.TCPFastOpen {
		m["tcpFastOpen"] = true
	}
	if so.TCPCongestion != "" {
		m["tcpCongestion"] = so.TCPCongestion
	}
	if so.TCPMptcp {
		m["tcpMptcp"] = true
	}
	if so.AcceptProxyProtocol {
		m["acceptProxyProtocol"] = true
	}
	return m
}

// realityStream is the stream settings shared by VLESS and Trojan.
func realityStream(ib store.Inbound) map[string]any {
	r := ib.Settings.Reality
	rs := map[string]any{
		"show":        false,
		"target":      r.Target,
		"xver":        r.Xver,
		"serverNames": r.ServerNames,
		"privateKey":  r.PrivateKey,
		"shortIds":    r.ShortIDs,
	}
	if r.MaxTimeDiff > 0 {
		rs["maxTimeDiff"] = r.MaxTimeDiff
	}
	if r.MinClientVer != "" {
		rs["minClientVer"] = r.MinClientVer
	}
	if r.MaxClientVer != "" {
		rs["maxClientVer"] = r.MaxClientVer
	}
	if r.MLDSA65Seed != "" {
		rs["mldsa65Seed"] = r.MLDSA65Seed
	}
	stream := map[string]any{"security": "reality", "realitySettings": rs}
	switch ib.Transport {
	case "xhttp":
		stream["network"] = "xhttp"
		stream["xhttpSettings"] = XHTTPServer(ib.Settings.XHTTP)
	case "grpc":
		stream["network"] = "grpc"
		stream["grpcSettings"] = map[string]any{"serviceName": ib.Settings.GRPC.ServiceName}
	default:
		stream["network"] = "raw"
	}
	return stream
}
