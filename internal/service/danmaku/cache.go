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
// 临时文件 → fsync → rename，和歌词缓存是同一套（见 `service/lyrics/cache.go`
// 的 `writeAtomic`）。就地写的话断电或进程被杀会留下半份 JSON，而半份 JSON
// 解析失败 = 这一集从此「没有弹幕」——用户看不出是缓存坏了，只觉得那集没弹幕。
package danmaku

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// TTL 是缓存的有效期：7 天。
//
// 7 天是「弹幕这东西基本不变，但也不至于永远不更新」之间的那个数：B 站的一集
// 弹幕在一周之内不会变出新的来，而一周之后重新拉一次也就一次往返。
const TTL = 7 * 24 * time.Hour

// Entry 是缓存里一个 cid 的一份弹幕。
type Entry struct {
	// Items 是那一份弹幕本身（`bili.Danmaku` 原样带出来）。
	//
	// ⚠️ **JSON 里的键名是 Go 字段名**（`TimeMS` / `Mode` / `FontSize` /
	// `Color` / `Text` / `ID`）——`bili.Danmaku` 没写 json tag，而这一层不去动
	// 它的定义（那是服务层的事）。这份文件是**自己的缓存**，不是要和谁交换的
	// 格式。
	Items []bili.Danmaku `json:"items"`
	// FetchedAt 是**取回来的时刻**。过期就靠它算（见 `Expired`）。
	//
	// ⚠️ **它不是「这一份是什么时候生成的」**：B 站给的历史弹幕就是这个时间，
	// 而我们要的是「我们什么时候拿到的」——只有后者能判缓存是不是还新鲜。
	FetchedAt time.Time `json:"fetchedAt"`
}

// Expired 报这一份是不是过期了。
//
// ⚠️ **零值（没取过时刻）算过期**：那份文件要么是手写的，要么是更早的版本
// 写的，而「不知道什么时候取的」就该当成不新鲜——宁可多拉一次，也不要拿一份
// 来路不明的东西当新的。
func (e Entry) Expired(now time.Time) bool {
	if e.FetchedAt.IsZero() {
		return true
	}
	return now.Sub(e.FetchedAt) > TTL
}

// Cache 是弹幕缓存。目录为空的那一份只读不写（测试和「没挂上」时用）。
type Cache struct {
	dir string
}

// NewCache 建一个落在 dir 的缓存。**建目录**：不在的话第一次写会失败，
// 而失败的表现是「每次进这一集都要重新拉一遍」。
func NewCache(dir string) *Cache {
	if dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	return &Cache{dir: dir}
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
	if e.Expired(time.Now()) {
		// 过期了就当没有（不删文件：删它是一次额外的写，而下一次 `Save` 会
		// 把它盖掉）。
		return Entry{}, false
	}
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
		// 没写时刻的那一份**不许落盘**：过期判据靠的就是它，而零值会被当成
		// 「一拉出来就过期」。要存就得把时刻写上。
		e.FetchedAt = time.Now()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return writeAtomic(path, b)
}

// writeAtomic 临时文件 → fsync → rename。任何一步失败都把临时文件删掉。
//
// ⚠️ 临时文件建在**目标同一个目录**里：rename 只在同一文件系统内是原子的，
// 临时文件放到系统临时目录去的话，跨文件系统时 rename 会失败——而那正是
// 「用户把数据目录放在另一个盘上」的情形。
func writeAtomic(path string, b []byte) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".danmaku-*.tmp")
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
	// CreateTemp 建出来的是 0600。弹幕里没有隐私，给成普通用户文件同样的
	// 权限，不然用户从访达里看一眼弹幕缓存目录会以为坏了。
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
