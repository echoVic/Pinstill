package clothesline

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/echoVic/Pinstill/internal/imagebudget"
)

func TestLinePersistsOrderAndVisibilityWithoutModifyingSources(t *testing.T) {
	dir := t.TempDir()
	line, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	fixture := image.NewNRGBA(image.Rect(0, 0, 640, 400))
	fixture.SetNRGBA(100, 100, color.NRGBA{R: 45, G: 140, A: 255})
	original, err := imagebudget.EncodePNG(fixture)
	if err != nil {
		t.Fatal(err)
	}
	first, err := line.Add(original, "第一张")
	if err != nil {
		t.Fatal(err)
	}
	second, err := line.Add(original, "第二张")
	if err != nil {
		t.Fatal(err)
	}
	firstID, secondID := first.Items[0].ID, second.Items[1].ID
	if _, err := line.Move(firstID, "missing"); err == nil {
		t.Fatal("a missing reorder target must not change the line")
	}
	if _, err := line.Move(secondID, firstID); err != nil {
		t.Fatal(err)
	}
	if _, err := line.SetVisible(false); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := reopened.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Visible || snapshot.Revision != 4 || !slices.EqualFunc(snapshot.Items, []Item{{ID: secondID}, {ID: firstID}}, func(got, want Item) bool { return got.ID == want.ID }) {
		t.Fatalf("incorrect restored line: %+v", snapshot)
	}
	thumb, err := os.ReadFile(filepath.Join(dir, firstID+".thumb.png"))
	if err != nil {
		t.Fatal(err)
	}
	size, err := png.DecodeConfig(bytes.NewReader(thumb))
	if err != nil || size.Width != 320 || size.Height != 200 {
		t.Fatalf("thumbnail not scaled: %+v, %v", size, err)
	}
	if _, err := reopened.Remove(secondID); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Read(secondID); err == nil {
		t.Fatal("removed item is still readable")
	}
	got, err := reopened.Read(firstID)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("removing one item changed the other original: %v", err)
	}
}

func TestLineRejectsInvalidImagesAndPreservesCorruptIndex(t *testing.T) {
	dir := t.TempDir()
	line, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := line.Add([]byte("not an image"), "bad"); err == nil {
		t.Fatal("invalid image was accepted")
	}
	index := filepath.Join(dir, "line.json")
	broken := []byte("{\"version\":1,\"items\":[{\"id\":\"../outside\"}]}")
	if err := os.WriteFile(index, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := line.SetVisible(true); err == nil {
		t.Fatal("invalid persisted state was accepted")
	}
	remaining, err := os.ReadFile(index)
	if err != nil || !bytes.Equal(remaining, broken) {
		t.Fatalf("corrupt index was overwritten: %v", err)
	}
}
