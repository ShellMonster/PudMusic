package ui

// 票 #296：历史记录页头右侧那枚开关旁边要有一句「记录播放历史」。
//
// # 这一票补的是「看得见的一句字」
//
// 上游那一行是三样：开关 + 「记录播放历史」+ 一枚清空键
// （`src/pages/history/index.tsx:258-275`）。我们此前只有那枚开关，于是它
// **没有一句话说清自己是干什么的**——用户看见一枚旋钮，不知道拨它会不会把
// 自己的播放记录报上去（那是**写操作**，是隐私）。
//
// 这一票只加那一句（上游原文照抄），**不加清空键**：那是 2026-10-24 拍板的取舍，
// 而要摆它得先把 `/x/v2/history/clear` 那条接口链接上（理由见 `history.go` 文件头
// 那一段）。
//
// # 判据是「画面上有没有那句话」，不是「代码里有没有那句字」
//
// ⚠️ 开关自己就带着一个 `Label("记录播放历史")`——那是**给读屏和测试器找它用的
// 名字**，画在屏幕上一个字也没有。所以「`tt.HasText("记录播放历史")`」这条判据
// **改之前就是绿的**：它证明不了那一句是**看得见的**。
//
// 所以这里判三件事：
//
//  1. 画面上有**两处**叫「记录播放历史」的节点（开关的名字 + 那一句可见的字）；
//  2. 找到的第一个是**那枚开关**（宽 36），不是那句话——顺序必须是开关在左、
//     文案在右（上游就是那个顺序）；
//  3. 开关右边那一段里**真的有字**（不是底色、也不是主色的像素）。
//
// 少任何一条都不算数：只有第 1 条的话，一句 `Label` 就够了；只有第 3 条的话，
// 开关自己那枚旋钮的影子也算进去。

import (
	"strings"
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// historyHeaderShell 造一个停在历史记录那一页的外壳：带登录态、带一台假 B 站、
// 喂了一页数据。
//
// ⚠️ **登录态是必需的**（票 #138）：这一页要登录才有，没登录时页面画的是
// 「先登录」那一支，页头右侧压根没有那个开关。
func historyHeaderShell(t *testing.T, opts ...shellOpt) (*mygoui.Tester, *Shell) {
	t.Helper()
	f := newLibFake(t).withHistory(1, 100, 600, "听过的那首")
	return libraryShellOpts(t, f, historyPath, opts...)
}

// TestTheHistoryHeaderSaysWhatTheSwitchIs 盯页头右侧那句「记录播放历史」。
//
// 🔴 **它的失败方式是静默的**：没有那句话，页面上仍然是那枚绿绿的旋钮，
// 而用户只能猜它是什么——界面上没有任何一样东西会报错。
//
// 深浅两套都跑：那句话在浅色下是深灰、在深色下是浅灰，而**判据取的是「不是
// 底色」**，所以两套用的是同一条。
func TestTheHistoryHeaderSaysWhatTheSwitchIs(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt, _ := historyHeaderShell(t, systemIsDark(dark))

			// ① 画面上有两处叫这句话的节点：开关的名字 + 那一句看得见的字。
			n := 0
			for _, s := range tt.Texts() {
				if s == reportHistoryLabel {
					n++
				}
			}
			if n < 2 {
				t.Fatalf("页头右侧只有 %d 处「%s」——那一句看得见的说明没画出来"+
					"（开关自己带一个同名的 Label，所以 %d 处就是「只有开关」）",
					n, reportHistoryLabel, n)
			}

			// ② 第一个找到的是**那枚开关**。
			//
			// ⚠️ 这一条不只是「位置」：无头 Tester 的 `Find` 只认**第一个**，
			// 而它按树上的顺序找——开关在左、文案在右，于是点「记录播放历史」
			// 找到的是开关（拨得动）。要是把顺序倒过来，先找到的是那句字，
			// 点它什么也不会发生。
			sw, ok := tt.Find(reportHistoryLabel)
			if !ok {
				t.Fatalf("画面上找不到「%s」，文字有 %q", reportHistoryLabel, tt.Texts())
			}
			if sw.W > 60 {
				t.Fatalf("第一个「%s」宽 %.0f——那不是那枚开关（36 宽），"+
					"是那句话自己：顺序必须是开关在左、文案在右", reportHistoryLabel, sw.W)
			}

			// ③ 开关右边那一段里真的有字。
			//
			// ⚠️ **底色是从画面上取的**（页头那一行最右边那个像素），不是写死的
			// 白：深色下底色是深灰，写死白的话那一段的每一个像素都算「有字」。
			img := tt.Image()
			bg := img.RGBAAt(img.Bounds().Dx()-4, int(sw.Y+sw.H/2))
			ink := 0
			for y := int(sw.Y); y < int(sw.Y+sw.H); y++ {
				for x := int(sw.X + sw.W); x < int(sw.X+sw.W+120); x++ {
					if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
						continue
					}
					c := img.RGBAAt(x, y)
					if !distinctPixel(c, bg) {
						continue
					}
					// 主色是绿的（绿通道比红通道高一截），那不是字。
					if int(c.G)-int(c.R) >= accentGreenGap {
						continue
					}
					ink++
				}
			}
			// 六个字 14 号，少说也有几百个墨点；门槛给 30 是为了**挡得住**
			// 开关边沿抗锯齿漏过来的那一两个像素。
			if ink < 30 {
				t.Errorf("开关右边那一段只有 %d 个字的像素——那句话没有画出来"+
					"（开关在 %v）", ink, sw)
			}
		})
	}
}

// TestTheHistoryHeaderHasNoClearKey 盯**没有**清空键。
//
// 上游那一行右边还有一枚垃圾桶（清空历史），我们**故意不摆**：那是 2026-10-24
// 拍板的取舍，而要摆它得先把 `/x/v2/history/clear` 那条接口链接上——
// 见 `history.go` 文件头那一段。
//
// ⚠️ **别把理由记成「我们没有共用的确认弹窗」**：那份弹窗**已经有了**
// （`internal/ui/confirmmodal.go`，643cb3f / #176 / #204，2026-10-22），缺的是
// **接口**，不是弹窗。
//
// ⚠️ 这一条是**守成**的：它盯的是「别有一天顺手把它加回来」。
func TestTheHistoryHeaderHasNoClearKey(t *testing.T) {
	tt, _ := historyHeaderShell(t)
	for _, s := range tt.Texts() {
		if strings.Contains(s, "清空") {
			t.Errorf("历史记录页头上出现了「%s」——那一枚键是**故意不摆**的"+
				"（票 #296）", s)
		}
	}
}
