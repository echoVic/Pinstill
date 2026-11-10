// Pinstill is the desktop shell for the screenshot and annotation workflow.
package main

import (
	"log"
	"os"
	"strings"

	"github.com/echoVic/Pinstill/internal/app"
	"github.com/egoist/mygo"
)

func main() {
	// Carry captures, pins and settings over from the pre-rename builds.
	app.MigrateLegacyData()
	// The README screenshots must never read the user's captures: the
	// showcase selftest refuses to run outside an isolated data directory.
	if os.Getenv("PINSTILL_SELFTEST") == "showcase" && strings.TrimSpace(os.Getenv("PINSTILL_DATA_DIR")) == "" {
		log.Fatal("PINSTILL_SELFTEST=showcase needs PINSTILL_DATA_DIR set to an isolated directory")
	}
	// A screenshot utility must have one instance: hotkeys, the tray icon
	// and the pin windows all belong to it.
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	mygo.App.SetName("Pinstill")
	mygo.App.SetVersion("0.1.0")

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
