// Package routing holds the panel's routing intent: where server traffic
// goes (direct, blocked, through WARP, IPv4 only), the DNS the server
// resolves with, and where the geo databases come from. One model serves all
// cores; package compile translates it, and Support says which parts a core
// can honour.
package routing

import (
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"panel/internal/panel/outbound"
	"panel/internal/panel/validate"
)

// Outbounds a rule can send traffic to.
const (
	Direct = "direct"
	Block  = "block"
	WARP   = "warp"
	IPv4   = "ipv4" // direct, but resolve and connect over IPv4 only
)

var outbounds = []string{Direct, Block, WARP, IPv4}

// Rule sends matching traffic to Outbound. Match entries are OR-ed; ports,
// network, protocols and users narrow the rule further (AND).
//
// Match entries: "geosite:openai", "geoip:ru", "domain:example.com" (with
// subdomains), "full:www.example.com", "keyword:google", "regexp:...",
// an IP or a CIDR. A bare name is taken as "domain:".
type Rule struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Enabled   bool     `json:"enabled"`
	Match     []string `json:"match"`
	Ports     string   `json:"ports"`   // "443", "1000-2000", "80,443"
	Network   string   `json:"network"` // "", "tcp" or "udp"
	Protocols []string `json:"protocols"`
	Users     []int64  `json:"users"`
	Outbound  string   `json:"outbound"`
}

// Presets are switches that expand into ordinary rules.
type Presets struct {
	BlockPrivate    bool     `json:"blockPrivate"`
	BlockSMTP       bool     `json:"blockSmtp"`
	BlockBittorrent bool     `json:"blockBittorrent"`
	BlockAds        bool     `json:"blockAds"`
	GoogleIPv4      bool     `json:"googleIpv4"`
	WarpServices    []string `json:"warpServices"` // keys of Services
	// Countries send a whole country (its sites and IP ranges) somewhere,
	// e.g. Russia through WARP so Russian services never see the server IP.
	Countries []CountryRule `json:"countries"`
}

// CountryRule is a country code ("ru") or RuBlocked, and where it goes.
type CountryRule struct {
	Code     string `json:"code"`
	Outbound string `json:"outbound"`
}

// RuBlocked is the runetfreedom list of what is blocked in Russia.
const RuBlocked = "ru-blocked"

// CountryMatch is what a country rule matches: the country's site
// category and its IP ranges.
func CountryMatch(code string) []string {
	if code == RuBlocked {
		return []string{"geosite:ru-blocked", "geoip:ru-blocked"}
	}
	return []string{"geosite:category-" + code, "geoip:" + code}
}

// Service is a WARP preset: sites that refuse data-centre IPs or regions.
type Service struct {
	Key   string
	Match []string
}

// Services in display order. The geosite names exist in both supported
// databases.
var Services = []Service{
	{"openai", []string{"geosite:openai"}},
	{"gemini", []string{"geosite:google-gemini"}},
	{"anthropic", []string{"geosite:anthropic"}},
	{"netflix", []string{"geosite:netflix"}},
	{"spotify", []string{"geosite:spotify"}},
}

// WARP modes.
const (
	WarpOff   = "off"   // never used; rules that name WARP go direct
	WarpRules = "rules" // only what rules send there
	WarpAll   = "all"   // everything not matched by a rule
)

type WarpSettings struct {
	Mode    string       `json:"mode"`
	Account *WarpAccount `json:"account,omitempty"`
}

// WarpAccount is a registered Cloudflare WARP device.
type WarpAccount struct {
	ID            string    `json:"id"`
	Token         string    `json:"token"`
	License       string    `json:"license"`
	WarpPlus      bool      `json:"warpPlus"`
	PrivateKey    string    `json:"privateKey"`
	PeerPublicKey string    `json:"peerPublicKey"`
	Endpoint      string    `json:"endpoint"`
	Address4      string    `json:"address4"`
	Address6      string    `json:"address6"`
	Reserved      []int     `json:"reserved"`
	RegisteredAt  time.Time `json:"registeredAt"`
}

// Upstream DNS presets (DoH unless noted).
var DNSUpstreams = map[string]string{
	"cloudflare": "https://1.1.1.1/dns-query",
	"google":     "https://8.8.8.8/dns-query",
	"quad9":      "https://9.9.9.9/dns-query",
	"adguard":    "https://94.140.14.140/dns-query", // unfiltered
	"yandex":     "77.88.8.8",                       // plain DNS; answers Russian sites as seen from Russia
}

// DNS strategies.
const (
	PreferIPv4 = "prefer4"
	OnlyIPv4   = "ipv4"
	BothIPs    = "both"
)

type DNSSettings struct {
	Upstream string `json:"upstream"` // a key of DNSUpstreams, "system" or "custom"
	Custom   string `json:"custom"`   // https://host/dns-query or an IP (plain UDP)
	Strategy string `json:"strategy"`

	// Extra servers for some sites, e.g. Yandex DNS for Russian domains.
	Extra []DNSExtra `json:"extra"`
	// Expert flags (Xray); the defaults suit almost everyone.
	DisableCache           bool      `json:"disableCache"`
	DisableFallback        bool      `json:"disableFallback"`
	DisableFallbackIfMatch bool      `json:"disableFallbackIfMatch"`
	Parallel               bool      `json:"parallel"`
	SystemHosts            bool      `json:"systemHosts"`
	ClientIP               string    `json:"clientIp"` // EDNS client subnet hint
	Hosts                  []DNSHost `json:"hosts"`
}

// DNSExtra resolves Domains (match entries: domain:, full:, geosite:...)
// with Server (a key of DNSUpstreams, an https:// URL or an IP).
type DNSExtra struct {
	Server  string   `json:"server"`
	Domains []string `json:"domains"`
}

// DNSHost is a fixed answer, like a line of /etc/hosts.
type DNSHost struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

// ResolveServer turns a preset key, URL or IP into a server.
func ResolveServer(s string) (DNSServer, error) {
	if u, ok := DNSUpstreams[s]; ok {
		s = u
	}
	return ParseCustomDNS(s)
}

// Geo database sources.
var GeoSources = map[string][2]string{ // geoip, geosite
	"loyalsoldier": {
		"https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat",
		"https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat",
	},
	"runetfreedom": {
		"https://github.com/runetfreedom/russia-v2ray-rules-dat/releases/latest/download/geoip.dat",
		"https://github.com/runetfreedom/russia-v2ray-rules-dat/releases/latest/download/geosite.dat",
	},
}

type GeoSettings struct {
	Source     string `json:"source"` // a key of GeoSources or "custom"
	GeoIPURL   string `json:"geoipUrl"`
	GeoSiteURL string `json:"geositeUrl"`
	AutoUpdate bool   `json:"autoUpdate"`
}

// Bridge is the loopback SOCKS entrance into Xray's WARP outbound, used by
// Hysteria (which has no WireGuard of its own) and by the panel's WARP
// check. It is generated, never edited.
type Bridge struct {
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
}

type Settings struct {
	Presets Presets      `json:"presets"`
	Rules   []Rule       `json:"rules"`
	WARP    WarpSettings `json:"warp"`
	DNS     DNSSettings  `json:"dns"`
	Geo     GeoSettings  `json:"geo"`
	Bridge  Bridge       `json:"bridge"`
	// Outbounds are the admin's own exits (another server, a proxy).
	Outbounds []CustomOutbound `json:"outbounds"`
}

// CustomOutbound is an exit given as a share link. Rules send traffic to
// it as "out:<ID>". Hysteria and sing-box reach it through its own
// loopback bridge into Xray, like WARP.
type CustomOutbound struct {
	ID     string `json:"id"` // short, letters and digits
	Name   string `json:"name"`
	Link   string `json:"link"`
	Bridge Bridge `json:"bridge"`
}

// OutPrefix marks a rule outbound that names a custom exit.
const OutPrefix = "out:"

// OutTag is the core-side tag of a custom exit (no dashes: sing-box user
// outbounds are "<tag>-u7").
func OutTag(id string) string { return "out_" + id }

func (s Settings) hasOutbound(o string) bool {
	id, ok := strings.CutPrefix(o, OutPrefix)
	return ok && slices.ContainsFunc(s.Outbounds, func(c CustomOutbound) bool { return c.ID == id })
}

func (s Settings) validOutbound(o string) bool {
	return slices.Contains(outbounds, o) || s.hasOutbound(o)
}

// Defaults are what a fresh panel starts with: the protective presets on,
// WARP off, Cloudflare DNS preferring IPv4, Loyalsoldier databases updated
// daily.
func Defaults() Settings {
	return Settings{
		Presets: Presets{BlockPrivate: true, BlockSMTP: true, BlockBittorrent: true, WarpServices: []string{}, Countries: []CountryRule{}},
		Rules:   []Rule{},
		WARP:    WarpSettings{Mode: WarpOff},
		DNS:     DNSSettings{Upstream: "cloudflare", Strategy: PreferIPv4},
		Geo:     GeoSettings{Source: "loyalsoldier", AutoUpdate: true},
	}
}

var (
	domainRe  = regexp.MustCompile(`^(?i)[a-z0-9_]([a-z0-9_-]{0,62})(\.[a-z0-9_]([a-z0-9_-]{0,62}))*\.?$`)
	geoRe     = regexp.MustCompile(`^[a-z0-9!_.-]{1,64}(@[a-z0-9!_-]{1,32})?$`)
	countryRe = regexp.MustCompile(`^[a-z]{2}$`)
	portsRe   = regexp.MustCompile(`^\d{1,5}(-\d{1,5})?(,\d{1,5}(-\d{1,5})?)*$`)
)

// NormalizeMatch cleans one match entry and reports whether it is valid.
func NormalizeMatch(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	kind, val, ok := strings.Cut(s, ":")
	if ok && !strings.Contains(s, "/") && strings.Count(s, ":") > 1 {
		ok = false // an IPv6 address, not a prefix
	}
	if !ok {
		if _, err := netip.ParsePrefix(s); err == nil {
			p, _ := netip.ParsePrefix(s)
			return p.Masked().String(), true
		}
		if a, err := netip.ParseAddr(s); err == nil {
			return a.String(), true
		}
		kind, val = "domain", s
	}
	kind = strings.ToLower(kind)
	switch kind {
	case "geosite", "geoip":
		val = strings.ToLower(val)
		return kind + ":" + val, geoRe.MatchString(val)
	case "domain", "full":
		val = strings.TrimSuffix(strings.ToLower(val), ".")
		return kind + ":" + val, domainRe.MatchString(val) && len(val) <= 253
	case "keyword":
		val = strings.ToLower(val)
		return kind + ":" + val, len(val) > 0 && len(val) <= 100 && !strings.ContainsAny(val, " \t\"'")
	case "regexp":
		_, err := regexp.Compile(val)
		return kind + ":" + val, err == nil && len(val) <= 300
	}
	return s, false
}

// ParsePorts turns "80,443,1000-2000" into ranges.
func ParsePorts(s string) ([][2]int, error) {
	if s == "" {
		return nil, nil
	}
	if !portsRe.MatchString(s) {
		return nil, fmt.Errorf("bad ports")
	}
	var out [][2]int
	for _, part := range strings.Split(s, ",") {
		lo, hi, isRange := strings.Cut(part, "-")
		a, _ := strconv.Atoi(lo)
		b := a
		if isRange {
			b, _ = strconv.Atoi(hi)
		}
		if a < 1 || b > 65535 || a > b {
			return nil, fmt.Errorf("bad ports")
		}
		out = append(out, [2]int{a, b})
	}
	return out, nil
}

// Normalize validates settings coming from the UI and fills defaults.
// existingUsers says which user ids may appear in rules.
func Normalize(s Settings, userExists func(int64) bool) (Settings, error) {
	d := Defaults()
	if s.Rules == nil {
		s.Rules = []Rule{}
	}
	if len(s.Rules) > 200 {
		return s, validate.Fail("rules", "too many rules")
	}
	if err := normalizeOutbounds(&s); err != nil {
		return s, err
	}
	seen := map[string]bool{}
	for i := range s.Rules {
		r := &s.Rules[i]
		field := "rules." + strconv.Itoa(i)
		r.Name = strings.TrimSpace(r.Name)
		if len(r.Name) > 80 {
			return s, validate.Fail(field+".name", "too long")
		}
		if r.ID == "" || seen[r.ID] || len(r.ID) > 40 {
			r.ID = "r" + strconv.Itoa(i+1) + "-" + strconv.FormatInt(time.Now().UnixNano()%1e6, 36)
		}
		seen[r.ID] = true
		var match []string
		for _, m := range r.Match {
			if strings.TrimSpace(m) == "" {
				continue
			}
			n, ok := NormalizeMatch(m)
			if !ok {
				return s, validate.Fail(field+".match", "bad entry: "+m)
			}
			if !slices.Contains(match, n) {
				match = append(match, n)
			}
		}
		if len(match) > 500 {
			return s, validate.Fail(field+".match", "too many entries")
		}
		r.Match = match
		r.Ports = strings.ReplaceAll(r.Ports, " ", "")
		if _, err := ParsePorts(r.Ports); err != nil {
			return s, validate.Fail(field+".ports", "e.g. 443 or 1000-2000,8443")
		}
		if r.Network != "" && r.Network != "tcp" && r.Network != "udp" {
			return s, validate.Fail(field+".network", "tcp, udp or empty")
		}
		if r.Protocols == nil {
			r.Protocols = []string{}
		}
		for _, p := range r.Protocols {
			if p != "bittorrent" {
				return s, validate.Fail(field+".protocols", "only bittorrent")
			}
		}
		if r.Users == nil {
			r.Users = []int64{}
		}
		for _, u := range r.Users {
			if !userExists(u) {
				return s, validate.Fail(field+".users", "unknown user")
			}
		}
		if !s.validOutbound(r.Outbound) {
			return s, validate.Fail(field+".outbound", "direct, block, warp, ipv4 or one of your exits")
		}
		if len(r.Match) == 0 && r.Ports == "" && r.Network == "" && len(r.Protocols) == 0 && len(r.Users) == 0 {
			return s, validate.Fail(field+".match", "the rule matches nothing")
		}
	}
	if s.Presets.WarpServices == nil {
		s.Presets.WarpServices = []string{}
	}
	for _, k := range s.Presets.WarpServices {
		if !slices.ContainsFunc(Services, func(sv Service) bool { return sv.Key == k }) {
			return s, validate.Fail("presets.warpServices", "unknown service")
		}
	}
	if s.Presets.Countries == nil {
		s.Presets.Countries = []CountryRule{}
	}
	if len(s.Presets.Countries) > 60 {
		return s, validate.Fail("presets.countries", "too many countries")
	}
	seenCountry := map[string]bool{}
	for i, c := range s.Presets.Countries {
		c.Code = strings.ToLower(strings.TrimSpace(c.Code))
		if !countryRe.MatchString(c.Code) && c.Code != RuBlocked || seenCountry[c.Code] {
			return s, validate.Fail("presets.countries", "bad or repeated country: "+c.Code)
		}
		if !s.validOutbound(c.Outbound) {
			return s, validate.Fail("presets.countries", "direct, block, warp, ipv4 or one of your exits")
		}
		seenCountry[c.Code] = true
		s.Presets.Countries[i] = c
	}
	switch s.WARP.Mode {
	case WarpOff, WarpRules, WarpAll:
	case "":
		s.WARP.Mode = WarpOff
	default:
		return s, validate.Fail("warp.mode", "off, rules or all")
	}
	if s.DNS.Upstream == "" {
		s.DNS = d.DNS
	}
	if _, ok := DNSUpstreams[s.DNS.Upstream]; !ok && s.DNS.Upstream != "system" && s.DNS.Upstream != "custom" {
		return s, validate.Fail("dns.upstream", "unknown")
	}
	if s.DNS.Upstream == "custom" {
		s.DNS.Custom = strings.TrimSpace(s.DNS.Custom)
		if _, err := ParseCustomDNS(s.DNS.Custom); err != nil {
			return s, validate.Fail("dns.custom", "https://host/dns-query or an IP address")
		}
	} else {
		s.DNS.Custom = ""
	}
	if err := normalizeDNSExtra(&s.DNS); err != nil {
		return s, err
	}
	switch s.DNS.Strategy {
	case PreferIPv4, OnlyIPv4, BothIPs:
	case "":
		s.DNS.Strategy = PreferIPv4
	default:
		return s, validate.Fail("dns.strategy", "prefer4, ipv4 or both")
	}
	switch s.Geo.Source {
	case "loyalsoldier", "runetfreedom":
		s.Geo.GeoIPURL, s.Geo.GeoSiteURL = "", ""
	case "custom":
		for f, u := range map[string]string{"geo.geoipUrl": s.Geo.GeoIPURL, "geo.geositeUrl": s.Geo.GeoSiteURL} {
			if !strings.HasPrefix(u, "https://") || len(u) > 500 || strings.ContainsAny(u, " \t\n") {
				return s, validate.Fail(f, "an https:// link")
			}
		}
	case "":
		s.Geo = d.Geo
	default:
		return s, validate.Fail("geo.source", "unknown")
	}
	return s, nil
}

// DNSServer is a parsed upstream: DoH (URL set) or plain UDP (Addr only).
type DNSServer struct {
	URL  string // https://1.1.1.1/dns-query
	Host string // 1.1.1.1 or dns.example
	Addr string // ip:port to connect to, when the host is an IP
}

func ParseCustomDNS(s string) (DNSServer, error) {
	if a, err := netip.ParseAddr(s); err == nil {
		return DNSServer{Host: a.String(), Addr: netip.AddrPortFrom(a, 53).String()}, nil
	}
	if !strings.HasPrefix(s, "https://") || len(s) > 300 || strings.ContainsAny(s, " \t\n") {
		return DNSServer{}, fmt.Errorf("bad dns")
	}
	rest := strings.TrimPrefix(s, "https://")
	host, _, _ := strings.Cut(rest, "/")
	if host == "" {
		return DNSServer{}, fmt.Errorf("bad dns")
	}
	srv := DNSServer{URL: s, Host: host}
	h := host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		h, _, _ = strings.Cut(host, ":")
	}
	if a, err := netip.ParseAddr(strings.Trim(h, "[]")); err == nil {
		srv.Addr = netip.AddrPortFrom(a, 443).String()
	}
	return srv, nil
}

// Upstream returns the DNS server to use, or ok=false for the system
// resolver.
func (d DNSSettings) Server() (DNSServer, bool) {
	if d.Upstream == "custom" {
		s, err := ParseCustomDNS(d.Custom)
		return s, err == nil
	}
	u, ok := DNSUpstreams[d.Upstream]
	if !ok {
		return DNSServer{}, false
	}
	s, _ := ParseCustomDNS(u)
	return s, true
}

// Final is a rule after presets are expanded and users are named the way
// the cores know them.
type Final struct {
	Name      string
	Match     []string
	Ports     string
	Network   string
	Protocols []string
	Users     []string // core user tags (u7)
	Outbound  string
}

// WarpUsable says whether WARP can carry traffic now.
func (s Settings) WarpUsable() bool { return s.WARP.Mode != WarpOff && s.WARP.Account != nil }

// Expand turns presets and rules into the ordered list the cores apply
// (first match wins): protective blocks, the admin's rules, then the
// service presets. Rules sending to WARP fall back to direct while WARP is
// off or not registered. The final (catch-all) outbound is returned apart.
func (s Settings) Expand(email func(int64) string, has func(entry string) bool) (rules []Final, final string) {
	defer func() { rules = available(rules, has) }()
	warp := s.WarpUsable()
	out := func(o string) string {
		if o == WARP && !warp || strings.HasPrefix(o, OutPrefix) && !s.hasOutbound(o) {
			return Direct
		}
		return o
	}
	p := s.Presets
	if p.BlockPrivate {
		rules = append(rules, Final{Name: "preset:private", Match: []string{"geoip:private"}, Outbound: Block})
	}
	if p.BlockSMTP {
		rules = append(rules, Final{Name: "preset:smtp", Ports: "25", Network: "tcp", Outbound: Block})
	}
	if p.BlockBittorrent {
		rules = append(rules, Final{Name: "preset:bittorrent", Protocols: []string{"bittorrent"}, Outbound: Block})
	}
	if p.BlockAds {
		rules = append(rules, Final{Name: "preset:ads", Match: []string{"geosite:category-ads-all"}, Outbound: Block})
	}
	for _, r := range s.Rules {
		if !r.Enabled {
			continue
		}
		f := Final{Name: r.Name, Match: r.Match, Ports: r.Ports, Network: r.Network, Protocols: r.Protocols, Outbound: out(r.Outbound)}
		for _, id := range r.Users {
			f.Users = append(f.Users, email(id))
		}
		if len(r.Users) > 0 && len(f.Users) == 0 {
			continue
		}
		rules = append(rules, f)
	}
	for _, c := range p.Countries {
		name := "country:" + c.Code
		rules = append(rules, Final{Name: name, Match: CountryMatch(c.Code), Outbound: out(c.Outbound)})
	}
	if warp {
		for _, sv := range Services {
			if slices.Contains(p.WarpServices, sv.Key) {
				rules = append(rules, Final{Name: "preset:warp-" + sv.Key, Match: sv.Match, Outbound: WARP})
			}
		}
	}
	if p.GoogleIPv4 {
		rules = append(rules, Final{Name: "preset:google-ipv4", Match: []string{"geosite:google"}, Outbound: IPv4})
	}
	final = Direct
	if warp && s.WARP.Mode == WarpAll {
		final = WARP
	}
	return rules, final
}

// GeoNames lists the geosite and geoip categories rules refer to.
func GeoNames(rules []Final) (sites, ips []string) {
	for _, r := range rules {
		for _, m := range r.Match {
			if v, ok := strings.CutPrefix(m, "geosite:"); ok {
				v, _, _ = strings.Cut(v, "@")
				if !slices.Contains(sites, v) {
					sites = append(sites, v)
				}
			} else if v, ok := strings.CutPrefix(m, "geoip:"); ok && !slices.Contains(ips, v) {
				ips = append(ips, v)
			}
		}
	}
	return sites, ips
}

// Support says how well each core can apply a rule.
type Support struct {
	Xray     string `json:"xray"` // "full", "partial" or "none"
	Hysteria string `json:"hysteria"`
	SingBox  string `json:"singbox"`
}

func (r Rule) Support() Support {
	s := Support{Xray: "full", Hysteria: "full", SingBox: "full"}
	if len(r.Users) > 0 || len(r.Protocols) > 0 {
		// Hysteria's ACL knows neither users nor protocols; applying the rest
		// would widen the rule, so it is skipped there.
		s.Hysteria = "none"
	} else {
		for _, m := range r.Match {
			if strings.HasPrefix(m, "keyword:") || strings.HasPrefix(m, "regexp:") || strings.Contains(m, "@") {
				s.Hysteria = "partial"
			}
		}
	}
	return s
}

// available drops geo entries the installed databases do not have (Xray
// and Hysteria refuse to start on an unknown category) and rules left with
// nothing to match. has == nil means "everything is available".
func available(rules []Final, has func(string) bool) []Final {
	if has == nil {
		return rules
	}
	out := rules[:0:0]
	for _, r := range rules {
		if len(r.Match) == 0 {
			out = append(out, r)
			continue
		}
		var keep []string
		for _, m := range r.Match {
			if m == "geoip:private" || !(strings.HasPrefix(m, "geosite:") || strings.HasPrefix(m, "geoip:")) || has(m) {
				keep = append(keep, m)
			}
		}
		if len(keep) > 0 {
			r.Match = keep
			out = append(out, r)
		}
	}
	return out
}

func normalizeDNSExtra(d *DNSSettings) error {
	if d.Extra == nil {
		d.Extra = []DNSExtra{}
	}
	if d.Hosts == nil {
		d.Hosts = []DNSHost{}
	}
	if len(d.Extra) > 20 || len(d.Hosts) > 200 {
		return validate.Fail("dns.extra", "too many entries")
	}
	for i := range d.Extra {
		e := &d.Extra[i]
		e.Server = strings.TrimSpace(e.Server)
		if _, err := ResolveServer(e.Server); err != nil {
			return validate.Fail("dns.extra", "server: a preset, https://host/dns-query or an IP")
		}
		var domains []string
		for _, m := range e.Domains {
			if strings.TrimSpace(m) == "" {
				continue
			}
			n, ok := NormalizeMatch(m)
			if !ok || strings.HasPrefix(n, "geoip:") || !strings.Contains(n, ":") {
				return validate.Fail("dns.extra", "domains only (example.ru, geosite:category-ru): "+m)
			}
			if !slices.Contains(domains, n) {
				domains = append(domains, n)
			}
		}
		if len(domains) == 0 {
			return validate.Fail("dns.extra", "each extra server needs at least one domain")
		}
		e.Domains = domains
	}
	for i := range d.Hosts {
		h := &d.Hosts[i]
		h.Name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h.Name)), ".")
		h.IP = strings.TrimSpace(h.IP)
		if !domainRe.MatchString(h.Name) {
			return validate.Fail("dns.hosts", "bad name: "+h.Name)
		}
		if _, err := netip.ParseAddr(h.IP); err != nil {
			return validate.Fail("dns.hosts", "bad address for "+h.Name)
		}
	}
	d.ClientIP = strings.TrimSpace(d.ClientIP)
	if d.ClientIP != "" {
		if _, err := netip.ParseAddr(d.ClientIP); err != nil {
			return validate.Fail("dns.clientIp", "an IP address or empty")
		}
	}
	return nil
}

// DNSGeoNames lists geosite categories the extra DNS servers refer to.
func (d DNSSettings) DNSGeoNames() []string {
	var out []string
	for _, e := range d.Extra {
		for _, m := range e.Domains {
			if v, ok := strings.CutPrefix(m, "geosite:"); ok {
				v, _, _ = strings.Cut(v, "@")
				if !slices.Contains(out, v) {
					out = append(out, v)
				}
			}
		}
	}
	return out
}

var outIDRe = regexp.MustCompile(`^[a-z0-9]{1,12}$`)

func normalizeOutbounds(s *Settings) error {
	if s.Outbounds == nil {
		s.Outbounds = []CustomOutbound{}
	}
	if len(s.Outbounds) > 20 {
		return validate.Fail("outbounds", "at most 20 exits")
	}
	seen := map[string]bool{}
	for i := range s.Outbounds {
		o := &s.Outbounds[i]
		o.Name = strings.TrimSpace(o.Name)
		o.Link = strings.TrimSpace(o.Link)
		if !outIDRe.MatchString(o.ID) || seen[o.ID] {
			return validate.Fail("outbounds", "bad exit id")
		}
		seen[o.ID] = true
		if o.Name == "" || len(o.Name) > 40 {
			return validate.Fail("outbounds.name", "1-40 characters")
		}
		if _, err := outbound.Parse(o.Link); err != nil {
			return validate.Fail("outbounds.link", err.Error())
		}
	}
	return nil
}
