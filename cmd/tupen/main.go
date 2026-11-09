// Tupen is the desktop shell for the screenshot and annotation workflow.
package main

import (
	"log"

	"github.com/echoVic/Tupen/internal/app"
	"github.com/egoist/mygo"
)

func main() {
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
