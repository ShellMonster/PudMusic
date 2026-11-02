// 切歌之后，上一首的字节源**不再立刻关**：收养给后台的 finisher，等它下完。
//
// # 这条治的是什么（2026-11-02）
//
// 字节缓存记「完整」的唯一时刻是**字节源下到 EOF**（`OnComplete` →
// `markByteCacheComplete`）。而用户切歌的那一瞬间，上一首的源往往只下了一半
// ——原来的行为是直接 `Close`：半截下载作废，缓存永远不完整。于是**用户
// 跳过的每一首都白下了**，下一次冷启动停在那一首上就要从零再下一整首
// ——「正在启动应用… 等半天」的元凶之一。
//
// 泵（`service/audio` 的 `pump`）是**自己往下走的**：不受读游标牵制，一路
// 下到 EOF 才停。所以「补完」不需要任何人读它——只要**别关它**。finisher
// 做的就三件事：
//
//	收养	把源从旧播放器的 closer 手里接过来（见 adoptedSource）
//	等完	源自己的 OnComplete 落缓存（那一份闭包认的还是这一首，不用重记）
//	收尸	下完（或拉不动了）之后把源关掉，把自己从表里摘掉
//
// # 边界
//
//   - **同一首被重新点上**：`cancelFinishersFor` 会先把在补的那条源关掉，
//     再让新源去建——新源以 `O_TRUNC` 打开同一个缓存文件，两条源同时写
//     同一个文件是绝对不许发生的。
//   - **退出**：`App.Close` 收掉所有在补的源。进程都要没了，下载到一半的
//     缓存留不住——跨进程补完是另一件事（断点续传），这一层不做。
//   - **用户按停止 / 清空**：那一耳是「用户不要了」，照旧立刻关（Stop 那条
//     路不收养）。

package app

import (
	"io"
	"sync/atomic"

	"github.com/ShellMonster/PudMusic/internal/service/audio"
)

// adoptedSource 包着一条字节源，把它的 Close 变成**可让渡**的。
//
// 播放器把源记在自己的 closers 里（`audio.OpenStream`），换歌时关播放器就
// 会连源一起关。收养之后这份 closer 变成空操作——源多活一段，归 finisher。
// 直接拿着裸源 `Close` 的那些路径（超时掐源、退出全关、finisher 自己收尸）
// 不经过这层包装，照常生效。
type adoptedSource struct {
	*audio.StreamSource
	adopted atomic.Bool
}

// Adopt 宣告「这条源归 finisher 了」。
func (s *adoptedSource) Adopt() { s.adopted.Store(true) }

// Close 在被收养之后是空操作；没被收养就照旧关。
func (s *adoptedSource) Close() error {
	if s.adopted.Load() {
		return nil
	}
	return s.StreamSource.Close()
}

// sourceFinisher 是一条被收养的字节源。
type sourceFinisher struct {
	// identity 是这一首的**入口无关**身份（`byteCacheIdentity`）。同一首从
	// 另一个入口被点上时（键可能还算不出来），靠它也能把在补的源找回来。
	identity string
	src      *audio.StreamSource
}

// byteCacheIdentity 报这一首「不管从哪个入口进来都是这一个」的身份。
//
// 它比 `byteCacheTrack` 粗（mv 只认到 bvid，不分 P）——它只用于「把同一首
// 的在补源收回来」这种宁可错杀的匹配，粗一点才罩得住 cid 还没补出来的那一路。
func byteCacheIdentity(it Item) string {
	if it.Type == "audio" {
		return "audio|" + it.Sid
	}
	if it.Bvid != "" {
		return "mv|" + it.Bvid
	}
	return ""
}

// adoptPreviousTrack 把**刚被换下去的那一首**的字节源收养给后台补完。
//
// 🔴 **必须在旧播放器关掉之前调**（`installOnlinePlayer` 的锁外段）：
// 收养是「旧播放器的 Close 关不到它」的唯一手段，晚了源就死了。
//
// ⚠️ **哪些条件不收养**：没有源（盘上缓存那一路）、键算不出来（收养了也没处
// 记账）、`adopt` 是 nil（这一条不是开新歌那一路装上的——换音质 / 从播放位置
// 重开那两条路装的源，换下去时照旧立刻关：它们是**同一首**的替换，留着写
// 反而要和新的抢同一个缓存文件）。
func (a *App) adoptPreviousTrack(prev *onlineTrack) {
	if prev == nil || prev.source == nil || prev.cacheTrack == "" || prev.adopt == nil {
		return
	}
	// 🔴 **先在锁里把退出判了**：`adopt` 一翻就没有人能关这条源了，而退出
	// 那一路（`abortAllFinishers`）只收**已经登记**的。已经进了退出流程的话
	// 不收养，让播放器的 Close 照旧把它带走。
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	if a.finishers == nil {
		a.finishers = map[string]*sourceFinisher{}
	}
	f := &sourceFinisher{identity: byteCacheIdentity(prev.item), src: prev.source}
	a.finishers[prev.cacheTrack] = f
	a.mu.Unlock()

	prev.adopt()
	go f.drain(a)
}

// drain 等这条源走到终态，然后收尸。
//
// 「等」的方式是**读它**：泵自己在往下走，读只是跟着它的进度走；下完那一刻
// 源会先报 `OnComplete`（缓存记上「完整」——那一份闭包认的还是这一首的
// 键和档），然后 `Read` 返回 `io.EOF`。中途拉不动了是 `ErrStreamDead`，
// 被人关了是 `ErrSourceClosed`——三种终态都一样：关源，走人。
func (f *sourceFinisher) drain(a *App) {
	defer a.removeFinisher(f)
	buf := make([]byte, 64<<10)
	for {
		if _, err := f.src.Read(buf); err != nil {
			if err == io.EOF {
				a.logf("biu: 上一首在后台下完了，缓存记上了")
			}
			break
		}
	}
	f.src.Close()
}

// removeFinisher 把走完了的 finisher 从表里摘掉。
func (a *App) removeFinisher(f *sourceFinisher) {
	a.mu.Lock()
	for k, v := range a.finishers {
		if v == f {
			delete(a.finishers, k)
		}
	}
	a.mu.Unlock()
}

// cancelFinishersFor 把「和 it 是同一首」的在补源收回来（关掉）。
//
// 🔴 **它必须在为 it 建新的字节源之前调**：新源以 `O_TRUNC` 打开同一个
// 缓存文件，两条源同时写一个文件写出来的就是垃圾。匹配按**键或身份**：
// 键罩得住「身份齐了的同一首」，身份罩得住「cid 还没补出来」的那一路。
//
// ⚠️ **锁外面关**：源的 `Close` 要等它的下载 goroutine 退出来。
func (a *App) cancelFinishersFor(it Item) {
	key := ""
	if byteCacheKeyReady(it) {
		key = byteCacheTrack(it)
	}
	ident := byteCacheIdentity(it)
	if key == "" && ident == "" {
		return
	}
	a.mu.Lock()
	var kill []*sourceFinisher
	for k, f := range a.finishers {
		if (key != "" && k == key) || (ident != "" && f.identity == ident) {
			delete(a.finishers, k)
			kill = append(kill, f)
		}
	}
	a.mu.Unlock()
	for _, f := range kill {
		f.src.Close()
	}
}

// abortAllFinishers 关掉**所有**在补的源。只给 `App.Close` 用：进程都要没了。
func (a *App) abortAllFinishers() {
	a.mu.Lock()
	all := make([]*sourceFinisher, 0, len(a.finishers))
	for k, f := range a.finishers {
		delete(a.finishers, k)
		all = append(all, f)
	}
	a.mu.Unlock()
	for _, f := range all {
		f.src.Close()
	}
}
