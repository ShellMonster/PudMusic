// 评论缓存：一条评论对象一个文件，落在磁盘上。
//
// # 为什么照歌词缓存那一套写
//
// 歌词缓存（`internal/service/lyrics/cache.go`）已经趟过一遍：写一首不会碰另外
// 几十首，而 JSON 的键名要能对上 Biu 那边。评论缓存要的东西一样——
// **一个对象一个文件**、**原子写**、**读不出来一律当成没有**——所以照它写，
// 而不是另发明一种存法。
//
// # 键是 `<type>-<oid>`
//
// B 站的评论接口用两样东西认一个对象：`type`（1 = 视频稿件 / 14 = 音乐库音频）
// 和 `oid`（视频是 **avid**，音频是 **auid**）。**只按 oid 认会撞**：
// avid 是 12345、auid 也是 12345 是两回事，而它们各自有一整个评论区。
// ⇒ 键里必须带上 type（`1-12345` 和 `14-12345` 是两个文件）。
//
// # 写盘是原子的
//
// 临时文件 → fsync → rename（和歌词缓存同一套）。就地写的话断电或进程被杀会
// 留下半份 JSON，而半份 JSON 解析失败 = 这一首歌从此「没有评论」——用户看不出
// 是缓存坏了，只觉得这首歌没人评论。
//
// # 这一层不发请求
//
// `Save` 收下的评论是调用方给的（应用层那一层拉回来的）。这一层只管存和取。
package comments

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/service/diskcache"
)

// TTL 是一份评论缓存多久算过期。
//
// 一个小时：评论不会变天，但也不会永远不变——而缓存太旧的话用户看到的评论跟
// B 站上现在的差太多。
const TTL = time.Hour

// Entry 是一个对象的评论在盘上的那一份。
type Entry struct {
	// Type 是对象类型（`bili.CommentTypeVideo` / `bili.CommentTypeAudio`）。
	Type int64 `json:"type"`
	// Oid 是对象 id（视频是 avid，音频是 auid）。
	Oid int64 `json:"oid"`
	// Sort 是这一份是**哪一档排序**拉回来的（`bili.CommentSort` 的值）。
	//
	// 🔴 **它必须在键里**：同一个对象的两档排序给的是**两份不同的评论**
	//（「热门评论」和「最新评论」），而缓存的键只有 type + oid（理由见文件头）。
	// 不记这一栏的话，用户切到另一档排序时读出来的是**上一档的那一份**——
	// 界面上「切了档但内容没变」，而用户会以为那一档是坏的。
	//
	// ⚠️ **零值 = 「没记」**：旧缓存文件里没有这一栏，于是它对不上任何一档
	// ⇒ 当成没有缓存（重拉一遍）。**不为自己的老状态写兼容逻辑**
	// （AGENTS.md §5），而这一栏本来就该从落盘的第一天起就在。
	Sort int `json:"sort"`
	// FetchedAt 是这份缓存是什么时候拉回来的（Unix 秒）。
	//
	// ⚠️ **它不是过期判据**（见 `service/diskcache` 的文件头）：那个时刻不
	// 因为用户又看了而变。而用户天天在看的那一首的评论，不该在一小时后消失。
	//
	// ⚠️ **过期的判据是文件的修改时刻**（mtime）——每一次读到它都会把它推到
	// 当下。而这一栏记的是「我们什么时候拿到的」，那是**另一个事实**。
	FetchedAt int64 `json:"fetchedAt"`
	// NextOffset 是下一页的游标（**偏移量**，B 站给什么就是什么，不透明）。
	//
	// ⚠️ **它是字符串**：`cursor.pagination_reply.next_offset` 带 base64 字符
	// （`+` `/` `=`），而**改一个字符 B 站不报错**——它回的是第一页（实测）。
	//
	// ⚠️ **不是 `cursor.next`**：那一栏在「评论」档（mode=1）上带上去必挂
	// （B 站回 `-400 min_score … must be <= max_score …`）。
	NextOffset string `json:"nextOffset"`
	// IsEnd 是「到底了」。
	IsEnd bool `json:"isEnd"`
	// AllCount 是评论总数。
	AllCount int64 `json:"allCount"`
	// NoComments 记着「这个对象在，只是还没有人评论」。
	//
	// ⚠️ **它和「没有缓存」是两回事**：没有这一栏的话，第二次进这一首时缓存
	// 读回来是空的，界面上就分不清「还没人评论」和「正在问」。
	NoComments bool `json:"noComments"`
	// Comments 是这一份评论列表。
	Comments []bili.Comment `json:"comments"`
}

// Cache 是评论缓存。目录为空的那一份只读不写（测试和「没挂上」时用）。
type Cache struct {
	dir string
	// now 是时钟。nil 用 time.Now（给假时钟是为了让过期能被测）。
	now func() time.Time
}

// NewCache 建一个落在 dir 的缓存。**建目录**：不在的话第一次写会失败，
// 而失败的表现是「评论每次进来都要重新问一遍」。
func NewCache(dir string) *Cache {
	if dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	return &Cache{dir: dir, now: time.Now}
}

// WithClock 换时钟。给假时钟是为了让过期能被测（等一小时没法测）。
func (c *Cache) WithClock(now func() time.Time) *Cache {
	if now != nil {
		c.now = now
	}
	return c
}

// Dir 是缓存目录，空表示这个缓存不落盘。
func (c *Cache) Dir() string { return c.dir }

// Key 是这一份缓存在盘上的键：`<type>-<oid>`。
//
// ⚠️ **type 必须进去**（理由见文件头）。
func Key(typ, oid int64) string {
	return strconv.FormatInt(typ, 10) + "-" + strconv.FormatInt(oid, 10)
}

// Path 是 key 这一条在磁盘上的位置。空目录时是空串。
//
// ⚠️ 文件名就是 key 本身（`<type>-<oid>.json`）：这两样都是数字和横线，
// 在 Windows 上也是合法文件名——不像歌词缓存那个键（本地文件的完整路径）。
func (c *Cache) Path(key string) string {
	if c.dir == "" || key == "" {
		return ""
	}
	return filepath.Join(c.dir, key+".json")
}

// Load 读回这一个对象的评论缓存。没有、文件坏了、或者**过期了**，都只是
// 「没有」。
//
// ⚠️ **读不出来一律当成没有，绝不报错**：评论缓存坏掉的表现应该是「这一次
// 没有评论」，而不是应用开不起来。用户删掉缓存目录、清磁盘、换机器都会走到
// 这里。
func (c *Cache) Load(typ, oid int64) (Entry, bool) {
	path := c.Path(Key(typ, oid))
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
	// 🔴 **键要对得上**：文件名是键，而内容里也记着键。两者对不上说明这份文件
	// 不是这个对象的（手改过、或者别的程序写的）——照用的话，界面上会出现
	// 「这一首歌的评论」是另一首歌的。
	if e.Type != typ || e.Oid != oid {
		return Entry{}, false
	}
	// 🔴 **过期是滚动的**（2026-10-30）：判据是「**最后一次用到**」。
	//
	// ⚠️ **判据是文件的修改时刻**（见 `service/diskcache` 的文件头）——
	// 每次读到它都会把它推到当下。而用户天天在看的那一首的评论，不该在
	// 一小时后消失。
	if !diskcache.Fresh(path, c.now(), TTL) {
		return Entry{}, false
	}
	// 🔴 **记一次「用过」**：把文件的修改时刻推到当下——**只改元数据，
	// 不碰内容**。
	//
	// ⚠️ **不写回整份记录**：那会（1）把读路径变成写路径，
	// （2）把并发写进去的新内容盖成旧内容（丢更新）。
	diskcache.Touch(path, c.now())
	return e, true
}

// Save 把这一个对象的评论原子写下去。空目录时什么都不做（不是错误：没配盘
// 就是这一次的评论关掉就没了）。
func (c *Cache) Save(typ, oid int64, e Entry) error {
	path := c.Path(Key(typ, oid))
	if path == "" {
		return nil
	}
	e.Type, e.Oid = typ, oid
	if e.FetchedAt == 0 {
		e.FetchedAt = c.now().Unix()
	}
	return c.write(path, e)
}

// write 把一份记录原子写下去。
//
// 🔴 **写完把文件的修改时刻设成「取回来的时刻」**：过期的判据是 mtime
// （见 `diskcache` 的文件头），而磁盘上的 mtime 是**真实时间**——
// 于是用假时钟的测试会看到「刚写的文件已经过期了」。
//
// ⚠️ **这一步在生产上也是对的**：文件的修改时刻就该是「这一份是什么时候
// 拿到的」，而不是「写它的那一瞬间」（两者相差一次 fsync 的工夫，
// 而那正是「用户多久没用了」这件事的起点）。
func (c *Cache) write(path string, e Entry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := diskcache.WriteAtomic(path, b); err != nil {
		return err
	}
	diskcache.Touch(path, c.now())
	return nil
}

// ── bvid → avid 的索引 ─────────────────────────────────────────────────────
//
// 🔴 **为什么要有它**
//
// 视频那一路的 oid 是 **avid**，而队列里的条目只有 bvid——所以要用
// `bili.Client.ArchiveID` 换一次（一次网络往返）。**漏了这一步会静默拿到空
// 列表**：B 站把一个认不出来的 oid 判成「这个对象没有评论」。
//
// 而「同一首歌第二次进是瞬时的（不发请求）」这条验收要求**连这一趟往返也不发**
// ——所以换过的 avid 得记住。记在盘上是为了**重启之后**也记住：不然用户重启
// 应用、回到刚才那首歌，还是要等一次往返才看到评论。
//
// 文件名是 bvid 的哈希：bvid 是 B 站给的一串字符，当文件名之前先哈希一下，
// 免得里面出现路径分隔符（和歌词缓存那一条是同一条理由）。

// avidPath 是 bvid 那一项在盘上的位置。
func (c *Cache) avidPath(bvid string) string {
	if c.dir == "" || bvid == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(bvid))
	return filepath.Join(c.dir, "avid-"+hex.EncodeToString(sum[:16])+".json")
}

// avidEntry 是索引文件的内容。
type avidEntry struct {
	Bvid string `json:"bvid"`
	Avid int64  `json:"avid"`
}

// LoadAVID 读回这个 bvid 换过的 avid。没有就是没有。
func (c *Cache) LoadAVID(bvid string) (int64, bool) {
	path := c.avidPath(bvid)
	if path == "" {
		return 0, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	var e avidEntry
	if err := json.Unmarshal(b, &e); err != nil {
		return 0, false
	}
	// bvid 要对得上：文件名是哈希，而内容里也记着 bvid。对不上就是别人的。
	if e.Bvid != bvid || e.Avid <= 0 {
		return 0, false
	}
	return e.Avid, true
}

// SaveAVID 把 bvid → avid 这一条记下去。
func (c *Cache) SaveAVID(bvid string, avid int64) error {
	path := c.avidPath(bvid)
	if path == "" {
		return nil
	}
	if avid <= 0 {
		return nil
	}
	b, err := json.Marshal(avidEntry{Bvid: bvid, Avid: avid})
	if err != nil {
		return err
	}
	return diskcache.WriteAtomic(path, b)
}
