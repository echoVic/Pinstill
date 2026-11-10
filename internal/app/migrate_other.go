//go:build !darwin

package app

// MigrateLegacyData is a no-op outside macOS: the other platforms derive their
// data directories from the app's own directories, which the first run of the
// renamed app creates afresh.
func MigrateLegacyData() {}
