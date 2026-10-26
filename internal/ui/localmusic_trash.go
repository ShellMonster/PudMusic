// 本地音乐的「移到系统废纸篓」那一份零件。
//
// 单独一个文件，因为它 import MyGo 而 `internal/app` 不 import（那一层
// 不认识界面）。而且它**必须**住在界面层：`mygo.Shell.TrashItem` 在应用
// 还没 ready 的时候调会 panic（框架的 `needsApp`，`mygo/app.go:128`）。
//
// # 为什么这一层只做「搬」，不做「删」
//
// 框架那一侧的语义是现成的、也是这一票要的（`mygo/modules.go:44`）：
//
//	「It fails, and leaves the item where it is, when the item cannot go
//	to the trash and would be deleted permanently instead: on Windows one
//	too big for the recycle bin, or on a drive without one, as network
//	shares are.」
//
// ⚠️ **「leaves the item where it is」是这一层全部的立足点**：失败时文件
// 还在原处，而这一层**不做任何补救性的删除**。框架自己在 Windows 那边
// 装了一个 progress sink，把「本来会被永久删除」的那一次**拒掉**
// （`mygo/internal/windows/trash.go`）——宁可失败也不让文件被悄悄删掉。
// 我们这一层照它那份语义做，**不**加一条「回收站坏了就 os.Remove」的
// 兜底：加了的话，用户在界面上看到的还是「已移到废纸篓」，而文件已经没了
// （症状是静默的——那正是这一票要消灭的那种错）。
//
// # 三平台的原话不一样
//
// macOS 是 `NSFileManager` 的 `trashItemAtURL:` 报出来的
// `localizedDescription`；Windows 是 `IFileOperation` 的 HRESULT 翻出来的
// 一句 `mygo: cannot move … to the recycle bin: …`；Linux 是 GIO 的
// `GError`。这一层**不**去猜那些话是什么意思——那是
// `service/localmusic.ClassifyTrashError` 的活（归类只在一个地方）。
//
// ⚠️ **那一层认的是结构化的片段**（HRESULT 码、框架写死的那一句），
// **不是**整句里的关键词——Windows 那边每一句失败的话都带同一截
// 「to the recycle bin」，拿它当判据会把权限失败 / 已存在 / 文件不存在
// 全归成同一档（那正是评审打回来的那一种）。

package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/service/localmusic"
	"github.com/egoist/mygo"
)

// mygoTrash 是 `localmusic.Trash` 的框架实现。
type mygoTrash struct{}

// TrashItem 把 path 移到系统废纸篓。实现了 `localmusic.Trash`。
//
// ⚠️ **失败时文件一定还在原处**：这是框架那一侧的契约（见文件头），
// 而这一层**不做任何补救**。这一层只把系统的原话往上递。
func (mygoTrash) TrashItem(path string) error {
	// 框架的 `Shell.TrashItem` 自己会：
	//   - 把相对路径转成绝对路径（`filepath.Abs`），
	//   - 把请求 post 到主线程再等结果（`onMainValue`），
	//   - 在「进不了回收站」时**失败并把东西留在原地**。
	//
	// ⚠️ **它在应用还没 ready 的时候调会 panic**（`needsApp`）——而这一层
	// 是在**用户点了删除之后**才被调到的，那时应用早就起来了。
	if err := mygo.Shell.TrashItem(path); err != nil {
		return localmusic.ClassifyTrashError(err, path)
	}
	return nil
}

// 编译期确认这一摊确实实现了 localmusic 那个接口。
var _ localmusic.Trash = mygoTrash{}
