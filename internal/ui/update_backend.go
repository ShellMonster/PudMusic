// 更新器要框架做的那几件事的真实现：把 `mygo.Updater` 装成
// `app.UpdateBackend`。
//
// ## 为什么放在界面层
//
// `internal/app` **不 import mygo**（那是这一层的一条硬规矩，见 app.go 的包注释：
// 「它不认识界面」）。而更新器那一半非框架不可：`mygo.Updater` 是 MyGo 的 API，
// 版本号、公证票据、增量补丁、整个 `.app` bundle 的替换全在里面。
//
// 所以真实现住在这里，`internal/app` 只认那个接口。这一层依赖界面层的方向是
// 正的（界面层本来就是最上层），而反过来会让整个业务状态层都依赖一个 GUI
// 框架——那才是真正难拆的方向。
//
// ## 这一层薄到什么程度
//
// 它只有四个方法，每一个都是**原样转发**，一个字节的逻辑都没有。签名校验、
// 增量补丁、失败回全量、原地替换 bundle——那些全在框架里（`mygo/updater.go`），
// 这一层既不重写也不「加固」：重写一遍的结果是两条更新链，而只有一条被测过。
//
// 真实现为什么值得单独一个文件而不是塞进 `internal/app/update.go`：因为**方向
// 相反**。那边定义接口，那边不该知道谁来实现它。

package ui

import (
	"context"
	"log"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo"
)

// mygoUpdaterBackend 是 `mygo.Updater` 的那一层实现。
type mygoUpdaterBackend struct{}

// 编译期钉住它真的实现了那个接口：接口和方法一旦对不上，这里是**唯一**一个会
// 立刻炸掉的地方，而不是等到界面上「按了没反应」才发现。
var _ app.UpdateBackend = mygoUpdaterBackend{}

// Enabled 报这个应用能不能给自己更新。
//
// 框架那一份（`mygo/updater.go` 的 `Enabled`）已经把三件事压进了一枚 bool：
// 没打包（`packageUpdateFeed` / `packageUpdateKey` 是 `mygo build` 用 `-ldflags
// -X` 塞的）、开发构建（`IsDev()`）、以及安装位置写不写得动（它在应用旁边
// 建一个临时目录试一下）。
//
// ⚠️ **别在每帧调它**：那一次「建临时目录 + 删掉」是真实的磁盘 IO，每帧 60 次
// 是白烧，而且画面会跟着它的快慢抖。所以 `app.MountUpdates` 在**开窗口之前**
// 问一次就够（那里本来就该做阻塞 IO）。
func (mygoUpdaterBackend) Enabled() bool { return mygo.Updater.Enabled() }

// Version 是正在跑的版本号。开发构建里可能是空串（`mygo/app.go` 的 `Version`
// 在没有打包信息时返回空），界面那上面显示成「未知」。
func (mygoUpdaterBackend) Version() string { return mygo.App.Version() }

// Check 要一个新版本，已经是最新时返回 (nil, nil)。
func (mygoUpdaterBackend) Check(ctx context.Context) (*app.UpdateRelease, error) {
	up, err := mygo.Updater.Check(ctx)
	if err != nil || up == nil {
		return nil, err
	}
	// ⚠️ `Date` 可能是零值（更新源没给日期，框架 `update.Manifest.Date` 解析失败
	// 时就是零值）。零值**原样传过去**：界面判的是 `Released.IsZero()`，所以
	// 「没给日期」和「日期是公元 1 年」分得开——而在这里把它换成别的任何一个
	// 日期都是编造。
	return &app.UpdateRelease{Version: up.Version, Notes: up.Notes, Date: up.Date}, nil
}

// Install 下 want.Version 那一版，验签名，把它装到磁盘上。
//
// ⚠️ **这里要去问一次更新源**，而不是拿一个「已经查好的」来装：框架那个能装的
// `*mygo.Update` 带着清单和密钥（私有字段），只活过一次 `Check` 调用。所以
// `app` 那一层传的 `want` 只有一个版本号，装的那一份是这一次查出来的。
//
// ⚠️ **签名校验在框架里**（`download` 里的 `update.Verify`）：大小对不上或者
// 签名不对，`Install` 返回错，而它返回错之后**磁盘上那一份一个字都没动**（换
// 目录在 `installUpdate` 的最后一步 `swap`，在那之前全程只写应用旁边那个临时
// 目录）。所以「装到一半被杀」不会留下一个装坏的自己。
//
// ⚠️ **进度回调会让增量回退显形**：框架先下增量包（`total` = 增量包大小），
// 失败改下全量时 `total` 变成整个安装包。`app` 那一层认得出这个变化并在界面上
// 说明白——见 `internal/app/update.go` 文件头 §增量。
func (mygoUpdaterBackend) Install(ctx context.Context, want app.UpdateRelease, progress func(downloaded, total int64)) error {
	up, err := mygo.Updater.Check(ctx)
	if err != nil {
		return err
	}
	// 两次查之间中间发了新的一版：装一个已经不是最新的，用户白下几十兆、装完还得
	// 再更新一次。回上去让 `app` 那一层重查一次。
	if up == nil || up.Version != want.Version {
		return app.ErrUpdateSuperseded
	}
	return up.Install(ctx, progress)
}

// Relaunch 退出并起一个进程进新版本。
//
// 框架那一份走的是完整的退出流程（`OnBeforeQuit` → 关窗口 → 起新的），所以
// 退出前挂着的那些收尾都会照跑：设置落盘、下载队列落盘、文件监听关掉。
func (mygoUpdaterBackend) Relaunch() { mygo.App.Relaunch() }

// MountUpdates 把更新挂到 a 上：**必须在开窗口之前调**。
//
// 为什么挂这么早：`mygoUpdaterBackend.Enabled()` 在框架里是一次真实的磁盘 IO
// （在应用旁边建一个临时目录试写），而它要垫在界面第一帧之前——第一帧就摆着
// 「这个版本不能自动更新」然后过两秒变回能，是最难解释的那种闪烁。
//
// 拿不到应用数据目录不算错误：记一条日志，更新照常能用，只是关掉之后不记得
// 上次查过（和 `MountSettings` 同一个取舍）。
func MountUpdates(a *app.App) error {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("biu: 拿不到应用数据目录，这次不记得上次查过更新：%v", err)
	}
	a.MountUpdates(mygoUpdaterBackend{}, app.UpdatePrefsPath(dir))
	return nil
}
