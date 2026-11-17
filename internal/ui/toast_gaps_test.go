package ui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo"
)

// 这一组补的是「动作发生了，界面上却一个字都没有」的那几个口子。
// 判据是用户能看到的那句话，不是内部调了哪个函数。

// TestFilesThatCannotPlaySaySo 选完文件之后起播失败，要说清楚是文件放不了。
//
// 出过的问题：文件框选回来的路径直接交给 `Enqueue`，而空队列时它会顺手起播；
// 返回值没人看，于是用户选了一首坏文件，界面上什么反应都没有。
func TestFilesThatCannotPlaySaySo(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	s := &Shell{app: a}

	s.enqueuePicked([]string{filepath.Join(t.TempDir(), "没有这一首.m4a")})

	if !hasToast(s, "放不了这些文件") {
		t.Fatalf("选了放不了的文件，说的是 %q，want 一句「放不了这些文件」", dropToastTexts(s))
	}
}

// TestAFailingAudioPickerSaysSo 选歌的文件框打不开时要说一句。
//
// ⚠️ 取消返回空列表和 nil；打不开才带错。
func TestAFailingAudioPickerSaysSo(t *testing.T) {
	sh := newTestShell(t, withoutTester())
	s := sh.s
	s.pickAudio = func(*mygo.Window) ([]string, error) {
		return nil, errors.New("没有权限打开文件框")
	}

	s.pickAndEnqueue()

	waitFor(t, "选歌的文件框打不开要说一句", func() bool {
		return hasToast(s, "打不开文件框")
	})
}

// TestAFailedDownloadSaysSoWhileTheUserIsHere 用户还在应用里时，下载失败要弹一条提示。
//
// 下载记录页上那一行会变红，但用户可能正在别的页面。只有读屏软件念的那一句
// 不够，看不见屏幕的人之外，普通用户什么也得不到。
func TestAFailedDownloadSaysSoWhileTheUserIsHere(t *testing.T) {
	p := newFocusProbe(t)
	p.add(true)

	sh := newTestShell(t, withoutTester())
	s := sh.s
	old := swapMainShell(s)
	t.Cleanup(func() { swapMainShell(old) })

	notifyDownloadFailed("BV1x|cid1", "晴天", "网断了")

	if !hasToast(s, "《晴天》下载失败：网断了") {
		t.Fatalf("用户就在应用里，下载失败却没人说一句（提示：%q）", dropToastTexts(s))
	}
}

// TestAFailedDownloadNotifiesWhenTheUserIsAway 用户离开时，下载失败走系统通知。
//
// 这一条和完成那一条同一个判据：人在别处时，应用内提示他看不见。
func TestAFailedDownloadNotifiesWhenTheUserIsAway(t *testing.T) {
	p := newFocusProbe(t)
	p.addHidden(false)

	sh := newTestShell(t, withoutTester())
	s := sh.s
	old := swapMainShell(s)
	t.Cleanup(func() { swapMainShell(old) })

	grace := catchGrace(t)
	got := captureNotify(t, func() {
		notifyDownloadFailed("BV1x|cid1", "晴天", "网断了")
	})

	if n := grace.waitPending(t); n != 1 {
		t.Fatalf("宽限期排了 %d 次，want 1 次", n)
	}
	grace.fire(t)

	if got.len() != 1 {
		t.Fatalf("用户不在应用里，发了 %d 条通知，want 1 条", got.len())
	}
	n := got.at(0)
	if n.title != "下载失败" || !strings.Contains(n.body, "晴天") {
		t.Errorf("通知是 %q / %q，want 「下载失败」和带歌名的一句", n.title, n.body)
	}
	if hasToast(s, "下载失败") {
		t.Error("用户不在应用里，却只弹了应用内提示（那一扇窗他看不见）")
	}
}

// TestRescanSaysWhenItStartsAndFinishes 重新扫描要有头和尾两句话。
//
// 列表非空时这一页没有任何「正在扫」的标志，按下之后不说一句，用户会以为
// 按钮是死的；扫完了不说一句，他也不知道结果。
func TestRescanSaysWhenItStartsAndFinishes(t *testing.T) {
	dir := t.TempDir()
	libSong(t, dir, "晴天.mp3")
	tt, s, a, _ := shellOnLocalMusic(t, dir)

	if err := tt.Click("重新扫描"); err != nil {
		t.Fatalf("点不到「重新扫描」：%v（文字 %q）", err, tt.Texts())
	}
	if !hasToast(s, "正在重新扫描") {
		t.Fatalf("按下重新扫描之后没有说一句（提示：%q）", dropToastTexts(s))
	}

	waitFrames(t, tt, "扫描收尾", func() bool { return !a.LocalMusicState().Scanning })
	waitFrames(t, tt, "扫描完成说一句", func() bool { return hasToast(s, "扫描完成，共 1 首") })
}
