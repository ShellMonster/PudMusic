// Package iconset 是**图标资源那一份 embed**，谁要画图标都从这里取。
//
// # 为什么它单独一个包
//
// 资源本来在 `internal/ui` 自己手里（`icons.go` 的 `//go:embed icons`），而
// **封面占位也要画一张图标**——它住在 `widgets/imgcache`，那个包要反过来 import
// `internal/ui` 才拿得到图，而 `internal/ui` 本来就 import `widgets/imgcache`，
// 那是个环。环的解法通常有两条：控件层自己再 embed 一份（**同一张 SVG 在仓库里
// 躺两份**，加图标的人只改了一份，另一份就过期了，而且没有任何测试会红），或者把
// 资源挪到一个谁都能 import 的叶子包里。这是后者。
//
// 挪过来之后 `internal/ui` 仍然只认**自己那批命名过的图标**（`icons.go` 的常量
// 和 `iconNames`），而 `widgets/imgcache` 只多认一张（封面占位那张图片图标）。
// 两边共用的是**同一份资源和同一个解析器**，不是两份拷贝。
//
// # 目录结构照 RemixIcon 上游，不拍平
//
// 见 `icons.go` 文件头那一段：`icons/` 下面是上游的分类目录（`Arrows/`、`Media/`、
// `System/`、`Editor/`、`Document/`、`Design/`、`Business/`、`Device/`、
// `Others/`、`User & Faces/`、`Weather/`、`Communication/`）。名字逐字照上游，
// 任何一个都能反查回 `Remix-Design/RemixIcon` 的哪一个文件。
//
// # 解析结果按名字缓存
//
// 一张 SVG 解析出来是一个 `*ui.SVG`，`ui.Icon` 要的就是它；每帧重新解析不但白烧
// CPU，还会让那个元素每帧换一个 doc（理由见 `icons.go` 的 mustParseIcons）。
// 界面层是自己建一张表的，而控件层随手画一张——两边都从这里拿，**同一个名字永远
// 是同一个指针**。
package iconset

import (
	"embed"
	"fmt"
	"path"
	"sync"

	"github.com/egoist/mygo/ui"
)

//go:embed icons
var files embed.FS

// cache 是「名字 → 解析结果」。存 `*ui.SVG` 或 `error`，用 `sync.Map` 是因为名字
// 集合是封闭的（都是编译进来的资源），而几条并发的画法不冲突、也不需要一把大锁。
var cache sync.Map

// Load 按名字取一张图标，解析过一次就记住。
//
// 名字是上游的「分类/文件名」（`Media/image-line.svg`）。解析不出来、或者解析出来
// 没有尺寸，返回 error——**不是**一个空图标：解析成功但 0×0 画出来是一格空白，
// 而那和「根本没画」在画面上长得一模一样。
func Load(name string) (*ui.SVG, error) {
	if v, ok := cache.Load(name); ok {
		switch got := v.(type) {
		case *ui.SVG:
			return got, nil
		case error:
			return nil, got
		}
	}
	svg, err := parse(name)
	if err != nil {
		cache.Store(name, err)
		return nil, err
	}
	cache.Store(name, svg)
	return svg, nil
}

// Must 是 Load 的启动期那一支：失败当场 panic。
//
// 界面层用它（`icons.go` 的 mustParseIcons）——那些是**随程序编进去的文件**，不是
// 用户装进来的一份资源，它在开发机上就坏掉的唯一原因是「有人在重构里改了名字」，
// 而那种坏在第一次启动就该炸出来。
func Must(name string) *ui.SVG {
	svg, err := Load(name)
	if err != nil {
		panic(err)
	}
	return svg
}

// Source 读一份资源的**原文**（不解析）。
//
// 给「资源本身对不对」那几条测试用：单色性、断的是文件里写了什么颜色，而不是
// 画出来是什么颜色——画出来那个颜色来自主题，永远不会是资源里写死的那一个。
func Source(name string) ([]byte, error) {
	return files.ReadFile(path.Join("icons", name))
}

// parse 读一份资源并解析，顺带拒掉「解析得出但没尺寸」的那种。
func parse(name string) (*ui.SVG, error) {
	b, err := files.ReadFile(path.Join("icons", name))
	if err != nil {
		return nil, fmt.Errorf("iconset: 图标 %q 读不到（icons/%s）：%w", name, name, err)
	}
	s, err := ui.ParseSVG(b)
	if err != nil {
		return nil, fmt.Errorf("iconset: 图标 %q 解析不了（icons/%s）：%w", name, name, err)
	}
	// 尺寸要断：`ui.ParseSVG` 对一份既没有 viewBox 也没有 width/height 的文件
	// 返回 0×0，而那画出来是一格空白——和「根本没画出来」在画面上一模一样。
	if w, h := s.Size(); w <= 0 || h <= 0 {
		return nil, fmt.Errorf("iconset: 图标 %q 没有尺寸（icons/%s 是不是空的）", name, name)
	}
	return s, nil
}
