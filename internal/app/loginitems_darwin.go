//go:build darwin

package app

import (
	"fmt"
	"os/exec"
)

// loginItemsSettingsURL is the Settings deep link for General > Login Items
// (macOS 13+). Pinstill targets Ventura and later, where login items live in
// their own pane.
const loginItemsSettingsURL = "x-apple.systempreferences:com.apple.LoginItems-Settings.extension"

// openLoginItemsSettings opens System Settings > General > Login Items, where
// the user can see, add or remove Pinstill's login item.
func openLoginItemsSettings() error {
	if err := exec.Command("/usr/bin/open", loginItemsSettingsURL).Run(); err != nil {
		return fmt.Errorf("打开登录项设置失败: %w", err)
	}
	return nil
}
