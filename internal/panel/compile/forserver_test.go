package compile

import (
	"testing"

	"panel/internal/panel/outbound"
	"panel/internal/panel/routing"
)

func TestForServer(t *testing.T) {
	c := &routing.Cascade{Port: 31000, UUID: "11111111-2222-4333-8444-555555555555", PublicKey: "pk", ShortID: "ab", SNI: "www.microsoft.com"}
	rt := Routing{
		Final:  "out:nl",
		Rules:  []routing.Final{{Outbound: "out:man"}},
		Custom: []Custom{{Tag: "out_man", Out: outbound.Parsed{Host: "198.51.100.9"}}},
		Exits:  []routing.CustomOutbound{{ID: "nl", Server: "5", Cascade: c}},
	}
	hosts := map[string]string{"main": "203.0.113.1", "5": "198.51.100.9"}
	host := func(s string) string { return hosts[s] }

	// Главный: идёт через NL; ручной выход на другой адрес остаётся.
	m := rt.ForServer("main", "203.0.113.1", host)
	if m.Final != "out:nl" || len(m.Custom) != 2 || len(m.Cascades) != 0 {
		t.Fatalf("main: final %q custom %d cascades %d", m.Final, len(m.Custom), len(m.Cascades))
	}
	// Сам NL: его выход и ручной выход на его же адрес — напрямую, плюс скрытое подключение.
	n := rt.ForServer("5", "198.51.100.9", host)
	if n.Final != routing.Direct || n.Rules[0].Outbound != routing.Direct || len(n.Custom) != 0 || len(n.Cascades) != 1 {
		t.Fatalf("nl: final %q rule %q custom %d cascades %d", n.Final, n.Rules[0].Outbound, len(n.Custom), len(n.Cascades))
	}
	if rt.Final != "out:nl" {
		t.Fatal("ForServer changed the original")
	}
}
