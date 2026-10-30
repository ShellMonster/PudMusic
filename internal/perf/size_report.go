package perf

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PackageArtifact is one compressed distribution file: a dmg, an installer, a
// signed update archive.
type PackageArtifact struct {
	Name  string
	Bytes int64
}

// TargetSize is one build/<goos-goarch> directory, split into the layers that
// change for different reasons.
type TargetSize struct {
	// Name is the build target directory, e.g. darwin-arm64.
	Name string
	// AppName is the bundle name (BiU.app) when the target produced one, else
	// empty: Windows and Linux install files next to the executable.
	AppName string

	Bytes int64
	// Files is the installed file count, so it matches Installed rather than
	// the directory listing (which also holds the compressed packages).
	Files int

	// AppBytes is the .app bundle alone: what a macOS user drags into
	// /Applications. Zero when the target has no bundle.
	AppBytes int64

	MainBinary int64
	Resources  int64
	Other      int64
	Packed     int64

	// Installed is everything except the compressed packages.
	Installed int64

	// Packages are the top-level compressed files: dmg, installer, tar.gz.
	Packages []PackageArtifact
	// ResourceSources is the resource layer aggregated by its top-level entry,
	// largest first.
	ResourceSources []ResourceSource
}

// BuildSummary is the attribution report for a build output directory.
type BuildSummary struct {
	Dir string
	// Built says whether any artifact file was found. A false value is a
	// normal state (nothing built yet), not an error.
	Built bool
	Note  string

	Targets      []TargetSize
	Dependencies []DependencyGroup
}

// SummarizeBuild measures every <goos>-<goarch> directory under dir.
//
// A missing directory or one without artifacts reads as "not built" rather than
// an error: this entry point runs after a build in CI and on developer machines
// where build/ may not exist, and a crash would say nothing useful.
func SummarizeBuild(dir string) (BuildSummary, error) {
	s := BuildSummary{Dir: dir}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			s.Note = fmt.Sprintf("未构建：%s 不存在（先跑 scripts/build.sh）", displayDir(dir))
			return s, nil
		}
		return BuildSummary{}, err
	}

	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue // .staging-* and other temporary siblings
		}
		t, files, err := measureTarget(filepath.Join(dir, e.Name()), e.Name())
		if err != nil {
			return BuildSummary{}, err
		}
		if files == 0 {
			continue
		}
		s.Targets = append(s.Targets, t)
	}
	sort.Slice(s.Targets, func(i, j int) bool { return s.Targets[i].Name < s.Targets[j].Name })

	if len(s.Targets) == 0 {
		s.Note = fmt.Sprintf("未构建：%s 里没有产物目录（先跑 scripts/build.sh）", displayDir(dir))
		return s, nil
	}
	s.Built = true
	s.Note = fmt.Sprintf("已构建：%d 个目标", len(s.Targets))
	return s, nil
}

// measureTarget measures one build/<target> directory. The second result is
// every file walked, including the compressed packages: it answers "did this
// target produce anything at all", while TargetSize.Files counts only the
// installed files so it lines up with Installed.
func measureTarget(dir, name string) (TargetSize, int, error) {
	t := TargetSize{Name: name}
	resources := map[string]*ResourceSource{}
	walked := 0

	err := fs.WalkDir(os.DirFS(dir), ".", func(rel string, d fs.DirEntry, err error) error {
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
		rel = filepath.ToSlash(rel)
		kind := classifyArtifactFile(rel, fi.Mode())
		size := fi.Size()
		t.Bytes += size
		walked++
		switch kind {
		case LayerPackages:
			// The compressed form is what a release uploads, not what a user
			// installs: it stays out of Files and Installed.
		case LayerMainBinary:
			t.Files++
			t.MainBinary += size
		case LayerResources:
			t.Files++
			t.Resources += size
		default:
			t.Files++
			t.Other += size
		}

		top := rel
		if i := strings.Index(rel, "/"); i >= 0 {
			top = rel[:i]
		}
		if kind == LayerPackages {
			t.Packed += size
			if top == rel {
				t.Packages = append(t.Packages, PackageArtifact{Name: rel, Bytes: size})
			}
		}
		if isBundleDir(top) {
			t.AppName = top
			t.AppBytes += size
		}
		if kind == LayerResources {
			src := resourceSourceName(rel)
			r := resources[src]
			if r == nil {
				r = &ResourceSource{Name: src}
				resources[src] = r
			}
			r.Bytes += size
			r.Files++
		}
		return nil
	})
	if err != nil {
		return TargetSize{}, 0, err
	}

	t.Installed = t.Bytes - t.Packed
	for _, r := range resources {
		t.ResourceSources = append(t.ResourceSources, *r)
	}
	sort.Slice(t.ResourceSources, func(i, j int) bool {
		if t.ResourceSources[i].Bytes != t.ResourceSources[j].Bytes {
			return t.ResourceSources[i].Bytes > t.ResourceSources[j].Bytes
		}
		return t.ResourceSources[i].Name < t.ResourceSources[j].Name
	})
	sort.Slice(t.Packages, func(i, j int) bool { return t.Packages[i].Name < t.Packages[j].Name })
	return t, walked, nil
}

// isBundleDir reports whether a top-level entry is a macOS app bundle.
func isBundleDir(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".app")
}

// displayDir is how the report names the build directory. A relative path is
// printed as given; an absolute path is shortened to a path relative to the
// working directory, and otherwise to its base name. The report is pasted into
// CI summaries and issue comments, where a machine's home directory is noise at
// best and a leaked internal path at worst.
func displayDir(dir string) string {
	if !filepath.IsAbs(dir) {
		return dir
	}
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, dir); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return filepath.Base(dir)
}

// FormatBuildSummary renders the report. Sizes always carry their unit; a
// reader must never have to guess whether a number is bytes or megabytes.
func FormatBuildSummary(w io.Writer, s BuildSummary) error {
	b := &strings.Builder{}
	fmt.Fprintf(b, "包体归因（%s）\n", displayDir(s.Dir))
	if !s.Built {
		fmt.Fprintf(b, "  %s\n", s.Note)
		_, err := io.WriteString(w, b.String())
		return err
	}
	b.WriteString("  主可执行文件 / Resources / 其他 是安装后（解包）体积；安装包/压缩包是下载体积。\n")

	for _, t := range s.Targets {
		b.WriteString("\n")
		if t.AppName != "" {
			fmt.Fprintf(b, "目标 %s（%s）\n", t.Name, t.AppName)
		} else {
			fmt.Fprintf(b, "目标 %s\n", t.Name)
		}
		fmt.Fprintf(b, "  安装后总计     %10s  %d 个文件\n", FormatBytes(t.Installed), t.Files)
		writeLayer := func(label string, n int64) {
			if t.Installed > 0 {
				fmt.Fprintf(b, "    %-13s %10s  %3.0f%%\n", label, FormatBytes(n), 100*float64(n)/float64(t.Installed))
			} else {
				fmt.Fprintf(b, "    %-13s %10s\n", label, FormatBytes(n))
			}
		}
		writeLayer("主可执行文件", t.MainBinary)
		writeLayer("Resources", t.Resources)
		writeLayer("其他", t.Other)
		if t.AppName != "" {
			fmt.Fprintf(b, "  App 总大小（%s）%10s\n", t.AppName, FormatBytes(t.AppBytes))
		}
		if t.Packed > 0 {
			fmt.Fprintf(b, "  安装包/压缩包  %10s\n", FormatBytes(t.Packed))
			for _, p := range t.Packages {
				fmt.Fprintf(b, "    %-36s %10s\n", p.Name, FormatBytes(p.Bytes))
			}
		}

		b.WriteString("  资源来源（按顶层条目聚合；>= 1 MiB 的需复核是否该静态进基础安装包）：\n")
		if len(t.ResourceSources) == 0 {
			b.WriteString("    （无）\n")
		} else {
			for i, r := range t.ResourceSources {
				if i == 8 {
					fmt.Fprintf(b, "    ……另有 %d 项\n", len(t.ResourceSources)-i)
					break
				}
				mark := ""
				if r.Bytes >= LargeResourceBytes {
					mark = "  ← 大资源"
				}
				fmt.Fprintf(b, "    %-36s %10s  %d 个文件%s\n", r.Name, FormatBytes(r.Bytes), r.Files, mark)
			}
		}
	}

	b.WriteString("\n链接依赖（go list -deps，按模块聚合；字节不按模块拆分）\n")
	if len(s.Dependencies) == 0 {
		b.WriteString("  （未收集）\n")
	} else {
		for _, d := range s.Dependencies {
			line := fmt.Sprintf("  %s %s", d.Module, d.Version)
			if n := len(d.Packages); n > 0 {
				line += fmt.Sprintf("  %d 个包", n)
			}
			if len(d.Targets) > 0 {
				line += "  [" + strings.Join(d.Targets, ", ") + "]"
			}
			b.WriteString(strings.TrimRight(line, " ") + "\n")
			if note := CapabilityNote(d.Module); note != "" {
				fmt.Fprintf(b, "      %s\n", note)
			}
		}

		b.WriteString("\n按来源聚合（一个来源 = 仓库 host/owner，或 golang.org/x）\n")
		for _, g := range GroupBySource(s.Dependencies) {
			targets := ""
			if len(g.Targets) > 0 {
				targets = "  [" + strings.Join(g.Targets, ", ") + "]"
			}
			fmt.Fprintf(b, "  %-28s %3d 个包  %2d 个模块%s\n", g.Source, g.Packages, len(g.Modules), targets)
		}

		b.WriteString("\n格式与能力来源（候选裁剪清单；裁一项会删掉它列的模块）\n")
		for _, f := range LinkedFormats(s.Dependencies) {
			fmt.Fprintf(b, "  %s\n    ← %s\n", f.Capability, strings.Join(f.Modules, " + "))
		}
		b.WriteString("  MP3 解码器是 vendor 进仓库的（internal/service/audio/mp3dec/gomp3），不在 go list -deps 里。\n")
	}

	_, err := io.WriteString(w, b.String())
	return err
}
