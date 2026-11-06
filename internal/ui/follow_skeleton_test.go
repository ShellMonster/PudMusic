// 「我的关注」首屏骨架屏的测试（关注页 loading 改版：八行歌曲占位 → 两列关注卡占位）。
//
// # 为什么要专用骨架
//
// 这一格原来的加载态是 `viewLoading` 的八行**曲目**骨架（`skeletonRows` × 64
// 一行竖着排），而数据一到换成的是**两列人像卡**（`pud.UCard`：头像 56、名字
// 16、签名/认证各一行、右侧一枚 32 高的「已关注」药丸）——形状对不上，换
// 那一帧所有行高、列宽一起跳，关注卡自己还有入场过渡，两样在同一帧里抢
// （「首次进入时关注图标叠加/加载卡顿」那一票）。
//
// 所以加载中那一格画的是**和真网格同一个形状**的占位：同样的两列、同样的行
// 高、同样的卡内摆布（左头像、中间两条字条、右侧按钮占位），数据回来只是
// 「灰块换真内容」，版面一步都不挪。
package ui

import (
	"net/http"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	mygoui "github.com/egoist/mygo/ui"
)

// followHoldGrid 把右边那一格的两拉都拦住（`/x/relation/followings` 和
// 分组明细那一支），分组那一列照常回——骨架屏要量的是「右边那一格」，左边
// 得先就位，不然整页的布局都没展开。
//
// 返回释放门（send 之后挂住的那一拉才返回）。搜索那一支也挂同一个门：
// `uiFollowSearch` 那一份只认「命中词」，没匹配到就回一个空列表——那会把
// 加载态提前放掉，而这一组用例量的就是「还没回来」那一帧。
//
// ⚠️ **cleanup 里要主动放行**：不释放的那几条用例会把那一拉一直挂着，
// `httptest.Server.Close`（`newUIExplore` 的 cleanup）会等它——挂住的连接是
// 这一份用例自己要的（量「还没回来」那一帧），不是泄漏；cleanup 往门里发一
// 份空数据，让挂着的 handler 走完、连接收掉，测试才能干净退出。
func followHoldGrid(t *testing.T, b *uiExplore) chan<- []any {
	t.Helper()
	release := make(chan []any, 1)
	wait := func() []any {
		select {
		case users := <-release:
			return users
		case <-t.Context().Done():
			return nil
		}
	}
	b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
		users := wait()
		uiExploreJSON(w, 0, map[string]any{"total": len(users), "list": users})
	})
	b.on("/x/relation/tag", func(w http.ResponseWriter, r *http.Request) {
		wait()
		uiExploreJSON(w, 0, []any{})
	})
	b.on("/x/relation/followings/search", func(w http.ResponseWriter, r *http.Request) {
		wait()
		uiExploreJSON(w, 0, map[string]any{"total": 0, "list": []any{}})
	})
	t.Cleanup(func() {
		select {
		case release <- nil:
		default:
		}
	})
	return release
}

// followSkeletonFinds 找骨架那一格上某个占位件，找不到就报「画出来的是别的」。
func followSkeletonFinds(t *testing.T, tt *mygoui.Tester, label string) mygoui.Rect {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("骨架屏上没有「%s」那一格，文字有 %q", label, tt.Texts())
	}
	return r
}

// TestTheFollowLoadingStateIsCardSkeletons 钉住「关注页在拉的时候画的是关注卡
// 骨架，不是八行歌曲占位、也不是一枚转圈」。
//
// # 判据是画面：头像 / 字条 / 按钮占位都在，而且那一格里没有歌曲行骨架
func TestTheFollowLoadingStateIsCardSkeletons(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	followHoldGrid(t, b)

	s, tt, _ := newSpaceShell(t, b, true, "77", at(1400, 900))
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	if !f.pager.loading {
		t.Fatal("右边那一格没在拉——这一组用例量的是加载态本身")
	}
	tt.Frame()

	followSkeletonFinds(t, tt, "正在加载关注的人…")
	// 头像占位：真卡上是 56 的一方块（`UCard` 的 `Avatar(c, u.Name, 56, …)`）。
	face := followSkeletonFinds(t, tt, "关注占位:0:头像")
	if face.W != 56 || face.H != 56 {
		t.Errorf("头像占位是 %.0f×%.0f，want 56×56（和真卡同一个数）", face.W, face.H)
	}
	// 右侧按钮占位：真卡上是 `pud.Sm` 那一档（`tok.BtnSmH` = 32）。
	btn := followSkeletonFinds(t, tt, "关注占位:0:按钮")
	if btn.H != tok.BtnSmH {
		t.Errorf("按钮占位高 %.0f，want %.0f（和「已关注」那一枚同高）", btn.H, tok.BtnSmH)
	}
	// 整张占位卡不塌：头像 56 + 上下各 16 内边距（`UCard` 的 padding）。
	card := followSkeletonFinds(t, tt, "关注占位:0")
	if card.H < 88 {
		t.Errorf("占位卡高 %.0f，want ≥ 88（56 头像 + 上下各 16）", card.H)
	}
	// 骨架屏里没有八行歌曲占位的那几个块。
	if _, ok := tt.Find("skel:0:t"); ok {
		t.Error("加载态里画着歌曲行骨架（skel:0:t）——关注页要的是关注卡占位")
	}
}

// TestTheFollowSkeletonMatchesTheRealCardLayout 钉住「骨架和真卡同一个摆布」。
//
// # 判据是画面：同一格里，骨架件和真件落在同样的位置
func TestTheFollowSkeletonMatchesTheRealCardLayout(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	release := followHoldGrid(t, b)

	s, tt, _ := newSpaceShell(t, b, true, "77", at(1400, 900))
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	if !f.pager.loading {
		t.Fatal("右边那一格没在拉")
	}
	tt.Frame()

	sCard := followSkeletonFinds(t, tt, "关注占位:0")
	sFace := followSkeletonFinds(t, tt, "关注占位:0:头像")
	sBtn := followSkeletonFinds(t, tt, "关注占位:0:按钮")

	// 骨架卡内部：头像贴左边、按钮占位贴右边（`UCard` 的 1px border + 16 内
	// 边距——头像左缘 = 卡盒左缘 + 1 + 16，和真卡同一个落点）。
	if d := sFace.X - sCard.X; absF(d-17) > 0.5 {
		t.Errorf("头像占位离卡左边 %.1f，want 17（1 边框 + 16 内边距）", d)
	}
	if d := (sCard.X + sCard.W) - (sBtn.X + sBtn.W); absF(d-17) > 0.5 {
		t.Errorf("按钮占位离卡右边 %.1f，want 17", d)
	}
	// 头像和按钮占位都竖向居中。
	if d := absF((sFace.Y + sFace.H/2) - (sCard.Y + sCard.H/2)); d > 1 {
		t.Errorf("头像占位中心偏 %.1f，want ≤1", d)
	}
	if d := absF((sBtn.Y + sBtn.H/2) - (sCard.Y + sCard.H/2)); d > 1 {
		t.Errorf("按钮占位中心偏 %.1f，want ≤1", d)
	}

	// 放人进来：骨架和真卡的位置要**对得上**（同一格、同一条边）。
	release <- []any{uiFollowUser(11, "第一个 UP"), uiFollowUser(12, "第二个 UP")}
	waitFrames(t, tt, "人到了", func() bool { return len(f.users) == 2 })
	tt.Frame()

	real, ok := tt.Find("打开 第一个 UP 的空间")
	if !ok {
		t.Fatalf("第一张真卡没画出来，文字有 %q", tt.Texts())
	}
	if d := absF(real.X - sCard.X); d > 0.5 {
		t.Errorf("真卡左边在 %.1f，骨架在 %.1f——数据一到整格横着挪了", real.X, sCard.X)
	}
	if d := absF(real.Y - sCard.Y); d > 0.5 {
		t.Errorf("真卡顶边在 %.1f，骨架在 %.1f——数据一到整格竖着跳了", real.Y, sCard.Y)
	}
	if _, ok := tt.Find("关注占位:0"); ok {
		t.Error("数据回来了骨架占位还挂着")
	}
	// 真卡的「已关注」药丸和骨架的按钮占位是**同一档高**（`pud.Sm`），而且
	// 都在卡内竖向居中——右缘对不齐是预期的（真卡的按钮底座只到内容宽，
	// 探针实测 `Find("已关注")` 拿到的是药丸里的**字**而不是底座）。
	pill, ok := tt.Find("已关注")
	if !ok {
		t.Fatalf("真卡上找不到「已关注」，文字有 %q", tt.Texts())
	}
	if pill.H > sBtn.H {
		t.Errorf("「已关注」高 %.1f 超过了按钮占位 %.1f——占位比真件还矮",
			pill.H, sBtn.H)
	}
	if d := absF((pill.Y + pill.H/2) - (sCard.Y + sCard.H/2)); d > 8 {
		t.Errorf("「已关注」中心偏 %.1f，want ≤8（和占位一样竖向居中）", d)
	}
}

// TestTheFollowSkeletonGridIsTwoColumns 钉住「骨架网格和真网格同一个形状」：
// 两列、缝 16、行高稳定、相邻占位互不叠压。
func TestTheFollowSkeletonGridIsTwoColumns(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	followHoldGrid(t, b)

	s, tt, _ := newSpaceShell(t, b, true, "77", at(1400, 900))
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	if !f.pager.loading {
		t.Fatal("右边那一格没在拉")
	}
	tt.Frame()

	first := followSkeletonFinds(t, tt, "关注占位:0")
	second := followSkeletonFinds(t, tt, "关注占位:1")
	below := followSkeletonFinds(t, tt, "关注占位:2")

	// 两列：第二格在第一格右边 16。
	if second.Y != first.Y {
		t.Errorf("同一行的两张占位卡不在同一条基线上（%.1f vs %.1f）", first.Y, second.Y)
	}
	if d := second.X - (first.X + first.W); absF(d-followCardGap) > 0.5 {
		t.Errorf("两列之间的缝是 %.1f，want %.0f", d, followCardGap)
	}
	// 行间距 16（`followRowGap`）。
	if d := below.Y - (first.Y + first.H); absF(d-float32(followRowGap)) > 0.5 {
		t.Errorf("两行之间的缝是 %.1f，want %v", d, followRowGap)
	}
	// 行高稳定：同一行两张卡同高，下一行也高一样。
	if second.H != first.H || below.H != first.H {
		t.Errorf("占位卡行高不稳（%.0f / %.0f / %.0f）", first.H, second.H, below.H)
	}
	// 无重叠。
	if first.X+first.W > second.X {
		t.Errorf("第 0 格（右边 %.1f）压到了第 1 格（左边 %.1f）", first.X+first.W, second.X)
	}
	if first.Y+first.H > below.Y {
		t.Errorf("第一行（底 %.1f）压到了第二行（顶 %.1f）", first.Y+first.H, below.Y)
	}
}

// TestTheFollowSkeletonCoversTheFirstViewport 钉住「骨架铺满首屏」：
// 一屏大约四行（用户拍板「差不多 4 行」），两列 × 4 行 = 至少 8 格占位。
func TestTheFollowSkeletonCoversTheFirstViewport(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	followHoldGrid(t, b)

	s, tt, _ := newSpaceShell(t, b, true, "77", at(1400, 900))
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	if !f.pager.loading {
		t.Fatal("右边那一格没在拉")
	}
	tt.Frame()

	followSkeletonFinds(t, tt, "关注占位:6")
	followSkeletonFinds(t, tt, "关注占位:7")
}
