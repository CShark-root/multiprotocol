package compile

import (
	"encoding/json"
	"strconv"

	"panel/internal/agent/hysteria"
	"panel/internal/panel/store"
)

// Hysteria builds the state for the Hysteria adapter: one server per enabled
// hysteria2 inbound and the password table for its auth endpoint. It returns
// nil when there is nothing to serve.
func Hysteria(inbounds []store.Inbound, users []store.User) ([]byte, error) {
	st := hysteria.State{Users: map[string]string{}}
	for _, ib := range inbounds {
		if !ib.Enabled || ib.Protocol != "hysteria2" || ib.Settings.Hysteria == nil {
			continue
		}
		h := ib.Settings.Hysteria
		server := map[string]any{
			"listen": ":" + strconv.Itoa(ib.Port),
			// Anyone who is not a Hysteria client (scanners, browsers over
			// HTTP/3) sees this real site.
			"masquerade": map[string]any{
				"type":  "proxy",
				"proxy": map[string]any{"url": h.Masquerade, "rewriteHost": true},
			},
			"acl": map[string]any{"inline": hysteriaACL()},
		}
		if h.ObfsPassword != "" {
			server["obfs"] = map[string]any{
				"type":       "salamander",
				"salamander": map[string]any{"password": h.ObfsPassword},
			}
		}
		if h.UpMbps > 0 || h.DownMbps > 0 {
			bw := map[string]any{}
			if h.UpMbps > 0 {
				bw["up"] = strconv.Itoa(h.UpMbps) + " mbps"
			}
			if h.DownMbps > 0 {
				bw["down"] = strconv.Itoa(h.DownMbps) + " mbps"
			}
			server["bandwidth"] = bw
		}
		st.Instances = append(st.Instances, hysteria.Instance{
			Tag:     "in-" + strconv.FormatInt(ib.ID, 10),
			Server:  server,
			CertPEM: h.CertPEM,
			KeyPEM:  h.KeyPEM,
		})
	}
	if len(st.Instances) == 0 {
		return nil, nil
	}
	for _, u := range users {
		st.Users[u.UUID] = Email(u.ID)
	}
	return json.MarshalIndent(st, "", "  ")
}

// hysteriaACL mirrors the Xray defaults: no access to private ranges and no
// outgoing SMTP.
func hysteriaACL() []string {
	rules := make([]string, 0, len(privateCIDRs)+1)
	for _, cidr := range privateCIDRs {
		rules = append(rules, "reject("+cidr+")")
	}
	return append(rules, "reject(all, tcp/25)")
}
