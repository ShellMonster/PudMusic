package ui

// 票 #633：下载记录页那一格的键**不分配**。
//
// # 这一组测的是什么
//
// 下载记录页的每一格、每一帧都要算一个「这一格是谁」的键，而那个键**只用来查
// 一张 map**（`d.failed`：记着「哪一格画崩了」）。算完查完就扔——而它原先是
// 一句 `fmt.Sprintf("%s#%d", …)`，于是**每格每帧一次分配 ＋ 一次格式化**。
//
// 一屏八行 × 五列 = **四十次**（实测）。
//
// # 两条硬约束（这一组分别钉住）
//
//  1. **键必须还是「行 id + 列号」**：行会重排（用户在上面删掉一条，下面的行
//     往上挪），所以「哪一格崩了」这个记录**必须跟着那一行走**。改成按行下标
//     记的话，用户删掉一行之后，崩过的那一格就记到别的行上了——而界面上只是
//     「有一格写着『画不出来』」，错位这件事**看不出来**。
//  2. **一格画崩了只有那一格降级**：别的格子照画，整页照常（这一条
//     `TestOneBrokenCellDoesNotBlankOutThePage` 早就钉着，这一组要保证没破坏它）。
//
// # 判据是**分配次数**，不是时间
//
// AGENTS §2：时间类的判据在这台机器上会被负载淹没，而分配次数不会——它是
// 「这一帧建了多少个对象」的计数。

import (
	"bytes"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/service/download"
	"github.com/egoist/mygo/ui"
)

// TestTheDownloadCellKeyDoesNotAllocate 钉住「那一格的键不是每帧拼出来的」。
//
// ## 判据
//
// **稳态下零分配**（`AllocsPerRun`）。
//
// ## 为什么是「零」而不是「少一点」
//
// 这一处是**每帧必做**的（不是偶尔）：每一格每一帧都要算一次那个键。于是
// 「少一点」说不清是修好了还是碰巧，而「零分配」是一个**死数**——要么是零，
// 要么不是。跟 `perframe_test.go` 里那三处（「主题」「满屏画布的键」「波形的
// 键」）是同一个判据，同一个理由。
//
// ## 为什么不用「整帧少了几十次」当判据
//
// 整帧那个数是**别的开销混在里面**的（表格、封面、进度条每一帧都要建一堆
// 元素），而「这一格的那一个键」只有一次分配——在整帧的几百次里减掉一次，
// 判据就变钝了。**单元级量它**才抓得住：改回 `fmt.Sprintf` 的话这里是 1~2，
// 改回来就是 0。
func TestTheDownloadCellKeyDoesNotAllocate(t *testing.T) {
	d := &downloadsPage{failed: map[dlCellKey]bool{}}
	row := download.Snapshot{ID: "BV1歌 甲|歌 甲"}

	allocs := testing.AllocsPerRun(200, func() {
		d.cellKey(row, 3)
	})
	if allocs > 0 {
		t.Errorf("下载记录那一格的键每帧拼一次：每帧 %.1f 次分配——\n"+
			"那个键只用来查一张 map（`d.failed`），查完就扔；\n"+
			"「行 id + 列号」可以是一个**结构体键**（map 的键不必是字符串），\n"+
			"拼成字符串反而多一次分配 ＋ 一次格式化", allocs)
	}
}

// TestTheDownloadCellKeyStillTellsRowsApart 钉住「键还认得出是哪一格」。
//
// ## 判据
//
// 同一个行 id 的**不同列**是**不同的格**；不同的行 id 的同一列也是**不同的
// 格**。而**同一格**（同一个行 id ＋ 同一列）永远认得出是同一格。
//
// ## 为什么这条要紧
//
// 「行 id + 列号」这个键的**两个维度都得在**：只认行 id 的话，一行里崩了的那一格
// 会把**整行**都废掉（用户看到的是整行写「画不出来」）；只认列号的话，**每一行**
// 的那一列都一起废掉（用户看到的是整列）。
//
// 这一条是**实现层**的判据（量的就是那个键本身），所以它不依赖界面——而界面
// 那一层的行为由下面那几条钉着。
func TestTheDownloadCellKeyStillTellsRowsApart(t *testing.T) {
	d := &downloadsPage{failed: map[dlCellKey]bool{}}
	rowA := download.Snapshot{ID: "BV1歌 甲|歌 甲"}
	rowB := download.Snapshot{ID: "BV1歌 乙|歌 乙"}

	if d.cellKey(rowA, 0) == d.cellKey(rowA, 1) {
		t.Error("同一行的两列是同一个键——崩了一格会把整行都废掉")
	}
	if d.cellKey(rowA, 0) == d.cellKey(rowB, 0) {
		t.Error("不同行的同一列是同一个键——崩了一格会把整列都废掉")
	}
	if d.cellKey(rowA, 0) != d.cellKey(rowA, 0) {
		t.Error("同一格两次算出来的键不一样——那张 map 记不住它")
	}
	// ⚠️ **行 id 一样就是同一行**：队列里同一首歌只出现一次，而真出现了两次
	// （用户下了两遍同一首），「行 id + 列号」认不出是哪一遍——这是**这套键的
	// 口径**，不是 bug：崩溃那一格的用户看到的是「这一格画不出来」，而它到底
	// 是哪一遍下载的，用户本来也分不出来。
	if d.cellKey(download.Snapshot{ID: "BV1x"}, 2) != d.cellKey(download.Snapshot{ID: "BV1x"}, 2) {
		t.Error("同一个行 id 的同一列，两次算出来的键不一样")
	}
}

// TestTheBrokenCellFollowsItsRowWhenTheRowsMove 钉住「**哪一格崩了**那个记录
// **对得上行**」。
//
// ## 这一条是这一票最要紧的一条
//
// 行会重排：用户在上面删掉一条，下面的行往上挪一格。而「崩过的那一格」那个
// 记录**必须跟着那一行走**——它是**行 id + 列号**，不是「第几行」。
//
// ## 它会怎么红（改坏实现的时候）
//
// 键改成**行下标**（`row` 而不是 `d.rows[row].ID`）的话：崩的是第 0 行（歌 甲）
// 的第 0 列，用户删掉歌 甲之后，**歌 乙**挪到第 0 行——于是「崩过的那一格」
// 记到了**歌 乙**头上：歌 乙的文件那一格也变成「画不出来」，而真正崩过的
// **歌 甲**已经不在屏幕上了。
//
// 症状是**静默的**：界面上只是「有一格写着画不出来」，用户不会知道那记的是
// 另一首歌。
//
// ## 判据
//
// 删掉崩过的那一行之后，**别的行照常画**（没有多出来的「画不出来」）。
func TestTheBrokenCellFollowsItsRowWhenTheRowsMove(t *testing.T) {
	restore := logOutput(&bytes.Buffer{})
	t.Cleanup(restore)

	// 两条记录：歌 甲（有封面 → 那一格崩）、歌 乙（没封面 → 照画）。
	withCover := newTask("歌 甲", download.StatusRunning, dlProgress, 50)
	withCover.Cover = "http://example.invalid/a.jpg"
	s, tt := newDownloads(t, withCover,
		newTask("歌 乙", download.StatusDone, dlChunk, 100))

	// 把封面缓存摘掉：于是**有封面的那一格**崩，别的格子照画
	// （理由同 `TestOneBrokenCellDoesNotBlankOutThePage`）。
	//
	// ⚠️ 摘掉之后**要还回去**：收尾走的是 `Shell.Close`，它在**调用那一刻**才读
	// `s.images` 这个字段。所以这一句得排在基座挂的那次收尾**前面**跑：
	// `t.Cleanup` 是后挂的先跑。
	keepImages := s.images
	t.Cleanup(func() { s.images = keepImages })
	s.images = nil
	dlFrame(tt)

	if !tt.HasText("画不出来") {
		t.Fatalf("画崩的那一格没有换成占位，文字有 %q", tt.Texts())
	}
	// 崩的是**歌 甲**（有封面那一格）；歌 乙照常。
	if !shows(tt, "歌 乙") {
		t.Fatalf("崩了一格之后整张表空了，文字有 %q", tt.Texts())
	}

	// ⚠️ **现在把崩过的那一行真的删掉**：歌 甲 不在了，歌 乙往上挪到第 0 行。
	// 那个「崩过」的记录必须**跟着歌 甲 走**（它是「行 id + 列号」，不是
	// 「第几行」）。
	//
	// ⚠️ **走的是真引擎那条路**（`RemoveDownload`）：这一页的数据是**每帧**从
	// 队列里读回来的（`syncDownloads`），所以直接改 `d.rows` 下一帧就被覆盖
	// 了——那样测的就不是「用户删掉一行之后会发生什么」。
	if err := s.app.RemoveDownload(s.downloads.rows[0].ID); err != nil {
		t.Fatalf("删掉崩过的那一行：%v", err)
	}
	dlFrame(tt)

	// 判据：**别的行照常画**——没有多出来的「画不出来」。
	//
	// ⚠️ 改坏成「按行下标记」的话：歌 乙挪到第 0 行，而记录还挂在「第 0 行」
	// 上，于是**歌 乙的文件那一格**也变成「画不出来」——上面这一句就红了。
	if tt.HasText("画不出来") {
		t.Errorf("删掉崩过的那一行之后，**别的行**上多了一个「画不出来」，文字有 %q——\n"+
			"「崩过的那一格」必须按**行 id**记（不是行下标）：\n"+
			"行会重排，按下标记的话，用户删掉一行之后崩过的那一格就记到别的行上了",
			tt.Texts())
	}
	// 而歌 乙自己照常画着。
	if !shows(tt, "歌 乙") {
		t.Errorf("删掉崩过的那一行之后，别的行也没了，文字有 %q", tt.Texts())
	}
}

// TestTheDownloadTableIsUnchanged 钉住「这一票**不改画面**」。
//
// ## 判据
//
// 表格的**内容、列宽、位置**全都和改之前一样。
//
// ## 为什么要有这一条
//
// 这一票动的是「怎么算那个键」，而那个键**只用来查一张 map**——按理说画面上
// 一个像素都不该变。但「按理说」不是判据：改着改着把列宽改了、把某几格的
// 内容改了，**画面上看不出来**（表格还是那张表格），而用户看到的是「下载记录
// 页跟以前不一样了」。
//
// 所以这里把改之前的实测值**钉成字面量**：表格的左右两缘、几列的边界、两个
// 歌名的位置、状态/大小/时间三段文字的内容。任何一个动了，这一组就红。
//
// ⚠️ **这些数是量出来的**（`Tester.Find` 给的是画面上的框），而夹具是
// 1200×800 的窗口 + 两条给定快照——见文件头那个「量」的过程。
//
// ⚠️ **只钉「不靠字体度量」的那些锚点**：居中的两列（大小 / 下载时间）的 x
// 是**按字的宽度摆的**（那几个字 25.8 或 51.6 像素宽），而**一段文字有多宽是
// 跟着这台机器的字体走的**——钉死它，换一台机器就会平白地红。
//
// ⇒ 所以居中的那两列钉的是「**它落在哪一段里**」（那几段的边界是按
// `dlStatusW` 那一族的定宽算出来的），而那正是「列宽没变」关心的东西。
func TestTheDownloadTableIsUnchanged(t *testing.T) {
	_, tt := newRows(t,
		dlSnapshot("歌 甲", download.StatusRunning, dlProgress, dlChunk),
		dlSnapshot("歌 乙", download.StatusDone, dlChunk, dlChunk),
	)

	// ⚠️ **表格的左右两缘**：左边 252（文件列内容的起点）、右边 1168（内容区
	// 的右缘）。左边那一列是「吃剩下的」那一个（四个定宽列之外它自己撑满），
	// 右边是内容区那条边——两个都不看字体。
	const tableLeft = 252.0
	const tableRight = 1168.0

	// 「状态」那一列的内容左缘：774（状态文字是**左对齐**的，所以它是按列的
	// 左缘摆的，不看字体）。
	statusLeft, ok := tt.Find("状态")
	if !ok {
		t.Fatalf("表头没有「状态」那一列，文字有 %q", tt.Texts())
	}
	if absf(statusLeft.X-774) > 1 {
		t.Errorf("「状态」那一列的内容在 x=%.1f，want 774——列宽变了", statusLeft.X)
	}
	// 百分比那一段的右缘：904（它是**右对齐**、定宽 40，见 `dlPctW`）。
	pct, ok := tt.Find("50%")
	if !ok {
		t.Fatalf("画面上没有那个百分比，文字有 %q", tt.Texts())
	}
	if absf((pct.X+pct.W)-904) > 1 {
		t.Errorf("百分比的右缘在 %.1f，want 904——状态列的宽变了", pct.X+pct.W)
	}
	// 操作列那两颗图标键：删除那颗的右缘 1158、打开那颗的左缘 1088（两颗都是
	// 定宽 32，见 `dlOpW`；中间那道缝是 6）。
	del, ok := tt.Find("删除")
	if !ok {
		t.Fatalf("画面上没有那个删除按钮，文字有 %q", tt.Texts())
	}
	if absf((del.X+del.W)-1158) > 1 {
		t.Errorf("删除按钮的右缘在 %.1f，want 1158——操作列的宽变了", del.X+del.W)
	}
	open, ok := tt.Find("打开")
	if !ok {
		t.Fatalf("画面上没有那个打开按钮，文字有 %q", tt.Texts())
	}
	if absf(open.X-1088) > 1 {
		t.Errorf("打开按钮在 x=%.1f，want 1088——操作列的宽变了", open.X)
	}

	// ⚠️ **居中的那两列钉「落在哪一段里」**：大小列 914..990、下载时间列
	// 990..1078、操作列 1078..1168。那几个边界是从状态列的右缘（914）加上
	// 三个定宽（`dlSizeW` / `dlTimeW` / `dlOpsW`）算出来的。
	sizeLo := float32(914)
	sizeHi := sizeLo + float32(dlSizeW)
	timeLo := sizeHi
	timeHi := timeLo + float32(dlTimeW)
	opsHi := timeHi + float32(dlOpsW)
	if absf(opsHi-tableRight) > 1 {
		t.Errorf("按那几个定宽算出来操作列的右缘是 %.0f，want %.0f——列宽那一族变了",
			opsHi, tableRight)
	}
	for _, c := range []struct {
		text   string
		lo, hi float32
	}{
		{"大小", sizeLo, sizeHi},
		{"下载时间", timeLo, timeHi},
		{"操作", timeHi, opsHi},
	} {
		r, ok := tt.Find(c.text)
		if !ok {
			t.Errorf("表头没有「%s」那一列，文字有 %q", c.text, tt.Texts())
			continue
		}
		if r.X < c.lo || r.X > c.hi {
			t.Errorf("「%s」那一列在 x=%.1f，而它该落在 %.0f..%.0f 里——列宽变了",
				c.text, r.X, c.lo, c.hi)
		}
	}

	// 两个歌名的位置：第一行 (252, 173)、第二行 (252, 233)（行高 60）。
	for _, c := range []struct {
		text string
		x, y float32
	}{
		{"歌 甲", tableLeft, 173},
		{"歌 乙", tableLeft, 233},
	} {
		r, ok := tt.Find(c.text)
		if !ok {
			t.Errorf("画面上没有 %q 那一行，文字有 %q", c.text, tt.Texts())
			continue
		}
		if absf(r.X-c.x) > 1 || absf(r.Y-c.y) > 1 {
			t.Errorf("%q 那一行在 (%.1f, %.1f)，want (%.1f, %.1f)——行的位置变了",
				c.text, r.X, r.Y, c.x, c.y)
		}
	}

	// 三段内容（状态 / 大小 / 下载时间）照旧写着那几句。
	for _, want := range []string{"下载中", "下载完成", "20.0 MB", "2023-11-15"} {
		if !shows(tt, want) {
			t.Errorf("画面上没有 %q，文字有 %q", want, tt.Texts())
		}
	}
}

// TestTheDownloadsPageDoesNotAllocatePerCell 钉住「整页每一帧**不因为这个键**
// 多分配」。
//
// ## 判据
//
// 这一页**每一帧**的分配次数，和「这个键不分配」之前那个数**不许差出一截**。
//
// ## 为什么要有这一条（单元级那条不够）
//
// `TestTheDownloadCellKeyDoesNotAllocate` 量的是**那一个键**。而这一条量的是
// **整页**：它抓的是「改了实现，但别处又冒出一处每帧的分配」——比如把
// `fmt.Sprintf` 换掉了，却在别处（兜底那一支、记崩过的那一支）又拼了一次。
//
// ## 怎么量
//
// 同一块界面、同一个夹具，两段比：这一页**画那一支**（`viewDownloadList`）在
// 键不分配之后，每帧的分配次数**不该**比「八行 × 五列」多出四十次——
// 那是「键还是每帧拼一次」那一版的数。
//
// ⚠️ **用相对判据，不用绝对数**：整页里混着表格、封面、进度条每一帧都要建的
// 那一堆元素，而那些的数会跟着别处的改动变。**这一条只比「有没有多出四十次
// 那一截」**。
func TestTheDownloadsPageDoesNotAllocatePerCell(t *testing.T) {
	rows := make([]download.Snapshot, 8)
	for i := range rows {
		rows[i] = dlSnapshot(
			"歌 "+string(rune('A'+i)),
			download.StatusDone, dlChunk, dlChunk)
	}

	b := newTestShell(t,
		reduceMotion(),
		withView(func(s *Shell) func(*ui.Context) {
			return func(c *ui.Context) { s.viewDownloadList(c, nil) }
		}),
	)
	b.s.downloads.sync(rows)

	// 稳态：先画几帧（字形进图集、布局定下来——理由同帧预算那一组）。
	for range 8 {
		b.tt.Frame()
	}
	if b.tt.FramePending() {
		t.Fatal("这一页在稳态下还在逐帧要帧——「一帧」这个单位已经不是一帧了")
	}

	allocs, bytes := measureAllocs(func() {
		for range 50 {
			b.tt.Frame()
		}
	})
	perFrame := float64(allocs) / 50
	t.Logf("下载记录页（八行）：每帧 %.1f 次分配 / %.0f 字节", perFrame, float64(bytes)/50)

	// ⚠️ **上限是「实测值 + 四十次」**：四十次是「八行 × 五列」——**键还是每帧
	// 拼一次**那一版的数（每格一次 `fmt.Sprintf`）。所以这一条的上限是
	// 「现在这个数 + 四十」——它拦得住「键改回拼字符串」，也拦得住「别处又冒出
	// 一处每帧的分配」。
	//
	// ⚠️ **而它不拦「整页多画了一点别的」**：那一截是别的票的账。
	const perCellBudget = 40
	if perFrame > float64(baselineDownloadsPerFrame+perCellBudget) {
		t.Errorf("下载记录页每一帧分配了 %.1f 次，want ≤ %.1f"+
			"（基线 %d + %d）——\n"+
			"多出来的 %.0f 次是「一屏八行 × 五列」那一截（每格一次）：\n"+
			"那个键只用来查一张 map，查完就扔，不该每帧拼一次字符串",
			perFrame, float64(baselineDownloadsPerFrame+perCellBudget),
			baselineDownloadsPerFrame, perCellBudget,
			perFrame-baselineDownloadsPerFrame)
	}
}

// baselineDownloadsPerFrame 是「键不分配」之后这一页每帧的分配次数。
//
// ⚠️ **它是一个实测值**（八行、隔离地画这一页那一支、1200×800），
// 而**不是**一个「应该有多少」的理论数——这一页每帧还要建表格、封面、进度条
// 那些元素，那一笔会跟着别处的改动变。
//
// ⇒ 所以它是**下界**：往上飘一点没关系（别的票改了别的东西），而**掉下去**
// 就是「这一屏几乎没画东西」——那正是帧预算那一档的 `floor` 拦的事。
const baselineDownloadsPerFrame = 180
