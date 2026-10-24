// 收藏夹管理那几个弹窗的接缝：「弹文件框选一张图」和「读那个文件」。
//
// 单独一个文件是因为它只有接缝、没有界面——界面在 folderedit.go /
// covermodal.go / folderselect.go 里。

package ui

import (
	"errors"
	"image"
	"io"
	"os"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/platform"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/egoist/mygo"
)

// coverPick 是「后台把那个封面文件读出来、解好了」的一个交接槽。
//
// 🔴 **它存在的唯一理由是「后台那趟不许碰界面状态」**（铁律一）。写它的是
// `pickCover` 的 goroutine，读它的是界面线程（`flushCoverPick`，每帧一次）。
//
// 早一版没有这一层：那个 goroutine 直接 `crop.setSource(...)` +
// `crop.open = true`，而 `cropState` 是界面状态。`-race` 报出来是
// 「goroutine N 写、主线程读」，症状是裁剪弹窗**有时候开不起来**（`-race` 下
// 跑几次红几次，单线程跑全绿）。这一层把那趟交接变成**带锁的一放一取**，
// 界面上的一切仍然只在主线程上改。
type coverPick struct {
	mu   sync.Mutex
	have bool
	img  *image.RGBA
	path string
	// err 是「那句要说给用户听的话」。空串表示没出错。
	err string
}

// put 放一份。**取走过就没有了**——放两份的话界面会开两次裁剪弹窗，而第二次
// 盖在第一次上面（两个 Modal），用户看到的是弹窗卡在弹窗里出不来。
func (p *coverPick) put(img *image.RGBA, path string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.have = true
	p.img, p.path, p.err = img, path, errText(err)
}

// errText 把那个错翻成一句人话（空串 = 没出错）。
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// take 取走那一份。
func (p *coverPick) take() (img *image.RGBA, path, errMsg string, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.have {
		return nil, "", "", false
	}
	p.have = false
	return p.img, p.path, p.err, true
}

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
	acts.FavSelect = s.openFolderSelect
	// 焦点交接（票 #245 的 E4）：每一帧各个列表把自己登记上去，于是「侧栏按 ↓
	// 把焦点交给内容区」有一个统一的目标——理由见 `widgets/list` 的 `focus.go`。
	acts.Focus = s.registerContentFocus
	// 反过来：内容区的列表在顶上一行时按 ↑，把焦点交回侧栏。
	acts.FocusOut = s.focusSide
	// 点列表里的 UP 名进他的空间（票 #360）。
	//
	// ⚠ **走 `upPathOf` 而不是写死 `"/user/"`**：路由表里那一条改了的话，写死
	// 的会安静地退回占位页（理由见 `upspace.go` 的 `upPathPrefix`）。
	//
	// ⚠ **别在列表里再加第二套导航**：那是「同一条路两套写法」，改一处就会
	// 有一处安静地走错。
	acts.OpenOwner = func(mid int64, _ string) {
		s.navigate(upPathOf(mid))
	}
	return acts
}
