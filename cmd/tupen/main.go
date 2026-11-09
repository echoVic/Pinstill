// Tupen is the desktop shell for the screenshot and annotation workflow.
package main

import (
	"log"
	"os"
	"strings"

	"github.com/echoVic/Tupen/internal/app"
	"github.com/egoist/mygo"
)

func main() {
	// The README screenshots must never read the user's captures: the
	// showcase selftest refuses to run outside an isolated data directory.
	if os.Getenv("TUPEN_SELFTEST") == "showcase" && strings.TrimSpace(os.Getenv("TUPEN_DATA_DIR")) == "" {
		log.Fatal("TUPEN_SELFTEST=showcase needs TUPEN_DATA_DIR set to an isolated directory")
	}
	// A screenshot utility must have one instance: hotkeys, the tray icon
	// and the pin windows all belong to it.
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	mygo.App.SetName("Tupen")
	mygo.App.SetVersion("0.1.0")

	tupen := app.New()
	tupen.Bind()
	mygo.App.OnSecondInstance(func(args []string, workingDir string) {
		tupen.ShowMain()
	})
	mygo.App.WhenReady(tupen.Start)
	// Closing the last window only puts Tupen to sleep: the menu bar icon
	// and the global shortcuts keep working until the user quits.
	mygo.App.OnWindowAllClosed(func() {})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
