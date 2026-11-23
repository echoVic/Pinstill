package app

import (
	"testing"

	"github.com/echoVic/Pinstill/internal/pin"
	"github.com/egoist/mygo"
)

func addManagedTestPin(test *testing.T, application *App, id string) {
	test.Helper()
	file, err := application.pinStore.SaveImage(id, gnPNGData(test, 40, 20))
	if err != nil {
		test.Fatal(err)
	}
	state, err := application.pinStore.Upsert(pin.State{ID: id, File: file, Width: 40, Height: 20})
	if err != nil {
		test.Fatal(err)
	}
	application.pinStates[id] = state
}

func TestPinManagerPersistsGroupControlsAndRetainsOriginal(test *testing.T) {
	application, initial := pinPersistenceApp(test)
	for _, id := range []string{"paired", "unrelated"} {
		addManagedTestPin(test, application, id)
	}
	for _, id := range []string{initial.ID, "paired"} {
		if _, err := application.managePin(id, "group", "  设计  审阅  "); err != nil {
			test.Fatal(err)
		}
		if _, err := application.managePin(id, "lock", ""); err != nil {
			test.Fatal(err)
		}
	}
	if err := application.managePinGroup("设计 审阅", "rename", "今天"); err != nil {
		test.Fatal(err)
	}
	if err := application.managePinGroup("今天", "hide", ""); err != nil {
		test.Fatal(err)
	}
	if err := application.managePinGroup("今天", "unlock", ""); err != nil {
		test.Fatal(err)
	}
	reopened, err := pin.Open(application.pinStore.Dir())
	if err != nil {
		test.Fatal(err)
	}
	states, err := reopened.PrepareRestore(10)
	if err != nil || len(states) != 3 {
		test.Fatalf("restoring managed pins = %v, %v", states, err)
	}
	for _, state := range states {
		grouped := state.ID != "unrelated"
		if (state.Group == "今天") != grouped || state.Hidden != grouped || state.ClickThrough || state.Archived {
			test.Errorf("unexpected persisted pin: %+v", state)
		}
		if _, err := reopened.ReadImage(state); err != nil {
			test.Fatalf("managed pin lost its image: %v", err)
		}
	}
}

func TestPinManagerRotationUpdatesPresentationAndDimensions(test *testing.T) {
	application, _ := pinPersistenceApp(test)
	addManagedTestPin(test, application, "wide")
	rotated, err := application.managePin("wide", "rotate-right", "")
	if err != nil || rotated.Rotation != 1 || rotated.Width != 20 || rotated.Height != 40 {
		test.Fatalf("rotating landscape = %+v, %v", rotated, err)
	}
	flipped, err := application.managePin("wide", "flip-horizontal", "")
	if err != nil || !flipped.FlipY || flipped.FlipX {
		test.Fatalf("horizontal flip after a 90-degree rotation = %+v, %v", flipped, err)
	}
	if err := application.updatePinState("wide", PinWindowState{Opacity: 90}); err != nil {
		test.Fatal(err)
	}
	stored, ok := application.pinState("wide")
	if !ok || stored.Rotation != 1 || !stored.FlipY || stored.Width != 20 || stored.Opacity != 90 {
		test.Fatalf("geometry and presentation persistence crossed: %+v", stored)
	}
}

func TestPinManagerRetainsGeometryClampedAtRestore(test *testing.T) {
	application, initial := pinPersistenceApp(test)
	display := testDisplay(testRect(900, 0, 1000, 700), testRect(900, 20, 1000, 680))
	clamped := clampPinToDisplays(initial, []mygo.Display{display})
	application.pinStates[initial.ID] = clamped
	if _, err := application.managePin(initial.ID, "group", "设计"); err != nil {
		test.Fatal(err)
	}
	stored := application.pinStore.List()[0]
	if stored.X != clamped.X || stored.Y != clamped.Y || stored.Group != "设计" {
		test.Fatalf("manager lost the restored window position: got %+v, want (%d, %d)", stored, clamped.X, clamped.Y)
	}
}

func TestPinMutationsPersistGeometryAndOpacityBeforeApplying(test *testing.T) {
	application, initial := pinPersistenceApp(test)
	assertApplied := func(applied pin.State) {
		test.Helper()
		reopened, err := pin.Open(application.pinStore.Dir())
		if err != nil {
			test.Fatal(err)
		}
		states := reopened.List()
		if len(states) != 1 {
			test.Fatalf("expected one persisted pin, got %d", len(states))
		}
		persisted := states[0]
		persisted.CreatedAt = persisted.CreatedAt.UTC()
		applied.CreatedAt = applied.CreatedAt.UTC()
		if persisted != applied {
			test.Fatalf("native apply saw a different state on disk: apply=%+v store=%+v", applied, persisted)
		}
	}
	if err := application.changePin(initial.ID, func(state *pin.State) {
		state.Width, state.Height = 210, 120
	}, assertApplied); err != nil {
		test.Fatal(err)
	}
	if err := application.changePin(initial.ID, func(state *pin.State) {
		state.Opacity = 80
	}, assertApplied); err != nil {
		test.Fatal(err)
	}
}

func TestGroupMutationKeepsAllPinsWhenWriteCannotCommit(test *testing.T) {
	application, initial := pinPersistenceApp(test)
	addManagedTestPin(test, application, "paired")
	failPinIndex(test, application)
	if err := application.managePinGroup("", "hide", ""); err == nil {
		test.Fatal("group hide reported success after a storage failure")
	}
	for _, id := range []string{initial.ID, "paired"} {
		if state, _ := application.pinState(id); state.Hidden {
			test.Errorf("pin %s was hidden after the failed write", id)
		}
	}
}
