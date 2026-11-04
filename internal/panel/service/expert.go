package service

import (
	"crypto/mldsa"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"panel/internal/panel/store"
	"panel/internal/panel/validate"
)

// ExpertInput holds the optional fine-tuning of Xray inbounds. Every empty
// value means "Xray default".
type ExpertInput struct {
	Reality  *RealityExpert          `json:"reality,omitempty"`
	XHTTP    *store.XHTTPSettings    `json:"xhttp,omitempty"` // path and mode come from the main form
	Sniffing *store.SniffingSettings `json:"sniffing,omitempty"`
	Sockopt  *store.SockoptSettings  `json:"sockopt,omitempty"`
}

type RealityExpert struct {
	Xver         int      `json:"xver"`
	MaxTimeDiff  int      `json:"maxTimeDiff"`
	MinClientVer string   `json:"minClientVer"`
	MaxClientVer string   `json:"maxClientVer"`
	SpiderX      string   `json:"spiderX"`
	ShortIDs     []string `json:"shortIds"` // empty = keep / generate
	MLDSA65      bool     `json:"mldsa65"`  // post-quantum signature on top of X25519
}

var (
	rangeRe      = regexp.MustCompile(`^\d{1,9}(-\d{1,9})?$`)
	clientVerRe  = regexp.MustCompile(`^\d{1,3}(\.\d{1,3}){0,2}$`)
	shortIDRe    = regexp.MustCompile(`^([0-9a-f]{2}){0,8}$`)
	spiderXRe    = regexp.MustCompile(`^/[A-Za-z0-9/_.~?=&%-]{0,63}$`)
	headerNameRe = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
)

var (
	xhttpPlacements        = []string{"path", "cookie", "header", "query"}
	xhttpPaddingPlacements = []string{"queryInHeader", "header", "cookie", "query"}
	xhttpPaddingMethods    = []string{"repeat-x", "tokenish"}
	xhttpUplinkMethods     = []string{"POST", "PUT", "PATCH", "GET"}
	xhttpDataPlacements    = []string{"auto", "body", "cookie", "header"}
	sniffProtocols         = []string{"http", "tls", "quic"} // fakedns needs Xray's DNS module, not configured yet
	congestions            = []string{"bbr", "cubic", "reno"}
)

// normalizeExpert validates and copies expert settings onto an Xray inbound.
func normalizeExpert(f *store.InboundFields, in InboundInput) error {
	ex := in.Expert
	if r := f.Settings.Reality; r != nil && ex.Reality != nil {
		e := ex.Reality
		if e.Xver < 0 || e.Xver > 2 {
			return validate.Fail("xver", "0, 1 or 2")
		}
		if e.MaxTimeDiff < 0 || e.MaxTimeDiff > 86_400_000 {
			return validate.Fail("maxTimeDiff", "0 (off) to 86400000 ms")
		}
		for field, v := range map[string]string{"minClientVer": e.MinClientVer, "maxClientVer": e.MaxClientVer} {
			if v != "" && !validClientVer(v) {
				return validate.Fail(field, "a version like 25.9.11 (each part 0-255)")
			}
		}
		if e.SpiderX != "" && !spiderXRe.MatchString(e.SpiderX) {
			return validate.Fail("spiderX", "a path starting with '/', up to 64 characters")
		}
		if len(e.ShortIDs) > 8 {
			return validate.Fail("shortIds", "at most 8")
		}
		for _, id := range e.ShortIDs {
			if !shortIDRe.MatchString(strings.ToLower(id)) {
				return validate.Fail("shortIds", "hex, an even number of characters, up to 16")
			}
		}
		r.Xver, r.MaxTimeDiff, r.MinClientVer, r.MaxClientVer, r.SpiderX = e.Xver, e.MaxTimeDiff, e.MinClientVer, e.MaxClientVer, e.SpiderX
	}

	if x := f.Settings.XHTTP; x != nil && ex.XHTTP != nil {
		e := *ex.XHTTP
		if e.Host != "" && !hostnameRe.MatchString(e.Host) {
			return validate.Fail("host", "a domain name")
		}
		if len(e.Headers) > 16 {
			return validate.Fail("xhttpHeaders", "at most 16 headers")
		}
		for k, v := range e.Headers {
			if !headerNameRe.MatchString(k) || strings.EqualFold(k, "host") || strings.EqualFold(k, "content-length") {
				return validate.Fail("xhttpHeaders", fmt.Sprintf("invalid header name %q", k))
			}
			if len(v) > 256 || strings.ContainsAny(v, "\r\n") {
				return validate.Fail("xhttpHeaders", fmt.Sprintf("invalid value for %q", k))
			}
		}
		ranges := map[string]string{
			"paddingBytes": e.PaddingBytes, "maxEachPostBytes": e.MaxEachPostBytes,
			"minPostsIntervalMs": e.MinPostsIntervalMs, "streamUpServerSecs": e.StreamUpServerSecs,
		}
		if e.Xmux != nil {
			ranges["maxConcurrency"], ranges["maxConnections"] = e.Xmux.MaxConcurrency, e.Xmux.MaxConnections
			ranges["cMaxReuseTimes"], ranges["hMaxRequestTimes"] = e.Xmux.CMaxReuseTimes, e.Xmux.HMaxRequestTimes
			ranges["hMaxReusableSecs"] = e.Xmux.HMaxReusableSecs
			if e.Xmux.HKeepAlivePeriod < 0 || e.Xmux.HKeepAlivePeriod > 3600 {
				return validate.Fail("hKeepAlivePeriod", "0-3600 seconds")
			}
		}
		for field, v := range ranges {
			if v != "" && !validRange(v) {
				return validate.Fail(field, `a number or a range like "100-1000"`)
			}
		}
		enums := []struct {
			field, v string
			allowed  []string
		}{
			{"paddingPlacement", e.PaddingPlacement, xhttpPaddingPlacements},
			{"paddingMethod", e.PaddingMethod, xhttpPaddingMethods},
			{"uplinkHttpMethod", strings.ToUpper(e.UplinkHTTPMethod), xhttpUplinkMethods},
			{"sessionPlacement", e.SessionPlacement, xhttpPlacements},
			{"seqPlacement", e.SeqPlacement, xhttpPlacements},
			{"uplinkDataPlacement", e.UplinkDataPlacement, xhttpDataPlacements},
		}
		for _, en := range enums {
			if en.v != "" && !slices.Contains(en.allowed, en.v) {
				return validate.Fail(en.field, "one of "+strings.Join(en.allowed, ", "))
			}
		}
		packetUp := x.Mode == "packet-up"
		if strings.EqualFold(e.UplinkHTTPMethod, "GET") && !packetUp {
			return validate.Fail("uplinkHttpMethod", "GET works only in packet-up mode")
		}
		if (e.UplinkDataPlacement == "cookie" || e.UplinkDataPlacement == "header") && !packetUp {
			return validate.Fail("uplinkDataPlacement", "cookie and header work only in packet-up mode")
		}
		if e.MaxBufferedPosts < 0 || e.MaxBufferedPosts > 10000 {
			return validate.Fail("maxBufferedPosts", "0-10000")
		}
		e.Path, e.Mode, e.UplinkHTTPMethod = x.Path, x.Mode, strings.ToUpper(e.UplinkHTTPMethod)
		if len(e.Headers) == 0 {
			e.Headers = nil
		}
		*x = e
	}

	if s := ex.Sniffing; s != nil {
		for _, p := range s.DestOverride {
			if !slices.Contains(sniffProtocols, p) {
				return validate.Fail("sniffing", "protocols: "+strings.Join(sniffProtocols, ", "))
			}
		}
		cp := *s
		f.Settings.Sniffing = &cp
	}
	if so := ex.Sockopt; so != nil {
		if so.TCPCongestion != "" && !slices.Contains(congestions, so.TCPCongestion) {
			return validate.Fail("tcpCongestion", "one of "+strings.Join(congestions, ", "))
		}
		if *so != (store.SockoptSettings{}) {
			cp := *so
			f.Settings.Sockopt = &cp
		}
	}
	return nil
}

// fillRealityExpertSecrets applies custom short IDs and the ML-DSA-65 key
// pair (kept across edits, created when first switched on).
func fillRealityExpertSecrets(r *store.RealitySettings, in InboundInput, old *store.RealitySettings) error {
	e := in.Expert.Reality
	if e == nil {
		if old != nil {
			r.MLDSA65Seed, r.MLDSA65Verify = old.MLDSA65Seed, old.MLDSA65Verify
		}
		return nil
	}
	if len(e.ShortIDs) > 0 {
		ids := make([]string, len(e.ShortIDs))
		for i, id := range e.ShortIDs {
			ids[i] = strings.ToLower(id)
		}
		r.ShortIDs = ids
	}
	switch {
	case !e.MLDSA65:
		r.MLDSA65Seed, r.MLDSA65Verify = "", ""
	case old != nil && old.MLDSA65Seed != "":
		r.MLDSA65Seed, r.MLDSA65Verify = old.MLDSA65Seed, old.MLDSA65Verify
	default:
		seed, verify, err := NewMLDSA65()
		if err != nil {
			return err
		}
		r.MLDSA65Seed, r.MLDSA65Verify = seed, verify
	}
	return nil
}

// NewMLDSA65 returns a Reality ML-DSA-65 key pair encoded like
// `xray mldsa65`: the server seed and the client verification key.
func NewMLDSA65() (seed, verify string, err error) {
	b := make([]byte, mldsa.PrivateKeySize)
	rand.Read(b)
	return MLDSA65FromSeed(base64.RawURLEncoding.EncodeToString(b))
}

// MLDSA65FromSeed derives the verification key for a seed.
func MLDSA65FromSeed(seed string) (string, string, error) {
	b, err := base64.RawURLEncoding.DecodeString(seed)
	if err != nil {
		return "", "", err
	}
	sk, err := mldsa.NewPrivateKey(mldsa.MLDSA65(), b)
	if err != nil {
		return "", "", err
	}
	return seed, base64.RawURLEncoding.EncodeToString(sk.PublicKey().Bytes()), nil
}

func validRange(v string) bool {
	if !rangeRe.MatchString(v) {
		return false
	}
	from, to, ok := strings.Cut(v, "-")
	if !ok {
		return true
	}
	a, _ := strconv.Atoi(from)
	b, _ := strconv.Atoi(to)
	return a <= b
}

func validClientVer(v string) bool {
	if !clientVerRe.MatchString(v) {
		return false
	}
	for _, p := range strings.Split(v, ".") {
		if n, _ := strconv.Atoi(p); n > 255 {
			return false
		}
	}
	return true
}
