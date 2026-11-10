//go:build darwin

package app

import (
	"os"
	"path/filepath"
)

// legacyPaths lists data directories written by the pre-rename builds and
// where they move to. macOS derives these paths from the bundle identifier, so
// the rename changes them and the old data has to be carried over once.
var legacyPaths = [][2]string{
	{"Application Support/Tupen", "Application Support/Pinstill"},
	{"Caches/Tupen", "Caches/Pinstill"},
	{"WebKit/dev.echovic.tupen", "WebKit/dev.echovic.pinstill"},
	{"Caches/dev.echovic.tupen", "Caches/dev.echovic.pinstill"},
}

// MigrateLegacyData moves data written under the old bundle identity into the
// new one. It never overwrites an existing directory and stays silent when
// there is nothing to move.
func MigrateLegacyData() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	library := filepath.Join(home, "Library")
	for _, pair := range legacyPaths {
		src := filepath.Join(library, pair[0])
		dst := filepath.Join(library, pair[1])
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			continue
		}
		_ = os.Rename(src, dst)
	}
}
