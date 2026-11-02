// 切歌 / 换档之后，被换下去的字节源**不再立刻关**：收养给后台的 finisher，
// 等它下完。
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
// # 表为什么按「曲目 + 档」建键
//
// 同一首的两个档是**两个不同的缓存文件**，可以同时各有一条在补的源（换音质
// 那一刻正是这个形状：旧档被收养、新档在播）。按曲目单键的话，新档收养会
// 把旧档的登记**顶掉**——顶掉的那条从此没人能收回，下一次同一首再被点上，
// 新源会截断旧档还在写的文件。
//
// # 边界
//
//   - **同一文件被重新点上**：精确回收（`cancelFinishersForCache`）会在新源
//     建起来**之前**把写着同一个文件的在补源关掉——两条源同时写一个文件
//     是绝对不许发生的。身份兜底（`cancelFinishersForItem`）罩住 cid 还没
//     问出来、键算不出来那一路。
//   - **退出**：`App.Close` 收掉所有在补的源。进程都要没了，下载到一半的
//     缓存留不住——跨进程的半截由**断点续传**接手（偏移文件）。
//   - **用户按停止 / 清空**：那一耳是「用户不要了」，照旧立刻关（Stop 那条
//     路不收养）。

package app

import (
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
	// identity 是这一首的**入口无关**身份（`byteCacheIdentity`）。cid 还没
	// 问出来、键算不出来的时候，靠它把同一首的在补源找回来。
	identity string
	src      *audio.StreamSource
	// track / tier 是这一份在缓存里的位置（**表键就是这两样**）。收养时要
	// Protect（不许被容量淘汰半路删掉），走人时要解除；两个动作配对。
	track string
	tier  string
}

// finisherKey 是在补源的表键：**曲目 + 档**——键就是缓存文件的身份，
// 键相同 = 文件相同 = 绝对不许两条源同时活着。
func finisherKey(track, tier string) string { return track + "\x00" + tier }

// byteCacheIdentity 报这一首「不管从哪个入口进来都是这一个」的身份。
//
// 它比 `byteCacheTrack` 粗（mv 只认到 bvid，不分 P）——它只用于「cid 还没
// 补出来」那一路的兜底回收，宁可错杀不可漏杀。
func byteCacheIdentity(it Item) string {
	if it.Type == "audio" {
		return "audio|" + it.Sid
	}
	if it.Bvid != "" {
		return "mv|" + it.Bvid
	}
	return ""
}

// adoptPreviousTrack 把**刚被换下去的那一条**的字节源收养给后台补完。
//
// 🔴 **必须在旧播放器关掉之前调**（各条安装路的锁外段）：收养是「旧播放器的
// Close 关不到它」的唯一手段，晚了源就死了。
//
// ⚠️ **哪些条件不收养**：没有源（盘上缓存那一路）、键算不出来（收养了也没处
// 记账）、`adopt` 是 nil（这一条不是能安全让渡的那几路装上的——**从播放位置
// 重开**那一路装的是**同曲同档**的替换源，让渡出去它会和新的抢同一个
// 缓存文件，换下去时照旧立刻关）。
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
	f := &sourceFinisher{
		identity: byteCacheIdentity(prev.item),
		src:      prev.source,
		track:    prev.cacheTrack,
		tier:     prev.cacheTier,
	}
	a.finishers[finisherKey(prev.cacheTrack, prev.cacheTier)] = f
	a.mu.Unlock()

	// 🔴 **补完的那一份不许被淘汰半路删掉**（审查①的 P2-2）：缓存逼近上限
	// 时，LRU 按「最久没动过」排，别的一首下完就会把这份还没动过几下的
	// 删掉——pump 还在往一个已 unlink 的 inode 上写，下完落的是一份
	// 「complete:true 指向不存在的 bin」的说谎记录。收养时刻刚被
	// `releaseByteCacheProtect` 解除过保护，这里要重新标上。
	//
	// ⚠️ **锁外面调**：它要拿 `a.mu`（取缓存对象）。
	a.protectByteCache(f.track, f.tier)
	prev.adopt()
	go f.drain(a)
}

// drain 等这条源走到终态，然后收尸。
//
// 「等」的方式是**读它**：泵自己在往下走，读只是跟着它的进度走；下完那一刻
// 源会先报 `OnComplete`（缓存记上「完整」并写日志——那一份闭包认的还是这一
// 首的键和档，「记没记上」只有它知道，这里不说这句话），然后 `Read` 返回
// `io.EOF`。中途拉不动了是 `ErrStreamDead`，被人关了是 `ErrSourceClosed`
// ——三种终态都一样：关源，走人。
func (f *sourceFinisher) drain(a *App) {
	defer a.removeFinisher(f)
	buf := make([]byte, 64<<10)
	for {
		if _, err := f.src.Read(buf); err != nil {
			break
		}
	}
	f.src.Close()
}

// removeFinisher 把走完了的 finisher 从表里摘掉，并解除它的淘汰保护。
//
// 🔴 **Protect / Unprotect 必须配对**：收养那一刻标上的保护，在**每一条**
// 退出路径（下完了、拉死了、被收回、退出全关）都要解掉——不然用户听过的
// 每一首被切走的歌都会永远占着盘，2 GB 上限形同虚设。
//
// ⚠️ **引用计数**（复审的 P3-b）：补完的最后一拍和「切回同一首」撞上时，
// 新播放 Protect 的一票不能被这里的 Unprotect 解掉——计数下这里只解
// 自己收养时记下的那一票。
func (a *App) removeFinisher(f *sourceFinisher) {
	a.mu.Lock()
	for k, v := range a.finishers {
		if v == f {
			delete(a.finishers, k)
		}
	}
	a.mu.Unlock()
	if f.track != "" && f.tier != "" {
		a.unprotectByteCache(f.track, f.tier)
	}
}

// cancelFinishersForCache 把「正在写**这一个缓存文件**」的源收回来——
// **在补的那条**，以及**正在播的那条**。
//
// 🔴 **它必须在为同一 (track, tier) 建新的字节源 / 读同一份缓存之前调**：
// 新源以 `O_TRUNC` 打开文件，读的人不该撞上一个还在写的人。匹配精确到档：
// 同首**另一档**是另一个文件，各补各的，不冲突也不该杀。
//
// 🔴 **「正在播的那条」也必须收**（2026-11-02 审查①的 P0-1，回归）：
// 同一首点第二次时，新源会 `O_TRUNC` **截断**那个缓存文件——而正在播的
// 旧源**不知情**，它继续从自己那个偏移往后写，于是盘上留下一个**洞**
// （新源的 [0,128K) ∪ 旧源的 [200K,256K)，中间 [128K,200K) 是洞）。
//
// 改动前这一段是安全的：旧源随即被 `old.Close()` 关掉，洞只存在一瞬，
// 下一趟从零重下就自愈了。**收养把它固化了**——而续传会从洞之后接着下，
// 洞**永远填不上**，而文件大小与整首一致 → 记成 `complete:true` →
// **永久坏缓存**（用户此后每次听这首都是坏字节，且系统永远不会重下它）。
//
// ⚠️ **锁外面关**：源的 `Close` 要等它的下载 goroutine 退出来。
func (a *App) cancelFinishersForCache(track, tier string) {
	if track == "" || tier == "" {
		return
	}
	// 🔴 **正在播的那一条**：它写的是同一个缓存文件。
	a.mu.Lock()
	var playing *audio.StreamSource
	if tr := a.online; tr != nil && tr.cacheTrack == track && tr.cacheTier == tier {
		playing = tr.source
	}
	a.mu.Unlock()
	if playing != nil {
		// 关掉它（等它的下载 goroutine 退出来）。
		//
		// ⚠️ **它正在放的那一份会被打断**——但那一首本来就要被换下去
		// （用户点了别的），而**留着它就会有两个写者同写一个文件**：新源
		// `O_TRUNC` 截断文件，旧源不知情、继续从旧偏移往后写 → 盘上留下一个
		// **洞**（新源的 [0,128K) ∪ 旧源的 [200K,250K)，中间是洞）。
		//
		// 改动前这一段是安全的（旧源随即被 `old.Close()` 关掉，洞只存在一瞬，
		// 下一趟从零重下就自愈）；**收养把它固化了**——而续传会从洞之后接着
		// 下，洞永远填不上，而大小与整首一致 → 记成 `complete:true` →
		// **永久坏缓存**。
		playing.Close()
	}
	a.mu.Lock()
	f, ok := a.finishers[finisherKey(track, tier)]
	if ok {
		delete(a.finishers, finisherKey(track, tier))
	}
	a.mu.Unlock()
	if ok {
		f.src.Close()
	}
}

// cancelFinishersForItem 按**入口无关身份**把同一首的在补源收回来——cid 还
// 没问出来、键算不出来那一路的兜底（宁粗勿漏：漏了就是两条源同写一个文件）。
// 键算得出来的调用方应该走 `cancelFinishersForCache`，别用这一档错杀别的分 P。
func (a *App) cancelFinishersForItem(it Item) {
	ident := byteCacheIdentity(it)
	if ident == "" {
		return
	}
	a.mu.Lock()
	var kill []*sourceFinisher
	for k, f := range a.finishers {
		if f.identity == ident {
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
