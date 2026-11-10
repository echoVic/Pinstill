package store

import (
	"math"
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

func TestPruneRetentionZeroOrNegativeDisablesAgeLimit(t *testing.T) {
	s := testStore(t)
	ancient := mustSave(t, s, testPNG(t, 32, 32), time.Now().Add(-10*365*24*time.Hour))
	for _, days := range []int{0, -1, -100} {
		removed, err := s.Prune(days, 0)
		if err != nil || removed != 0 {
			t.Fatalf("Prune(%d, 0) = (%d, %v), want (0, nil)", days, removed, err)
		}
	}
	if ids := listedIDs(s); len(ids) != 1 || ids[0] != ancient.ID {
		t.Fatalf("List() after disabled retention = %v, want %q", ids, ancient.ID)
	}
}

func TestPruneRetentionWindowBoundary(t *testing.T) {
	s := testStore(t)
	now := time.Now()
	// Comfortably outside/inside the 7 day window; Prune computes its cutoff
	// from its own time.Now(), so the margins keep this deterministic.
	justOutside := mustSave(t, s, testPNG(t, 32, 32), now.Add(-7*24*time.Hour-1*time.Hour))
	justInside := mustSave(t, s, testPNG(t, 32, 32), now.Add(-7*24*time.Hour+1*time.Hour))
	// A clock jump that puts a screenshot in the future must never prune it.
	future := mustSave(t, s, testPNG(t, 32, 32), now.Add(24*time.Hour))

	removed, err := s.Prune(7, 0)
	if err != nil {
		t.Fatalf("Prune(7, 0) error = %v", err)
	}
	if removed != 1 {
		t.Fatalf("Prune(7, 0) removed = %d, want 1", removed)
	}
	ids := listedIDs(s)
	want := []string{future.ID, justInside.ID}
	if len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] {
		t.Fatalf("List() after boundary Prune = %v, want %v", ids, want)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), justOutside.File)); !os.IsNotExist(err) {
		t.Fatalf("entry outside the retention window still exists (err = %v)", err)
	}
}

func TestPruneMaxBytesBoundaries(t *testing.T) {
	base := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

	t.Run("empty store ignores both limits", func(t *testing.T) {
		s := testStore(t)
		removed, err := s.Prune(7, 1)
		if err != nil || removed != 0 {
			t.Fatalf("Prune(7, 1) on empty store = (%d, %v), want (0, nil)", removed, err)
		}
	})

	t.Run("zero and negative budgets disable the size limit", func(t *testing.T) {
		s := testStore(t)
		mustSave(t, s, testPNG(t, 32, 32), base)
		mustSave(t, s, testPNG(t, 32, 32), base.Add(time.Minute))
		for _, budget := range []int64{0, -1024} {
			removed, err := s.Prune(0, budget)
			if err != nil || removed != 0 {
				t.Fatalf("Prune(0, %d) = (%d, %v), want (0, nil)", budget, removed, err)
			}
		}
		if got := len(s.List()); got != 2 {
			t.Fatalf("List() has %d entries, want 2", got)
		}
	})

	t.Run("huge budget keeps everything", func(t *testing.T) {
		s := testStore(t)
		oldest := mustSave(t, s, testPNG(t, 64, 48), base)
		newest := mustSave(t, s, testPNG(t, 32, 32), base.Add(time.Minute))
		removed, err := s.Prune(0, math.MaxInt64)
		if err != nil || removed != 0 {
			t.Fatalf("Prune(0, MaxInt64) = (%d, %v), want (0, nil)", removed, err)
		}
		if ids := listedIDs(s); len(ids) != 2 || ids[0] != newest.ID || ids[1] != oldest.ID {
			t.Fatalf("List() = %v, want [%q %q]", ids, newest.ID, oldest.ID)
		}
	})

	t.Run("one byte over budget removes the oldest entry", func(t *testing.T) {
		s := testStore(t)
		oldest := mustSave(t, s, testPNG(t, 64, 48), base)
		middle := mustSave(t, s, testPNG(t, 48, 32), base.Add(time.Minute))
		newest := mustSave(t, s, testPNG(t, 32, 32), base.Add(2*time.Minute))
		total := oldest.Size + middle.Size + newest.Size

		removed, err := s.Prune(0, total-1)
		if err != nil {
			t.Fatalf("Prune(0, total-1) error = %v", err)
		}
		if removed != 1 {
			t.Fatalf("Prune(0, total-1) removed = %d, want 1", removed)
		}
		if ids := listedIDs(s); len(ids) != 2 || ids[0] != newest.ID || ids[1] != middle.ID {
			t.Fatalf("List() = %v, want [%q %q]", ids, newest.ID, middle.ID)
		}
	})
}

func TestPruneCombinesRetentionAndSize(t *testing.T) {
	s := testStore(t)
	now := time.Now()
	tooOld := mustSave(t, s, testPNG(t, 64, 48), now.Add(-10*24*time.Hour))
	alsoOld := mustSave(t, s, testPNG(t, 64, 48), now.Add(-9*24*time.Hour))
	middle := mustSave(t, s, testPNG(t, 48, 32), now.Add(-2*24*time.Hour))
	newest := mustSave(t, s, testPNG(t, 32, 32), now.Add(-1*time.Hour))

	// The age limit drops the two old screenshots first; the size budget then
	// only counts the survivors, so it removes "middle" and keeps "newest".
	removed, err := s.Prune(7, newest.Size)
	if err != nil {
		t.Fatalf("Prune(7, %d) error = %v", newest.Size, err)
	}
	if removed != 3 {
		t.Fatalf("Prune(7, %d) removed = %d, want 3", newest.Size, removed)
	}
	if ids := listedIDs(s); len(ids) != 1 || ids[0] != newest.ID {
		t.Fatalf("List() = %v, want only %q", ids, newest.ID)
	}
	for _, entry := range []Entry{tooOld, alsoOld, middle} {
		for _, name := range []string{entry.File, entry.Thumb} {
			if _, err := os.Stat(filepath.Join(s.Dir(), name)); !os.IsNotExist(err) {
				t.Fatalf("pruned entry file %q still exists (err = %v)", name, err)
			}
		}
	}
	if _, err := Open(s.Dir()); err != nil {
		t.Fatalf("Open() after combined Prune error = %v", err)
	}
}

func TestPruneHugeRetentionDaysDoesNotOverflow(t *testing.T) {
	s := testStore(t)
	// Older than a normal retention window but far younger than the internal
	// clamp, so any overflow/wrap in the duration math would prune it.
	ancient := mustSave(t, s, testPNG(t, 32, 32), time.Now().Add(-10*365*24*time.Hour))
	for _, days := range []int{maxRetentionDays + 1, 1_000_000, math.MaxInt32, math.MaxInt} {
		removed, err := s.Prune(days, 0)
		if err != nil || removed != 0 {
			t.Fatalf("Prune(%d, 0) = (%d, %v), want (0, nil): an absurd retention must not wrap and prune", days, removed, err)
		}
	}
	if ids := listedIDs(s); len(ids) != 1 || ids[0] != ancient.ID {
		t.Fatalf("List() after huge retention = %v, want %q", ids, ancient.ID)
	}
}
