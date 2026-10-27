// 「点一首歌」在**真页面**上的端到端行为（票 #608）。
//
// 这一份验的是**用户那一次点击**：在 UP 主空间的「投稿」Tab 里，先把整页加进
// 播放列表（「全部播放」），再双击其中一首——队列必须还是原来那 112 首（不是被
// 换成「从这一首往后」的 54 首），而且当前项要指到点的那一首。
//
// ⚠️ **为什么要 E2E 而不是只测 app 层**：`widgets/list` 那一层只认 `Actions`
// 的桩，桩接错了（比如双击还接在 `Play` 上）单测照样绿——只有从页面的行一路走到
// 队列，才看得见「双击到底清了没清队列」。
//
// ⚠️ **全部走内存里的假 B 站**（`uiExplore`），一条真接口都不打（AGENTS.md §6）。
package ui

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// upSpaceManyVideos 装一条「有 total 首投稿」的假投稿接口。
//
// 每页 30 条（`bili.UpVideoDefaultSize`），和真接口一个形状——翻页那一条
// （`pn` / `ps`）也照着真的解，不然一次加载就把 112 首全给了，E2E 就测不到
// 真实的分页行为。
func upSpaceManyVideos(b *uiExplore, total int) {
	b.on("/x/space/wbi/arc/search", func(w http.ResponseWriter, r *http.Request) {
		page := 1
		fmt.Sscan(r.URL.Query().Get("pn"), &page)
		size := 30
		fmt.Sscan(r.URL.Query().Get("ps"), &size)
		if size <= 0 {
			size = 30
		}
		start := (page - 1) * size
		vlist := []any{}
		for i := start; i < total && i < start+size; i++ {
			vlist = append(vlist, map[string]any{
				"bvid":   fmt.Sprintf("BV%03d", i+1),
				"title":  fmt.Sprintf("投稿 %d", i+1),
				"length": "3:20",
				"cid":    fmt.Sprintf("%d", 1000+i),
			})
		}
		uiExploreJSON(w, 0, map[string]any{
			"list": map[string]any{"vlist": vlist},
			"page": map[string]any{"count": total},
		})
	})
}

// loadEveryUpSubmission 把「投稿」Tab 的每一页都拉下来（模拟用户滑到底）。
//
// 返回加载完的列表。加载不满 total 就是测试自己没摆好，当场停下。
func loadEveryUpSubmission(t *testing.T, tt interface {
	Frame()
	Scroll(x, y, dx, dy float32)
}, sp *upSpace, total int) {
	t.Helper()
	for i := 0; i < 400; i++ {
		tt.Frame()
		p := sp.videos.Paged()
		if p.Loaded >= total || (!p.Loading && !p.HasMore) {
			break
		}
		tt.Scroll(400, 400, 0, 4000)
	}
	if got := len(sp.videos.Items()); got != total {
		t.Fatalf("列表里加载出了 %d 首，want %d", got, total)
	}
}

// doubleClickSongRow 把第 n 首（1 起，和界面上那个序号一致）滚进画面并双击。
//
// ⚠️ **滚的是「列表的行号」**：页内头部占着第 0 行（`list.Lead`），所以第 n 首
// 在列表里的行号是 n。滚完等它真的画出来（盒子宽高非 0）再点——虚拟列表对没进
// 画面的行也给一句文字，但盒子是 0×0，点一个 0×0 的盒子点不到任何东西。
func doubleClickSongRow(t *testing.T, tt interface {
	Frame()
	ClickAt(x, y float32)
	Find(s string) (ui.Rect, bool)
	Texts() []string
}, sp *upSpace, n int) {
	t.Helper()
	title := fmt.Sprintf("投稿 %d", n)
	// 页内头部那一行 + 前面 n-1 首 → 第 n 行。
	sp.videos.List().ScrollTo(n, ui.Start)
	for i := 0; i < 40; i++ {
		tt.Frame()
		if r, ok := tt.Find(title); ok && r.W > 0 && r.H > 0 {
			tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
			tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
			tt.Frame()
			tt.Frame()
			return
		}
	}
	t.Fatalf("滚不到「%s」那一行（画面：%q）", title, tt.Texts())
}

// TestDoubleClickingAnUpSubmissionKeepsThePlaylist 是这一票的 E2E 钉子。
//
// 复现用户报的那条路：
//
//  1. UP 主空间「投稿」Tab，滑到底把 112 首全加载出来；
//  2. 按「全部播放」——队列里是 112 首；
//  3. 回到列表里双击第 59 首；
//  4. 队列**还是 112 首**（旧行为是 54 首），而且当前项是第 59 首。
func TestDoubleClickingAnUpSubmissionKeepsThePlaylist(t *testing.T) {
	const total = 112
	b := newUIExplore(t)
	uiSpaceData(b, "投稿多的 UP", 0, 1)
	upSpaceManyVideos(b, total)

	s, tt, a := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	sp.tab = upTabVideo
	tt.Frame()

	// ① 滑到底，把每一页都拉下来。
	loadEveryUpSubmission(t, tt, sp, total)

	// ② 「全部播放」：队列里应该正好是这一页的 112 首。
	//
	// ⚠️ **先滚回顶上**：按钮在页内头部那一块，滑到底之后就滚出画面了（盒子
	// 之外点不到）。这一句也是「全部播放」这个入口在真页面上的可达性检查。
	sp.videos.ScrollTop()
	for i := 0; i < 6; i++ {
		tt.Frame()
	}
	if err := tt.Click(playAllLabel); err != nil {
		t.Fatalf("点「%s」：%v（画面：%q）", playAllLabel, err, tt.Texts())
	}
	waitFor(t, "队列装进这一页的 112 首", func() bool { return a.Queue().Len() == total })

	// ③ 双击第 59 首那一行。
	doubleClickSongRow(t, tt, sp, 59)

	// ④ 队列还是 112 首——这一条就是用户报的那个 bug 的反面。
	if got := a.Queue().Len(); got != total {
		t.Fatalf("双击第 59 首之后队列剩 %d 首，want 还是 %d 首（点一首歌不许把队列换成「从这一首往后」）",
			got, total)
	}

	// ⑤ 当前项是点的那一首（「投稿 59」是第 59 首，下标 58）。
	cur := a.Queue().CurrentIndex()
	items := a.Queue().Items()
	if cur < 0 || cur >= len(items) {
		t.Fatalf("当前项是 %d，队列里只有 %d 首", cur, len(items))
	}
	if got := items[cur].Title; got != "投稿 59" {
		t.Errorf("当前项是第 %d 首（%q），want 「投稿 59」", cur+1, got)
	}
	if got := items[cur].Bvid; got != "BV059" {
		t.Errorf("当前项是 %q，want BV059", got)
	}
}

// TestDoubleClickingASongOutsideTheQueueAppendsIt 是上面那一条的另一半：
// 点一首**不在队列里**的，队列变成 101 首（追加），不是替换。
func TestDoubleClickingASongOutsideTheQueueAppendsIt(t *testing.T) {
	const total = 112
	b := newUIExplore(t)
	uiSpaceData(b, "投稿多的 UP", 0, 1)
	upSpaceManyVideos(b, total)

	s, tt, a := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	sp.tab = upTabVideo
	tt.Frame()

	loadEveryUpSubmission(t, tt, sp, total)
	all := sp.videos.Items()

	// 只把前 100 首放进队列（模拟「这一批已经加过了，后面几首还没加」）。
	first := make([]app.Item, 0, 100)
	for _, m := range all[:100] {
		first = append(first, m.Item)
	}
	if err := a.PlayFrom(first...); err != nil {
		t.Fatalf("先把前 100 首放进队列：%v", err)
	}
	if got := a.Queue().Len(); got != 100 {
		t.Fatalf("先摆的那一批是 %d 首，want 100", got)
	}

	// 点第 105 首（它不在队列里）。
	doubleClickSongRow(t, tt, sp, 105)

	if got := a.Queue().Len(); got != 101 {
		t.Fatalf("点一个不在队列里的投稿之后队列是 %d 首，want 101 首（原来 100 首 + 追加这一首）", got)
	}
	q := a.Queue().Items()
	if got := q[len(q)-1].Title; got != "投稿 105" {
		t.Errorf("队尾是 %q，want 「投稿 105」", got)
	}
}
