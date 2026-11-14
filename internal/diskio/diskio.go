// Package diskio coordinates persistent directories across handles and processes.
package diskio

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LockDir acquires the directory's exclusive advisory lock. All cooperating
// readers/writers use the same permanent lock file; it must never be unlinked.
func LockDir(dir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, ".pinstill.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		f.Close()
		return nil, err
	}
	return func() { _ = unlockFile(f); _ = f.Close() }, nil
}

// ReadFile bounds allocation even if the file grows during the read.
func ReadFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("file exceeds limit or is not regular: %s", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds %d bytes", limit)
	}
	return data, nil
}

type committedError struct{ error }

func (e *committedError) Unwrap() error { return e.error }

// MarkCommitted preserves the fact that an operation completed its visible
// rename before a later durability step failed. It is useful to adapters that
// perform an atomic write themselves and need to expose the same contract as
// WriteFileAtomic.
func MarkCommitted(err error) error {
	if err == nil {
		err = errors.New("operation committed")
	}
	return &committedError{err}
}

// Committed reports an error after rename: the new data is visible but its
// crash durability could not be confirmed. Callers must not roll it back by
// deleting referenced image files.
func Committed(err error) bool { var target *committedError; return errors.As(err, &target) }

// WriteFileAtomic writes, syncs, renames and syncs the containing directory.
// The caller must hold LockDir across its full read/modify/write transaction.
func WriteFileAtomic(path string, data []byte, mode os.FileMode) error {
	return writeFileAtomic(path, data, mode, SyncDir)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode, syncDir func(string) error) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if err := temp.Chmod(mode); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := replaceFile(temp.Name(), path); err != nil {
		return err
	}
	if err := syncDir(dir); err != nil {
		return &committedError{err}
	}
	return nil
}
