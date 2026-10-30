// Package danmaku 是弹幕的**缓存**那一层：一份弹幕落在磁盘上，7 天之内再进是
// 瞬时的。
//
// # 为什么单独一个包
//
// 弹幕**从 B 站拿**（`service/bili` 那一层的 `Client.Danmaku`），而缓存**落在本机
// 磁盘上**。两者混在一起的话，「存」和「取」就分不开了——而这一层的规矩恰恰
// 是：**它自己不发任何请求**（照 `service/lyrics` 那一层的规矩）。
//
// ## 这一层不发请求
//
// `Save` 收下的那一份是调用方给的（拿它的那一趟在应用层，见
// `internal/app/danmaku.go`），`Load` 只管读盘。拿不到就什么都不存，界面上照实
// 显示「没有弹幕」。
//
// # 一集一个文件
//
// 文件名是 `<bvid>-<cid>.json`：弹幕是**按分 P**（cid）组织的，而同一个投稿的
// 每一集弹幕都不一样，所以文件名里两个都要有（照 `service/lyrics` 那一层
// 「一个对象一个文件」的规矩）。
//
// ## 写盘是原子的
//
// 临时文件 → fsync → rename，和歌词缓存是同一套（见 `service/diskcache` 的
// `WriteAtomic`——五个缓存包共用那一份）。就地写的话断电或进程被杀会留下半份
// JSON，而半份 JSON 解析失败 = 这一集从此「没有弹幕」——用户看不出是缓存坏了，
// 只觉得那集没弹幕。
package danmaku

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/service/diskcache"
)

// TTL 是缓存的有效期：7 天。
//
// 7 天是「弹幕这东西基本不变，但也不至于永远不更新」之间的那个数：B 站的一集
// 弹幕在一周之内不会变出新的来，而一周之后重新拉一次也就一次往返。
//
// 🔴 **过期是滚动的**（2026-10-30）：判据是「最后一次用到」，不是「取回来的
// 时刻」——用户天天在听的那一集，弹幕不该在第七天消失（见 `service/diskcache`）。
const TTL = diskcache.TTL

// Entry 是缓存里一个 cid 的一份弹幕。
type Entry struct {
	// Items 是那一份弹幕本身（`bili.Danmaku` 原样带出来）。
	//
	// ⚠️ **JSON 里的键名是 Go 字段名**（`TimeMS` / `Mode` / `FontSize` /
	// `Color` / `Text` / `ID`）——`bili.Danmaku` 没写 json tag，而这一层不去动
	// 它的定义（那是服务层的事）。这份文件是**自己的缓存**，不是要和谁交换的
	// 格式。
	Items []bili.Danmaku `json:"items"`
	// FetchedAt 是**取回来的时刻**。
	//
	// ⚠️ **它不是过期判据**（见 `diskcache` 的文件头）：那个时刻不因为用户
	// 又听了而变。而用户天天在听的那一集，弹幕不该在第七天消失。
	//
	// ⚠️ **过期的判据是文件的修改时刻**（mtime）——每一次读到它都会把它推到
	// 当下。而这一栏记的是「我们什么时候拿到的」，那是**另一个事实**。
	FetchedAt time.Time `json:"fetchedAt"`
}

// Cache 是弹幕缓存。目录为空的那一份只读不写（测试和「没挂上」时用）。
type Cache struct {
	dir string
	// now 是时钟。nil 用 time.Now（给假时钟是为了让过期能被测）。
	now func() time.Time
}

// NewCache 建一个落在 dir 的缓存。**建目录**：不在的话第一次写会失败，
// 而失败的表现是「每次进这一集都要重新拉一遍」。
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

// Path 是 bvid 的第 cid 集在磁盘上的位置。空目录、或身份不全时是空串。
//
// ⚠️ **文件名是 `<bvid>-<cid>.json`，不是哈希**（和歌词缓存不一样）：这一层的
// 键本来就是两个 B 站的编号，里面没有路径、没有 `:`，而人要是想手工删掉一份
// 缓存，得能一眼看出那个文件是哪一集的。
//
// ⚠️ **身份不全或者不像是编号时给空串**（不落盘）：文件名是要被拼出来的，
// 而 bvid / cid 拼不出来的场合（本地文件、音乐库条目）是「压根没有弹幕」，
// 不该在缓存目录里造出一个怪名字的文件。
func (c *Cache) Path(bvid, cid string) string {
	if c.dir == "" || !validID(bvid) || !validID(cid) {
		return ""
	}
	return filepath.Join(c.dir, bvid+"-"+cid+".json")
}

// validID 报 s 是不是一个能当文件名一部分的编号。
//
// B 站的 bvid 是 `BV` 加一串字母数字，cid 是纯数字——但这一层不去查那件事
// （查了就得认识 B 站的编号规则，而那规则会变）。它只挡**会拼坏文件名**的
// 那些：路径分隔符、`.` / `..`、控制字符。
//
// ⚠️ **不用「只放行字母数字」那一条**：那会让「拿一个不是编号的东西来问」
// 变成「静悄悄地不落盘」，而调用方看到的只是「缓存里没有」——分不清是缓存
// 过期了还是身份压根不对。
func validID(s string) bool {
	if s == "" {
		return false
	}
	if s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		switch r {
		case '/', '\\', 0:
			return false
		}
		if r < 0x20 || r == 0x7f {
			// 控制字符：它们进文件名之后各种工具都难受。
			return false
		}
	}
	return true
}

// Load 读 bvid 的第 cid 集那一份。没有这一条、文件坏了、**过期了**、目录都没建，
// 都只是「没有」。
//
// ⚠️ **读不出来一律当成没有，绝不报错**：弹幕缓存坏掉的表现应该是「这一集
// 暂时没有弹幕」，而不是应用开不起来。用户删掉缓存目录、清磁盘、换机器都会
// 走到这里。
//
// 🔴 **命中之后要记一次「用过」**（2026-10-30）：过期是**滚动**的——用户今天
// 看了这一集，它今天就重新开始算 7 天。而「用过」这件事只有这一层知道
// （调用方可能读完之后就没再管它了）。
//
// ⚠️ **写回去失败不算错**：那只是这一次没记上「用过」，而下一次读还会再记。
// 缓存不该因为写不进去就拒绝服务。
func (c *Cache) Load(bvid, cid string) (Entry, bool) {
	path := c.Path(bvid, cid)
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
	// 🔴 **过期判据是文件的修改时刻**（见 `diskcache` 的文件头）：那才是
	// 「最后一次用到它」的时刻。
	//
	// ⚠️ **不用记录里那一栏**（`FetchedAt`）：那个时刻不因为用户又听了而变，
	// 而用户天天在听的那一集，弹幕不该在第七天消失。
	if !diskcache.Fresh(path, now, TTL) {
		// 过期了就当没有（不删文件：删它是一次额外的写，而下一次 `Save` 会
		// 把它盖掉）。
		return Entry{}, false
	}
	// 🔴 **记一次「用过」**：把文件的修改时刻推到当下——**只改元数据，
	// 不碰内容**。
	//
	// ⚠️ **不写回整份记录**：那会（1）把读路径变成写路径，
	// （2）把并发写进去的新内容盖成旧内容（丢更新）。
	diskcache.Touch(path, now)
	return e, true
}

// Save 把 bvid 的第 cid 集那一份原子写下去。空目录时什么都不做（不是错误：
// 没配盘就是关掉就没了）。
func (c *Cache) Save(bvid, cid string, e Entry) error {
	path := c.Path(bvid, cid)
	if path == "" {
		return nil
	}
	if e.FetchedAt.IsZero() {
		// 没写时刻的那一份**不许落盘**：它记的是「我们什么时候拿到的」，
		// 而零值会说不出这件事。要存就得把时刻写上。
		e.FetchedAt = c.now()
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
