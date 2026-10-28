package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// 播放条上的取流反馈（票 #282 立的「要说话」，票 #581 改成「光圈说话」）。
//
// # 这一组盯的是「点了歌一直不开始」时用户看到什么
//
// 取流这一趟里界面的反馈只有**播放键上那一圈光**（loadhalo_test 那一族断的是
// 光圈本身），播放条左下角**不写字**——那一行留给出错/重连/停住那三档。
// 等够了（阈值见 app 的 onlinePlayTimeout）上屏的是一句失败的话，不是一直转圈。

// TestThePlayBarStaysQuietWhileLoading 盯「取流的时候那一行不说话」（票 #581）。
//
// 🔴 **判据是「没有那句话」**：以前这里有「正在取流：<歌名>」（票 #282）和
// 「取流很慢」（票 #220），用户不要这两行——反馈集中在播放键的光圈上。
// 歌名和 UP 名照旧，位置不挪。
func TestThePlayBarStaysQuietWhileLoading(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "还在放的那一首", Loading: true,
	}, 1000)

	if tt.HasText("正在取流") || tt.HasText("取流很慢") {
		t.Errorf("取流的时候播放条上不该有取流的字：%q", tt.Texts())
	}
	// 上一首的歌名照旧：取流这一趟里正在放的还是它。
	if !tt.HasText("还在放的那一首") {
		t.Errorf("取流的时候歌名不见了：%q", tt.Texts())
	}
}

// TestThePauseKeyIsStillThereWhileLoading 盯**取流期间还能按暂停**。
//
// 🔴 **这一条是票 #282 的评审挑出来的**：取流这一趟里**上一首还在放**（在线流
// 是后台建的），而把播放/暂停那一枚换成转圈的话，用户就按不了暂停了——光圈
// 绕的是那一枚键，键本身照旧。
func TestThePauseKeyIsStillThereWhileLoading(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		HasTrack: true, Title: "还在放的那一首", Playing: true,
		Loading: true,
	}, 1000)

	if _, ok := tt.Find("暂停"); !ok {
		t.Errorf("取流时播放条上按不了暂停：%q", tt.Texts())
	}
	// 反过来：没在放的时候那一枚是「播放」，不能因为在取流就变成别的。
	tt2, _ := metaBarTester(t, app.State{
		Loading: true,
	}, 1000)
	if _, ok := tt2.Find("播放"); !ok {
		t.Errorf("取流时播放条上那一枚不是「播放」：%q", tt2.Texts())
	}
}

// TestAFailedPlayGivesASentenceInsteadOfASpinner 盯「失败之后是一句话，不是一直转圈」。
//
// 这一条是票 #282 的验收：坏的那一首要么能出声，要么**给一句能看懂的话**，
// **不许无限转圈**。
func TestAFailedPlayGivesASentenceInsteadOfASpinner(t *testing.T) {
	tt, _ := metaBarTester(t, app.State{
		Err: "取流超时了", ErrDetail: "取流超时了（20s 内这一段网络没回）。可能是网络慢，也可能是这一首的流取不到——换一首放，或者点重试再试一次。",
	}, 1000)

	if tt.HasText("正在取流") {
		t.Errorf("失败了还在转圈：%q", tt.Texts())
	}
	// 🔴 **那句话在播放键那一枚的「详情」面板里**（票：左下角出错那三行太碎）：
	// 错误文案从那一列里搬走了——那一列只有 75.7 像素宽（实测），一句 26 个
	// 字的话上屏只剩十来个。
	//
	// ⚠️ **判据是「打开面板之后那句话在」**：面板没打开时那句话不在画面上，
	// 而「失败之后是一句话」这条规格指的是**用户能得到那句话**。
	if !tt.HasText(errPlayKeyLabel) {
		t.Errorf("失败之后播放键没有变成那个出错的键：%q", tt.Texts())
	}
	openErrDetail(t, tt, "")
	if !tt.HasText("取流超时了") {
		t.Errorf("打开详情之后界面上没有那句话：%q", tt.Texts())
	}
}

// TestTheFailureSentenceFitsOnOneLine 盯**失败那句话在面板里排得开**。
//
// 🔴 **判据是「这句话本身有多宽」比「那一格有多宽」**（不是「字在不在」）：
// 播放条上那一列只有 75.7 像素宽（实测），12 号字放得下十来个字。一句 50 个字
// 的话上屏只剩「打不开：取流…」这样的碎片——那不是「一句能看懂的话」
// （票 #282 的评审）。
//
// 🔴 **那句话在「详情」面板里**（票：左下角出错那三行太碎）：面板宽 320
// （`t.Space(80)`），一句 50 个字的话**排成两三行**——而「排开了」才是看得全
// 的证明。
//
// ⚠️ **完整那一句在悬停提示里**（`State.ErrDetail`），所以短的那句在画面上、
// 完整的那句在提示和面板里，两边合起来才是一句能看懂的话。
func TestTheFailureSentenceFitsOnOneLine(t *testing.T) {
	const short = "取流超时了"
	const detail = "取流超时了（20s 内这一段网络没回）。可能是网络慢，也可能是这一首的流取不到——换一首放，或者点重试再试一次。"
	tt, _ := metaBarTester(t, app.State{Err: short, ErrDetail: detail}, 1200)

	openErrDetail(t, tt, "")
	body, ok := tt.Find(detail)
	if !ok {
		t.Fatalf("面板里没有完整那一句：%q", tt.Texts())
	}
	// 🔴 **它排开了**：一行 12 号字是 14 像素高，两行就是 28。不止一行，才说明
	// 这句话是**排开的、看得全的**，而不是又被截了一次。
	if body.H <= 20 {
		t.Errorf("面板里那句话只有 %.1f 高——它被压成一行了（= 又截了一次）：%q",
			body.H, tt.Texts())
	}
}
