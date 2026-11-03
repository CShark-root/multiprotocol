package config

import (
	"testing"
)

func TestLoadOrCreateGeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()

	first, created, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected a new config to be created")
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("generated config is invalid: %v", err)
	}

	second, created, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("expected the existing config to be reused")
	}
	if second != first {
		t.Fatalf("config changed between loads: %+v vs %+v", first, second)
	}
}

func TestRandomBasePathIsValidAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		p := RandomBasePath()
		if !ValidBasePath(p) {
			t.Fatalf("invalid base path %q", p)
		}
		if seen[p] {
			t.Fatalf("duplicate base path %q", p)
		}
		seen[p] = true
	}
}
