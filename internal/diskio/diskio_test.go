package diskio

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteReportsCommittedDirectorySyncFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.json")
	failure := errors.New("directory sync failed")
	calls := 0
	err := writeFileAtomic(path, []byte("new index"), 0o600, func(got string) error {
		calls++
		if got != dir {
			t.Errorf("synced %q, want %q", got, dir)
		}
		return failure
	})
	if calls != 1 || !errors.Is(err, failure) || !Committed(err) {
		t.Fatalf("sync result: calls=%d err=%v committed=%v", calls, err, Committed(err))
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new index" {
		t.Fatalf("committed file = %q, %v", data, err)
	}
}
