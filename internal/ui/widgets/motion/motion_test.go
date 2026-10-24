package motion

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// 这一组是**动效值表**的护栏：曲线和弹簧的值不许漂，
// 而且要跟仓库根 DESIGN.md 的 `motion` 令牌块对得上。
//
// ⚠️ **这两个值是全应用动效的唯一出处**：曲线散在五处调用点上，弹簧散在
// 主 Tab 那四处 `AnimateTo` 上——改了这里，所有用到的地方一起变。

// repoRoot 是仓库根（从本文件的路径往上四级）。
//
// ⚠️ **`runtime.Caller` 在 `-trimpath` 之下给的是 import 路径**（不是文件系统
// 路径），所以还要拿工作目录兜一层：两层都试，从哪一层往上找得到
// 同时带 `go.mod` 和 `DESIGN.md` 的目录就用哪一层（同 `widgets/space` 的做法）。
func repoRoot(t *testing.T) string {
	t.Helper()
	var starts []string
	if _, thisFile, _, ok := runtime.Caller(0); ok && filepath.IsAbs(thisFile) {
		starts = append(starts, filepath.Dir(thisFile))
	}
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	for _, start := range starts {
		for dir := start; ; {
			if isRegularFile(filepath.Join(dir, "go.mod")) &&
				isRegularFile(filepath.Join(dir, "DESIGN.md")) {
				return dir
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	t.Fatalf("找不到仓库根：从 %v 往上都没有同时带 go.mod 和 DESIGN.md 的目录", starts)
	return ""
}

// isRegularFile 报 path 是不是一个存在的普通文件。
func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// TestTheUnifiedCurveIsTheOneWeCopied 把统一曲线的值钉死。
//
// 出处：《回顾: 制作微信广告官网》——「统一选择 `0.8s` 与贝塞尔控制点
// `(0.16, 0.9, 0.21, 0.98)`」（https://wxad.design/abc/wxad）。
//
// ⚠️ **顺带钉住它的形状**：这条曲线是「前重后轻」的（前 50% 的时间走完
// 95.4%），而框架默认的 `EaseOut` 是 1-(1-t)³（前 50% 走完 87.5%）。
// 两者的差别肉眼看得出来——「是不是同一条曲线」这件事，靠的就是这几个数。
func TestTheUnifiedCurveIsTheOneWeCopied(t *testing.T) {
	// 端点必须是端点。
	if got := Unified(0); got != 0 {
		t.Errorf("曲线在 0 处是 %v，want 0", got)
	}
	if got := Unified(1); got != 1 {
		t.Errorf("曲线在 1 处是 %v，want 1", got)
	}
	// 中点：0.954（= 原文说的「前 50% 完成 95%」）。
	if got := Unified(0.5); got < 0.94 || got > 0.97 {
		t.Errorf("曲线在一半处是 %.4f，want 0.954（原文说前 50%% 的时间走完 95%%）", got)
	}
	// 前重后轻：四分之一处已经走了八成多（`EaseOut` 那时只有 57.8%）。
	if got := Unified(0.25); got < 0.80 || got > 0.83 {
		t.Errorf("曲线在四分之一处是 %.4f，want 0.815", got)
	}
	// 单调不减。
	prev := float32(0)
	for i := 1; i <= 100; i++ {
		got := Unified(float32(i) / 100)
		if got < prev {
			t.Fatalf("曲线在 %d%% 处回落了（%v < %v）——它不是一条能用的缓动", i, got, prev)
		}
		prev = got
	}
}

// TestTheTabSlideIsASpring 把主 Tab 滑块那一下的值钉死。
//
// ⚠️ **它是弹簧不是曲线**（`Duration` 必须是 0：Duration > 0 就是曲线那一支，
// 见 `mygo/ui/motion.go` 的 `retarget`）——曲线中途换目标会从当前值重新起速，
// 弹簧保留速度。
func TestTheTabSlideIsASpring(t *testing.T) {
	if TabSlide.Duration != 0 {
		t.Errorf("Tab 滑块有 Duration %v——那一条是曲线，不是弹簧", TabSlide.Duration)
	}
	want := ui.SpringOptions{Stiffness: 260, Damping: 32, Precision: 0.5}
	got := TabSlide.Spring
	if got.Stiffness != want.Stiffness {
		t.Errorf("刚度是 %v，want %v（出处：《使用 AI 制作交互动画的建议》的 demo）",
			got.Stiffness, want.Stiffness)
	}
	if got.Damping != want.Damping {
		t.Errorf("阻尼是 %v，want %v（demo 原值 26 会回弹 2%%，按手册⓪「not springy」上调）",
			got.Damping, want.Damping)
	}
	if got.Precision != want.Precision {
		t.Errorf("精度是 %v，want %v（框架默认的 0.001 会让弹簧多跑 400ms 才收手）",
			got.Precision, want.Precision)
	}
	if got.Mass != 0 {
		t.Errorf("质量是 %v，want 0（框架兜底 1，和 react-spring 一致）", got.Mass)
	}
}

// 下面是**和 DESIGN.md 令牌块对账**的那一条。
//
// ⚠️ **DESIGN.md 是「唯一目标值」**（它自己顶上那句话）：代码和它对不上时，
// 以它为准。所以这张表必须跟它一致——不一致的话，哪一边是错的就说不清了。
//
// 同仓的 `widgets/space` 有一模一样的一条（`TestTheTableMatchesTheDesignTokens`），
// 那是间距那一档的做法；这里是动效那一档的。
//
// 这一条直接读 DESIGN.md 那个 yaml 块，**不另抄一份数**：抄一份的话，两边各改
// 各的，这条测试还是绿的。
//
// ⚠️ **只对账这个包自己拥有的那两条**（`curve` 与 `tab_slider`）：`motion` 块里
// 其余那几行时长（`sidebar_collapse` / `hover_fade` / `toast_in` / …）归
// `internal/ui` 那一层拥有——那个包反过来引了这个包（`widgets/motion`），这里
// 再引它就成环了。
func TestTheTableMatchesTheDesignTokens(t *testing.T) {
	tokens := parseDesignMotion(t, repoRoot(t))

	// ① 曲线：令牌块里那四个控制点必须和代码里那一条是同一条曲线。
	//
	// ⚠️ **不比控制点，比曲线本身**：`Unified` 是个闭包，代码里读不出它的控制
	// 点。所以拿令牌块里的四个数**重新建一条**曲线，逐点比——两条曲线一样，
	// 就说明令牌块写的就是代码里那一条。
	curve, ok := tokens["curve"]
	if !ok {
		t.Fatalf("DESIGN.md 的 motion 一节里没有 curve 令牌")
	}
	ctrl := bezierControl(t, curve)
	fromToken := ui.CubicBezier(ctrl[0], ctrl[1], ctrl[2], ctrl[3])
	for _, at := range []float32{0.1, 0.25, 0.5, 0.75, 0.9} {
		want, got := fromToken(at), Unified(at)
		if want != got {
			t.Errorf("DESIGN.md motion.curve = %s，在 %.2f 处是 %.6f；"+
				"代码里的 Unified 是 %.6f——两边不是同一条曲线", curve, at, want, got)
		}
	}

	// ② Tab 滑块：令牌块要写明是弹簧，而且刚度与阻尼跟代码里一样。
	slide, ok := tokens["tab_slider"]
	if !ok {
		t.Fatalf("DESIGN.md 的 motion 一节里没有 tab_slider 令牌")
	}
	if !strings.Contains(slide, "spring") {
		t.Errorf("DESIGN.md motion.tab_slider = %q——它不是弹簧那一支"+
			"（代码里 Duration=%d、Spring.Stiffness=%v）",
			slide, TabSlide.Duration, TabSlide.Spring.Stiffness)
	}
	for _, w := range []struct {
		name string
		want float64
	}{
		{"stiffness", TabSlide.Spring.Stiffness},
		{"damping", TabSlide.Spring.Damping},
	} {
		got, ok := numberIn(w.name, slide)
		if !ok {
			t.Errorf("DESIGN.md motion.tab_slider = %q——里面没有 %s 的数值", slide, w.name)
			continue
		}
		if got != w.want {
			t.Errorf("DESIGN.md motion.tab_slider 的 %s 是 %v，代码里是 %v——"+
				"两边对不上", w.name, got, w.want)
		}
	}
}

// parseDesignMotion 读 DESIGN.md 里 `motion:` 那一段，返回令牌表。
//
// 值是「值 + 行尾注释」两部分（`tab_slider: spring  # stiffness 260 / damping 32`），
// 这里**两部分都要**：注释里那几个数正是要跟代码对账的。
func parseDesignMotion(t *testing.T, root string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "DESIGN.md"))
	if err != nil {
		t.Fatalf("读 DESIGN.md 失败：%v", err)
	}
	out := map[string]string{}
	inBlock := false
	inMotion := false
	for line := range strings.SplitSeq(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inBlock = !inBlock
			continue
		}
		if !inBlock {
			continue
		}
		if !inMotion {
			if trimmed == "motion:" {
				inMotion = true
			}
			continue
		}
		// 这一段里的令牌都是缩进两格的；顶格的行就是下一段了。
		if trimmed == "" || line == trimmed {
			break
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, val, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		out[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	if len(out) == 0 {
		t.Fatalf("DESIGN.md 里没找到 motion 那一段")
	}
	return out
}

// bezierControl 从 `cubic-bezier(0.16, 0.9, 0.21, 0.98)` 里抠出四个控制点。
func bezierControl(t *testing.T, s string) [4]float32 {
	t.Helper()
	m := regexp.MustCompile(`cubic-bezier\(([^)]*)\)`).FindStringSubmatch(s)
	if m == nil {
		t.Fatalf("motion.curve = %q——认不出 cubic-bezier(...) 的写法", s)
	}
	parts := strings.Split(m[1], ",")
	if len(parts) != 4 {
		t.Fatalf("motion.curve = %q——控制点要四个，只认出 %d 个", s, len(parts))
	}
	var out [4]float32
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			t.Fatalf("motion.curve = %q——第 %d 个控制点 %q 不是数", s, i+1, p)
		}
		out[i] = float32(v)
	}
	return out
}

// numberIn 在一段文字里找 `name 数字` 那个数（`stiffness 260`）。
func numberIn(name, s string) (float64, bool) {
	m := regexp.MustCompile(name + `\s+([0-9.]+)`).FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
