package ui_test

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 播放列表抽屉的验收：行高 64、按判重标识去重、「定位当前播放」平滑滚过去、
// 行菜单那五项。
//
// 窗口是 760×460（winW / winH），抽屉是 384 宽、到播放条上沿为止，所以：
// y < 372 是抽屉那一层（点它关抽屉），y >= 372 是播放条（照常用）。
//
// 「定位当前播放」那一条钉的是**平滑**：钟没走的时候那一行还看不见，走够了才
// 看见。用的是 Tester 那只站着不动的钟（`Advance` 才推进），所以「立刻就滚到位」
// 和「滚了半天还在原地」都断得出来——单看最终位置的话，一跳到位和平滑滚过去
// 长得一模一样。

// onlineSong 是造一个在线条目用。标题唯一，免得几行在画面上分不清。
func onlineSong(bvid, title string) app.Item {
	return app.Item{Source: "online", Type: "mv", Bvid: bvid, Cid: "1", Title: title}
}

// localSong 造一个**真的放得出来**的本地项：歌名就是文件名去掉后缀（所以起名
// 就是选断言里用的那个字）。
//
// 要真能放，是因为有一批用例断的就是「这一项真的跳过去放了」——用在线条目的话，
// 这个壳里没挂取流，`PlayAt` 会打不开然后把下标退回去，那条断言就成了永远绿。
func localSong(t *testing.T, dir, name string) app.Item {
	t.Helper()
	return app.LocalItem(writeCopy(t, dir, name))
}

// queueEmptyHint 是抽屉空着时那一句话（queue_drawer.go 的 queueEmpty.Hint）。
//
// 这里写死一份是故意的：那句话是**给人看的**，改文案就该顺手改这里，而不是
// 让测试跟着代码走、什么文案都判「在」。
const queueEmptyHint = "点歌右边的「加入队列」，或者用播放条上的「加歌」放几个文件进来"

// writeCopy 把 FLAC 素材复制成 dir/name 这个文件，返回完整路径。dir 不存在就建。
func writeCopy(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(flac(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// pixelAt 是画面上 (x, y) 那一点的颜色，用来断「正在放的那一行换了底色」。
func pixelAt(img *image.RGBA, x, y float32) color.RGBA {
	px := int(x)
	py := int(y)
	if px < 0 || py < 0 || px >= img.Rect.Dx() || py >= img.Rect.Dy() {
		return color.RGBA{}
	}
	return img.RGBAAt(px, py)
}

// putCurrent 把队列的当前项落在第 n 首，**不放歌**。
//
// 走 `Move` 是因为队列还没有别的公开办法只挪下标：还没开始（cur = -1）时
// `Move` 正向一律落在第 0 首，所以先挪一格再挪到位。
func putCurrent(t *testing.T, a *app.App, n int) {
	t.Helper()
	q := a.Queue()
	q.Move(1)
	q.Move(n)
	if got := q.CurrentIndex(); got != n {
		t.Fatalf("当前项落在第 %d 首，want %d", got, n)
	}
}

// countRows 是抽屉里画着几行。
//
// 数的是每行末尾那颗「⋯」：**别去数歌名**。一行会报出两次它的歌名——一次是那一
// 行自己的无障碍名字（ListState.Label 给的），一次是行里那个 Text——数歌名
// 得到的是行数的两倍，看起来像去重没做。
//
// ⚠️ 数的是**画出来的**行。列表只建看得见的那几行（虚拟滚动），所以这一条只
// 拿来数「视线之内那几首」，队列要更长的话得先滚。
func countRows(h *harness) int {
	n := 0
	for _, x := range h.tt.Texts() {
		if x == "⋯" {
			n++
		}
	}
	return n
}

// openDrawer 点播放条上的那个键把抽屉拉出来，顺手确认它真的开了。
func openDrawer(t *testing.T, h *harness) {
	t.Helper()
	if err := h.tt.Click("播放列表"); err != nil {
		t.Fatalf("点不了播放列表：%v（画面上的字：%q）", err, h.tt.Texts())
	}
	h.tt.Frame()
	if !h.tt.HasText("播放列表") {
		t.Fatalf("点了播放列表，抽屉没开（画面上的字：%q）", h.tt.Texts())
	}
}

func TestQueueDrawerOpensFromThePlaybar(t *testing.T) {
	h := newHarness(t)

	if h.tt.HasText("暂无内容") {
		// 抽屉没开的时候画面上本来就有别的空状态（「先在顶栏「打开文件」放几首
		// 进来」那一句），所以不能只断这个字。
		if h.tt.HasText(queueEmptyHint) {
			t.Fatal("抽屉还没点就开着了")
		}
	}
	openDrawer(t, h)

	// 空队列：两个键不摆（Biu 的 `Boolean(pureList?.length) && …`），摆一句空状态。
	if h.tt.HasText("定位当前播放") {
		t.Error("队列是空的还摆着「定位当前播放」")
	}
	if !h.tt.HasText(queueEmptyHint) {
		t.Errorf("空的抽屉没有那句话，文字有 %q", h.tt.Texts())
	}

	// Esc 关掉。少这一条的表现是「按了 Esc 什么都没发生」。
	h.tt.Key(0, mygoui.KeyEscape)
	h.tt.Frame()
	if h.tt.HasText(queueEmptyHint) {
		t.Error("按了 Esc 抽屉还开着")
	}
}

func TestQueueDrawerClosesOnAClickOutside(t *testing.T) {
	h := newHarness(t)
	enqueue(t, h, copiesOf(t, 3)...)
	openDrawer(t, h)

	h.tt.ClickAt(40, 120) // 抽屉那一层上、面板左边
	h.tt.Frame()
	if h.tt.HasText("定位当前播放") {
		t.Error("点了抽屉外面，抽屉还开着")
	}
}

// TestQueueDrawerLeavesThePlaybarUsable 钉住抽屉那一层**不盖播放条**。
//
// 差一格的表现是「按了下一首，抽屉跟着关了」——用户明明只想换歌。
// Biu 那边是 `data-[placement=right]:mb-22`，88 就是播放条的高。
func TestQueueDrawerLeavesThePlaybarUsable(t *testing.T) {
	h := newHarness(t)
	enqueue(t, h, copiesOf(t, 2)...)
	openDrawer(t, h)

	if err := h.tt.Click("下一首"); err != nil {
		t.Fatalf("抽屉开着的时候点不了「下一首」：%v", err)
	}
	waitFor(t, 2*time.Second, "下一首", func() bool {
		h.tt.Frame()
		return h.app.Snapshot().Title == "b"
	})
	if !h.tt.HasText("定位当前播放") {
		t.Error("点了一下播放条，抽屉跟着关了")
	}
}

func TestQueueDrawerListsEverySongOnce(t *testing.T) {
	h := newHarness(t)
	h.app.Queue().Append(
		onlineSong("BV1", "第一首"),
		onlineSong("BV2", "第二首"),
		onlineSong("BV1", "第一首"), // 同一个投稿排了两遍
	)
	h.tt.Frame()
	openDrawer(t, h)

	if got := countRows(h); got != 2 {
		t.Errorf("抽屉里画着 %d 行，want 2（同一首排了两遍，判重标识要去掉一个）", got)
	}
	// 顶上那个计数也是去重之后的（Biu 的 `pureList?.length`）。
	if !h.tt.HasText("(2)") {
		t.Errorf("顶上写的不是去重之后的数量 (2)，文字有 %q", h.tt.Texts())
	}
}

// TestQueueDrawerTakesLocalSongsFromTheirPath 判重标识的三种拼法里，本地那一支
// 是拿路径当键的（`local:<完整路径>`）。两个不同的文件长成同一个歌名也不能并成
// 一行——那是两首不同的歌。
func TestQueueDrawerTakesLocalSongsFromTheirPath(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	twinA := writeCopy(t, dir, "同名.flac")
	twinB := writeCopy(t, dir+"-别的目录", "同名.flac")
	h.app.Queue().Append(app.LocalItem(twinA), app.LocalItem(twinB))
	h.tt.Frame()
	openDrawer(t, h)

	if got := countRows(h); got != 2 {
		t.Errorf("两个同名文件在抽屉里合成了一行（画着 %d 行），want 2（本地项的键是完整路径）", got)
	}
}

// TestQueueDrawerRowIsSixtyFourTall 钉住行高 64。
//
// 「定位当前播放」滚到的是 `targetIndex * 64`，行高不是 64 的话那个乘法算出来
// 的就不是这一行的顶边——而这里量到的**是行元素自己的框**（那一行还带着
// ListState.Label 给的无障碍名字，`Tester.Find` 按字找回来就是它）。
func TestQueueDrawerRowIsSixtyFourTall(t *testing.T) {
	h := newHarness(t)
	h.app.Queue().Append(onlineSong("BV1", "第一首"), onlineSong("BV2", "第二首"))
	h.tt.Frame()
	openDrawer(t, h)

	r, ok := h.tt.Find("第二首")
	if !ok {
		t.Fatalf("画面上找不到第二首，文字有 %q", h.tt.Texts())
	}
	if r.H != 64 {
		t.Errorf("一行高 %v，want 64", r.H)
	}
	if r.W != 383 {
		t.Errorf("一行宽 %v，want 383（抽屉 384 减去左边那一线）", r.W)
	}
}

func TestQueueRowClickPlaysThatSong(t *testing.T) {
	h := newHarness(t)
	enqueue(t, h, copiesOf(t, 3)...)
	if got := h.app.Snapshot().Title; got != "a" {
		t.Fatalf("放的是 %q，want a", got)
	}
	openDrawer(t, h)

	if err := h.tt.Click("c"); err != nil {
		t.Fatalf("点不了第三行：%v（文字：%q）", err, h.tt.Texts())
	}
	waitFor(t, 2*time.Second, "跳到第三首", func() bool {
		h.tt.Frame()
		return h.app.Snapshot().Title == "c"
	})
	if got := pathsOfItems(h.app.Queue().Items()); len(got) != 3 {
		t.Errorf("点一行把队列换掉了，剩 %d 首，want 还是 3 首", len(got))
	}
}

// TestQueueDrawerMarksTheSongBeingPlayed 钉住「正在放的那首」是按**判重标识**
// 认的，不是按队列下标。
//
// 队列里同一首排了两遍时，正在放的可能是第二条，而抽屉只留第一条——按下标认
// 出来的高亮会落在别的歌上。
func TestQueueDrawerMarksTheSongBeingPlayed(t *testing.T) {
	h := newHarness(t)
	first := onlineSong("BV1", "重复的那首")
	h.app.Queue().Append(
		onlineSong("BV2", "另一首"),
		first,
		first, // 同一首排了两遍
	)
	putCurrent(t, h.app, 2) // 正在放的是队列里的第三条（抽屉里的第一行）
	h.tt.Frame()
	openDrawer(t, h)

	if h.app.Queue().CurrentIndex() != 2 {
		t.Fatalf("测试自己没摆好：当前项是第 %d 首", h.app.Queue().CurrentIndex())
	}
	// 高亮是画在行上的底色，从画面上量得到：正在放的那一行是主色，其余是面板
	// 自己的底色。两者的像素必须不同——一样的话这个状态就没画出来。
	playing, _ := h.tt.Find("重复的那首")
	other, _ := h.tt.Find("另一首")
	if playing.H == 0 || other.H == 0 {
		t.Fatalf("两行都没画出来，文字：%q", h.tt.Texts())
	}
	img := h.tt.Image()
	pc := pixelAt(img, playing.X+playing.W/2, playing.Y+playing.H-4)
	oc := pixelAt(img, other.X+other.W/2, other.Y+other.H-4)
	if pc == oc {
		t.Errorf("正在放的那一行和别的行底色一样（%v），「正在放」没画出来", pc)
	}
}

// TestQueueRowDeleteStopsWhenTheNextSongWillNotPlay 是「从列表删除」接上
// `App.RemoveAt` 之后，那一条坑在**界面这一层**的样子：删掉的正是正在放的那首、
// 而队列里下一首打不开。
//
// 断的是三样：停下来（播放条上不再有歌）、话别说反（说的是「已经删掉了，但接不
// 上下一首」，不是「删不掉」——它确实删掉了）、队列里确实没了。
//
// ⚠️ 这里用坐标右键，不用 `RightClick(歌名)`：要删的那首正在放，同一个名字在**播放
// 条上也有**，按名字找会找中播放条那一处（那儿没有菜单）。
func TestQueueRowDeleteStopsWhenTheNextSongWillNotPlay(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	enqueue(t, h, localSong(t, dir, "甲.flac"))
	// 队列里下一首是一个打不开的文件（还没进队列的文件从没被放过，所以还不在队列里，
	// 这里直接塞一个不存在的路径）。
	h.app.Enqueue(app.LocalItem(filepath.Join(dir, "缺了这一首.flac")))
	openDrawer(t, h)

	// 抽屉里的第一行（视口 372-53 = 319，行高 64）：y 53~117。
	h.tt.RightClickAt(500, 85)
	if got := h.tt.Menu(); len(got) == 0 {
		t.Fatalf("右键抽屉第一行没有菜单，文字：%q", h.tt.Texts())
	}
	if err := h.tt.ChooseMenuItem("从列表删除"); err != nil {
		t.Fatalf("选不了「从列表删除」：%v", err)
	}
	h.tt.Frame()

	if !h.tt.HasText("已经删掉了") {
		t.Errorf("没有一句话说清「删掉了、下一首接不上」，文字：%q", h.tt.Texts())
	}
	if h.tt.HasText("删不掉") {
		t.Errorf("说的是「删不掉」，可它确实删掉了，文字：%q", h.tt.Texts())
	}
	if st := h.app.Snapshot(); st.HasTrack || st.Title != "" {
		t.Errorf("下一首打不开之后还在放已经删掉的那首：%+v", st)
	}
	if got := titlesOf(h.app.Queue().Items()); len(got) != 1 || got[0] != "缺了这一首" {
		t.Errorf("删完之后队列是 %q，want 只剩「缺了这一首」", got)
	}
}

// findRow 是「那一行现在**真的在画面上**吗」。
//
// ⚠️ 别直接用 `Tester.Find`：MyGo 会给滚出视口、但列表还留着它那个位置的行发一个
// **大小是 0** 的节点，`Find` 照样报 ok=true。实测：目标偏移和行高对不上时，
// 被定位的那一行就是这个 0×0 的样子——只断 `ok` 的话，「滚到了旁边那一首」是绿的。
func findRow(h *harness, title string) (mygoui.Rect, bool) {
	r, ok := h.tt.Find(title)
	if !ok || r.W <= 0 || r.H <= 0 {
		return r, false
	}
	return r, true
}

// TestLocateCurrentGlidesToTheCurrentSong 是验收里「定位当前播放能平滑滚过去」。
//
// 断的是三件事，一件一件都拿掉就会红：
//
//	点之前那一行在画面外 → 队列够长、定位真的会滚
//	点完钟没走还在画面外 → **不是一跳到位**（ListState.ScrollTo 就是一跳，
//	                  换成它这条立刻红）
//	Advance 够久之后在画面上 → 真的滚过去了
func TestLocateCurrentGlidesToTheCurrentSong(t *testing.T) {
	const target = 30
	h := newHarness(t)
	for i := range 40 {
		h.app.Queue().Append(onlineSong(fmt.Sprintf("BV%d", i), fmt.Sprintf("第%02d首", i)))
	}
	putCurrent(t, h.app, target)
	h.tt.Frame()
	openDrawer(t, h)

	if _, visible := findRow(h, "第30首"); visible {
		t.Fatal("没定位那一行就在画面上了，测不出滚动")
	}
	if err := h.tt.Click("定位当前播放"); err != nil {
		t.Fatalf("点不了「定位当前播放」：%v", err)
	}
	h.tt.Frame()

	if _, visible := findRow(h, "第30首"); visible {
		t.Error("一点就跳到位了，那不是平滑滚过去（用了 ListState.ScrollTo？）")
	}
	h.tt.Advance(2 * time.Second)
	r, visible := findRow(h, "第30首")
	if !visible {
		t.Fatalf("Advance 之后还是滚不过去，文字：%q", h.tt.Texts())
	}
	// 滚到的是**第 30 行自己的顶边**，不是别的行的：40 行 × 64 = 2560，视口
	// 372-53 = 319，MaxY 是 2241，30×64 = 1920 没被钳。列表视口的顶在 y=53，
	// 所以这一行的顶边就该落在 53 上。
	//
	// 差得越远说明算的不是 `targetIndex*行高`：**行高和目标偏移用的是同一个数**
	// （见 queue_drawer.go 的 locateCurrent）。两个数分家的时候，滚过去的是
	// 另一首——这一行会变成那个 0×0 的节点，上面那句 `visible` 就把它挡下来了。
	if r.Y > 56 {
		t.Errorf("那一行停在 y=%v，want ≈53（滚到的是第 30 行的顶边）", r.Y)
	}
}

// TestLocateCurrentSaysSoWhenThereIsNothingToLocate 两种「滚不了」都要说话
// （Biu 的两个 addToast）：默默不动的话，用户分不清是按钮坏了还是自己记错了。
func TestLocateCurrentSaysSoWhenThereIsNothingToLocate(t *testing.T) {
	t.Run("没在放", func(t *testing.T) {
		h := newHarness(t)
		h.app.Queue().Append(onlineSong("BV1", "第一首")) // 放进去了，还没开始放
		h.tt.Frame()
		openDrawer(t, h)

		if err := h.tt.Click("定位当前播放"); err != nil {
			t.Fatal(err)
		}
		h.tt.Frame()
		if !h.tt.HasText("当前没有正在播放的歌曲") {
			t.Errorf("没在放的时候一句话也没有，文字：%q", h.tt.Texts())
		}
	})

	t.Run("这一首认不出来", func(t *testing.T) {
		h := newHarness(t)
		// 一个什么来源都没有的项（还没接上的占位项）：Identity 是空串，
		// 定位无从谈起。
		h.app.Queue().Append(app.Item{Title: "还没接来源的一项"})
		putCurrent(t, h.app, 0)
		h.tt.Frame()
		openDrawer(t, h)

		if err := h.tt.Click("定位当前播放"); err != nil {
			t.Fatal(err)
		}
		h.tt.Frame()
		if !h.tt.HasText("认不出来") {
			t.Errorf("认不出的那一首没有说话，文字：%q", h.tt.Texts())
		}
	})
}

// TestQueueRowMenuOffersFiveItemsForAnOnlineSong 五项都在，而且项和顺序是写死的
// 那一套（理由见 queueMenuItems：Biu 那五项里三项要还没做的 B 站接口）。
func TestQueueRowMenuOffersFiveItemsForAnOnlineSong(t *testing.T) {
	h := newHarness(t)
	h.app.Queue().Append(onlineSong("BV1", "第一首"))
	h.tt.Frame()
	openDrawer(t, h)

	if err := h.tt.RightClick("第一首"); err != nil {
		t.Fatalf("右键不了这一行：%v", err)
	}
	want := []string{"播放", "插到下一首播放", "在 B 站打开", "清空播放列表", "从列表删除"}
	got := h.tt.Menu()
	if len(got) != len(want) {
		t.Fatalf("菜单有 %d 项 %q，want %d 项 %q", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("菜单第 %d 项是 %q，want %q", i+1, got[i], want[i])
		}
	}
}

// TestQueueRowMenuOffersTheSameFiveItemsForALocalSong 本地项也是**五项**。
//
// 这一条盯的是「今天唯一能进队列的入口是 enqueuePicked（本地文件）」：照 Biu 的
// hidden 规则（收藏 / 下载 / 在 B 站打开对本地项都藏起来）的话，普通用户看到的
// 行菜单永远只有「从列表删除」一项——五项一条都见不到。
//
// 第三项对本地项写的是「在访达中显示」（本地文件也有来处，只是不在 B 站上，
// 和内容列表那一套菜单同一个判断，见 list.OpenLabel）。
func TestQueueRowMenuOffersTheSameFiveItemsForALocalSong(t *testing.T) {
	h := newHarness(t)
	// 歌名起得特别一点：Tester 是按**精确的字**找元素的，歌名太普通会先撞上
	// 播放条上那一行同一个名字（那儿也写着这一首）。
	h.app.Queue().Append(app.LocalItem(writeCopy(t, t.TempDir(), "本地那一首.flac")))
	h.tt.Frame()
	openDrawer(t, h)

	if err := h.tt.RightClick("本地那一首"); err != nil {
		t.Fatalf("右键不了这一行：%v", err)
	}
	want := []string{"播放", "插到下一首播放", "在访达中显示", "清空播放列表", "从列表删除"}
	got := h.tt.Menu()
	if len(got) != len(want) {
		t.Fatalf("本地项的菜单有 %d 项 %q，want %d 项 %q", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 项是 %q，want %q", i+1, got[i], want[i])
		}
	}
}

// TestQueueRowMenuItemsReallyDoTheirThing 钉验收那句话的后半段：**接上真实
// 操作**。每一项按下去，队列都要真的动一下。
//
// 逐项走菜单（不是直接调 App 的方法）：走菜单才是用户按的那条路——菜单里少建一
// 项、或者那一项接错了函数，只有这样才断得出来。
//
// 用**本地文件**（真的能放），不用在线条目：在线条目在这个壳里没挂取流
// （`Streaming` 没装），`PlayAt` 会打不开然后把下标退回去，「播放」那一项就断不出
// 「它有没有真的跳过去」。
//
// ⚠️ 「在访达中显示 / 在 B 站打开」这一项**不在这里按**：最后一步是
// `mygo.Shell.ShowItemInFolder` / `OpenExternal`，它们要真的事件循环，在测试里
// 会永远等下去（实测整个测试挂死）。那一项验的是它在菜单里（上面两条）以及它就是
// 内容列表那一套已经测过的 Open（`list.Actions`）。
func TestQueueRowMenuItemsReallyDoTheirThing(t *testing.T) {
	t.Run("播放", func(t *testing.T) {
		h := newHarness(t)
		dir := t.TempDir()
		enqueue(t, h, localSong(t, dir, "甲.flac"))
		h.app.Enqueue(localSong(t, dir, "乙.flac"), localSong(t, dir, "丙.flac"))
		openDrawer(t, h)
		// 右键的是**没在放**的那一行：歌名撞上播放条上那一行同一个字的话，
		// RightClick 会点中播放条，点出来是空的菜单。
		chooseQueueItem(t, h, "丙", "播放")

		waitFor(t, 2*time.Second, "跳到丙", func() bool {
			h.tt.Frame()
			return h.app.Snapshot().Title == "丙"
		})
		if got := h.app.Queue().CurrentIndex(); got != 2 {
			t.Errorf("按了「播放」当前项是第 %d 首，want 第 3 首", got)
		}
	})

	t.Run("插到下一首播放", func(t *testing.T) {
		h := newHarness(t)
		dir := t.TempDir()
		enqueue(t, h, localSong(t, dir, "甲.flac"))
		h.app.Enqueue(localSong(t, dir, "乙.flac"), localSong(t, dir, "丙.flac"))
		openDrawer(t, h)
		chooseQueueItem(t, h, "丙", "插到下一首播放")

		got := titlesOf(h.app.Queue().Items())
		want := []string{"甲", "丙", "乙"}
		if len(got) != len(want) {
			t.Fatalf("插队之后队列是 %q，want %q", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("插队之后队列是 %q，want %q", got, want)
			}
		}
		if h.app.Queue().CurrentIndex() != 0 {
			t.Errorf("插队把当前项也改了：第 %d 首", h.app.Queue().CurrentIndex())
		}
		if got := h.app.Snapshot().Title; got != "甲" {
			t.Errorf("插队把正在放的歌换了：%q", got)
		}
	})

	t.Run("清空播放列表", func(t *testing.T) {
		h := newHarness(t)
		dir := t.TempDir()
		enqueue(t, h, localSong(t, dir, "甲.flac"))
		h.app.Enqueue(localSong(t, dir, "乙.flac"))
		openDrawer(t, h)
		chooseQueueItem(t, h, "乙", "清空播放列表")

		if n := h.app.Queue().Len(); n != 0 {
			t.Errorf("按了「清空播放列表」队列还剩 %d 首", n)
		}
		if st := h.app.Snapshot(); st.HasTrack {
			t.Errorf("按了「清空播放列表」还在放：%+v", st)
		}
	})

	t.Run("从列表删除", func(t *testing.T) {
		h := newHarness(t)
		dir := t.TempDir()
		enqueue(t, h, localSong(t, dir, "甲.flac"))
		h.app.Enqueue(localSong(t, dir, "乙.flac"), localSong(t, dir, "丙.flac"))
		openDrawer(t, h)
		chooseQueueItem(t, h, "乙", "从列表删除")

		got := titlesOf(h.app.Queue().Items())
		if len(got) != 2 || got[0] != "甲" || got[1] != "丙" {
			t.Errorf("按了「从列表删除」队列是 %q，want 甲/丙", got)
		}
	})
}

// chooseQueueItem 右键 title 那一行，再从菜单里选 label 那一项。
func chooseQueueItem(t *testing.T, h *harness, title, label string) {
	t.Helper()
	if err := h.tt.RightClick(title); err != nil {
		t.Fatalf("右键不了 %q 那一行：%v", title, err)
	}
	if err := h.tt.ChooseMenuItem(label); err != nil {
		t.Fatalf("选不了菜单里的 %q（菜单是 %q）：%v", label, h.tt.Menu(), err)
	}
	h.tt.Frame()
}

// TestQueueRowsActOnTheirQueuePositionNotTheirRowNumber 队列是 **[甲, 甲, 乙]**：
// 抽屉里去重之后画两行（甲、乙），可乙在**队列**里是第 3 首、在抽屉里是第 2 行。
//
// 这一条钉的就是那多出来的一格。把 `queueRowsFor` 里的 `at` 写成行号，原来那批
// 用例**一条都不会红**——它们排的队列里没有重复项，行号和队列下标本来就是同一个
// 数。差这一格的后果不是难看，是**删错/放错那一首**：右键第二行乙、按「从列表
// 删除」，删掉的是队列里第二首（另一个甲）。
//
// 全用**本地文件**：在线条目在这个壳里放不了，「播放」那一项就断不出「它有没有
// 真的跳到那一首」。
func TestQueueRowsActOnTheirQueuePositionNotTheirRowNumber(t *testing.T) {
	// dupQueue 排出 [甲, 甲, 乙] 并且正在放甲（队列下标 0）。
	dupQueue := func(t *testing.T) (*harness, app.Item) {
		t.Helper()
		h := newHarness(t)
		dir := t.TempDir()
		a := localSong(t, dir, "甲.flac")
		b := localSong(t, dir, "乙.flac")
		enqueue(t, h, a)
		h.app.Enqueue(a, b) // 队列变成 甲,甲,乙；正在放的仍然是第一个甲
		if h.app.Queue().Len() != 3 {
			t.Fatalf("测试自己没摆好：队列 %d 首", h.app.Queue().Len())
		}
		return h, b
	}

	t.Run("从列表删除", func(t *testing.T) {
		h, _ := dupQueue(t)
		openDrawer(t, h)
		if got := countRows(h); got != 2 {
			t.Fatalf("抽屉里画着 %d 行，want 2（去重之后）", got)
		}
		chooseQueueItem(t, h, "乙", "从列表删除")

		got := titlesOf(h.app.Queue().Items())
		if len(got) != 2 || got[0] != "甲" || got[1] != "甲" {
			t.Fatalf("删完之后队列是 %q，want 两个「甲」", got)
		}
	})

	t.Run("播放", func(t *testing.T) {
		h, _ := dupQueue(t)
		openDrawer(t, h)
		if err := h.tt.Click("乙"); err != nil {
			t.Fatalf("点不了第二行：%v", err)
		}
		waitFor(t, 2*time.Second, "跳到乙", func() bool {
			h.tt.Frame()
			return h.app.Snapshot().Title == "乙"
		})
		if got := h.app.Queue().CurrentIndex(); got != 2 {
			t.Errorf("点第二行之后当前项是第 %d 首，want 第 3 首（它在队列里的位置）", got)
		}
	})

	t.Run("插到下一首播放", func(t *testing.T) {
		h, _ := dupQueue(t)
		openDrawer(t, h)
		chooseQueueItem(t, h, "乙", "插到下一首播放")

		got := titlesOf(h.app.Queue().Items())
		want := []string{"甲", "乙", "甲"}
		if len(got) != len(want) {
			t.Fatalf("插队之后队列是 %q，want %q", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("插队之后队列是 %q，want %q", got, want)
			}
		}
	})
}

func TestQueueRowMenuRemovesThatSong(t *testing.T) {
	h := newHarness(t)
	h.app.Queue().Append(
		onlineSong("BV1", "第一首"),
		onlineSong("BV2", "第二首"),
		onlineSong("BV3", "第三首"),
	)
	putCurrent(t, h.app, 0)
	h.tt.Frame()
	openDrawer(t, h)

	if err := h.tt.RightClick("第二首"); err != nil {
		t.Fatalf("右键不了这一行：%v", err)
	}
	if err := h.tt.ChooseMenuItem("从列表删除"); err != nil {
		t.Fatalf("选不了「从列表删除」：%v", err)
	}
	h.tt.Frame()

	got := titlesOf(h.app.Queue().Items())
	if len(got) != 2 || got[0] != "第一首" || got[1] != "第三首" {
		t.Fatalf("删完之后队列是 %q，want 第一首/第三首", got)
	}
	if h.tt.HasText("第二首") {
		t.Errorf("删掉了还画在抽屉里，文字：%q", h.tt.Texts())
	}
	// 删的不是正在放的那首：歌照放。
	if h.app.Queue().CurrentIndex() != 0 {
		t.Errorf("删一首不在放的歌把当前项也改了：%d", h.app.Queue().CurrentIndex())
	}
}

// TestQueueRowMenuSaysSoWhenThereIsNowhereToOpen 「在 B 站打开」接的是内容列表
// 那一套 Actions.Open（本地文件先判，其次才轮到 B 站地址）。这里断的是**它确实
// 接在那套上**：给一个没有 bvid 也没有 sid 的在线项，那一套会回一句「这一项还没有
// 可以打开的来处」。要是这一项自己写一份、或者干脆什么都不做，这句话不会出现。
//
// ⚠️ 不在这里真的按一个**有** bvid 的：那条路最后是 `mygo.Shell.OpenExternal`，
// 它要真的事件循环，测试里会永远等下去，还会真的把浏览器打开。
func TestQueueRowMenuSaysSoWhenThereIsNowhereToOpen(t *testing.T) {
	h := newHarness(t)
	h.app.Queue().Append(app.Item{Source: "online", Type: "mv", Title: "没有来处的一项"})
	h.tt.Frame()
	openDrawer(t, h)

	if err := h.tt.RightClick("没有来处的一项"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem("在 B 站打开"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if !h.tt.HasText("还没有可以打开的来处") {
		t.Errorf("没有一句话说清开不了，文字：%q", h.tt.Texts())
	}
}

func TestQueueDrawerClearEmptiesItAndStops(t *testing.T) {
	h := newHarness(t)
	enqueue(t, h, copiesOf(t, 2)...)
	openDrawer(t, h)

	if err := h.tt.Click("清空"); err != nil {
		t.Fatalf("点不了「清空」：%v", err)
	}
	h.tt.Frame()

	if n := h.app.Queue().Len(); n != 0 {
		t.Errorf("清空之后队列还剩 %d 首", n)
	}
	if st := h.app.Snapshot(); st.HasTrack {
		t.Errorf("清空之后还在放：%+v", st)
	}
	if !h.tt.HasText(queueEmptyHint) {
		t.Errorf("清空之后抽屉里不是空状态，文字：%q", h.tt.Texts())
	}
}

// TestQueueDrawerSurvivesADuplicateSong 是一道保险，不是走过场。
//
// 抽屉的行身份是判重标识（铁律四），而 MyGo 在 Tester 里**撞了 key 当场 panic**
// （`t.rt.strict = true`）。所以这一条钉的是「去重之后 key 必然是唯一的」：
// 去掉 queueRowsFor 里那一次去重，这里就红。
func TestQueueDrawerSurvivesADuplicateSong(t *testing.T) {
	h := newHarness(t)
	dup := onlineSong("BV1", "第一首")
	h.app.Queue().Append(dup, onlineSong("BV2", "第二首"), dup, dup, onlineSong("BV1", "第一首"))
	h.tt.Frame()

	openDrawer(t, h)
	if got := countRows(h); got != 2 {
		t.Errorf("抽屉里画着 %d 行，want 2（五首里只有两个不同的判重标识）", got)
	}
}

// pathsOfItems / titlesOf 是一列项的路径 / 歌名，好和期望逐个比。
func pathsOfItems(items []app.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Path
	}
	return out
}

func titlesOf(items []app.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Title
	}
	return out
}
