//go:build !darwin

package app

import "errors"

// errLoginItemsUnsupported is returned where there is no macOS-style login
// items settings pane to open.
var errLoginItemsUnsupported = errors.New("登录项设置: 当前平台不支持")

// openLoginItemsSettings is not available outside macOS.
func openLoginItemsSettings() error { return errLoginItemsUnsupported }
