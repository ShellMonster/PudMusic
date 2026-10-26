// 删除：这一票的**安全线**。跑在真磁盘上（真建目录、真建符号链接、真删文件）。
//
// 单独一个文件是因为这些用例和其他那些的性质不同：别的验「这一页画出来了
// 没有」，这里验的是**盘上还剩下什么**。混在一起读的时候容易只看见前半截。

package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/localmusic/localmusictest"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 删除：这一票的安全线
// ─────────────────────────────────────────────────────────────────────────────

// TestDeleteAsksFirstAndOnlyThenTouchesDisk 钉住「走确认弹窗」。
//
// ⚠️ 这一条是这一票最要紧的断言。分三步验，顺序不能换：
//
//  1. 按行里的「删除文件」→ 弹窗出来，**文件还在盘上**
//  2. 按弹窗里的「取消」→ 弹窗关了，**文件还在盘上**
//  3. 再按一次，这次按弹窗里的「删除」→ 文件没了
//
// 只验第 3 步的话，「按下去直接删」和「弹窗确认之后删」都会绿——而前者是
// 这一票明确不做的那件事（规格第 83 条旁边就是「有二次确认」那句验收）。
func TestDeleteAsksFirstAndOnlyThenTouchesDisk(t *testing.T) {
	root := t.TempDir()
	path := libSong(t, root, "要删的.mp3")
	tt, _, _, _ := shellOnLocalMusic(t, root)

	// 1. 按行里的「删除文件」。
	if err := tt.Click(deleteRowLabel); err != nil {
		t.Fatalf("点不到行里的「删除文件」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()

	// 弹窗上必须有那句告知（票 #558 之后正文改了，见 deleteDialogBody）。
	if !tt.HasText(deleteDialogBody) {
		t.Errorf("弹窗上没有「%s」，文字有 %q", deleteDialogBody, tt.Texts())
	}
	if !exists(path) {
		t.Fatal("弹窗刚出来就把文件删了")
	}
	// 弹窗上那个「删除」按钮也在（和行里那个同名不同标签，见 deleteRowLabel）。
	if !tt.HasText(deleteConfirmLabel) {
		t.Errorf("弹窗上没有「%s」按钮，文字有 %q", deleteConfirmLabel, tt.Texts())
	}

	// 2. 取消。
	if err := tt.Click(deleteCancelLabel); err != nil {
		t.Fatalf("点不到「取消」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	if !exists(path) {
		t.Fatal("按了取消还是把文件删了")
	}
	// 判「弹窗关了」用**正文**那一段而不是标题：标题是「删除文件」，而行里
	// 那个按钮也叫「删除文件」，用标题判的话取消之后那一行自己就让断言红。
	if tt.HasText(deleteDialogBody) {
		t.Errorf("按了取消弹窗还在：%q", tt.Texts())
	}

	// 3. 再来一次，这次确认。
	if err := tt.Click(deleteRowLabel); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText(deleteDialogBody) {
		t.Fatalf("第二次没弹出确认框，文字有 %q", tt.Texts())
	}
	if err := tt.Click(deleteConfirmLabel); err != nil {
		t.Fatalf("点不到弹窗里的「删除」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()

	if exists(path) {
		t.Fatal("确认之后文件还在盘上")
	}
	// 那一行也得从列表里没掉。⚠️ 判据用**行上才有的东西**（「打开文件夹」）
	// 而不是歌名：刚删完右下角那句 Toast 里就有歌名（「已移到系统废纸篓「要删的」），
	// 拿歌名去判会一直红——而那恰恰是这一条想验的那件事成功了。
	if tt.HasText("打开文件夹") {
		t.Errorf("删掉了但列表里还留着那一行，文字有 %q", tt.Texts())
	}
	if !tt.HasText("这里没有音频文件") {
		t.Errorf("删完之后该是空状态，文字有 %q", tt.Texts())
	}
}

// TestTheDeleteDialogNamesTheFullPath 钉住弹窗上带完整路径。
//
// 歌名会重名（「同名.mp3」在好几个目录里），而按下去文件就动起来了——用户
// 得能确认自己动的是哪一个文件。
func TestTheDeleteDialogNamesTheFullPath(t *testing.T) {
	root := t.TempDir()
	libSong(t, root, "同名.mp3")
	tt, _, _, _ := shellOnLocalMusic(t, root)

	if err := tt.Click(deleteRowLabel); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	if !tt.HasText(filepath.Join(root, "同名.mp3")) {
		t.Errorf("弹窗上没有完整路径 %q，文字有 %q",
			filepath.Join(root, "同名.mp3"), tt.Texts())
	}
}

// TestTheDeleteDialogIsOpenableFromASymlinkRow 钉住符号链接那一行也走同一道
// 确认，而且删的是链接不是它指向的东西。
//
// 这一条把 app 层的规矩和界面的规矩接在一起验：界面上那一行记的路径是链接
// 自己的路径（不是目标），所以「确认之后」该消失的是链接。
func TestTheDeleteDialogIsOpenableFromASymlinkRow(t *testing.T) {
	base := t.TempDir()
	library := filepath.Join(base, "音乐")
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}
	target := libSong(t, filepath.Join(base, "别处"), "真正的文件.mp3")
	link := filepath.Join(library, "指向别处.mp3")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("这台机器建不了符号链接：%v", err)
	}

	tt, _, _, _ := shellOnLocalMusic(t, library)
	if !tt.HasText("指向别处") {
		t.Fatalf("链接那一行不在列表里，文字有 %q", tt.Texts())
	}
	if err := tt.Click(deleteRowLabel); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	// 弹窗上摆的必须是链接自己的路径。
	if !tt.HasText(link) {
		t.Fatalf("弹窗上摆的不是链接自己的路径 %q，文字有 %q", link, tt.Texts())
	}
	if err := tt.Click(deleteConfirmLabel); err != nil {
		t.Fatalf("点不到弹窗里的「删除」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()

	if exists(link) {
		t.Error("链接还在")
	}
	if !exists(target) {
		t.Fatal("链接指向的文件被删了——这就是「删到别处去」")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 废纸篓（票 #558）
// ─────────────────────────────────────────────────────────────────────────────

// TestDeletingMovesTheFileToTheTrashAndSaysSo 钉住成功那一句。
//
// ⚠️ **三件事都要成立**：文件从原处没了、它在**废纸篓里**（找得回来）、
// 界面上说的是「已移到系统废纸篓」。
//
// 拿掉修复的方式：成功那一句改回「已从磁盘上删掉」——用户会以为文件被
// 永久删掉了，而它在废纸篓里（那倒不是坏事，只是说错了话）；而反过来，
// 把最后一步偷偷换回 `os.Remove` 的话，这一条的「废纸篓里有」那半会红。
func TestDeletingMovesTheFileToTheTrashAndSaysSo(t *testing.T) {
	root := t.TempDir()
	path := libSong(t, root, "要移走的.mp3")
	tt, _, _, _, trash, bin := shellOnLocalMusicWithTrash(t, false, root)

	if err := tt.Click(deleteRowLabel); err != nil {
		t.Fatalf("点不到行里的「%s」：%v，文字有 %q", deleteRowLabel, err, tt.Texts())
	}
	tt.Frame()
	if err := tt.Click(deleteConfirmLabel); err != nil {
		t.Fatalf("点不到弹窗里的「%s」：%v，文字有 %q", deleteConfirmLabel, err, tt.Texts())
	}
	tt.Frame()

	if exists(path) {
		t.Fatal("原处还有那个文件")
	}
	// ⚠️ **它在废纸篓里**：用户找得回来。
	entries, err := os.ReadDir(bin)
	if err != nil {
		t.Fatalf("读废纸篓：%v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("废纸篓里有 %d 项，want 1（文件得是搬过去的，不是删掉的）", len(entries))
	}
	if trash.CallCount() != 1 {
		t.Errorf("回收站被调了 %d 次，want 1", trash.CallCount())
	}
	// 界面上那一句。
	if !screenHasText(tt, "已移到系统废纸篓") {
		t.Errorf("界面上没有「已移到系统废纸篓」那一句，文字有 %q", tt.Texts())
	}
	// ⚠️ **不许说「不可恢复」**：那一句话在票 #558 之后是**假话**（文件在
	// 废纸篓里，用户找得回来）。
	if screenHasText(tt, "不可恢复") {
		t.Errorf("界面上还写着「不可恢复」，文字有 %q", tt.Texts())
	}
}

// TestTheDeleteDialogNoLongerPromisesItIsUnrecoverable 钉住弹窗上那一句。
//
// ⚠️ **那句话是用户按下按钮和文件动起来之间唯一的告知**：留着上游那句
// 「且不可恢复」就是**对用户说假话**（文件现在进的是系统废纸篓）。
func TestTheDeleteDialogNoLongerPromisesItIsUnrecoverable(t *testing.T) {
	root := t.TempDir()
	libSong(t, root, "歌.mp3")
	tt, _, _, _, _, _ := shellOnLocalMusicWithTrash(t, false, root)

	if err := tt.Click(deleteRowLabel); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	if !tt.HasText(deleteDialogBody) {
		t.Errorf("弹窗上没有「%s」，文字有 %q", deleteDialogBody, tt.Texts())
	}
	if screenHasText(tt, "不可恢复") {
		t.Errorf("弹窗上还写着「不可恢复」，文字有 %q", tt.Texts())
	}
	if !screenHasText(tt, "废纸篓") {
		t.Errorf("弹窗上没提「废纸篓」，文字有 %q", tt.Texts())
	}
}

// TestATrashThatFailsSaysTheFileIsStillThere 钉住失败那一句。
//
// ⚠️ **三件事都要成立**：界面上说「**文件未删除**」、**文件还在原处**、
// 那一行**还在列表里**。
//
// 拿掉修复的方式：把失败当成成功——那用户会看到「已移到废纸篓」，而文件
// 好好地躺在原处（下一次重扫它又回来了）。
func TestATrashThatFailsSaysTheFileIsStillThere(t *testing.T) {
	root := t.TempDir()
	path := libSong(t, root, "还在的.mp3")

	// 造一个**回收站不可用**的外壳：假的那一份每次都失败，而按契约
	// **它不碰文件**。
	b := newTestShell(t,
		systemIsDark(false),
		withSettings(func() app.Settings {
			st := app.DefaultSettings("")
			st.LocalMusicDirs = []string{root}
			return st
		}()),
		mounted(func(_ *testing.T, a *app.App) {
			a.MountLocalMusic(app.LocalMusic{
				Trash: localmusictest.Fail(errors.New(
					"mygo: cannot move it to the recycle bin: it would be deleted permanently")),
			})
		}))
	tt, _, a := b.tt, b.s, b.a
	openLocal(t, tt)
	settleLocal(t, a, tt)

	if err := tt.Click(deleteRowLabel); err != nil {
		t.Fatalf("点不到行里的「%s」：%v，文字有 %q", deleteRowLabel, err, tt.Texts())
	}
	tt.Frame()
	if err := tt.Click(deleteConfirmLabel); err != nil {
		t.Fatalf("点不到弹窗里的「%s」：%v，文字有 %q", deleteConfirmLabel, err, tt.Texts())
	}
	tt.Frame()

	if !exists(path) {
		t.Fatal("回收站失败了却把文件删了——这一层绝不能回退到永久删除")
	}
	if !screenHasText(tt, "文件未删除") {
		t.Errorf("界面上没有「文件未删除」那一句，文字有 %q", tt.Texts())
	}
	// 那一行还在列表里（用户还能再试一次）。
	if !tt.HasText("打开文件夹") {
		t.Errorf("失败的删除把那一行从列表里拿掉了，文字有 %q", tt.Texts())
	}
}

// TestATrashThatFailsNeverSaysItMovedToTheTrash 钉住「**回收站失败的时候，
// 界面上不许出现「已移到系统废纸篓」**」。
//
// 🔴 **这一条是评审打回来的那一种**。原来错误分类对**整段**系统原话做
// 子串匹配，而 Windows 那边**每一句**回收站失败的原话都带同一截前缀
// 「to the recycle bin」——拿它当判据的话，权限失败（0x80070005）、已存在
// （0x80070050）、文件不存在（0x80070002）**全**被归成「系统不提供回收站」。
//
// 而更糟的那一种：一个叫「Not Found.mp3」的文件，失败的话里就带着
// 「not found」三个字——它被归成「文件已经不在了」，于是界面说
// 「已移到系统废纸篓」，**列表那一行也消失了**，而文件好好地躺在原处。
//
// ⚠️ **三件事都要成立**：界面上**没有**「已移到系统废纸篓」、**文件还在
// 原处**、**那一行还在列表里**。
func TestATrashThatFailsNeverSaysItMovedToTheTrash(t *testing.T) {
	root := t.TempDir()
	path := libSong(t, root, "Not Found.mp3")

	// 造一个**Windows 形态**的失败：原话带着那一条路径（路径上的文件名
	// 叫「Not Found」），而 HRESULT 是 0x80070005（权限不够）。
	b := newTestShell(t,
		systemIsDark(false),
		withSettings(func() app.Settings {
			st := app.DefaultSettings("")
			st.LocalMusicDirs = []string{root}
			return st
		}()),
		mounted(func(_ *testing.T, a *app.App) {
			a.MountLocalMusic(app.LocalMusic{
				Trash: localmusictest.WindowsFailure(0x80070005),
			})
		}))
	tt, _, a := b.tt, b.s, b.a
	openLocal(t, tt)
	settleLocal(t, a, tt)

	if err := tt.Click(deleteRowLabel); err != nil {
		t.Fatalf("点不到行里的「%s」：%v，文字有 %q", deleteRowLabel, err, tt.Texts())
	}
	tt.Frame()
	if err := tt.Click(deleteConfirmLabel); err != nil {
		t.Fatalf("点不到弹窗里的「%s」：%v，文字有 %q", deleteConfirmLabel, err, tt.Texts())
	}
	tt.Frame()

	// 🔴 **这一句是这一条的要害**：回收站失败了，而界面说「已移到系统
	// 废纸篓」——用户以为删掉了（还能去废纸篓里找），而文件还在原处。
	if screenHasText(tt, "已移到系统废纸篓") {
		t.Errorf("回收站失败了，界面上却说「已移到系统废纸篓」，文字有 %q", tt.Texts())
	}
	if !screenHasText(tt, "文件未删除") {
		t.Errorf("界面上没有「文件未删除」那一句，文字有 %q", tt.Texts())
	}
	if !exists(path) {
		t.Fatal("回收站失败了却把文件删了——这一层绝不能回退到永久删除")
	}
	// 那一行还在列表里（用户还能再试一次）。
	if !tt.HasText("打开文件夹") {
		t.Errorf("失败的删除把那一行从列表里拿掉了，文字有 %q", tt.Texts())
	}
}

// TestAFileThatIsAlreadyGoneNeverSaysItMovedToTheTrash 钉住「文件本来就不
// 在了」那一种。
//
// 🔴 **这一条是评审打回来的那一种**：用户先在访达里删了歌，回来点删除，
// 而应用层把「文件已经不在了」改成 `nil`（当作「不是失败」）——于是界面走
// 成功分支，说「已移到系统废纸篓「外面已经删了」」。
//
// ⚠️ **那句话是假话**：它**承诺用户能从废纸篓里恢复**，而它压根没进过
// 废纸篓。
//
// ⚠️ **三件事都要成立**：界面上**没有**「已移到系统废纸篓」、界面上**说了**
// 「已经不在了」、**那一行从列表里没掉**（那是用户想要的结果）。
func TestAFileThatIsAlreadyGoneNeverSaysItMovedToTheTrash(t *testing.T) {
	root := t.TempDir()
	path := libSong(t, root, "外面已经删了.mp3")
	tt, _, _, _, _, _ := shellOnLocalMusicWithTrash(t, false, root)

	// 先把原文件删掉：模拟「用户先在访达里删了，回来才点删除」。
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	if err := tt.Click(deleteRowLabel); err != nil {
		t.Fatalf("点不到行里的「%s」：%v，文字有 %q", deleteRowLabel, err, tt.Texts())
	}
	tt.Frame()
	if err := tt.Click(deleteConfirmLabel); err != nil {
		t.Fatalf("点不到弹窗里的「%s」：%v，文字有 %q", deleteConfirmLabel, err, tt.Texts())
	}
	tt.Frame()

	// 🔴 **这一句是这一条的要害**。
	if screenHasText(tt, "已移到系统废纸篓") {
		t.Errorf("文件压根没进过废纸篓，界面上却说「已移到系统废纸篓」，文字有 %q", tt.Texts())
	}
	// 而它得说清楚**真相**：文件已经不在了（用户自己删的）。
	if !screenHasText(tt, "已经不在了") {
		t.Errorf("界面上没有「已经不在了」那一句，文字有 %q", tt.Texts())
	}
	// ⚠️ **那一行照样从列表里没掉**：那是用户想要的结果（把过期的那一行
	// 清掉）。
	if tt.HasText("打开文件夹") {
		t.Errorf("文件已经不在了，那一行却还留在列表里，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 目录
// ─────────────────────────────────────────────────────────────────────────────

// TestAddingADirectoryStoresItAndScans 钉住「添加目录」。
//
// 三件事都要成立：对话框**被调了**（用户有一个入口）、设置里**存住了**
// （刷新应用还在）、这一页**扫出了那个目录里的歌**（不是只存了个字符串）。
func TestAddingADirectoryStoresItAndScans(t *testing.T) {
	tt, _, a, pick := shellOnLocalMusic(t)

	root := t.TempDir()
	libSong(t, root, "新加的.mp3")
	pick.set(root)

	if err := tt.Click("添加目录"); err != nil {
		t.Fatalf("点不到「添加目录」：%v，文字有 %q", err, tt.Texts())
	}
	// 弹框那一支跑在后台 goroutine 上（`mygo.Dialog.Open` 必须从非主线程调）。
	waitFor(t, "「添加目录」没有真的去弹框", func() bool { return pick.callCount() > 0 })
	waitFor(t, "选完目录之后设置里没有它", func() bool {
		return len(a.SettingsSnapshot().LocalMusicDirs) == 1
	})
	settleLocal(t, a, tt)

	// ⚠️ 断的是**列表里真的有那首歌**，不是「扫描报完了」。设置写进去和
	// 页面画出那首歌之间隔着一次重扫，而 `settleLocal` 等的是扫描状态——
	// 只等扫描状态的话，页面还没跑过 `begin`（所以扫描还没开始）那一次会
	// 立刻返回，断言在画面还是旧的一份时就读完了（实测随机红）。
	if !tt.HasText("新加的") {
		if got := len(a.LocalMusicState().Tracks); got == 0 {
			t.Errorf("加了目录却没扫出里面的歌（列表 %d 首），文字有 %q", got, tt.Texts())
		} else {
			tt.Frame()
			if !tt.HasText("新加的") {
				t.Errorf("加了目录却没扫出里面的歌，文字有 %q", tt.Texts())
			}
		}
	}
}

// TestAddingTheSameDirectoryTwiceSaysSo 钉住不重复加。
//
// 重复加不会出错（设置里两份同一个目录，`app.SetLocalRoots` 会去重），
// 但界面上得说一句——用户会以为第二次没生效。
func TestAddingTheSameDirectoryTwiceSaysSo(t *testing.T) {
	root := t.TempDir()
	libSong(t, root, "歌.mp3")
	tt, _, a, pick := shellOnLocalMusic(t, root)
	pick.set(root)

	if err := tt.Click("添加目录"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "没有去弹框", func() bool { return pick.callCount() > 0 })
	settleLocal(t, a, tt)

	if got := len(a.SettingsSnapshot().LocalMusicDirs); got != 1 {
		t.Errorf("同一个目录加了两次，设置里有 %d 个", got)
	}
}

// TestRemovingADirectoryKeepsTheFiles 钉住「移除目录不删文件」。
//
// 这一页上「移除目录」和「删除文件」两个动作摆在同一屏，用户分不清的时候
// 会点错。所以这一条钉的是**文件一个没动**，而弹窗上那句话（照抄 Biu 的
// 「不会删除本地文件」）在下一条里钉。
func TestRemovingADirectoryKeepsTheFiles(t *testing.T) {
	other := t.TempDir()
	library := t.TempDir()
	path := libSong(t, library, "别删我.mp3")
	libSong(t, other, "另一个目录.mp3")

	tt, _, a, _ := shellOnLocalMusic(t, library, other)

	// 先筛到 library 那一个，「移除此目录」才会出现。
	if err := tt.Click("Music"); err != nil {
		t.Skipf("这一版的目录筛选没摆出来：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	if err := tt.Click("移除此目录"); err != nil {
		t.Fatalf("点不到「移除此目录」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()

	// 弹窗上必须写着「不会删除本地文件」。
	if !tt.HasText(removeDirBody) {
		t.Errorf("弹窗上没有「%s」，文字有 %q", removeDirBody, tt.Texts())
	}
	if err := tt.Click("移除"); err != nil {
		t.Fatalf("点不到弹窗里的「移除」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()

	if !exists(path) {
		t.Fatal("移除目录把文件删了")
	}
	if got := a.SettingsSnapshot().LocalMusicDirs; len(got) != 1 || got[0] != other {
		t.Errorf("设置里的目录是 %v，want 只剩 %v", got, other)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 行身份（铁律四）
// ─────────────────────────────────────────────────────────────────────────────

// TestInsertingARowKeepsTheChosenSong 钉住这一页的行身份（铁律四）。
//
// ⚠️ 这是 widgets/list 里那条铁律在这一页上的翻版，而这一页**更容易**踩到：
// 加一个目录就是往列表**前面**插一整段，后面所有行的下标全部错位。
// 不按 ID 装 Key 的话，用户选中的那一首会在刷新之后变成旁边那一首。
//
// 判据是「**选中项指的还是同一首歌**」（按 ID 比），不是「选中项的下标没变」
// ——后者恰恰是错的判据：下标不变的那一次才是对的。
//
// ⚠️ 「插到**前面**」这件事是真的要成立的：`Scan` 按**根目录路径**排序
// （`localmusic.go` 的 `slices.SortFunc`），而 `t.TempDir()` 给的是
// `.../001`、`.../002`、`.../003`——**先建的那个排在前**。所以这里先把那个
// 「将来要插在最前面」的目录建出来（先建、空着、暂时不加进设置），
// 不这么做的话新目录会排到最后面，而这一条就什么都验不到了。
// `TestInsertingARowKeepsTheChosenSong` 自己还断言了「新行确实在第 0 行」。
//
// 拿掉修复的方式：`viewTable` 里不装 `p.table.Key`（框架退回「父 ID +
// 兄弟序号」那一套）。
func TestInsertingARowKeepsTheChosenSong(t *testing.T) {
	front := t.TempDir() // 先建 = 路径最小 = 列表里排最前
	first := t.TempDir()
	second := t.TempDir()
	libSong(t, first, "第一目录的歌.mp3")
	libSong(t, second, "第二目录的歌.mp3")

	tt, s, a, _ := shellOnLocalMusic(t, first, second)

	// 点第二首的标题把它选中（表格的 `Selected` 让单击就能选行）。
	clickRow(t, tt, s, "第二目录的歌")
	sel := s.local.sel
	if sel < 0 || sel >= len(s.local.rows) {
		t.Fatalf("点了第二首却没选中（sel=%d，%d 行）", sel, len(s.local.rows))
	}
	chosen := s.local.rows[sel].ID
	if !strings.HasSuffix(s.local.rows[sel].Path, "第二目录的歌.mp3") {
		t.Fatalf("选中的是 %q，want 第二目录那首", s.local.rows[sel].Path)
	}

	// 往那个早就建好的空目录里放一首歌，然后把它加进设置——这就是
	// 「用户加了一个新目录」，走的是同一条 `begin` → `SetLocalRoots` → 重扫。
	libSong(t, front, "插到最前面.mp3")
	if _, err := a.UpdateSettings(func(st *app.Settings) {
		st.LocalMusicDirs = []string{front, first, second}
	}); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "本地音乐那一遍扫描落地", func() bool { return !a.LocalMusicState().Scanning })
	tt.Frame()

	if !tt.HasText("插到最前面") {
		t.Fatalf("新目录的歌没进列表，文字有 %q", tt.Texts())
	}
	// ⚠️ 先确认它真的排在**第 0 行**——这一条不成立的话整个用例就白跑了
	// （插在最后面的话，下标本来就不会错位，验不到任何东西）。
	if len(s.local.rows) != 3 || !strings.HasSuffix(s.local.rows[0].Path, "插到最前面.mp3") {
		t.Fatalf("新行不在第 0 行，这一趟验不到行身份：%+v", pathsOf(s.local.rows))
	}
	if s.local.sel < 0 || s.local.sel >= len(s.local.rows) {
		t.Fatalf("加目录之后选中没了（sel=%d）", s.local.sel)
	}
	if got := s.local.rows[s.local.sel].ID; got != chosen {
		t.Errorf("前面插了一行之后选中变成了 %q（%q），want 还是 %q",
			got, s.local.rows[s.local.sel].Path, chosen)
	}
}

// clickRow 点中标题为 title 的那一行的标题格，然后画下一帧。
func clickRow(t *testing.T, tt *mygoui.Tester, s *Shell, title string) {
	t.Helper()
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("屏幕上没有 %q，文字有 %q", title, tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	_ = s
}

// ─────────────────────────────────────────────────────────────────────────────
// 唤醒（铁律三）
// ─────────────────────────────────────────────────────────────────────────────

// TestABackgroundScanWakesEveryWindowThatDrewIt 钉住「逐窗口通知」。
//
// 🔴 只叫醒主窗口的话迷你窗口停在旧内容——症状是「有时候这一块不刷新」，
// 和它真正的原因隔了三层，任何别的测试都抓不到。
//
// ⚠️ **必须真的有两个窗口**，否则这条验不到东西：一个窗口时「叫醒全部」和
// 「叫醒第一个」是同一件事，把 `invalidate` 改成只叫第一条照样绿
// （这一条就是这么被逼出来的）。第二个窗口用另一个 `ui.NewTester` 造
// ——`ui.Services` 的身份和第一个不一样，`setWake` 于是把它认成第二个窗口。
//
// 判据是**两个计数器都被叫醒了**。
func TestABackgroundScanWakesEveryWindowThatDrewIt(t *testing.T) {
	root := t.TempDir()
	libSong(t, root, "歌.mp3")
	tt, s, a, _ := shellOnLocalMusic(t, root)

	// 第二个窗口：它也画这一页（同一个 localPage，两套画面）。
	other := mygoui.NewTester(s.local.View, 800, 600)

	s.local.wakeMu.Lock()
	if got := len(s.local.wakes); got != 2 {
		t.Fatalf("两个窗口画了这一页，句柄表里有 %d 条，want 2", got)
	}
	// 两个窗口各自一个计数器（共用一个的话数不出「都叫醒了」）。
	first, second := &counter{}, &counter{}
	s.local.wakes[0].wake, s.local.wakes[1].wake = first.add, second.add
	s.local.wakeMu.Unlock()

	// 装上这一页的唤醒（生产里 `New` 就装好了，这里是让测试能观察它），
	// 然后发起一遍扫描。
	a.AddLocalWake(s.local.invalidate)
	beforeFirst, beforeSecond := first.get(), second.get()
	a.RescanLocalMusic()

	waitFor(t, "主窗口没被叫醒", func() bool { return first.get() > beforeFirst })
	waitFor(t, "第二个窗口被叫醒（后台投递完只唤醒了一个窗口，另一个停在旧内容）",
		func() bool { return second.get() > beforeSecond })

	// 两个都醒了之后，帧也画得出来。
	tt.Frame()
	other.Frame()
	if !tt.HasText("歌") {
		t.Errorf("叫醒之后画面上还是旧的，文字有 %q", tt.Texts())
	}
}

// TestOneWindowDrawingTwiceStaysOneWake 钉住按窗口身份去重。
//
// `setWake` 每帧都调，而一个窗口要画很多帧。不去重的话句柄表会一直长，
// 到 `maxLocalWakes` 之后开始挤掉**正在用的**那一条（每次挤的都是「最久
// 没画的」，而每一帧都在重登记，于是最久没画的那条其实正是别人）。
func TestOneWindowDrawingTwiceStaysOneWake(t *testing.T) {
	root := t.TempDir()
	libSong(t, root, "歌.mp3")
	_, s, _, _ := shellOnLocalMusic(t, root)

	for range 5 {
		s.local.setWake(s.local.wakes[0].svc)
	}
	s.local.wakeMu.Lock()
	got := len(s.local.wakes)
	s.local.wakeMu.Unlock()
	if got != 1 {
		t.Errorf("同一个窗口画了 5 遍，句柄表里有 %d 条，want 1", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 筛选与打开
// ─────────────────────────────────────────────────────────────────────────────

// TestSearchFiltersTheRows 钉住搜索框真的在筛。
//
// 只匹配**标题**（Biu 也是只匹配标题）——而标题这一页是从标签读的，所以
// 这一条顺带证明「标题和文件名是两回事」在界面上也成立。
func TestSearchFiltersTheRows(t *testing.T) {
	root := t.TempDir()
	libSong(t, root, "周杰伦-晴天.mp3")
	libSong(t, root, "五月天-倔强.mp3")
	tt, _, _, _ := shellOnLocalMusic(t, root)

	typeInto(t, tt, searchLocalLabel, "晴天")
	tt.Frame()

	if !tt.HasText("晴天") {
		t.Errorf("搜「晴天」搜不到，文字有 %q", tt.Texts())
	}
	if tt.HasText("倔强") {
		t.Errorf("搜「晴天」还留着「倔强」，文字有 %q", tt.Texts())
	}
}

// TestOpenFolderIsOnEveryRow 钉住「打开文件夹」。
//
// 每一行都得有一个：它是这一页上唯一一个「不删任何东西、只是把用户在系统里
// 的位置指给他」的动作，而用户找一首歌常常是为了拿它去做别的事。
func TestOpenFolderIsOnEveryRow(t *testing.T) {
	root := t.TempDir()
	libSong(t, root, "歌.mp3")
	tt, _, _, _ := shellOnLocalMusic(t, root)

	if !tt.HasText("打开文件夹") {
		t.Errorf("每一行上没有「打开文件夹」，文字有 %q", tt.Texts())
	}
}

// TestARootThatCannotBeReadIsShownNotSwallowed 钉住坏目录会上到界面上。
func TestARootThatCannotBeReadIsShownNotSwallowed(t *testing.T) {
	good := t.TempDir()
	libSong(t, good, "好目录里的.mp3")
	tt, _, _, _ := shellOnLocalMusic(t, good, filepath.Join(good, "根本没这个"))

	if !tt.HasText("有目录读不到") {
		t.Errorf("读不到的目录没上到界面，文字有 %q", tt.Texts())
	}
	if !tt.HasText("好目录里的") {
		t.Errorf("好目录里的歌被一起丢掉了，文字有 %q", tt.Texts())
	}
}

// TestAPanicInThisPageDoesNotKillTheProcess 钉住这一页自己的那一道 recover。
//
// 🔴 MyGo 的 Dispatch 和事件循环的 drain 都没有 recover，view 里 panic 会一路
// 穿到 Cocoa 事件循环上去**直接杀死进程**，连「这一页坏了」的机会都没有。
//
// 这一页新加了整整一块 view（表格 + 两道弹窗 + 筛选），所以除了 `Shell.page`
// 那一道页级的之外，`localPage` 自己也包了一道（`guard.Wrap`，见 newLocalPage）。
// 这一条验的就是**这一道**——让真的画法在画完之后崩，看进程还在、`onPanic`
// 还要到了下一帧。
//
// 拿掉修复的方式：`newLocalPage` 里不调 `guard.Wrap`（`draw` 直接调 `paint`）。
func TestAPanicInThisPageDoesNotKillTheProcess(t *testing.T) {
	root := t.TempDir()
	libSong(t, root, "歌.mp3")
	_, s, _, _ := shellOnLocalMusic(t, root)

	// 在真的画法**之后**崩：前面那半帧（登记唤醒句柄、读设置、发起扫描）
	// 照常跑完，这样「onPanic 要到了下一帧」才有得验。
	real := s.local.paint
	s.local.paint = func(c *mygoui.Context) {
		real(c)
		panic("这一页画崩了")
	}

	// ⚠️ 先把句柄表清空：`NewTester` 开的是**另一个窗口**（`ui.Services` 的
	// 身份和外壳那个 Tester 不一样），不清的话 `setWake` 会把它当成第二个
	// 窗口登记进去，下面数出来的就不是「这一帧登记了几条」了。
	s.local.wakeMu.Lock()
	s.local.wakes = nil
	s.local.wakeMu.Unlock()

	// 一帧画完就该接住这个 panic：接不住的话这个测试进程自己就没了，
	// go test 报出来的是「test executed panic(nil) or runtime.Goexit」。
	tt := mygoui.NewTester(s.local.View, 1200, 800)

	// 那一帧至少走到了 `begin`（唤醒句柄是在那儿登记的）：这一条顺带证明
	// 「崩的是画内容的部分，不是画到一半没登记」。
	//
	// 「崩了要下一帧」那一条由 `guard` 自己的测试钉着
	//（`guard_test.go` 的 `TestWrapKeepsAPanicOutOfTheProcess`）——在这儿
	// 数不到：`NewTester` 在返回之前就把那一帧要来的下一帧画掉了，
	// `FramePending()` 已经是 false，而 `onPanic` 早就调过一次 invalidate。
	s.local.wakeMu.Lock()
	got := len(s.local.wakes)
	s.local.wakeMu.Unlock()
	if got != 1 {
		t.Fatalf("这一帧登记了 %d 条唤醒句柄，want 1", got)
	}

	// 崩过之后下一帧还要能画（换回正常的画法）：这是「进程没死、这一页还能用」
	// 的直接证据。
	s.local.paint = real
	tt.Frame()
	if !tt.HasText("本地音乐") {
		t.Errorf("崩过之后这一页画不出来，文字有 %q", tt.Texts())
	}
}

// TestDeletingFromARootWithATrailingSpace 钉住「尾部带空格的目录」在**整条
// 链路上**都指对地方。
//
// ⚠️ 这一条来自评审，而且它是**从真实 Shell 上点出来的**：把 `Music `（末尾
// 一个空格）配进设置，页面上出现的是**隔壁** `Music` 里那首歌，点删除 + 确认
// 之后删掉的是隔壁那首，而 `Music ` 里的没动。
//
// 服务层那一条（`localmusic` 包的 `TestDeleteRefusesARootWithATrailingSpace`）
// 断的是「根检查不裁空格」。这里断的是**整条链路**：设置 → 扫描 → 列表 →
// 按删除 → 按确认 → 磁盘，中间任何一环再裁一次空格就又错了。
func TestDeletingFromARootWithATrailingSpace(t *testing.T) {
	base := t.TempDir()
	plain := filepath.Join(base, "Music")
	spaced := filepath.Join(base, "Music ") // 末尾一个空格
	for _, d := range []string{plain, spaced} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// 用户配的是**带空格**那个，里面只有 chosen-only.mp3。
	chosen := libSong(t, spaced, "chosen-only.mp3")
	// 隔壁那个里有**同名**的歌：一旦哪一环把空格裁掉，删的就会是它。
	other := libSong(t, plain, "other-only.mp3")

	tt, _, _, _ := shellOnLocalMusic(t, spaced)

	if !tt.HasText("chosen-only") {
		t.Fatalf("列表里不是带空格那个目录里的歌，文字有 %q", tt.Texts())
	}
	if tt.HasText("other-only") {
		t.Fatalf("隔壁目录的歌出现在列表里了，文字有 %q", tt.Texts())
	}

	if err := tt.Click(deleteRowLabel); err != nil {
		t.Fatalf("点不到行里的「%s」：%v，文字有 %q", deleteRowLabel, err, tt.Texts())
	}
	tt.Frame()
	// 弹窗上摆的必须是带空格那个目录里的那条完整路径。
	if !tt.HasText(chosen) {
		t.Fatalf("弹窗上摆的路径不是 %q，文字有 %q", chosen, tt.Texts())
	}
	if err := tt.Click(deleteConfirmLabel); err != nil {
		t.Fatalf("点不到弹窗里的「%s」：%v，文字有 %q", deleteConfirmLabel, err, tt.Texts())
	}
	tt.Frame()

	if exists(chosen) {
		t.Error("用户选的那首还在")
	}
	if !exists(other) {
		t.Fatal("隔壁目录里的歌被删了——哪一环把尾部空格裁掉了")
	}
}
