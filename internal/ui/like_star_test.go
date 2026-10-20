// 播放条上的那个赞星标：亮了没有、点了会不会真的发出去、三连之后它和收藏那一键
// 有没有跟着 B 站的答案变。
package ui

import (
	"strconv"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// ---- 点赞星标 ----

// TestTheStarIsLitOnlyAfterTheServerSaysSo 钉住「点赞后播放条星标变亮」那一半。
//
// 「问回来之前」也断了一段：界面上显示的是「没赞」，而那**不是**用户已经知道的
// 事实（用户知道自己赞过）。锁屏解锁、切歌那一瞬，星标暗一下再亮是可接受的；
// 而**问都没问就显示「已赞」**是把一个我们猜的答案当事实说。
func TestTheStarIsLitOnlyAfterTheServerSaysSo(t *testing.T) {
	e := newLikeEnv(t, true, false, false)

	if _, ok := e.tt.Find("已赞"); !ok {
		t.Errorf("B 站说赞过了，星标却没亮，文字有 %q", e.tt.Texts())
	}
	if _, ok := e.tt.Find("赞"); ok {
		t.Errorf("赞过了还摆着「赞」这个键，文字有 %q", e.tt.Texts())
	}
}

// TestClickingTheStarLikesTheSong 断的是「点一下真的会发出去」。
//
// 星标是**两样东西**：画面上的一个字，和发出去的那一次 POST。第一版完全可以只做
// 前一样（把字换成「已赞」就完事），而用户点了没反应。
func TestClickingTheStarLikesTheSong(t *testing.T) {
	e := newLikeEnv(t, false, false, false)

	if _, ok := e.tt.Find("赞"); !ok {
		t.Fatalf("没赞的时候播放条上没有那个键，文字有 %q", e.tt.Texts())
	}
	if err := e.tt.Click("赞"); err != nil {
		t.Fatalf("点不了星标：%v（文字：%q）", err, e.tt.Texts())
	}

	// ⚠️ **等的是「那一趟发出去没有」这个状态**，不是画面——所以用 `waitFor`
	// （见 shelltest_test.go 的「两种等法」）。`waitFrames` 也能等，但那个 10 秒
	// 是**墙钟**而它每一轮都在**画一帧**（整个窗口重画一遍）：满载的机器上十秒的
	// 预算全花在重画上。
	waitFor(t, "赞发出去了", func() bool { return e.f.likePostsCount() > 0 })
	if got := e.f.lastLikeUp(); got != "1" {
		t.Errorf("发出去的 up 是 %q，want 1（1 是赞，2 是取消赞）", got)
	}
	// 乐观：点完那一帧就亮了，不用等网络。
	e.tt.Frame()
	if _, ok := e.tt.Find("已赞"); !ok {
		t.Errorf("点完星标还是暗的，文字有 %q", e.tt.Texts())
	}
}

// TestTheLikeRowFitsInThePlaybar 钉住「多摆一个赞字键，别的没被挤坏」。
//
// 🔴 两条都是 BMG-103 踩过的坑，加键之后要重新钉一遍：
//
//	歌名那一行被挤成竖条  左边那一块只有 `Grow(1)`，歌名和第二行吃的是**同一块**
//	                    宽度。而**挤没了之后界面上看不出坏了**——字还在那儿，只是
//	                    一个字一行（`playbar_meta_test.go` 的注释里有完整的事故）
//	键跑出窗口          三个键都是写死的宽度，最后一个跑出去会被 88 高的播放条裁掉
//
// 宽度取 760（比窗口下限 900 还窄）到 1200。UP 名给的是最长的那个（见 `likeItem`）。
func TestTheLikeRowFitsInThePlaybar(t *testing.T) {
	for _, width := range []int{760, 900, 1000, 1200} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			e := newLikeEnvSize(t, false, false, false, width)

			for _, label := range []string{"赞", "收藏", "下载"} {
				r, ok := e.tt.Find(label)
				if !ok {
					t.Fatalf("宽度 %d：%q 不在画面上，文字有 %q", width, label, e.tt.Texts())
				}
				if r.X < 0 || r.X+r.W > float32(width) {
					t.Errorf("宽度 %d：%q 画在 x=%.1f..%.1f，跑出窗口了",
						width, label, r.X, r.X+r.W)
				}
				if r.Y+float32(likeWinH-playbarHeight) < 0 || r.Y+r.H > float32(likeWinH) {
					t.Errorf("宽度 %d：%q 画在 y=%.1f..%.1f，跑出播放条了",
						width, label, r.Y, r.Y+r.H)
				}
			}

			title, ok := e.tt.Find("要赞的那一首")
			if !ok {
				t.Fatalf("宽度 %d：歌名不在画面上，文字有 %q", width, e.tt.Texts())
			}
			if title.W < 40 || title.W < title.H {
				t.Errorf("宽度 %d：歌名只剩 %.1f×%.1f，已经被挤成竖条了",
					width, title.W, title.H)
			}
		})
	}
}

// TestThereIsNoStarForALocalSong 钉住「本地文件不摆星标」。
//
// B 站那三个接口一个都不收本地文件（它压根没有投稿）。摆一个点了没反应的键，
// 比不摆更让人以为它坏了——Biu 那边也是 `playItem.source !== "local"` 才摆。
func TestThereIsNoStarForALocalSong(t *testing.T) {
	s, tt := newShell(t)
	tt.SetDark(true)
	if err := s.app.Enqueue(app.LocalItem(flacPath(t))); err != nil {
		t.Fatalf("放本地文件：%v", err)
	}
	tt.Frame()
	if !s.app.Snapshot().HasTrack {
		t.Fatalf("歌没开起来：%q", s.app.Snapshot().Err)
	}
	tt.Frame()
	if _, ok := tt.Find("赞"); ok {
		t.Errorf("本地文件的播放条上摆着星标，文字有 %q", tt.Texts())
	}
}

// ---- 三连之后 ----

// TestTheTripleLightsTheStarAndTheFavorite 钉住「三连成功后收藏状态同步更新」。
//
// 长按那一把顺手收藏了这一首（B 站的回包里有 `fav` 那一栏），所以**两个键**都要
// 跟着变。只变星标的话，用户在三连之后完全看不出收藏夹里多了一首。
//
// 量的是**画面**：星标换了词（好量），而收藏那一键只有颜色变了
// （它是「收藏」两个字，一直都在那儿）——所以量的是那个键自己的框里有没有像素
// 变掉。
func TestTheTripleLightsTheStarAndTheFavorite(t *testing.T) {
	e := newLikeEnv(t, false, false, true)
	favBox, ok := e.tt.Find("收藏")
	if !ok {
		t.Fatalf("播放条上没有收藏那个键，文字有 %q", e.tt.Texts())
	}
	before := cloneImage(e.tt.Image())

	holdCover(t, e)
	pt := coverPoint2(t, e.tt)
	advanceLike(e, biuLikeLongPressMS*time.Millisecond+200*time.Millisecond)
	e.tt.Release(pt[0], pt[1])

	waitFrames(t, e.tt, "星标亮了", func() bool {
		e.tt.Frame()
		_, ok := e.tt.Find("已赞")
		return ok
	})
	if n := diffIn(favBox, before, e.tt.Image()); n == 0 {
		t.Error("三连收藏了这一首，而「收藏」那一键一个像素都没变")
	}
}

// TestAPartialTripleSaysWhichHalfFailed 钉住「只做成一部分要说出来」。
//
// B 站会只做成一部分：硬币不够（那是**用户的钱**）、这一首投过了、风控拦了。
// 这时候界面上星标亮了、收藏没亮——用户看到的是「好像成了」，而那一枚币没花出去
// （或者花出去了而界面不说）。
//
// 断的是**那句话**：它必须点名「投币」，而不是笼统一句「三连成功」。
func TestAPartialTripleSaysWhichHalfFailed(t *testing.T) {
	e := newLikeEnv(t, false, false, false)
	// 赞成了，投币和收藏都没成。
	e.f.setTriple(map[string]any{"like": true, "coin": false, "fav": false})

	holdCover(t, e)
	pt := coverPoint2(t, e.tt)
	advanceLike(e, biuLikeLongPressMS*time.Millisecond+200*time.Millisecond)
	e.tt.Release(pt[0], pt[1])

	waitFrames(t, e.tt, "那句话弹出来了", func() bool {
		e.tt.Frame()
		return e.tt.HasText("投币")
	})
}

// TestTheWholeTripleIsSilent 钉住「三样都成了就不说话」。
//
// 成功时那句话是多余的：星标亮了、收藏那一键也亮了，那就是答复。Biu 那边成功
// 时也不弹提示。一条总弹的「三连成功」会把失败那一条淹掉。
func TestTheWholeTripleIsSilent(t *testing.T) {
	e := newLikeEnv(t, false, false, true)

	holdCover(t, e)
	pt := coverPoint2(t, e.tt)
	advanceLike(e, biuLikeLongPressMS*time.Millisecond+200*time.Millisecond)
	e.tt.Release(pt[0], pt[1])

	waitFrames(t, e.tt, "星标亮了", func() bool {
		e.tt.Frame()
		_, ok := e.tt.Find("已赞")
		return ok
	})
	e.tt.Frame()
	for _, unwanted := range []string{"没成功", "一键三连没成功"} {
		if e.tt.HasText(unwanted) {
			t.Errorf("三样都成了还弹了 %q，文字有 %q", unwanted, e.tt.Texts())
		}
	}
}

// TestAFailedTripleLeavesEverythingAlone 钉住「失败时什么都不改」。
//
// 三连失败时界面上要是留下一句「已赞」，用户会以为赞成功了；而他手上什么都没有。
func TestAFailedTripleLeavesEverythingAlone(t *testing.T) {
	e := newLikeEnv(t, false, false, true)
	e.f.failTriple()

	holdCover(t, e)
	x, y := coverPoint(t, e.tt)
	advanceLike(e, biuLikeLongPressMS*time.Millisecond+200*time.Millisecond)
	e.tt.Release(x, y)

	waitFrames(t, e.tt, "三连失败那句话弹出来了", func() bool {
		e.tt.Frame()
		return e.tt.HasText("一键三连没成功")
	})
	e.tt.Frame()
	if _, ok := e.tt.Find("已赞"); ok {
		t.Errorf("三连失败了星标还是亮的，文字有 %q", e.tt.Texts())
	}
}
