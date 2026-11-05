package warp

import "testing"

func TestNextEndpoint(t *testing.T) {
	for in, want := range map[string]string{
		"162.159.192.1:2408": "162.159.192.1:500",
		"162.159.192.1:4233": "162.159.192.1:2408",
		"162.159.192.1:9999": "162.159.192.1:2408", // незнакомый порт — с начала списка
	} {
		if got := NextEndpoint(in); got != want {
			t.Errorf("NextEndpoint(%q) = %q, want %q", in, got, want)
		}
	}
}
