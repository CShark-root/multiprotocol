//go:build linux

package agent

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// netSampler turns the cumulative counters of /proc/net/dev into rates.
// Readings closer together than minGap reuse the last rate, so several
// open dashboards do not shrink the averaging window to nothing.
var netSampler struct {
	sync.Mutex
	iface        string
	at           time.Time
	rx, tx       uint64
	rxBps, txBps float64
}

const minGap = 2 * time.Second

func netRate() (iface string, rxBps, txBps float64) {
	s := &netSampler
	s.Lock()
	defer s.Unlock()
	now := time.Now()
	if !s.at.IsZero() && now.Sub(s.at) < minGap {
		return s.iface, s.rxBps, s.txBps
	}
	name := defaultIface()
	rx, tx, ok := ifaceCounters(name)
	if !ok {
		return "", 0, 0
	}
	if name == s.iface && !s.at.IsZero() && now.Sub(s.at) < 5*time.Minute && rx >= s.rx && tx >= s.tx {
		sec := now.Sub(s.at).Seconds()
		s.rxBps, s.txBps = float64(rx-s.rx)/sec, float64(tx-s.tx)/sec
	} else {
		s.rxBps, s.txBps = 0, 0
	}
	s.iface, s.at, s.rx, s.tx = name, now, rx, tx
	return s.iface, s.rxBps, s.txBps
}

// defaultIface is the interface of the IPv4 default route.
func defaultIface() string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fl := strings.Fields(sc.Text())
		if len(fl) > 2 && fl[1] == "00000000" {
			return fl[0]
		}
	}
	return ""
}

func ifaceCounters(name string) (rx, tx uint64, ok bool) {
	if name == "" {
		return 0, 0, false
	}
	b, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		n, rest, found := strings.Cut(line, ":")
		if !found || strings.TrimSpace(n) != name {
			continue
		}
		fl := strings.Fields(rest)
		if len(fl) < 9 {
			return 0, 0, false
		}
		rx, _ = strconv.ParseUint(fl[0], 10, 64)
		tx, _ = strconv.ParseUint(fl[8], 10, 64)
		return rx, tx, true
	}
	return 0, 0, false
}
