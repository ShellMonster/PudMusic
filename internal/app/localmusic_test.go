package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/localmusic"
	"github.com/ShellMonster/PudMusic/internal/service/localmusic/localmusictest"
)

// 本地音乐这一摊的测试跑在真磁盘上：真的建目录、真的写文件、真的删文件。
//
// 断言的东西是**扫完之后列表里有什么**和**磁盘上还剩下什么**，不是
// 「调了哪个函数」。中间那一遍扫描跑在后台 goroutine 里，所以每个用例都
// 用 `waitScan` 等它落地——不等的话测试就是在和调度赛跑。

// libFixture 是一个挂好了本地音乐库的 App，加两个计数桶。
type libFixture struct {
	app   *App
	wakes *counter
	watch *stubWatch
	// trash 是「系统回收站」那一份假零件。删文件走的是它（票 #558：
	// 删是「移到废纸篓」，不是永久删除）。
	trash *localmusictest.Fake
	// bin 是假废纸篓放在哪儿：搬走的文件在那儿，用户「找得回来」。
	bin string
}

// counter 记一个函数被调了几次。它自己加锁：唤醒是从后台 goroutine 来的。
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

// stubWatch 是不碰框架的假监听：记下盯了哪些目录，改动由测试自己触发。
type stubWatch struct {
	mu      sync.Mutex
	dirs    []string
	changes map[string][]func()
	stopped int
}

func newStubWatch() *stubWatch {
	return &stubWatch{changes: map[string][]func(){}}
}

func (w *stubWatch) Watch(_ context.Context, dir string, onChange func()) (func(), error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.dirs = append(w.dirs, dir)
	w.changes[dir] = append(w.changes[dir], onChange)
	var once sync.Once
	return func() {
		once.Do(func() {
			w.mu.Lock()
			w.stopped++
			w.mu.Unlock()
		})
	}, nil
}

// fire 假装那个目录变了。**返回之后**回调已经跑完了（它是同步调的），
// 但扫描还在后台，所以用例仍然要 waitScan。
func (w *stubWatch) fire(dir string) {
	w.mu.Lock()
	fns := append([]func(){}, w.changes[dir]...)
	w.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

func (w *stubWatch) watching() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.dirs)
}

func (w *stubWatch) stoppedCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stopped
}

// newLib 造一个挂好本地音乐的 App。
//
// ⚠️ **它一定装上回收站**：删文件走的是「移到系统废纸篓」那一步（票 #558），
// 而这一层**没有**回收站就会报 `ErrNoTrash`——那是「应用自己没装好」，
// 不是这一组用例要验的东西。
func newLib(t *testing.T, roots ...string) *libFixture {
	t.Helper()
	a := New()
	t.Cleanup(a.Close)

	wakes := &counter{}
	w := newStubWatch()
	bin := filepath.Join(t.TempDir(), "废纸篓")
	trash := localmusictest.Success(bin)
	a.SetWake(wakes.add)
	a.MountLocalMusic(LocalMusic{Roots: roots, Watch: w, Trash: trash})
	return &libFixture{app: a, wakes: wakes, watch: w, trash: trash, bin: bin}
}

// waitScan 等那一遍后台扫描落地。
//
// 不用固定 sleep：睡多久都只有一半的把握（CI 上慢、扫得多），而轮询状态是
// 「真的好了」才算好。上限是防「永远等不到」的那种死等。
func waitScan(t *testing.T, a *App) LocalMusicState {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		st := a.LocalMusicState()
		if !st.Scanning {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了 10 秒扫描还没完：%d 首", len(st.Tracks))
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// song 建一个内容随便的 .mp3。
func song(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestScanningShowsTheRealFilesOnDisk 钉住最基本的一条：设置里给了目录，
// 扫完列表里就是磁盘上那些文件。
func TestScanningShowsTheRealFilesOnDisk(t *testing.T) {
	root := t.TempDir()
	song(t, root, "甲.mp3")
	song(t, root, "乙.flac")
	f := newLib(t, root)

	f.app.StartLocalMusicWatch()
	st := waitScan(t, f.app)

	if len(st.Tracks) != 2 {
		t.Fatalf("扫到 %d 首，want 2：%+v", len(st.Tracks), st.Tracks)
	}
	if st.Watching != 1 {
		t.Errorf("盯了 %d 个目录，want 1", st.Watching)
	}
}

// TestTheFirstScanIsNotOnTheCallingThread 钉住「扫描在后台」。
//
// MountLocalMusic 之后立刻问一次，答案应该是「正在扫」或者「已经扫完了」，
// 但**不能**是「扫了 500ms 才返回」——那就说明它跑在调用者的线程上。
//
// 换成实打实的判据：调用 RescanLocalMusic 的时间必须远小于扫描真正要花的
// 时候（一个几千文件的目录），否则就是同步的。
func TestTheFirstScanIsNotOnTheCallingThread(t *testing.T) {
	root := t.TempDir()
	for i := range 200 {
		song(t, root, string(rune('a'+i%26))+string(rune('a'+i/26))+".mp3")
	}
	f := newLib(t, root)

	start := time.Now()
	f.app.RescanLocalMusic()
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("RescanLocalMusic 花了 %v，它把扫描跑在调用线程上了", elapsed)
	}
	waitScan(t, f.app)
}

// TestABackgroundResultWakesTheInterface 钉住铁律三。
//
// 后台扫完了必须叫醒界面，不唤醒就是「结果到了、画面停在旧帧」（实测 0 帧
// 刷新）。这一条最容易被漏：状态更新对不对是一回事，界面会不会重画是另一
// 回事，而后者漏了没有任何一条别的测试会红。
//
// 拿掉修复的方式：把 `scan()` 里的 `wake()` 调用删掉。
func TestABackgroundResultWakesTheInterface(t *testing.T) {
	root := t.TempDir()
	song(t, root, "歌.mp3")
	f := newLib(t, root)

	before := f.wakes.get()
	f.app.StartLocalMusicWatch()
	st := waitScan(t, f.app)
	if len(st.Tracks) != 1 {
		t.Fatalf("扫到 %d 首", len(st.Tracks))
	}
	waitFor(t, func() bool { return f.wakes.get() > before }, "后台扫完没有叫醒界面")
}

// TestAFinishedScanAlreadyShowedUpInTheList 钉住「投递之后不用等用户点什么」。
//
// 这条和上一条是一对：唤醒是为了重画，而重画时读的那份状态**已经**是新的一
// 份。只投递不唤醒 = 界面上永远是旧帧（实测 0 帧刷新）。
func TestAFinishedScanAlreadyShowedUpInTheList(t *testing.T) {
	root := t.TempDir()
	song(t, root, "歌.mp3")
	f := newLib(t, root)

	f.app.StartLocalMusicWatch()
	waitScan(t, f.app)

	// 唤醒那一刻读到的必须是扫完的那一份，不是一个还要再等一帧的空列表。
	if got := len(f.app.LocalMusicState().Tracks); got != 1 {
		t.Errorf("扫描刚落地时列表里 %d 首，want 1", got)
	}
}

// TestAWatchedChangeRescans 钉住文件监听那一半。
//
// 用户往音乐目录里拷进一首歌，这一页该自己更新，不用他点刷新。
func TestAWatchedChangeRescans(t *testing.T) {
	root := t.TempDir()
	song(t, root, "第一首.mp3")
	f := newLib(t, root)

	f.app.StartLocalMusicWatch()
	if got := len(waitScan(t, f.app).Tracks); got != 1 {
		t.Fatalf("起手 %d 首", got)
	}

	song(t, root, "第二首.mp3")
	f.watch.fire(root)

	if got := len(waitScan(t, f.app).Tracks); got != 2 {
		t.Errorf("目录变了之后扫到 %d 首，want 2", got)
	}
}

// TestChangingRootsRescansAndMovesTheWatches 钉住换目录那一条路。
//
// 设置里换了目录之后：① 列表变成新的，② 监听挪到新的根上，旧的停掉。
// ②漏了的话就是「旧目录还在被监听，用户在新目录里加歌这一页不动」。
func TestChangingRootsRescansAndMovesTheWatches(t *testing.T) {
	oldRoot, newRoot := t.TempDir(), t.TempDir()
	song(t, oldRoot, "旧的.mp3")
	song(t, newRoot, "新的.mp3")
	f := newLib(t, oldRoot)

	f.app.StartLocalMusicWatch()
	waitScan(t, f.app)

	f.app.SetLocalRoots([]string{newRoot})
	st := waitScan(t, f.app)

	if len(st.Tracks) != 1 || filepath.Dir(st.Tracks[0].Path) != newRoot {
		t.Fatalf("换根之后扫到 %+v", st.Tracks)
	}
	if f.watch.stoppedCount() == 0 {
		t.Error("旧根的监听没停")
	}
	if f.watch.watching() != 2 { // 旧的一次 + 新的一次
		t.Errorf("一共装过 %d 次监听，want 2（旧的和新的）", f.watch.watching())
	}
}

// TestChangingRootsToTheSameListDoesNothing 钉住幂等。
//
// 界面每一帧都会拿设置里的目录来对一遍（见 `SetLocalRoots` 的注释），
// 对不上才换。不幂等的话就是每帧一次全表扫描。
func TestChangingRootsToTheSameListDoesNothing(t *testing.T) {
	root := t.TempDir()
	song(t, root, "歌.mp3")
	f := newLib(t, root)
	f.app.StartLocalMusicWatch()
	waitScan(t, f.app)

	before := f.wakes.get()
	for range 5 {
		f.app.SetLocalRoots([]string{root})
	}
	// 什么都没发生，所以不该有任何唤醒。
	time.Sleep(50 * time.Millisecond)
	if got := f.wakes.get(); got != before {
		t.Errorf("根没变却触发了 %d 次重扫", got-before)
	}
	if f.watch.watching() != 1 {
		t.Errorf("根没变却装了 %d 次监听", f.watch.watching())
	}
}

// TestChangingRootsNeverShowsAnEmptyLibrary 钉住换根那一瞬间不能出现
// 「已经扫完了、列表却是空的」。
//
// ⚠️ 症状是「搜了一下东西就没了，过一会儿又回来」：换根之后旧的那一批立刻
// 从列表里清掉（那些歌确实不在新目录里），而新的一遍还在跑。这中间界面
// 读到的那一份是 `tracks` 空 + `scanning` false，于是画「这里没有音频文件」
// ——它在报一件还没发生的事。
//
// ⚠️ **每一次都要换成一个不一样的根**：连着两遍 `SetLocalRoots(同一个)` 是
// 幂等的（`slicesEqual` 那一步就返回了），那样根本换不了根，这一遍扫描也就
// 不会把还在飞的那一遍作废——而「作废」正是这条要验的那一段路。
//
// 拿掉修复的方式：`land` 在作废那一支直接 `return`（不读 `again`、不再扫一遍）——
// 那一条是 `TestAScanVoidedByARootChangeStillFinishes` 在钉，两条一起兜住这一段。
func TestChangingRootsNeverShowsAnEmptyLibrary(t *testing.T) {
	roots := make([]string, 4)
	for i := range roots {
		roots[i] = t.TempDir()
		song(t, roots[i], "歌.mp3")
	}
	f := newLib(t, roots[0])
	f.app.StartLocalMusicWatch()
	if got := len(waitScan(t, f.app).Tracks); got != 1 {
		t.Fatalf("起手 %d 首", got)
	}

	// 反复换根，每换一次都在换的那一瞬间读一次状态。
	for i := range 200 {
		want := roots[i%len(roots)]
		if want == f.app.LocalMusicState().Roots[0] {
			want = roots[(i+1)%len(roots)]
		}
		f.app.SetLocalRoots([]string{want})
		st := f.app.LocalMusicState()
		if !st.Scanning && len(st.Tracks) == 0 {
			t.Fatalf("换根的那一瞬间报的是「扫完了但一个文件都没有」")
		}
	}
	// ⚠️ 而且最后**必须**落到「扫完了」：换根把还在飞的那一遍作废了，而正是
	// 换根把「还要再扫一遍」记上的。作废那一支忘了管这个的话，
	// `scanning` 永远停在 true——那一页永远转圈（`waitScan` 会超时）。
	if got := len(waitScan(t, f.app).Tracks); got != 1 {
		t.Errorf("换到最后停在 %d 首，want 1", got)
	}
}

// TestAScanVoidedByARootChangeStillFinishes 单独把「作废 + 再要一遍」那一段拎
// 出来钉。
//
// 症状：用户连着换目录（或者一边换一边按刷新），有一遍扫描是按老根算的、
// 被作废了，而「还要再扫一遍」这个要求没人接——`scanning` 永远 true，
// 那一页永远转圈，列表停在一个空状态上。
//
// 拿掉修复的方式：`land` 在作废那一支直接 `return`（不读 `again`、不再扫）。
func TestAScanVoidedByARootChangeStillFinishes(t *testing.T) {
	roots := make([]string, 6)
	for i := range roots {
		roots[i] = t.TempDir()
		song(t, roots[i], "歌.mp3")
	}
	f := newLib(t, roots[0])
	f.app.StartLocalMusicWatch()
	waitScan(t, f.app)

	// 连着换：每一个都可能在上一遍还在飞的时候到，于是反复作废。
	for i := range 5 {
		f.app.SetLocalRoots([]string{roots[i+1]})
		f.app.RescanLocalMusic()
	}
	st := waitScan(t, f.app)
	if !st.Scanning {
		if len(st.Tracks) != 1 {
			t.Errorf("停下来的时候扫到 %d 首，want 1", len(st.Tracks))
		}
		return
	}
	// 还在扫不算错，但最多再等它一会儿——它必须自己收尾。
	waitUntilScan(t, f.app, 10*time.Second)
}

// TestConcurrentRescansDoNotStackUp 钉住防重入。
//
// 文件监听报告一次改动常常连着报好几次（改一个文件：写 + 移动 + 属性），
// 没有防重入就是几遍全表扫描同时跑，每一遍都开几千个文件。
//
// 判据是「最后落地的结果里那首歌在」而不是「只跑了一遍」——这一层不承诺
// 合并次数，只承诺**结果正确且不会越跑越多**。
func TestConcurrentRescansDoNotStackUp(t *testing.T) {
	root := t.TempDir()
	song(t, root, "歌.mp3")
	f := newLib(t, root)

	f.app.StartLocalMusicWatch()
	waitScan(t, f.app)

	for range 10 {
		f.app.RescanLocalMusic()
	}
	st := waitScan(t, f.app)
	if len(st.Tracks) != 1 {
		t.Errorf("连点 10 次刷新之后扫到 %d 首，want 1", len(st.Tracks))
	}
}

// TestDeleteRemovesTheFileAndTheRow 钉住规格第 83 条。
//
// 「真的从原处搬走」+「列表里那一行也没了」。两件事都要成立：只搬文件而
// 留着行，用户会点第二次然后拿到一句「文件已经不在了」；只删行而留着文件，
// 那就是最像「实现了」的一种假实现。
//
// ⚠️ **而且文件得在废纸篓里找得回来**（票 #558）：这才是「从永久删除改成
// 可恢复」的那一件。只断「原处没了」的话，一个「偷偷 `os.Remove`」的实现
// 照样全绿——而那正是这一票明确要消灭的那一种。
func TestDeleteRemovesTheFileAndTheRow(t *testing.T) {
	root := t.TempDir()
	path := song(t, root, "要删的.mp3")
	f := newLib(t, root)

	f.app.StartLocalMusicWatch()
	st := waitScan(t, f.app)
	if len(st.Tracks) != 1 {
		t.Fatalf("扫到 %d 首", len(st.Tracks))
	}

	if err := f.app.DeleteLocalTrack(st.Tracks[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); err == nil {
		t.Fatal("文件还在盘上")
	}
	// ⚠️ **它在废纸篓里**：用户能找回来。
	entries, err := os.ReadDir(f.bin)
	if err != nil {
		t.Fatalf("读废纸篓：%v", err)
	}
	if len(entries) != 1 {
		t.Errorf("废纸篓里有 %d 项，want 1（文件得是搬过去的，不是删掉的）", len(entries))
	}
	if got := len(f.app.LocalMusicState().Tracks); got != 0 {
		t.Errorf("列表里还剩 %d 行", got)
	}
}

// TestDeleteOfAnUnknownIDTouchesNothing 钉住「界面给不了任意路径」。
//
// 这一层**没有**「按任意路径删」的入口，是有意的：路径从列表里取。
// 这条钉住它——拿掉「在列表里找」那一段、直接拿 id 当路径用的实现，
// 这里的 unknown id 就会删掉一个它算出来的路径。
func TestDeleteOfAnUnknownIDTouchesNothing(t *testing.T) {
	root := t.TempDir()
	path := song(t, root, "别人的歌.mp3")
	f := newLib(t, root)
	f.app.StartLocalMusicWatch()
	waitScan(t, f.app)

	err := f.app.DeleteLocalTrack("根本不存在的一个 id")

	// ⚠️ 断的**是哪一个错**，不是「有没有报错」。
	//
	// 本该报的是 `ErrMissing`（这一首已经不在列表里了）。如果实现改成
	// 「拿 id 当路径直接删」，一个假 id 当然不在任何根下面，报的会是
	// `ErrOutsideRoots`——**盘上的文件照样没动**（那一层还有一道），所以
	// 「文件还在不在」验不到差别，只有这一个错能。
	if !errors.Is(err, localmusic.ErrMissing) {
		t.Fatalf("错是 %v，want localmusic.ErrMissing（这一首已经不在列表里了）", err)
	}
	if got := len(f.app.LocalMusicState().Tracks); got != 1 {
		t.Errorf("失败的删除动了列表：还剩 %d 首，want 1", got)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatal("盘上的文件被动过了")
	}
}

// TestDeleteOfAFileGoneAlreadyIsNotAFailure 钉住最常见的那个场景。
//
// 用户先在访达里删了歌，回来在列表里点删除。那**不是删除失败**——把那一行
// 拿掉就对了，界面上如果弹一句「文件未删除」的红字报错，用户会以为应用
// 出问题了（而他自己刚删掉的）。
//
// ⚠️ **但它不是一个「成功」**：这一句原来断的是 `err == nil`，而那正是
// 评审打回来的那一种——`nil` 走到界面上就是「已移到系统废纸篓」，**一句话
// 承诺用户能从废纸篓里恢复**，而它压根没进过废纸篓（**说谎**）。
//
// 所以现在断的是**哪一个错**：`ErrMissing`（「文件已经不在了」）。界面据此
// 说「已经不在了」，而**不说**「已移到系统废纸篓」。
func TestDeleteOfAFileGoneAlreadyIsNotAFailure(t *testing.T) {
	root := t.TempDir()
	path := song(t, root, "先没了.mp3")
	f := newLib(t, root)
	f.app.StartLocalMusicWatch()
	st := waitScan(t, f.app)

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	err := f.app.DeleteLocalTrack(st.Tracks[0].ID)
	if !errors.Is(err, localmusic.ErrMissing) {
		t.Fatalf("错是 %v，want localmusic.ErrMissing（文件本来就不在了，"+
			"而它**没进过废纸篓**——界面不许说「已移到系统废纸篓」）", err)
	}
	if got := len(f.app.LocalMusicState().Tracks); got != 0 {
		t.Errorf("列表里还剩 %d 行", got)
	}
}

// TestATrashThatIsNotAvailableKeepsTheFileAndTheRow 钉住「那个位置没有
// 回收站」那一条。
//
// ⚠️ **三件事都要成立**：错报得出来（界面要说「文件未删除」）、**文件还在
// 原处**、**列表里那一行还在**。
//
// 拿掉修复的方式：把失败当成成功（`err == nil` 就当删掉了）——那用户会看到
// 「已移到废纸篓」，而文件好好地躺在那儿，下一次重扫它又回来了。
func TestATrashThatIsNotAvailableKeepsTheFileAndTheRow(t *testing.T) {
	root := t.TempDir()
	path := song(t, root, "还在.mp3")
	f := newLib(t, root)
	// 换成一个「那个位置没有回收站」的假零件。
	f.app.MountLocalMusic(LocalMusic{
		Roots: []string{root},
		Watch: f.watch,
		Trash: localmusictest.NotTrashable(),
	})
	f.app.StartLocalMusicWatch()
	st := waitScan(t, f.app)
	if len(st.Tracks) != 1 {
		t.Fatalf("扫到 %d 首", len(st.Tracks))
	}

	err := f.app.DeleteLocalTrack(st.Tracks[0].ID)

	if !errors.Is(err, localmusic.ErrNotTrashable) {
		t.Fatalf("错是 %v，want localmusic.ErrNotTrashable", err)
	}
	if _, statErr := os.Lstat(path); statErr != nil {
		t.Fatal("回收站不可用却把文件弄没了——这一层绝不能回退到永久删除")
	}
	if got := len(f.app.LocalMusicState().Tracks); got != 1 {
		t.Errorf("失败的删除动了列表：还剩 %d 首，want 1", got)
	}
}

// TestARootThatCannotBeReadIsReportedNotFatal 钉住一个坏目录不拖垮整次扫描。
func TestARootThatCannotBeReadIsReportedNotFatal(t *testing.T) {
	good := t.TempDir()
	song(t, good, "歌.mp3")
	f := newLib(t, filepath.Join(good, "根本没这个"), good)

	f.app.StartLocalMusicWatch()
	st := waitScan(t, f.app)

	if st.Err == "" {
		t.Error("读不到的目录没有报到界面上")
	}
	if len(st.Tracks) != 1 {
		t.Errorf("好目录里的歌被一起丢掉了：%+v", st.Tracks)
	}
}

// TestNoWatcherMeansThePageStillScans 钉住 DirWatch 是 nil 的那条路。
//
// 界面每一次进这一页都会扫一遍，所以「不盯」只是少一个自动更新，
// 不该是「这一页永远是空的」。
func TestNoWatcherMeansThePageStillScans(t *testing.T) {
	root := t.TempDir()
	song(t, root, "歌.mp3")
	a := New()
	t.Cleanup(a.Close)
	a.MountLocalMusic(LocalMusic{Roots: []string{root}})

	a.RescanLocalMusic()
	st := waitScan(t, a)

	if len(st.Tracks) != 1 {
		t.Fatalf("不盯文件的时候扫到 %d 首", len(st.Tracks))
	}
	if st.Watching != 0 {
		t.Errorf("没装监听却报 %d 个", st.Watching)
	}
}

// TestClosingTheAppStopsTheWatches 钉住不漏 goroutine。
func TestClosingTheAppStopsTheWatches(t *testing.T) {
	root := t.TempDir()
	song(t, root, "歌.mp3")
	a := New()
	w := newStubWatch()
	a.MountLocalMusic(LocalMusic{Roots: []string{root}, Watch: w})

	a.StartLocalMusicWatch()
	waitScan(t, a)
	a.Close()

	if got := w.stoppedCount(); got != 1 {
		t.Errorf("退出之后停了 %d 个监听，want 1", got)
	}
}

// TestUsingAnUnmountedLibraryIsNotACrash 钉住「还没挂上」那条路。
//
// 和下载不一样：本地音乐不读盘、不恢复什么，没挂上不该拦住开窗口
// （见 MountLocalMusic 的注释）。
func TestUsingAnUnmountedLibraryIsNotACrash(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	if a.LocalMusicMounted() {
		t.Error("没挂却说挂上了")
	}
	if got := a.LocalMusicState(); len(got.Tracks) != 0 {
		t.Errorf("没挂却读出了 %d 首", len(got.Tracks))
	}
	// 这两个都该是空操作，不该 panic。
	a.RescanLocalMusic()
	a.StartLocalMusicWatch()
	if err := a.DeleteLocalTrack("x"); !errors.Is(err, errLocalMusicMissing) {
		t.Errorf("错是 %v，want errLocalMusicMissing", err)
	}
}

// waitUntilScan 等扫描收尾，**上限 timeout**——`waitScan` 的 10 秒在这里
// 太短：这条等的是「连着换了五次根之后最后一遍落地」。
func waitUntilScan(t *testing.T, a *App, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for a.LocalMusicState().Scanning {
		if time.Now().After(deadline) {
			t.Fatalf("等了 %v 扫描还没收尾——永远转圈了", timeout)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// waitFor 等 cond 成立。上限 2 秒：这一组等的都是「后台一定会做完」的事，
// 两秒不成就是真出问题，不该继续等。
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	waitForWithin(t, 2*time.Second, cond, msg)
}

// waitForWithin 是 `waitFor` 带自定义上限的那一版。
//
// ⚠️ **什么时候该用它**：等的这件事**不止是「后台算完」**，还要过一遍外部
// 系统——开一个音频设备、等真实的采样时钟走起来、等一次网络往返。那种事在
// 慢机器上的上界和「后台算完」不是一回事，用 2 秒会红成「机器慢」，而不是
// 红成「坏了」（2026-10-30 的 `TestTheSpectrumPreferenceControlsTheCurrentPlayer`
// 就是这么飘的）。
//
// ⚠️ **上限放宽不等于放水**：坏掉的时候照样会到点失败，只是把「多等一会儿」
// 和「真的坏了」分开。
func waitForWithin(t *testing.T, limit time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s（等了 %v）", msg, limit)
		}
		time.Sleep(time.Millisecond)
	}
}
