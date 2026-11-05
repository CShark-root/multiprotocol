package service

import (
	"context"
	"testing"

	"panel/internal/agent"
)

func TestVersionLess(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"1.14.4", "1.15.0", true},
		{"26.10.31", "26.10.31", false},
		{"2.13.0", "2.9.9", false},
		{"1.15.0-alpha.13", "1.15.0", false}, // непонятная версия — не трогаем
	} {
		if got := versionLess(c.a, c.b); got != c.want {
			t.Errorf("versionLess(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestCoreCheck(t *testing.T) {
	c := &CoreUpdater{
		health: func(context.Context) agent.NodeHealth {
			return agent.NodeHealth{Cores: []agent.CoreHealth{
				{Name: "xray", HealthStatus: agent.HealthStatus{Installed: true, Version: "26.10.31"}},
				{Name: "hysteria", HealthStatus: agent.HealthStatus{Installed: true, Version: "v2.13.0"}},
				{Name: "singbox", HealthStatus: agent.HealthStatus{Installed: true, Version: "1.14.4"}},
			}}
		},
		latest: func(_ context.Context, repo string) (string, error) {
			return map[string]string{"XTLS/Xray-core": "v26.10.31", "apernet/hysteria": "app/v2.14.0", "SagerNet/sing-box": "v1.14.4"}[repo], nil
		},
	}
	got, err := c.Check(context.Background())
	if err != nil || len(got) != 1 || got[0] != "hysteria" {
		t.Fatalf("Check = %v, %v", got, err)
	}
}
