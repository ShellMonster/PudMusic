package perf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
)

// DependencyGroup is the link-level attribution of one module: the packages it
// contributes, and which build targets link it.
//
// It answers "what is linked in", not "how many bytes it costs". A per-module
// byte count would need a linker map or a symbol-level size pass; reporting the
// module list without inventing byte numbers is the honest half that can be
// reproduced without a build.
type DependencyGroup struct {
	Module   string
	Version  string
	Packages []string
	Targets  []string
}

// listedPackage is the subset of `go list -deps -json` this report reads.
type listedPackage struct {
	ImportPath string
	Standard   bool
	Module     *struct {
		Path    string
		Version string
		Main    bool
	}
}

// ParseGoListDeps turns a `go list -deps -json` stream into module groups,
// dropping standard-library packages and the main module.
//
// `go list -json` emits a stream of concatenated objects, not an array, so the
// decoder reads them one after another until EOF.
func ParseGoListDeps(data []byte) ([]DependencyGroup, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	type moduleInfo struct {
		version  string
		packages map[string]bool
	}
	modules := map[string]*moduleInfo{}
	for {
		var p listedPackage
		err := dec.Decode(&p)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("解 go list 的输出：%w", err)
		}
		if p.Standard || p.Module == nil || p.Module.Main || p.Module.Path == "" {
			continue
		}
		m := modules[p.Module.Path]
		if m == nil {
			m = &moduleInfo{packages: map[string]bool{}}
			modules[p.Module.Path] = m
		}
		if m.version == "" {
			m.version = p.Module.Version
		}
		if p.ImportPath != "" {
			m.packages[p.ImportPath] = true
		}
	}

	out := make([]DependencyGroup, 0, len(modules))
	for module, m := range modules {
		packages := make([]string, 0, len(m.packages))
		for pkg := range m.packages {
			packages = append(packages, pkg)
		}
		sort.Strings(packages)
		out = append(out, DependencyGroup{Module: module, Version: m.version, Packages: packages})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Module < out[j].Module })
	return out, nil
}

// MergeDependencies unions per-target dependency lists by module path.
//
// A module can appear for one target and not another: `//go:build windows`
// files change the graph, which is exactly why the report keeps the target
// list instead of collapsing the four builds into one.
func MergeDependencies(lists ...[]DependencyGroup) []DependencyGroup {
	type merged struct {
		version  string
		packages map[string]bool
		targets  map[string]bool
	}
	byModule := map[string]*merged{}
	for _, list := range lists {
		for _, g := range list {
			m := byModule[g.Module]
			if m == nil {
				m = &merged{packages: map[string]bool{}, targets: map[string]bool{}}
				byModule[g.Module] = m
			}
			if m.version == "" {
				m.version = g.Version
			}
			for _, p := range g.Packages {
				m.packages[p] = true
			}
			for _, t := range g.Targets {
				m.targets[t] = true
			}
		}
	}
	out := make([]DependencyGroup, 0, len(byModule))
	for module, m := range byModule {
		g := DependencyGroup{Module: module, Version: m.version}
		for p := range m.packages {
			g.Packages = append(g.Packages, p)
		}
		for t := range m.targets {
			g.Targets = append(g.Targets, t)
		}
		sort.Strings(g.Packages)
		sort.Strings(g.Targets)
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Module < out[j].Module })
	return out
}

// BuildTarget is one GOOS/GOARCH pair the report covers.
type BuildTarget struct {
	GOOS   string
	GOARCH string
}

// Name is the build/<target> directory name MyGo uses.
func (t BuildTarget) Name() string { return t.GOOS + "-" + t.GOARCH }

// CollectDependencies runs `go list -deps -json` once per target and merges the
// results, so the report names the modules each target actually links.
//
// A nil env means "inherit this process's environment": an empty one makes
// `go list` fail with "module cache not found", which says nothing about what
// the caller did wrong.
func CollectDependencies(ctx context.Context, dir string, env []string, targets []BuildTarget) ([]DependencyGroup, error) {
	if env == nil {
		env = os.Environ()
	}
	var lists [][]DependencyGroup
	for _, t := range targets {
		cmd := exec.CommandContext(ctx, goTool(), "list", "-deps", "-json", ".")
		cmd.Dir = dir
		// Copy before appending: the caller's slice may share its backing array.
		cmd.Env = append(append([]string{}, env...), "GOOS="+t.GOOS, "GOARCH="+t.GOARCH)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("go list -deps（%s）：%v\n%s", t.Name(), err, stderr.String())
		}
		groups, err := ParseGoListDeps(out)
		if err != nil {
			return nil, fmt.Errorf("%s：%w", t.Name(), err)
		}
		for i := range groups {
			groups[i].Targets = []string{t.Name()}
		}
		lists = append(lists, groups)
	}
	return MergeDependencies(lists...), nil
}

// goTool finds `go`. Without it exec reports "executable file not found in
// $PATH", which reads as a missing binary rather than a missing toolchain.
func goTool() string {
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	return "go"
}

// capabilityNotes says what each direct dependency is linked for, so the report
// answers "which formats does this binary support, and who brings them" without
// reading the import graph by hand.
//
// Only direct requirements are annotated. Transitive modules (`bitio`,
// `mewkiz/pkg`, `mewpkg/term`, `x/text`) are printed without a note: inventing a
// capability for them would claim more than the module boundary says.
var capabilityNotes = map[string]string{
	"github.com/Eyevinn/mp4ff":       "MP4/fMP4 容器、分片与 mux",
	"github.com/ebitengine/purego":   "调用系统音频框架（AudioToolbox / Media Foundation）",
	"github.com/egoist/mygo":         "原生 UI 框架",
	"github.com/mewkiz/flac":         "FLAC 帧与元数据解析",
	"github.com/tphakala/go-m4a":     "M4A/MP4 容器与 AAC 帧",
	"github.com/jfreymuth/oggvorbis": "Ogg Vorbis 解码",
	"github.com/jfreymuth/vorbis":    "Vorbis 包解码",
	"golang.org/x/image":             "JPEG/PNG/WebP/BMP 图片解码与缩放",
	"golang.org/x/net":               "HTTP 代理环境解析",
	"github.com/go-text/typesetting": "文本排版与字形",
}

// CapabilityNote is the report's description of what a module is linked for.
func CapabilityNote(module string) string { return capabilityNotes[module] }

// FormatSupport is one user-facing capability and the modules that provide it.
//
// This is the candidate list the package-size work needs: cutting a format
// means dropping these modules, and the report has to say which user-facing
// capability that would remove. A capability is listed only when at least one
// of its modules is actually linked.
type FormatSupport struct {
	Capability string
	Modules    []string
}

// formatSupport is the capability table, in the order the report prints it.
//
// Only modules that appear in `go list -deps` are listed here. MP3 decoding is
// vendored into the repository (internal/service/audio/mp3dec/gomp3), so it
// cannot be attributed to a module and is out of this table; the report says so
// once instead of pretending the table is exhaustive.
var formatSupport = []FormatSupport{
	{"MP4/fMP4 容器与分片", []string{"github.com/Eyevinn/mp4ff", "github.com/tphakala/go-m4a"}},
	{"AAC/M4A/M4S（经系统解码器）", []string{"github.com/tphakala/go-m4a", "github.com/ebitengine/purego"}},
	{"FLAC", []string{"github.com/mewkiz/flac", "github.com/tphakala/go-m4a"}},
	{"Ogg Vorbis", []string{"github.com/jfreymuth/oggvorbis", "github.com/jfreymuth/vorbis"}},
	{"图片解码与缩放（JPEG/PNG/WebP/BMP）", []string{"golang.org/x/image"}},
	{"文本排版与字形", []string{"github.com/go-text/typesetting"}},
}

// LinkedFormats returns the capabilities whose modules are present in the
// merged dependency list, each with the linked modules it actually uses.
func LinkedFormats(deps []DependencyGroup) []FormatSupport {
	linked := make(map[string]bool, len(deps))
	for _, d := range deps {
		linked[d.Module] = true
	}
	var out []FormatSupport
	for _, f := range formatSupport {
		var modules []string
		for _, m := range f.Modules {
			if linked[m] {
				modules = append(modules, m)
			}
		}
		if len(modules) > 0 {
			out = append(out, FormatSupport{Capability: f.Capability, Modules: modules})
		}
	}
	return out
}

// SourceGroup aggregates modules that come from the same source (a repository
// host plus its owner, or golang.org/x), which is the level a reader uses to
// ask "how much of this binary is the media stack versus the UI framework".
type SourceGroup struct {
	Source   string
	Modules  []string
	Packages int
	Targets  []string
}

// GroupBySource aggregates merged dependency groups by their source.
func GroupBySource(groups []DependencyGroup) []SourceGroup {
	type acc struct {
		modules  map[string]bool
		targets  map[string]bool
		packages int
	}
	bySource := map[string]*acc{}
	for _, g := range groups {
		src := dependencySource(g.Module)
		a := bySource[src]
		if a == nil {
			a = &acc{modules: map[string]bool{}, targets: map[string]bool{}}
			bySource[src] = a
		}
		a.modules[g.Module] = true
		a.packages += len(g.Packages)
		for _, t := range g.Targets {
			a.targets[t] = true
		}
	}
	out := make([]SourceGroup, 0, len(bySource))
	for src, a := range bySource {
		s := SourceGroup{Source: src, Packages: a.packages}
		for m := range a.modules {
			s.Modules = append(s.Modules, m)
		}
		for t := range a.targets {
			s.Targets = append(s.Targets, t)
		}
		sort.Strings(s.Modules)
		sort.Strings(s.Targets)
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Packages != out[j].Packages {
			return out[i].Packages > out[j].Packages
		}
		return out[i].Source < out[j].Source
	})
	return out
}

// TargetsOf parses build/<goos-goarch> directory names back into build targets.
//
// Names that do not start with a known GOOS and end in a known GOARCH are
// skipped: build/ also holds things a target directory never is, and an
// unparsed name would otherwise become a bogus `go list` target.
func TargetsOf(names []string) []BuildTarget {
	out := make([]BuildTarget, 0, len(names))
	for _, n := range names {
		goos, goarch, ok := strings.Cut(n, "-")
		if !ok || !knownGOOS[goos] || !knownGOARCH[goarch] {
			continue
		}
		out = append(out, BuildTarget{GOOS: goos, GOARCH: goarch})
	}
	return out
}

var knownGOOS = map[string]bool{
	"darwin": true, "linux": true, "windows": true, "freebsd": true, "js": true,
}

// knownGOARCH includes universal, which MyGo uses for a combined macOS bundle.
var knownGOARCH = map[string]bool{
	"amd64": true, "arm64": true, "386": true, "arm": true,
	"loong64": true, "mips": true, "mips64": true, "mips64le": true,
	"mipsle": true, "ppc64": true, "ppc64le": true, "riscv64": true,
	"s390x": true, "wasm": true, "universal": true,
}

// dependencySource is the report's wording for "where the format support comes
// from": the module path's first two segments, e.g. github.com/Eyevinn, except
// the golang.org/x family, which is reported as golang.org/x.
func dependencySource(module string) string {
	parts := strings.Split(module, "/")
	if len(parts) >= 3 && parts[0] == "golang.org" && parts[1] == "x" {
		return "golang.org/x"
	}
	if len(parts) >= 2 {
		return path.Join(parts[:2]...)
	}
	return module
}
