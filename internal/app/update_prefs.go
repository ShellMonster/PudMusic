// 更新器自己那一份落盘：上次什么时候查的、上次查到的版本是哪个。
//
// ## 为什么不进 `app.Settings`
//
// 两层理由，和搜索历史（`internal/ui/searchhistory.go`）那份一样：
//
//  1. **Biu 自己也是单独一个 store**（electron-updater 把偏好放在
//     `updater.json`，`app-settings.json` 里根本没有这个键），照抄就是这一份。
//  2. 顺带避开了往 `Settings` 里加键——那边 `settings_test.go` 的 `biuKeys` 和
//     `ourOwnKeys` 是两张封闭的表，加一个键就得同时改那两张，而那不是这半张卡片
//     的活儿。
//
// ## 这里**只有**跨启动要记住的两样，一样都不多
//
// 在下的那一次**一个字都不存**。这是「下载中退出应用，数据不损坏」（SPEC 118）
// 在这一层的样子：文件里没有任何「正在下 / 下到一半」的字段，所以一次中途退出
// 之后读回来的东西必然是自洽的——「上次查到了 1.3.0，但还没装上」。反过来写，
// 就得额外去定义「那一份半截下载算不算数」，而它算不算数对谁都没好处。
//
// 文件名**故意不叫 `updater.json`**：框架那个插件（`plugins/updater`）自己就叫
// 那一份，而它的形状和我们这份不一样。两份同名不同形的东西躺在同一个目录里，
// 迟早有人拿错。

package app

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// updatePrefsFileName 是更新这一份文件的名字。
	updatePrefsFileName = "app-update.json"

	// updatePrefsSchema 是这份文件的格式版本。改它的**结构**（加键、删键、改
	// 类型）就涨它，并往 updatePrefsMigrations 里加一条 v→v+1 的路。
	updatePrefsSchema = 1
)

// updatePrefsMigrations 把 v 升到 v+1。现在没有要迁的——留着是为了以后加键时有
// 一条明着的地方可写，而不是「到时候再说」。
var updatePrefsMigrations = map[int]func(*updatePrefsFile){}

// updatePrefsFile 是落盘的整体。
type updatePrefsFile struct {
	SchemaVersion int `json:"schemaVersion"`

	// LastCheck 是上一次**成功**查完的时刻。查失败不记：记了它，界面上就会说
	// 「上次检查：刚刚」而其实那一刻什么都没查出来。
	LastCheck time.Time `json:"lastCheck,omitzero"`
	// KnownVersion 是上一次查到的最新版本。
	//
	// 存它是为了**离线那次**也看得到有新版本：Biu 那边得等 electron-updater
	// 联网查完才知道，启动时离线的话设置页上永远是「检查更新」和当前版本。
	KnownVersion string `json:"knownVersion,omitzero"`
}

// updatePrefs 是更新器这一份文件。
//
// **零值可用**（path 为空就是只在内存里）：拿不到应用数据目录时更新照常能用，
// 只是关掉之后不记得上次查过。锁在 mu 后面——`last` 由界面每帧读，`put` 由后台
// 那一次检查写。
type updatePrefs struct {
	path string

	mu   sync.Mutex
	file updatePrefsFile
	// saveErr 是最近一次落盘失败的原因。文件写不出去不往界面上弹（更新不是
	// 那种「不存住就等于坏掉」的东西），但排障的人得能从日志里看见。
	saveErr error
}

// UpdatePrefsPath 报这份文件在用户数据目录里的位置。dir 为空时返回空串
// （= 只在内存里）。
//
// ⚠️ dir 为空**不能**拿 `filepath.Join("", "app-update.json")` 凑：那是当前工作
// 目录下的一个文件。用户从 Finder 双击启动时工作目录是 `/`，于是那份文件写到
// `/app-update.json`——权限不够，写不进去；写得进去的话，下次从终端启动的应用
// 就读到了另一份。两种都很难查。
func UpdatePrefsPath(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, updatePrefsFileName)
}

// newUpdatePrefs 建一份，path 为空就是只在内存里。
func newUpdatePrefs(path string) *updatePrefs {
	return &updatePrefs{path: path}
}

// load 读盘，读不到就用零值。**在挂上的时候调一次**（那是开窗口之前的那一刻）。
//
// 坏文件、别的版本写的文件、读不出来——三种都当没有：这份东西丢了最坏的结果
// 是下一次检查多花一个来回，而为一个几百字节的缓存去拒绝启动，是本末倒置。
// （和 `LoadSettings` 那边的处理不同是有道理的：那边丢的是**用户设过的**东西，
// 有一个键就少一个，所以要备份、要报错、要只读保护。）
func (p *updatePrefs) load() {
	if p.path == "" {
		return
	}
	b, err := os.ReadFile(p.path)
	if err != nil {
		return
	}
	var f updatePrefsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return
	}
	if f.SchemaVersion > updatePrefsSchema {
		// 别的（更新的）版本写的。一个字都不写回去，理由同 `LoadSettings`。
		p.mu.Lock()
		p.path = ""
		p.mu.Unlock()
		return
	}
	for v := f.SchemaVersion; v < updatePrefsSchema; v++ {
		if m, ok := updatePrefsMigrations[v]; ok {
			m(&f)
		}
	}
	p.mu.Lock()
	p.file = f
	p.mu.Unlock()
}

// last 返回现在记着的那一份。
func (p *updatePrefs) last() updatePrefsFile {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.file
}

// put 记下新的一份并落盘。
//
// ⚠️ 调用它的时候**不能拿着 `updater.mu`**：这一句是一次真实的文件写，
// 而 `UpdateSnapshot` 每帧要拿 `updater.mu`。虽然检查最多一小时一次、用户主动
// 那一次也在后台 goroutine 上（不占主线程），但「一次几毫秒的写卡住界面一帧」
// 这种毛病不值得赌，所以这里的规矩和 settings.go 那份一样：**落盘写在锁外面**。
//
// 记的是**整份**，不是逐键合并：这份文件只有两个字段，而它们总是**同时**在
// 一次检查里被定下来（查成了就同时定下时刻和版本），合并反而会出现「时刻是
// 上次的、版本是这次的」这种从没有人想过的组合。
func (p *updatePrefs) put(f updatePrefsFile) {
	f.SchemaVersion = updatePrefsSchema

	p.mu.Lock()
	path, prev := p.path, p.saveErr
	p.file = f
	p.mu.Unlock()

	if path == "" {
		return
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err == nil {
		// 用这一包**通用**的那一个原子写（`writeFileAtomic`，#81 持久化迁移把它从
		// 只服务设置的 `writeSettingsAtomic` 改名成了通用名，那一句注释写的就是
		// 「服务这一包里的任何一份文件」）。自己再写一份临时文件 + rename 的话，
		// 两条路迟早有一处漏掉 fsync 或者权限那两句。
		err = writeFileAtomic(path, b)
	}
	if err != nil && prev == nil {
		// 只记第一次：写不出去的时候多半是目录没了或者磁盘满了，每小时记一次
		// 同样的错只是把日志淹掉。
		p.mu.Lock()
		p.saveErr = err
		p.mu.Unlock()
		log.Printf("biu: 更新记录 %s 存不住：%v", path, err)
	}
}
