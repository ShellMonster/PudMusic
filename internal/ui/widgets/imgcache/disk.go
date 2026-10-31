package imgcache

// 落盘那一半：目录、字节上限、最久未用，以及「临时文件 → rename」的原子写。
//
// 它和内存那一半（`lru.go`）**各记各的账**：内存那本记解码之后的 RGBA 和
// mipmap，这一本记盘上的图片文件。两本的上限也是两个旋钮——同一张图在两边的
// 大小差一个数量级（盘上是压缩后的几十 KB，内存里是解码后的几百 KB）。
//
// # 为什么必须有自己的上限
//
// 落盘那一半以前只写不删（而且根本没打开——调用点传的是空选项）：文件名是 URL
// 的哈希，同一个 URL 只写一次，而不同的 URL 会一直堆下去。一个歌单滚一遍就是
// 几百张封面，几天之后这个目录能长到几个 GB——而**没有任何一处会报错**，用户
// 只觉得「缓存目录怎么这么大」。所以顺序是硬要求：**先补上限和淘汰，再打开**。
//
// # 删除只发生在自己的目录里
//
// 这里每一个文件名都由 `diskNameFor` 生成（32 位十六进制 + `.img`），删除之前
// 还要再过一遍 `filePath`：名字里不许有路径分隔符、拼出来的父目录必须就是
// `dir` 本身。`filePath` 是唯一的拼接点，`sweep` / `trim` / `remove` 都走它
// ——少走一次，`../` 之类的名字就能删到目录外面去。目录里不是我们命名的东西
// （用户手放进去的文件、别的程序的残留）一律不碰，也不计入字节账。
//
// # 写盘是原子的，半截文件按「没有」处理
//
// 写的时候先在同目录建 `名字.tmp-*`，写完再 rename 过去。rename 是原子的，
// 所以最终文件名要么不存在、要么是完整的一份。进程在中途被杀，留下的只是一个
// `.tmp-*`，下次启动的清扫会把它删掉；就算有人把半截字节直接放在了最终文件名
// 上（磁盘坏了、外部工具写坏），解码会失败，调用方会把这一份删掉重新下载
// （见 `decodeWorker`），**不会把坏图解出来糊在界面上**。
//
// ⚠️ **删不掉的时候不能「读→失败→再读」**：那是一个没有出口的循环（一个解码
// worker 100% 占着 CPU，那张图永远停在「加载中」，网络一次都没被试过）。所以
// 盘上那份一坏，那一张在本次运行内就**不再从盘上读**（`entry.skipDisk`）。
//
// # 线程模型
//
// 读盘由调用方的后台 goroutine 调（`Cache.fetch`），写盘走本结构自己的
// writer goroutine（`loop`）。主线程（`Get` / 渲染帧）**一次都不碰磁盘**：
// 一屏十几张图，一次 `os.ReadFile` 卡住的就不只是这一帧。

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/diskcache"
)

const (
	// diskFileExt 是缓存文件的扩展名。
	diskFileExt = ".img"
	// diskPolicyMark 是「这一版开始管缓存的时刻」那个标记的文件名。
	//
	// 🔴 **它为什么需要**（2026-10-30 的第二轮对抗性审查）：这一层以前
	// **根本没有过期这一说**，所以用户盘上的老封面是「几个月前写的」。
	// 照 mtime 判的话，用户升级一次就会**几百张封面全部消失、全部重下**
	// （一屏几十张，每张都要走图床）。
	//
	// ⚠️ **它记的是「这一版第一次管缓存的时刻」**，而**升级前写的文件**的
	// 修改时刻比它早——那就是「老文件」的判据。
	//
	// ⚠️ **它跨重启保留**：否则每次启动都会续一次命，而那些文件**永远过期不了**。
	diskPolicyMark = ".ttl-policy-start"
	// diskTempMark 是写盘过程中临时文件的标记，跟着最终文件名走
	// （`<最终名>.tmp-<随机数>`），清扫时按它认出「写到一半」。
	diskTempMark = ".tmp-"
	// diskWriteQueue 是排队等着写的图。写盘比解码慢得多，这个队列让解码
	// worker 交完字节就回去干下一张，不必站在磁盘前面等。
	diskWriteQueue = 64
	// diskNameBytes 是 URL 哈希取多少字节再转十六进制：16 字节 = 32 个字符。
	// 撞一条的概率在这个量级（几百 GB 的图）上可以忽略，而文件名短一半好读。
	diskNameBytes = 16
)

// diskItem 是账本上的一行：盘上的一个文件占多少字节。
type diskItem struct {
	name string
	size int64
}

// diskWrite 是一次排队的写盘。
type diskWrite struct {
	name string
	data []byte
	// done 非 nil 时，这一笔真的落完（或失败）之后关上。测试用它把
	// 「写完再看盘」变成确定的；生产上一直是 nil。
	done chan struct{}
}

// diskCache 是落盘缓存。
type diskCache struct {
	dir string
	max int64
	// ttl 是**一份封面多久算过期**。<= 0 = 永不过期。
	//
	// 🔴 **过期是滚动的**（2026-10-30）：判据是「最后一次用过」，
	// 不是「下载那一刻」——用户天天在看的那首歌，它的封面不该在第七天消失。
	//
	// ⚠️ **判据是文件自己的修改时刻**（见下面 `open` 的注释），而**不是**
	// 旁挂记录——那要求每次读都写一份记录（读路径变成写路径，还会攒垃圾）。
	ttl time.Duration
	// now 是时钟。nil 用 time.Now（给假时钟是为了让过期能被测）。
	now func() time.Time
	// start 是「这一版开始管缓存的时刻」（见 `policyStart`）。
	//
	// ⚠️ **它是「老文件」的判据**：修改时刻比它早的那一份，是**升级前写的**——
	// 而这一层以前根本没有过期这一说，所以那些文件不该在升级后被一次清空。
	start time.Time
	// startMu 护着 `start`（它是**懒初始化**的：第一次用到缓存的时候才定下来）。
	startMu sync.Mutex

	// writeFile 是「把字节原子地写到 path」的那一格。默认是同目录临时文件 +
	// rename；测试换成一个会停住的实现，用来证明写盘慢不挡主线程。
	writeFile func(path string, data []byte) error
	// readFile 是读盘那一格。默认 os.ReadFile；测试换成一个会停住的实现，
	// 用来证明 `Get` 不等磁盘。
	readFile func(path string) ([]byte, error)

	writeCh chan diskWrite
	// ready 在启动清扫跑完之后关上。测试等它，生产上没人等。
	ready chan struct{}

	closeOnce sync.Once
	wg        sync.WaitGroup

	mu    sync.Mutex
	used  int64
	order *list.List // 元素是 *diskItem，最近用过的在前面
	index map[string]*list.Element
}

// newDiskCache 建一个落盘缓存，并在后台先清一遍目录（上次异常退出留下的超额
// 文件和半截临时文件）。
//
// ⚠️ 清扫在后台：目录里可能躺着几千个文件，启动时在主线程上遍历它们等于把
// 第一帧往后推。清扫删掉某个文件时如果正好有人在读它，读的那一方拿到的要么是
// 已经打开的完整内容、要么是一次 miss（回去下载）——两种都对。
func newDiskCache(dir string, max int64, ttl time.Duration) *diskCache {
	d := &diskCache{
		dir: dir,
		max: max,
		ttl: ttl,
		now: time.Now,
		// ⚠️ **宽限期比 TTL 长**：老封面多留一个 TTL 的时间（也就是用户还有
		// 整整一个 TTL 的时间不用管它们），而它们在那之后照常过期。
		writeCh: make(chan diskWrite, diskWriteQueue),
		ready:   make(chan struct{}),
		order:   list.New(),
		index:   make(map[string]*list.Element),
	}
	d.readFile = os.ReadFile
	d.writeFile = func(path string, data []byte) error {
		return writeDiskFile(dir, path, data)
	}
	d.wg.Add(1)
	go d.loop()
	return d
}

// loop 是唯一的 writer goroutine：先清扫，再按顺序写。
func (d *diskCache) loop() {
	defer d.wg.Done()
	d.sweep()
	close(d.ready)
	for w := range d.writeCh {
		d.store(w)
	}
}

// write 把一张图排进写盘队列。任何 goroutine 都能调，不会碰磁盘本身；
// 写完由 writer goroutine 计入账本并触发淘汰。
//
// 队列满时**直接丢掉这一份**：磁盘慢（或者卡住）的时候，让解码 worker 站在
// 队列前面等，等于把「这张图能不能画出来」交给磁盘。落盘只是让下次启动少下
// 一次，丢一份的代价是下次再下一次——比卡住整条解码链便宜得多。
func (d *diskCache) write(name string, data []byte) {
	if name == "" {
		return
	}
	select {
	case d.writeCh <- diskWrite{name: name, data: data}:
	default:
	}
}

// writeSync 是 write 的测试版本：等这一笔真的写完（或失败）再返回。
func (d *diskCache) writeSync(name string, data []byte) {
	done := make(chan struct{})
	d.writeCh <- diskWrite{name: name, data: data, done: done}
	<-done
}

// close 收掉 writer goroutine，并把已经排着的写盘全部落完。
func (d *diskCache) close() {
	d.closeOnce.Do(func() { close(d.writeCh) })
	d.wg.Wait()
}

// filePath 把一个缓存文件名变成盘上的路径，同时守住「删除只在缓存目录内」。
//
// 名字里带路径分隔符、或者拼出来的东西不在 dir 这一层，一律拒绝。这是所有
// 删文件/写文件的唯一拼接点——绕开它一次，`../` 这种名字就能写到外面去。
func (d *diskCache) filePath(name string) (string, bool) {
	if name == "" || name == "." || name == ".." || name != filepath.Base(name) {
		return "", false
	}
	path := filepath.Join(d.dir, name)
	if filepath.Dir(path) != filepath.Clean(d.dir) {
		return "", false
	}
	return path, true
}

// open 读一份盘上的缓存。miss（文件不在、读不了、大小不像我们写的）返回 false；
// 读到之后把它提到 LRU 的最前面。
//
// 只由后台 goroutine 调（`Cache.fetch`）。**绝不在 Get / 渲染帧里调**。
func (d *diskCache) open(name string) ([]byte, bool) {
	path, ok := d.filePath(name)
	if !ok {
		return nil, false
	}
	// 🔴 **过期的那一份不算命中**（2026-10-30）：过期是**滚动的**——判据是
	// 「最后一次用过」，不是「下载那一刻」。
	//
	// ⚠️ **判据是文件自己的修改时刻**（mtime）：每次用到它都把它推到当下。
	// 而**不另记一份「用过」**：那要求每次读都写一次盘（读路径变成写路径，
	// 实测 100 张从 5.8 毫秒涨到 858 毫秒），还会攒出一堆「有记录、没有图」
	// 的残骸。
	//
	// ⚠️ **过期了就删掉**：留着它只会占地方，而它已经不会再被命中了。
	if d.expired(name) {
		_ = d.remove(name)
		return nil, false
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	// 不是普通文件、或者大得不像一张图：不读、也不当成命中。后者可能是别的
	// 东西占了这个名字，删掉它——名字在我们的命名空间里，删它是安全的。
	if !info.Mode().IsRegular() || info.Size() > maxImageBytes {
		d.remove(name)
		return nil, false
	}
	data, err := d.readFile(path)
	if err != nil {
		return nil, false
	}
	d.touch(name, int64(len(data)))
	// 🔴 **记一次「用过」**：把文件的修改时刻推到当下——**只改元数据，
	// 不碰内容**。
	diskcache.Touch(path, d.now())
	return data, true
}

// policyStart 返回「这一版开始管缓存的时刻」。
//
// 🔴 **它是「老文件」的判据**（2026-10-30 的第二轮对抗性审查）：这一层以前
// **根本没有过期这一说**，所以用户盘上的老封面是「几个月前写的」——照 mtime
// 判的话，用户升级一次就会**几百张封面全部消失、全部重下**（一屏几十张，
// 每张都要走图床）。
//
// ⇒ 所以「老文件」= **修改时刻早于这一刻**的那一份。而**这一版自己写的**
// 那些修改时刻都在它之后（或正好等于），所以它们照常按 TTL 过期。
//
// ⚠️ **它跨重启保留**（记在 `diskPolicyMark` 那个文件里）：否则每次启动都会
// 把「这一版的时刻」推到当下，于是那些文件**永远过期不了**。
//
// ⚠️ **它是懒初始化的**（第一次用到缓存的时候才定下来）：那样的话，测试用
// 假时钟时它拿到的就是那个假时钟的「现在」。
func (d *diskCache) policyStart() time.Time {
	d.startMu.Lock()
	defer d.startMu.Unlock()
	if !d.start.IsZero() {
		return d.start
	}
	path := filepath.Join(d.dir, diskPolicyMark)
	if b, err := os.ReadFile(path); err == nil {
		if t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(b))); err == nil {
			// ⚠️ **也截到秒**：理由同上（盘上的修改时刻精度不一定是纳秒）。
			d.start = t.Truncate(time.Second)
			return d.start
		}
	}
	// 第一次用这一版：把「从现在起」记下来。
	//
	// 🔴 **截到秒**（2026-10-30 的第三轮自查）：盘上那个修改时刻的精度**不
	// 一定到纳秒**（HFS+ 是秒，APFS 是纳秒，而网络盘和 Linux 上各有各的）。
	// 而比较是「修改时刻**早于** policyStart 就算老文件」——所以 policyStart
	// 带纳秒的话，同一秒里续命的那些封面会被判成「早于」，于是**永远不过期**
	// （而它们本该从那一刻起照常按 TTL 过期）。
	//
	// ⇒ 两边都截到秒：policyStart 记秒，盘上的比较也按秒。
	d.start = d.now().Truncate(time.Second)
	_ = os.WriteFile(path, []byte(d.start.Format(time.RFC3339Nano)), 0o644)
	return d.start
}

// expired 报这一张图是不是过期了。
//
// 🔴 **判据是文件自己的修改时刻**（mtime）：那才是「最后一次用过它」的
// 时刻——每一次用到它都会把它推到当下（见 `open`）。
//
// ⚠️ **升级前写的那些不算过期**（见 `policyStart`）：它们不该在用户升级的
// 那一刻被一次清空。而清扫那一轮会把它们的修改时刻推到当下（见 `sweep`），
// 于是它们从那一刻开始**照常按 TTL 过期**。
func (d *diskCache) expired(name string) bool {
	path, ok := d.filePath(name)
	if !ok {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if d.ttl <= 0 {
		return false
	}
	// ⚠️ **升级前写的**：不算过期。
	if info.ModTime().Before(d.policyStart()) {
		return false
	}
	return d.now().Sub(info.ModTime()) >= d.ttl
}

// remove 删掉一份缓存并把它从账本上拿掉。解码发现盘上那份坏了的时候走这里。
//
// ⚠️ **删不掉要说话**：目录只读、文件被别的进程占着，都会让 `os.Remove` 失败。
// 那种情况下文件还在（还占着地方），所以它**留在账上**，而调用方必须知道这次
// 没删掉——「以为删掉了」会让调用方走进一个没有出口的循环（见 `decodeWorker`
// 里那一段）。
func (d *diskCache) remove(name string) error {
	path, ok := d.filePath(name)
	if !ok {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	d.mu.Lock()
	d.forgetLocked(name)
	d.mu.Unlock()
	return nil
}

// forgetLocked 把 name 从账本里摘掉（调用方持锁）。
func (d *diskCache) forgetLocked(name string) {
	el, ok := d.index[name]
	if !ok {
		return
	}
	it := el.Value.(*diskItem)
	d.order.Remove(el)
	delete(d.index, name)
	d.used -= it.size
}

// touch 记一次使用，必要时把文件大小记进账本。
//
// 这里**不触发淘汰**：读一张图顺手删掉别的几张，把读路径变成了写路径。超额
// 由下一次写盘或者启动清扫收拾，那两处本来就要动磁盘。
func (d *diskCache) touch(name string, size int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if el, ok := d.index[name]; ok {
		it := el.Value.(*diskItem)
		if it.size != size {
			d.used += size - it.size
			it.size = size
		}
		if d.order.Front() != el {
			d.order.MoveToFront(el)
		}
		return
	}
	el := d.order.PushFront(&diskItem{name: name, size: size})
	d.index[name] = el
	d.used += size
}

// note 记一次写完，并把超额的按最久未用挤掉。
func (d *diskCache) note(name string, size int64) {
	d.mu.Lock()
	if el, ok := d.index[name]; ok {
		it := el.Value.(*diskItem)
		d.used += size - it.size
		it.size = size
		d.order.MoveToFront(el)
	} else {
		el := d.order.PushFront(&diskItem{name: name, size: size})
		d.index[name] = el
		d.used += size
	}
	victims := d.trimLocked()
	d.mu.Unlock()
	d.removeFiles(victims)
}

// trimLocked 从最久未用那一头挤，直到不超上限，返回该删的文件名（调用方持锁）。
//
// 单张图比整个上限还大时它自己也会被挤掉——留着一张超额的图，上限就不再是
// 上限了。
func (d *diskCache) trimLocked() (victims []string) {
	for d.used > d.max {
		el := d.order.Back()
		if el == nil {
			break
		}
		it := el.Value.(*diskItem)
		d.order.Remove(el)
		delete(d.index, it.name)
		d.used -= it.size
		victims = append(victims, it.name)
	}
	return victims
}

// removeFiles 删掉一批缓存文件。删之前每个名字都过一遍 filePath。
func (d *diskCache) removeFiles(names []string) {
	for _, name := range names {
		if path, ok := d.filePath(name); ok {
			_ = os.Remove(path)
		}
	}
}

// store 是 writer goroutine 里的真正写盘：临时文件 → rename → 记账 → 淘汰。
func (d *diskCache) store(w diskWrite) {
	path, ok := d.filePath(w.name)
	if ok {
		// 🔴 **先定下「这一版开始管缓存的时刻」，再写盘**（2026-10-30）：
		// 顺序反了的话，**这一版自己写的每一份**的修改时刻都早于那一刻——
		// 于是它们**全都成了「老文件」**，一份都不过期。
		//
		// ⚠️ **判据是「修改时刻在 policyStart 之后」**（见 `expired`），
		// 而写盘那一刻的修改时刻就在 policyStart 之后。
		d.policyStart()
		if err := d.writeFile(path, w.data); err == nil {
			d.note(w.name, int64(len(w.data)))
			// 🔴 **把修改时刻设成「下载完的那一刻」**：过期的判据是 mtime
			// （见上面 `open` 的注释），而它该从这一份**到位**的时候算起。
			diskcache.Touch(path, d.now())
		}
	}
	if w.done != nil {
		close(w.done)
	}
}

// writeDiskFile 原子地把 data 写到 path：同目录临时文件 → close → rename。
//
// 不 fsync：缓存丢了可以再下，而每张图一次 fsync 的代价比重新下载还高。
// rename 保证的是「最终文件名下永远是一份完整的字节」，断电最多丢这一次写。
func writeDiskFile(dir, path string, data []byte) error {
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+diskTempMark+"*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil {
		_ = os.Remove(tmpName)
		return werr
	}
	if cerr != nil {
		_ = os.Remove(tmpName)
		return cerr
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// sweep 是启动时那一次清扫：认出目录里哪些文件是我们的、删掉上次写到一半的
// 临时文件、按修改时间把最老的挤掉。
//
// 修改时间是这里唯一能跨进程拿到的「最近用过」的证据：文件内容我们没动过，
// 用完也不回写 mtime（每读一张图就改一次时间戳，等于把读变成了写）。所以
// 跨进程的排序是「上次写它的时候」，本次运行内的排序由内存里的 LRU 接着管。
func (d *diskCache) sweep() {
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		return
	}
	type found struct {
		name string
		size int64
		mod  time.Time
	}
	var files []found
	for _, ent := range entries {
		name := ent.Name()
		switch {
		case isDiskTempName(name):
			// 上次写到一半留下的：最终文件名下没有它，留着只会占地方。
			//
			// ⚠️ 路径也走 `filePath`：删除只发生在缓存目录内这一条，不许有第二
			// 个拼接点（名字来自 `os.ReadDir`，永远是基名，但规矩得是规矩）。
			if path, ok := d.filePath(name); ok {
				_ = os.Remove(path)
			}
		case isDiskName(name):
			info, err := ent.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			// 🔴 **升级前写的那些：给它们续一次命**（2026-10-30 的第二轮
			// 对抗性审查）。
			//
			// ⚠️ **为什么**：这一层以前**根本没有过期这一说**，所以用户盘上的
			// 老封面是「几个月前写的」——照 mtime 判的话，用户升级一次就会
			// **几百张封面全部消失、全部重下**（一屏几十张，每张都要走图床）。
			//
			// ⚠️ **续的是「从现在起算一个 TTL」**：它们从这一刻开始**照常过期**，
			// 而宽限期不是免死金牌。
			//
			// ⚠️ **排序用的仍是原来的修改时刻**：那是「上一次真的用到它」的
			// 时刻，而**淘汰要按那个排**（最久没用的先挤）。续命改的是
			// 「过期判据」，不是「淘汰顺序」。
			mod := info.ModTime()
			if mod.Before(d.policyStart()) {
				diskcache.Touch(filepath.Join(d.dir, name), d.now())
			} else if d.expired(name) {
				// 🔴 **过期的那一份在这一轮就清掉**（2026-10-30）：不然它要等到
				// 「容量超了才挤」才消失——而容量可能一直不超，那它就一直占着
				// 地方。
				_ = d.remove(name)
				continue
			}
			files = append(files, found{name: name, size: info.Size(), mod: mod})
		}
		// 别的东西不碰：目录是专用的，但「专用」不等于「我们有权删用户的文件」。
	}
	// 从新到旧排：后面按「 PushBack」加，队尾留的于是最老的那一份（先被挤）。
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })

	d.mu.Lock()
	// ⚠️ **不重置账本**：清扫和「读盘」是并着的（读盘在别的 goroutine 上）。
	// 重置的话，这期间刚被读过的那几份会从账上消失，字节数就少算了，超额
	// 也就跟着少挤。已经记上的留着（它比扫描时新），其余按修改时间补进队尾。
	for _, f := range files {
		if _, ok := d.index[f.name]; ok {
			continue
		}
		el := d.order.PushBack(&diskItem{name: f.name, size: f.size})
		d.index[f.name] = el
		d.used += f.size
	}
	victims := d.trimLocked()
	d.mu.Unlock()
	d.removeFiles(victims)
}

// diskNameFor 是 url 在磁盘缓存里的文件名。
//
// 用 URL 的哈希而不是原地址：URL 里有 `?`、`/`、`@` 这些不能当文件名的字符，
// 而且长度没有上限。取前 16 字节（128 位）已经远超「几百 GB 图不撞车」的需要。
func diskNameFor(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:diskNameBytes]) + diskFileExt
}

// isDiskName 认「我们自己命名的缓存文件」：32 位小写十六进制 + `.img`。
func isDiskName(name string) bool {
	if len(name) != diskNameBytes*2+len(diskFileExt) || !strings.HasSuffix(name, diskFileExt) {
		return false
	}
	for _, c := range name[:diskNameBytes*2] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// isDiskTempName 认「上次写到一半的临时文件」：`<缓存文件名>.tmp-<随机数>`。
func isDiskTempName(name string) bool {
	i := strings.LastIndex(name, diskTempMark)
	return i > 0 && isDiskName(name[:i])
}
