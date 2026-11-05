package api

import (
	"net/http/httptest"
	"testing"
)

// Одна ссылка на все приложения: формат выбирается по User-Agent.
func TestSubscriptionFormat(t *testing.T) {
	for ua, want := range map[string]string{
		"v2rayNG/1.10.5":   "links",
		"v2raytun/android": "links",
		"Happ/2.1":         "links",
		"FlClash/v0.8.80 clash-verge Platform/android":              "mihomo",
		"ClashMetaForAndroid/2.11":                                  "mihomo",
		"mihomo/1.19.33":                                            "mihomo",
		"HiddifyNext/2.5.7 (android) like ClashMeta v2ray sing-box": "singbox",
		"SFA/1.14.4 (sing-box 1.14.4)":                              "singbox",
	} {
		r := httptest.NewRequest("GET", "/sub/x", nil)
		r.Header.Set("User-Agent", ua)
		got := "links"
		if wantsMihomo(r) {
			got = "mihomo"
		} else if wantsSingBox(r) {
			got = "singbox"
		}
		if got != want {
			t.Errorf("%q: got %s, want %s", ua, got, want)
		}
	}
}
