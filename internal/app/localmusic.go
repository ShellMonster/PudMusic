// 本地音乐库：把设置里的目录变成一份列表，变了就重扫，能真的删文件。
//
// # 这一层只做接线
//
// 扫描、元信息、删除策略全在 `service/localmusic`。这一层做四件它做不了的
// 事：
//
//	1. **根目录来自设置**（`Settings.LocalMusicDirs`），不是某一层的常量。
//	2. **扫描跑在后台**：那是几千次 stat 加若干次打开文件，放在界面线程上
//	   就是界面卡住。
//	3. **结果到了要叫醒界面**（见 `SetWake`）。即时模式不会自己重画。
//	4. **换目录、删文件这两件事各自只有一个入口**，界面层不直接碰
//	   `service/localmusic.Delete`。
//
// # 删文件这一条为什么收在这里
//
// 因为它是**唯一**一个「界面上按一下、用户的东西就动了」的动作，而界面上
// 有两个地方能按（行里的菜单、表格的选中行）。收在一处之后，
// `Delete` 的那三条规矩（落在某个根下、是普通文件、符号链接移链接本身）
// 只需要在一个地方被守住——`service/localmusic` 的那三条，见 delete.go。
//
// ⚠️ **删是「移到系统废纸篓」，不是永久删除**（票 #558）：所以这一层要
// 拿到「回收站」那一份零件（`LocalMusic.Trash`），而它**由界面层装**
// （理由同 `DirWatch`：框架那侧的 API 必须在事件循环起来之后才能调）。
//
// 确认弹窗在界面上（用户按「删除」和「真的删」之间隔着一句话），
// 但**删不删**这件事在这里判：界面层传来的 id 必须在当前列表里，
// 路径从列表里取，界面层没有机会指定一个别的路径。

package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"slices"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/service/localmusic"
)

// errLocalMusicMissing 是「本地音乐还没挂上」。见 `LocalMusicMounted`。
//
// ⚠️ **这句是给用户看的**，不许说「还没接上」（票 #242，UX 报告 §3-F 的 F2）。
var errLocalMusicMissing = errors.New("本地音乐暂时用不了")

// DirWatch 盯一个目录，目录里的东西变了就调 onChange。
//
// 为什么是接口而不是直接调 `mygo.WatchFiles`：`internal/app` 不 import
// MyGo（这一层不认识界面），而 `WatchFiles` **必须在应用的事件循环起来之
// 后**调（`needsApp`，见 `mygo/app.go:128`）。于是实现放在界面层
// （`internal/ui/localmusic_watch.go`），这里只认「能盯一个目录」这件事。
//
// 装 nil 是允许的，意思就是「不盯」：界面每一次进这一页都会扫一遍，
// 只是文件在别处被改了不会自己冒出来。
type DirWatch interface {
	// Watch 盯 dir，变了调 onChange，返回停掉它的动作。
	//
	// ⚠️ onChange 在**它自己的 goroutine** 上调，而且可能连着来好几次：	// 实现必须自己防抖。这一层的做法是扫描带代次 + 在飞的那次没回来就
	// 不再起新的（见 `localLibrary.scan`）。
	Watch(ctx context.Context, dir string, onChange func()) (stop func(), err error)
}

// Trash 是「把一个文件移到系统废纸篓」。
//
// ⚠️ **为什么是接口而不是直接调框架**：这一层（`internal/app`）不 import
// MyGo（它不认识界面），而 `mygo.Shell.TrashItem` **必须在应用的事件循环
// 起来之后调**（`needsApp`，见 `mygo/app.go:128`）。于是实现放在界面层
// （`internal/ui/localmusic_trash.go`），这里只认「能把一个文件移进废纸篓」
// 这件事。
//
// ⚠️ **失败时它必须把文件留在原处**，而且**不许**回退到永久删除——那是
// 这一票（#558）拍板的语义，悄悄换回去的话界面上一个字都不会变。
type Trash = localmusic.Trash

// LocalMusic 是本地音乐这一摊的依赖。
type LocalMusic struct {
	// Roots 是第一次挂上来时的目录。之后以**设置里那一份**为准
	// （`LocalMusicState` 每次都会对着设置核一遍，见 `App.LocalRoots`）。
	//
	// 这里给的是「还没挂上设置」的兜底——挂上了就用设置里的。
	Roots []string

	// Watch 盯目录。nil = 不盯。
	Watch DirWatch

	// Trash 是「移进系统废纸篓」那一份零件。**nil = 没有回收站**，
	// 而那时的删除会**报错**（`localmusic.ErrNoTrash`）而不是永久删除。
	//
	// ⚠️ 生产路径上它一定装上了（`ui.MountLocalMusic`），而测试里多数
	// 故意不装——那些用例根本不删文件。
	Trash Trash
}

// localLibrary 是本地音乐库此刻的全部状态。
type localLibrary struct {
	mu sync.Mutex

	roots  []string
	tracks []localmusic.Track
	cache  map[string]localmusic.Meta

	scanning bool
	scanErr  error
	// gen 是扫描代次。换根之后旧的扫描结果作废——用户连点「添加目录」时，
	// 上一遍扫旧目录的结果回来会盖掉新目录的那一份。
	gen uint64
	// flying 是「已经有一遍在飞」。防重入：文件监听连着报三四次变化时，
	// 没有它就是三遍全表扫描同时跑，每一遍都开几千个文件。
	flying bool
	// again 是「飞的那一遍回来之后还要再扫一遍」。用户在这期间又改了目录，
	// 这一遍的结果是按老根算的，得再扫一次。
	again bool

	// wakes 是**逐窗口**的那一份（见 `App.AddLocalWake`）：一个窗口一条，
	// 扫完了挨个叫醒。只叫醒主窗口的话，迷你播放器那个窗口不会重画。
	wakes []func()

	watch DirWatch
	// trash 是「移进废纸篓」那一份零件（见 `LocalMusic.Trash`）。
	trash Trash
	// stops 是每个根一个停掉动作，换根时挨个调。
	stops  []func()
	cancel context.CancelFunc
	closed bool
}

// LocalMusicState 是一帧里界面要读的本地音乐库状态。
type LocalMusicState struct {
	// Tracks 是扫出来的条目，**按（根目录，路径）排好序**。
	Tracks []localmusic.Track
	// Roots 是当前在扫的根。
	Roots []string
	// Scanning 是「正在扫」。第一遍扫完之前是 true——那一页上画的是
	// 「正在扫描…」，不是「这里没有歌」。
	Scanning bool
	// Err 是有目录读不到时的错，空串是没有。
	Err string
	// Watching 是文件监听起来了几个根。0 就是没盯（DirWatch 是 nil，
	// 或者那些根被删了）。界面上据此决定要不要说「手动刷新」。
	Watching int
}

// MountLocalMusic 挂上本地音乐库。**可以在开窗口之前调，也可以不调**。
//
// 不像下载那样非挂不可：没挂的话这一页画的是一句「还没接上」，而不是崩。
// （下载要读盘恢复队列，本地音乐什么都不用读。）
func (a *App) MountLocalMusic(l LocalMusic) {
	a.mu.Lock()
	lib := &localLibrary{
		roots: cleanRoots(l.Roots),
		cache: map[string]localmusic.Meta{},
		watch: l.Watch,
		trash: l.Trash,
		// ⚠️ 这里**不**开始扫描：Scan 是阻塞 IO，而 MountLocalMusic 是
		// 在开窗口之前调用的。第一遍扫描由界面的第一帧叫起来
		// （`RescanLocalMusic`），或者由文件监听叫起来。
	}
	a.localMusic = lib
	wake := a.wake
	a.mu.Unlock()

	if wake != nil {
		_ = lib.addWake(wake)
	}
}

// LocalMusicMounted 报告本地音乐挂上没有。
func (a *App) LocalMusicMounted() bool { return a.localMusicSnapshot() != nil }

// LocalMusicState 返回此刻的库状态。每一帧界面都读它。
//
// ⚠️ 返回的是**拷贝**：里面的切片被复制过，界面拿着它爱怎么改怎么改，
// 下一帧再读一遍就是新的。
func (a *App) LocalMusicState() LocalMusicState {
	lib := a.localMusicSnapshot()
	if lib == nil {
		return LocalMusicState{}
	}
	lib.mu.Lock()
	defer lib.mu.Unlock()
	return LocalMusicState{
		Tracks:   append([]localmusic.Track(nil), lib.tracks...),
		Roots:    append([]string(nil), lib.roots...),
		Scanning: lib.scanning,
		Watching: len(lib.stops),
		Err:      errText(lib.scanErr),
	}
}

// LocalTrack 按身份找一项。删除时用它拿路径。
func (a *App) LocalTrack(id string) (localmusic.Track, bool) {
	lib := a.localMusicSnapshot()
	if lib == nil {
		return localmusic.Track{}, false
	}
	lib.mu.Lock()
	defer lib.mu.Unlock()
	for _, t := range lib.tracks {
		if t.ID == id {
			return t, true
		}
	}
	return localmusic.Track{}, false
}

// DeleteLocalTrack 把一首移到系统废纸篓，然后把它从列表里拿掉。
//
// ⚠️ **路径是从列表里取的，调用方给不了**。传进来的 id 必须在当前列表里，
// 否则一个 id 都对不上——这一层没有「按任意路径删」的入口，那是有意的。
//
// 几个错分得开，界面据此说不同的话：
//
//   - `ErrOutsideRoots` / `ErrNotRegular` 是「不能删」（多半是这一行已经
//     过期了）；
//   - `ErrMissing` 是「它已经不在了」——**它没进过废纸篓**；
//   - `ErrTrashFailed` / `ErrNotTrashable` / `ErrDenied` 是「没能移进
//     废纸篓」（**文件还在原处**）。
//
// ⚠️ **三种结果不许混成两种**：只有「真的搬进废纸篓了」那一种能让界面说
// 「已移到系统废纸篓」。`ErrMissing` 说的是「它本来就不在了」——那一行照样
// 从列表里拿掉（那是用户想要的结果），但界面**不许**说「已移到系统废纸篓」：
// 那句话**承诺用户能从废纸篓里恢复**，而它压根没进过废纸篓（**说谎**）。
//
// ⚠️ **所以这一层不把 `ErrMissing` 改成 nil**：评审打回来的那一种正是
// 「把「已经不在了」当成「成功了」」——回收站真失败的时候，界面说「已移到
// 系统废纸篓」、**列表那一行也消失了**，而文件好好地躺在原处。
func (a *App) DeleteLocalTrack(id string) error {
	lib := a.localMusicSnapshot()
	if lib == nil {
		return errLocalMusicMissing
	}

	lib.mu.Lock()
	var track localmusic.Track
	found := false
	for _, t := range lib.tracks {
		if t.ID == id {
			track, found = t, true
			break
		}
	}
	roots := append([]string(nil), lib.roots...)
	trash := lib.trash
	lib.mu.Unlock()

	if !found {
		return fmt.Errorf("%w：这一首已经不在列表里了", localmusic.ErrMissing)
	}

	// ⚠️ 传的是**整个 track**：删除那一侧要拿 `track.Canonical` 做一道身份
	// 检查（`localmusic.Delete` 的规矩 4），只给路径它无从判断「这个位置上
	// 的东西还是不是扫描时那一个」。
	//
	// ⚠️ **第三份参数是回收站那一份零件**：它 nil 的时候删除会**报错**
	// （`ErrNoTrash`），而**不会**退回去永久删除——那正是这一票要的语义
	// （票 #558）。
	err := localmusic.Delete(track, roots, trash)

	lib.mu.Lock()
	if err == nil || errors.Is(err, localmusic.ErrMissing) {
		lib.drop(track.ID)
	}
	if errors.Is(err, localmusic.ErrMissing) {
		// 用户先在访达里删了。那一行从列表里拿掉就是用户要的结果，而它
		// **没进过废纸篓**——所以这个错**继续往上抛**，界面据此说
		// 「文件已经不在了」而不是「已移到系统废纸篓」。
		//
		// ⚠️ **不许在这里把它改成 nil**：那正是评审打回来的那一种
		// （界面说「已移到系统废纸篓」，而文件还在原处）。
		//
		// 日志留着，因为「文件从列表里消失」这件事本身值得知道。
		log.Printf("biu: 本地音乐 %s 已经不在盘上了，把它从列表里拿掉", track.Path)
	}
	wakes := slices.Clone(lib.wakes)
	lib.mu.Unlock()

	for _, wake := range wakes {
		if wake != nil {
			wake()
		}
	}
	return err
}

// RescanLocalMusic 重新扫一遍全部根。
//
// 界面进这一页时调一次（`LocalRoots` 发现根变了也会调），用户点「刷新」
// 再调一次。它是**幂等**的：在飞的那一遍没回来之前再调只会记一个「回来之后
// 再扫一遍」，不会起第二遍全表扫描。
func (a *App) RescanLocalMusic() {
	lib := a.localMusicSnapshot()
	if lib == nil {
		return
	}
	lib.scan()
}

// SetLocalRoots 换掉在扫的根，并重扫。
//
// 界面每次画这一页都会拿设置里的目录来对一遍，对不上就换——这样用户改了
// 设置，这一页下一帧就是新的，不用谁去订阅设置的变化。
func (a *App) SetLocalRoots(roots []string) {
	lib := a.localMusicSnapshot()
	if lib == nil {
		return
	}
	lib.setRoots(cleanRoots(roots))
}

// AddLocalWake **再**登记一个「库变了，要一帧」的回调。
//
// ⚠️ 界面层**每个窗口建外壳时都要登记一次**（`Shell.New`）。原来这里是
// `SetLocalWake`（只存一个），而迷你播放器是第二个窗口——它建外壳时会把主窗口
// 那一条顶掉，于是**后台扫完本地库之后主窗口再也不刷新**：歌单那儿停在「扫
// 描中」，而界面上看不出任何异样。
//
// 传进来的回调自己也是「挨个叫醒」的（`internal/ui/localmusic.go` 的
// `localPage.invalidate` 记着每一个画过这一页的窗口），这一层照样挨个调，
// 两边是同一件事的两半。
func (a *App) AddLocalWake(fn func()) (remove func()) {
	lib := a.localMusicSnapshot()
	if lib == nil {
		// 本地音乐还没挂上（`MountLocalMusic` 排在 `New` 之前，正常不会这样）。
		// 给一个空操作而不是 nil：调用方无条件地摘一次。
		return func() {}
	}
	return lib.addWake(fn)
}

// StartLocalMusicWatch 装上文件监听。**必须在事件循环起来之后调。**
//
// 由界面层在 `mygo.App.WhenReady` 里调（见 `ui.Run`）：`mygo.WatchFiles`
// 在应用还没 ready 的时候调会 panic（框架的 `needsApp`）。
//
// 幂等：调两次只是重装一遍监听，不会多开一摊 goroutine。
func (a *App) StartLocalMusicWatch() {
	if lib := a.localMusicSnapshot(); lib != nil {
		lib.startWatching()
	}
}

// localMusicSnapshot 拿一份库。没挂上是 nil。
func (a *App) localMusicSnapshot() *localLibrary {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.localMusic
}

// ─────────────────────────────────────────────────────────────────────────────
// localLibrary
// ─────────────────────────────────────────────────────────────────────────────

// addWake 登记一个「库变了，要一帧」的回调，返回摘掉它的那一个。
//
// ⚠️ 是**追加**不是替换：一个进程有不止一个窗口（迷你播放器），每个窗口建外壳
// 时都来登记一次。替换的话第二个窗口建好之后，第一个窗口就再也听不到「库变了」
// ——本地音乐扫完它在主窗口上不刷新，而界面上看不出任何异样。
//
// 🔴 **能摘是必须的**，不是「可选的优化」：迷你窗口是**进/退来回新建外壳**的
// （界面层的 `newMiniShell`），只登记不摘的话这个列表随开关次数单调增长——每扫一遍
// 库就多走一趟空转，而多出来的那几条属于**已经收掉的**外壳。
//
// 函数值不能比（只能和 nil 比），所以「摘掉自己那一条」靠**下标**：登记时就记下了
// 它在哪一位，而这一段里只有这一处在改 `wakes`。
func (l *localLibrary) addWake(fn func()) (remove func()) {
	l.mu.Lock()
	l.wakes = append(l.wakes, fn)
	i := len(l.wakes) - 1
	l.mu.Unlock()

	// 可以调任意次：摘掉之后再调是空操作（关窗流程走了两遍不是什么怪事）。
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if i < len(l.wakes) {
			l.wakes = append(l.wakes[:i], l.wakes[i+1:]...)
		}
	}
}

// scan 起一遍扫描，在飞的那一遍没回来之前不重复起。
//
// ⚠️ `arm` 要在锁里调（它改 `flying` / `scanning` / `gen`），而 `launch`
// **不能在锁里**调：它起一个 goroutine，那条 goroutine 落地时要拿同一把锁。
// 所以锁只罩住 arm 这一句。
func (l *localLibrary) scan() {
	l.mu.Lock()
	roots, prev, gen, ok := l.arm()
	l.mu.Unlock()
	if ok {
		l.launch(roots, prev, gen)
	}
}

// arm 决定「现在起不起一遍」，起的话把状态一次摆好。
//
// ⚠️ **必须持锁调**（它改 `flying` / `scanning` / `again` / `gen`）。两个调用点
// 都守着这条：`scan` 自己在外面加了锁，`setRoots` 因为已经拿着锁所以直接调。
//
// 拆成 arm 和 run 两个函数只有一个理由：**中间不能有窗口**。
// 「摆好状态」和「决定起不起」必须在同一把锁里做完，否则会出现这样一刻：
// 根换了、`tracks` 清空了、`scanning` 还是 false、新的扫描还没起——界面读到
// 这一份就画「这里没有音频文件」，下一帧那几首歌又冒出来（症状是「搜了一下
// 东西就没了又回来」，`TestChangingRootsNeverShowsAnEmptyLibrary` 钉的就是它）。
//
// 抢不到（还在飞）时记一个 `again`，**不**把 `scanning` 落回去：在飞的那一遍
// 起手时就已经把它置上了。
func (l *localLibrary) arm() (roots []string, prev map[string]localmusic.Meta, gen uint64, ok bool) {
	if l.closed {
		return nil, nil, 0, false
	}
	if l.flying {
		l.again = true
		return nil, nil, 0, false
	}
	roots, prev = append([]string(nil), l.roots...), l.cache
	l.flying, l.scanning, l.again = true, true, false
	l.gen++
	return roots, prev, l.gen, true
}

// launch 把这一遍扫描放到后台 goroutine 上跑。
//
// ⚠️ **必须有这一步，而且必须在 `arm` 之后立刻做**。
//
// `arm` 只在锁里摆状态，真正的扫描是阻塞 IO（几千次 stat，每个没缓存上的文件
// 还要开一次读一次标签）。直接在调用者的 goroutine 上跑它的话，「调用者」
// 就是界面线程——`setRoots` 是每一帧从 `begin` 调的（`internal/ui/localmusic.go`），
// 于是**每换一次目录，界面就卡住一整遍扫描的时间**。而目录筛选、手动刷新、
// 文件监听这三个入口也都是同步调 `scan` 的，一个都跑不掉。
//
// `TestTheFirstScanIsNotOnTheCallingThread` 钉的就是这一句。
func (l *localLibrary) launch(roots []string, prev map[string]localmusic.Meta, gen uint64) {
	go func() {
		// 🔴 **这一层 recover 不是可有可无的保险。**
		//
		// 这一遍跑在一个没人管的 goroutine 上：界面层的 `Guard`、行级的
		// recover、`widgets/list` 那个 `fetchOne` 的 recover，**一个都接不住
		// 它**——Go 里没有人在跑那条 goroutine。解析器在畸形文件上切错一个
		// 切片（评审就逮到过一个四字节的 `RIFF` 让它越界），panic 会一路
		// 上去把**整个应用**带走，退出码 2，而界面上什么都看不见。
		//
		// 音乐目录里出现半截文件是常事（拷到一半、被中断的下载留下的
		// `.part` 改名来的、正在被写的一个）。所以这里把崩了变成「这一遍
		// 什么都没有」+ 一条日志，下一遍扫描（用户按刷新、或者文件又变了）
		// 自然会再试。
		var res localmusic.Result
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("biu: 扫本地音乐时崩了（多半是目录里有畸形文件）：%v\n%s",
						rec, debug.Stack())
				}
			}()
			res = localmusic.Scan(roots, prev)
		}()
		l.land(res, gen)
	}()
}

// land 把扫完的结果交回状态里。
//
// ⚠️ 这一段**不许**碰界面状态，它从后台 goroutine 上来：结果一律先落到锁
// 里面，再通过 `wake` 去要一帧（只投递不唤醒 = 画面停在旧帧，实测 0 帧刷新）。
func (l *localLibrary) land(res localmusic.Result, gen uint64) {
	l.mu.Lock()
	l.flying = false
	// 作废的两种：应用关了，或者根在扫的时候换过（代次对不上，那一遍是按老根
	// 算的）。作废的那一遍结果**不能**落地——用户换了目录，列表里不该还是旧的。
	void := l.closed || gen != l.gen
	if !void {
		l.tracks, l.cache, l.scanErr = res.Tracks, res.Cache, res.Err
	}
	again := l.again
	// ⚠️ `scanning` 只在「没人再要一遍」的时候才落回去。落早了就是界面读到
	// 一次「扫完了」而列表还是空的。
	if l.closed || (!void && !again) {
		l.scanning = false
	}
	wakes := slices.Clone(l.wakes)
	l.mu.Unlock()

	for _, wake := range wakes {
		if wake != nil {
			wake()
		}
	}
	// ⚠️ **作废的那一支也得看 `again`**：换根会把还在飞的那一遍作废，而正是
	// 换根把 `again` 置上的。在作废那一支直接 return 的话，`again` 就再也没人
	// 管，`scanning` 永远停在 true——那一页永远转圈。
	if again {
		l.scan()
	}
}

// setRoots 换根。换了就重扫、并且把文件监听挪到新的根上。
func (l *localLibrary) setRoots(roots []string) {
	l.mu.Lock()
	if l.closed || slicesEqual(roots, l.roots) {
		l.mu.Unlock()
		return
	}
	l.roots = roots
	l.tracks = nil
	l.cache = map[string]localmusic.Meta{}
	// 代次 +1：还在飞的那一遍是按老根算的，让它自己作废。
	l.gen++
	// ⚠️ 在**同一把锁里**把新一轮 arm 好，不给「已清空但还没开扫」那个窗口。
	scanRoots, prev, gen, ok := l.arm()
	watch, started := l.watch, l.cancel != nil
	l.mu.Unlock()

	l.stopWatches()
	if watch != nil && started {
		l.startWatching()
	}
	if ok {
		l.launch(scanRoots, prev, gen)
	}
}

// startWatching 给每个根装一个监听。已经在盯就什么都不做。
func (l *localLibrary) startWatching() {
	l.mu.Lock()
	if l.closed || l.watch == nil || l.cancel != nil {
		l.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	roots := append([]string(nil), l.roots...)
	watch := l.watch
	l.mu.Unlock()

	var stops []func()
	for _, dir := range roots {
		stop, err := watch.Watch(ctx, dir, l.scan)
		if err != nil {
			// 一个目录盯不上（外接盘没插、被删了）不该让其余的不盯。
			log.Printf("biu: 盯不住本地音乐目录 %s，只能手动刷新：%v", dir, err)
			continue
		}
		stops = append(stops, stop)
	}

	l.mu.Lock()
	// ⚠️ 装监听和停监听之间可能换过根（`setRoots` 会先 cancel 再来一次）。
	// ctx 已经 Done 了就是把这一轮作废，关掉刚建的、别挂上去。
	if l.cancel == nil || ctx.Err() != nil {
		l.mu.Unlock()
		cancel()
		for _, stop := range stops {
			stop()
		}
		return
	}
	l.stops = append(l.stops, stops...)
	l.mu.Unlock()

	// 有目录被盯上了才扫第一遍：不然用户第一次打开这一页要等一次全表扫描，
	// 而那个目录可能压根不存在。
	if len(stops) > 0 {
		l.scan()
	}
}

// stopWatches 关掉所有监听。
func (l *localLibrary) stopWatches() {
	l.mu.Lock()
	cancel, stops := l.cancel, l.stops
	l.cancel, l.stops = nil, nil
	l.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	for _, stop := range stops {
		stop()
	}
}

// drop 把一项从列表里拿掉。必须在锁里调。
func (l *localLibrary) drop(id string) {
	out := make([]localmusic.Track, 0, len(l.tracks))
	for _, t := range l.tracks {
		if t.ID != id {
			out = append(out, t)
		}
	}
	l.tracks = out
}

// close 关掉监听。应用退出时调，可以重复调。
//
// ⚠️ 已经在飞的那一遍扫描**不会**被取消（context 只管监听）：它在几秒之内会
// 自己结束然后发现 `closed`，结果扔掉。退出路径上没有用户在看，不值得为它
// 加一套「扫描到一半取消」的机制。
func (l *localLibrary) close() {
	l.mu.Lock()
	l.closed = true
	l.wakes = nil
	l.mu.Unlock()

	l.stopWatches()
}

// ─────────────────────────────────────────────────────────────────────────────
// 小工具
// ─────────────────────────────────────────────────────────────────────────────

// cleanRoots 把根目录收拾干净：去掉空的、去掉重复的。
func cleanRoots(roots []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// errText 把一个错变成一句话，空错给空串。
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
