package nethelper

import (
	"strings"
	"testing"
)

func TestSpeedLimitBatch(t *testing.T) {
	if got := speedLimitBatch("enp0s3"); got != "qdisc del dev enp0s3 ingress\nqdisc del dev enp0s3 root\nqdisc add dev enp0s3 root fq\n" {
		t.Fatalf("cleanup: %q", got)
	}
}

func TestSpeedLimitRuleset(t *testing.T) {
	rules := speedLimitRuleset([]SpeedLimit{{Mark: 0x50000007, Mbps: 5}, {Mark: 0x50000009, Mbps: 20}})
	for _, want := range []string{"table inet panel_speed", "hook output priority mangle", "ct mark set meta mark", "hook prerouting priority filter", "ct mark 0x50000007 limit rate over 625000 bytes/second burst 32 kbytes counter drop", "ct mark 0x50000009 limit rate over 2500000 bytes/second burst 32 kbytes counter drop"} {
		if !strings.Contains(rules, want) {
			t.Errorf("missing %q in\n%s", want, rules)
		}
	}
	if rules := speedLimitRuleset(nil); strings.Contains(rules, "chain") {
		t.Fatalf("cleanup leaves chains: %s", rules)
	}
}

func TestMissingQdiscOutput(t *testing.T) {
	for _, out := range []string{
		"",
		"Error: Invalid handle.\nCommand failed -:1",
		"Error: No such file or directory.\nCommand failed -:2",
		"Error: Cannot find specified qdisc on specified device.\nCommand failed -:1\nError: Cannot delete qdisc with handle of zero.\nCommand failed -:2",
	} {
		if !missingQdiscOutput(out) {
			t.Errorf("must tolerate %q", out)
		}
	}
	if missingQdiscOutput("Error: Operation not permitted.") {
		t.Fatal("real tc errors must not be hidden")
	}
}

func TestValidSpeedLimits(t *testing.T) {
	if err := validSpeedLimits([]SpeedLimit{{Mark: 0x50000001, Mbps: 5}}); err != nil {
		t.Fatal(err)
	}
	for _, limits := range [][]SpeedLimit{
		{{Mark: 0x50000001, Mbps: 0}},
		{{Mark: 0x40000001, Mbps: 5}},
		{{Mark: 0x50000001, Mbps: 5}, {Mark: 0x50000001, Mbps: 10}},
	} {
		if err := validSpeedLimits(limits); err == nil {
			t.Errorf("must reject %+v", limits)
		}
	}
}
