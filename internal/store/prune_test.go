package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneRetention(t *testing.T) {
	s := testStore(t)
	now := time.Now()
	old := mustSave(t, s, testPNG(t, 32, 32), now.Add(-10*24*time.Hour))
	recent := mustSave(t, s, testPNG(t, 32, 32), now.Add(-2*24*time.Hour))

	removed, err := s.Prune(7, 0)
	if err != nil {
		t.Fatalf("Prune(7, 0) error = %v", err)
	}
	if removed != 1 {
		t.Fatalf("Prune(7, 0) removed = %d, want 1", removed)
	}
	if ids := listedIDs(s); len(ids) != 1 || ids[0] != recent.ID {
		t.Fatalf("List() after Prune = %v, want only %q", ids, recent.ID)
	}
	for _, name := range []string{old.File, old.Thumb} {
		if _, err := os.Stat(filepath.Join(s.Dir(), name)); !os.IsNotExist(err) {
			t.Fatalf("%q still exists after Prune (err = %v)", name, err)
		}
	}
	if _, err := Open(s.Dir()); err != nil {
		t.Fatalf("Open() after Prune error = %v", err)
	}

	removed, err = s.Prune(7, 0)
	if err != nil || removed != 0 {
		t.Fatalf("second Prune(7, 0) = (%d, %v), want (0, nil)", removed, err)
	}
}

func TestPruneMaxBytes(t *testing.T) {
	base := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

	t.Run("keeps newest entries that fit", func(t *testing.T) {
		s := testStore(t)
		oldest := mustSave(t, s, testPNG(t, 64, 48), base)
		middle := mustSave(t, s, testPNG(t, 48, 32), base.Add(time.Minute))
		newest := mustSave(t, s, testPNG(t, 32, 32), base.Add(2*time.Minute))

		budget := newest.Size + middle.Size
		removed, err := s.Prune(0, budget)
		if err != nil {
			t.Fatalf("Prune(0, %d) error = %v", budget, err)
		}
		if removed != 1 {
			t.Fatalf("Prune() removed = %d, want 1", removed)
		}
		if ids := listedIDs(s); len(ids) != 2 || ids[0] != newest.ID || ids[1] != middle.ID {
			t.Fatalf("List() after Prune = %v, want [%q %q]", ids, newest.ID, middle.ID)
		}
		if _, err := os.Stat(filepath.Join(s.Dir(), oldest.File)); !os.IsNotExist(err) {
			t.Fatalf("oldest screenshot still exists after Prune (err = %v)", err)
		}
	})

	t.Run("always keeps the newest screenshot", func(t *testing.T) {
		s := testStore(t)
		mustSave(t, s, testPNG(t, 64, 48), base)
		mustSave(t, s, testPNG(t, 48, 32), base.Add(time.Minute))
		newest := mustSave(t, s, testPNG(t, 32, 32), base.Add(2*time.Minute))

		removed, err := s.Prune(0, 1)
		if err != nil {
			t.Fatalf("Prune(0, 1) error = %v", err)
		}
		if removed != 2 {
			t.Fatalf("Prune() removed = %d, want 2", removed)
		}
		if ids := listedIDs(s); len(ids) != 1 || ids[0] != newest.ID {
			t.Fatalf("List() after Prune = %v, want only %q", ids, newest.ID)
		}
	})

	t.Run("leaves history under budget untouched", func(t *testing.T) {
		s := testStore(t)
		first := mustSave(t, s, testPNG(t, 32, 32), base)
		second := mustSave(t, s, testPNG(t, 32, 32), base.Add(time.Minute))
		budget := first.Size + second.Size
		removed, err := s.Prune(0, budget)
		if err != nil || removed != 0 {
			t.Fatalf("Prune(0, %d) = (%d, %v), want (0, nil)", budget, removed, err)
		}
		if entries := s.List(); len(entries) != 2 {
			t.Fatalf("List() = %v, want 2 entries", entries)
		}
	})
}

func TestPruneDisabled(t *testing.T) {
	s := testStore(t)
	if removed, err := s.Prune(0, 0); err != nil || removed != 0 {
		t.Fatalf("Prune(0, 0) on empty store = (%d, %v), want (0, nil)", removed, err)
	}
	entry := mustSave(t, s, testPNG(t, 32, 32), time.Now())
	if removed, err := s.Prune(0, 0); err != nil || removed != 0 {
		t.Fatalf("Prune(0, 0) = (%d, %v), want (0, nil)", removed, err)
	}
	if ids := listedIDs(s); len(ids) != 1 || ids[0] != entry.ID {
		t.Fatalf("List() = %v, want %q", ids, entry.ID)
	}
}
