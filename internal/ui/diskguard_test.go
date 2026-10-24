package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 这一份是 `internal/ui` 的**磁盘护栏**：跑之前先看一眼临时目录所在的文件系统
// 还剩多少空间，不够就别跑。
//
// # 为什么要有它
//
// 这台机器是**共享**的：别的 agent 在编译、写盘，用户的应用也在写，可用空间在
// **2.3 GB ~ 104 GB** 之间摆动（go 的编译缓存本身就有 7.5 GB）。
//
// 磁盘写满时，Go 的 `t.TempDir()` 建目录直接失败，测试**在进测试体之前**就红：
//
//	testing.go:1579: TempDir: mkdir /var/folders/.../TestXxx: no space left on device
//	--- FAIL: TestXxx (0.00s)
//
// 报错的行号是 `testing.go`（不是被测代码），红的那条用例和改动毫无关系——
// 而 `internal/ui` 里有 **136 处** `t.TempDir()`，所以它随时会换成别的测试名。
// 顺着「哪个测试红了」去查，会查到一条完全无辜的界面测试（票 #230）。
//
// # 为什么是「一条都不跑 + 退出码非零」，而不是「整包 t.Skip」
//
// `go test` 在**非 `-v`** 模式下会**吞掉通过包的输出**（实测：包里往 stderr 写
// 一行字，`go test ./...` 只打一行 "ok"）。而验收命令恰恰没有 `-v`：
//
//	go test ./internal/ui/ -count=3 -timeout 5m -run '你改的那几个用例名'
//
// 所以「整包 t.Skip」在默认的验收姿势下是**看不见的**——agent 只看到 "ok"，
// 会以为测试真的跑过了。**静默 skip 比红更坏**：红至少会被看见。
//
// 于是这里选的是：**一条测试都不跑**，把话说清楚写在 stderr 上，然后
// **退出码非零**。`go test` 对失败的包会把这段输出原样打出来，于是它没法被
// 误当成「跑过了」——看见那几行字的人只会去查磁盘，不会去查界面代码。
//
// 顺带解决另一样东西：**失败的运行不会被 go test 缓存**（缓存只存通过的），
// 所以磁盘恢复之后重跑就是真的重跑，不会拿到一个假的 "ok (cached)"。

const (
	// minFreeDiskBytes 是门槛：临时目录所在的文件系统剩下的空间低于它，就不跑。
	//
	// 5 GB 这个数**跟 AGENTS.md §2 里「整包扫描前先看磁盘」那条是同一个数**——
	// 人看的那条和机器守的那条必须是同一个数，否则会出现「机器说够、人说不够」
	// （或者反过来），两边打架的时候没人知道该信谁。
	//
	// 为什么不是「等到真的建不出来才停」：写满的那一刻，测试会红在一条**无辜的**
	// 用例上（见上面那段报错），而那正是这个护栏要拦掉的东西。留 5 GB 的余量
	// 是为了让「跑一半写满」这件事压根不发生——半路炸掉比一开始就停更难看。
	minFreeDiskBytes = 5 << 30

	// allowLowDiskEnv 是**逃生阀**：设了它就照跑，不看空间。
	//
	// 什么时候用：量出来的那块盘不是测试真正要用的那块（`TMPDIR` 指到了另一块
	// 盘），或者人已经拍板「就现在跑」。名字沿用本仓库 `BIU_*` 的规矩。
	allowLowDiskEnv = "BIU_TEST_ALLOW_LOW_DISK"
)

// TestMain 是这一包的门：空间不够就一条测试都不跑。
func TestMain(m *testing.M) {
	free, err := freeDiskBytes()
	run, why := shouldRunPackage(free, err, os.Getenv(allowLowDiskEnv) != "")
	// why 非空就说明有话要说：要么是拦下来的理由，要么是硬跑的那句提醒。
	if why != "" {
		fmt.Fprint(os.Stderr, why)
	}
	if !run {
		os.Exit(1)
	}
	// 本包的老用例量的是经典外观（见 `app.DefaultThemePreset` 的说明）。
	app.DefaultThemePreset = app.ThemePresetClassic
	os.Exit(m.Run())
}

// shouldRunPackage 决定这一包要不要跑，并把「为什么」写成一句能直接贴给
// 人看的话。
//
// 两个参数的分工：
//
//   - free 是量出来的可用字节数；err 非 nil 表示**量不出来**。
//   - forced 是逃生阀有没有被设上。
//
// **量不出来不等于空间不够**，而拦下一整包测试的代价（没人知道它被拦了，
// 于是「界面测试通过了」这句话就变成了假话）比跑一次更贵——所以量不出来
// 就照常跑。
func shouldRunPackage(free uint64, err error, forced bool) (run bool, why string) {
	if err != nil {
		return true, ""
	}
	if free >= minFreeDiskBytes {
		return true, ""
	}
	if forced {
		return true, fmt.Sprintf("[%s] 已设置：磁盘只剩 %.1f GB，照跑（风险自负）。\n",
			allowLowDiskEnv, asGB(free))
	}
	return false, fmt.Sprintf(`
================ 磁盘空间不足：internal/ui 一条测试都没有跑 ================
可用空间 %.1f GB < 门槛 %.1f GB（量的是临时目录所在的文件系统：%s）。
这不是被测逻辑的红：磁盘写满时 t.TempDir() 建不出来，测试会在进测试体之前就红，
报出来的是一条和改动毫无关系的用例（testing.go:1579: no space left on device）。
先腾空间再跑：go clean -cache 能腾出几 GB，但会让别的 agent 全部重编——
共享机器上这是伤人的动作，先跟人说；或者设 %s=1 硬跑。
==========================================================================
`, asGB(free), asGB(minFreeDiskBytes), os.TempDir(), allowLowDiskEnv)
}

// asGB 把字节数说成人看得懂的 GB。
func asGB(b uint64) float64 { return float64(b) / (1 << 30) }

// TestTheDiskGuardStopsThePackageOnlyWhenThereIsNoRoom 钉住这道护栏的判据：
// 空间够就放行、量不出来就放行、空间不够且没人拍板才拦。
//
// 这一条不是洁癖：判据写错了，两头都出事——**放行得太松**，磁盘满时又红一条
// 无辜的用例（本护栏存在的理由）；**拦得太紧**，一次误判就让一整包界面测试
// 悄悄没跑，而报告里写着「界面测试通过」。
func TestTheDiskGuardStopsThePackageOnlyWhenThereIsNoRoom(t *testing.T) {
	const oneGB = uint64(1) << 30

	// 空间够：放行。
	if run, _ := shouldRunPackage(10*oneGB, nil, false); !run {
		t.Fatal("空间够的时候不该拦")
	}
	// 刚好到门槛：放行（门槛是「低于」，不是「不高于」）。
	if run, _ := shouldRunPackage(minFreeDiskBytes, nil, false); !run {
		t.Fatal("刚好等于门槛的时候不该拦")
	}
	// 量不出来：放行——量不出来不等于空间不够。
	if run, _ := shouldRunPackage(0, os.ErrPermission, false); !run {
		t.Fatal("量不出来的时候不该拦")
	}
	// 空间不够：拦，而且要说清楚为什么。
	run, why := shouldRunPackage(oneGB, nil, false)
	if run {
		t.Fatal("空间不够的时候该拦")
	}
	for _, want := range []string{"磁盘空间不足", "一条测试都没有跑", "testing.go:1579", allowLowDiskEnv} {
		if !strings.Contains(why, want) {
			t.Errorf("拦下的话里没有 %q：\n%s", want, why)
		}
	}
	// 逃生阀：空间不够但有人拍板了，照跑，而且要说一句。
	run, why = shouldRunPackage(oneGB, nil, true)
	if !run {
		t.Fatal("设了逃生阀就不该拦")
	}
	if !strings.Contains(why, allowLowDiskEnv) {
		t.Errorf("硬跑的那句里没有逃生阀的名字：\n%s", why)
	}
}
