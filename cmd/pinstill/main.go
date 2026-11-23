// Pinstill is the desktop shell for the screenshot and annotation workflow.
package main

import (
	"log"
	"os"
	"time"

	"github.com/echoVic/Pinstill/internal/app"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
)

func main() {
	// Native tests can delete history and pins. Validate isolation before the
	// single-instance lock, migration, MyGo initialization, or storage setup.
	selftest := os.Getenv("PINSTILL_SELFTEST") != ""
	if selftest {
		if err := app.ValidateSelftestDataDir(os.Getenv("PINSTILL_DATA_DIR")); err != nil {
			log.Fatalf("PINSTILL_SELFTEST needs an isolated PINSTILL_DATA_DIR: %v", err)
		}
	}
	// A screenshot utility must have one instance: hotkeys, the tray icon
	// and the pin windows all belong to it.
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	// Carry captures, pins and settings over from the pre-rename builds.
	app.MigrateLegacyData()
	mygo.App.SetName("Pinstill")
	if !selftest {
		mygo.Use(updater.New(updater.Options{Interval: 4 * time.Hour}))
	}

	pinstill := app.New()
	pinstill.Bind()
	mygo.App.OnSecondInstance(func(args []string, workingDir string) {
		pinstill.ShowMain()
	})
	mygo.App.WhenReady(pinstill.Start)
	// Closing the last window only puts Pinstill to sleep: the menu bar icon
	// and the global shortcuts keep working until the user quits.
	mygo.App.OnWindowAllClosed(func() {})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
