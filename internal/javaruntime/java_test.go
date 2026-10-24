package javaruntime

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/NLstn/chunkd/internal/testutil"
)

func TestDetectAndValidateTrustedRuntime(t *testing.T) {
	path := testutil.FakeJava(t)
	t.Setenv("CHUNKD_JAVA_PATHS", path)
	r, err := Validate(context.Background(), path, 25)
	if err != nil || r.Major != 25 || r.Version != "25.0.1" {
		t.Fatalf("runtime=%+v err=%v", r, err)
	}
	if _, err := Validate(context.Background(), path, 26); err == nil {
		t.Fatal("accepted incompatible Java")
	}
	if _, err := Validate(context.Background(), filepath.Join(t.TempDir(), "untrusted-java"), 21); err == nil {
		t.Fatal("accepted arbitrary executable")
	}
	found := false
	for _, r := range Detect(context.Background()) {
		if r.Path == path {
			found = true
		}
	}
	if !found {
		t.Fatal("configured runtime not detected")
	}
}
