package service

import (
	"context"
	"time"

	"panel/internal/amnezia"
)

const (
	EventAmneziaDown = "amnezia.down"
	EventAmneziaUp   = "amnezia.up"
)

// WatchAmnezia records an event when an Amnezia container stops or comes
// back (checked once a minute through the read-only helper).
func WatchAmnezia(ctx context.Context, client *amnezia.HelperClient, events *Events) {
	if client == nil {
		return
	}
	running := map[string]bool{}
	first := true
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		if st, err := client.Status(ctx); err == nil && st.Docker {
			for _, c := range st.Containers {
				up := c.State == "running"
				was, known := running[c.Name]
				if !first && known && was != up {
					typ, sev := EventAmneziaUp, "info"
					if !up {
						typ, sev = EventAmneziaDown, "danger"
					}
					events.Add(ctx, typ, sev, 0, c.Status, map[string]any{"container": c.Name})
				}
				running[c.Name] = up
			}
			first = false
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
