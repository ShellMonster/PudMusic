// genicon 画出 `resources/icon.png`（MyGo 打包用的应用图标源图）。
//
//	go generate ./internal/platform/
//
// 图案本身是 `internal/platform/icon-source.png`（人工挑定的那张）加上
// `platform.AppIcon` 里的缩放与切形；这个命令只是把它落到 `resources/` 下——
// 而 `resources/icon.png` 必须是**精确的 1024×1024**，因为 MyGo 的 `mygo build`
// 从它出发渲 `.icns`（最大一档 1024）与 `.ico`（最大一档 256）。手绘提交的那一张
// 会和代码里的图案分家，而分家这件事没有任何构建会报错：PNG 照样能解码，
// MyGo 照样能渲出全部尺寸，只是图案已经不是代码里那个了。
//
// `platform/icon_test.go` 的 `TestTheShippedIconIsTheOneTheCodeDraws` 双向钉住它。
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ShellMonster/PudMusic/internal/platform"
)

// iconRelPath 是源图的位置，相对**仓库根**。
const iconRelPath = "resources/icon.png"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "genicon:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	// 1024 不是随便挑的：`.icns` 最大的一档就是 1024，`.ico` 最大的一档是 256。
	// 源图给 1024，这两档都是**下采样**出来的；源图给 512 的话 1024 那档就是
	// 插值出来的模糊——这正是 Biu 自己那张 `logo.png` 只有 512×512 的毛病。
	const size = 1024

	png, err := platform.AppIconPNG(size)
	if err != nil {
		return err
	}
	path := filepath.Join(root, iconRelPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, png, 0o644); err != nil {
		return err
	}
	fmt.Printf("写了 %s（%dx%d，%d 字节）\n", path, size, size, len(png))
	return nil
}

// moduleRoot 往上找 `go.mod` 所在的那层目录。
//
// 不能直接 `filepath.Join(os.Getwd(), iconRelPath)`：`go generate` 的工作目录是
// **包目录**（`internal/platform`），那样写会落到 `internal/platform/resources/`
// 下面去——一个没人看的地方，图标于是「不见了」而不是「报错了」。
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
