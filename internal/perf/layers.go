package perf

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// LayerKind is one bucket of the artifact-size attribution.
//
// The buckets exist because "the app is 18 MB" is not an actionable number: the
// main executable is what a dependency change moves, Resources is what an
// embedded asset moves, and the installer/archive is what the user downloads.
// Mixing them hides which change caused which number.
type LayerKind string

const (
	// LayerMainBinary is the linked executable the app runs.
	LayerMainBinary LayerKind = "main_binary"
	// LayerResources is what ships in the app's resource directory: the icon,
	// the license texts, localization strings and any bundled data.
	LayerResources LayerKind = "resources"
	// LayerOther is installed but neither executable nor resource: Info.plist,
	// PkgInfo, code signature, install scripts, update metadata.
	LayerOther LayerKind = "other"
	// LayerPackages is the compressed distribution form: dmg, installer exe,
	// tar.gz, delta. It is counted apart because it is what a release uploads,
	// while the installed total is what a user's disk holds.
	LayerPackages LayerKind = "packages"
)

func allLayers() []LayerKind {
	return []LayerKind{LayerMainBinary, LayerResources, LayerOther, LayerPackages}
}

// LayerSize is the bytes and file count of one layer.
type LayerSize struct {
	Kind  LayerKind
	Bytes int64
	Files int
}

// ResourceSource is one top-level entry of the resource directory, aggregated
// over everything below it. Names are relative, including the bundle's
// Contents/Resources segment, so a report does not leak a machine path.
type ResourceSource struct {
	Name  string
	Bytes int64
	Files int
}

// ArtifactBreakdown is a measured artifact (a file, an app bundle, or a
// build/<target> directory) split into layers.
type ArtifactBreakdown struct {
	Path      string
	Bytes     int64
	Files     int
	Layers    []LayerSize
	Resources []ResourceSource
}

// LayerBytes returns the byte count of a layer, zero when it has none.
func (b ArtifactBreakdown) LayerBytes(kind LayerKind) int64 {
	for _, l := range b.Layers {
		if l.Kind == kind {
			return l.Bytes
		}
	}
	return 0
}

// InstalledBytes is what an installed copy occupies: everything except the
// compressed packages, which are distribution artifacts rather than files a
// user keeps.
func (b ArtifactBreakdown) InstalledBytes() int64 {
	return b.Bytes - b.LayerBytes(LayerPackages)
}

// LargeResourceBytes is the threshold above which a single resource source is
// worth reviewing: at that size it is a candidate for an on-demand download
// instead of being statically shipped in the base installer.
const LargeResourceBytes int64 = 1 << 20

// ClassifyArtifactFile assigns a relative path inside an artifact to a layer.
//
// The rules follow the layout MyGo produces: a macOS bundle puts the executable
// in Contents/MacOS and resources in Contents/Resources; Windows and Linux put
// resources next to the executable. Only mygo's own top-level files (install
// scripts, update metadata) are "other"; everything else beside the executable
// is a resource, because that is how `resources/` is laid out after copying.
func ClassifyArtifactFile(rel string) LayerKind {
	return classifyArtifactFile(rel, 0)
}

func classifyArtifactFile(rel string, mode fs.FileMode) LayerKind {
	rel = path.Clean(filepath.ToSlash(rel))
	if rel == "." || rel == "" {
		return LayerOther
	}
	lower := strings.ToLower(rel)
	base := strings.ToLower(path.Base(rel))

	if isPackageName(base) {
		return LayerPackages
	}

	if section, rest, ok := bundleSection(rel); ok {
		switch section {
		case "resources":
			return LayerResources
		case "macos":
			if !strings.Contains(rest, "/") {
				return LayerMainBinary
			}
			return LayerOther
		}
	}
	if isInBundleContents(lower) {
		return LayerOther
	}

	if !strings.Contains(rel, "/") {
		switch {
		case base == "install.sh", base == "install.ps1":
			return LayerOther
		case strings.HasPrefix(base, "update-") && strings.HasSuffix(base, ".json"):
			return LayerOther
		}
		if strings.HasSuffix(base, ".exe") {
			return LayerMainBinary
		}
		if !strings.Contains(base, ".") {
			// A top-level file with no extension is the Linux executable. The
			// walker passes the real mode; a direct call passes zero and is
			// treated as the executable.
			if mode == 0 || mode&0o111 != 0 {
				return LayerMainBinary
			}
			return LayerOther
		}
		if isAssetName(base) {
			return LayerResources
		}
		return LayerOther
	}

	// Below the top level of a non-bundle target: `resources/` is copied
	// preserving its structure next to the executable, so this is resource
	// content (licenses/, icons/, bundled data).
	return LayerResources
}

// bundleSection reports which part of a macOS bundle a path lives in, and the
// path below that part.
//
// Resources is checked before MacOS on purpose: a helper app inside
// Contents/Resources has its own Contents/MacOS, and it is still resource
// payload of the outer bundle.
func bundleSection(rel string) (section, rest string, ok bool) {
	lower := strings.ToLower(rel)
	for _, candidate := range []struct{ name, marker string }{
		{"resources", "contents/resources/"},
		{"macos", "contents/macos/"},
	} {
		if i := strings.Index(lower, candidate.marker); i >= 0 {
			return candidate.name, rel[i+len(candidate.marker):], true
		}
	}
	return "", "", false
}

// isInBundleContents reports whether a path is inside a bundle's Contents
// directory: either below a .app segment, or already relative to the bundle
// root (the walker can be pointed at the .app itself).
func isInBundleContents(lower string) bool {
	return strings.Contains(lower, ".app/contents/") || strings.HasPrefix(lower, "contents/")
}

func isPackageName(base string) bool {
	for _, suffix := range []string{".dmg", ".pkg", ".tar.gz", ".tgz", ".zip", ".delta", ".deb", ".msi"} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	// MyGo names the Windows installer <slug>-setup-<version>-<arch>.exe; the
	// plain <name>.exe next to it is the app itself.
	return strings.HasSuffix(base, ".exe") && strings.Contains(base, "-setup-")
}

func isAssetName(base string) bool {
	ext := path.Ext(base)
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".icns", ".ico", ".svg",
		".txt", ".md", ".html", ".css", ".js", ".wasm",
		".ttf", ".otf", ".woff", ".woff2", ".strings", ".lproj",
		".json", ".plist", ".yaml", ".yml", ".toml",
		".bin", ".dat", ".pack", ".db", ".sqlite",
		".mp3", ".wav", ".ogg", ".flac", ".m4a", ".aac":
		return true
	}
	return false
}

// BreakdownArtifact measures a file or directory and splits it into layers.
// Directories are walked with the same accounting as MeasureArtifacts: the sum
// of the regular files below them. A missing path is an error so a typo cannot
// read as "this artifact is empty".
func BreakdownArtifact(p string) (ArtifactBreakdown, error) {
	info, err := os.Stat(p)
	if err != nil {
		return ArtifactBreakdown{}, err
	}
	b := ArtifactBreakdown{
		Path:   p,
		Layers: make([]LayerSize, len(allLayers())),
	}
	for i, k := range allLayers() {
		b.Layers[i].Kind = k
	}

	if !info.IsDir() {
		b.Bytes, b.Files = info.Size(), 1
		addLayer(&b, classifyArtifactFile(p, info.Mode()), info.Size())
		return b, nil
	}

	resources := map[string]*ResourceSource{}
	err = fs.WalkDir(os.DirFS(p), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		kind := classifyArtifactFile(rel, fi.Mode())
		b.Bytes += fi.Size()
		b.Files++
		addLayer(&b, kind, fi.Size())
		if kind == LayerResources {
			name := resourceSourceName(rel)
			r := resources[name]
			if r == nil {
				r = &ResourceSource{Name: name}
				resources[name] = r
			}
			r.Bytes += fi.Size()
			r.Files++
		}
		return nil
	})
	if err != nil {
		return ArtifactBreakdown{}, err
	}
	b.Resources = make([]ResourceSource, 0, len(resources))
	for _, r := range resources {
		b.Resources = append(b.Resources, *r)
	}
	sort.Slice(b.Resources, func(i, j int) bool {
		if b.Resources[i].Bytes != b.Resources[j].Bytes {
			return b.Resources[i].Bytes > b.Resources[j].Bytes
		}
		return b.Resources[i].Name < b.Resources[j].Name
	})
	return b, nil
}

func addLayer(b *ArtifactBreakdown, kind LayerKind, n int64) {
	for i := range b.Layers {
		if b.Layers[i].Kind == kind {
			b.Layers[i].Bytes += n
			b.Layers[i].Files++
			return
		}
	}
}

// resourceSourceName is the top-level name a resource file is attributed to:
// the first entry below Contents/Resources in a bundle, or the first path
// component next to a non-bundle executable.
func resourceSourceName(rel string) string {
	rel = path.Clean(filepath.ToSlash(rel))
	lower := strings.ToLower(rel)
	const marker = "contents/resources/"
	if i := strings.Index(lower, marker); i >= 0 {
		rest := rel[i+len(marker):]
		if j := strings.Index(rest, "/"); j >= 0 {
			return rest[:j]
		}
		return rest
	}
	if i := strings.Index(rel, "/"); i >= 0 {
		return rel[:i]
	}
	return rel
}

// FormatBytes renders a byte count for the report. It is intentionally a
// single short form: 1000 B, 5 KiB, 17.8 MiB.
func FormatBytes(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.0f KiB", float64(n)/(1<<10))
	case n < 1<<30:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	}
}
