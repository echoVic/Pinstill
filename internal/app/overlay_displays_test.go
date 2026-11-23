package app

import (
	"reflect"
	"testing"

	"github.com/egoist/mygo"
)

func TestOverlayInfoProjectsDesktopToEachPane(t *testing.T) {
	primary := overlaySurface{window: new(mygo.Window), bounds: mygo.Rectangle{Width: 100, Height: 80}, display: mygo.Display{ScaleFactor: 2}, session: 17, cursor: mygo.Point{X: 10, Y: 20}}
	secondary := overlaySurface{window: new(mygo.Window), bounds: mygo.Rectangle{X: -40, Y: -20, Width: 40, Height: 100}, display: mygo.Display{ScaleFactor: 1}, session: 17, cursor: primary.cursor}
	app := &App{overlay: primary.window, overlaySurfaces: []overlaySurface{primary, secondary}}
	info := app.overlayInfoFor(primary.window)
	if !info.HasCursor || info.Desktop == nil || *info.Desktop != (Rect{X: -40, Y: -20, Width: 140, Height: 100}) {
		t.Fatalf("primary overlay layout = %+v", info)
	}
	info = app.overlayInfoFor(secondary.window)
	want := []OverlayDisplay{{Rect: Rect{X: 40, Y: 20, Width: 100, Height: 80}, Scale: 2}, {Rect: Rect{Width: 40, Height: 100}, Scale: 1}}
	if info.HasCursor || info.Session != 17 || info.DisplayX != -40 || info.DisplayY != -20 || !reflect.DeepEqual(info.Displays, want) {
		t.Fatalf("secondary overlay layout = %+v", info)
	}
	selection := Rect{X: -30, Y: 15, Width: 40, Height: 20}
	if got := projectOverlayRect(selection, primary.bounds, secondary.bounds); got != (Rect{X: 10, Y: 35, Width: 40, Height: 20}) {
		t.Fatalf("peer selection = %+v", got)
	}
}
