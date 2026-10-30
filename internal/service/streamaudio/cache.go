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
// # 两类记录：字节和「偏好 → 档」
//
// 盘上有两种记录，靠**文件名前缀**区分：
//
//	"b-" + <曲目> + "-" + <档> + ".bin"	字节
//	"b-" + <曲目> + "-" + <档> + ".json"	那一份字节的记录
//	"p-" + <曲目> + "-" + <偏好> + ".json"	「这个偏好解析到了哪一档」
//
// ## 🔴 为什么字节按**实际那一档**存，而不是按「用户要的那一档」
//
// 用户点了「无损」，而这一首没有无损——B 站会给 192K（降档，票 #581）。
// 于是**同一个偏好**在不同的歌上解析到不同的档，而**不同的偏好**在某些歌上
// 解析到同一个档。
//
// ⚠️ **如果字节按偏好存**，那么「无损 → 高音质 → 无损」这一圈下来，
// 用户会把**同一段字节**在盘上存两份（键不同）、下两遍（网络白花）。
// 而用户那句话要的正是「已经取到的就别再取」：
//
//	「前面比如取到了的，那应该是在本地电脑缓存上啊，
//	  为啥还要再去跑网络请求；应该是没有才跑网络请求啊」
//
// 所以：**字节按实际那一档存**（一份字节只存一份），
// 而「偏好 → 档」的映射单独记（一张小表）。
//
// 于是「无损」和「高音质」在**同一首歌**上解析到同一档时，
// 它们**指向同一份字节**——用户来回换档不会白下一遍。
//
// # 🔴 文件名里带身份，而不只是哈希
//
// 曲目身份里有 `|` `/` `:` 这些在 Windows 上**不是合法文件名**的字符，
// 所以文件名不能用它本身。而只用哈希的话，**身份就找不回来了**——
// 那时候「这一份是哪一首」只能靠记录文件里那一栏，而那一栏是**可以坏的**
// （用户手改、写盘写到一半被杀）。
//
// 拿坏掉的一栏去算路径的后果是：淘汰**删掉了另一个文件**，而真正那个
// `.bin` 永远删不掉——缓存目录于是无上限地长。
//
// 所以身份**转义成十六进制**放进文件名：十六进制里只有 `[0-9a-f]`，
// **没有分隔符会撞上**，而分隔符只有一个（`-`），所以**能可靠地反查回来**。
//
// # 「下完了」怎么判
//
// ⚠️ **不靠文件大小**：字节源的文件布局是「顺序下载的前沿 + 并行取回来的
// 末尾那一段 + 中间可能的洞」（见 `service/audio/stream_source.go` 的
// `availAt`）。一个下到一半的文件大小可能和整首**一样**（末尾那一段先到了），
// 而它的中间是**洞**——洞里的字节是零，解出来是噪声。
//
// 所以完整性靠**旁挂的记录**（`Bytes.Complete`），
// 而记录是**下完了那一刻**才写的。
//
// # 过期和容量
//
// 过期：7 天（和 `service/danmaku` 的 TTL 一样）。
// 容量：默认 2 GB，超了按**最久没动过的先删**（LRU）。
package streamaudio

import (
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

// 记录种类的前缀。
const (
	bytesPrefix = "b-"
	prefPrefix  = "p-"
)

// Meta 是这一份缓存附带的**界面要用的东西**。
//
// ⚠️ **它存在这里是因为「缓存命中那一路不发任何请求」**：那些字段（作者名、
// 封面、字幕那一对的 bvid/cid）正常是**问 B 站**问来的，而命中缓存时
// 不问——所以上一次问到的要存下来。
//
// ⚠️ **空串就是「没有」**：界面照旧显示空着，而不是猜一个。
type Meta struct {
	Title        string `json:"title,omitempty"`
	Cover        string `json:"cover,omitempty"`
	Artist       string `json:"artist,omitempty"`
	ArtistMid    int64  `json:"artistMid,omitempty"`
	SubtitleBvid string `json:"subtitleBvid,omitempty"`
	SubtitleCid  string `json:"subtitleCid,omitempty"`
}

// Bytes 是「一份下完的字节」的记录。
type Bytes struct {
	// Track 是这一份属于哪一首（曲目身份，不含音质）。
	Track string `json:"track"`
	// Tier 是**实际那一档**（`"q30280"` / `"m2"` 这样的标识）。
	Tier string `json:"tier"`
	// Size 是盘上那份字节的字节数。-1 = 还不知道。
	Size int64 `json:"size"`
	// Complete 说的是「这一份的字节已经齐了」。
	//
	// ⚠️ **它不是从文件大小推出来的**（见文件头「完整性靠旁挂的记录」）：
	// 字节源的文件中间可能有洞，而洞里的字节是零。
	Complete bool `json:"complete"`
	// Label 是这一份实际的音质档文字（"192K" / "Hi-Res 无损" ……）。
	//
	// ⚠️ **它记的是「B 站真的给了哪一档」，不是「用户要哪一档」**：
	// 用户点了无损而这一首没有无损，B 站会给 192K（降档），
	// 而缓存里躺的是**192K 的字节**。播放条上的徽章要照实写 192K——
	// 写「无损」是骗人。
	Label string `json:"label,omitempty"`
	// Meta 是界面要用的那些字段（见 `Meta`）。
	Meta Meta `json:"meta,omitempty"`
	// FetchedAt 是**这一份下完的时刻**。过期判据靠它。
	FetchedAt time.Time `json:"fetchedAt"`
	// TouchedAt 是**最后一次用到它**的时刻。容量淘汰按它排（LRU）。
	TouchedAt time.Time `json:"touchedAt"`
}

// Expired 报这一份是不是过期了。
func (b Bytes) Expired(now time.Time) bool {
	if b.FetchedAt.IsZero() {
		return true
	}
	return now.Sub(b.FetchedAt) > TTL
}

// Pref 是「这个偏好解析到了哪一档」。
type Pref struct {
	Track string `json:"track"`
	Pref  string `json:"pref"`
	Tier  string `json:"tier"`
	// TouchedAt 是最后一次用到它（容量淘汰按它排）。
	TouchedAt time.Time `json:"touchedAt"`
}

// Cache 是字节缓存。目录为空的那一份只读不写（测试和「没挂上」时用）。
type Cache struct {
	dir string
	// capacity 是容量上限（字节）。<= 0 用 DefaultCapacity。
	capacity int64
	// now 是时钟。nil 用 time.Now（给假时钟是为了让过期和淘汰能被测）。
	now func() time.Time
	// protect 是**正在用、不许淘汰**的那些份（按曲目 + 档）。
	//
	// 🔴 **它必须有**：淘汰会按「最久没动过的先删」排，而「正在放的那一份」
	// 恰恰是**刚被 Touch 过**的——但那只是在**容量只超了一点点**的时候
	// 才保得住它。用户数据目录里堆满了缓存的时候，正在听的歌会被删掉，
	// 歌就断了（那是最不能接受的一种后果）。
	protect map[string]bool
}

// NewCache 建一个落在 dir 的缓存。**建目录**：不在的话第一次写会失败，
// 而失败的表现是「每次播这一首都重新下一遍」。
//
// ⚠️ **空目录进、空目录出**：空目录的缓存只读不写。
// 拿不到用户数据目录时，缓存就整个关掉——那只是「每次都重新下」，
// 而不该挡着应用开起来。
func NewCache(dir string) *Cache {
	if dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	return &Cache{
		dir:      dir,
		capacity: DefaultCapacity,
		now:      time.Now,
		protect:  map[string]bool{},
	}
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

// encode 把身份转义成**文件名的一部分**。
//
// ⚠️ **它必须是可逆的**：身份里有 `|` `/` `:` 这些非法文件名字符，
// 而文件名又要能**反查回身份**（见文件头「文件名里带身份」那一段）。
//
// 转义用十六进制：只有 `[0-9a-f]`，**没有分隔符会撞上**。
func encode(s string) string { return hex.EncodeToString([]byte(s)) }

// decode 把文件名那一段**反查回**身份。
//
// ⚠️ **反查不出来就返回空串**：那一份是坏的（文件名被用户改过、
// 或者格式对不上），调用方据此当它不存在。
func decode(encoded string) string {
	raw, err := hex.DecodeString(encoded)
	if err != nil {
		return ""
	}
	return string(raw)
}

// BytesPath 是「曲目 + 档」这一份字节在磁盘上的位置。空目录时是空串。
func (c *Cache) BytesPath(track, tier string) string {
	if c.dir == "" || track == "" || tier == "" {
		return ""
	}
	return filepath.Join(c.dir, bytesPrefix+encode(track)+"-"+encode(tier)+".bin")
}

// bytesMetaPath 是「曲目 + 档」那一份的记录在磁盘上的位置。
func (c *Cache) bytesMetaPath(track, tier string) string {
	if c.dir == "" || track == "" || tier == "" {
		return ""
	}
	return filepath.Join(c.dir, bytesPrefix+encode(track)+"-"+encode(tier)+".json")
}

// prefPath 是「曲目 + 偏好」那张映射表在磁盘上的位置。
func (c *Cache) prefPath(track, pref string) string {
	if c.dir == "" || track == "" || pref == "" {
		return ""
	}
	return filepath.Join(c.dir, prefPrefix+encode(track)+"-"+encode(pref)+".json")
}

// Lookup 查「这个曲目、这个偏好」有没有一份**可以直接播**的字节。
//
// ⚠️ **它只查盘，不发请求**：这是「连地址都不要」的前提。
//
// 三个判据缺一不可：
//
//  1. 「偏好 → 档」那张表里有这一条；
//  2. 那一份的记录说它下完了；
//  3. **没过期**，而且**盘上的字节文件还在、大小对得上**。
//
// ⚠️ **第 3 条不能省**：只信记录的话，用户清空缓存目录之后，
// 应用会拿着一份「下完了」的记录去播一个**不存在的文件**。
//
// 返回 ok = false 表示「没有可用的缓存」，调用方照旧走网络那一路。
func (c *Cache) Lookup(track, pref string) (Bytes, bool) {
	if c.dir == "" {
		return Bytes{}, false
	}
	p, ok := c.readPref(track, pref)
	if !ok {
		return Bytes{}, false
	}
	b, ok := c.Complete(track, p.Tier)
	if !ok {
		return Bytes{}, false
	}
	return b, true
}

// Complete 查「曲目 + 档」这一份是不是一份可以直接播的完整字节。
func (c *Cache) Complete(track, tier string) (Bytes, bool) {
	if c.dir == "" {
		return Bytes{}, false
	}
	b, ok := c.readBytes(track, tier)
	if !ok || !b.Complete {
		return Bytes{}, false
	}
	if b.Expired(c.now()) {
		return Bytes{}, false
	}
	path := c.BytesPath(track, tier)
	if path == "" {
		return Bytes{}, false
	}
	st, err := os.Stat(path)
	if err != nil {
		return Bytes{}, false
	}
	// 大小对不上就是没下完（或者字节被截断了）。
	if b.Size >= 0 && st.Size() != b.Size {
		return Bytes{}, false
	}
	// 记一次「用过」：容量淘汰按这个排（LRU）。
	b.TouchedAt = c.now()
	_ = c.writeBytes(b)
	return b, true
}

// SaveBytes 记下一份**下完了**的字节。
//
// ⚠️ **它只在「真的下完了」时调**：中途断了的话盘上是「一半 + 洞」
// （见 `service/audio` 的 `availAt`），而那不是一份能播的缓存。
//
// ⚠️ **size 传 -1 表示「不知道有多长」**：那样的记录**不算完整**
// （见 `Complete` 的判据）——不知道有多长就判不出「下完了」。
func (c *Cache) SaveBytes(track, tier string, size int64, label string, meta Meta) error {
	if c.dir == "" {
		return nil
	}
	now := c.now()
	b := Bytes{
		Track:     track,
		Tier:      tier,
		Size:      size,
		Complete:  size >= 0,
		Label:     label,
		Meta:      meta,
		FetchedAt: now,
		TouchedAt: now,
	}
	if err := c.writeBytes(b); err != nil {
		return err
	}
	// 写完就顺手做一次容量淘汰——否则「一直下」会让缓存目录无上限地长。
	c.Evict()
	return nil
}

// SavePref 记下「这个曲目、这个偏好解析到了那一档」。
//
// ⚠️ **它在「字节下完之后」才调**：映射表指向的档必须真的有字节。
func (c *Cache) SavePref(track, pref, tier string) error {
	if c.dir == "" {
		return nil
	}
	p := Pref{Track: track, Pref: pref, Tier: tier, TouchedAt: c.now()}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return writeAtomic(c.prefPath(track, pref), b)
}

// Touch 记一次「用过曲目 + 档这一份」（只改记录里的 `TouchedAt`）。
func (c *Cache) Touch(track, tier string) {
	b, ok := c.readBytes(track, tier)
	if !ok {
		return
	}
	b.TouchedAt = c.now()
	_ = c.writeBytes(b)
}

// Protect 标住「这一份正在用，不许淘汰」。
func (c *Cache) Protect(track, tier string) {
	if c.dir == "" {
		return
	}
	c.protect[protectKey(track, tier)] = true
}

// Unprotect 解除「这一份正在用」。
func (c *Cache) Unprotect(track, tier string) {
	if c.dir == "" {
		return
	}
	delete(c.protect, protectKey(track, tier))
}

// protectKey 是「曲目 + 档」的保护键。
func protectKey(track, tier string) string { return track + "\x00" + tier }

// Remove 删掉「曲目 + 档」这一份（字节和记录都删）。
//
// ⚠️ **删不掉不算错误**：缓存清理是「尽力而为」——
// 用户手工删过文件、磁盘满了、权限变了，都会走到这里，
// 而那都不该挡住播放。
func (c *Cache) Remove(track, tier string) {
	_ = os.Remove(c.BytesPath(track, tier))
	_ = os.Remove(c.bytesMetaPath(track, tier))
}

// RemovePref 删掉「曲目 + 偏好」那张映射表的一条。
func (c *Cache) RemovePref(track, pref string) {
	_ = os.Remove(c.prefPath(track, pref))
}

// CleanOrphans 清掉目录里**没有配对**的那些文件。
//
// 🔴 **它必须做**：一次「没下完就切歌」会在盘上留下一个 `.bin` 而**没有**
// 它的 `.json`（记录是「下完了」才写的）。而那份 `.bin` 在容量账本之外
// （账本只数有记录的那些）——于是缓存目录**无上限地长**，2 GB 上限形同虚设。
//
// ⚠️ **反向的也要清**：有 `.json` 而没有 `.bin` 的那一份是**指向一个
// 不存在的字节文件**的（用户手工删过、或者上一次写盘写到一半被杀）。
//
// ⚠️ **它返回清掉了多少个文件**：调用方（以及测试）要看得见它干了什么。
func (c *Cache) CleanOrphans() int {
	if c.dir == "" {
		return 0
	}
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return 0
	}
	// 先把目录里的名字收成两张表（去掉后缀）。
	binSet := map[string]bool{}
	jsonSet := map[string]bool{}
	for _, de := range entries {
		name := de.Name()
		switch {
		case strings.HasPrefix(name, bytesPrefix) && strings.HasSuffix(name, ".bin"):
			binSet[strings.TrimSuffix(name, ".bin")] = true
		case strings.HasPrefix(name, bytesPrefix) && strings.HasSuffix(name, ".json"):
			jsonSet[strings.TrimSuffix(name, ".json")] = true
		}
	}
	removed := 0
	// 没有配对记录的 `.bin`：上一次进程被杀留下的半截。
	for base := range binSet {
		if jsonSet[base] {
			continue
		}
		if err := os.Remove(filepath.Join(c.dir, base+".bin")); err == nil {
			removed++
		}
	}
	// 没有配对字节的 `.json`：指向一个不存在的字节文件。
	for base := range jsonSet {
		if binSet[base] {
			continue
		}
		if err := os.Remove(filepath.Join(c.dir, base+".json")); err == nil {
			removed++
		}
	}
	return removed
}

// Evict 按容量上限删掉最久没动过的那些份。
//
// ⚠️ **正在用的那一份不许删**（见 `protect` 那一栏的注释）：正在播的歌被删掉
// 的话，歌就断了——那是最不能接受的一种后果。
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
	// 容量统计要算上**所有** `.bin`——包括那些没有记录的孤儿
	// （它们占着地方，却不在「有记录的那些」的账本里）。
	total := c.totalBytes(entries)
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
		if c.protect[protectKey(e.Track, e.Tier)] {
			continue
		}
		size := int64(0)
		if st, err := os.Stat(c.BytesPath(e.Track, e.Tier)); err == nil {
			size = st.Size()
		}
		c.Remove(e.Track, e.Tier)
		// 指向它的那几张映射表也作废（那一档没了，映射就没意义）。
		c.removePrefsForTier(e.Track, e.Tier)
		total -= size
	}
	// 🔴 **孤儿也得清**：它们不在账本的条目里，但它们占着地方。
	if total > c.capacity {
		c.cleanOrphanBytes(c.capacity - total)
	}
}

// totalBytes 报盘上现在占了多少字节（**包括没有记录的孤儿**）。
func (c *Cache) totalBytes(entries []Bytes) int64 {
	var total int64
	seen := map[string]bool{}
	for _, e := range entries {
		path := c.BytesPath(e.Track, e.Tier)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		if st, err := os.Stat(path); err == nil {
			total += st.Size()
		}
	}
	// 没有配对记录的 `.bin`。
	dirEntries, err := os.ReadDir(c.dir)
	if err != nil {
		return total
	}
	for _, de := range dirEntries {
		if !strings.HasPrefix(de.Name(), bytesPrefix) || !strings.HasSuffix(de.Name(), ".bin") {
			continue
		}
		path := filepath.Join(c.dir, de.Name())
		if seen[path] {
			continue
		}
		if info, err := de.Info(); err == nil {
			total += info.Size()
		}
	}
	return total
}

// cleanOrphanBytes 删掉**没有配对记录**的 `.bin`，直到不超 `room`。
//
// ⚠️ **只删最老的那些**：孤儿也按 mtime 排，和有记录的那些一个口径。
//
// 🔴 **绝不许删「有配对记录」的那些**：它们是正常的缓存（有记录的那些由
// `Evict` 的主循环负责，那一轮会连记录一起删）。在这里把它们删掉的话，
// 就留下了一堆「有记录、没有字节」的残骸——而那要等 `CleanOrphans`
// 那一轮才清得掉。
func (c *Cache) cleanOrphanBytes(room int64) {
	dirEntries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	// 先收下「有配对记录的」那些名字。
	paired := map[string]bool{}
	for _, de := range dirEntries {
		name := de.Name()
		if strings.HasPrefix(name, bytesPrefix) && strings.HasSuffix(name, ".json") {
			paired[strings.TrimSuffix(name, ".json")] = true
		}
	}
	type orphan struct {
		path string
		size int64
		mod  time.Time
	}
	var orphans []orphan
	for _, de := range dirEntries {
		name := de.Name()
		if !strings.HasPrefix(name, bytesPrefix) || !strings.HasSuffix(name, ".bin") {
			continue
		}
		if paired[strings.TrimSuffix(name, ".bin")] {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		orphans = append(orphans, orphan{
			path: filepath.Join(c.dir, name),
			size: info.Size(),
			mod:  info.ModTime(),
		})
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].mod.Before(orphans[j].mod) })
	for _, o := range orphans {
		if room >= 0 {
			return
		}
		if err := os.Remove(o.path); err == nil {
			room += o.size
		}
	}
}

// removePrefsForTier 删掉所有指向 tier 的映射表。
func (c *Cache) removePrefsForTier(track, tier string) {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	for _, de := range entries {
		if !strings.HasPrefix(de.Name(), prefPrefix) || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(c.dir, de.Name()))
		if err != nil {
			continue
		}
		var p Pref
		if json.Unmarshal(b, &p) != nil {
			continue
		}
		if p.Track == track && p.Tier == tier {
			_ = os.Remove(filepath.Join(c.dir, de.Name()))
		}
	}
}

// readAll 读目录里所有的**字节记录**。
//
// 🔴 **它靠文件名认身份，不靠记录里那一栏**：文件名里带着身份的转义
// （见文件头「文件名里带身份」），所以**用户手改过记录里那一栏**也不会
// 让淘汰删错文件——而拿坏掉的一栏去算路径的话，淘汰会**删掉另一个文件**，
// 真正那个 `.bin` 永远删不掉。
func (c *Cache) readAll() ([]Bytes, error) {
	if c.dir == "" {
		return nil, nil
	}
	dirEntries, err := os.ReadDir(c.dir)
	if err != nil {
		return nil, err
	}
	var out []Bytes
	for _, de := range dirEntries {
		name := de.Name()
		if !strings.HasPrefix(name, bytesPrefix) || !strings.HasSuffix(name, ".json") {
			continue
		}
		track, tier, ok := parseBytesName(name)
		if !ok {
			continue
		}
		b, err := os.ReadFile(filepath.Join(c.dir, name))
		if err != nil {
			continue
		}
		var rec Bytes
		if json.Unmarshal(b, &rec) != nil {
			continue
		}
		// 🔴 **身份以文件名为准**：记录里那一栏只是**记录**（可能被人改过）。
		rec.Track = track
		rec.Tier = tier
		out = append(out, rec)
	}
	return out, nil
}

// parseBytesName 从「字节记录」的文件名里**反查回**身份。
//
// ⚠️ **反查不出来就返回 false**：那一份是坏的（文件名被改过），
// 而它既不该被淘汰，也不该被当成一份缓存。
func parseBytesName(name string) (track, tier string, ok bool) {
	if !strings.HasPrefix(name, bytesPrefix) || !strings.HasSuffix(name, ".json") {
		return "", "", false
	}
	base := strings.TrimSuffix(strings.TrimPrefix(name, bytesPrefix), ".json")
	// 名字是 <曲目编码>-<档编码>。编码是十六进制（**没有 `-`**），
	// 所以按**唯一一个** `-` 切就够。
	idx := strings.Index(base, "-")
	if idx <= 0 || idx == len(base)-1 {
		return "", "", false
	}
	track = decode(base[:idx])
	tier = decode(base[idx+1:])
	if track == "" || tier == "" {
		return "", "", false
	}
	return track, tier, true
}

// readBytes 读「曲目 + 档」那一份的记录。
func (c *Cache) readBytes(track, tier string) (Bytes, bool) {
	path := c.bytesMetaPath(track, tier)
	if path == "" {
		return Bytes{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Bytes{}, false
	}
	var rec Bytes
	if err := json.Unmarshal(b, &rec); err != nil {
		return Bytes{}, false
	}
	return rec, true
}

// writeBytes 把一份字节记录原子写下去。
func (c *Cache) writeBytes(b Bytes) error {
	path := c.bytesMetaPath(b.Track, b.Tier)
	if path == "" {
		return nil
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return writeAtomic(path, raw)
}

// readPref 读「曲目 + 偏好」那张映射表的一条。
func (c *Cache) readPref(track, pref string) (Pref, bool) {
	path := c.prefPath(track, pref)
	if path == "" {
		return Pref{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Pref{}, false
	}
	var p Pref
	if err := json.Unmarshal(b, &p); err != nil {
		return Pref{}, false
	}
	if p.Tier == "" {
		return Pref{}, false
	}
	return p, true
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
