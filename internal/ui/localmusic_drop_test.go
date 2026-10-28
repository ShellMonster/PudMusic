package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// 这一组验的是「把文件从访达 / 资源管理器拖进窗口」这一条路。所有测试都走
// 内存夹具：临时目录 + 内容随便的假音频文件，不碰真 B 站接口，也不起真窗口
// （无头 Tester 里没有系统事件循环，拖放事件本身发不进来）。
//
// 入口是 `Shell.handleFileDrop`，也就是 `OnFileDrop` 回调里的那一句。真窗口
// 上那一条注册线在 `Attach` 里，无法在无头环境里点；这里测的是它后面整套
// 「路径 → 设置 → 扫描 → 画面」的行为。

// dropToastTexts 读此刻挂着的提示。直接读状态而不是画一帧再找文字：toast 只有
// 两秒寿命，而这里要断言的是「刚拖完就说了这句话」。
//
// 不叫 `toastTexts`：那个名字已经被 `router_test.go` 用了（它收的是
// `[]toast`，不是外壳）。
func dropToastTexts(s *Shell) []string {
	s.toastMu.Lock()
	defer s.toastMu.Unlock()
	out := make([]string, len(s.toasts))
	for i, t := range s.toasts {
		out[i] = t.text
	}
	return out
}

// hasToast 报有没有一条提示包含 substr。
func hasToast(s *Shell, substr string) bool {
	for _, text := range dropToastTexts(s) {
		if strings.Contains(text, substr) {
			return true
		}
	}
	return false
}

// TestDroppingAnAudioFileAddsItsFolderAndShowsTheTrack 钉住主验收线：
// 拖一个音频文件进来，它出现在本地音乐里——不是「设置里多了个字符串」，
// 而是页面上真有那一行。
func TestDroppingAnAudioFileAddsItsFolderAndShowsTheTrack(t *testing.T) {
	tt, s, a, _ := shellOnLocalMusic(t)

	dir := t.TempDir()
	path := libSong(t, dir, "拖进来的.mp3")

	s.handleFileDrop([]string{path})

	if got := a.SettingsSnapshot().LocalMusicDirs; len(got) != 1 || got[0] != dir {
		t.Fatalf("设置里的目录是 %v，want [%v]", got, dir)
	}
	if !hasToast(s, "已加入本地音乐") {
		t.Errorf("拖完没有说「已加入本地音乐」，提示有 %q", dropToastTexts(s))
	}
	// 「同目录下的音频也会出现」这一句必须说：用户拖的是一首，列表里可能
	// 多出一整张专辑；不说的话看起来像是程序乱扫。
	if !hasToast(s, "同目录下的音频都会出现") {
		t.Errorf("没有说清楚同目录下的音频也会进来，提示有 %q", dropToastTexts(s))
	}

	waitFrames(t, tt, "拖进来的歌出现在本地音乐列表里", func() bool {
		return tt.HasText("拖进来的")
	})
}

// TestDroppingTheSameFolderTwiceAddsItOnce 钉住一次拖放里的去重：同一个新
// 目录下的两首歌只加一个根，而且不能把刚加的那个说成「已经在库里」。
func TestDroppingTheSameFolderTwiceAddsItOnce(t *testing.T) {
	tt, s, a, _ := shellOnLocalMusic(t)

	dir := t.TempDir()
	one := libSong(t, dir, "第一首.mp3")
	two := libSong(t, dir, "第二首.flac")

	s.handleFileDrop([]string{one, two})

	if got := a.SettingsSnapshot().LocalMusicDirs; len(got) != 1 {
		t.Fatalf("同一个目录拖了两首，设置里有 %d 个：%v", len(got), got)
	}
	if hasToast(s, "已经在本地音乐里") {
		t.Errorf("刚加进来的目录被说成「已经在了」，提示有 %q", dropToastTexts(s))
	}

	waitFrames(t, tt, "两首都在列表里", func() bool {
		return tt.HasText("第一首") && tt.HasText("第二首")
	})
}

// TestDroppingADirectoryAddsItAsARoot 钉住「拖文件夹」和「添加目录」同义。
func TestDroppingADirectoryAddsItAsARoot(t *testing.T) {
	tt, s, a, _ := shellOnLocalMusic(t)

	dir := t.TempDir()
	libSong(t, dir, "目录里的.mp3")

	s.handleFileDrop([]string{dir})

	if got := a.SettingsSnapshot().LocalMusicDirs; len(got) != 1 || got[0] != dir {
		t.Fatalf("设置里的目录是 %v，want [%v]", got, dir)
	}
	waitFrames(t, tt, "拖进来的目录被扫出了歌", func() bool {
		return tt.HasText("目录里的")
	})
}

// TestDroppingAnUnsupportedFormatSaysWhy 钉住「不是静默丢掉」：用户得知道
// 为什么没收，而且知道本地音乐到底收什么。
func TestDroppingAnUnsupportedFormatSaysWhy(t *testing.T) {
	_, s, a, _ := shellOnLocalMusic(t)

	path := filepath.Join(t.TempDir(), "说明.txt")
	if err := os.WriteFile(path, []byte("not music"), 0o644); err != nil {
		t.Fatal(err)
	}

	s.handleFileDrop([]string{path})

	if got := a.SettingsSnapshot().LocalMusicDirs; len(got) != 0 {
		t.Errorf("不支持的文件也被加进设置：%v", got)
	}
	if !hasToast(s, "说明.txt") || !hasToast(s, "不是支持的格式") {
		t.Fatalf("没有说清楚为什么没收，提示有 %q", dropToastTexts(s))
	}
	// 光说「不支持」不够——.txt 的用户不知道本地音乐收什么。
	if !hasToast(s, ".mp3") {
		t.Errorf("提示里没有支持的扩展名清单，提示有 %q", dropToastTexts(s))
	}
	if n := len(a.LocalMusicState().Tracks); n != 0 {
		t.Errorf("不支持的文件进了本地音乐列表：%d 首", n)
	}
}

// TestDroppingAnAlreadyKnownFolderDoesNotDuplicate 钉住「已经在库里」那一支：
// 不重复写设置、也不重复列一行。
func TestDroppingAnAlreadyKnownFolderDoesNotDuplicate(t *testing.T) {
	dir := t.TempDir()
	path := libSong(t, dir, "已经在的.mp3")
	tt, s, a, _ := shellOnLocalMusic(t, dir)

	// 这一首是在页面扫完之后才放进目录的：测试里没装文件监听，只有这次拖放
	// 引起的重扫才会把它带出来——于是「重扫真的发生了」有了判据，而不是
	// 「等了等，列表里本来就有那一首」。
	libSong(t, dir, "刚放进去的.mp3")

	s.handleFileDrop([]string{path})

	if got := a.SettingsSnapshot().LocalMusicDirs; len(got) != 1 {
		t.Errorf("库里已有的目录又被加了一次：%v", got)
	}
	if !hasToast(s, "已经在本地音乐里") {
		t.Errorf("没有说「已经在本地音乐里」，提示有 %q", dropToastTexts(s))
	}
	waitFrames(t, tt, "重扫把刚放进目录的那首带出来了", func() bool {
		return tt.HasText("刚放进去的")
	})
	if n := len(a.LocalMusicState().Tracks); n != 2 {
		t.Errorf("重扫之后列表里有 %d 首，want 2（原有的 + 刚放进去的）", n)
	}
}

// TestDroppingDoesNotTouchPlayback 钉住「拖文件不等于替当前这一首」。
//
// 拖放只该动本地音乐目录；队列里正在放的、播放状态，一个都不许变。
func TestDroppingDoesNotTouchPlayback(t *testing.T) {
	_, s, a, _ := shellOnLocalMusic(t)

	playing := app.LocalItem("/别处/正在放的那首.mp3")
	if _, ok := a.Queue().Reset(playing); !ok {
		t.Fatal("队列没摆上")
	}
	before, ok := a.Queue().Current()
	if !ok || before.Identity() != playing.Identity() {
		t.Fatalf("队列当前项是 %+v，want %+v", before, playing)
	}

	dir := t.TempDir()
	path := libSong(t, dir, "拖进来的.mp3")
	s.handleFileDrop([]string{path})

	after, ok := a.Queue().Current()
	if !ok || after.Identity() != before.Identity() {
		t.Errorf("拖放把当前播放换成了 %+v，want 还是 %+v", after, before)
	}
	if n := a.Queue().Len(); n != 1 {
		t.Errorf("拖放往队列里加了东西：现在 %d 项", n)
	}
	if st := a.Snapshot(); st.Playing || st.HasTrack {
		t.Errorf("拖放动了播放状态：Playing=%v HasTrack=%v", st.Playing, st.HasTrack)
	}
}

// TestDroppingWhileSettingsAreReadOnlySaysWhy 钉住只读设置下的那句话。
//
// 只读时 `UpdateSettings` 给的是 `ErrSettingsFromNewer`——那句话对用户没有
// 意义，界面上要说清楚「加不进去」，而不是一声不响。
func TestDroppingWhileSettingsAreReadOnlySaysWhy(t *testing.T) {
	settingsPath := writeReadOnlySettings(t)
	b := newTestShell(t,
		mounted(func(t *testing.T, a *app.App) {
			if err := a.MountSettings(settingsPath); !errors.Is(err, app.ErrSettingsFromNewer) {
				t.Fatalf("拿到的是 %v，want ErrSettingsFromNewer", err)
			}
			a.MountLocalMusic(app.LocalMusic{})
		}),
		withoutTester())
	s := b.s

	path := libSong(t, t.TempDir(), "加不进去.mp3")
	s.handleFileDrop([]string{path})

	if !hasToast(s, "设置只读") {
		t.Errorf("只读设置下没有说清楚，提示有 %q", dropToastTexts(s))
	}
	if got := b.a.SettingsSnapshot().LocalMusicDirs; len(got) != 0 {
		t.Errorf("只读设置被改了：%v", got)
	}
}

// TestPlanFileDropMatchesRootsByRealPath 钉住「同一个目录的不同写法算一个」：
// 设置里记的是符号链接，用户从真实路径把文件拖进来，不该再加一个根。
func TestPlanFileDropMatchesRootsByRealPath(t *testing.T) {
	dir := t.TempDir()
	path := libSong(t, dir, "歌.mp3")
	link := filepath.Join(t.TempDir(), "链接")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("这个系统上建不了符号链接：%v", err)
	}

	plan := planFileDrop([]string{path}, []string{link})
	if len(plan.added) != 0 || len(plan.known) != 1 {
		t.Errorf("同一个目录的两种写法没认成同一个：added=%v known=%v", plan.added, plan.known)
	}
}

// TestPlanFileDropAcceptsUppercaseExtensions 钉住扩展名大小写不敏感。
// `localmusic.Scan` 是 lower 之后比的，拖放这一侧不能比它更严。
func TestPlanFileDropAcceptsUppercaseExtensions(t *testing.T) {
	dir := t.TempDir()
	path := libSong(t, dir, "大写的.MP3")

	plan := planFileDrop([]string{path}, nil)
	if len(plan.added) != 1 || len(plan.rejected) != 0 {
		t.Errorf("大写扩展名被拒了：added=%v rejected=%v", plan.added, plan.rejected)
	}
}
