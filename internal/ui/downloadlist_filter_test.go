package ui

import (
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
	"github.com/egoist/mygo/ui"
)

// 这一组跑的是下载记录页那一排「全部 / 音频 / 视频」（#99 / SPEC 的 A26）。
//
// 走两条造数据的路，各有各的用处，**别混**：
//
//	newRows       给定的快照。断的是**画法**：哪一档留下哪些行、空了说什么。
//	newDownloads  队列文件读回来的真引擎快照。断的是**那一排筛的是真数据**：
//	             `Snapshot.Video` 要是没从队列里读出来（或者读成了别的某一位），
//	             这一档就会把两类认成同一类，而屏幕上多出来的是一列一模一样的
//	             歌行——看不出任何异样。
//
// ⚠️ 判据一律是**画面上有没有那一行**（`shows`），不是「`d.rows` 的长度」：
// 用户看的是表格，量页面上的字段等于量实现，而实现和画面对不上正是这一页历史上
// 出过的那种事。

// dlFilterNamesInOrder 是三档在界面上的名字，从左到右。
//
// 它抄的是 `dlFilterLabels`，不是查的：这一条要验的正是「摆出去的是这三档、
// 按这个次序」，而查了就等于拿实现验实现。
var dlFilterNamesInOrder = []string{"全部", "音频", "视频"}

// dlPick 切到第 i 档（0 = 全部），并重新画几帧。
//
// ⚠️ `Tester.Click` 认的是**控件上的文字**，而三档各自的标签就是那三个词——所以
// 「点第 i 档」= 点它的标签。
func dlPick(t *testing.T, tt *ui.Tester, i int) {
	t.Helper()
	if err := tt.Click(dlFilterNamesInOrder[i]); err != nil {
		t.Fatalf("点 %q：%v（画面上的字有 %q）", dlFilterNamesInOrder[i], err, tt.Texts())
	}
	dlFrame(tt)
}

// dlThreeTasks / dlThreeShots 是「两支歌 + 一支 MV」的标准搭配。两条造数据的路
// 都用它，所以「走真引擎」和「走给定快照」量的是同一件事。
func dlThreeTasks() []queuedTask {
	return []queuedTask{
		newTask("歌 甲", download.StatusDone, dlChunk, 100),
		newVideoTask("MV 乙", download.StatusDone, dlChunk, 100),
		newTask("歌 丙", download.StatusDone, dlChunk, 100),
	}
}

func dlThreeShots() []download.Snapshot {
	return []download.Snapshot{
		dlSnapshot("歌 甲", download.StatusDone, dlChunk, dlChunk),
		dlVideoSnapshot("MV 乙", download.StatusDone, dlChunk, dlChunk),
		dlSnapshot("歌 丙", download.StatusDone, dlChunk, dlChunk),
	}
}

// dlThreeNames 是上面那三条的歌名，判据按它取「画面上有哪些」。
var dlThreeNames = []string{"歌 甲", "MV 乙", "歌 丙"}

// dlNamesOnScreen 是画面上真的出现的那几个（按画出来的次序）。
func dlNamesOnScreen(tt *ui.Tester) []string {
	var out []string
	for _, name := range dlThreeNames {
		if shows(tt, name) {
			out = append(out, name)
		}
	}
	return out
}

// sameNames 比两串歌名。次序也算——筛不重排，而一个顺手按类型排了序的实现，
// 界面上看不出异样，而用户会以为自己的下载记录被人动过。
func sameNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestTheThreeFiltersAreOnScreenAndDefaultToEverything 断那一排存在、就是那三档、
// 而且**从左到右**是那个次序。
//
// 次序这一条不是凑数：Biu 那边三档的排法是「全部在前、具体的那一档在后」，而
// 一个把「视频」摆在第一档的实现，用户按习惯点第二个（音频）会拿到视频那一档，
// 而屏幕上摆着的字都摆在那儿——没有任何东西说「你点错了档」。
func TestTheThreeFiltersAreOnScreenAndDefaultToEverything(t *testing.T) {
	_, tt := newRows(t, dlThreeShots()...)

	xs := make([]float32, 0, len(dlFilterNamesInOrder))
	for _, label := range dlFilterNamesInOrder {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("没有 %q 那一档，文字有 %q", label, tt.Texts())
		}
		xs = append(xs, r.X)
	}
	for i := 1; i < len(xs); i++ {
		if xs[i] <= xs[i-1] {
			t.Errorf("%q 画在 %q 的左边（x=%v ≤ %v）：次序不对", dlFilterNamesInOrder[i],
				dlFilterNamesInOrder[i-1], xs[i], xs[i-1])
		}
	}
	// 默认是「全部」：MV 和歌都在。
	//
	// ⚠️ 默认成「音频」的话，用户升级之后打开这一页，**下过的 MV 会凭空消失**，
	// 而界面上没有任何一处说它们去了哪——「一进页面列表变短了」看起来像是下载
	// 丢了，而用户唯一能做的事是把它们重新下一遍。
	for _, name := range dlThreeNames {
		if !shows(tt, name) {
			t.Errorf("刚进来 %q 就不在了，文字有 %q：默认档不是「全部」", name, tt.Texts())
		}
	}
}

// TestEachFilterShowsOnlyThatKind 断三档各留下该留的那些行。
//
// 判据是**剩下的是哪几个**，不是「剩下几个」：一个把两档都认成同一类的实现，
// 剩下来的行数照样对得上（「全部」和某错误档都是 3 个），而屏幕上摆着一支 MV。
func TestEachFilterShowsOnlyThatKind(t *testing.T) {
	shots := dlThreeShots()
	for _, c := range []struct {
		i    int
		档    string
		want []string
		no   []string
	}{
		{0, "全部", dlThreeNames, nil},
		{1, "音频", []string{"歌 甲", "歌 丙"}, []string{"MV 乙"}},
		{2, "视频", []string{"MV 乙"}, []string{"歌 甲", "歌 丙"}},
	} {
		t.Run(c.档, func(t *testing.T) {
			_, tt := newRows(t, shots...)

			dlPick(t, tt, c.i)
			if got := dlNamesOnScreen(tt); !sameNames(got, c.want) {
				t.Errorf("切到「%s」之后画面上是 %v，want %v，文字有 %q",
					c.档, got, c.want, tt.Texts())
			}
			for _, gone := range c.no {
				if shows(tt, gone) {
					t.Errorf("切到「%s」之后 %q 还在画面上", c.档, gone)
				}
			}
		})
	}
}

// TestTheFilterReadsTheKindOutOfTheRealQueue 断那一排筛的是**真引擎**给的那一位。
//
// 上面几条喂的是快照，所以一个「`Snapshot.Video` 谁填谁自便」的接线照样绿。
// 这一条走队列文件读回来的真任务：`Task.Video` 要是没进快照（或者读成了
// `Identity.Video`、`ID` 的 `|video` 后缀这另外两位），「视频」那一档会把歌
// 也留下——而屏幕上多出来的是一列一模一样的歌行，看不出任何异样。
func TestTheFilterReadsTheKindOutOfTheRealQueue(t *testing.T) {
	_, tt := newDownloads(t, dlThreeTasks()...)

	dlPick(t, tt, 2) // 视频
	if got := dlNamesOnScreen(tt); !sameNames(got, []string{"MV 乙"}) {
		t.Errorf("真队列上切到「视频」之后画面上是 %v，want 只有 MV 乙，文字有 %q", got, tt.Texts())
	}
	dlPick(t, tt, 1) // 音频
	if got := dlNamesOnScreen(tt); !sameNames(got, []string{"歌 甲", "歌 丙"}) {
		t.Errorf("真队列上切到「音频」之后画面上是 %v，want 两支歌，文字有 %q", got, tt.Texts())
	}
}

// TestTheFilterSurvivesLeavingAndComingBack 断「切走再切回来，选中的还是那一档」。
//
// 验收项点名要这一条。实现上它是白捡的（页面状态住在 `Shell.downloads` 上，
// 而它建一次不换），可**白捡的东西也会被顺手改坏**——有人把这一摊挪进
// `viewDownloadList` 的局部变量时，红的只有这一条。
//
// ⚠️ 走的是真的 `navigate`（切路由），不是「重新建一个外壳」：后者会把筛选
// 状态一起清掉，而那不是用户做的事。
func TestTheFilterSurvivesLeavingAndComingBack(t *testing.T) {
	s, tt := newRows(t, dlThreeShots()...)

	dlPick(t, tt, 2) // 视频
	s.navigate(homePath)
	dlFrame(tt)
	s.navigate(downloadListPath)
	dlFrame(tt)

	if got := dlNamesOnScreen(tt); !sameNames(got, []string{"MV 乙"}) {
		t.Errorf("切走再回来之后画面上是 %v，want 仍然是「视频」那一份，文字有 %q"+
			"：筛选状态没跟着页面活过一趟路由", got, tt.Texts())
	}
	if s.downloads.filter != dlFilterVideo {
		t.Errorf("切走再回来之后选中的是 %q，want %q",
			s.downloads.filter.label(), dlFilterVideo.label())
	}
}

// TestTheEmptyCopyAfterFilteringIsNotTheCopyForNoDownloads 断筛出来的空和真的
// 没下载**说的不是同一句话**。
//
// 验收项点名要这一条：用户分得清「我筛出来的这一档没有」和「我一首歌都没下过」。
// 混成一句的话，他在「视频」那一档看到「暂无内容」，会以为自己白下了几十首——
// 而那些歌明明就在下面一档里躺着。
//
// 断的是**两边各自那句话各自不许出现在对方的屏幕上**。
func TestTheEmptyCopyAfterFilteringIsNotTheCopyForNoDownloads(t *testing.T) {
	_, tt := newRows(t, dlSnapshot("歌 甲", download.StatusDone, dlChunk, dlChunk))
	real := lookup(downloadListPath).Empty
	filtered := dlEmptyAfterFilter(dlFilterVideo)

	// 先确认两句话**本来就**是两句话（否则下面那两条断言在文案被人合并之后
	// 会一起变成空断言：既查不出「该说的没说」，也查不出「不该说的说了」）。
	if filtered.Title == real.Title && filtered.Hint == real.Hint {
		t.Fatalf("筛出来的空和真的没下载说的是同一句 %q——用户分不出这两种空", filtered.Title)
	}

	// 还没筛：一句空状态都不该出现。
	if shows(tt, real.Title) || shows(tt, real.Hint) {
		t.Fatalf("有记录却画了空状态，文字有 %q", tt.Texts())
	}

	dlPick(t, tt, 2) // 视频：一支都没有
	if !shows(tt, filtered.Title) || !shows(tt, filtered.Hint) {
		t.Errorf("筛出来的空没有说 %q / %q，文字有 %q", filtered.Title, filtered.Hint, tt.Texts())
	}
	for _, wrong := range []string{real.Title, real.Hint} {
		if shows(tt, wrong) {
			t.Errorf("筛出来的空却说了「怎么才能有下载」%q：用户会以为自己一首歌"+
				"都没下过（而歌就在「音频」那一档里），文字有 %q", wrong, tt.Texts())
		}
	}
}

// TestTheEmptyPageHasNoFilterToPoke 断队列整个是空的时候**不摆**那一排。
//
// 一条记录都没有时，那三个开关能筛出来的只有一个空列表，而下面那句「搜一首歌，
// 在它旁边点下载」已经说清了怎么才能不空。在一排开关前面摆一句「这排开关什么
// 也筛不出来」是白占一行。
//
// ⚠️ 它和下一条是一对：**筛出来的空要摆**（用户得回得去），**真的没下载不摆**
// （没得可回）。只钉一条的话，「一空就收起来」那个实现照样过一半。
func TestTheEmptyPageHasNoFilterToPoke(t *testing.T) {
	// ⚠️ **有目录**：这一条断的是「真的没下载过」那一支的文案，而那一句只在
	// **有目录**的时候才是那一句（没目录时那一格说的是「去设置里选一个」，
	// 见报告 F3）。
	_, tt := newRowsWithDir(t)
	for _, label := range dlFilterNamesInOrder {
		if tt.HasText(label) {
			t.Errorf("一条记录都没有却摆出了筛选 %q，文字有 %q", label, tt.Texts())
		}
	}
	// 而真的没下载那一句还在（这一条是 `TestTheEmptyListStillSaysWhyItIsEmpty`
	// 的对面：不是把它弄丢了）。
	want := lookup(downloadListPath).Empty
	if !shows(tt, want.Hint) {
		t.Errorf("空着的时候没有说「怎么才能不空」，文字有 %q", tt.Texts())
	}
}

// TestTheFilterIsStillThereAfterFilteringEverythingOut 断「筛出来的空」那一支里
// **那一排还在**，而且按一下「全部」真的回得去。
//
// 用户点错了档、或者刚好那一档真的没有，他得有一条看得见的路回去。把那一排和
// 空状态绑在一起（「一空就收起来」）的话，这一页就成了一个走不出去的地方：
// 界面上没有任何东西说「刚才那几行去哪了」。
func TestTheFilterIsStillThereAfterFilteringEverythingOut(t *testing.T) {
	_, tt := newRows(t, dlSnapshot("歌 甲", download.StatusDone, dlChunk, dlChunk))

	dlPick(t, tt, 2) // 视频：一支都没有
	for _, label := range dlFilterNamesInOrder {
		if !tt.HasText(label) {
			t.Errorf("筛空了之后 %q 那一档不见了：用户没有路回去，文字有 %q", label, tt.Texts())
		}
	}
	// 按一下「全部」——这是「那一排还在」唯一拿得出手的证据：光说它还在不算。
	dlPick(t, tt, 0)
	if !shows(tt, "歌 甲") {
		t.Errorf("回到「全部」之后 %q 还是不回来，文字有 %q", "歌 甲", tt.Texts())
	}
}

// TestClearingWhileFilteredStillCountsTheWholeQueue 断筛着的时候点「清空记录」，
// 弹窗里报的是**整个队列**的条数。
//
// 判据钉的是那句**人话**（以及点下去真的清掉整个队列），不是按钮灰不灰：
// `App.ClearDownloads` 清的是整个队列，而用户看着的只有筛出来的那几行。按屏幕
// 上那几行报数的话，他在「视频」那一档看到「确认清空这 1 条」而点下去删掉的是
// 4 条——不可撤销（还在下的半成品会被删掉，见 `askClear`）。那句话是在骗用户，
// 而骗的正是最贵的那一步。
func TestClearingWhileFilteredStillCountsTheWholeQueue(t *testing.T) {
	tasks := append(dlThreeTasks(), newTask("歌 丁", download.StatusRunning, dlProgress, 50))
	s, tt := newDownloads(t, tasks...)

	dlPick(t, tt, 2) // 视频：屏幕上只剩一支 MV
	if err := tt.Click("清空记录"); err != nil {
		t.Fatalf("点「清空记录」：%v，文字有 %q", err, tt.Texts())
	}
	dlFrame(tt)

	// 四条里三条是歌（屏幕上一条都看不见），其中一条还在下。
	//
	// ⚠️ 判的是**那句话在画面上**（`HasText` 是「有一段文字包含它」），不是
	// 「有一个元素的文字逐字等于它」：弹窗的正文是一个元素，那两句是**接在
	// 一起**的一段（`askClear` 里 `body +=`），而它还会按宽度折行。
	for _, want := range []string{
		"确认清空这 4 条下载记录吗？下完的歌留在盘上，删不掉。",
		"其中 1 条还没下完，清空之后那些半成品也会被删掉。",
	} {
		if !tt.HasText(want) {
			t.Errorf("弹窗里没有 %q，文字有 %q", want, tt.Texts())
		}
	}
	if tt.HasText("确认清空这 1 条") {
		t.Errorf("弹窗按屏幕上那 1 行报了数：点下去删掉的是 4 条，文字有 %q", tt.Texts())
	}
	// 真的点了就真的清掉**整个**队列——那句话说的和做的是同一件事。
	if err := tt.Click("清空"); err != nil {
		t.Fatalf("点「清空」：%v，文字有 %q", err, tt.Texts())
	}
	dlFrame(tt)
	if got := len(s.app.DownloadSnapshots()); got != 0 {
		t.Errorf("点完「清空」队列里还剩 %d 条", got)
	}
}

// TestChangingTheFilterSendsNoRequest 断「纯前端：一个请求都不发、队列一个字节
// 都不动」。
//
// 判据是**那台记请求的 http.Client 上仍然是零**加上**队列前后一模一样**，不是
// 「界面上那几行变了」——后者一个「每帧重拉一次队列」的写法照样绿，而那个写法
// 的症状是：切一次档，B 站就被叫一次，而用户只是看了一眼自己的下载记录。
//
// 挂上去的那台客户端就是 `Downloads.Media`（整个下载引擎拉字节用的那一个），
// 所以它记下的不只是筛选这一段。
func TestChangingTheFilterSendsNoRequest(t *testing.T) {
	var mu sync.Mutex
	var hits int
	rec := &countingTransport{count: func() { mu.Lock(); hits++; mu.Unlock() }}
	a := app.New()
	t.Cleanup(a.Close)
	if err := a.MountDownloads(app.Downloads{
		Media: &http.Client{Transport: rec},
		// 不给 QueueFile：这个外壳里队列只在内存中，省得 `OnWillQuit` 之外的
		// 什么路径往临时目录里写东西。
		FallbackDir: dlDir,
	}); err != nil {
		t.Fatal(err)
	}
	for _, it := range []app.Item{
		{Type: "mv", Bvid: "BV1x", Cid: "c1", Title: "歌 甲"},
		{Type: "mv", Bvid: "BV1x", Cid: "c1", Title: "MV 乙"},
	} {
		var err error
		if it.Title == "MV 乙" {
			_, err = a.DownloadVideo(it, 0)
		} else {
			_, err = a.Download(it)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	before := dlQueueIDs(t, a)

	s := newDownloadShell(t, a)
	tt := ui.NewTester(s.View, dlWide, 800)
	s.navigate(downloadListPath)
	dlFrame(tt)

	for i := range len(dlFilterNamesInOrder) {
		dlPick(t, tt, i)
	}
	// 再走一趟路由（验收项点名要切路由保留），顺带再数一遍。
	s.navigate(homePath)
	dlFrame(tt)
	s.navigate(downloadListPath)
	dlFrame(tt)
	dlPick(t, tt, 1)

	mu.Lock()
	defer mu.Unlock()
	if hits != 0 {
		t.Errorf("换了 %d 次档、切了一趟路由之后，那台客户端被叫了 %d 次。数据早就在"+
			"内存里了（`app.DownloadSnapshots` 就是队列快照），筛选不许碰网络",
			len(dlFilterNamesInOrder)+1, hits)
	}
	if after := dlQueueIDs(t, a); !sameNames(before, after) {
		t.Errorf("换档前后队列变了：%v → %v。筛选是页面上的事，队列一个字节都不该动",
			before, after)
	}
}

// dlQueueIDs 把队列里每一项的身份列出来（筛前 / 筛后比这一个）。
//
// ⚠️ **只比身份，不比状态**：这一条造出来的两项没有取流可用，后台那条线会很快
// 把它们从「排队中」走到「出错」——那是引擎在动，和换档没关系，把它算进来这条
// 就会飘（而飘的方向还是「队列变了」这条错话，排查起来要多绕一层）。
func dlQueueIDs(t *testing.T, a *app.App) []string {
	t.Helper()
	var out []string
	for _, s := range a.DownloadSnapshots() {
		out = append(out, s.ID)
	}
	return out
}

// countingTransport 记下自己被调了几次。
//
// 真去发请求的那一支是**故意坏的**：这一条测试里任何请求都是意外，而一个
// 「顺手把这一档该做的取流也做了」的实现会在这里拿到一个错——测试要能看见。
type countingTransport struct {
	count func()
}

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.count()
	return nil, errors.New("这一条测试里不许有任何请求")
}

// TestTheFilterIsHorizontalRadioButtonsNow 那一排是**单选圆点**，不是 Segmented。
//
// 出处是上游 `download-list/index.tsx:110-120` 的 `RadioGroup orientation="horizontal"`。
// 判据是像素：选中那一档（「全部」）的字左边 11 像素处是**主色**——那里正好
// 落在 16 像素的圆点上、又避开了中间那 6 像素的小白点；未选中的「音频」同一
// 位置是页面底色。
//
// ⚠️ **Segmented 在那一点上不是主色**：它选中那一格是一块近白的滑块，左边也
// 没有圆点。所以这一条能把两种形态分开，而不是只断「有那三档字」。
func TestTheFilterIsHorizontalRadioButtonsNow(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := newRows(t, dlThreeShots()...)
			tt.SetDark(dark)
			dlFrame(tt)

			th := BuildTheme(s.app.SettingsSnapshot(), dark)
			accent := rgbaOf(th.Accent)
			page := rgbaOf(th.Background)
			img := tt.Image()

			// 圆点中心 = 字左缘 − 24（16 的圆 + 8 的缝），取中心右边 5 像素。
			selected, ok := tt.Find("全部")
			if !ok {
				t.Fatalf("找不到「全部」，文字有 %q", tt.Texts())
			}
			y := round32(selected.Y + selected.H/2)
			got := img.RGBAAt(clampX(img, round32(selected.X)-11), clampY(img, y))
			if maxDiff(got, accent) > probeTolerance {
				t.Errorf("选中的「全部」左边那一格是 %v，want 主色 %v——那一排不是单选圆点"+
					"（还是原来的 Segmented？）", got, accent)
			}

			off, ok := tt.Find("音频")
			if !ok {
				t.Fatalf("找不到「音频」，文字有 %q", tt.Texts())
			}
			y = round32(off.Y + off.H/2)
			got = img.RGBAAt(clampX(img, round32(off.X)-16), clampY(img, y))
			if maxDiff(got, page) > probeTolerance {
				t.Errorf("未选中的「音频」圆点中心是 %v，want 页面底色 %v——未选中的圆点"+
					"应该是一圈描边里面空着", got, page)
			}
		})
	}
}
