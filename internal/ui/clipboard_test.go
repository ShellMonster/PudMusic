package ui

// 「复制当前歌曲信息和封面」这一族（票 #556）。
//
// # 这一族量的是**剪贴板里到底有什么**
//
// 不是「调了某个函数」（那是实现），而是：
//
//   - **文本**：粘出来是什么（逐字），有没有多余的前缀；
//   - **图**：粘出来的是不是一张**能被解开的 PNG**（别的应用要能贴）；
//   - **没有封面时**：菜单里那一项是不是灰的、有没有说清楚为什么；
//   - **失败时**：有没有看得见的反馈（不静默）。
//
// ⚠️ **剪贴板走接缝**（`Shell.writeClipboardText` / `Shell.writeClipboardImage`）：
// 无头 Tester 里 `mygo.Clipboard.Write*` 会 panic（`needsApp`，见 `mygo/app.go:128`），
// 所以这里换成记下内容的桩（和 `openExternal` 那个接缝同一个理由）。
//
// ⚠️ **不打真 B 站**：封面夹具是本地的 `httptest` 服务。

import (
	"bytes"
	"errors"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// ---- 剪贴板桩 ----

// clipStub 是剪贴板那一头的桩：记下写进去的东西。
type clipStub struct {
	mu    sync.Mutex
	text  string
	image []byte
	// textErr / imageErr 是「写失败」那一档：让测试能造出失败来。
	textErr  error
	imageErr error
	// textCalls / imageCalls 数着写了几次。
	textCalls  int
	imageCalls int
}

func (c *clipStub) writeText(text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.textCalls++
	if c.textErr != nil {
		return c.textErr
	}
	c.text = text
	return nil
}

func (c *clipStub) writeImage(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.imageCalls++
	if c.imageErr != nil {
		return c.imageErr
	}
	c.image = data
	return nil
}

// textIs 报剪贴板里的文本是什么（线程安全：后台那条线也会写）。
func (c *clipStub) textIs() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.text
}

// imageIs 报剪贴板里的图是什么（线程安全）。
func (c *clipStub) imageIs() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.image
}

// ---- 摆场 ----

// copyShell 造一个外壳，剪贴板换成桩，状态是给的那一份。
//
// ⚠️ **只画播放条那一块**：这一族问的是「封面那一层的右键菜单」，而整窗里
// 播放条只是最底下那一行——隔离出来量，归因才说得清（和 `coverBarTester`
// 同一个理由）。
func copyShell(t *testing.T, st app.State) (*Shell, *mygoui.Tester, *clipStub) {
	t.Helper()
	clip := &clipStub{}
	sh := newTestShell(t, at(760, coverWinH), withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.wake.set(c)
			s.viewPlayBar(c, st)
			// ⚠️ **提示也要画**：整窗里提示是 `Shell.View` 画的，而这里绕开了
			// 整窗——不补这一句，「失败那一句说出来了」就永远看不见（提示是
			// 状态，状态在，而画面上没有）。
			s.viewToasts(c)
		}
	}), stubbed(func(_ *testing.T, s *Shell) {
		s.writeClipboardText = clip.writeText
		s.writeClipboardImage = clip.writeImage
	}))
	return sh.s, sh.tt, clip
}

// ---- 文本 ----

// TestTheSongInfoTextHasEveryField 断**文本里有哪几行**。
//
// ⚠️ **判据是逐字**：这一份是**给人粘进聊天框里**的，多一个前缀（"歌名："前面
// 有一句「正在播放：」）或者少一行（UP 主那行没出现）用户都看得见。
//
// ⚠️ **顺序也是判据**：稳定的格式意味着同样的一首歌每次复制出来一模一样。
func TestTheSongInfoTextHasEveryField(t *testing.T) {
	st := app.State{
		HasTrack: true,
		Title:    "一首歌",
		Artist:   "某个 UP 主",
		Duration: 3*time.Minute + 5*time.Second,
		BiliURL:  "https://www.bilibili.com/video/BV1xx411c7mD",
		Path:     "",
	}
	got := songInfoText(st)
	want := "歌名：一首歌\n" +
		"UP主：某个 UP 主\n" +
		"时长：3:05\n" +
		"BVID：BV1xx411c7mD\n" +
		"链接：https://www.bilibili.com/video/BV1xx411c7mD"
	if got != want {
		t.Errorf("复制出去的是\n%q\nwant\n%q", got, want)
	}
}

// TestTheSongInfoTextOmitsTheFieldsThatAreNotThere 断**没有的字段整行不出现**。
//
// ⚠️ **本地文件那一档**：它没有 UP 主、没有 B 站地址，只有路径。而**留一行空的
// 「UP主：」比不出现更难读**——用户会以为程序没填上。
func TestTheSongInfoTextOmitsTheFieldsThatAreNotThere(t *testing.T) {
	got := songInfoText(app.State{
		HasTrack: true,
		Title:    "本地的一首歌",
		Path:     "/Users/me/Music/本地的一首歌.flac",
	})
	want := "歌名：本地的一首歌\n路径：/Users/me/Music/本地的一首歌.flac"
	if got != want {
		t.Errorf("本地那一首复制出去的是\n%q\nwant\n%q", got, want)
	}
}

// TestTheSongInfoTextDoesNotCallAnAudioIDSomethingItIsNot 断**音频条目的那一串
// id 不被当成 BVID 报出去**。
//
// ⚠️ **音频条目的地址是 `audio/au<sid>`**（`app.BiliLink` 的第二种形状，见
// `internal/app/unplayable.go:345`）——那一串是**音频 id**，不是稿件号（BVID）。
// 报成「BVID：au123」是**说错了**：用户拿着它去 B 站搜，搜出来的是别的东西。
func TestTheSongInfoTextDoesNotCallAnAudioIDSomethingItIsNot(t *testing.T) {
	got := songInfoText(app.State{
		HasTrack: true,
		Title:    "一首音频",
		BiliURL:  "https://www.bilibili.com/audio/au123",
	})
	if strings.Contains(got, "BVID：") {
		t.Errorf("音频 id 被当成 BVID 报出去了：\n%q", got)
	}
	if !strings.Contains(got, "链接：https://www.bilibili.com/audio/au123") {
		t.Errorf("音频条目的链接没报出来：\n%q", got)
	}
}

// TestTheSongInfoTextUsesTheNameOnScreen 断**复制出去的歌名和界面上摆的是同一个**。
//
// ⚠️ **这一条是「同一个东西」那一档**：界面上摆的是清洗过的歌名（去掉了
// 「【4K修复】」那些视频痕迹），而复制出去的要是**同一个**——复制出来是
// 「【4K修复】晴天」而界面上写着「晴天」，用户看到的是两首歌。
func TestTheSongInfoTextUsesTheNameOnScreen(t *testing.T) {
	got := songInfoText(app.State{HasTrack: true, Title: "【4K修复】晴天"})
	if !strings.Contains(got, "歌名：晴天") {
		t.Errorf("复制出去的歌名不是界面上那个：%q", got)
	}
	if strings.Contains(got, "4K") {
		t.Errorf("复制出去的歌名里还留着视频痕迹：%q", got)
	}
}

// TestTheSongInfoTextHasNoExtraPrefix 断**没有多余的前缀**。
//
// 票面原话：「粘贴得到完整、无多余前缀的信息」。多出来的一行（比如「正在播放：」）
// 会让用户粘进聊天框之后还得手动删一行。
func TestTheSongInfoTextHasNoExtraPrefix(t *testing.T) {
	got := songInfoText(app.State{HasTrack: true, Title: "一首歌"})
	first := strings.SplitN(got, "\n", 2)[0]
	if !strings.HasPrefix(first, "歌名：") {
		t.Errorf("第一行是 %q——它该是「歌名：…」那一行，前面不该有别的话", first)
	}
}

// ---- 菜单 ----

// TestTheCoverMenuOffersTheTwoCopyActions 断**菜单里有那两项**。
//
// ⚠️ **每一项都要 `m.Item` 一遍**（框架的规矩，见 `mygo/ui/menu.go`）：少建一项，
// 那一项就永远点不到。所以这一条钉的是**菜单里真的有这两项**。
func TestTheCoverMenuOffersTheTwoCopyActions(t *testing.T) {
	_, tt, _ := copyShell(t, app.State{
		HasTrack: true, Title: "一首歌", CoverURL: "https://example.invalid/cover.png",
	})

	if err := tt.RightClick("当前封面"); err != nil {
		t.Fatalf("右键不了封面：%v（文字：%q）", err, tt.Texts())
	}
	got := tt.Menu()
	for _, want := range []string{copyInfoLabel, copyCoverLabel} {
		if !slicesContains(got, want) {
			t.Errorf("菜单里没有 %q：%q", want, got)
		}
	}
}

// TestTheCopyCoverItemIsGreyWithoutACover 断**没有封面时那一项是灰的，而且说清楚
// 为什么**。
//
// ⚠️ **按不动而不说为什么等于让用户猜**（同一句话在 `widgets/list/menu.go` 的
// `note` 那一栏写着）。所以除了那一项是灰的，末尾还要补一句说明。
//
// ⚠️ **本地文件永远是这一档**（见 `app.State.CoverURL`）——而本地文件是常客。
func TestTheCopyCoverItemIsGreyWithoutACover(t *testing.T) {
	_, tt, _ := copyShell(t, app.State{
		HasTrack: true, Title: "本地的一首歌", Source: app.SourceLocal,
	})

	if err := tt.RightClick("当前封面"); err != nil {
		t.Fatalf("右键不了封面：%v（文字：%q）", err, tt.Texts())
	}
	got := tt.Menu()
	if !slicesContains(got, copyCoverLabel) {
		t.Fatalf("菜单里连「复制封面」那一项都没有：%q", got)
	}
	if !slicesContains(got, copyNoCoverNote) {
		t.Errorf("菜单末尾没有说清楚为什么不能复制：%q", got)
	}
	// 灰的那一项按不动：框架的 `ChooseMenuItem` 会对禁用的项报错。
	if err := tt.ChooseMenuItem(copyCoverLabel); err == nil {
		t.Errorf("没有封面时「复制封面」还能按——它该是灰的")
	}
}

// ---- 动作真的发生 ----

// TestChoosingCopySongInfoPutsTheTextOnTheClipboard 断**走菜单按下去，文本真的
// 进剪贴板**。
//
// ⚠️ **走菜单而不是直接调函数**：菜单是这趟全部建出来的（框架的规矩），少建一项
// 那一项就永远点不到——只有真的按一次才断得出来。
func TestChoosingCopySongInfoPutsTheTextOnTheClipboard(t *testing.T) {
	_, tt, clip := copyShell(t, app.State{
		HasTrack: true, Title: "一首歌", Artist: "某个 UP 主",
		Duration: 2 * time.Minute,
		BiliURL:  "https://www.bilibili.com/video/BV1xx411c7mD",
	})

	if err := tt.RightClick("当前封面"); err != nil {
		t.Fatalf("右键不了封面：%v", err)
	}
	if err := tt.ChooseMenuItem(copyInfoLabel); err != nil {
		t.Fatalf("选不了 %q：%v（菜单：%q）", copyInfoLabel, err, tt.Menu())
	}

	got := clip.textIs()
	want := "歌名：一首歌\nUP主：某个 UP 主\n时长：2:00\n" +
		"BVID：BV1xx411c7mD\n链接：https://www.bilibili.com/video/BV1xx411c7mD"
	if got != want {
		t.Errorf("剪贴板里是\n%q\nwant\n%q", got, want)
	}
}

// TestAFailedCopySaysSo 断**写失败有看得见的反馈**。
//
// ⚠️ **票面原话**：「编码失败有可见反馈，不静默失败」。静默的那一种最坏：用户
// 按了「复制歌曲信息」，什么都没有，而他还以为复制好了。
func TestAFailedCopySaysSo(t *testing.T) {
	_, tt, clip := copyShell(t, app.State{HasTrack: true, Title: "一首歌"})
	clip.textErr = errors.New("剪贴板不可用")

	if err := tt.RightClick("当前封面"); err != nil {
		t.Fatalf("右键不了封面：%v", err)
	}
	if err := tt.ChooseMenuItem(copyInfoLabel); err != nil {
		t.Fatalf("选不了 %q：%v", copyInfoLabel, err)
	}

	// 那一句提示是**画面上**的一句（读屏也要念，announce.go）。
	waitFrames(t, tt, "失败那一句说出来了", func() bool {
		return tt.HasText("复制不了歌曲信息")
	})
}

// ---- 封面 ----

// TestCopyCoverPutsAPNGOnTheClipboard 断**剪贴板里那张图能被解开**。
//
// ⚠️ **判据是「外部应用能粘」**：别的应用要的是一张**合法的 PNG**（`png.Decode`
// 能解开），而框架的 `Clipboard.WriteImage` 自己也会验这一遍——不是 PNG 就
// 直接返回错（`mygo/modules.go:184`）。
//
// ⚠️ **夹具是本地的 httptest 服务**：不打真 B 站（那条禁令，AGENTS.md §6）。
func TestCopyCoverPutsAPNGOnTheClipboard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(coverPNG(t))
	}))
	defer srv.Close()

	_, tt, clip := copyShell(t, app.State{
		HasTrack: true, Title: "一首歌", CoverURL: srv.URL + "/cover.png",
	})

	if err := tt.RightClick("当前封面"); err != nil {
		t.Fatalf("右键不了封面：%v", err)
	}
	if err := tt.ChooseMenuItem(copyCoverLabel); err != nil {
		t.Fatalf("选不了 %q：%v（菜单：%q）", copyCoverLabel, err, tt.Menu())
	}

	// 编码在后台那条线上（要下网络 + 解一张图），所以等它。
	var data []byte
	waitFor(t, "封面进了剪贴板", func() bool {
		data = clip.imageIs()
		return len(data) > 0
	})
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("剪贴板里那张不是一张能解开的 PNG：%v", err)
	}
	if img.Bounds().Dx() <= 0 || img.Bounds().Dy() <= 0 {
		t.Errorf("剪贴板里那张图没有像素：%v", img.Bounds())
	}
}

// TestCopyCoverWithoutACoverWritesNothing 断**没有封面时不往剪贴板里写图**。
//
// ⚠️ **票面原话**：「无封面时不会写入空/损坏图片」。写一份空字节或者一段坏
// 字节进去，用户在别的应用里粘出来的是一张**打不开的图**——比什么都不写更坏。
func TestCopyCoverWithoutACoverWritesNothing(t *testing.T) {
	_, tt, clip := copyShell(t, app.State{
		HasTrack: true, Title: "本地的一首歌", Source: app.SourceLocal,
	})

	if err := tt.RightClick("当前封面"); err != nil {
		t.Fatalf("右键不了封面：%v", err)
	}
	// 那一项是灰的：框架不让你选一个禁用的项（`ChooseMenuItem` 会报错）。
	if err := tt.ChooseMenuItem(copyCoverLabel); err == nil {
		t.Fatalf("没有封面时「复制封面」还能按——它该是灰的")
	}
	if len(clip.imageIs()) != 0 {
		t.Errorf("没有封面却往剪贴板里写了 %d 字节的图", len(clip.imageIs()))
	}
}

// TestAFailedCoverCopySaysSo 断**编码失败有看得见的反馈**。
//
// ⚠️ **这一档是「封面取不到」**：地址指着一个不回来的服务，于是这一趟拿不到
// 字节、编不出 PNG——**这时候必须说一句**，不能什么都不做。
func TestAFailedCoverCopySaysSo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "没了", http.StatusNotFound)
	}))
	defer srv.Close()

	_, tt, clip := copyShell(t, app.State{
		HasTrack: true, Title: "一首歌", CoverURL: srv.URL + "/404.png",
	})

	if err := tt.RightClick("当前封面"); err != nil {
		t.Fatalf("右键不了封面：%v", err)
	}
	if err := tt.ChooseMenuItem(copyCoverLabel); err != nil {
		t.Fatalf("选不了 %q：%v", copyCoverLabel, err)
	}

	waitFrames(t, tt, "失败那一句说出来了", func() bool {
		return tt.HasText("复制不了封面")
	})
	if len(clip.imageIs()) != 0 {
		t.Errorf("失败之后却往剪贴板里写了 %d 字节", len(clip.imageIs()))
	}
}

// ---- 键盘 ----

// TestTheMenuKeyOpensTheCopyMenuOnTheCover 断**键盘够得着**。
//
// ⚠️ **判据是「焦点落在封面上时按菜单键能开出这一份菜单」**：框架的 `menuKey`
// 找的是「焦点链上最里层那个带菜单的元素」（`mygo/ui/menu.go` 的 `menuTarget`），
// 而焦点链上**得先有封面那一层**——所以那一层必须是可聚焦的。
func TestTheMenuKeyOpensTheCopyMenuOnTheCover(t *testing.T) {
	_, tt, _ := copyShell(t, app.State{
		HasTrack: true, Title: "一首歌", CoverURL: "https://example.invalid/cover.png",
	})
	tt.SetFocused(true)

	// Tab 到封面那一层（焦点顺序取决于树，最多二十下）。
	reached := false
	for range 20 {
		tt.Key(0, mygoui.KeyTab)
		tt.Frame()
		if tt.Focused(copyCoverActionsLabel) {
			reached = true
			break
		}
	}
	if !reached {
		t.Fatalf("Tab 二十下都没走到封面那一层（键盘够不着这一份菜单）：文字有 %q", tt.Texts())
	}

	tt.Key(0, mygoui.KeyContextMenu)
	tt.Frame()
	got := tt.Menu()
	if !slicesContains(got, copyInfoLabel) {
		t.Errorf("菜单键开出来的不是这一份菜单：%q", got)
	}
}

// ---- 小工具 ----

// slicesContains 报一个切片里有没有那一项。
func slicesContains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
