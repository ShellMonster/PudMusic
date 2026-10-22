package ui

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/localmusic"
	mygoui "github.com/egoist/mygo/ui"
)

// 本地音乐页这一组测试跑在真实的 Shell 上：真 App、真设置文件、真音乐目录、
// 真画一帧、真点按钮。
//
// 断言的东西只有三类：**画面上的文字**（这一页有没有真画出来）、
// **磁盘上的文件**（删到底删没删）、**设置里的目录**（加/移除存没存住）。
// 不断言「调了哪个函数」——那只能证明状态层有那个方法，证明不了界面上真有
// 一个入口。三档按钮摆在文件里没人调用过，就是这么漏过去的。

// shellOnLocalMusic 造一个停在本地音乐页上的外壳：真设置、真目录、真扫描。
//
// ⚠️ `pickDir` 换成一个记下「选了什么」的桩：系统对话框在无头 Tester 里弹
// 不出来（而且它**必须**从后台 goroutine 调，见 localmusic.go 的 addDirectory）。
// 桩返回什么，这一组测的就是「用户选了这个目录之后设置有没有写进去」。
func shellOnLocalMusic(t *testing.T, dirs ...string) (*mygoui.Tester, *Shell, *app.App, *dirPicker) {
	t.Helper()

	pick := &dirPicker{}
	// ⚠️ **不挂文件监听**：测试里换掉 `s.local.pickDir` 和 wake 都很方便，
	// 而真监听要系统事件循环（`WhenReady`），无头环境里根本起不来。
	// 监听那一半由 app 层的桩验（`internal/app/localmusic_test.go`）。
	b := newTestShell(t,
		withSettings(func() app.Settings {
			st := app.DefaultSettings("")
			st.LocalMusicDirs = dirs
			return st
		}()),
		mounted(func(_ *testing.T, a *app.App) { a.MountLocalMusic(app.LocalMusic{}) }),
		stubbed(func(_ *testing.T, s *Shell) { s.local.pickDir = pick.pick }))
	a, s, tt := b.a, b.s, b.tt
	openLocal(t, tt)
	settleLocal(t, a, tt)
	return tt, s, a, pick
}

// dirPicker 是「添加目录」那个系统对话框的桩。
type dirPicker struct {
	mu    sync.Mutex
	next  string
	err   error
	calls int
}

// pick 是给 `localPage.pickDir` 用的那一个。
func (d *dirPicker) pick() (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	return d.next, d.err
}

func (d *dirPicker) set(dir string) {
	d.mu.Lock()
	d.next = dir
	d.mu.Unlock()
}

func (d *dirPicker) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// openLocal 从侧栏切到本地音乐页。
func openLocal(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	if err := tt.Click("本地音乐"); err != nil {
		t.Fatalf("侧栏上点不到「本地音乐」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
}

// settleLocal 等后台那一遍扫描落地，然后画下一帧。
//
// ⚠️ **不能只 `tt.Frame()`**：那一遍扫描跑在后台 goroutine 上，第一帧发起的
// 时候还没扫完，画出来的还是「正在扫描…」。等 `Scanning` 变 false 才画下一帧。
func settleLocal(t *testing.T, a *app.App, tt *mygoui.Tester) {
	t.Helper()
	waitFrames(t, tt, "本地音乐那一遍扫描落地", func() bool { return !a.LocalMusicState().Scanning })
	tt.Frame()
}

// 每一遍都要画帧，这不是多余的：状态变更（比如「设置里加了一个目录」）要等页面
// 的下一帧跑过 `begin` 才被应用状态看见——`begin` 里对着设置核一遍根，对不上才
// `SetLocalRoots` 并重扫。只在循环里等而不画帧，那个条件永远等不到，于是断言在
// 扫描**还没开始**的时候就跑完了。
// （`TestAddingADirectoryStoresItAndScans` 原来就是这么随机红的。）

// libSong 在 dir 里造一个内容随便的 .mp3。
func libSong(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// pathsOf 把筛选之后那一批压成路径列表，断言行序时比逐个字段比短。
func pathsOf(rows []localmusic.Track) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Path
	}
	return out
}

// exists 报路径还在不在。
func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// counter 记一个函数被调了几次。
//
// ⚠️ **它自己加锁不是多余的**：装进唤醒句柄里的那个是被后台扫描 goroutine
// 调的，而测试那条 goroutine 同时在读它。不加锁的话 `-race` 会在这里报一条
// **测试自己**的竞态，而它的栈顶正好落在「唤醒」这条链上——看着像生产代码
// 的问题，其实不是（`widgets/list` 的 `paged_test.go` 踩过同一个坑）。
type counter struct {
	mu sync.Mutex
	n  int
}

func (c *counter) add() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *counter) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// typeInto 点进那个输入框然后敲字。
//
// ⚠️ 两步都要：只 `Type` 不点的话字进不去（焦点不在那个框上），而只点不敲
// 的话输入框里是空的——两种都验不出「搜索框真的在筛」。
func typeInto(t *testing.T, tt *mygoui.Tester, placeholder, text string) {
	t.Helper()
	// ⚠️ `SetFocused(true)` 是必须的：MyGo 的文本输入要窗口有键盘焦点才收
	// `TextInput` 事件（navbar_test.go 顶上那条注释就是为它写的）。
	tt.SetFocused(true)
	if err := tt.Click(placeholder); err != nil {
		t.Fatalf("点不到输入框 %q：%v，文字有 %q", placeholder, err, tt.Texts())
	}
	tt.Frame()
	tt.Type(text)
}

// ─────────────────────────────────────────────────────────────────────────────
// 这一页画出来了没有
// ─────────────────────────────────────────────────────────────────────────────

// TestTheLocalMusicPageShowsItsColumns 验切过去看到的是本地音乐页。
//
// 逐项点名七列，因为这一页最可能的失败是「画出来了，但少了一列」——
// 用户看见一个没有「时长」的列表，不会知道还有一列。
func TestTheLocalMusicPageShowsItsColumns(t *testing.T) {
	root := t.TempDir()
	libSong(t, root, "第一首.mp3")
	libSong(t, root, "第二首.flac")

	tt, s, _, _ := shellOnLocalMusic(t, root)

	for _, want := range []string{"本地音乐", "#", "标题", "大小", "格式", "时长", "创建时间"} {
		if !tt.HasText(want) {
			t.Errorf("本地音乐页上没有 %q，文字有 %q", want, tt.Texts())
		}
	}
	for _, want := range []string{"第一首", "第二首", "FLAC", "10 B"} {
		if !tt.HasText(want) {
			t.Errorf("列表里没有 %q，文字有 %q", want, tt.Texts())
		}
	}
	// 「不是占位页」这一条要能失败才算数：没接上 builtPage 的话这一页会退回
	// 空状态，而空状态里没有这些列名——上面那几条断言已经能抓到，但这条把
	// 原因说清楚。
	if s.builtPage(lookup(localMusicPath)) == nil {
		t.Error("本地音乐页没接上 builtPage，画出来的是占位页")
	}
}

// TestTheTableAndSongSlotsSwitch 钉住这一页自己的那一排切换。
//
// ⚠️ **这一页的开关现在是两档（表格 / 歌曲），不是三档**：这一页原来自己管着
// 「表格 / 列表 / 网格」，而设置里的「数据显示样式」管的是同一件事——两个控件抢一件
// 事，用户改了设置却看见这一页没反应，还不知道该看谁。分工见 localmusic.go 文件头
// 那一段：这一页的开关管「表格还是歌曲」，摆成什么样由设置说了算。
//
// 「歌曲」那一档跟着设置换视图，由 `TestEveryPageFollowsTheDisplayStyleSetting`
// 那一组里本地音乐那一条钉住（它量的正是行高 / 列数变了）。
func TestTheTableAndSongSlotsSwitch(t *testing.T) {
	root := t.TempDir()
	libSong(t, root, "第一首.mp3")
	libSong(t, root, "第二首.mp3")

	tt, _, _, _ := shellOnLocalMusic(t, root)

	// 默认是表格：有列头。
	if !tt.HasText("创建时间") {
		t.Fatalf("默认不是表格那一档，文字有 %q", tt.Texts())
	}
	for _, style := range localStyles {
		if !tt.HasText(style) {
			t.Errorf("样式切换里没有 %q", style)
		}
	}

	// 切到歌曲：列头没有了（那是表格才有的），卡片那一档在画。
	if err := tt.Click(localStyles[localStyleSong]); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("创建时间") {
		t.Errorf("切到歌曲之后列头还在，样式没切过去：%q", tt.Texts())
	}
	if !tt.HasText("第一首") {
		t.Errorf("切到歌曲之后歌没了：%q", tt.Texts())
	}

	// 切回表格：列头回来。
	if err := tt.Click(localStyles[localStyleTable]); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("创建时间") {
		t.Errorf("切回表格之后列头没回来：%q", tt.Texts())
	}
	if !tt.HasText("第二首") {
		t.Errorf("切回表格之后歌没了：%q", tt.Texts())
	}
}

// TestTheEmptyStateSaysWhyAndHow 钉住没目录时的那两句。
//
// 两句话必须分开，而且第二句得说得出**怎么做**。
func TestTheEmptyStateSaysWhyAndHow(t *testing.T) {
	tt, _, _, _ := shellOnLocalMusic(t)

	if !tt.HasText("还没有添加音乐目录") {
		t.Errorf("没说为什么空，文字有 %q", tt.Texts())
	}
	if !tt.HasText("添加目录") {
		t.Errorf("没说怎么才能不空，文字有 %q", tt.Texts())
	}
}

// TestTheEmptyStateSaysWhyAndHowForAnEmptyFolder 钉住「加了目录但里面没有歌」。
//
// 这和「没加目录」是两种不同的空，原因和做法都不一样。
func TestTheEmptyStateSaysWhyAndHowForAnEmptyFolder(t *testing.T) {
	root := t.TempDir()
	tt, _, _, _ := shellOnLocalMusic(t, root)

	if !tt.HasText("这里没有音频文件") {
		t.Errorf("加了空目录却说「还没有添加音乐目录」，文字有 %q", tt.Texts())
	}
	if !tt.HasText("第一层") {
		t.Errorf("没说清楚只扫一层，文字有 %q", tt.Texts())
	}
}

// TestTheFirstFrameDoesNotClaimThereIsNoMusic 钉住「正在扫描」。
//
// ⚠️ 第一遍扫完之前说「这里没有歌」是在报一件还没发生的事：一个几千文件的
// 目录要扫好几秒，那几秒里用户看到的是「这里没有歌」，然后歌自己冒出来。
//
// 这一条验的是**顺序**：第一帧就该是「正在扫描…」，而不是空状态。
func TestTheFirstFrameDoesNotClaimThereIsNoMusic(t *testing.T) {
	root := t.TempDir()
	for i := range 50 {
		libSong(t, root, string(rune('a'+i%26))+string(rune('a'+i/26))+".mp3")
	}
	b := newTestShell(t,
		withSettings(func() app.Settings {
			st := app.DefaultSettings("")
			st.LocalMusicDirs = []string{root}
			return st
		}()),
		mounted(func(_ *testing.T, a *app.App) { a.MountLocalMusic(app.LocalMusic{}) }))
	s, a, tt := b.s, b.a, b.tt
	s.navigate(localMusicPath)
	tt.Frame()

	// ⚠️ 这一帧是**同步**画完的（`Frame()` 返回时画面就是它），而扫描在
	// 后台。所以要么看见「正在扫描」，要么扫描快到已经画完了——两个都接受，
	// 只有「看见空状态」是错的。
	texts := strings.Join(tt.Texts(), "|")
	if tt.HasText("还没有添加音乐目录") || tt.HasText("这里没有音频文件") {
		t.Errorf("第一帧说的是空状态，可扫描还没完：%q", texts)
	}
	settleLocal(t, a, tt)
}

// TestAnUnplayableFormatIsNamedAsSuch 钉住 WMA 那条缺口。
//
// PLAN §5.4 的原话：「这是真实的、功能性的缺口…不要含糊过去」。一个 `.wma`
// 列出来却不说我们放不了，用户会以为文件坏了。
func TestAnUnplayableFormatIsNamedAsSuch(t *testing.T) {
	root := t.TempDir()
	libSong(t, root, "老歌.wma")
	libSong(t, root, "新歌.mp3")

	tt, _, _, _ := shellOnLocalMusic(t, root)

	if !tt.HasText("老歌") {
		t.Errorf("WMA 该列出来，文字有 %q", tt.Texts())
	}
	if !tt.HasText("我们还放不出的格式") {
		t.Errorf("没有说这个格式放不了，文字有 %q", tt.Texts())
	}
	if !tt.HasText(".wma") {
		t.Errorf("没有点名是哪一种格式，文字有 %q", tt.Texts())
	}
}

// TestDirectoryLabelsAreAllDifferent 钉住「目录选项不许重名」。
//
// ⚠️ 这一条来自评审：下拉框的**选项就是值**，而选中项是按值反查路径的。
// 两个选项同名 → 反查永远命中第一个 → 选第三项筛出来的却是第二个目录，
// 「移除此目录」删的也是第二个。
//
// 评审给的反例是 `/a/x/music`、`/b/x/music`、`/c/x/music`：原来「撞了往前
// 多带一段」只躲得开**一个**重名，第三项又和第二项撞上。
//
// 断的是**两两不同**这件事本身，不是「看起来不一样」。
func TestDirectoryLabelsAreAllDifferent(t *testing.T) {
	roots := []string{
		"/a/x/music", "/b/x/music", "/c/x/music", // 三条同尾
		"/a/music",       // 和第一条同尾
		"/完全一样", "/完全一样", // 两条一模一样
	}
	labels := dirLabels(roots)

	if len(labels) != len(roots)+1 {
		t.Fatalf("标签有 %d 个，want %d（一个「全部目录」+ 每个根一个）", len(labels), len(roots))
	}
	seen := map[string]int{}
	for i, l := range labels[1:] { // labels[0] 是「全部目录」
		if prev, dup := seen[l]; dup {
			t.Errorf("第 %d 和第 %d 个根的标签撞了：%q（%v 和 %v）",
				prev, i, l, roots[prev], roots[i])
		}
		seen[l] = i
	}
	// 而且每一个都要能反查回**它自己**那一个根。
	for i, r := range roots {
		p := &localPage{dirLabel: labels[i+1]}
		if got := p.currentRoot(roots); got != r {
			t.Errorf("选中标签 %q 反查得到 %q，want %q（反查撞到了别的目录）",
				labels[i+1], got, r)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 票 #175：行 hover 的播放图标、会展开的搜索
// ─────────────────────────────────────────────────────────────────────────────

// TestTheLocalTableRowHasNoSolidAccentBlock 钉验收第 1 条在**表格**那一档：
// 点一行不再整行刷实心主色底。
//
// ⚠️ **这一页有两档，两档都要断**：表格那一档是框架的 `ui.Table`（它给选中的行
// 铺主色底的方式和「歌曲」那一档自己画的列表不同），「歌曲」那一档是
// `widgets/list` 的卡片控件。只断一档的话，另一档还是一整块绿——而用户在同一个
// 页面里切一下档就看见了。
//
// ⚠️ **判据是「主色像素多了几个」**：那一行本来就有别的东西（序号、标题、按钮），
// 所以数的是**点之前和点之后那一行的主色像素差**，不是「那��行有没有主色」。
func TestTheLocalTableRowHasNoSolidAccentBlock(t *testing.T) {
	dir := t.TempDir()
	libSong(t, dir, "甲.mp3")
	libSong(t, dir, "乙.mp3")
	tt, s, _, _ := shellOnLocalMusic(t, dir)
	s.local.style = localStyleTable
	waitFrames(t, tt, "把文件扫回来", func() bool { return len(s.local.rows) >= 2 })
	tt.Frame()

	r, ok := tt.Find("甲")
	if !ok {
		t.Fatalf("画面上找不到「甲」，文字有 %q", tt.Texts())
	}
	before := accentPixelsIn(tt, r)
	if err := tt.Click("甲"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	after := accentPixelsIn(tt, r)
	if after > before {
		t.Errorf("点过那一行之后主色像素从 %d 变成 %d——表格那一档还在整行刷实心主色底",
			before, after)
	}
	// 状态还是选中的（去掉的是**颜色**，不是选中这件事）。
	//
	// ⚠️ **比的是歌名而不是行号**：文件按名字排过（「乙」在「甲」前面），所以
	// 「甲」是第二行不是第一行——钉死行号的话，改一次排序就飘。
	if s.local.sel < 0 || s.local.sel >= len(s.local.rows) ||
		s.local.rows[s.local.sel].Title != "甲" {
		t.Errorf("点了「甲」，选中的却是第 %d 行（%q）",
			s.local.sel+1, s.local.rows[s.local.sel].Title)
	}
}

// accentPixelsIn 是那个框里「主色」的像素有几个。
//
// ⚠️ **判的是绿调**（绿通道比红通道高出一截），和 `partdrawer_test.go` 的
// `accentGreenGap` 同一个门槛：主题里那个 Accent 是 {30 215 96}（绿 − 红 = 185），
// 而正文和底色都是灰的（差 0）——所以这一个条件既认得出主色，又不会把字算进去。
func accentPixelsIn(tt *mygoui.Tester, r mygoui.Rect) int {
	img := tt.Image()
	n := 0
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
				continue
			}
			c := img.RGBAAt(x, y)
			if int(c.G)-int(c.R) >= accentGreenGap {
				n++
			}
		}
	}
	return n
}

// TestHoveringALocalRowTurnsItsNumberIntoAPlayIcon 行 hover 时序号换成播放图标。
//
// 出处是上游 `local-music/item.tsx:57-64`：`group-hover:hidden` 藏序号、
// `hidden group-hover:inline` 亮 `RiPlayFill`（16）。
//
// 判据是画面上**那枚图标的名字**：它只在 hover 的那一行画出来。三条——
// 平时没有、指到第一首有、换到第二首时它跟着换行，最后指针离开表格又消失。
func TestHoveringALocalRowTurnsItsNumberIntoAPlayIcon(t *testing.T) {
	root := t.TempDir()
	libSong(t, root, "第一首.mp3")
	libSong(t, root, "第二首.mp3")
	tt, _, _, _ := shellOnLocalMusic(t, root)

	if _, ok := tt.Find(localPlayIconLabel); ok {
		t.Fatalf("没有 hover 就有播放图标")
	}
	first, ok := tt.Find("第一首")
	if !ok {
		t.Fatalf("找不到第一首，文字有 %q", tt.Texts())
	}
	tt.Move(first.X+first.W/2, first.Y+first.H/2)
	// 两帧：hover 是上一帧的布局决定的。
	tt.Frame()
	tt.Frame()
	icon, ok := tt.Find(localPlayIconLabel)
	if !ok {
		t.Fatalf("hover 第一首之后没有播放图标，文字有 %q", tt.Texts())
	}
	if icon.W < 15 || icon.W > 17 || icon.H < 15 || icon.H > 17 {
		t.Errorf("播放图标是 %.0f×%.0f，want 16×16（上游 `RiPlayFill size={16}`）",
			icon.W, icon.H)
	}
	if icon.Y+icon.H < first.Y || icon.Y > first.Y+first.H {
		t.Errorf("播放图标画在 %.1f…%.1f，不在第一首那一行（%.1f…%.1f）里",
			icon.Y, icon.Y+icon.H, first.Y, first.Y+first.H)
	}

	// ⚠️ **行的上/下各 6 像素也算「压在这一行」**：那 6 像素是 `ui.Table` 给
	// 每一行的内边距（格子本身只有 32 高，行是 44 高）。只按格子算的话，指针在
	// 那两圈上时图标不出现，图标会跟着指针上下闪（上游的 `group-hover` 是整行
	// 都亮的）。
	for _, where := range []struct {
		what string
		y    float32
	}{
		{"行的上边", first.Y + 2},
		{"行的下边", first.Y + first.H - 2},
	} {
		tt.Move(first.X+first.W/2, where.y)
		tt.Frame()
		tt.Frame()
		if _, ok := tt.Find(localPlayIconLabel); !ok {
			t.Errorf("指针在%s（y=%.1f）时没有播放图标——那一行不全是热区",
				where.what, where.y)
		}
	}

	// 指到第二首：图标跟着换行。
	second, ok := tt.Find("第二首")
	if !ok {
		t.Fatalf("找不到第二首，文字有 %q", tt.Texts())
	}
	tt.Move(second.X+second.W/2, second.Y+second.H/2)
	tt.Frame()
	tt.Frame()
	icon, ok = tt.Find(localPlayIconLabel)
	if !ok {
		t.Fatalf("hover 第二首之后没有播放图标")
	}
	if icon.Y+icon.H < second.Y || icon.Y > second.Y+second.H {
		t.Errorf("hover 第二首时播放图标画在 %.1f…%.1f，不在第二首那一行（%.1f…%.1f）",
			icon.Y, icon.Y+icon.H, second.Y, second.Y+second.H)
	}

	// 指针离开表格（播放条那条空地）：图标收回去。
	tt.Move(600, 795)
	tt.Frame()
	tt.Frame()
	if _, ok := tt.Find(localPlayIconLabel); ok {
		t.Errorf("指针离开表格之后播放图标还在")
	}
}

// TestTheLocalSearchIsACollapsedButton 本地音乐的搜索也是一枚会展开的图标键。
//
// 上游那一页用的是同一个 `SearchButton`（`local-music/index.tsx:236`）；而这一页
// 的工具条在最小窗口上本来就挤（搜索框 180 的时候侧栏一拉宽，「重新扫描」整个
// 被挤到窗口外面），收起来正好也把那 152 像素还回去。
func TestTheLocalSearchIsACollapsedButton(t *testing.T) {
	root := t.TempDir()
	libSong(t, root, "歌.mp3")
	tt, _, _, _ := shellOnLocalMusic(t, root)

	r, ok := tt.Find(searchLocalLabel)
	if !ok {
		t.Fatalf("找不到搜索，文字有 %q", tt.Texts())
	}
	// 收起态那枚是 `iconHitMd` × `iconMd` 的一枚图标键（上游 HeroUI 的
	// `isIconOnly` 恒是 32×32，见 `icons.go` 的 `iconHit`）。
	if r.W > iconHitMd || r.H > iconMd {
		t.Errorf("搜索刚进来就是 %.0f×%.0f——它该是一枚图标键（%d×%d）",
			r.W, r.H, iconHitMd, iconMd)
	}
	if err := tt.Click(searchLocalLabel); err != nil {
		t.Fatalf("点不开搜索：%v", err)
	}
	tt.Frame()
	r, ok = tt.Find(searchLocalLabel)
	if !ok {
		t.Fatalf("点开之后搜索不见了，文字有 %q", tt.Texts())
	}
	if r.W <= iconHitMd {
		t.Fatalf("点开之后还是 %.0f 宽——搜索没有展开", r.W)
	}
}
