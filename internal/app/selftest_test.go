package app

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Run the real result handler in a child test process. It must turn failures
// into OS exit codes without initializing MyGo, a window, or the clipboard.
func TestSelftestProcessResult(t *testing.T) {
	if scenario := os.Getenv("PINSTILL_TEST_RESULT"); scenario != "" {
		selftestRun("synthetic", func() {
			switch scenario {
			case "error":
				selftestFail("clipboard error %v", errors.New("synthetic failure"))
			case "assertion":
				selftestRequire(false, "pin count mismatch")
			case "panic":
				panic("unexpected synthetic failure")
			case "artifact":
				selftestArtifact("../escape.png", []byte("synthetic"))
			}
			selftestExit(0)
		}, os.Exit)
		return
	}
	for _, scenario := range []string{"success", "error", "assertion", "panic", "artifact"} {
		t.Run(scenario, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestSelftestProcessResult$")
			cmd.Env = append(os.Environ(), "PINSTILL_TEST_RESULT="+scenario)
			output, err := cmd.CombinedOutput()
			if scenario == "success" {
				if err != nil || !bytes.Contains(output, []byte("SELFTEST synthetic: checks=ok")) {
					t.Fatalf("success: %v: %s", err, output)
				}
			} else if err == nil || bytes.Contains(output, []byte("checks=ok")) || !bytes.Contains(output, []byte("checks=FAILED")) {
				t.Fatalf("failure must exit nonzero without success marker: %v: %s", err, output)
			}
		})
	}
}

func isolatedSelftestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".pinstill-selftest"), []byte("pinstill-selftest\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSelftestDataDirectoryValidation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, dir := range []string{"", ".", "/", home, filepath.Join(home, "Library", "Application Support", "Pinstill"), filepath.Join(home, "Library", "Caches", "dev.echovic.pinstill")} {
		if filepath.IsAbs(dir) && dir != "/" {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, ".pinstill-selftest"), []byte("pinstill-selftest\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := ValidateSelftestDataDir(dir); err == nil {
			t.Errorf("accepted unsafe directory %q", dir)
		}
	}
	if err := ValidateSelftestDataDir(t.TempDir()); err == nil {
		t.Fatal("accepted missing isolation marker")
	}
	if err := ValidateSelftestDataDir(isolatedSelftestDir(t)); err != nil {
		t.Fatal(err)
	}
}

func TestSelftestArtifactsStayInIsolatedDirectory(t *testing.T) {
	dir := isolatedSelftestDir(t)
	t.Setenv("PINSTILL_DATA_DIR", dir)
	path, err := writeSelftestArtifact("synthetic.png", []byte("synthetic"))
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "artifacts", "synthetic.png") {
		t.Fatalf("wrong artifact location %q", path)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "synthetic" {
		t.Fatalf("artifact: %q %v", b, err)
	}
	for _, name := range []string{"", ".", "..", "../outside.png", "/tmp/outside.png", `..\outside.png`, "nested/file.png"} {
		if _, err := writeSelftestArtifact(name, []byte("bad")); err == nil {
			t.Errorf("accepted unsafe basename %q", name)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "artifacts", "link.png")); err != nil {
		t.Fatal(err)
	}
	_, _ = writeSelftestArtifact("link.png", []byte("changed"))
	if b, _ := os.ReadFile(outside); string(b) != "keep" {
		t.Fatal("followed an artifact symlink")
	}
	other := isolatedSelftestDir(t)
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(other, "artifacts")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PINSTILL_DATA_DIR", other)
	if _, err := writeSelftestArtifact("outside.png", []byte("bad")); err == nil {
		t.Fatal("accepted artifact directory symlink")
	}
}

func TestSelftestArtifactWriteFailureIsReported(t *testing.T) {
	dir := isolatedSelftestDir(t)
	t.Setenv("PINSTILL_DATA_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "artifacts"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := writeSelftestArtifact("test.png", nil); err == nil || !strings.Contains(err.Error(), "artifacts") {
		t.Fatalf("write failure lost: %v", err)
	}
}
