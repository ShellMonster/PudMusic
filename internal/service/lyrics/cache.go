// 歌词缓存：一首歌一条，落在磁盘上。
//
// ## 为什么一首歌一个文件
//
// Biu 把歌词缓存放在 electron-store 的 `lyrics-cache.json` 里，**整个 store 一份**
// （`shared/store` 的 `StoreNameMap.LyricsCache`），键是 `${bvid}-${cid}`。这里改成
// **一首歌一个文件**：写一首不会碰另外几十首，而用户拖一次偏移就写一次盘（500ms
// 防抖），一份大文件意味着每 500ms 重写整个歌单的歌词。
//
// JSON 的**键名照抄**（`lyrics` / `tLyrics` / `offset` / `fontSize`），为的是以后
// 「从 Biu 首次导入」那一张能直接搬——那是 `PLAN.md`「持久化与迁移」里点名的东西，
// 搬的时候按字段名读，不用再对一遍。
//
// ## 写盘是原子的
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
	"runtime"
)

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
	// ⚠️ **它不是 Biu 那边那几个键之一**：Biu 的歌词缓存只有
	// `lyrics` / `tLyrics` / `offset` / `fontSize` 四个（见 `biuimport.go`），
	// 而这一栏是我们自己加的——它要回答的是「界面上那句话说的是哪一种」，
	// 而那一句在 Biu 那边不存在（上游的歌词只有一种来路）。
	//
	// ⚠️ **空串是合法的**：从 Biu 导入的老记录没有这一栏，而**不知道来路**
	// 不该被当成「搜索来的」——界面那句话会因此说错。
	Origin string `json:"origin,omitempty"`
}

// Cache 是歌词缓存。目录为空的那一份只读不写（测试和「没挂上」时用）。
type Cache struct {
	dir string
}

// NewCache 建一个落在 dir 的缓存。**建目录**：不在的话第一次写会失败，
// 而失败的表现是「用户调了偏移，下次打开又变回去了」。
func NewCache(dir string) *Cache {
	if dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	return &Cache{dir: dir}
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
	return e, true
}

// Save 把 key 这一条原子写下去。空目录时什么都不做（不是错误：没配盘就是
// 内存里的默认值，关掉就没了）。
func (c *Cache) Save(key string, e Entry) error {
	path := c.Path(key)
	if path == "" {
		return nil
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
	tmp, err := os.CreateTemp(dir, ".lyrics-*.tmp")
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
	// CreateTemp 建出来的是 0600。歌词里没有隐私，给成普通用户文件同样的权限，
	// 不然用户从访达里看一眼歌词缓存目录会以为坏了。
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
