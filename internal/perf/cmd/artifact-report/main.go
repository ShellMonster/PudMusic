// artifact-report 打印构建产物的包体归因：分层体积 + 链接依赖。
//
//	go run ./internal/perf/cmd/artifact-report            # 量 build/
//	go run ./internal/perf/cmd/artifact-report -dir build # 同上
//
// 它**只测量、不构建**：`scripts/build.sh` 和 CI 在 `go tool mygo build` 之后调
// 它一次，本地也能单独跑。build/ 不存在或没有产物时输出「未构建」并成功返回——
// 那是一个正常状态，不是错误。
//
// 链接依赖来自 `go list -deps`：和许可证清单同一个口径（数的是**实际编进去的
// 模块**，不是 go.mod 的要求）。依赖图每个目标一份，因为 `//go:build windows`
// 的文件让四个目标的依赖不一样。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ShellMonster/PudMusic/internal/perf"
)

func main() {
	dir := flag.String("dir", "build", "构建产物根目录（mygo.json 的 out）")
	noDeps := flag.Bool("no-deps", false, "跳过 go list -deps（只量体积）")
	flag.Parse()

	if err := run(os.Stdout, *dir, *noDeps); err != nil {
		fmt.Fprintln(os.Stderr, "artifact-report:", err)
		os.Exit(1)
	}
}

func run(w io.Writer, dir string, noDeps bool) error {
	summary, err := perf.SummarizeBuild(dir)
	if err != nil {
		return err
	}

	if summary.Built && !noDeps {
		root, err := moduleRoot()
		if err != nil {
			return err
		}
		names := make([]string, 0, len(summary.Targets))
		for _, t := range summary.Targets {
			names = append(names, t.Name)
		}
		deps, err := perf.CollectDependencies(context.Background(), root, os.Environ(), perf.TargetsOf(names))
		if err != nil {
			return err
		}
		summary.Dependencies = deps
	}

	return perf.FormatBuildSummary(w, summary)
}

// moduleRoot 往上找 go.mod：`go run` 的工作目录是调用者的目录，可能是 build/。
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("从 %s 往上找不到 go.mod", dir)
		}
		dir = parent
	}
}
