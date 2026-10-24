// Package testutil builds a deterministic native-process fixture, not a Minecraft
// distribution. It never downloads software or accepts an upstream EULA.
package testutil

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func FakeJava(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	name := "java"
	goName := "go"
	if runtime.GOOS == "windows" {
		name += ".exe"
		goName += ".exe"
	}
	path := filepath.Join(t.TempDir(), name)
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", goName), "build", "-o", path, filepath.Join(filepath.Dir(file), "..", "fixtures", "fakejava", "main.go"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build process fixture: %v %s", err, output)
	}
	return path
}
