// UP 空间头图跟着滚走（票 #509 / BMG-278）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderUpSpaceScrollShots
//
// # 这一票改的是「头图在不在滚动区里」
//
// 头图 + Tab 条是**当前那一档滚动内容的第 0 行**（见 `viewSpaceHead`），所以
// 滚动时它跟着往下滑，滑够了就整个滑出视野。
//
// 判据是画面：滚一段之后画面上**没有那个 UP 主了**（昵称、头像、横幅都不在）。
package ui

import (
	"net/http"
	"os"
	"strconv"
	"testing"

	"github.com/egoist/mygo/ui"
)

// spaceScrollShell 造一台有动态的假 B 站，开一个 UP 的空间。
//
// ⚠️ **动态要够多才滚得动**：一行两行的列表滚不出 1200 像素（内容不够高，
// 列表压根不滚），而「头图滑不滑走」这件事恰恰要在**滚得动**的时候才验得出来。
func spaceScrollShell(t *testing.T, rows int) (*Shell, *ui.Tester, *upSpace) {
	t.Helper()
	b := newUIExplore(t)
	uiSpaceData(b, "一个会滚走的 UP", 0, 123456)
	items := make([]any, 0, rows)
	for i := range rows {
		items = append(items, uiDynamicWithFace(
			"d"+strconv.Itoa(i), "第 "+strconv.Itoa(i)+" 条动态", "https://f/face-"+strconv.Itoa(i)+".jpg"))
	}
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": items, "offset": "", "has_more": false})
	})

	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	waitFrames(t, tt, "动态加载完", func() bool { return sp.dyn.started && len(sp.dynItems) == rows })
	tt.Frame()
	return s, tt, sp
}

// TestTheSpaceHeaderScrollsAwayWithTheList 钉住这一票的正题：**头图跟着滚走**。
//
// ⚠️ **怎么滚**：滚轮要落在**列表**那一块上（`ui.Tester.Scroll` 给的是指针位置）。
func TestTheSpaceHeaderScrollsAwayWithTheList(t *testing.T) {
	_, tt, _ := spaceScrollShell(t, 12)

	// 滚之前：头图在画面上。
	if !showsText(tt, "一个会滚走的 UP") {
		t.Fatalf("打开空间之后昵称没画出来，文字有 %q", tt.Texts())
	}

	tt.Scroll(700, 600, 0, 1200)
	homeFrame(tt)

	// 滚之后：昵称（也就是头图那一块）不在画面上了。
	if showsText(tt, "一个会滚走的 UP") {
		t.Errorf("滚了 1200 像素之后昵称还在画面上——头图没有跟着滚走，文字有 %q", tt.Texts())
	}
	// 头像也走了（它跟横幅、名字是同一块）。
	if showsText(tt, "https://f/face-0.jpg") {
		t.Errorf("滚了 1200 像素之后头像还在画面上——它没有跟着横幅一起走，文字有 %q", tt.Texts())
	}
	// 而下面那些动态还在（滚的是列表，不是把整页滚没了）。
	if !showsText(tt, "第 0 条动态") && !showsText(tt, "第 11 条动态") {
		t.Errorf("滚了 1200 像素之后一条动态都没画出来，文字有 %q", tt.Texts())
	}
}

// TestTheAvatarAndTheNameGoTogetherWithTheBanner 钉住「头像 + 名字那一行跟着
// 横幅一起走」（上游是**一起**走的）。
//
// 判据是**相对位置**：滚之前头像和昵称差多少，滚之后还差多少——差值变了就是
// 有一块自己跑了。
func TestTheAvatarAndTheNameGoTogetherWithTheBanner(t *testing.T) {
	_, tt, _ := spaceScrollShell(t, 12)

	name, ok := tt.Find("一个会滚走的 UP")
	if !ok {
		t.Fatalf("画面上找不到昵称，文字有 %q", tt.Texts())
	}
	face, ok := tt.Find("https://f/face-0.jpg")
	if !ok {
		t.Fatalf("画面上找不到头像，文字有 %q", tt.Texts())
	}
	before := face.Y - name.Y

	// 滚一小段（不滚到底，两块都还在画面上，才量得到相对位置）。
	tt.Scroll(700, 600, 0, 120)
	homeFrame(tt)

	name2, ok := tt.Find("一个会滚走的 UP")
	if !ok {
		t.Fatalf("滚了 120 像素之后昵称不见了，文字有 %q", tt.Texts())
	}
	face2, ok := tt.Find("https://f/face-0.jpg")
	if !ok {
		t.Fatalf("滚了 120 像素之后头像不见了，文字有 %q", tt.Texts())
	}
	if got := face2.Y - name2.Y; got != before {
		t.Errorf("滚了 120 像素之后头像和昵称的相对位置变了：%v → %v——它们必须一起走",
			before, got)
	}
	// 而且两块都往上走了（不是原地不动）。
	if name2.Y >= name.Y {
		t.Errorf("滚了 120 像素之后昵称没有往上走：y 从 %v 变成 %v", name.Y, name2.Y)
	}
}

// TestEachSpaceTabHasItsOwnHeaderPlace 钉住「切 Tab 时头图在哪儿」。
//
// 口径：**跟着当前那一档的滚动位置走**——切到没滚过的那一张，头图在顶上；
// 切回滚过的那一张，头图还停在滚过的地方（那一档的滚动位置是记着的，
// 票 #370 那一套）。
func TestEachSpaceTabHasItsOwnHeaderPlace(t *testing.T) {
	_, tt, sp := spaceScrollShell(t, 12)

	// 滚这一张（动态）。
	tt.Scroll(700, 600, 0, 1200)
	homeFrame(tt)
	if showsText(tt, "一个会滚走的 UP") {
		t.Fatalf("滚了之后昵称还在画面上，文字有 %q", tt.Texts())
	}

	// 切到「收藏夹」——那一档没滚过，头图该在顶上。
	sp.tab = upTabCollection
	tt.Frame()
	waitFrames(t, tt, "收藏夹加载完", func() bool { return sp.fav.started })
	tt.Frame()
	if !showsText(tt, "一个会滚走的 UP") {
		t.Errorf("切到「收藏夹」之后头图不在画面上——每一档的头图各自在各自的位置，文字有 %q",
			tt.Texts())
	}

	// 切回「动态」——那一档滚过 1200，头图该还在滚过的地方。
	sp.tab = upTabDynamic
	tt.Frame()
	homeFrame(tt)
	if showsText(tt, "一个会滚走的 UP") {
		t.Errorf("切回「动态」之后头图回到顶上来了——那一档的滚动位置没记住（票 #370 那一套），文字有 %q",
			tt.Texts())
	}
}

// TestTheSpaceHeaderScrollsAwayOnTheVideosTabToo 验「投稿」那一张**也一样**。
//
// ⚠️ **那一张是另一个控件**：它走 `list.PagedList`（页内头部是那个包自己的
// `Lead`），而动态 / 收藏夹 / 合集那三张走的是裸的 `ui.List`（头块是那一档
// 的第 0 行）。两套都要跟着滚走。
func TestTheSpaceHeaderScrollsAwayOnTheVideosTabToo(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "一个会滚走的 UP", 0, 123456)
	vlist := make([]any, 0, 20)
	for i := range 20 {
		vlist = append(vlist, map[string]any{
			"bvid": "BV" + strconv.Itoa(i), "title": "第 " + strconv.Itoa(i) + " 个投稿",
			"length": "3:20", "cid": "9",
		})
	}
	b.on("/x/space/wbi/arc/search", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"list": map[string]any{"vlist": vlist},
			"page": map[string]any{"count": 20},
		})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	sp.tab = upTabVideo
	tt.Frame()
	waitFrames(t, tt, "投稿加载完", func() bool { return sp.videos.Paged().Loaded > 0 })
	tt.Frame()

	if !showsText(tt, "一个会滚走的 UP") {
		t.Fatalf("切到「投稿」之后头图不在画面上，文字有 %q", tt.Texts())
	}

	tt.Scroll(700, 600, 0, 1200)
	homeFrame(tt)

	if showsText(tt, "一个会滚走的 UP") {
		t.Errorf("「投稿」那一张滚了 1200 像素之后昵称还在画面上——头图没有跟着滚走，文字有 %q",
			tt.Texts())
	}
}

// TestRenderUpSpaceScrollShots 把「滚前 / 滚后」两帧存成 PNG。
//
// ⚠️ **出图是材料，审查才是那道门**（AGENTS.md §1）：对着这两张图要核五项——
// 布局、间距、层级、密度、对齐；间距要量，不要目测。
func TestRenderUpSpaceScrollShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	_, tt, _ := spaceScrollShell(t, 12)
	homeFrame(tt)
	save(t, dir, "UP空间-动态-滚前.png", tt)

	tt.Scroll(700, 600, 0, 120)
	homeFrame(tt)
	save(t, dir, "UP空间-动态-滚后.png", tt)

	tt.Scroll(700, 600, 0, -120)
	homeFrame(tt)

	// 「投稿」那一张（另一个控件：页内头部是 `widgets/list` 那个包的 `Lead`）。
	s, tt2, sp := spaceScrollShell(t, 12)
	_ = s
	sp.tab = upTabVideo
	tt2.Frame()
	homeFrame(tt2)
	save(t, dir, "UP空间-投稿-滚前.png", tt2)
}
