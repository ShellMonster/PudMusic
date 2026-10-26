package localmusic

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/service/localmusic/localmusictest"
)

// 这一组盯的是「移进废纸篓」那一步的**三条路**（成功 / 失败 / 不可回收）
// 以及「没装回收站」那一条。
//
// ⚠️ **失败那几条断的是「文件还在原处」**：那一半才是这一票的要害。界面上
// 说了「文件未删除」而文件其实没了，那是**最坏**的一种错——用户信了界面，
// 于是他不会再去文件管理器里找。而一个「失败时不碰文件」的实现，
// 哪怕它说的话是错的，损失也只是一次困惑。

// TestDeleteMovesTheFileIntoTheTrash 钉住成功那一条。
//
// 三件事都要成立：原处没了、废纸篓里有了、**目录还在**。
func TestDeleteMovesTheFileIntoTheTrash(t *testing.T) {
	root := t.TempDir()
	song := songPath(t, root, "歌.mp3")
	trash, bin := trashFor(t)

	if err := Delete(trOf(t, song), []string{root}, trash); err != nil {
		t.Fatal(err)
	}
	if exists(t, song) {
		t.Fatal("原处还有那个文件")
	}
	entries, err := os.ReadDir(bin)
	if err != nil {
		t.Fatalf("读废纸篓：%v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("废纸篓里有 %d 项，want 1", len(entries))
	}
	if !exists(t, root) {
		t.Fatal("目录被一起搬走了")
	}
}

// TestDeleteHandsTheLexicalPathToTheTrash 钉住「交给回收站的是哪一条路径」。
//
// ⚠️ **这一条是符号链接那条安全线的另一半**（`TestDeleteRemovesTheSymlinkAndNotWhatItPointsAt`
// 断的是「目标还在」）：交给回收站的必须是**词法上的那一条**（链接自己的
// 路径），而不是解析之后的那一条（它指向的文件）。解析过就等于「移目标」，
// 也就是把用户音乐库外面的东西搬走了。
func TestDeleteHandsTheLexicalPathToTheTrash(t *testing.T) {
	base := t.TempDir()
	library := filepath.Join(base, "音乐")
	outside := filepath.Join(base, "别处")
	for _, d := range []string{library, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	target := songPath(t, outside, "真正的文件.mp3")
	link := filepath.Join(library, "指向别处.mp3")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("这台机器建不了符号链接：%v", err)
	}
	trash, _ := trashFor(t)

	if err := Delete(trOf(t, link), []string{library}, trash); err != nil {
		t.Fatal(err)
	}
	calls := trash.Calls()
	if len(calls) != 1 {
		t.Fatalf("回收站被调了 %d 次，want 1：%v", len(calls), calls)
	}
	if calls[0] != link {
		t.Errorf("交给回收站的是 %q，want %q（链接自己的路径）", calls[0], link)
	}
}

// TestDeleteLeavesTheFileAloneWhenTheTrashFails 钉住失败那一条的**要紧的一半**。
//
// ⚠️ **文件必须还在原处**：回收站不可用的时候，框架那一侧「失败并把东西
// 留在原地」——而这一层**不做任何补救性的删除**。
//
// 拿掉修复的方式：在 `Delete` 的错误分支里加一句 `os.Remove`（或者干脆
// 忽略回收站的错、直接返回 nil）。前者是这一票明确要消灭的那一种，
// 后者会让界面说「已移到废纸篓」而文件还在原处。
func TestDeleteLeavesTheFileAloneWhenTheTrashFails(t *testing.T) {
	root := t.TempDir()
	song := songPath(t, root, "歌.mp3")
	trash := localmusictest.Fail(errors.New("mygo: cannot move it to the recycle bin: it would be deleted permanently"))

	err := Delete(trOf(t, song), []string{root}, trash)

	if err == nil {
		t.Fatal("回收站失败了，删除却报了成功")
	}
	if !exists(t, song) {
		t.Fatal("回收站失败之后文件被删了——这正是这一票要防的那一种")
	}
}

// TestDeleteSaysTheTrashIsNotAvailable 钉住「那个位置没有回收站」那一条。
//
// 判的是**哪一个错**：用户看到的是「这个位置没有回收站」，而不是一句
// 「没能移到系统废纸篓」——后者会让他以为是应用坏了，而前者他能自己
// 判断「那我去文件管理器里删」。
func TestDeleteSaysTheTrashIsNotAvailable(t *testing.T) {
	root := t.TempDir()
	song := songPath(t, root, "歌.mp3")

	err := Delete(trOf(t, song), []string{root}, localmusictest.NotTrashable())

	if !errors.Is(err, ErrNotTrashable) {
		t.Fatalf("错是 %v，want ErrNotTrashable", err)
	}
	if !exists(t, song) {
		t.Fatal("文件被删了")
	}
}

// TestDeleteSaysTheNameIsTaken 钉住「废纸篓里已经有同名的东西」那一条。
//
// 系统的原话是 `The file … already exists`——用户读不懂，而界面会把这一档
// 说成「废纸篓里已经有同名的东西」。
func TestDeleteSaysTheNameIsTaken(t *testing.T) {
	root := t.TempDir()
	song := songPath(t, root, "歌.mp3")

	err := Delete(trOf(t, song), []string{root}, localmusictest.NameConflict())

	if !errors.Is(err, ErrTrashNameConflict) {
		t.Fatalf("错是 %v，want ErrTrashNameConflict", err)
	}
	if !exists(t, song) {
		t.Fatal("文件被删了")
	}
}

// TestDeleteWithoutATrashIsAnError 钉住「没装回收站」那一条。
//
// ⚠️ **它是报错，不是「那就不删了当没事」**：界面上得有一句「文件未删除」
// 或者「这个应用没有装上系统回收站」——**绝不能**静悄悄地什么都不做
// （那会让用户以为删掉了）。
//
// ⚠️ **更不能**改成永久删除：那一票（#558）拍板的就是「可恢复」，而悄悄
// 换回去的话，界面上一个字都不会变。
func TestDeleteWithoutATrashIsAnError(t *testing.T) {
	root := t.TempDir()
	song := songPath(t, root, "歌.mp3")

	err := Delete(trOf(t, song), []string{root}, nil)

	if !errors.Is(err, ErrNoTrash) {
		t.Fatalf("错是 %v，want ErrNoTrash", err)
	}
	if !exists(t, song) {
		t.Fatal("没装回收站却把文件删了")
	}
}

// TestClassifyTrashErrorSortsTheSystemsWording 钉住「系统原话 → 这一层的
// 几档」那个归类。
//
// ⚠️ **三平台的原话完全不一样**（macOS 是 `localizedDescription`，Windows
// 是 HRESULT，Linux 是 GIO 的 `GError`），而**用户读不懂它们**。这一层把
// 它们归成几档，界面才能说人话——而**关键词只在这一处**，系统改文案的话
// 漏判的后果是「用户看到一句英文」，不是「文件被删了」。
func TestClassifyTrashErrorSortsTheSystemsWording(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"文件已经不在了", errors.New("The operation couldn’t be completed. No such file"), ErrMissing},
		{"no such file", errors.New("mygo: cannot move /x to the trash: no such file"), ErrMissing},
		// ⚠️ **macOS 的原话是弯撇号**（U+2019）：2026-10-26 的原生冒烟跑出来的
		// 就是这一句。不认它的话，macOS 上「文件已经不在了」那一档一条都
		// 命中不了——它会掉进「别的」那一档，被说成「没能移到系统废纸篓」。
		{"macOS 的弯撇号", errors.New("The file “歌.mp3” doesn’t exist."), ErrMissing},
		// ⚠️ **这一句是 Windows 框架那一版的原话**
		// （`mygo/internal/windows/trash.go`）：它在「本来会被永久删掉」的时候
		// 说的就是这一句（而它宁可失败也不让文件被删）。
		//
		// ⚠️ **Windows 那边没法在本机冒烟**（没有 Windows 机器、也没有 Wine），
		// 所以这一句就靠这一条钉着——它一旦改文案，这一档会静悄悄地掉进
		// 「别的」那一档，用户看到的是「没能移到系统废纸篓」而不是
		// 「那个位置没有回收站」。
		{"Windows 的原话", errors.New("mygo: cannot move C:\\music\\song.mp3 to the recycle bin: it would be deleted permanently"), ErrNotTrashable},
		{"同名的", errors.New("The file “歌.mp3” already exists."), ErrTrashNameConflict},
		{"别的", errors.New("some other failure"), ErrTrashFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ClassifyTrashError(c.err)
			if !errors.Is(got, c.want) {
				t.Errorf("错是 %v，want %v", got, c.want)
			}
		})
	}
}

// TestClassifyTrashErrorDoesNotSayItTwice 钉住「已经归过档的别再归一次」。
//
// 归两次的话，界面上会出现「文件未删除：文件未删除：…」这种两遍的话。
func TestClassifyTrashErrorDoesNotSayItTwice(t *testing.T) {
	once := ClassifyTrashError(errors.New("some other failure"))
	twice := ClassifyTrashError(once)

	if twice.Error() != once.Error() {
		t.Errorf("归了两次之后的话变了：%q → %q", once, twice)
	}
}
