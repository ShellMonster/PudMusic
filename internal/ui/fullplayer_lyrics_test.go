// 满屏播放器接上歌词之后的样子（票 #236，UX 报告 F-08 + B3 + B4）：
//
//	满屏能显示歌词，「显示歌词」那枚开关真的管它
//	没有歌词时是空状态（「暂无歌词」+ 一枚「找歌词」），不是整屏空白
//	偏好面板有标题、能关、而且和齿轮对齐
//
// 这一族走仓库现成的接缝：外壳与测试器基座（shelltest_test.go）、假时钟
// （clock.go）、`BIU_SHOT_DIR` 出图。
package ui

import (
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// fullTestLRC 是这一组测试用的那份歌词：四句，各带一个时间戳。
//
// ⚠️ **句数要够**：一屏放得下十来句才看得出「滚到中间」那件事，而四句是
// 「一屏放得下」的最小值——再多几句，这一组测试就慢起来了。
const fullTestLRC = "[00:00.00]第一句\n[00:01.00]第二句\n[00:02.00]第三句\n[00:03.00]第四句\n"

// fullLyricsTester 造一个开着歌、**暂停着**、挂着假歌词源的外壳 + Tester。
//
// ⚠️ **假源是必须的**：满屏那个「找歌词」的键会真的去搜（那是它存在的意义），
// 而不挂源的话那一次搜索会打到真网上去——验收一律走 fixture（AGENTS §6）。
//
// ⚠️ **暂停是必须的**：歌在放的时候播放位置一直在走，于是「正在唱的是第几句」
// 每隔一秒换一句，而换一句就要重新对一次滚动的目标——界面于是永远在要帧
// （`settleFull` 等不到它停）。歌词那一列的滚动要停下来，位置就得停住。
func fullLyricsTester(t *testing.T) (*Shell, *mygoui.Tester, *fakeClock) {
	t.Helper()
	s, tt, fc := fullTester(t)
	s.app.Pause()
	s.app.SetWake(s.wake.invalidate)
	s.app.MountLyricSources(lrclibSource(cand("1", "第一句")))
	return s, tt, fc
}

// fullNoLyricsPrefs 把「显示歌词」关掉——满屏那一屏于是不画歌词。
//
// ⚠️ **这是给「量界面」的那些测试用的**：歌词是**内容**，界面藏起来之后它还在
// （见 `TestTheLyricsStayWhenTheInterfaceHides`），而那些测试量的是「界面上还
// 有没有亮像素」——歌词的字也在那儿，会把量出来的东西搅浑。
func fullNoLyricsPrefs(t *testing.T, s *Shell) {
	t.Helper()
	p := app.DefaultFullPlayerPrefs()
	p.ShowLyrics = false
	s.app.SetFullPlayerPrefs(p)
}

// TestTheLyricsStayWhenTheInterfaceHides 钉住「歌词是内容，不是界面」。
//
// 满屏那个「三秒不动就藏起来」藏的是顶栏、频谱和底栏那些**控件**；封面和歌词
// 是**内容**——用户盯着歌词读词，指针不动三秒就把字也收走的话，满屏那一屏就
// 只剩一张封面了（而满屏存在的理由正是看歌词）。
//
// 判据是**画面上还有没有亮像素**：正在唱的那一句是全亮的（别的句是 60%），
// 界面藏起来之后它还在，就说明歌词没跟着藏。
func TestTheLyricsStayWhenTheInterfaceHides(t *testing.T) {
	s, tt, fc := fullLyricsTester(t)
	s.app.PutLyrics(fullTestLRC, "")
	openFull(t, tt)
	tt.Move(600, 400)
	tt.Frame()

	advanceFull(s, tt, fc, biuHideSeconds*time.Second+time.Second)
	if !s.fullHidden() {
		t.Fatal("三秒过去了界面没有藏起来")
	}
	line, ok := tt.Find("第一句")
	if !ok {
		t.Fatalf("找不到「第一句」，文字有 %q", tt.Texts())
	}
	if n := brightIn(tt.Image(), line); n == 0 {
		t.Errorf("界面藏起来之后歌词一个亮像素都没有了——歌词是内容，不该跟着控件一起藏")
	}
}

// TestTheLyricsStayClearOfThePartsPanel 钉住「歌词不许被分集面板从中间切断」。
//
// ⚠️ **这一条是评审抓出来的**：分集面板是**贴右边的一竖条**（280 宽，
// `viewFullPartsPanel` 的 `Absolute().Right(0)`），而它开着的时候浮在歌词那一列
// 上面——歌词从中间被切掉（实测：歌词那一行的右沿 1128，面板的左沿 920，
// 超出 208 像素）。这一票之前满屏中间只有一张居中的封面（408..792），不碰
// 这一块，所以是这个改动引入的。
//
// 修法：歌词那一列的右边界**让开面板**（`viewFullStage`）。判据是「歌词的
// 文字不许越过面板的左沿」。
func TestTheLyricsStayClearOfThePartsPanel(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	s.app.PutLyrics(fullTestLRC, "")
	openFull(t, tt)
	settleFull(t, tt)

	// 开分集面板（和 `fullplayer_parts_test.go` 那一族一样直接开那一档）。
	s.full.partsOpen = true
	tt.Frame()
	settleFull(t, tt)

	panel, ok := tt.Find("分集列表")
	if !ok {
		t.Fatalf("画面上找不到分集面板，文字有 %q", tt.Texts())
	}
	lyrics, ok := tt.Find("满屏歌词")
	if !ok {
		t.Fatalf("画面上找不到歌词那一列，文字有 %q", tt.Texts())
	}
	// 「满屏歌词」那个名字在**内容盒**上（壳 → 内容盒 → 歌词），而字就画在内容盒
	// 里——所以字最远能到的地方就是内容盒的右沿。
	//
	// ⚠️ **不能再减一份内边距**：以前那个名字在壳上，壳的右边有一份 48 的留白
	// （`fullStagePadX`），字到「壳右沿 − 48」为止；现在名字在内容盒上，内容盒
	// 本身就已经是去掉留白之后的那一块了。
	textRight := lyrics.X + lyrics.W
	if textRight > panel.X {
		t.Errorf("歌词的字到 %.1f，分集面板从 %.1f 开始——面板会把歌词从中间切断",
			textRight, panel.X)
	}
}

// TestTheCoverIsCentredInTheWindowWhenTheLyricsAreOff 钉住「关掉歌词那一条
// 路径，封面和这一票之前**逐像素一样**」。
//
// ⚠️ **这一条是评审抓出来的**：原来那一条路径把封面放在「顶栏与频谱之间那一段」
// 里居中，于是它**上移了 40 像素**（y=216，而这一票之前是整窗居中的 256）——
// 而代码注释和报告都写着「和这一票之前一模一样」。
//
// 判据是**两个方向都在窗口中线上**（x 和 y）：只量 x 的话，上移 40 像素照样
// 全绿。
func TestTheCoverIsCentredInTheWindowWhenTheLyricsAreOff(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	fullNoLyricsPrefs(t, s)
	openFull(t, tt)
	settleFull(t, tt)

	cover, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("满屏上找不到封面，文字有 %q", tt.Texts())
	}
	if d := absF(cover.X + cover.W/2 - float32(fullWinW)/2); d > 1 {
		t.Errorf("封面中心在 x=%.1f，窗口中线是 %d——它没有在整窗里居中",
			cover.X+cover.W/2, fullWinW)
	}
	if d := absF(cover.Y + cover.H/2 - float32(fullWinH)/2); d > 1 {
		t.Errorf("封面中心在 y=%.1f，窗口的中线是 y=%.1f——它没有在整窗里居中"+
			"（这一票之前它就是整窗居中，关掉歌词那一条路径不该被这一票动到）",
			cover.Y+cover.H/2, float32(fullWinH)/2)
	}
}

// fullTestLRC12 是十二句的那一份歌词（和 `budgetLRC` 一样多）。
//
// ⚠️ **十二句是「一屏放不下」的量**：歌词区的高度是窗口高减去顶栏底栏，一行
// 二十来像素加上下留白，一屏十来行——所以十二句里**总有几行在容器外面**。
// 「滚过去之后看得见」那一条要验的就是这种情形。
const fullTestLRC12 = "[00:00.00]第一句\n[00:01.00]第二句\n[00:02.00]第三句\n" +
	"[00:03.00]第四句\n[00:04.00]第五句\n[00:05.00]第六句\n" +
	"[00:06.00]第七句\n[00:07.00]第八句\n[00:08.00]第九句\n" +
	"[00:09.00]第十句\n[00:10.00]第十一句\n[00:11.00]第十二句\n"

// fullTestLineNames 是上面那十二句的字（逐行量像素那一条要用）。
var fullTestLineNames = []string{
	"第一句", "第二句", "第三句", "第四句", "第五句", "第六句",
	"第七句", "第八句", "第九句", "第十句", "第十一句", "第十二句",
}

// lyricReadableBright 是「这一行还在屏幕上」的亮度门槛。
//
// ⚠️ **100 是量出来的，不是拍的**：满屏那一屏的字是白 92%（235），没淡的那几句
// 是 60% 的不透明度——压在糊过的封面上量到的最亮像素是 **146**；而淡到 4% 的话
// 只剩 **11**。100 在两者中间，离两头都远。
//
// ⚠️ **不能用「== 0」当判据**：坏掉那一版不是让行全灭，而是让行淡到 11——
// 「一个字都没有」测不出来，得测「还读不读得出来」。
const lyricReadableBright = 100

// TestTheLyricsStayVisibleWhenScrolled 钉住「滚过去的歌词看得见」。
//
// ⚠️ **这一条是评审抓出来的（critical）**：满屏那一屏的渐隐曾经按「离正在唱那
// 一句多远」压暗——而用户滚过去以后**滚进视野的正是离它最远的那几句**，于是它们
// 被算成「远」而全灭：十二句的歌有六句永远不显示，用户看到的是一整列空白。
//
// 判据：**滚下去之后，落在歌词区中间那一带（上游那条遮罩的全亮区）的行必须还有
// 字**。边缘那几行淡掉是上游那条遮罩本来就有的（行滑到边缘时被淡掉），所以
// 不在判据里。
func TestTheLyricsStayVisibleWhenScrolled(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	s.app.PutLyrics(fullTestLRC12, "")
	openFull(t, tt)
	settleFull(t, tt)

	// 滚轮往下滚：滚过去之后，容器里换成的是**离正在唱那一句最远**的那几句。
	tt.Scroll(800, 400, 0, 400)
	settleFull(t, tt)

	col, ok := tt.Find("满屏歌词")
	if !ok {
		t.Fatalf("画面上找不到歌词那一列，文字有 %q", tt.Texts())
	}
	img := tt.Image()
	visible := 0
	for _, name := range fullTestLineNames {
		box, ok := tt.Find(name)
		if !ok {
			continue
		}
		rel := (box.Y + box.H/2 - col.Y) / col.H
		// 边缘带（上游那条遮罩在那一带是淡的）不管。
		if rel <= lyricsFadeTop || rel >= 1-lyricsFadeBottom {
			continue
		}
		visible++
		if got := maxBright(img, box); got < lyricReadableBright {
			t.Errorf("%q 落在歌词区的中间（rel=%.2f）却几乎没有字（最亮 %d）——"+
				"滚过去的歌词必须看得见", name, rel, got)
		}
	}
	if visible == 0 {
		t.Fatal("滚下去之后，歌词区中间那一带一行都没有——" +
			"这一条量不到「滚过去的歌词看得见」")
	}
}

// TestAShortLyricDoesNotFadeItsLastLine 钉住「短歌词不会无缘无故暗一截」。
//
// ⚠️ **这一条是评审抓出来的（minor）**：满屏的渐隐那一档是按「离正在唱的那一句
// 多远」压暗的，而上游那条遮罩是按**容器上下沿**淡的——歌词很少时（几行）两者
// 不一样：上游那两条带子正好落在居中留白的空处，几行一样亮，而这一档会让最远
// 的那一句无缘无故暗一截（实测：第二句 192、第三句 192、**第四句 166**）。
//
// 判据是**逐行量最亮的像素**：正在唱的那一句最亮（放大 1.5 倍且不透明），**其余
// 几句一样亮**。
func TestAShortLyricDoesNotFadeItsLastLine(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	s.app.PutLyrics(fullTestLRC, "")
	openFull(t, tt)
	settleFull(t, tt)

	img := tt.Image()
	bright := map[string]int{}
	for _, line := range []string{"第一句", "第二句", "第三句", "第四句"} {
		box, ok := tt.Find(line)
		if !ok {
			t.Fatalf("找不到 %q，文字有 %q", line, tt.Texts())
		}
		bright[line] = maxBright(img, box)
	}
	// 最亮的那一句是正在唱的（它放大 1.5 倍、字重也更重）。
	active, others := "", []string{}
	for _, line := range []string{"第一句", "第二句", "第三句", "第四句"} {
		if bright[line] == 0 {
			t.Fatalf("%q 那一行一个像素都没有", line)
		}
		if active == "" || bright[line] > bright[active] {
			if active != "" {
				others = append(others, active)
			}
			active = line
		} else {
			others = append(others, line)
		}
	}
	want := bright[others[0]]
	for _, line := range others[1:] {
		if absDiff(byte(bright[line]), byte(want)) > 12 {
			t.Errorf("%q 那一行最亮的是 %d，%q 是 %d——短歌词不该有哪一句无缘无故暗一截",
				line, bright[line], others[0], want)
		}
	}
}

// TestTheFullPlayerShowsTheLyrics 断满屏那一屏真的画歌词。
//
// ⚠️ **判据不只是「文字出现了」**：歌词也可能压在封面底下（那一屏的底是糊过
// 的封面，字照样在画面上）。所以量的是它和**封面右沿**的关系——它在右边那一列。
func TestTheFullPlayerShowsTheLyrics(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	s.app.PutLyrics(fullTestLRC, "")
	openFull(t, tt)
	settleFull(t, tt)

	for _, want := range []string{"第一句", "第二句", "第三句", "第四句"} {
		if !tt.HasText(want) {
			t.Fatalf("满屏上没有歌词 %q，文字有 %q", want, tt.Texts())
		}
	}
	cover, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("满屏上找不到封面，文字有 %q", tt.Texts())
	}
	line, ok := tt.Find("第二句")
	if !ok {
		t.Fatalf("找不到「第二句」，文字有 %q", tt.Texts())
	}
	if line.X < cover.X+cover.W {
		t.Errorf("歌词压在封面底下（歌词 x=%.1f，封面右沿 %.1f）——"+
			"它应该在右边那一列", line.X, cover.X+cover.W)
	}
}

// TestTurningOffTheLyricsHidesThem 断「显示歌词」那枚开关真的管画面。
//
// 摆一个按下去什么都不变的开关比不摆更坏（见 fullplayer_settings.go 顶上）。
func TestTurningOffTheLyricsHidesThem(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	s.app.PutLyrics(fullTestLRC, "")
	openFull(t, tt)
	settleFull(t, tt)
	if !tt.HasText("第一句") {
		t.Fatalf("满屏上本来就没有歌词，文字有 %q", tt.Texts())
	}

	if err := tt.Click("全屏设置"); err != nil {
		t.Fatalf("点不了偏好面板：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	if err := tt.Click("开关：显示歌词"); err != nil {
		t.Fatalf("点不了歌词开关：%v", err)
	}
	tt.Frame()

	if tt.HasText("第一句") {
		t.Error("关掉「显示歌词」之后歌词还在画面上")
	}
	if s.app.FullPlayerPrefs().ShowLyrics {
		t.Error("偏好里没有记下歌词关着")
	}
	// 关掉之后右边那一栏让出来了：封面回到**整窗**里居中。
	cover, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("关掉歌词之后封面不见了，文字有 %q", tt.Texts())
	}
	if d := absF(cover.X + cover.W/2 - float32(fullWinW)/2); d > 1 {
		t.Errorf("关掉歌词之后封面的中心在 x=%.1f，窗口中线是 %d——"+
			"它没有回到整窗里居中", cover.X+cover.W/2, fullWinW)
	}
}

// TestTheFullPlayerSaysWhenThereAreNoLyrics 断「没有歌词」时是空状态。
//
// ⚠️ **这一条是 UX 报告 B3**：满屏开着、这一首没有歌词的时候，界面上一个字都
// 不说——用户分不清是「这一首没有」还是「这一版没做」。
func TestTheFullPlayerSaysWhenThereAreNoLyrics(t *testing.T) {
	_, tt, _ := fullLyricsTester(t)
	openFull(t, tt)
	settleFull(t, tt)

	if !tt.HasText("暂无歌词") {
		t.Fatalf("满屏上没有说「暂无歌词」，文字有 %q", tt.Texts())
	}
	// 而「怎么才能不空」那一句要兑现得了：找歌词那一枚摆着，按下去开搜索弹窗。
	if !tt.HasText(lyricSearchKnobLabel) {
		t.Fatalf("没有歌词时那一枚「%s」没摆，文字有 %q",
			lyricSearchKnobLabel, tt.Texts())
	}
	if err := tt.Click(lyricSearchKnobLabel); err != nil {
		t.Fatalf("点不了「%s」：%v", lyricSearchKnobLabel, err)
	}
	tt.Frame()
	if !tt.HasText(lyricSearchHeading) {
		t.Errorf("按了「%s」，搜索弹窗没开，文字有 %q",
			lyricSearchKnobLabel, tt.Texts())
	}
}

// TestTheLyricsColorSwitchChangesTheLyrics 断「歌词字体颜色」那一枚真的管字色。
//
// 它以前也是死控件（满屏没有歌词视图，挑了颜色画面上什么都不会变）。
//
// 判据是**画面上的像素**：歌词是红的（挑的就是红），而别的字（歌名那些）
// 仍然是满屏自己的前景色。
func TestTheLyricsColorSwitchChangesTheLyrics(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	s.app.PutLyrics(fullTestLRC, "")
	p := app.DefaultFullPlayerPrefs()
	p.LyricsColor = "#ff0000"
	s.app.SetFullPlayerPrefs(p)
	openFull(t, tt)
	settleFull(t, tt)

	line, ok := tt.Find("第二句")
	if !ok {
		t.Fatalf("找不到「第二句」，文字有 %q", tt.Texts())
	}
	if n := redIn(tt.Image(), line); n == 0 {
		t.Errorf("歌词那一块里一个红的像素都没有（字色没跟着「歌词字体颜色」走）")
	}
}

// redIn 是 box 里「明显是红」的像素个数。
func redIn(img *image.RGBA, box mygoui.Rect) int {
	n := 0
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			p := img.RGBAAt(x, y)
			if p.R > 120 && p.G < 90 && p.B < 90 {
				n++
			}
		}
	}
	return n
}

// TestTheFullSettingsPanelHasATitleAndCanBeClosed 断偏好面板有标题、能关。
//
// ⚠️ **两条都是 UX 报告 B4**：面板没有标题（用户得靠里面的开关名猜这是什么
// 面板），只能再点一次齿轮关掉（忘了齿轮在哪，面板就一直浮在歌的封面上）。
func TestTheFullSettingsPanelHasATitleAndCanBeClosed(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	openFull(t, tt)
	if err := tt.Click("全屏设置"); err != nil {
		t.Fatalf("点不了偏好面板：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()

	// ⚠️ **必须用 `Find`（精确等于），不能用 `HasText`（子串匹配）**：
	// 「显示」是「显示歌词」「显示频谱图」「显示封面」「显示虚化背景」这四行的
	// **子串**——`HasText("显示")` 命中的是它们，标题整行删掉这条断言照样绿
	// （评审抓到的那一条：这条断言本来是空的）。
	if _, ok := tt.Find(fullSettingsTitle); !ok {
		t.Errorf("偏好面板没有标题 %q，文字有 %q", fullSettingsTitle, tt.Texts())
	}
	if err := tt.Click("关闭设置"); err != nil {
		t.Fatalf("面板上没有「关闭设置」那一枚：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	if s.full.settings {
		t.Error("按了「关闭设置」，面板还开着")
	}
	// 关掉之后面板里那些开关也不在了。
	if tt.HasText("显示频谱图") {
		t.Errorf("面板关了，「显示频谱图」还在画面上，文字有 %q", tt.Texts())
	}
}

// TestTheLyricsDoNotJumpWhenTheInterfaceHides 钉住「界面藏起来时歌词列不横向跳」。
//
// ⚠️ **这一条是评审抓出来的（major）**：分集面板开着的时候，歌词那一列的右边界
// **让开面板**；而界面三秒不动自动隐藏的时候**面板不画了**（`viewFullParts` 见到
// hidden 就返回），歌词却**还亮着**（歌词是内容不是控件）——那时候若把歌词列改回
// 整宽，整列歌词就在界面淡出的那一刻**横向跳 128 像素**（实测：第一句从 x=681
// 滑到 x=809），指针一动界面回来它又滑回去。
//
// 判据：藏起来前后，歌词那一列的**框**不许变。
func TestTheLyricsDoNotJumpWhenTheInterfaceHides(t *testing.T) {
	s, tt, fc := fullLyricsTester(t)
	s.app.PutLyrics(fullTestLRC, "")
	openFull(t, tt)
	settleFull(t, tt)
	// 开分集面板：歌词那一列要让开它。
	s.full.partsOpen = true
	tt.Frame()
	settleFull(t, tt)
	before, ok := tt.Find("满屏歌词")
	if !ok {
		t.Fatalf("画面上找不到歌词那一列，文字有 %q", tt.Texts())
	}
	// 推钟过三秒：界面藏起来。
	advanceFull(s, tt, fc, biuHideSeconds*time.Second+time.Second)
	if !s.fullHidden() {
		t.Fatal("三秒过去了界面没有藏起来")
	}
	tt.Frame()
	after, ok := tt.Find("满屏歌词")
	if !ok {
		t.Fatalf("界面藏起来之后歌词那一列不见了，文字有 %q", tt.Texts())
	}
	if after.X != before.X || after.W != before.W {
		t.Errorf("界面藏起来之后，歌词那一列从 (x=%.1f, 宽 %.1f) 变成 (x=%.1f, 宽 %.1f)"+
			"——整列歌词横向挪了 %.1f 像素、宽窄变了 %.1f（面板不画了，歌词却还亮着）",
			before.X, before.W, after.X, after.W,
			after.X-before.X, after.W-before.W)
	}
}

// TestTheLyricsTakeTheWholeWidthWhenTheCoverIsOff 钉住「封面关着时左边不留空列」。
//
// ⚠️ **这一条是评审抓出来的（minor）**：分栏只按「显示歌词」分，不看「显示封面」——
// 于是「关掉封面、留着歌词」这一种组合下，左边 42% 的窗宽整块空着（只有糊过
// 的封面底），歌词从 x=809 才开始，像少画了一半。
func TestTheLyricsTakeTheWholeWidthWhenTheCoverIsOff(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	p := app.DefaultFullPlayerPrefs()
	p.ShowCover = false
	s.app.SetFullPlayerPrefs(p)
	s.app.PutLyrics(fullTestLRC, "")
	openFull(t, tt)
	settleFull(t, tt)

	col, ok := tt.Find("满屏歌词")
	if !ok {
		t.Fatalf("画面上找不到歌词那一列，文字有 %q", tt.Texts())
	}
	// ⚠️ **判据是「左边那一半不留空列」**，不是「字从 x=0 开始」：上游的歌词
	// 容器是 `px-12 py-24`（`full-screen-player/index.tsx:357`），所以**内容盒**
	// 从 48 开始——封面关着的时候它占的是整窗（上游那个 `w-full` 的兄弟少了一个，
	// 它就摊开成整窗），不是右边那一半。
	if col.X > fullStagePadX {
		t.Errorf("封面关着，歌词的内容盒却从 x=%.1f 才开始——左边那一整块空着，"+
			"歌词被挤到了右半边", col.X)
	}
}

// TestTheFullSettingsPanelLinesUpWithTheGear 断面板的左沿和齿轮的**图形**左沿对齐。
//
// ⚠️ **对齐的是图形，不是那枚键的点击区**：齿轮的点击区是 32 见方、图形是 22，
// 图形在框里居中（左右各留 5）。面板默认贴在点击区的左沿上，于是它比图形左沿
// 还左 5——看过去就是「从按钮底下歪出来一块」（UX 报告 B4 的第三条）。
func TestTheFullSettingsPanelLinesUpWithTheGear(t *testing.T) {
	_, tt, _ := fullLyricsTester(t)
	openFull(t, tt)
	gear, ok := tt.Find("全屏设置")
	if !ok {
		t.Fatalf("找不到「全屏设置」，文字有 %q", tt.Texts())
	}
	if err := tt.Click("全屏设置"); err != nil {
		t.Fatalf("点不了偏好面板：%v", err)
	}
	tt.Frame()

	panel, ok := tt.Find("全屏设置面板")
	if !ok {
		t.Fatalf("画面上找不到偏好面板那个盒子，文字有 %q", tt.Texts())
	}
	wantX := gear.X + fullSettingsAlignPad
	if d := absF(panel.X - wantX); d > 1 {
		t.Errorf("面板左沿在 x=%.1f，齿轮的图形左沿是 %.1f（键的左沿 %.1f + %d）——"+
			"面板没有和齿轮对齐", panel.X, wantX, gear.X, fullSettingsAlignPad)
	}
}

// ---- 出图 ----

// TestRenderFullPlayerLyricsShot 存图：满屏播放器**带歌词**的那一屏，深浅各一套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderFullPlayerLyricsShot
//
// ⚠️ **深浅两套都要出**：满屏的字是白 92%（浅色底上换成黑 92%），而歌词那一列
// 是这一屏里唯一一大块字——只出一套等于没验它压在两种底上读不读得出来。
func TestRenderFullPlayerLyricsShot(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	body := splitCoverPNG(t, 1200, 1200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, at(fullWinW, fullWinH), systemIsDark(dark),
				withClock(newFakeClock(time.Unix(1_700_000_000, 0))))
			item := app.LocalItem(flacPath(t))
			item.Cover = srv.URL + "/cover.png"
			if err := b.a.Enqueue(item); err != nil {
				t.Fatalf("把歌放进队列：%v", err)
			}
			waitFrames(t, b.tt, "歌开起来了", func() bool {
				return b.a.Snapshot().HasTrack
			})
			waitFrames(t, b.tt, "封面下回来了", func() bool {
				return b.s.images.Get(item.Cover).Ready()
			})
			// ⚠️ **暂停**：歌在放的时候播放位置一直在走，于是「正在唱的是第几句」
			// 每隔一秒换一句，而换一句就要重新对一次滚动的目标——界面于是永远在
			// 要帧（`settleFull` 等不到它停，出来的是一张半路上的图）。
			b.a.Pause()
			b.a.PutLyrics(fullTestLRC, "")
			b.s.openFullPlayer()
			waitFrames(t, b.tt, "满屏开出来了", func() bool {
				return b.tt.HasText("关闭全屏")
			})
			settleFull(t, b.tt)
			save(t, dir, "63-满屏播放器-歌词-"+name+".png", b.tt)
			b.Close()
		})
	}
}

// TestRenderFullPlayerLyricsAndPartsPanelShot 存图：满屏 + **分集面板开着**，
// 歌词让开它。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderFullPlayerLyricsAndPartsPanelShot
//
// 这一张是评审那条 major 的证据：分集面板（贴右边 280 宽的一竖条）开着的时候，
// 歌词那一列**让开它**——而不是从中间被面板切断。
func TestRenderFullPlayerLyricsAndPartsPanelShot(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	body := splitCoverPNG(t, 1200, 1200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, at(fullWinW, fullWinH), systemIsDark(dark),
				withClock(newFakeClock(time.Unix(1_700_000_000, 0))))
			item := app.LocalItem(flacPath(t))
			item.Cover = srv.URL + "/cover.png"
			if err := b.a.Enqueue(item); err != nil {
				t.Fatalf("把歌放进队列：%v", err)
			}
			waitFrames(t, b.tt, "歌开起来了", func() bool {
				return b.a.Snapshot().HasTrack
			})
			waitFrames(t, b.tt, "封面下回来了", func() bool {
				return b.s.images.Get(item.Cover).Ready()
			})
			b.a.Pause()
			b.a.PutLyrics(fullTestLRC, "")
			b.s.openFullPlayer()
			waitFrames(t, b.tt, "满屏开出来了", func() bool {
				return b.tt.HasText("关闭全屏")
			})
			// 开分集面板：歌词那一列要让开它。
			b.s.full.partsOpen = true
			b.tt.Frame()
			settleFull(t, b.tt)
			save(t, dir, "65-满屏播放器-歌词与分集面板-"+name+".png", b.tt)
			b.Close()
		})
	}
}

// TestRenderFullPlayerLyricsScrolledShot 存图：满屏 + 歌词 + **滚下去之后**。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderFullPlayerLyricsScrolledShot
//
// 这一张是「滚过去的歌词看得见」那一格的**现场**：十二句的歌词滚下去 400 像素
// 之后，落在歌词区中间的那几句是全亮的（上游那条遮罩的语义），滚出容器的那
// 几句淡掉。
//
// ⚠️ **这一张以前要靠评审自己写探针才出得来**（票 #236 的第 3 轮评审：它为了
// 验「滚过去之后看得见」，自己写了一个量逐行亮度的探针 + 一张出图）。收进仓库
// 之后，下一次要看这一格不用再写一遍。
func TestRenderFullPlayerLyricsScrolledShot(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	body := splitCoverPNG(t, 1200, 1200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, at(fullWinW, fullWinH), systemIsDark(dark),
				withClock(newFakeClock(time.Unix(1_700_000_000, 0))))
			item := app.LocalItem(flacPath(t))
			item.Cover = srv.URL + "/cover.png"
			if err := b.a.Enqueue(item); err != nil {
				t.Fatalf("把歌放进队列：%v", err)
			}
			waitFrames(t, b.tt, "歌开起来了", func() bool {
				return b.a.Snapshot().HasTrack
			})
			waitFrames(t, b.tt, "封面下回来了", func() bool {
				return b.s.images.Get(item.Cover).Ready()
			})
			// ⚠️ **暂停**：位置一直在走的话「正在唱的是第几句」每隔一秒就换一句，
			// 换一句就要重新对一次滚动的目标——界面永远在要帧（`settleFull` 等不到
			// 它停）。
			b.a.Pause()
			b.a.PutLyrics(fullTestLRC12, "")
			b.s.openFullPlayer()
			waitFrames(t, b.tt, "满屏开出来了", func() bool {
				return b.tt.HasText("关闭全屏")
			})
			settleFull(t, b.tt)
			// 滚轮往下滚 400 像素：滚进视野的正是离正在唱那一句最远的那几句。
			b.tt.Scroll(800, 400, 0, 400)
			settleFull(t, b.tt)
			save(t, dir, "66-满屏播放器-歌词滚下去之后-"+name+".png", b.tt)
			b.Close()
		})
	}
}

// TestRenderFullPlayerNoLyricsShot 存图：满屏播放器**这一首没有歌词**的那一屏。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderFullPlayerNoLyricsShot
//
// 这一张是给 B3 那一格留的证据：空状态**不是整屏空白**。
func TestRenderFullPlayerNoLyricsShot(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	body := splitCoverPNG(t, 1200, 1200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, at(fullWinW, fullWinH), systemIsDark(dark),
				withClock(newFakeClock(time.Unix(1_700_000_000, 0))))
			item := app.LocalItem(flacPath(t))
			item.Cover = srv.URL + "/cover.png"
			if err := b.a.Enqueue(item); err != nil {
				t.Fatalf("把歌放进队列：%v", err)
			}
			waitFrames(t, b.tt, "歌开起来了", func() bool {
				return b.a.Snapshot().HasTrack
			})
			waitFrames(t, b.tt, "封面下回来了", func() bool {
				return b.s.images.Get(item.Cover).Ready()
			})
			// ⚠️ **暂停**：理由同上（位置一直在走，界面就一直在要帧）。
			b.a.Pause()
			b.s.openFullPlayer()
			waitFrames(t, b.tt, "满屏开出来了", func() bool {
				return b.tt.HasText("关闭全屏")
			})
			settleFull(t, b.tt)
			save(t, dir, "64-满屏播放器-没有歌词-"+name+".png", b.tt)
			b.Close()
		})
	}
}

// ---- 小帮手 ----
