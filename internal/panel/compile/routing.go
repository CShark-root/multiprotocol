package compile

import (
	"net/netip"
	"slices"
	"strings"

	"panel/internal/panel/geo"
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
}

// DefaultRouting is the routing of a fresh panel, with no geo data loaded.
func DefaultRouting() Routing {
	return NewRouting(routing.Defaults(), "", nil, nil)
}

// NewRouting expands settings for the compilers.
func NewRouting(s routing.Settings, geoDir string, sites map[string]geo.Site, ips map[string][]netip.Prefix) Routing {
	rules, final := s.Expand(Email)
	rt := Routing{Rules: rules, Final: final, DNS: s.DNS, GeoDir: geoDir, Sites: sites, IPs: ips}
	if s.WarpUsable() {
		rt.Warp = s.WARP.Account
		rt.Bridge = s.Bridge
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
