// Package geo reads and updates the v2ray-format geo databases
// (geoip.dat, geosite.dat) that Xray and Hysteria load directly. sing-box
// cannot read them, so the panel extracts the categories its rules use and
// inlines them into the sing-box config.
package geo

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"slices"
	"strings"
)

// Domain kinds as stored in geosite.dat.
const (
	KindKeyword = 0 // substring
	KindRegex   = 1
	KindSuffix  = 2 // the domain and its subdomains
	KindFull    = 3 // exact name
)

type Domain struct {
	Kind  int
	Value string
	Attrs []string // "cn", "ads" and the like (written "@cn" in rules)
}

// Site is one geosite category, e.g. "openai".
type Site struct {
	Domains []Domain
}

var errTruncated = errors.New("truncated protobuf")

// field reads one protobuf field and returns its number, wire type and the
// varint value or the payload of a length-delimited field.
func field(b []byte) (num, typ int, val uint64, payload, rest []byte, err error) {
	key, n := binary.Uvarint(b)
	if n <= 0 {
		return 0, 0, 0, nil, nil, errTruncated
	}
	b = b[n:]
	num, typ = int(key>>3), int(key&7)
	switch typ {
	case 0:
		v, n := binary.Uvarint(b)
		if n <= 0 {
			return 0, 0, 0, nil, nil, errTruncated
		}
		return num, typ, v, nil, b[n:], nil
	case 2:
		l, n := binary.Uvarint(b)
		if n <= 0 || uint64(len(b)-n) < l {
			return 0, 0, 0, nil, nil, errTruncated
		}
		return num, typ, 0, b[n : n+int(l)], b[n+int(l):], nil
	case 1:
		if len(b) < 8 {
			return 0, 0, 0, nil, nil, errTruncated
		}
		return num, typ, 0, nil, b[8:], nil
	case 5:
		if len(b) < 4 {
			return 0, 0, 0, nil, nil, errTruncated
		}
		return num, typ, 0, nil, b[4:], nil
	}
	return 0, 0, 0, nil, nil, fmt.Errorf("unsupported wire type %d", typ)
}

// entries calls fn with the code and body of every top-level entry
// (GeoSiteList.entry and GeoIPList.entry are both field 1, and both keep
// their code in field 1).
func entries(data []byte, fn func(code string, body []byte) error) error {
	for len(data) > 0 {
		num, typ, _, payload, rest, err := field(data)
		if err != nil {
			return err
		}
		data = rest
		if num != 1 || typ != 2 {
			continue
		}
		code, err := firstString(payload)
		if err != nil {
			return err
		}
		if err := fn(strings.ToLower(code), payload); err != nil {
			return err
		}
	}
	return nil
}

// firstString returns field 1 of a message when it is a string.
func firstString(body []byte) (string, error) {
	for len(body) > 0 {
		num, typ, _, payload, rest, err := field(body)
		if err != nil {
			return "", err
		}
		if num == 1 && typ == 2 {
			return string(payload), nil
		}
		body = rest
	}
	return "", nil
}

// Codes lists the categories of a geoip.dat or geosite.dat file.
func Codes(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var codes []string
	err = entries(data, func(code string, _ []byte) error {
		codes = append(codes, code)
		return nil
	})
	slices.Sort(codes)
	return codes, err
}

// Sites reads the wanted geosite categories (lower case). Categories the
// file does not have are absent from the result.
func Sites(path string, want []string) (map[string]Site, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]Site{}
	err = entries(data, func(code string, body []byte) error {
		if !slices.Contains(want, code) {
			return nil
		}
		var s Site
		for len(body) > 0 {
			num, typ, _, payload, rest, err := field(body)
			if err != nil {
				return err
			}
			body = rest
			if num == 2 && typ == 2 {
				d, err := parseDomain(payload)
				if err != nil {
					return err
				}
				s.Domains = append(s.Domains, d)
			}
		}
		out[code] = s
		return nil
	})
	return out, err
}

func parseDomain(b []byte) (Domain, error) {
	var d Domain
	for len(b) > 0 {
		num, typ, val, payload, rest, err := field(b)
		if err != nil {
			return d, err
		}
		b = rest
		switch {
		case num == 1 && typ == 0:
			d.Kind = int(val)
		case num == 2 && typ == 2:
			d.Value = string(payload)
		case num == 3 && typ == 2: // Attribute{key = 1, ...}
			if key, err := firstString(payload); err == nil && key != "" {
				d.Attrs = append(d.Attrs, strings.ToLower(key))
			}
		}
	}
	return d, nil
}

// IPs reads the wanted geoip categories as prefixes.
func IPs(path string, want []string) (map[string][]netip.Prefix, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string][]netip.Prefix{}
	err = entries(data, func(code string, body []byte) error {
		if !slices.Contains(want, code) {
			return nil
		}
		var list []netip.Prefix
		for len(body) > 0 {
			num, typ, _, payload, rest, err := field(body)
			if err != nil {
				return err
			}
			body = rest
			if num != 2 || typ != 2 {
				continue
			}
			if p, ok := parseCIDR(payload); ok {
				list = append(list, p)
			}
		}
		out[code] = list
		return nil
	})
	return out, err
}

func parseCIDR(b []byte) (netip.Prefix, bool) {
	var ip []byte
	var bits int
	for len(b) > 0 {
		num, typ, val, payload, rest, err := field(b)
		if err != nil {
			return netip.Prefix{}, false
		}
		b = rest
		switch {
		case num == 1 && typ == 2:
			ip = payload
		case num == 2 && typ == 0:
			bits = int(val)
		}
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return netip.Prefix{}, false
	}
	p, err := addr.Unmap().Prefix(bits)
	return p, err == nil
}
