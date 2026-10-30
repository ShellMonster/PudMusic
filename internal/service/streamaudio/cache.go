// Package streamaudio 是**在线音频字节的落盘缓存**：一首歌的字节下过一遍之后
// 留在盘上，下次播同一首直接从盘上读，**不再走网络**。
//
// # 为什么单独一个包
//
// 字节是**从 B 站 CDN 拿**的（`service/bili` 的取流 + `service/audio` 的字节源），
// 而缓存**落在本机磁盘上**。两者混在一起的话，「存」和「取」就分不开了。
//
// ## 这一层不发请求
//
// 照 `service/lyrics` / `service/danmaku` 那一层的规矩：这一层只管读写盘，
// 取不取字节是调用方的事（`internal/app/online.go`）。
//
// # 一首一档一个文件
//
// 键是 `source|type|bvid|cid|sid` **加上音质档**：同一首歌在 128K 和无损下
// 是**两段不同的字节**，共用一个文件的话，第二档会把第一档的字节覆盖掉，
// 而第一档的文件名还指向它——于是切回第一档时听到的是第二档的声音。
//
// # 完整性靠**旁挂的元数据**，不靠文件大小
//
// ⚠️ **光看文件大小判不出「下完了」**：字节源的文件布局是
// 「顺序下载的前沿 + 并行取回来的末尾那一段 + 中间可能的洞」
// （见 `service/audio/stream_source.go` 的 `availAt`）。一个下到一半的文件
// 大小和整首**可能一样**（末尾那一段先到了），而它的中间是**洞**——
// 洞里的字节是零，解出来是噪声。
//
// 所以每份缓存旁边有一份 `.meta`，记着「这一份应该是多少字节」
// （那是 CDN 报回来的 Content-Length）。**下完了**的判据是
// 「文件大小 == 那个数」**而且**「那一趟真的报完了」。
//
// # 过期和容量
//
// 过期：7 天（和 `service/danmaku` 的 TTL 一样）。
// 容量：默认 2 GB，超了按**最久没动过的先删**（LRU）。
package streamaudio

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// TTL 是缓存的有效期：7 天。
//
// ⚠️ **它管的是「要不要再去问一次 B 站」，不是「这份字节还能不能播」**：
// 一份完整的字节在 7 天之后照样能播（B 站的音频不会因为过期而变），
// 只是过期之后会**重新下**（而下回来的那份是一样的）。
const TTL = 7 * 24 * time.Hour

// DefaultCapacity 是缓存目录的总容量上限：2 GB。
//
// 取 2 GB 的理由：一首 320K 的歌约 4 分钟、约 10 MB；2 GB 装得下两百首。
// 而用户数据目录里的东西是要用户自己清的（见 `docs/experience/data-and-credentials.md`），
// 所以上限要**小到用户不会觉得被占了空间**，也要**大到日常听歌用不着重下**。
const DefaultCapacity = int64(2) << 30

// Entry 是一份缓存的元数据。
//
// ⚠️ **它和字节分开放**（`.meta` 一个文件，字节一个文件）：字节是**大块**的，
// 而元数据是**小块**且**要频繁改**的（下载时要更新进度、下载完要写总长）。
// 放在一起的话，每写一次进度都要重写整个字节文件——那是几 MB 的写。
type Entry struct {
	// Key 是这一份的键（`source|type|bvid|cid|sid|quality`）。
	Key string `json:"key"`
	// Size 是这一份**应该**有多少字节。-1 = 还不知道（没下完、或者
	// CDN 没给 Content-Length）。
	//
	// ⚠️ **-1 的那一份不算完整**（见 `Complete`）：不知道有多长就判不出
	// 「下完了」。
	Size int64 `json:"size"`
	// Complete 说的是「这一份的字节已经齐了」。
	//
	// ⚠️ **它不是从文件大小推出来的**（见文件头「完整性靠旁挂的元数据」）：
	// 字节源的文件中间可能有洞，而洞里的字节是零。
	Complete bool `json:"complete"`
	// FetchedAt 是**这一份下完的时刻**。过期判据靠它。
	FetchedAt time.Time `json:"fetchedAt"`
	// TouchedAt 是**最后一次用到它**的时刻。容量淘汰按它排（LRU）。
	TouchedAt time.Time `json:"touchedAt"`
	// Label 是**这一份实际的音质档**（"192K" / "Hi-Res 无损" ……）。
	//
	// ⚠️ **它记的是「B 站真的给了哪一档」，不是「用户要哪一档」**：
	// 用户点了无损而这一首没有无损，B 站会给 192K（降档，票 #581），
	// 而缓存里躺的是**192K 的字节**。播放条上的徽章要照实写 192K——
	// 写「无损」是骗人。
	//
	// 空串 = 不知道（那就退到用缓存键里的那档偏好名）。
	Label string `json:"label,omitempty"`
}

// Expired 报这一份是不是过期了。
func (e Entry) Expired(now time.Time) bool {
	if e.FetchedAt.IsZero() {
		return true
	}
	return now.Sub(e.FetchedAt) > TTL
}

// Cache 是字节缓存。目录为空的那一份只读不写（测试和「没挂上」时用）。
type Cache struct {
	dir string
	// capacity 是容量上限（字节）。<= 0 用 DefaultCapacity。
	capacity int64
	// now 是时钟。nil 用 time.Now（给假时钟是为了让过期和淘汰能被测）。
	now func() time.Time
}

// NewCache 建一个落在 dir 的缓存。**建目录**：不在的话第一次写会失败，
// 而失败的表现是「每次播这一首都重新下一遍」。
//
// ⚠️ **空目录进、空目录出**：空目录的缓存只读不写（见 `Cache.Path`）。
// 拿不到用户数据目录时，缓存就整个关掉——那只是「每次都重新下」，
// 而不该挡着应用开起来。
func NewCache(dir string) *Cache {
	if dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	return &Cache{dir: dir, capacity: DefaultCapacity, now: time.Now}
}

// Dir 是缓存目录，空表示这个缓存不落盘。
func (c *Cache) Dir() string { return c.dir }

// WithCapacity 改容量上限。<= 0 不改。
func (c *Cache) WithCapacity(n int64) *Cache {
	if n > 0 {
		c.capacity = n
	}
	return c
}

// WithClock 换时钟。给假时钟是为了让过期和淘汰能被测。
func (c *Cache) WithClock(now func() time.Time) *Cache {
	if now != nil {
		c.now = now
	}
	return c
}

// Path 是 key 这一份字节在磁盘上的位置。空目录时是空串。
//
// ⚠️ **文件名是 key 的哈希**：键里带 `|` `/` `:`，在 Windows 上根本不是合法
// 文件名（和 `service/lyrics` 的缓存同一套理由）。
func (c *Cache) Path(key string) string {
	if c.dir == "" || key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:16])+".bin")
}

// metaPath 是 key 这一份的元数据在磁盘上的位置。
func (c *Cache) metaPath(key string) string {
	if c.dir == "" || key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:16])+".meta")
}

// Load 读 key 这一份的元数据。没有这一条、文件坏了、目录都没建，都只是「没有」。
//
// ⚠️ **读不出来一律当成没有，绝不报错**：缓存坏掉的表现应该是「重新下这一首」，
// 而不是应用开不起来。用户删掉缓存目录、清磁盘、换机器都会走到这里。
func (c *Cache) Load(key string) (Entry, bool) {
	path := c.metaPath(key)
	if path == "" {
		return Entry{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, false
	}
	var e Entry
	if err := json.Unmarshal(b, &e); err != nil {
		return Entry{}, false
	}
	return e, true
}

// CompleteEntry 读 key 这一份的元数据，**并且**确认它是一份可以直接播的
// 完整字节（见 `Complete` 的三个判据）。
//
// ⚠️ **它返回的是元数据和路径两样**：路径给播放那一层用，元数据给
// 「这一份是几 MB / 什么音质」用——**播放条上的音质徽章要照实写**
// （用户点了无损而 B 站给了 192K，那徽章就该写 192K）。
func (c *Cache) CompleteEntry(key string) (Entry, string, bool) {
	if c.dir == "" {
		return Entry{}, "", false
	}
	e, ok := c.Load(key)
	if !ok || !e.Complete {
		return Entry{}, "", false
	}
	if e.Expired(c.now()) {
		return Entry{}, "", false
	}
	path := c.Path(key)
	if path == "" {
		return Entry{}, "", false
	}
	st, err := os.Stat(path)
	if err != nil {
		return Entry{}, "", false
	}
	if e.Size >= 0 && st.Size() != e.Size {
		return Entry{}, "", false
	}
	// 记一次「用过」：容量淘汰按这个排（LRU）。
	e.TouchedAt = c.now()
	_ = c.Save(key, e)
	return e, path, true
}

// Save 把 key 这一份的元数据原子写下去。空目录时什么都不做。
func (c *Cache) Save(key string, e Entry) error {
	path := c.metaPath(key)
	if path == "" {
		return nil
	}
	if e.Key == "" {
		e.Key = key
	}
	if e.TouchedAt.IsZero() {
		e.TouchedAt = c.now()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := writeAtomic(path, b); err != nil {
		return err
	}
	// 写完就顺手做一次容量淘汰——否则「一直下」会让缓存目录无上限地长。
	c.Evict()
	return nil
}

// Complete 报 key 这一份是不是「一份可以直接播的完整字节」。
//
// ⚠️ **三个判据缺一不可**：
//
//  1. 元数据说它下完了（`Complete`）；
//  2. **没过期**（过期了就去重新下）；
//  3. **盘上的字节文件还在，而且大小对得上**——元数据说下完了而字节文件
//     被删了（用户清了缓存目录、清磁盘），那就得重新下。
//
// ⚠️ **第 3 条不能省**：只信元数据的话，用户清空缓存目录之后，应用会
// 拿着一份「下完了」的元数据去播一个**不存在的文件**——而那一份的字节
// 已经在磁盘上了。
func (c *Cache) Complete(key string) (string, bool) {
	if c.dir == "" {
		return "", false
	}
	e, ok := c.Load(key)
	if !ok || !e.Complete {
		return "", false
	}
	if e.Expired(c.now()) {
		return "", false
	}
	path := c.Path(key)
	if path == "" {
		return "", false
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	// 大小对不上就是没下完（或者字节被截断了）。
	if e.Size >= 0 && st.Size() != e.Size {
		return "", false
	}
	// 记一次「用过」：容量淘汰按这个排（LRU）。
	e.TouchedAt = c.now()
	_ = c.Save(key, e)
	return path, true
}

// Touch 记一次「用过 key 这一份」（只改元数据里的 `TouchedAt`）。
//
// ⚠️ **它不写字节文件**：那份字节一个字节都不用动。
func (c *Cache) Touch(key string) {
	e, ok := c.Load(key)
	if !ok {
		return
	}
	e.TouchedAt = c.now()
	_ = c.Save(key, e)
}

// Remove 删掉 key 这一份（字节和元数据都删）。
//
// ⚠️ **删不掉不算错误**：缓存清理是「尽力而为」——
// 用户手工删过文件、磁盘满了、权限变了，都会走到这里，
// 而那都不该挡住播放。
func (c *Cache) Remove(key string) {
	_ = os.Remove(c.Path(key))
	_ = os.Remove(c.metaPath(key))
}

// Evict 按容量上限删掉最久没动过的那些份。**不会**删掉正在用的那一份
// （调用方自己管那一份）。
//
// ⚠️ **它只删「元数据说下完了」的**：没下完的那些是**在途**的，
// 删掉它们等于让正在下的那一趟白下（而那一趟还在往那个文件里写）。
//
// ⚠️ **它按 `TouchedAt` 排（LRU）**：最近用过的先留着。
func (c *Cache) Evict() {
	if c.dir == "" || c.capacity <= 0 {
		return
	}
	entries, err := c.readAll()
	if err != nil {
		return
	}
	var total int64
	for _, e := range entries {
		if !e.Complete {
			continue
		}
		if st, err := os.Stat(c.Path(e.Key)); err == nil {
			total += st.Size()
		}
	}
	if total <= c.capacity {
		return
	}
	// 老的在前。
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].TouchedAt.Before(entries[j].TouchedAt)
	})
	for _, e := range entries {
		if total <= c.capacity {
			return
		}
		if !e.Complete {
			continue
		}
		size := int64(0)
		if st, err := os.Stat(c.Path(e.Key)); err == nil {
			size = st.Size()
		}
		c.Remove(e.Key)
		total -= size
	}
}

// readAll 读目录里所有的元数据。
//
// ⚠️ **读不出来的跳过，不报错**：那一份的元数据坏了（写了一半、被外力改了），
// 而它的字节文件还占着地方——清它是对的（它已经不可用了）。
func (c *Cache) readAll() ([]Entry, error) {
	if c.dir == "" {
		return nil, nil
	}
	dirEntries, err := os.ReadDir(c.dir)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, de := range dirEntries {
		name := de.Name()
		if !strings.HasSuffix(name, ".meta") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(c.dir, name))
		if err != nil {
			continue
		}
		var e Entry
		if err := json.Unmarshal(b, &e); err != nil {
			continue
		}
		if e.Key == "" {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// writeAtomic 临时文件 → fsync → rename。任何一步失败都把临时文件删掉。
//
// ⚠️ 临时文件建在**目标同一个目录**里：rename 只在同一文件系统内是原子的，
// 临时文件放到系统临时目录去的话，跨文件系统时 rename 会失败——而那正是
// 「用户把数据目录放在另一个盘上」的情形。
func writeAtomic(path string, b []byte) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".streamaudio-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(name)
		}
	}()

	if _, err = tmp.Write(b); err != nil {
		return err
	}
	// 数据落盘了但目录项没有的话，断电之后可能还是旧文件名。
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(name, 0o644); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	return syncDir(dir)
}

// syncDir 把目录项本身刷到盘上。Windows 上打不开目录句柄，直接跳过——
// 那边 rename 本身就已经是原子的。
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
