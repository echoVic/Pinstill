package diskio

import (
	"os"
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var lockFileEx = kernel32.NewProc("LockFileEx")
var unlockFileEx = kernel32.NewProc("UnlockFileEx")
var moveFileEx = kernel32.NewProc("MoveFileExW")

func lockFile(f *os.File) error {
	var overlapped syscall.Overlapped
	ok, _, err := lockFileEx.Call(f.Fd(), 2, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if ok == 0 {
		return err
	}
	return nil
}
func unlockFile(f *os.File) error {
	var overlapped syscall.Overlapped
	ok, _, err := unlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if ok == 0 {
		return err
	}
	return nil
}
func replaceFile(from, to string) error {
	a, err := syscall.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	b, err := syscall.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	// REPLACE_EXISTING | WRITE_THROUGH is Windows' durable rename boundary.
	ok, _, err := moveFileEx.Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)), 1|8)
	if ok == 0 {
		return err
	}
	return nil
}

// Windows does not support fsync on directory handles; replaceFile requests a
// write-through rename. Individual files have already been flushed.
func SyncDir(string) error { return nil }
