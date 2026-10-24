// 下载队列：谁在队里、现在轮到谁、暂停和继续、进度怎么报出去。
//
// # 一次只下一首
//
// 分块并发是 3（`fetch.go` 的 `concurrency`），而**任务**之间是串行的。
// B 站上游是「任务并发 3 + 每任务分块并发 5」（`download-queue.ts:23` 和
// `download-core.ts:434`），最坏 15 个在飞的请求。这里取串行有两个理由：
// 一个歌下到一半换下一首，串行下用户等的是这一首的下一次重试而不是
// 「第三首先下完」；而且 3×3 条连接对一个音乐应用来说是多的。
//
// # 进度不在这层节流，在这层**转发**
//
// 真正的节流在 `noteProgress`（500 毫秒）。字节数是从 `fetch.go` 的读
// 回调里报上来的——每收到一个 TCP 包一次，一个任务每秒几千次，全走一遍
// 队列的锁是没有意义的。
//
// # 状态写在锁里，网络在锁外
//
// 所有对 `Task` 字段的改动都在 `m.mu` 下面，只有取流和下载在锁外。
// 一次 `runOne` 全程都不拿锁——不然界面在下载期间一帧都画不出来。

package download

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// ErrNotFound 是队列里没有这一项。
var ErrNotFound = errors.New("download: 队列里没有这一项")

// ErrPaused 是「这一项是被用户停掉的」，不是失败。
var ErrPaused = errors.New("download: 已暂停")

// Resolver 每次开始或续传时问一句：「这一首现在从哪儿下？」
//
// 是**函数**不是接口：B 站客户端在 app 层，download 不该认识它（依赖
// 单向，见 `spec.md` 的架构分层）。地址会过期（实测两小时），所以**每次**
// 开下都要重新问，不能拿着上次的地址续。
//
// 传进去的是任务自己的身份：只给一个 ctx 的话，Manager 就得自己知道
// 「现在下的是哪一首」，而那正是 app 层才有的知识（`Item` 到 bvid/cid 的
// 对应关系）。`want` 里的 `Quality` 对新任务是 0（还不知道 B 站会给哪
// 一档），调用方按自己的偏好去问就行，返回的 `Source.Quality` 是 B 站
// 真正给的那一档。
type Resolver func(ctx context.Context, want Identity) (*Source, error)

// Options 是 Manager 的装配。
type Options struct {
	// Resolve 是问地址的那一句。必填。
	Resolve Resolver
	// Client 是拉字节用的 http.Client。
	//
	// ⚠️ 它**必须**是 `app.CredentialTransport.Client(nil)`：带凭据边界但
	// **不挂 jar**。B 站 CDN 跟 B 站 API 不是同一个凭据域，把 SESSDATA
	// 发给一台存音频文件的机器没有任何理由。Referer 照给，不给直接 403
	// （真机验过）。
	Client *http.Client
	// Now 是取时刻（Unix 毫秒）。nil 用真时钟。给它是给测试用的。
	Now func() int64
	// OnDone 是「一项有结果了」的回调：**下完了**或者**下坏了**。
	//
	// ⚠️ **暂停 / 取消不报**：那不是「下完了」，报出去是撒谎（用户还要接着下）。
	//
	// ⚠️ **它在后台那条线上被调**（不是界面线程），所以它不许碰界面状态。
	// 上层拿它做的是「说一句话」（见 `internal/app/announce.go`）。
	OnDone func(Snapshot)
}

// Manager 是下载队列。
type Manager struct {
	mu      sync.Mutex
	tasks   map[string]*Task
	order   []string // 入队顺序。界面按它排，记录页不按时间排。
	cancels map[string]context.CancelFunc
	lastAt  map[string]time.Time // 进度上次报出去的时刻（节流用）

	client  *http.Client
	resolve Resolver
	now     func() int64

	// wake 是「状态变了，要一帧」的回调，由界面层装。
	wake func()

	// onDone 是「一项有结果了」的回调（见 `Options.OnDone`）。
	onDone func(Snapshot)

	ctx    context.Context
	quit   context.CancelFunc
	wakeup chan struct{}
	wg     sync.WaitGroup

	// coverWG 数着「后台存封面」那些线（`startCoverSave`）。
	//
	// 🔴 **它和 `wg` 是两把锁，不是一把**：存封面**不在**队列那条串行的路上
	// （那正是它要摆脱的东西——见 cover.go 的文件头），所以 `loop` 不等它们，
	// 队列才不会被一张图卡住。
	//
	// 而 `Close` 要等它们：`m.quit()` 会取消 `m.ctx`，请求立刻断，所以这一步
	// 不会把退出拖慢，而它保证的是「Close 返回之后没有一条线还在往用户的下载
	// 目录里写东西」。
	//
	// ⚠️ `Add` 和这里的 `Wait` 不会撞上（`WaitGroup` 复用踩过这个坑）：只有
	// `loop` 会 `Add`，而 `Close` 是**先等 `loop` 退出**（`m.wg.Wait()`）
	// **之后**才等这一组。
	coverWG sync.WaitGroup

	closed bool
}

// New 建一个队列。**必须在开窗口之前调**（它会读盘）。
//
// path 是队列文件；非空时会 Restore 一次。恢复出来的任务一律是**暂停**
// （见 store.go 的文件头）。
func New(opts Options, path string) (*Manager, error) {
	if opts.Resolve == nil {
		return nil, errors.New("download: 少一个 Resolve —— 没有它就问不出地址")
	}
	if opts.Client == nil {
		return nil, errors.New("download: 少一个 Client")
	}
	now := opts.Now
	if now == nil {
		now = func() int64 { return time.Now().UnixMilli() }
	}

	ctx, quit := context.WithCancel(context.Background())
	m := &Manager{
		tasks:   map[string]*Task{},
		cancels: map[string]context.CancelFunc{},
		lastAt:  map[string]time.Time{},
		client:  opts.Client,
		resolve: opts.Resolve,
		now:     now,
		onDone:  opts.OnDone,
		ctx:     ctx,
		quit:    quit,
		wakeup:  make(chan struct{}, 1),
	}
	if path != "" {
		if err := m.Restore(path); err != nil {
			return nil, err
		}
	}
	m.wg.Add(1)
	go m.loop()
	return m, nil
}

// SetWake 装「状态变了，要一帧」。由界面层装一次。
func (m *Manager) SetWake(fn func()) {
	m.mu.Lock()
	m.wake = fn
	m.mu.Unlock()
}

// AddRequest 是入队要给的东西。
type AddRequest struct {
	Bvid  string
	Cid   string
	Title string
	Cover string
	// Dir 是落盘目录。空 = 用 Manager 建的目录。
	Dir string
	// Ext 是扩展名（带点）。空 = 先按 `.m4a` 记一个**临时**名，真正开始
	// 下之前按取流地址上的后缀重算（那一刻还没有地址，见 `Task.Ext`）。
	Ext string
	// Video 报「这一项是视频下载」。
	//
	// 它必须是入队时就说的，**不能等取流回来才认**：
	//	取流是一次几百毫秒的网络往返，而入队是在界面线程上调的
	//	落盘名（`.mp4` 还是 `.m4a`）和「要不要多留一段半成品的位置」
	//	都得在**入队那一刻**定下来
	//
	// VideoQ 是点名要的那一档视频画质（B 站的 `qn` 号）。0 = 不点名。
	// 和 `Video` 一样是入队时就定的——理由见 `Identity.VideoQ`。
	VideoQ int
	// 一旦定了就**不能改**：`Dest` 变了等于把已经下了一半的字节挪到另一个
	// 文件名下面，而续传记的是分块下标、不是「文件里的偏移」——换个文件，
	// 那些下标就全指错了位置（见 `Task.Dest`）。
	Video bool
}

// Enqueue 把一首加进队列，返回它的快照。
//
// **同一首同一件事已经在队列里就返回那一个**，不会新加一个。判重用的是
// `Task.ID`（= `<bvid>|<cid>`，视频再多一个 `|video`，见 `TaskID`）——同一首歌点了两次
// 下载，用户要的是「就一个」，而不是两个任务抢同一个文件；而**音频和视频是两个
// 文件**，不能认成同一件。已经在队列里的那一项：
//
//	下完了   什么都不做，返回它
//	排着/在下/暂停   什么都不做，返回它
//	出错了   把它重新排上（这是「重试」）
func (m *Manager) Enqueue(req AddRequest) (Snapshot, error) {
	if req.Bvid == "" || req.Cid == "" {
		// 不是网络错也不是 bug：调用方给错了参数，重试和排队都救不回来。
		return Snapshot{}, fmt.Errorf("download: 这一首缺稿件 id 或分 P id（bvid=%q cid=%q）", req.Bvid, req.Cid)
	}
	id := TaskID(req.Bvid, req.Cid, req.Video)

	// ⚠️ 落盘名在**锁里面**算：它要看队列里别的任务认领了谁（`destFor`），
	// 那张表在锁下才读得到。stat 的那几下一并在锁里——同名避让最多试 1000 次
	//（`maxNameTries`），正常情况下就是 1~2 次，够短。
	ext := extOr(req.Ext)

	m.mu.Lock()
	if t := m.tasks[id]; t != nil {
		retry := t.Status == StatusError
		snap := SnapshotOf(t)
		if retry {
			t.Status = StatusQueued
			t.Err = ""
			snap = SnapshotOf(t)
		}
		m.mu.Unlock()
		if retry {
			m.kick()
		} else {
			m.notify()
		}
		return snap, nil
	}

	t := &Task{
		ID:       id,
		Title:    req.Title,
		Cover:    req.Cover,
		Identity: Identity{Bvid: req.Bvid, Cid: req.Cid, Video: req.Video, VideoQ: req.VideoQ},
		Ext:      ext,
		Status:   StatusQueued,
		AddedAt:  m.now(),
		Video:    req.Video,
	}
	if req.Video {
		// 第二那条流（音频）从零开始：一个字节都没下过。
		// 建出来而不是等到交织那一刻再建，是因为 `destFor` 要多给它
		// 留出半成品后缀的位置（见 `destFor` 的 `video`）。
		t.Companion = &Companion{}
		if ext == ".m4a" {
			t.Ext = videoExt
		}
	}
	// 先把自己放进表里再挑名字：`destFor` 认得 id，会跳过自己。
	m.tasks[id] = t
	t.Dest = m.destFor(req.Dir, t)
	m.order = append(m.order, id)
	snap := SnapshotOf(t)
	m.mu.Unlock()

	m.kick()
	return snap, nil
}

// Pause 停下一项。已经停着的什么都不做（可以重复调）。
func (m *Manager) Pause(id string) error {
	m.mu.Lock()
	t := m.tasks[id]
	if t == nil {
		m.mu.Unlock()
		return ErrNotFound
	}
	if !t.Status.CanPause() {
		m.mu.Unlock()
		return nil
	}
	// 状态先落成暂停：不落的话这一项在队列里还是 running，而暂停是
	// **用户已经看到的意图**，它不该等到网络真的停下来才成立。
	t.Status = StatusPaused
	cancel := m.cancels[id]
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	m.notify()
	return nil
}

// Resume 接着下一项。暂停的和出错的都归它（「继续」和「重试」是同一个动作）。
func (m *Manager) Resume(id string) error {
	m.mu.Lock()
	t := m.tasks[id]
	if t == nil {
		m.mu.Unlock()
		return ErrNotFound
	}
	if t.Status == StatusDone || t.Status.Running() {
		m.mu.Unlock()
		return nil
	}
	t.Status = StatusQueued
	t.Err = ""
	m.mu.Unlock()

	m.kick()
	return nil
}

// Remove 从队列里拿掉一项，并删掉它的半成品。
//
// **正在下的先取消再删**：不取消的话那个 goroutine 会在删完之后继续往
// 一个已经不存在的任务里写，而那个任务的下一次落盘会凭空冒出一个
// `.part` 文件——用户已经删掉的东西还在下载目录里躺着。
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	t := m.tasks[id]
	if t == nil {
		m.mu.Unlock()
		return ErrNotFound
	}
	cancel := m.cancels[id]
	m.mu.Unlock()

	if cancel != nil {
		cancel()
		// 等它真的停下来再删文件：不等的话删完之后那个 worker 还可能
		// 往这个路径上写几个字节（fd 还开着，POSIX 语义），于是删掉的
		// 那个 inode 上又多出几百 KB 的垃圾，而目录里干干净净——用户
		// 看不到，磁盘也释放不掉。
		m.waitIdle(id)
	}

	m.mu.Lock()
	delete(m.tasks, id)
	m.order = drop(m.order, id)
	dest := t.Dest
	m.mu.Unlock()

	// 半成品删掉。已经下完的（真的那首歌）留着——用户删的是队列里这一
	// 项，不是硬盘上那首歌。
	//
	// **两个半成品都要删**：视频有两个，只删主那个的话音频那几百 KB
	// 永远躺在下载目录里，而界面上再没有任何东西指向它（`destFor` 的
	// 注释里那四步孤儿场景）。
	video := t.Video
	for _, p := range partPathsOf(dest, video) {
		_ = os.Remove(p)
	}
	m.notify()
	return nil
}

// Clear 清空队列并停掉所有在下的。已下完的文件留着。
func (m *Manager) Clear() {
	m.mu.Lock()
	ids := append([]string(nil), m.order...)
	cancels := make([]context.CancelFunc, 0, len(m.cancels))
	for _, c := range m.cancels {
		cancels = append(cancels, c)
	}
	dests := make([]string, 0, len(m.order))
	videos := make([]bool, 0, len(m.order))
	for _, id := range ids {
		if t := m.tasks[id]; t != nil {
			dests = append(dests, t.Dest)
			videos = append(videos, t.Video)
		}
	}
	m.mu.Unlock()

	for _, c := range cancels {
		c()
	}
	for _, id := range ids {
		m.waitIdle(id)
	}

	m.mu.Lock()
	m.tasks = map[string]*Task{}
	m.order = nil
	m.cancels = map[string]context.CancelFunc{}
	m.lastAt = map[string]time.Time{}
	m.mu.Unlock()

	for i, d := range dests {
		for _, p := range partPathsOf(d, videos[i]) {
			_ = os.Remove(p)
		}
	}
	m.notify()
}

// Snapshots 返回队列的一份拷贝，按入队顺序。改它不影响队列。
func (m *Manager) Snapshots() []Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Snapshot, 0, len(m.order))
	for _, id := range m.order {
		if t := m.tasks[id]; t != nil {
			out = append(out, SnapshotOf(t))
		}
	}
	return out
}

// Get 返回一项的快照。
func (m *Manager) Get(id string) (Snapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tasks[id]
	if t == nil {
		return Snapshot{}, false
	}
	return SnapshotOf(t), true
}

// Close 停掉所有在下的、落一次盘。可以重复调。
//
// **必须调**：它是退出前唯一一次把队列写下来的机会，而队列文件是
// 「下到哪儿了」的全部依据。不调的话用户下次开应用看到的是空队列，
// 盘上还有一堆 `.part`。
func (m *Manager) Close(path string) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	for _, c := range m.cancels {
		c()
	}
	m.mu.Unlock()

	m.quit()
	m.wg.Wait()

	// 🔴 存封面那些线也要收（`m.quit()` 已经把它们的请求掐断了，所以这一步不会
	// 拖慢退出）。顺序**不能换**：`Add` 只来自 `loop`，而 `m.wg.Wait()` 保证
	// `loop` 已经退出，所以到这里已经不会再有新的 `Add`（见 `Manager.coverWG`）。
	m.coverWG.Wait()

	// 🔴 后台那条线退出时**可能没走完队列**：`loop` 的外层 select 命中
	// `ctx.Done()` 就直接 return，还没轮到内层那个 `for`。那些项于是留在「排队中」。
	//
	// 而「排队中」是不该被存下去的语义——下次启动会**自动**接着下，用户关应用时
	// 可没要求过这件事。`runOne` 在关着的时候会把捡到的那一项标成暂停（「下次接着
	// 下」），但那是捡到才标，捡不到的那些没人管。实测这条竞态占 `go test` 的失败
	// 率约八成（连跑五次红四次），而它本该是确定的。
	//
	// 所以这里补上确定的一步：`loop` 已经退出，不会再有并发改状态。
	//
	// ⚠️ 这一段**与 `origin/main` 的 `#82` 修的是同一个问题**（那一票先一步把同样的
	// 逻辑抽成了 `pauseQueued`）。两边独立撞上同一条，说明它不是某一张票的边角——
	// 是 `Close` 的 `wg.Wait()` 之后必然有一批 `queued` 谁都没管。合并时采用上游那一
	// 份：它是个有名字的方法（可以单独被测），注释也写清了「为什么不能改成别的」。
	m.pauseQueued()

	if path == "" {
		return nil
	}
	return m.Save(path)
}

// pauseQueued 把还在排队的项落到「暂停」。
//
// ⚠️ 必须在后台那条线退出之后（`Close` 里 `wg.Wait()` 之后）调：这时才不会有并发
// 在改同一批状态。
func (m *Manager) pauseQueued() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tasks {
		if t != nil && t.Status == StatusQueued {
			t.Status = StatusPaused
		}
	}
}

// ---- 后台那条线 ----

// loop 一次下一首，下完接着下一首。
func (m *Manager) loop() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.wakeup:
		}
		for {
			id, ok := m.nextQueued()
			if !ok {
				break
			}
			m.runOne(id)
		}
	}
}

// kick 叫后台那条线醒来。
func (m *Manager) kick() {
	select {
	case m.wakeup <- struct{}{}:
	default: // 已经在等着了
	}
	m.notify()
}

// notify 叫界面要一帧。
//
// ⚠️ 铁律三的后半段：**只投递不唤醒，界面永远停在旧帧**。这里的 wake
// 由界面层装（`ui.Services.Invalidate`），而逐窗口的通知是界面层的事。
func (m *Manager) notify() {
	m.mu.Lock()
	wake := m.wake
	m.mu.Unlock()
	if wake != nil {
		wake()
	}
}

// nextQueued 找下一个该下的。**先来先服务**。
func (m *Manager) nextQueued() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range m.order {
		if t := m.tasks[id]; t != nil && t.Status == StatusQueued {
			return id, true
		}
	}
	return "", false
}

// runOne 下完一首。
func (m *Manager) runOne(id string) {
	m.mu.Lock()
	t := m.tasks[id]
	if t == nil || t.Status != StatusQueued || m.closed {
		// 🔴 **关着的时候要把这一项标成暂停**，不能原样返回。早退不改状态的话：
		// loop 的内层 `for` 不认 ctx.Done()，而 `nextQueued()` 只看 Status ——
		// 于是这一项被无限次捡起来又放下，`Close` 里的 `wg.Wait()` **永远等不到
		// 后台那条线**，应用退不出去（实测整包卡满 10 分钟超时）。
		//
		// 标成暂停也是对的语义：关应用时还排着队的，就是「下次接着下」的那一种
		// （`persistable()` 对正在下的也是这么记的）。
		if m.closed && t != nil && t.Status == StatusQueued {
			t.Status = StatusPaused
		}
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	t.Status = StatusRunning
	t.Err = ""
	m.cancels[id] = cancel
	wake := m.wake
	m.mu.Unlock()
	if wake != nil {
		wake()
	}

	// 🔴 **不能无限等 `run` 返回。**
	//
	// `Close` 里的 `wg.Wait()` 等的就是这条后台线，而这条线的 `runOne` 卡在 `run`
	// 里——`run` 的第一件事是 `m.resolve(ctx, want)`，**取流**。所以只要取流那一方
	// 不理 ctx，`Close` 就永远返回不了，**应用退不出去**（实测整个测试包卡满
	// 10 分钟超时）。
	//
	// 我们的 `resolveDownload` 走的是 `bili.Client.PlayURL(ctx, …)`，`http` 会尊重
	// ctx，所以真机上不会挂在这里。但「关掉应用」是一个**必须保证**的动作，不能
	// 建立在一句「大家都会尊重 ctx」上——`Resolver` 是个函数类型，谁实现它都能不
	// 理。
	//
	// 做法：把 `run` 放出去，用 select 等「它回来了」或者「ctx 取消了」。后者直接
	// 当这一页放弃，让 `runOne` 走下去把状态改成暂停、让后台线退出去。
	//
	// ⚠️ 代价：还在跑的 `run` 被丢在后台，它那个 goroutine 会**泄漏**，而且它可能
	// 还在往 `.part` 里写（而状态已经被标成暂停了）。这一条只在那条「取流方不理
	// ctx」的路上发生，而进程正要退出——**退不出去**和**退出时漏一个 goroutine**
	// 之间，后者明显更好。用户点「暂停」也走这条路，但那时我们的 `resolve` 尊重
	// ctx，`run` 会立刻自己退出来，select 几乎总是等 `done` 那一支。
	runDone := make(chan error, 1)
	go func() { runDone <- m.run(ctx, id) }()

	var err error
	select {
	case err = <-runDone:
	case <-ctx.Done():
		err = context.Canceled
	}
	cancel()

	m.mu.Lock()
	delete(m.cancels, id)
	var done Snapshot
	if t := m.tasks[id]; t != nil {
		switch {
		case err == nil:
			t.Status = StatusDone
			t.Err = ""
			done = SnapshotOf(t)
		case errors.Is(err, context.Canceled), errors.Is(err, ErrPaused):
			// 暂停 / 应用退出：这不是失败，用户还要接着下。
			//
			// ⚠️ 判的是 `Running()` 而不是「等于 running」：交织那一下也
			// 认取消（用户点了暂停 / 应用在退出），那时候状态已经是
			// `StatusMuxing` 了——只判 `running` 的话那一行会永远停在
			// 「交织中」，而按钮上的「继续」按下去也不动。
			if t.Status.Running() {
				t.Status = StatusPaused
			}
		default:
			t.Status = StatusError
			t.Err = err.Error()
			done = SnapshotOf(t)
		}
	}
	wake = m.wake
	onDone := m.onDone
	m.mu.Unlock()
	if wake != nil {
		wake()
	}
	// 🔴 **「下完了 / 下坏了」要报出去**（无障碍）：这一格的变化在下载记录页的
	// 一列上，而用户多半在别的页——**他既看不见那个勾，也听不见声音**。
	//
	// ⚠️ **只有终态才报**：暂停 / 取消不是「下完了」，报出去是撒谎。
	//
	// ⚠️ **锁外面调**：那是上层装进来的回调，而它回头要拿应用层的锁。
	if onDone != nil && done.ID != "" {
		onDone(done)
	}
}

// waitIdle 等一个任务真的停下来（取消之后网络要断开、goroutine 要退出）。
func (m *Manager) waitIdle(id string) {
	// 有上限：网络层的取消是即时的，但真机上「服务器不回也不断」的
	// 那种连接能拖十几秒。不能让界面上的「删除」按钮卡那么久。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		_, running := m.cancels[id]
		m.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ---- 锁内的小工具（给 fetch.go 用）----

// ⚠️ 下面这几个「读一项」的小工具**每一个都自己拿锁**，不是「调用方拿好
// 锁了再用」：调用点分散在 `fetch.go` 的各处，而 `m.tasks` 这张 map 会被
// `Enqueue` / `Remove` / `Clear` 改（它们跑在别的 goroutine 上）。
// `run` 是在 `runOne` 放开锁之后才进来的，那时候直接查 map 就是一次数据
// 竞争——而它不一定每次都被 `-race` 抓到（要正好撞上）。

// destFor 给 t 挑一个落盘路径。**必须在锁下调**——它要看队列里别的任务
// 认领了谁。
//
// 这就是 `DestFor` 少的那一样：`DestFor` 面对的是**盘**，而撞车恰恰发生在
// **队列**里（另一项排在队上认领了一个名字，盘上却什么都没有）。漏了它，
// 两首同名歌会拿到同一个路径，后下的那个把先下完的静默盖掉——见 `DestFor`
// 那段注释里的四步。
func (m *Manager) destFor(dir string, t *Task) string {
	claimed := make(map[string]bool, len(m.tasks))
	for id, other := range m.tasks {
		if id == t.ID || other.Dest == "" {
			continue
		}
		claimed[other.Dest] = true
	}
	return destFor(dir, t.Title, t.Ext, t.ID, t.Video, claimed)
}

// identityOf 拿一项的资源身份，取流要用。
func (m *Manager) identityOf(id string) (Identity, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tasks[id]
	if t == nil {
		return Identity{}, false
	}
	return t.Identity, true
}

// pendingChunks 返回指定那条流还没下完的分块下标。
func (m *Manager) pendingChunks(id string, companion bool) []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tasks[id]
	if t == nil {
		return nil
	}
	chunks := t.streamChunks(companion)
	done := make(map[int]bool, len(chunks))
	for _, i := range chunks {
		done[i] = true
	}
	n := totalChunks(t.streamLen(companion))
	out := make([]int, 0, n)
	for i := range n {
		if !done[i] {
			out = append(out, i)
		}
	}
	return out
}

// chunkBounds 给指定那条流的一块的闭区间。第二个返回值是「这一块现在
// 还算不算数」。
func (m *Manager) chunkBounds(id string, companion bool, i int) (int64, int64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tasks[id]
	if t == nil {
		return 0, 0, false
	}
	return chunkRangeOf(t.streamLen(companion), i)
}

// finishChunk 记下指定那条流的一块下完了，并报一次进度。
func (m *Manager) finishChunk(id string, companion bool, i int) {
	m.mu.Lock()
	t := m.tasks[id]
	if t == nil {
		m.mu.Unlock()
		return
	}
	if companion {
		if t.Companion != nil {
			t.Companion.Chunks, _ = markChunkOf(t.Companion.Chunks, i)
		}
	} else {
		t.markChunk(i)
	}
	m.mu.Unlock()
	m.noteProgress(id)
}

// noteProgress 报一次进度，**节流到 500 毫秒一次**。
//
// 字节数是从读回调里报上来的（每收到一个 TCP 包一次，一个任务每秒几千
// 次）。不节流的话界面每秒要重画几千次，画出来的是同一帧。
func (m *Manager) noteProgress(id string) {
	m.mu.Lock()
	t := m.tasks[id]
	if t == nil {
		m.mu.Unlock()
		return
	}
	now := time.Now()
	if now.Sub(m.lastAt[id]) < progressInterval {
		m.mu.Unlock()
		return
	}
	m.lastAt[id] = now
	wake := m.wake
	m.mu.Unlock()
	if wake != nil {
		wake()
	}
}

// extOr 把空的扩展名换成 `.m4a`。
//
// 这是**入队时那个临时名**用的猜测：B 站绝大多数音频是 AAC 装在 MP4 里，
// 而 B 站上游的 `getAudioExt()`（`download-core.ts:350`）也是这么兜的。
// 它在真正开始下之前会被地址上的后缀覆盖，所以猜错也不会留在盘上。
func extOr(ext string) string {
	if ext == "" {
		return ".m4a"
	}
	return ext
}

// videoExt 是视频下载的落盘扩展名。
//
// ⚠️ **不能按取流地址的后缀来**（那是音频那一票的做法，见 `extFor`）：
// 视频落盘的不是任何一条流，而是**交织出来的那个 MP4**。两条源流都是
// `.m4s`，而 `歌.m4s` 是一个谁也播不了的壳。
const videoExt = ".mp4"

// partPath 是半成品的路径。
//
// **在最终文件名后面加 `.part`，下完才改名**。理由有两条，缺一条都不够：
//
//	用户在自己的下载目录里看到的是**完整的一首**。预分配到完整长度的
//	做法（B 站上游的思路）会让一个半成品以完整大小躺在那里，用户点开
//	就是「打不开」——而歌名、图标、大小全都对，看起来像一个坏文件。
//	改名是原子的（同一个目录），所以中途退出也不会出现「两个歌名」。
//
//	它和最终文件在**同一个目录**，所以 rename 不跨设备。放到临时目录的
//	话，最后那一下 rename 可能变成一次几百兆的拷贝，而那正好是用户
//	快要看到「下完了」的那一刻。
func partPath(dest string) string { return dest + partSuffix }

// partSuffix 是半成品的后缀。挑落盘名时要给它预留位置（见 `destFor`），
// 不预留的话长歌名的 `.part` 会顶破文件名上限。
const partSuffix = ".part"

// companionPartPath 是第二那条流的半成品。
//
// 就是主半成品后面再接一段：`<歌>.mp4.part` 和 `<歌>.mp4.part.audio`。
// 用户在自己的下载目录里一眼能看出这两个是同一条视频的两半，而删的时候
// 是一并删的（见 `Remove` / `Clear`）。
//
// ⚠️ 视频任务的**主**半成品装的是视频流，第二那半装的是音频流。
// 两者的顺序不能反——它们的名字是这一票定的，而交织那一步是按
// 「主 = 视频、伴 = 音频」去读这两个文件的（见 `runMux`）。
func companionPartPath(dest string) string { return partPath(dest) + companionSuffix }

// companionSuffix 是第二那条流那一段的记号。
const companionSuffix = ".audio"

// mixPartPath 是**交织产物**的临时路径：`<歌>.mp4.part.mix`。
//
// # ⚠️ 它必须和那两条源流的半成品**不是一个文件**
//
// 交织是「读两条源流 → 写一个产物」。而 mp4ff 那一侧是**边扫源记偏移、
// 边写产物**，写产物和读源的数据是交错发生的——产物路径一旦等于某条源
// 流的路径，`os.Create` 就在读之前把那一条**截断了**，读回来的全是刚
// 写进去的字节。出来的文件**结构完全正常**：两条轨、对的 avcC/esds、
// 对的采样数，只有画面的内容是乱的。
//
// 三条各有各的名字，谁也不会盖到谁。名字总长（`.part.mix` 9 个字符）
// 也短过 `destFor` 给视频预留的 `.part.audio`（11 个），所以落盘名
// 的 255 字节预算一个字都不用改。
func mixPartPath(dest string) string { return partPath(dest) + mixSuffix }

// mixSuffix 是交织产物那一段的记号。
const mixSuffix = ".mix"

// partPathsOf 是这一项在盘上的全部半成品。
//
// 音频任务只有一个。视频有三个：两条源流各一个，加交织的产物。
// `Remove` / `Clear` 靠它把**每一个**都删掉——少删一个就是一份孤儿
// （见 `Remove` 里那段四步的场景）。
func partPathsOf(dest string, video bool) []string {
	paths := []string{partPath(dest)}
	if video {
		paths = append(paths, companionPartPath(dest), mixPartPath(dest))
	}
	return paths
}

// drop 从 ids 里去掉一个。
func drop(ids []string, id string) []string {
	out := ids[:0]
	for _, x := range ids {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}
