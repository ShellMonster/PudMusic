// 收藏夹管理那几个弹窗的接缝：「弹文件框选一张图」和「读那个文件」。
//
// 单独一个文件是因为它只有接缝、没有界面——界面在 folderedit.go /
// covermodal.go / folderselect.go 里。

package ui

import (
	"errors"
	"io"
	"os"

	"github.com/ShellMonster/Biu_Go/internal/platform"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/egoist/mygo"
)

// errCoverTooBig 是「选的那张图太大了」。
//
// ⚠️ **这是给用户看的那一句**，所以不带包装：带上了就会变成
// 「图片太大，要小于等于 5MB: 图片太大，要小于等于 5MB」。
var errCoverTooBig = errors.New("图片太大，要小于等于 5MB")

// pickImageFile 是真的那个文件框。`Shell.New` 把它接上，测试换成桩。
//
// 单独一个函数是为了让接缝在 `New` 里是一行赋值，也为了让测试的替换点一眼
// 看得出换的是哪一条。
func pickImageFile(win *mygo.Window) ([]string, error) {
	return platform.PickImageFile(win)
}

// readFileLimited 读一个文件，但**最多读 limit 个字节**。
//
// ⚠️ 为什么不能直接 `os.ReadFile`：选图的框最后一项是「所有文件」，用户完全
// 可能选中一个几百兆的视频（`.webp` 的封面在 B 站上满地都是，而白名单只是
// 把该看见的摆到前面、不是拦路虎）。`ReadFile` 会**真的**把它整个读进内存。
// `LimitReader` 读到头就停。
func readFileLimited(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	raw, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) >= limit {
		return nil, errCoverTooBig
	}
	return raw, nil
}

// listActions 是列表那几个视图用的那一份操作。
//
// ⚠️ **收藏这一条接在这里，不在 `list.AppActions` 里**：那个函数拿得到
// `*app.App` 和 Toast，拿不到「开弹窗」——弹窗是**界面状态**，而 app 那一层
// 不认识界面（`internal/app` 的包注释第一条）。所以这里补上，列表照旧只认
// `list.Actions` 那几个字段。
func (s *Shell) listActions() list.Actions {
	acts := list.AppActions(s.app, s.addToast)
	acts.Favorite = s.openFolderSelect
	return acts
}
