package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestEveryHandMadePopupHasAShadow 钉住「浮层阴影统一」。
//
// 框架的 `stylePanel` 给浮层的是一套四件（圆角 / 底 / 描边 / 阴影），而我们用
// `SelectBase` / `PopoverBase` 自己拼面板的几处**只抄了前三件**——于是同一屏上
// 框架自己弹的浮层有影子、我们抄的那几个没有。
//
// 判据是代码：自己拼的面板必须走 `popupPanel`（`popup.go`）。这一层验不了像素
// ——阴影是一圈往外淡的模糊，采样点差几个像素就量不到；而「有没有调那个出口」
// 是一句能对上号的事实。
func TestEveryHandMadePopupHasAShadow(t *testing.T) {
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range matches {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		text := string(src)
		// 自己拼面板的两个入口：下拉的 `sel.Popup(func(panel ...` 和浮层的
		// `PopoverBase(c, ..., func(panel ...`。框架自带的 `ui.Popover` 不在此列
		// （`stylePanel` 里已经有阴影）。
		handMade := strings.Contains(text, ".Popup(func(") ||
			strings.Contains(text, "PopoverBase(c,")
		if handMade && !strings.Contains(text, "popupPanel(") {
			t.Errorf("%s 自己拼了一个浮层面板，却没有走 `popupPanel`——"+
				"漏掉的那件是阴影（框架的 `stylePanel` 有）", name)
		}
	}
}

// TestTheFontListComesFromTheSystem 钉住「字体名单是框架枚举出来的」。
//
// 这一条对应的是一句**说错了的注释**：设置页原来写着「MyGo v0.5.5 没有枚举
// 字体的 API」，于是名单是手挑的十条。实际上 `ui.FontFamilies()` 一直在
// （`mygo/ui/window.go:66`）。名单现在必须是系统报上来的那些族。
func TestTheFontListComesFromTheSystem(t *testing.T) {
	opts := fontOptionList()
	if len(opts) < 2 {
		t.Fatalf("字体名单只有 %d 条——系统枚举没有接上（这台机器上应该有几十上百条）", len(opts))
	}
	if got := opts[0].family; got != systemFontFamily {
		t.Errorf("第一条是 %q，want 系统默认 %q", got, systemFontFamily)
	}

	system := map[string]bool{}
	for _, f := range ui.FontFamilies() {
		system[f.Name] = true
	}
	if len(system) == 0 {
		t.Fatal("`ui.FontFamilies()` 一条都没报——这一版框架或这台机器不支持")
	}
	for _, opt := range opts[1:] {
		if !system[opt.family] {
			t.Errorf("名单里的 %q 不在系统报上来的那批里——名单还是写死的？", opt.family)
		}
		if opt.label != opt.family {
			t.Errorf("%q 的标签是 %q，want 族名（上游那个下拉显示的就是系统给的名字）",
				opt.family, opt.label)
		}
	}

	// `system-ui` 不许在系统那一批里再出现一次：同一个值出现两次的话点第二条
	// 得到的还是第一条，看上去像「点了没反应」。
	for _, opt := range opts[1:] {
		if strings.EqualFold(opt.family, systemFontFamily) {
			t.Errorf("系统枚举里又有了一条 %q，会和「系统默认」撞值", opt.family)
		}
	}
}
