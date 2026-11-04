package nethelper

import (
	"strings"
	"testing"
)

func TestRuleset(t *testing.T) {
	got := ruleset([]Hop{{From: 20000, To: 20100, Target: 443}})
	for _, want := range []string{"delete table inet panel_hop", "type nat hook prerouting priority dstnat", "udp dport 20000-20100 redirect to :443"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if empty := ruleset(nil); strings.Contains(empty, "chain") {
		t.Errorf("an empty list must only remove the table:\n%s", empty)
	}
}

func TestCheck(t *testing.T) {
	amnezia := map[int]bool{34813: true, 443: true}
	if err := check([]Hop{{From: 30000, To: 40000, Target: 443}}, amnezia); err == nil {
		t.Error("a range over Amnezia's port must be refused")
	}
	if err := check([]Hop{{From: 20000, To: 30000, Target: 443}}, amnezia); err != nil {
		t.Errorf("a free range was refused: %v", err)
	}
	if err := check([]Hop{{From: 20000, To: 21000, Target: 443}, {From: 20500, To: 22000, Target: 8443}}, nil); err == nil {
		t.Error("overlapping ranges must be refused")
	}
	if err := check([]Hop{{From: 500, To: 600, Target: 443}}, nil); err == nil {
		t.Error("privileged ports must be refused")
	}
}
