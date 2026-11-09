// Tupen is the desktop shell for the screenshot and annotation workflow.
package main

import (
	"log"

	"github.com/echoVic/Tupen/internal/capture"
	"github.com/egoist/mygo"
)

func main() {
	app := mygo.App
	app.SetName("Tupen")
	mygo.Bind(capture.CaptureService{})
	app.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Label: "File", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleQuit},
		}},
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleWindowMenu},
	}))

	app.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:           "Tupen",
			Width:           1360,
			Height:          880,
			MinWidth:        760,
			MinHeight:       540,
			TitleBarStyle:   mygo.TitleBarHiddenInset,
			TitleBarHeight:  52,
			BackgroundColor: "#141412",
			URL:             "/",
		})
	})

	// A screenshot utility should stay available after its editor window closes.
	app.OnWindowAllClosed(func() {})
	app.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			mygo.NewWindow(mygo.WindowOptions{
				Title:           "Tupen",
				Width:           1360,
				Height:          880,
				MinWidth:        760,
				MinHeight:       540,
				TitleBarStyle:   mygo.TitleBarHiddenInset,
				TitleBarHeight:  52,
				BackgroundColor: "#141412",
				URL:             "/",
			})
		}
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
