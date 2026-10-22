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
)

const (
	// diskFileExt 是缓存文件的扩展名。
	diskFileExt = ".img"
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
func newDiskCache(dir string, max int64) *diskCache {
	d := &diskCache{
		dir:     dir,
		max:     max,
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
	return data, true
}

// remove 删掉一份缓存并把它从账本上拿掉。解码发现盘上那份坏了的时候走这里。
func (d *diskCache) remove(name string) {
	path, ok := d.filePath(name)
	if !ok {
		return
	}
	d.mu.Lock()
	d.forgetLocked(name)
	d.mu.Unlock()
	_ = os.Remove(path)
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
		if err := d.writeFile(path, w.data); err == nil {
			d.note(w.name, int64(len(w.data)))
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
			_ = os.Remove(filepath.Join(d.dir, name))
		case isDiskName(name):
			info, err := ent.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			files = append(files, found{name: name, size: info.Size(), mod: info.ModTime()})
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
