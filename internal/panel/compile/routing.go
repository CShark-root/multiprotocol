package compile

import (
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"panel/internal/panel/geo"
	"panel/internal/panel/outbound"
	"panel/internal/panel/routing"
)

// Routing is what the compilers need besides inbounds and users: the
// expanded rules, DNS, the WARP device (nil when WARP is off or not
// registered) and, for sing-box, the geo categories the rules use.
type Routing struct {
	Rules  []routing.Final
	Final  string
	DNS    routing.DNSSettings
	Warp   *routing.WarpAccount
	Bridge routing.Bridge
	// GeoDir holds geoip.dat and geosite.dat for Hysteria's ACL.
	GeoDir string
	Sites  map[string]geo.Site
	IPs    map[string][]netip.Prefix
	// Custom are the admin's own exits with their loopback bridges.
	Custom []Custom
	// Exits — выходы через наши серверы; ForServer превращает их в Custom
	// или, на самом сервере-выходе, в скрытые подключения Cascades.
	Exits    []routing.CustomOutbound
	Cascades []routing.CustomOutbound
}

// Custom is a parsed custom exit.
type Custom struct {
	Tag    string // out_<id>
	Out    outbound.Parsed
	Bridge routing.Bridge
}

// outTag maps a rule outbound to the core tag ("out:x" -> "out_x").
func outTag(o string) string {
	if id, ok := strings.CutPrefix(o, routing.OutPrefix); ok {
		return routing.OutTag(id)
	}
	return o
}

func (rt Routing) custom(tag string) *Custom {
	for i := range rt.Custom {
		if rt.Custom[i].Tag == tag {
			return &rt.Custom[i]
		}
	}
	return nil
}

// DefaultRouting is the routing of a fresh panel, with no geo data loaded.
func DefaultRouting() Routing {
	return NewRouting(routing.Defaults(), "", nil, nil, nil)
}

// NewRouting expands settings for the compilers. has reports whether a
// geo entry exists in the installed databases (nil: assume it does).
func NewRouting(s routing.Settings, geoDir string, has func(string) bool, sites map[string]geo.Site, ips map[string][]netip.Prefix) Routing {
	rules, final := s.Expand(Email, has)
	rt := Routing{Rules: rules, Final: final, DNS: dnsAvailable(s.DNS, has), GeoDir: geoDir, Sites: sites, IPs: ips}
	if s.WarpUsable() {
		rt.Warp = s.WARP.Account
		rt.Bridge = s.Bridge
	}
	for _, o := range s.Outbounds {
		if o.Server != "" {
			if o.Cascade != nil {
				rt.Exits = append(rt.Exits, o)
			}
			continue
		}
		if p, err := outbound.Parse(o.Link); err == nil {
			rt.Custom = append(rt.Custom, Custom{Tag: routing.OutTag(o.ID), Out: p, Bridge: o.Bridge})
		}
	}
	return rt
}

// usesWarp says whether any rule or the final outbound goes to WARP.
func (rt Routing) usesWarp() bool {
	if rt.Warp == nil {
		return false
	}
	if rt.Final == routing.WARP {
		return true
	}
	return slices.ContainsFunc(rt.Rules, func(r routing.Final) bool { return r.Outbound == routing.WARP })
}

// split sorts match entries into domain-like ones and IP-like ones, with
// "geoip:private" replaced by the fixed private ranges so it never depends
// on the database.
func split(match []string) (domains, ips []string) {
	for _, m := range match {
		switch {
		case m == "geoip:private":
			ips = append(ips, privateCIDRs...)
		case strings.HasPrefix(m, "geoip:"):
			ips = append(ips, m)
		case strings.HasPrefix(m, "geosite:") || strings.HasPrefix(m, "domain:") || strings.HasPrefix(m, "full:") ||
			strings.HasPrefix(m, "keyword:") || strings.HasPrefix(m, "regexp:"):
			domains = append(domains, m)
		default:
			ips = append(ips, m) // an IP or CIDR
		}
	}
	return domains, ips
}

// portRanges parses a validated ports string.
func portRanges(s string) [][2]int {
	r, _ := routing.ParsePorts(s)
	return r
}

// dnsAvailable drops extra-DNS categories the databases lack (Xray would
// refuse to start) and servers left without domains.
func dnsAvailable(d routing.DNSSettings, has func(string) bool) routing.DNSSettings {
	if has == nil || len(d.Extra) == 0 {
		return d
	}
	out := d
	out.Extra = nil
	for _, e := range d.Extra {
		var keep []string
		for _, m := range e.Domains {
			if !strings.HasPrefix(m, "geosite:") || has(m) {
				keep = append(keep, m)
			}
		}
		if len(keep) > 0 {
			out.Extra = append(out.Extra, routing.DNSExtra{Server: e.Server, Domains: keep})
		}
	}
	return out
}

// ForNode — маршрутизация для другого нашего сервера. Устройство WARP одно,
// и с двух серверов сразу оно работать не может, поэтому на узлах всё, что
// шло в WARP, идёт напрямую. Мост WARP там тоже не нужен.
func (rt Routing) ForNode() Routing {
	out := rt
	out.Warp, out.Bridge = nil, routing.Bridge{}
	if out.Final == routing.WARP {
		out.Final = routing.Direct
	}
	out.Rules = make([]routing.Final, len(rt.Rules))
	for i, r := range rt.Rules {
		if r.Outbound == routing.WARP {
			r.Outbound = routing.Direct
		}
		out.Rules[i] = r
	}
	return out
}

// CascadeLink — ссылка vless:// на скрытое подключение сервера-выхода.
func CascadeLink(c routing.Cascade, host string) string {
	return "vless://" + c.UUID + "@" + net.JoinHostPort(host, strconv.Itoa(c.Port)) +
		"?type=tcp&security=reality&flow=xtls-rprx-vision&fp=chrome&sni=" + url.QueryEscape(c.SNI) +
		"&pbk=" + url.QueryEscape(c.PublicKey) + "&sid=" + c.ShortID
}

// ForServer — маршрутизация для одного сервера. self — "main" или id узла,
// selfHost — его адрес. Выход на этот же сервер (свой или по совпадению
// адреса в ссылке) становится «напрямую», иначе трафик ходил бы по кругу.
// Выход через наш сервер становится обычным своим выходом до него, а на
// самом сервере-выходе — скрытым подключением, которое этот трафик принимает.
func (rt Routing) ForServer(self, selfHost string, hostOf func(server string) string) Routing {
	out := rt
	direct := map[string]bool{}
	out.Custom = nil
	for _, c := range rt.Custom {
		if selfHost != "" && strings.EqualFold(c.Out.Host, selfHost) {
			direct[c.Tag] = true
			continue
		}
		out.Custom = append(out.Custom, c)
	}
	out.Cascades = nil
	for _, e := range rt.Exits {
		tag := routing.OutTag(e.ID)
		if e.Server == self {
			direct[tag] = true
			out.Cascades = append(out.Cascades, e)
			continue
		}
		host := hostOf(e.Server)
		p, err := outbound.Parse(CascadeLink(*e.Cascade, host))
		if host == "" || err != nil {
			direct[tag] = true // адрес сервера неизвестен — лучше напрямую, чем в никуда
			continue
		}
		out.Custom = append(out.Custom, Custom{Tag: tag, Out: p, Bridge: e.Bridge})
	}
	if len(direct) == 0 {
		return out
	}
	if direct[outTag(out.Final)] {
		out.Final = routing.Direct
	}
	out.Rules = make([]routing.Final, len(rt.Rules))
	for i, r := range rt.Rules {
		if direct[outTag(r.Outbound)] {
			r.Outbound = routing.Direct
		}
		out.Rules[i] = r
	}
	return out
}
