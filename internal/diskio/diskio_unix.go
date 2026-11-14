//go:build !windows

package diskio

import (
	"errors"
	"os"
	"syscall"
)

func lockFile(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}
func unlockFile(f *os.File) error       { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
func replaceFile(from, to string) error { return os.Rename(from, to) }

// SyncDir makes completed directory-entry updates durable.
func SyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
