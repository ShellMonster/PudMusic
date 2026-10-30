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
	"strconv"
	"sync"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/platform"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
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
	// 🔴 **复制走 `s.copyText`，不是 `s.wake.writeClipboard`**（票 #685 的对抗性
	// 审查）：后者**不取号**，于是在途的封面那一趟（后台编 PNG，慢）跑完时问
	// `isLatest(gen)` 仍然是 true，它会照写——用户最后按的「复制歌名」被一张旧
	// 封面盖掉。`copyText` 走 `commitNow`，取号 + 写在同一段里（理由见
	// `clipboard.go` 的 `copyClipboard`）。
	//
	// ⚠️ **说话在列表那一层**（`widgets/list` 的 `notifyCopied` 与
	// `menu.go` 的「已复制歌曲信息」），因为它知道用户按的是 ⌘C 还是菜单项。
	acts.Copy = func(text string) {
		if err := s.copyText(text); err != nil {
			s.addToast("复制不了："+err.Error(), notice.Danger)
		}
	}
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
		s.openOwnerSpace(mid)
	}
	// 合集那一行的「打开这个合集」（票 #683，缺陷 B）。
	//
	// ⚠️ **和侧栏那条路走同一个拼法**（`collectionPath` + `collectionTypeQuery`）：
	// 两处各拼一遍的话，改了一处另一处就安静地跳到错页面上。
	acts.OpenCollection = func(it app.Item) {
		s.navigate(collectionPath(it.CollectionID) +
			"?" + collectionTypeQuery + "=" + strconv.Itoa(int(bili.CollectionAlbum)))
	}
	return acts
}

// openOwnerSpace 进那个 UP 主的空间。
//
// ⚠️ **只有这一处导航**（票 #360 / BMG-253）：列表里的 UP 名、收藏夹头图区
// 那一行、这一页的创建者头像——三处都走它。各写一遍 `navigate` 的话，改了
// 一处另一处就安静地走错（而那种错在界面上只是「点了没反应」）。
func (s *Shell) openOwnerSpace(mid int64) {
	s.navigate(upPathOf(mid))
}

// ownerActions 是「进那个 UP 的空间」这一件事用的那份操作。
//
// ⚠️ **它只装 `OpenOwner`**：收藏夹头图区那一行能做的只有这一件事，列表那一
// 整套（收藏夹选择、焦点交接……）在那里都用不上——而那一整套每帧要建五六个
// 闭包。
func (s *Shell) ownerActions() list.Actions {
	return list.Actions{OpenOwner: func(mid int64, _ string) { s.openOwnerSpace(mid) }}
}
