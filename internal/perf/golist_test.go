package perf

import (
	"context"
	"testing"
)

// fixedGoList is a fixed `go list -deps -json` stream: standard library, the
// main module, and two external modules, with one module contributing two
// packages. The parser must drop the first two and group by module path.
const fixedGoList = `
{"ImportPath":"fmt","Standard":true}
{"ImportPath":"internal/abi","Standard":true}
{"ImportPath":"github.com/ShellMonster/PudMusic/internal/perf","Module":{"Path":"github.com/ShellMonster/PudMusic","Main":true}}
{"ImportPath":"github.com/Eyevinn/mp4ff/mp4","Module":{"Path":"github.com/Eyevinn/mp4ff","Version":"v0.60.0","Main":false}}
{"ImportPath":"github.com/Eyevinn/mp4ff/bits","Module":{"Path":"github.com/Eyevinn/mp4ff","Version":"v0.60.0","Main":false}}
{"ImportPath":"golang.org/x/image/webp","Module":{"Path":"golang.org/x/image","Version":"v0.46.0","Main":false}}
{"ImportPath":"github.com/Eyevinn/mp4ff/mp4","Module":{"Path":"github.com/Eyevinn/mp4ff","Version":"v0.60.0","Main":false}}
`

func TestParseGoListDepsGroupsByModuleWithoutStandardOrMain(t *testing.T) {
	groups, err := ParseGoListDeps([]byte(fixedGoList))
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %+v, want 2", groups)
	}
	if groups[0].Module != "github.com/Eyevinn/mp4ff" || groups[0].Version != "v0.60.0" {
		t.Fatalf("first group = %+v", groups[0])
	}
	if len(groups[0].Packages) != 2 {
		t.Errorf("mp4ff packages = %v, want two deduplicated entries", groups[0].Packages)
	}
	if groups[1].Module != "golang.org/x/image" || len(groups[1].Packages) != 1 {
		t.Errorf("second group = %+v", groups[1])
	}
	for _, g := range groups {
		for _, p := range g.Packages {
			if p == "fmt" || p == "internal/abi" {
				t.Errorf("standard library package %q leaked into %+v", p, g)
			}
			if p == "github.com/ShellMonster/PudMusic/internal/perf" {
				t.Errorf("main module package leaked into %+v", g)
			}
		}
	}
}

func TestParseGoListDepsRejectsBrokenJSON(t *testing.T) {
	for _, data := range []string{`{"ImportPath":"fmt"`, `not json at all`} {
		if _, err := ParseGoListDeps([]byte(data)); err == nil {
			t.Errorf("ParseGoListDeps(%q) did not report an error", data)
		}
	}
}

func TestParseGoListDepsOfNothingIsEmpty(t *testing.T) {
	groups, err := ParseGoListDeps(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 0 {
		t.Fatalf("groups = %+v, want none", groups)
	}
}

func TestMergeDependenciesUnionsTargetsAndPackages(t *testing.T) {
	merged := MergeDependencies(
		[]DependencyGroup{{Module: "b", Version: "v1", Packages: []string{"b/x", "b/y"}, Targets: []string{"darwin-arm64"}}},
		[]DependencyGroup{
			{Module: "a", Version: "v2", Packages: []string{"a"}, Targets: []string{"windows-amd64"}},
			{Module: "b", Version: "v1", Packages: []string{"b/y", "b/z"}, Targets: []string{"windows-amd64"}},
		},
	)
	if len(merged) != 2 {
		t.Fatalf("merged = %+v, want two modules", merged)
	}
	if merged[0].Module != "a" || merged[1].Module != "b" {
		t.Fatalf("merged order = %+v", merged)
	}
	if got := merged[1].Packages; len(got) != 3 || got[0] != "b/x" || got[2] != "b/z" {
		t.Errorf("merged packages = %v, want b/x b/y b/z", got)
	}
	if got := merged[1].Targets; len(got) != 2 {
		t.Errorf("merged targets = %v, want both", got)
	}
}

func TestGroupBySourceAggregatesOwnerAndGolangX(t *testing.T) {
	groups := []DependencyGroup{
		{Module: "github.com/Eyevinn/mp4ff", Packages: []string{"mp4", "bits"}, Targets: []string{"darwin-arm64"}},
		{Module: "github.com/mewkiz/flac", Packages: []string{"frame"}, Targets: []string{"darwin-arm64"}},
		{Module: "golang.org/x/image", Packages: []string{"draw"}, Targets: []string{"darwin-arm64"}},
		{Module: "golang.org/x/text", Packages: []string{"unicode"}, Targets: []string{"darwin-arm64"}},
	}
	got := GroupBySource(groups)
	if len(got) != 3 {
		t.Fatalf("groups = %+v, want 3", got)
	}
	bySource := map[string]SourceGroup{}
	for _, g := range got {
		bySource[g.Source] = g
	}
	if g := bySource["golang.org/x"]; g.Packages != 2 || len(g.Modules) != 2 {
		t.Errorf("golang.org/x group = %+v", g)
	}
	if g := bySource["github.com/Eyevinn"]; g.Packages != 2 {
		t.Errorf("Eyevinn group = %+v", g)
	}
	if got[0].Packages < got[len(got)-1].Packages {
		t.Errorf("groups are not sorted by package count: %+v", got)
	}
}

func TestTargetsOfParsesBuildDirectoryNames(t *testing.T) {
	got := TargetsOf([]string{"darwin-arm64", "windows-amd64", "not-a-target-name-x"})
	if len(got) != 2 {
		t.Fatalf("targets = %+v, want 2", got)
	}
	if got[0].GOOS != "darwin" || got[0].GOARCH != "arm64" || got[0].Name() != "darwin-arm64" {
		t.Errorf("first target = %+v", got[0])
	}
	if got[1].GOOS != "windows" || got[1].GOARCH != "amd64" {
		t.Errorf("second target = %+v", got[1])
	}
}

func TestLinkedFormatsOnlyNamesCapabilitiesThatAreLinked(t *testing.T) {
	deps := []DependencyGroup{
		{Module: "github.com/Eyevinn/mp4ff"},
		{Module: "golang.org/x/image"},
	}
	got := LinkedFormats(deps)
	if len(got) != 2 {
		t.Fatalf("formats = %+v, want two", got)
	}
	for _, f := range got {
		for _, m := range f.Modules {
			if m != "github.com/Eyevinn/mp4ff" && m != "golang.org/x/image" {
				t.Errorf("%s lists unlinked module %q", f.Capability, m)
			}
		}
	}
	if empty := LinkedFormats(nil); len(empty) != 0 {
		t.Errorf("no dependencies reported formats: %+v", empty)
	}
}

// TestCollectDependenciesReadsTheRealModuleGraph runs the parser on a real
// `go list -deps -json` stream for one target. The fixed-input test above pins
// the grouping rules; this one pins that those rules still match what the
// toolchain actually emits (field names, a stream of objects, module info).
func TestCollectDependenciesReadsTheRealModuleGraph(t *testing.T) {
	if testing.Short() {
		t.Skip("跑 go list 太慢，-short 下跳过")
	}
	root := perfRepoRoot(t)
	deps, err := CollectDependencies(context.Background(), root, nil, []BuildTarget{{GOOS: "darwin", GOARCH: "arm64"}})
	if err != nil {
		t.Fatalf("CollectDependencies：%v", err)
	}
	if len(deps) == 0 {
		t.Fatal("真实依赖图返回了空清单")
	}
	byModule := map[string]DependencyGroup{}
	for _, d := range deps {
		byModule[d.Module] = d
		if len(d.Targets) != 1 || d.Targets[0] != "darwin-arm64" {
			t.Errorf("%s 的目标 = %v，want [darwin-arm64]", d.Module, d.Targets)
		}
		if len(d.Packages) == 0 {
			t.Errorf("%s 没有包：模块级条目应当至少带一个包", d.Module)
		}
	}
	// 生产代码真的会链接这几个；少一个说明 go list 的输出没被读全。
	for _, want := range []string{"github.com/egoist/mygo", "github.com/Eyevinn/mp4ff", "github.com/tphakala/go-m4a"} {
		if _, ok := byModule[want]; !ok {
			t.Errorf("真实依赖图里没有 %s——解析器可能漏了模块信息", want)
		}
	}
}
