// Package subscription builds client share links and the subscription
// response served at /<subPath>/<token>.
package subscription

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"panel/internal/panel/store"
)

// VLESSLink returns a vless:// URI understood by v2rayNG, v2rayN, Hiddify,
// Streisand and other Xray-based clients.
func VLESSLink(ib store.Inbound, u store.User, host string) string {
	if ib.Address != "" {
		host = ib.Address
	}
	q := url.Values{}
	q.Set("encryption", "none")
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
	switch ib.Transport {
	case "xhttp":
		q.Set("type", "xhttp")
		q.Set("path", ib.Settings.XHTTP.Path)
		q.Set("mode", ib.Settings.XHTTP.Mode)
	case "grpc":
		q.Set("type", "grpc")
		q.Set("serviceName", ib.Settings.GRPC.ServiceName)
		q.Set("mode", "gun")
	default:
		q.Set("type", "tcp")
		q.Set("flow", "xtls-rprx-vision")
	}
	return fmt.Sprintf("vless://%s@%s?%s#%s",
		u.UUID, net.JoinHostPort(host, strconv.Itoa(ib.Port)), q.Encode(), url.PathEscape(ib.Remark+" · "+u.Name))
}

// Links returns one link per enabled inbound.
func Links(inbounds []store.Inbound, u store.User, host string) []string {
	var out []string
	for _, ib := range inbounds {
		if ib.Enabled && ib.Protocol == "vless" {
			out = append(out, VLESSLink(ib, u, host))
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
