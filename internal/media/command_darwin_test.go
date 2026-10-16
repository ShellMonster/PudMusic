//go:build darwin

package media

// 远程命令那一半的真机测试：命令拿不拿得到、装得上装不上、以及「收到的是
// 哪一条命令」那段判断。
//
// ## 这里断不了什么（以及为什么）
//
// **断不了「系统真的会为一次点击造出命令事件」。** `MPRemoteCommandEvent`
// **拒绝外部构造**——实测 `-[MPRemoteCommandEvent init]` 抛
// `NSGenericException: MPRemoteCommandEvents cannot be initialized externally.`，
// 所以一个自己写的用例永远造不出真的事件对象，那条路走不通。
// 这不是本层能绕过去的：唯一能造出事件的是系统自己。
//
// 所以能测的部分被拆成了两半：往系统上**装**（真的系统调用），和从命令对象
// 上**认出**是哪一条（纯逻辑，用假的命令对象）。中间那条缝——系统递事件过
// 来、我们的 IMP 收到——只能靠真机肉眼确认，见任务报告。

import (
	"testing"
	"time"
)

// commandForTest 拿一个命令中心上的命令，没有就报出来。
func commandForTest(t *testing.T, name string) id {
	t.Helper()
	cmd := commandIfPresent(name)
	if cmd == 0 {
		t.Errorf("命令中心上没有 %s", name)
	}
	return cmd
}

// TestTheCommandCenterHasTheCommandsWeNeed 盯需要的命令都在。
//
// 少一个的症状是「控制中心上就一个播放键」——而那在界面上看不出是这一层
// 没接上，只会被当成系统的问题。
func TestTheCommandCenterHasTheCommandsWeNeed(t *testing.T) {
	if commandCenter() == 0 {
		t.Fatal("拿不到 MPRemoteCommandCenter 的单例")
	}
	for _, c := range commandTargets {
		commandForTest(t, c.selector)
	}
	for _, name := range commandsOff {
		commandForTest(t, name)
	}
}

// TestAttachingGetsATokenFromEveryCommand 盯 `addTarget:action:` 真的装上了。
//
// 返回的是 0 就是没装上，而没装上的话系统的行为是**按了没反应**——
// 不报错、不记日志。
func TestAttachingGetsATokenFromEveryCommand(t *testing.T) {
	p := mpTestPlatform(t)
	p.attach(func(Command, time.Duration) {})

	p.mu.Lock()
	tokens := append([]target(nil), p.targets...)
	p.mu.Unlock()

	if len(tokens) != len(commandTargets) {
		t.Fatalf("只装上了 %d 条命令，want %d", len(tokens), len(commandTargets))
	}
	for i, tk := range tokens {
		if tk.token == 0 {
			t.Errorf("%s 没拿到装上去的句柄", commandTargets[i].selector)
		}
	}
	p.detach()

	// 摘干净了：句柄清空，处理器也没了。
	p.mu.Lock()
	rest := len(p.targets)
	attached := p.attached
	p.mu.Unlock()
	if rest != 0 {
		t.Errorf("摘掉之后还留着 %d 个句柄", rest)
	}
	if attached {
		t.Error("摘掉之后还记着自己装着")
	}
	if currentCommandHandler() != nil {
		t.Error("摘掉之后处理器还挂着")
	}
}

// TestAttachingTwiceOnlyInstallsOnce 盯重复 attach 只装一遍。
//
// 装两遍的话同一个按键会被执行两次——一次按键切两首，而界面上看不出任何异样。
func TestAttachingTwiceOnlyInstallsOnce(t *testing.T) {
	p := mpTestPlatform(t)
	p.attach(func(Command, time.Duration) {})
	first := len(p.targets)
	p.attach(func(Command, time.Duration) {})
	if got := len(p.targets); got != first {
		t.Errorf("attach 两次之后有 %d 个句柄，want %d（装了两遍）", got, first)
	}
	p.detach()
}

// TestTheCommandsWeDoNotHandleAreTurnedOff 盯「不接的那些」显式关掉。
//
// ⚠️ 它们**默认是开着的**。不关的话控制中心会摆出一个按下去没反应的按钮
// （快进 15 秒、变速、评分），而「按了没反应」是这一类问题里最难查的一种
// ——用户只会以为耳机坏了。
func TestTheCommandsWeDoNotHandleAreTurnedOff(t *testing.T) {
	p := mpTestPlatform(t)
	p.attach(func(Command, time.Duration) {})
	defer p.detach()

	for _, name := range commandsOff {
		if cmd := commandIfPresent(name); cmd != 0 && commandEnabled(cmd) {
			t.Errorf("%s 我们不接，却还开着", name)
		}
	}
	// 接了的那几条必须是开的，否则控制中心上连播放键都没有。
	for _, c := range commandTargets {
		if cmd := commandIfPresent(c.selector); cmd != 0 && !commandEnabled(cmd) {
			t.Errorf("%s 我们接了，却是关着的", c.selector)
		}
	}
}

// TestAnUnknownSelectorIsRefusedNotFatal 盯不存在的命令名不会把进程带走。
//
// 往一个不响应的选择子发消息，Objective-C 抛 `unrecognized selector`，
// 那是 Go 拦不住的异常。`commandsOff` 上就有一个真实的例子：
// `skipIntervalCommand` 在 macOS 上**不存在**（实测：加上它之后 attach 一调
// 就崩，整个进程死掉）。`commandIfPresent` 就是为它准备的，这里把那个判断
// 单独钉住。
func TestAnUnknownSelectorIsRefusedNotFatal(t *testing.T) {
	if commandIfPresent("skipIntervalCommand") != 0 {
		t.Error("skipIntervalCommand 在 macOS 上不存在，却拿得到")
	}
	if commandIfPresent("noSuchCommandAtAll") != 0 {
		t.Error("一个瞎编的命令名竟然拿得到")
	}
	if commandIfPresent("playCommand") == 0 {
		t.Error("playCommand 明明在")
	}
}

// TestEveryCommandIsRecognised 盯「命令对象认得出是哪一条」。
//
// 这是那一段判断的**全部**——用假的命令对象，不碰真的系统（真的事件对象
// 造不出来，见文件顶上）。
//
// 认错的后果是「按一下跳两首」：表里有两条命令映射到了同一个对象，
// 或者认不出来的时候硬猜一个。这两种都断在这里。
func TestEveryCommandIsRecognised(t *testing.T) {
	// 假的一套：每个选择子对应一个唯一地址。
	objs := map[string]id{}
	lookup := func(name string) id { return objs[name] }
	for i, c := range commandTargets {
		objs[c.selector] = id(0x1000 + i)
	}

	for _, c := range commandTargets {
		if got := commandOf(objs[c.selector], lookup); got != c.cmd {
			t.Errorf("%s 的命令对象被认成了 %v，want %v", c.selector, got, c.cmd)
		}
	}
	if got := commandOf(0, lookup); got != cmdUnknown {
		t.Errorf("空事件被认成了 %v，want 认不出来", got)
	}
	if got := commandOf(id(0xdead), lookup); got != cmdUnknown {
		t.Errorf("表外的东西被认成了 %v，want 认不出来", got)
	}
	// lookup 返回 0 的那几条也不能被当成匹配：0 是「这条命令不存在」，
	// 拿它和 0 比会「匹配上」，于是所有不存在的命令都变成第一条命令。
	empty := func(string) id { return 0 }
	if got := commandOf(0, empty); got != cmdUnknown {
		t.Errorf("lookup 全返回 0 时，空命令对象被认成了 %v，want 认不出来", got)
	}
	if got := commandOf(id(0x1000), empty); got != cmdUnknown {
		t.Errorf("lookup 全返回 0 时，真的命令对象被认成了 %v，want 认不出来", got)
	}
}

// TestTheCommandTableHasNoDuplicates 盯表里没有重名、没有串味。
//
// 两个选择子指向同一个命令对象（改错了、或者将来有人复制了一行），
// 上面的用例就会把其中一条认成另一条。
func TestTheCommandTableHasNoDuplicates(t *testing.T) {
	seen := map[string]string{}
	for _, c := range commandTargets {
		if prev, dup := seen[c.selector]; dup {
			t.Errorf("选择子 %s 出现了两次（%s 和 %s）", c.selector, prev, c.cmd.String())
		}
		seen[c.selector] = c.cmd.String()
	}
	for _, name := range commandsOff {
		for _, c := range commandTargets {
			if name == c.selector {
				t.Errorf("%s 既在接的表里、又在不接的表里", name)
			}
		}
	}
}

// TestSeekPositionClamping 盯跳转位置被夹住。
//
// `positionTime` 是系统给的，单位秒。负数是从那边传过来的坏值，而负的
// 时长会让进度条停在一根不存在的位置上。
func TestSeekPositionClamping(t *testing.T) {
	cases := []struct {
		in   float64
		want time.Duration
	}{
		{0, 0},
		{-1, 0},
		{42.5, 42*time.Second + 500*time.Millisecond},
	}
	for _, c := range cases {
		if got := seekDuration(c.in); got != c.want {
			t.Errorf("positionTime=%v 秒变成了 %v，want %v", c.in, got, c.want)
		}
	}
}

// TestTheTargetIsStillAliveAfterAttaching 盯那个 target 实例不会被回收。
//
// `addTarget:action:` **不 retain target**，系统只记下这个指针。而 Go 这边
// 也管不着 Objective-C 的引用计数——它要是被释放了，系统下一次发消息就是
// 往一段野内存上发，后果是随机的崩溃，而且和「按了一下耳机」毫无关系。
//
// 所以这里断言它还在（创建时的 +1 没有还过，这是有意的）。
func TestTheTargetIsStillAliveAfterAttaching(t *testing.T) {
	if commandTarget() == 0 {
		t.Fatal("target 实例没建出来")
	}
	p := mpTestPlatform(t)
	p.attach(func(Command, time.Duration) {})
	defer p.detach()

	if commandTarget() == 0 {
		t.Fatal("装上之后 target 实例不见了")
	}
	if !sendBool(commandTarget(), "respondsToSelector:", uintptr(sel(commandSelector))) {
		t.Error("target 上没有那个方法——它不是我们注册的那个类的实例")
	}
}

// TestTheHandlerIsSwappedNotAppended 盯换处理器是换掉而不是多挂一个。
//
// 退出流程里 OnWillQuit 和关窗那一条都会走到（见 ui/media_mount.go）。
func TestTheHandlerIsSwappedNotAppended(t *testing.T) {
	p := mpTestPlatform(t)
	p.attach(func(Command, time.Duration) {})
	defer p.detach()

	var got Command
	p.attach(func(c Command, _ time.Duration) { got = c })

	fn := currentCommandHandler()
	if fn == nil {
		t.Fatal("换完之后没有处理器")
	}
	fn(CommandNext, 0)
	if got != CommandNext {
		t.Errorf("当前处理器收到的是 %v，want next", got)
	}
	// 挂上第二个之后句柄仍然是原来那一份（没多装一遍）。
	if got := len(p.targets); got != len(commandTargets) {
		t.Errorf("换处理器之后句柄变成了 %d 个，want %d（多装了一遍）", got, len(commandTargets))
	}
}
