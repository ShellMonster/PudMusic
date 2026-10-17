package media

import (
	"errors"
	"testing"
)

// bindingPlatform 是一个**要窗口**的假平台：它认 windowBinder。
type bindingPlatform struct {
	fakePlatform
	handles []uintptr
	err     error
}

func (b *bindingPlatform) bindWindow(h uintptr) error {
	b.handles = append(b.handles, h)
	return b.err
}

// TestBindWindowGoesToThePlatformThatWantsOne 断「要窗口的那一半」拿到句柄。
//
// Windows 的 SMTC 和任务栏缩略图都是按窗口算的，而 `MountMedia` 在开窗口
// 之前就调过了——所以绑定必须发生在**窗口建出来之后**。这条钉住那个契约：
// 句柄原样递到底，不做换算、不做包装。
func TestBindWindowGoesToThePlatformThatWantsOne(t *testing.T) {
	b := &bindingPlatform{}
	s := newSession(b)
	t.Cleanup(s.Close)

	if !s.BindWindow(0x1234) {
		t.Fatal("绑定失败了")
	}
	if len(b.handles) != 1 || b.handles[0] != 0x1234 {
		t.Errorf("平台收到的是 %v，want [0x1234]", b.handles)
	}
}

// TestAPlatformThatDoesNotWantAWindowIsFine 断 macOS 那一半不会被这一下弄坏。
//
// macOS 的 `MPNowPlayingInfoCenter` 是全局的，`windowBinder` 那一个方法
// 根本不存在。所以这一句在那边必须是**什么都不做就成功**——
//
//	返回 false 会让界面层记一条「系统媒体会话没接上」，而 macOS 上明明接上了；
//	返回错误更糟：那是一条和事实相反的日志。
func TestAPlatformThatDoesNotWantAWindowIsFine(t *testing.T) {
	s := newSession(&fakePlatform{})
	t.Cleanup(s.Close)

	if !s.BindWindow(0xdead) {
		t.Error("不需要窗口的平台被判成绑定失败")
	}
}

// TestABindFailureIsReportedAndNothingElseBreaks 断失败只失败在绑定这一件事上。
//
// 症状上它很重要：绑定失败意味着浮窗、锁屏、任务栏按钮都没有，而**界面、
// 播放、队列全都不受影响**。所以这里断的是「Publish 之后还照样发」——
// 少了这条，一个绑不上的会话会顺手把状态更新也一起吞掉，而症状变成
// 「系统媒体会话装不上之后，界面上的播放条也不动了」。
func TestABindFailureIsReportedAndNothingElseBreaks(t *testing.T) {
	b := &bindingPlatform{err: errors.New("装不上")}
	s := newSession(b)
	t.Cleanup(s.Close)

	if s.BindWindow(1) {
		t.Error("绑失败了却报成功")
	}
	s.Publish(playing())
	if got := b.count(); got != 1 {
		t.Errorf("绑失败之后 Publish 发了 %d 次，want 1", got)
	}
	if !s.Available() {
		t.Error("绑失败之后这个会话被判成没装上")
	}
}

// TestADummySessionBindsTrivially 断「压根没装平台」的那一种不算失败。
//
// `media.New` 在平台那一半建不起来时会造一个哑会话（界面、播放一点都察觉不到）。
// 对它调 BindWindow 的应该是界面层那条**无条件**的接线——所以它不能报错，
// 否则日志上会出现一条「没接上」而其实压根没打算接。
func TestADummySessionBindsTrivially(t *testing.T) {
	s := newSession(nil)
	t.Cleanup(s.Close)

	if s.Available() {
		t.Error("哑会话被判成装上了")
	}
	if !s.BindWindow(0) {
		t.Error("哑会话的绑定报了失败")
	}
}

// TestBindingTwiceReachesThePlatformTwice 断重复绑是**允许**的。
//
// 幂等是平台那一半自己的责任（Windows 那边要先把上一个窗口收干净），这一层
// 只保证不去挡它。真机上的路径确实会连着来两次：先 `ensureWindow` 一次，
// 窗口被关掉又重开再一次。
func TestBindingTwiceReachesThePlatformTwice(t *testing.T) {
	b := &bindingPlatform{}
	s := newSession(b)
	t.Cleanup(s.Close)

	s.BindWindow(1)
	s.BindWindow(2)
	if len(b.handles) != 2 {
		t.Fatalf("平台收到 %d 次绑定，want 2", len(b.handles))
	}
	if b.handles[1] != 2 {
		t.Errorf("第二次绑的是 %#x，want 2", b.handles[1])
	}
}
