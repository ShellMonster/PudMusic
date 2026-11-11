// 首页主视觉标题的宽度自适应：七字歌名在 1025–1157 这一档窗口里会排成 6+1 的孤字行。
// 这里钉住三件事：放不下就缩到一行、缩不过下限就回到均衡两行、本来就放得下就不缩。
package ui

import (
	"fmt"
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	mygoui "github.com/egoist/mygo/ui"
)

// homeHeroFitTitle 是那条报告的七字歌名：第六个字之后换行，最后一个字孤零零甩在第二行。
const homeHeroFitTitle = "天龙八部之宿敌"

// homeHeroFitWidth 量出这一窗口宽下首页内容区的宽，算式和 `homePage.view` 同一条。
func homeHeroFitWidth(win int) float32 {
	side := float32(tok.SideW)
	if float32(win) <= tok.AutoCollapseBelow {
		side = tok.SideCollapsedW
	}
	bodyW := min(float32(win)-side, float32(homeMaxW))
	return bodyW - 2*homeLayoutFor(float32(win)).pad
}

// heroTitleBox 只画 viewHero 这一块，返回主标题的盒子。
//
// 曲目行里也有同名的文字，所以再找一下艺人行：主标题必须在它上面。
func heroTitleBox(t *testing.T, win int) mygoui.Rect {
	t.Helper()
	view := func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.applyFrameTheme(c)
			s.home.win = float32(win)
			inner := homeHeroFitWidth(win)
			hero := []list.Music{{
				ID: "BV1herofit", Title: homeHeroFitTitle, Owner: "许嵩",
				Item: app.Item{Title: homeHeroFitTitle, Bvid: "BV1herofit"},
			}}
			mygoui.Column(c).Width(inner).FillHeight().Background(tok.Of(c.Theme()).Bg).
				Children(func() { s.home.viewHero(c, s, inner, hero) })
		}
	}
	b := newTestShell(t, at(win, 900), withView(view))
	for range 3 {
		b.tt.Frame()
	}
	if dir := os.Getenv("BIU_SHOT_DIR"); dir != "" {
		save(t, dir, fmt.Sprintf("hero-title-%d.png", win), b.tt)
	}
	title, ok := b.tt.Find(homeHeroFitTitle)
	if !ok {
		t.Fatalf("画面上没有标题 %q，有 %q", homeHeroFitTitle, b.tt.Texts())
	}
	owner, ok := b.tt.Find("许嵩")
	if !ok {
		t.Fatalf("画面上没有艺人行，有 %q", b.tt.Texts())
	}
	if title.Y >= owner.Y {
		t.Fatalf("找到的 %q 在艺人行下面（y=%.1f，艺人 y=%.1f）——那是曲目行里的同名文字，不是主标题",
			homeHeroFitTitle, title.Y, owner.Y)
	}
	return title
}

// TestTheHeroTitleShrinksToStayOnOneLine 钉住「窗口 1100 时七字歌名缩到一行」。
func TestTheHeroTitleShrinksToStayOnOneLine(t *testing.T) {
	title := heroTitleBox(t, 1100)
	// 自然行高下两行的总高；一行必须明显低于它。
	twoLines := 2 * scale.HeroTitleFluid(1100) * typo.DisplayNatural
	if title.H >= 0.75*twoLines {
		t.Errorf("窗口 1100 时标题高 %.1f，两行的 75%% 是 %.1f——它还是两行，没有缩到一行",
			title.H, 0.75*twoLines)
	}
	// 缩也有下限：最多让掉 15%，再小就宁可两行。
	floorLine := 0.85 * scale.HeroTitleFluid(1100) * typo.DisplayNatural
	if title.H < 0.98*floorLine {
		t.Errorf("窗口 1100 时标题高 %.1f，低于缩字号的 85%% 下限 %.1f——缩过头了",
			title.H, 0.98*floorLine)
	}
}

// TestTheHeroTitleFallsBackToTwoLinesBelowTheShrinkFloor 钉住「窗口 1025 时缩不过下限，
// 回到两行」。没有下限的话这里会缩到 39 号左右、一行就放得下。
func TestTheHeroTitleFallsBackToTwoLinesBelowTheShrinkFloor(t *testing.T) {
	title := heroTitleBox(t, 1025)
	twoLines := 2 * scale.HeroTitleFluid(1025) * typo.DisplayNatural
	if title.H < 0.75*twoLines {
		t.Errorf("窗口 1025 时标题高 %.1f，两行的 75%% 是 %.1f——它被缩成了一行，穿过了下限",
			title.H, 0.75*twoLines)
	}
}

// TestTheHeroTitleKeepsItsFluidSizeWhenItFits 钉住「窗口 1440 时本来一行就放得下，不许缩」。
func TestTheHeroTitleKeepsItsFluidSizeWhenItFits(t *testing.T) {
	title := heroTitleBox(t, 1440)
	twoLines := 2 * scale.HeroTitleFluid(1440) * typo.DisplayNatural
	if title.H >= 0.75*twoLines {
		t.Errorf("窗口 1440 时标题高 %.1f，两行的 75%% 是 %.1f——本来就放得下，不该缩成两行",
			title.H, 0.75*twoLines)
	}
}
