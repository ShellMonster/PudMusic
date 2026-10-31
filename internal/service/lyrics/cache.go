// 歌词缓存：一首歌一条，落在磁盘上。
//
// 歌词缓存独立成文件，避免写入设置或大型共享缓存。缓存字段保持稳定，便于当前版本读写。
//
// 临时文件 → fsync → rename，和设置那边是同一套（见 `app.writeSettingsAtomic`）。
// 就地写的话断电或进程被杀会留下半份 JSON，而半份 JSON 解析失败 = 这首歌从此
// 「没有歌词」——用户看不出是缓存坏了，只觉得这首歌没歌词。
//
// ## 这一层不发请求
//
// `Save` 收下的歌词文本是调用方给的（搜索那一摊在 search.go，两处分开是这个原因）。
// 调用方拿不到就什么都不存，界面上照实显示「暂无歌词」。
package lyrics

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/diskcache"
)

// TTL 是一份歌词缓存多久算过期：7 天（和弹幕缓存同一档）。
//
// 🔴 **它以前根本不存在**（2026-10-30）：歌词缓存只增不减，于是用户数据目录里
// 的歌词文件只涨不落。而现在有了它，判据是**滚动**的——用户还在听的那一份
// 不算过期。
//
// ⚠️ **过期的只是「歌词文本」**：用户自己调过的偏移和字号是**用户数据**，
// 过期时也得留着（见 `Load` 的注释）。
const TTL = diskcache.TTL

// Entry 是缓存里一首歌的一条。
//
// OffsetMS 和 FontSize 是**用户调过的值**，零值表示「没调过，用默认的」。
// 所以读回来时 FontSize 为 0 不是「字号 0」，是「没调过」——界面那边把它当
// 默认字号（见 app 的 ClampLyricsFontSize）。
type Entry struct {
	// Lyrics 是原文的 LRC 文本。
	Lyrics string `json:"lyrics,omitempty"`
	// TLyrics 是译文的 LRC 文本，空串表示这份歌词没有翻译。
	TLyrics string `json:"tLyrics,omitempty"`
	// OffsetMS 是歌词偏移（毫秒），正数 = 歌词整体往后挪。
	OffsetMS int `json:"offset,omitempty"`
	// FontSize 是字号（像素）。
	FontSize float32 `json:"fontSize,omitempty"`
	// Origin 是这份歌词的**来路**：`bili`（B 站自动带出来的）/ `search`
	// （用户在歌词搜索里找来的）。空串 = 不知道。
	//
	// 这一栏是我们自己加的，用来说明界面上展示的歌词来源。
	Origin string `json:"origin,omitempty"`
}

// Cache 是歌词缓存。目录为空的那一份只读不写（测试和「没挂上」时用）。
type Cache struct {
	dir string
	// now 是时钟。nil 用 time.Now（给假时钟是为了让过期能被测）。
	now func() time.Time
}

// NewCache 建一个落在 dir 的缓存。**建目录**：不在的话第一次写会失败，
// 而失败的表现是「用户调了偏移，下次打开又变回去了」。
func NewCache(dir string) *Cache {
	if dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	return &Cache{dir: dir, now: time.Now}
}

// WithClock 换时钟。给假时钟是为了让过期能被测（等 7 天没法测）。
func (c *Cache) WithClock(now func() time.Time) *Cache {
	if now != nil {
		c.now = now
	}
	return c
}

// Dir 是缓存目录，空表示这个缓存不落盘。
func (c *Cache) Dir() string { return c.dir }

// Path 是 key 这一条在磁盘上的位置。空目录时是空串。
//
// ⚠️ 文件名是 key 的**哈希**，不是 key 本身：本地那一条的键是用户目录下的
// 完整路径（`local-/Users/…/歌.mp3`），直接当文件名会造出一层一层的目录，
// 而键里带 `:` `/` 在 Windows 上根本不是合法文件名。
func (c *Cache) Path(key string) string {
	if c.dir == "" || key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:16])+".json")
}

// Load 读 key 这一条。没有这一条、文件坏了、目录都没建，都只是「没有」。
//
// ⚠️ **读不出来一律当成没有，绝不报错**：歌词缓存坏掉的表现应该是「这首歌
// 暂时没有歌词」，而不是应用开不起来。用户删掉缓存目录、清磁盘、换机器都
// 会走到这里。
//
// 🔴 **ok 的意思是「盘上有这一份记录」，不是「这一份有歌词」**（2026-10-30）：
// 一份**过期**的记录，它的歌词文本已经被清掉了（见下面那段），但**用户调过
// 的偏移和字号还在**——而那两样是**用户数据**，调用方要拿得到它们。
//
// ⇒ 所以过期的时候返回的是「**记录在、歌词没了**」：ok = true，而
// `Lyrics` / `TLyrics` 是空串。调用方判断「有没有歌词」要看 `Lyrics`，
// 不是看 ok。
func (c *Cache) Load(key string) (Entry, bool) {
	path := c.Path(key)
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
	now := c.now()
	// 🔴 **过期是滚动的**（2026-10-30）：判据是「**最后一次用到**」。
	//
	// ⚠️ **判据是文件的修改时刻**（见 `diskcache` 的文件头）——每次读到它
	// 都会把它推到当下。而用户天天在听的那一首，不该在第七天消失。
	if !diskcache.Fresh(path, now, TTL) {
		// 🔴 **过期时把「歌词文本」清掉，但把用户自己的值留着**（2026-10-30
		// 的对抗性审查挖出来的 P0）。
		//
		// ⚠️ **为什么不能整份丢掉**：那份缓存里除了歌词文本，还有**用户调过的
		// 偏移和字号**。而「歌词文本过期了要重新拉一份」是**另一件事**——
		// 顺手把用户的数据清零，用户看到的现象是「某天回来发现我调过的歌词
		// 偏移不见了，字号也变小了」，而且**永久丢失**（缓存里唯一一份）。
		//
		// ⚠️ **老缓存文件不需要迁移**：它们没有时间戳这一栏，而它们的 mtime
		// 就是当初写下去的时刻——判据按 mtime 走，老文件的行为和当初一致。
		//
		// 🔴 **返回的是「记录在、歌词没了」**（ok = true）：调用方要拿得到
		// **用户调过的偏移和字号**——那两样是用户数据，不该跟着歌词文本一起
		// 过期（见上面那段）。
		e.Lyrics = ""
		e.TLyrics = ""
		return e, true
	}
	// 🔴 **记一次「用过」**：把文件的修改时刻推到当下——**只改元数据，
	// 不碰内容**。
	//
	// ⚠️ **不写回整份记录**：那会（1）把读路径变成写路径，
	// （2）把并发写进去的新内容盖成旧内容（丢更新）。
	diskcache.Touch(path, now)
	return e, true
}

// Save 把 key 这一条原子写下去。空目录时什么都不做（不是错误：没配盘就是
// 内存里的默认值，关掉就没了）。
func (c *Cache) Save(key string, e Entry) error {
	path := c.Path(key)
	if path == "" {
		return nil
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
