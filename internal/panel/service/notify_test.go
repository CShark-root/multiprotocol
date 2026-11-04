package service

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/panel/store"
)

func TestNotifierSettings(t *testing.T) {
	ctx := context.Background()
	st, _ := store.Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	defer st.Close()
	n := NewNotifier(st, NewEvents(st), t.TempDir(), "vps", slog.Default())
	if _, err := n.Save(ctx, 0, TelegramSettings{Token: "nope"}); err == nil {
		t.Error("accepted a bad token")
	}
	token := "123456789:AAEabcdefghijklmnopqrstuvwxyz0123456789"
	if _, err := n.Save(ctx, 0, TelegramSettings{Token: token, DailyBackup: true}); err == nil {
		t.Error("daily backup needs a password")
	}
	got, err := n.Save(ctx, 0, TelegramSettings{Token: token, ChatID: 42, Events: []string{EventCoreDown, "made.up"}, BackupPassword: "long enough"})
	if err != nil || len(got.Events) != 1 {
		t.Fatalf("save: %+v %v", got, err)
	}
	// Empty secrets keep the saved ones.
	again, _ := n.Save(ctx, 0, TelegramSettings{Events: []string{EventCoreUp}})
	if again.Token != token || again.BackupPassword != "long enough" || again.ChatID != 42 {
		t.Fatalf("secrets lost: %+v", again)
	}
	text := eventText(store.Event{Type: EventLoginThrottle, Data: map[string]any{"ip": "198.51.100.7"}}, "")
	if !strings.Contains(text, "198.51.100.7") {
		t.Errorf("text: %s", text)
	}
}
