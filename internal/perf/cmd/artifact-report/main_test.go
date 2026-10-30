package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWithoutBuildReportsNotBuilt(t *testing.T) {
	var out bytes.Buffer
	if err := run(&out, filepath.Join(t.TempDir(), "build"), true); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "未构建") {
		t.Fatalf("output does not say 未构建:\n%s", out.String())
	}
}

func TestRunMeasuresSyntheticTargetWithoutDependencies(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "darwin-arm64")
	if err := os.MkdirAll(filepath.Join(target, "BiU.app", "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, "BiU.app", "Contents", "Resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path string, n int) {
		t.Helper()
		if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(target, "BiU.app", "Contents", "MacOS", "BiU"), 2048)
	write(filepath.Join(target, "BiU.app", "Contents", "Resources", "THIRD-PARTY-NOTICES.txt"), 512)
	write(filepath.Join(target, "biu-0.1.0-arm64.dmg"), 1024)

	var out bytes.Buffer
	if err := run(&out, dir, true); err != nil {
		t.Fatalf("run: %v", err)
	}
	text := out.String()
	for _, want := range []string{"darwin-arm64", "主可执行文件", "Resources", "安装包/压缩包", "biu-0.1.0-arm64.dmg"} {
		if !strings.Contains(text, want) {
			t.Errorf("output is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "未构建") {
		t.Errorf("a measured target still reads as 未构建:\n%s", text)
	}
}
