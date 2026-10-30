package perf

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSummarizeBuildWithoutArtifactsSaysNotBuilt is the "no build yet" case: the
// entry point must say 未构建 and exit successfully, not crash on a missing
// build/ directory.
func TestSummarizeBuildWithoutArtifactsSaysNotBuilt(t *testing.T) {
	summary, err := SummarizeBuild(filepath.Join(t.TempDir(), "build"))
	if err != nil {
		t.Fatalf("a missing build directory reported an error: %v", err)
	}
	if summary.Built {
		t.Fatalf("summary claims a build exists: %+v", summary)
	}
	if !strings.Contains(summary.Note, "未构建") {
		t.Fatalf("note = %q, want it to say 未构建", summary.Note)
	}
	var out bytes.Buffer
	if err := FormatBuildSummary(&out, summary); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "未构建") {
		t.Fatalf("rendered summary does not say 未构建:\n%s", out.String())
	}
}

func TestSummarizeBuildIgnoresEmptyAndStagingDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".staging-darwin-arm64-123"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "darwin-arm64"), 0o755); err != nil {
		t.Fatal(err)
	}
	summary, err := SummarizeBuild(dir)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Built {
		t.Fatalf("an empty build directory counts as built: %+v", summary)
	}
}

// TestSummarizeBuildAttributesSyntheticTarget is the end-to-end measurement:
// a synthetic darwin target with an app bundle and a dmg next to it, plus a
// windows target with an exe, copied resources and an installer. The summary
// must separate the main executable, resources, other bundle files, the
// installed total and the compressed packages.
func TestSummarizeBuildAttributesSyntheticTargets(t *testing.T) {
	dir := t.TempDir()

	app := filepath.Join(dir, "darwin-arm64", "BiU.app")
	writeSized(t, filepath.Join(app, "Contents", "MacOS", "BiU"), 18_000_000)
	writeSized(t, filepath.Join(app, "Contents", "Resources", "AppIcon.icns"), 600_000)
	writeSized(t, filepath.Join(app, "Contents", "Resources", "licenses", "THIRD-PARTY-NOTICES.txt"), 40_000)
	writeSized(t, filepath.Join(app, "Contents", "Info.plist"), 2_000)
	writeSized(t, filepath.Join(app, "Contents", "PkgInfo"), 8)
	writeSized(t, filepath.Join(dir, "darwin-arm64", "biu-0.1.0-arm64.dmg"), 8_500_000)
	writeSized(t, filepath.Join(dir, "darwin-arm64", "install.sh"), 4_000)
	writeSized(t, filepath.Join(dir, "darwin-arm64", "update-darwin-arm64.json"), 500)

	win := filepath.Join(dir, "windows-amd64")
	writeSized(t, filepath.Join(win, "BiU.exe"), 19_000_000)
	writeSized(t, filepath.Join(win, "licenses", "THIRD-PARTY-NOTICES.txt"), 40_000)
	writeSized(t, filepath.Join(win, "icon.png"), 700_000)
	writeSized(t, filepath.Join(win, "biu-setup-0.1.0-amd64.exe"), 9_000_000)

	summary, err := SummarizeBuild(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !summary.Built {
		t.Fatalf("summary says not built: %+v", summary)
	}
	if len(summary.Targets) != 2 {
		t.Fatalf("targets = %+v, want two", summary.Targets)
	}
	darwin := summary.Targets[0]
	if darwin.Name != "darwin-arm64" {
		t.Fatalf("first target = %q", darwin.Name)
	}
	if darwin.MainBinary != 18_000_000 {
		t.Errorf("main binary = %d", darwin.MainBinary)
	}
	if darwin.Resources != 640_000 {
		t.Errorf("resources = %d", darwin.Resources)
	}
	if darwin.Other != 6_508 { // Info.plist + PkgInfo + install.sh + update json
		t.Errorf("other = %d", darwin.Other)
	}
	if darwin.AppBytes != 18_642_008 {
		t.Errorf("app bytes = %d", darwin.AppBytes)
	}
	if darwin.Packed != 8_500_000 {
		t.Errorf("packed = %d", darwin.Packed)
	}
	if darwin.Installed != 18_646_508 { // app + install.sh + update json
		t.Errorf("installed = %d", darwin.Installed)
	}

	windows := summary.Targets[1]
	if windows.Name != "windows-amd64" {
		t.Fatalf("second target = %q", windows.Name)
	}
	if windows.AppBytes != 0 {
		t.Errorf("windows app bytes = %d, want 0 (no bundle)", windows.AppBytes)
	}
	if windows.MainBinary != 19_000_000 {
		t.Errorf("windows main binary = %d", windows.MainBinary)
	}
	if windows.Resources != 740_000 {
		t.Errorf("windows resources = %d", windows.Resources)
	}
	if windows.Packed != 9_000_000 {
		t.Errorf("windows packed = %d", windows.Packed)
	}
	if windows.Installed != 19_740_000 {
		t.Errorf("windows installed = %d", windows.Installed)
	}

	summary.Dependencies = []DependencyGroup{
		{Module: "github.com/Eyevinn/mp4ff", Version: "v0.60.0", Targets: []string{"darwin-arm64", "windows-amd64"}},
	}
	var out bytes.Buffer
	if err := FormatBuildSummary(&out, summary); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"darwin-arm64", "windows-amd64",
		"主可执行文件", "Resources", "其他", "安装包/压缩包",
		"BiU.app", "biu-0.1.0-arm64.dmg",
		"github.com/Eyevinn/mp4ff",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("rendered summary is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "未构建") {
		t.Errorf("built summary still says 未构建:\n%s", text)
	}
}

func TestFormatBytesUsesReadableUnits(t *testing.T) {
	cases := map[int64]string{
		0:          "0 B",
		1000:       "1000 B",
		5120:       "5 KiB",
		1 << 20:    "1.0 MiB",
		3 << 20:    "3.0 MiB",
		18_642_008: "17.8 MiB",
		8_500_000:  "8.1 MiB",
	}
	for n, want := range cases {
		if got := FormatBytes(n); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

// TestFormatBuildSummaryShortensMachinePaths keeps absolute machine paths out of
// the rendered report: it is pasted into CI summaries and issue comments.
func TestFormatBuildSummaryShortensMachinePaths(t *testing.T) {
	abs, err := filepath.Abs("build")
	if err != nil {
		t.Fatal(err)
	}
	summary, err := SummarizeBuild(abs)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := FormatBuildSummary(&out, summary); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), abs) {
		t.Fatalf("rendered report leaks the absolute path %q:\n%s", abs, out.String())
	}
}
