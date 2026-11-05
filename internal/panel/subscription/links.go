// Package subscription builds client share links and the subscription
// responses served at /<subPath>/<token>.
package subscription

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"panel/internal/panel/compile"
	"panel/internal/panel/store"
)

func endpoint(ib store.Inbound, host string) string {
	if ib.Address != "" {
		host = ib.Address
	}
	return net.JoinHostPort(host, strconv.Itoa(ib.Port))
}

func fragment(ib store.Inbound, u store.User) string {
	return url.PathEscape(ib.Remark + " · " + u.Name)
}

// realityQuery holds the parameters VLESS and Trojan links share.
func realityQuery(ib store.Inbound) url.Values {
	q := url.Values{}
	q.Set("security", "reality")
	r := ib.Settings.Reality
	if len(r.ServerNames) > 0 {
		q.Set("sni", r.ServerNames[0])
	}
	q.Set("fp", r.Fingerprint)
	q.Set("pbk", r.PublicKey)
	if len(r.ShortIDs) > 0 {
		q.Set("sid", r.ShortIDs[0])
	}
	if r.SpiderX != "" {
		q.Set("spx", r.SpiderX)
	}
	if r.MLDSA65Verify != "" {
		q.Set("pqv", r.MLDSA65Verify)
	}
	switch ib.Transport {
	case "xhttp":
		x := ib.Settings.XHTTP
		q.Set("type", "xhttp")
		q.Set("path", x.Path)
		q.Set("mode", x.Mode)
		if x.Host != "" {
			q.Set("host", x.Host)
		}
		// Settings the client must mirror travel as JSON in "extra"
		// (understood by v2rayN/v2rayNG/Happ and other Xray clients).
		if extra := compile.XHTTPClientExtra(x); extra != nil {
			b, _ := json.Marshal(extra)
			q.Set("extra", string(b))
		}
	case "grpc":
		q.Set("type", "grpc")
		q.Set("serviceName", ib.Settings.GRPC.ServiceName)
		q.Set("mode", "gun")
	default:
		q.Set("type", "tcp")
	}
	return q
}

// VLESSLink returns a vless:// URI understood by v2rayNG, v2rayN, Hiddify,
// Streisand and other Xray-based clients.
func VLESSLink(ib store.Inbound, u store.User, host string) string {
	q := realityQuery(ib)
	q.Set("encryption", "none")
	if ib.Transport == "tcp" {
		q.Set("flow", "xtls-rprx-vision")
	}
	return fmt.Sprintf("vless://%s@%s?%s#%s", u.UUID, endpoint(ib, host), q.Encode(), fragment(ib, u))
}

func TrojanLink(ib store.Inbound, u store.User, host string) string {
	return fmt.Sprintf("trojan://%s@%s?%s#%s", url.PathEscape(u.UUID), endpoint(ib, host), realityQuery(ib).Encode(), fragment(ib, u))
}

// VMessLink uses the de-facto v2rayN base64 JSON format.
func VMessLink(ib store.Inbound, u store.User, host string) string {
	h, port, _ := net.SplitHostPort(endpoint(ib, host))
	v := map[string]string{
		"v": "2", "ps": ib.Remark + " · " + u.Name, "add": h, "port": port, "id": u.UUID,
		"aid": "0", "scy": "auto", "net": "tcp", "type": "none", "host": "", "path": "", "tls": "",
	}
	if ib.Transport == "ws" && ib.Settings.WS != nil {
		v["net"], v["path"] = "ws", ib.Settings.WS.Path
	}
	b, _ := json.Marshal(v)
	return "vmess://" + base64.StdEncoding.EncodeToString(b)
}

// ShadowsocksLink follows SIP002 for 2022 ciphers: the user info is
// percent-encoded "method:serverPSK:userKey", not base64.
func ShadowsocksLink(ib store.Inbound, u store.User, host string) string {
	ss := ib.Settings.Shadowsocks
	userinfo := url.UserPassword(ss.Method, ss.ServerPSK+":"+compile.SSUserKey(u.UUID))
	return fmt.Sprintf("ss://%s@%s#%s", userinfo.String(), endpoint(ib, host), fragment(ib, u))
}

// Hysteria2Link returns a hysteria2:// URI (official scheme). A self-signed
// certificate is pinned, with insecure=1 for clients that cannot pin; the
// domain certificate is simply verified.
func Hysteria2Link(ib store.Inbound, u store.User, host string) string {
	h := ib.Settings.Hysteria
	q := url.Values{}
	q.Set("sni", h.SNI)
	if !h.DomainCert {
		// Self-signed: pinned. Hysteria and most apps read pinSHA256; Xray 26
		// cores (v2RayTun, v2rayNG) dropped "insecure" and read pcs.
		q.Set("insecure", "1")
		q.Set("pinSHA256", h.PinSHA256)
		q.Set("pcs", h.PinSHA256)
	}
	if h.ObfsPassword != "" {
		q.Set("obfs", "salamander")
		q.Set("obfs-password", h.ObfsPassword)
	}
	if h.HopPorts != "" {
		// Port hopping: the URI keeps one port so every parser accepts it;
		// clients that hop read the range from mport.
		q.Set("mport", h.HopPorts)
	}
	return fmt.Sprintf("hysteria2://%s@%s/?%s#%s", url.PathEscape(u.UUID), endpoint(ib, host), q.Encode(), fragment(ib, u))
}

func TUICLink(ib store.Inbound, u store.User, host string) string {
	q := url.Values{}
	q.Set("sni", ib.Settings.TLS.SNI)
	q.Set("alpn", "h3")
	q.Set("congestion_control", "bbr")
	q.Set("udp_relay_mode", "native")
	if !ib.Settings.TLS.DomainCert {
		q.Set("allow_insecure", "1")
	}
	return fmt.Sprintf("tuic://%s@%s?%s#%s", url.UserPassword(u.UUID, u.UUID).String(), endpoint(ib, host), q.Encode(), fragment(ib, u))
}

func AnyTLSLink(ib store.Inbound, u store.User, host string) string {
	q := url.Values{}
	q.Set("sni", ib.Settings.TLS.SNI)
	if !ib.Settings.TLS.DomainCert {
		q.Set("insecure", "1")
	}
	return fmt.Sprintf("anytls://%s@%s/?%s#%s", url.PathEscape(u.UUID), endpoint(ib, host), q.Encode(), fragment(ib, u))
}

// Link returns the share link for an inbound, or "" when the protocol has
// no common share-link format (ShadowTLS: use the sing-box subscription).
func Link(ib store.Inbound, u store.User, host string) string {
	s := ib.Settings
	switch {
	case ib.Protocol == "vless" && s.Reality != nil:
		return VLESSLink(ib, u, host)
	case ib.Protocol == "trojan" && s.Reality != nil:
		return TrojanLink(ib, u, host)
	case ib.Protocol == "vmess":
		return VMessLink(ib, u, host)
	case ib.Protocol == "shadowsocks" && s.Shadowsocks != nil:
		return ShadowsocksLink(ib, u, host)
	case ib.Protocol == "hysteria2" && s.Hysteria != nil:
		return Hysteria2Link(ib, u, host)
	case ib.Protocol == "tuic" && s.TLS != nil:
		return TUICLink(ib, u, host)
	case ib.Protocol == "anytls" && s.TLS != nil:
		return AnyTLSLink(ib, u, host)
	}
	return ""
}

// Links returns one link per enabled inbound that has a link format.
func Links(inbounds []store.Inbound, u store.User, host string) []string {
	var out []string
	for _, ib := range inbounds {
		if !ib.Enabled {
			continue
		}
		if l := Link(ib, u, host); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// Body is the classic base64 subscription format.
func Body(links []string) string {
	return base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n")))
}

// UserInfo is the `subscription-userinfo` header clients use to show
// traffic and expiry.
func UserInfo(u store.User) string {
	parts := []string{"upload=0", "download=" + strconv.FormatInt(u.TrafficUsed, 10)}
	parts = append(parts, "total="+strconv.FormatInt(u.TrafficLimit, 10))
	if u.ExpireAt != nil {
		parts = append(parts, "expire="+strconv.FormatInt(u.ExpireAt.Unix(), 10))
	}
	return strings.Join(parts, "; ")
}

// ProfileTitle is the `profile-title` header; base64 keeps non-ASCII names
// intact in HTTP headers.
func ProfileTitle(u store.User) string {
	return "base64:" + base64.StdEncoding.EncodeToString([]byte(u.Name))
}

// UpdateInterval is how often clients should refresh, in hours.
const UpdateInterval = 12 * time.Hour

// Settings — то, что подписка сообщает приложению кроме самих подключений.
// Хранится в настройке "subscription".
type Settings struct {
	Title      string `json:"title"`      // имя профиля; "{name}" — имя пользователя; пусто — имя пользователя
	Interval   int    `json:"interval"`   // часов между обновлениями, 1–168
	Announce   string `json:"announce"`   // короткое объявление, если приложение умеет его показывать
	SupportURL string `json:"supportUrl"` // куда писать за помощью (t.me/…, https://…)
}

const SettingsKey = "subscription"

func DefaultSettings() Settings { return Settings{Interval: int(UpdateInterval.Hours())} }

// Title — имя профиля для пользователя u.
func (s Settings) TitleFor(u store.User) string {
	t := strings.TrimSpace(s.Title)
	if t == "" {
		t = u.Name
	}
	t = strings.ReplaceAll(t, "{name}", u.Name)
	return "base64:" + base64.StdEncoding.EncodeToString([]byte(t))
}

// Headers дописывает заголовки подписки.
func (s Settings) Headers(h http.Header, u store.User) {
	h.Set("Profile-Title", s.TitleFor(u))
	h.Set("Subscription-Userinfo", UserInfo(u))
	iv := s.Interval
	if iv < 1 || iv > 168 {
		iv = int(UpdateInterval.Hours())
	}
	h.Set("Profile-Update-Interval", strconv.Itoa(iv))
	if a := strings.TrimSpace(s.Announce); a != "" {
		h.Set("Announce", "base64:"+base64.StdEncoding.EncodeToString([]byte(a)))
	}
	if su := strings.TrimSpace(s.SupportURL); su != "" {
		h.Set("Support-Url", su)
	}
}
