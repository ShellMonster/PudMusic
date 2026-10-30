package perf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyArtifactFileRecognisesBundleLayout(t *testing.T) {
	cases := []struct {
		name string
		rel  string
		want LayerKind
	}{
		{"app executable", "BiU.app/Contents/MacOS/BiU", LayerMainBinary},
		{"app resources", "BiU.app/Contents/Resources/AppIcon.icns", LayerResources},
		{"nested helper in resources", "BiU.app/Contents/Resources/Helper.app/Contents/MacOS/Helper", LayerResources},
		{"localization", "BiU.app/Contents/Resources/zh-Hans.lproj/InfoPlist.strings", LayerResources},
		{"plist is not resources", "BiU.app/Contents/Info.plist", LayerOther},
		{"code signature is not resources", "BiU.app/Contents/_CodeSignature/CodeResources", LayerOther},

		{"windows executable", "biu.exe", LayerMainBinary},
		{"windows installer", "biu-setup-0.1.0-amd64.exe", LayerPackages},
		{"dmg", "biu-0.1.0-arm64.dmg", LayerPackages},
		{"tar.gz", "biu-0.1.0-darwin-arm64.tar.gz", LayerPackages},
		{"delta", "biu-0.1.0-arm64.delta", LayerPackages},
		{"install script", "install.sh", LayerOther},
		{"update metadata", "update-darwin-arm64.json", LayerOther},
		{"flat resource", "licenses/THIRD-PARTY-NOTICES.txt", LayerResources},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyArtifactFile(c.rel); got != c.want {
				t.Errorf("ClassifyArtifactFile(%q) = %q, want %q", c.rel, got, c.want)
			}
		})
	}
}

// TestBreakdownArtifactIsTheESliceOfAnAppBundle is the end-to-end shape test:
// a synthetic .app with an executable, resources, bundle metadata and a package
// beside it, measured as one artifact.
func TestBreakdownArtifactLayersSumToTotal(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "BiU.app")
	writeSized(t, filepath.Join(app, "Contents", "MacOS", "BiU"), 1000)
	writeSized(t, filepath.Join(app, "Contents", "Resources", "AppIcon.icns"), 300)
	writeSized(t, filepath.Join(app, "Contents", "Resources", "licenses", "THIRD-PARTY-NOTICES.txt"), 50)
	writeSized(t, filepath.Join(app, "Contents", "Info.plist"), 20)
	writeSized(t, filepath.Join(app, "Contents", "PkgInfo"), 8)
	writeSized(t, filepath.Join(root, "biu-0.1.0-arm64.dmg"), 5000)

	got, err := BreakdownArtifact(app)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bytes != 1378 {
		t.Fatalf("total bytes = %d, want 1378", got.Bytes)
	}
	if got.Files != 5 {
		t.Fatalf("files = %d, want 5", got.Files)
	}
	if n := got.LayerBytes(LayerMainBinary); n != 1000 {
		t.Errorf("main binary = %d, want 1000", n)
	}
	if n := got.LayerBytes(LayerResources); n != 350 {
		t.Errorf("resources = %d, want 350", n)
	}
	if n := got.LayerBytes(LayerOther); n != 28 {
		t.Errorf("other = %d, want 28", n)
	}
	if n := got.LayerBytes(LayerPackages); n != 0 {
		t.Errorf("packages inside the bundle = %d, want 0", n)
	}
	if n := got.InstalledBytes(); n != 1378 {
		t.Errorf("installed bytes = %d, want 1378", n)
	}
	if len(got.Resources) != 2 {
		t.Fatalf("resource sources = %+v, want two", got.Resources)
	}
	if got.Resources[0].Name != "AppIcon.icns" || got.Resources[0].Bytes != 300 {
		t.Errorf("largest resource source = %+v", got.Resources[0])
	}
	if got.Resources[1].Name != "licenses" || got.Resources[1].Bytes != 50 {
		t.Errorf("second resource source = %+v", got.Resources[1])
	}

	// Every layer is reported even when it is empty, so a reader never has to
	// guess whether "no row" means zero bytes or a missing rule.
	if len(got.Layers) != len(allLayers()) {
		t.Fatalf("layers = %+v, want one row per layer", got.Layers)
	}
}

func TestBreakdownArtifactCountsPackagesSeparately(t *testing.T) {
	root := t.TempDir()
	writeSized(t, filepath.Join(root, "biu.exe"), 4096)
	writeSized(t, filepath.Join(root, "biu-setup-0.1.0-amd64.exe"), 2048)
	writeSized(t, filepath.Join(root, "biu-0.1.0-windows-amd64.tar.gz"), 1024)

	got, err := BreakdownArtifact(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bytes != 7168 {
		t.Fatalf("total = %d, want 7168", got.Bytes)
	}
	if n := got.InstalledBytes(); n != 4096 {
		t.Errorf("installed = %d, want 4096 (installers are not installed bytes)", n)
	}
	if n := got.LayerBytes(LayerPackages); n != 3072 {
		t.Errorf("packages = %d, want 3072", n)
	}
}

func TestBreakdownArtifactOfSingleFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "biu-0.1.0-arm64.dmg")
	writeSized(t, path, 5000)
	got, err := BreakdownArtifact(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bytes != 5000 || got.LayerBytes(LayerPackages) != 5000 {
		t.Fatalf("file breakdown = %+v", got)
	}
}

func TestBreakdownArtifactFailsOnMissingPath(t *testing.T) {
	if _, err := BreakdownArtifact(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("a missing artifact reported a breakdown instead of an error")
	}
}

func writeSized(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLayersAlwaysSumToTheMeasuredTotal pins the arithmetic invariant: the
// report is only trustworthy if the four layers add back up to the measured
// bytes. A classification rule that drops a file (or counts it twice) would
// otherwise show up as "the percentages do not add to 100" — or not at all.
func TestLayersAlwaysSumToTheMeasuredTotal(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "BiU.app")
	writeSized(t, filepath.Join(app, "Contents", "MacOS", "BiU"), 1000)
	writeSized(t, filepath.Join(app, "Contents", "Resources", "AppIcon.icns"), 300)
	writeSized(t, filepath.Join(app, "Contents", "Info.plist"), 20)
	writeSized(t, filepath.Join(root, "biu-0.1.0-arm64.dmg"), 5000)
	writeSized(t, filepath.Join(root, "install.sh"), 40)

	for _, target := range []string{root, app} {
		got, err := BreakdownArtifact(target)
		if err != nil {
			t.Fatal(err)
		}
		var sum int64
		for _, l := range got.Layers {
			sum += l.Bytes
		}
		if sum != got.Bytes {
			t.Errorf("%s: layers sum to %d, measured %d", target, sum, got.Bytes)
		}
		if got.InstalledBytes() != got.Bytes-got.LayerBytes(LayerPackages) {
			t.Errorf("%s: installed bytes inconsistent", target)
		}
	}
}
