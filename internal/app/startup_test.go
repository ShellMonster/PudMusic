package app_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 这一组盯「**冷启动**」那一段（票 #478）。
//
// # 它盯的是什么
//
// 「双击 app → 第一首歌开始出声」是一条**串行**的链，而它的每一段慢起来
// 的原因**完全不同**：读盘、读钥匙串、建窗口、B 站接口、网络、这台机器的
// 解码器。混在一起只有一个「总的等了多久」，而那个数**说不出慢的是哪一段**。
//
// # 两条判据
//
//	「分了几段」		拆开之后每段都有名字
//	「慢」和「等人」	钥匙串那一段**单独一栏**（它可能是「等人点一下」，
//			不是「我们慢」——见下面那条用例）

// TestTheStartupIsSplitIntoSegments 盯「冷启动分了段」。
//
// 🔴 **它断的是「每一段都有名字」**：段数对不对是次要的，而**每一段都能
// 被单独说出来**才是要的——说不出来的段等于没量。
func TestTheStartupIsSplitIntoSegments(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	a.NoteMounted()
	a.NoteReady()
	a.NoteFirstFrame()

	st := a.StartupTiming()
	if st.Mount <= 0 {
		t.Error("没有「零件挂好」那一段")
	}
	if st.Ready <= 0 {
		t.Error("没有「应用就绪」那一段")
	}
	if st.Frame <= 0 {
		t.Error("没有「首帧」那一段")
	}
	// 段与段之间是**递进**的：挂好 ≤ 就绪 ≤ 首帧。
	if st.Mount > st.Ready || st.Ready > st.Frame {
		t.Errorf("三段不是递进的：%v / %v / %v", st.Mount, st.Ready, st.Frame)
	}
	// 那一行日志里三段都要在——事后查的时候只看得见日志。
	line := st.String()
	for _, want := range []string{"零件挂好", "应用就绪", "首帧"} {
		if !strings.Contains(line, want) {
			t.Errorf("冷启动那一行里没有 %q：%s", want, line)
		}
	}
}

// TestTheKeychainWaitIsNotCalledSlow 盯「**等人点钥匙串**」和「慢」是
// 两件事（票 #478 的验收项）。
//
// # 为什么这一条要紧
//
// macOS 会在应用**还没有窗口**的时候弹一个系统授权框（「BiuGo 想要访问
// 钥匙串里的机密信息」），用户要点一下才继续。那一段的时长**是用户点的**，
// 不是我们慢——
//
//	混进「冷启动一共多久」里	→ 用户读到的是「应用慢」
//	单独一栏 + 说清是「等人」	→ 用户读到的是「它在等一个授权」
//
// 根因（为什么每次都要授权）是另一张票（#473）在查；这一票只负责**把它
// 从「慢」里分出来**。
func TestTheKeychainWaitIsNotCalledSlow(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	// 一次**正常**的钥匙串读：毫秒级。
	a.NoteKeychainRead(3 * time.Millisecond)
	st := a.StartupTiming()
	if st.Keychain != 3*time.Millisecond {
		t.Errorf("钥匙串那一段没被单独记下来：%v", st.Keychain)
	}
	if st.KeychainWaited {
		t.Error("3 毫秒的钥匙串读被判成「等过人」了")
	}
	if !strings.Contains(st.String(), "读钥匙串") {
		t.Errorf("冷启动那一行里没有钥匙串那一段：%s", st.String())
	}

	// 一次**等了人**的：超过阈值。
	a2 := app.New()
	t.Cleanup(a2.Close)
	a2.NoteKeychainRead(4 * time.Second)
	st2 := a2.StartupTiming()
	if !st2.KeychainWaited {
		t.Error("4 秒的钥匙串读没被判成「等过人」")
	}
	// 🔴 **那一行里必须说清是「等人」**：用户读到的是「它在等一个授权」，
	// 而不是「应用慢」。
	if !strings.Contains(st2.String(), "等人") {
		t.Errorf("冷启动那一行里没把「等人」说清：%s", st2.String())
	}
}

// TestTheStartupEndsWhenTheFirstTrackSettles 盯「**启动中**」这一档
// 什么时候结束。
//
// # 冷启动那一句是给谁看的
//
// 冷启动恢复上次那一首时，**窗口开了、界面在那儿、就是没声音**——
// 而「还没有打开文件」是**一句假话**（应用不是没有文件，它是在等上一首）。
// 所以界面上要有一句「正在启动应用…」。
//
// ⚠️ **它不能一直亮着**：应用准备好了之后还写「正在启动」，用户会以为
// 应用没起来。
func TestTheStartupEndsWhenTheFirstTrackSettles(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	// 一、刚起来：还没画过第一帧 → 是在启动。
	if !a.Snapshot().Starting {
		t.Error("刚起来的时候不是「启动中」")
	}
	// 二、第一帧画出来了，而那一刻**没有取流在路上** → 启动完了。
	a.NoteFirstFrame()
	if a.Snapshot().Starting {
		t.Error("第一帧画出来之后还在「启动中」——那一句话会一直亮着")
	}
}

// TestTheStartupLineIsOnlyShownWhenThereIsNoTrack 盯「**有歌的时候不许说
// 「正在启动」**」。
//
// ⚠️ **有歌的时候界面上摆的是歌名**：用户看得见应用已经准备好了，
// 而那一句话会跟歌名抢位置（而歌名那一格是给歌名的）。
func TestTheStartupLineIsOnlyShownWhenThereIsNoTrack(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	a.NoteFirstFrame()
	if a.Snapshot().Starting {
		t.Error("没有歌的时候也在「启动中」——那句「正在启动应用…」该只在冷启动时说")
	}
}
