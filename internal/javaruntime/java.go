package javaruntime

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Runtime struct {
	Path    string `json:"Path"`
	Version string `json:"Version"`
	Major   int64  `json:"Major"`
}

var versionPattern = regexp.MustCompile(`(?:openjdk|java) version "([^"]+)"`)

// Candidates are chosen by the host operator, never arbitrary API paths.
func Candidates() []string {
	var paths []string
	if p, err := exec.LookPath("java"); err == nil {
		paths = append(paths, p)
	}
	name := "java"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if home := os.Getenv("JAVA_HOME"); home != "" {
		paths = append(paths, filepath.Join(home, "bin", name))
	}
	paths = append(paths, filepath.SplitList(os.Getenv("CHUNKD_JAVA_PATHS"))...)
	seen := map[string]bool{}
	result := []string{}
	for _, p := range paths {
		if p == "" {
			continue
		}
		absolute, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			continue
		}
		if !seen[resolved] {
			seen[resolved] = true
			result = append(result, resolved)
		}
	}
	return result
}

func Detect(ctx context.Context) []Runtime {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result := []Runtime{}
	for _, path := range Candidates() {
		if ctx.Err() != nil {
			break
		}
		if r, err := Inspect(ctx, path); err == nil {
			result = append(result, r)
		}
	}
	return result
}

func Inspect(ctx context.Context, path string) (Runtime, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var out limitedBuffer
	cmd := exec.CommandContext(ctx, path, "-version")
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return Runtime{}, fmt.Errorf("Java version check failed: %w", err)
	}
	match := versionPattern.FindStringSubmatch(out.String())
	if len(match) != 2 {
		return Runtime{}, fmt.Errorf("unrecognized Java version output")
	}
	part := strings.Split(match[1], ".")[0]
	if part == "1" {
		parts := strings.Split(match[1], ".")
		if len(parts) > 1 {
			part = parts[1]
		}
	}
	part = strings.Split(part, "-")[0]
	major, err := strconv.ParseInt(part, 10, 64)
	if err != nil {
		return Runtime{}, fmt.Errorf("unrecognized Java major version")
	}
	return Runtime{Path: path, Version: match[1], Major: major}, nil
}

func Validate(ctx context.Context, path string, required int64) (Runtime, error) {
	allowed := false
	for _, candidate := range Candidates() {
		if candidate == path {
			allowed = true
			break
		}
	}
	if !allowed {
		return Runtime{}, fmt.Errorf("select a detected Java runtime; configure additional paths with CHUNKD_JAVA_PATHS")
	}
	r, err := Inspect(ctx, path)
	if err != nil {
		return r, err
	}
	if r.Major < required {
		return r, fmt.Errorf("Java %d or newer is required; selected Java %d", required, r.Major)
	}
	return r, nil
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := 64*1024 - b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}
