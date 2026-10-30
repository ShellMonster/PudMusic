package perf

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// perfRepoRoot walks up from the package directory to the repository root.
// `go test` sets the working directory to the package, not the repository.
func perfRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("拿不到工作目录：%v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("从工作目录往上找不到 go.mod")
		}
		dir = parent
	}
}

// TestNoOptionalLargeResourceIsStaticallyShipped is the guard behind the
// report's "大资源" marker: everything under resources/ is copied into every
// installer, so a 20 MB model or sample dropping in there would silently grow
// the base download for every user.
//
// The threshold is LargeResourceBytes and the rule is a straight failure rather
// than a warning: a resource that big needs an explicit decision (ship it, or
// fetch it at runtime), and the failure is where that decision gets recorded.
func TestNoOptionalLargeResourceIsStaticallyShipped(t *testing.T) {
	root := perfRepoRoot(t)
	resources := filepath.Join(root, "resources")
	info, err := os.Stat(resources)
	if err != nil {
		t.Fatalf("读不到 resources/：%v", err)
	}
	if !info.IsDir() {
		t.Fatalf("resources 不是目录")
	}

	entries, err := os.ReadDir(resources)
	if err != nil {
		t.Fatal(err)
	}
	type top struct {
		name  string
		bytes int64
	}
	var tops []top
	for _, e := range entries {
		if e.IsDir() {
			var n int64
			err := fs.WalkDir(os.DirFS(filepath.Join(resources, e.Name())), ".", func(_ string, d fs.DirEntry, err error) error {
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
				n += fi.Size()
				return nil
			})
			if err != nil {
				t.Fatalf("%s：%v", e.Name(), err)
			}
			tops = append(tops, top{e.Name(), n})
			continue
		}
		fi, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		tops = append(tops, top{e.Name(), fi.Size()})
	}

	sort.Slice(tops, func(i, j int) bool { return tops[i].bytes > tops[j].bytes })
	for _, r := range tops {
		t.Logf("resources/%s = %s", r.name, FormatBytes(r.bytes))
		if r.bytes >= LargeResourceBytes {
			t.Errorf("resources/%s 有 %s，>= %s：它会被复制进每一个安装包。\n"+
				"可选的大资源应当运行时按需下载或拆成独立资源包；确实要静态随包的话，"+
				"在这里记下理由并调整阈值，别让它悄悄撑大基础安装包",
				r.name, FormatBytes(r.bytes), FormatBytes(LargeResourceBytes))
		}
	}
}
