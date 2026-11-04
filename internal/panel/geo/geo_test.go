package geo

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// entry builds one top-level GeoSite/GeoIP record: field 1 = {1: code, 2: body}.
func entry(code string, body []byte) []byte {
	inner := append([]byte{0x0a, byte(len(code))}, code...)
	inner = append(inner, body...)
	out := []byte{0x0a}
	out = binary.AppendUvarint(out, uint64(len(inner)))
	return append(out, inner...)
}

func domain(kind byte, v string) []byte {
	d := append([]byte{0x08, kind, 0x12, byte(len(v))}, v...)
	return append([]byte{0x12, byte(len(d))}, d...)
}

func TestSitesAndSubset(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "geosite.dat")
	data := append(entry("OPENAI", domain(KindSuffix, "openai.com")), entry("CATEGORY-RU", domain(KindFull, "ya.ru"))...)
	os.WriteFile(src, data, 0o644)

	codes, err := Codes(src)
	if err != nil || len(codes) != 2 || codes[0] != "category-ru" {
		t.Fatalf("codes %v %v", codes, err)
	}
	sites, _ := Sites(src, []string{"openai"})
	if d := sites["openai"].Domains; len(d) != 1 || d[0].Value != "openai.com" || d[0].Kind != KindSuffix {
		t.Fatalf("sites %+v", sites)
	}
	sub := filepath.Join(dir, "sub.dat")
	if err := Subset(src, sub, []string{"category-ru"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := Codes(sub); len(got) != 1 || got[0] != "category-ru" {
		t.Fatalf("subset codes %v", got)
	}
}
