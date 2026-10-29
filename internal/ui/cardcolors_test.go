package ui

import (
	"fmt"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 这一组是「关注页那张卡的三个颜色只算一次」（票 #632）。
//
// # 这一票在做什么
//
// 关注页每一张卡、每一帧都要用三个值：**卡片底色**、**卡片圆角**、**卡片悬停
// 那一档底色**。这三个值只依赖两样东西——**主题方案**和**深浅**——而原来它们
// **各自去读一遍设置**（`themepreset.go` 的 `cardBackground` / `cardRadius` /
// `cardHoverBackground`）。一屏四张卡、每一帧就是**几百次**「读设置」
// （实测整窗 400 次/帧、隔离 48 次/帧）。
//
// 新的做法是跟主题本身**同一层地**算一次、存起来，各处去读那一份——而那一层
// 就是 `themeCache`（它的键里就含方案和深浅）。
//
// # 判据为什么是「数调用次数」而不是「数分配」
//
// 帧预算量的是**分配次数**，而那是「读了几次设置」的**间接**后果（一次读 =
// 一次锁 + 一次几百字节的拷贝 + 一次堆分配）。这一票要做的事是「**读几次**」，
// 所以要有一���**直接**数它的证据——`app.TakeSettingsReadCount()`（票 #631 留的
// 那个口子）数的就是 `App.SettingsSnapshot()` 的调用次数。
//
// ⚠️ **分配次数照旧会降**（省掉了每张卡那几次几百字节的拷贝），但那是另一条
// 证据（帧预算那一条），别拿它当这一票的判据。

// ─────────────────────────────────────────────────────────────────────────────
// 夹具
// ─────────────────────────────────────────────────────────────────────────────

// cardColorShell 造一台停在关注页的**整窗**外壳，卡片数由 n 给。
//
// ⚠️ **走假 B 站**（`newUIExplore`）：整窗口径画的是**当前路由**那一页，而
// 那一页的数据是后台拉回来的——挂真 B 站的话拉关注失败会把这一页换成错误态，
// 那样量到的压根不是关注页（实测那样量到的是 10 次/帧，一张卡都没画）。
//
// ⚠️ **等后台那一趟落地**：卡片是拉回来的，不是摆进去的——而「还没拉回来」
// 那一帧画的是加载态（一张卡都没有），量到的不是「一屏关注卡片」。
//
// ⚠️ **画的是整窗**（没有 `withView`）：这一档量的是「用户感受到的那一帧」——
// 侧栏、顶栏、播放条都要算进去。
func cardColorShell(t *testing.T, n int, opts ...shellOpt) (*Shell, *mygoui.Tester, *app.App) {
	t.Helper()
	users := make([]any, n)
	for i := range users {
		users[i] = uiFollowUser(int64(11+i), fmt.Sprintf("第 %d 个 UP", i+1))
	}
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	uiFollowList(b, n, users)
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})

	all := append([]shellOpt{withSettings(app.DefaultSettings("")), reduceMotion()}, opts...)
	s, tt, a := newSpaceShell(t, b, true, "77", all...)
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "人到了", func() bool { return len(f.users) == n && f.tagStarted })
	// ⚠️ **先画几帧**：这一页的网格是**虚拟的**，而「列数」第一帧还没量出来
	//（`Bounds` 量的是上一帧的盒子），头几帧的列数是**估的**。画稳了量到的才是
	//「用户看见的那一屏」（理由同 `measureFrames` 里 `warmFrames` 那一段）。
	for range warmFrames {
		tt.Frame()
	}
	return s, tt, a
}

// cardColorIsolatedShell 造一台**只画关注页那一块**的外壳。
//
// ⚠️ **隔离与整窗是两个口径**：帧预算是隔离量的、而用户感受是整窗的。两者
// 都要量，因为整窗里多出来的是侧栏 / 顶栏 / 播放条那些地方要读的设置——
// **这一票改的是关注页卡片那三个颜色**，所以整窗的数不会掉到个位数。
//
// ⚠️ **`withView` 换掉的是整支 view**：于是 `syncFollow`（在 `viewMain` 的
// 钩子里）不会被调到、后台那一趟也不会发——所以**卡片得自己摆进去**。
//
// ⚠️ **它自己数「画了几遍」**：整窗那一档数的是播放条建了几遍（每一遍 view
// 恰好建一次播放条），而隔离这一档播放条压根不画——所以这一档数自己那个。
func cardColorIsolatedShell(t *testing.T, n int) (*Shell, *mygoui.Tester, *app.App, *int64) {
	t.Helper()
	builds := new(int64)
	all := []shellOpt{
		budgetFollowMount(),
		withSettings(app.DefaultSettings("")),
		reduceMotion(),
		withView(func(sh *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) {
				*builds++
				sh.viewFollow(c, nil)
			}
		}),
	}
	b := newTestShell(t, all...)
	s, tt, a := b.s, b.tt, b.a
	f := s.followPageOf()
	f.tagStarted = true
	f.tags = []app.FollowTag{{ID: 0, Name: "默认分组", Count: n}}
	f.total = n
	f.pager.started = true
	f.users = budgetFollowUsers(n)
	for range warmFrames {
		tt.Frame()
	}
	return s, tt, a, builds
}

// cardColorFrameReads 量「画一遍关注页读了几次设置」。
//
// 量法是「归零 → 画一帧 → 取走增量」（理由见 `app.TakeSettingsReadCount` 的
// 注释）。
//
// ⚠️ **它量的是「一遍」，不是「一帧」**：`tt.Frame()` 可能画**好几遍**（切页
// 动画还没走完时它会一直画到不再要帧），而每遍都要读一遍设置——于是那个数会在
// 「1 遍」和「20 遍」之间跳（实测 8 次 vs 160 次）。⇒ 所以这里把**读设置的
// 次数除以画了几遍**，量的是「**画一遍**关注页读了几次设置」。
//
// ⚠️ **遍数从哪儿来**：隔离那一档是**夹具自己数的**（`cardColorIsolatedShell`
// 里那个计数器——那一档的 view 只画关注页，播放条压根不建）；整窗那一档是
// **播放条建了几遍**（`buildTally.playbar`）——每一遍 view 恰好建一次播放条
// （见 `playbar.go`），而它每遍都要读一次设置（读曲目的那一档）。
//
// ⚠️ **除法要求遍数大于 0**：一帧一次都不画的情形（画崩了 / 路由没匹配）会让
// 它除零——那种情形要报错，而不是静悄悄地当成 0。
func cardColorFrameReads(t *testing.T, builds *int64, tt *mygoui.Tester) int64 {
	t.Helper()
	*builds = 0
	app.TakeSettingsReadCount() // 归零
	tt.Frame()                  // 画一帧
	reads := app.TakeSettingsReadCount()
	if *builds <= 0 {
		t.Fatalf("这一帧一遍关注页都没画（读设置 %d 次）——量到的不是「画一遍关注页」",
			reads)
	}
	return reads / *builds
}

// cardColorWholeFrameReads 是整窗口径的那一份：遍数从**播放条建了几遍**拿。
//
// ⚠️ **为什么整窗不能数「关注页画了几遍」**：整窗那一档画的是**当前路由**那一
// 页——而切页动画进行中时，当前路由可能**已经不是**关注页了（那一遍画的是别的
// 页面）。播放条是**每一遍 view 都建**的，所以它数的是「整支 view 画了几遍」
// ——那才是这一档要除的那个数。
func cardColorWholeFrameReads(t *testing.T, s *Shell, tt *mygoui.Tester) int64 {
	t.Helper()
	s.builds = buildTally{}
	app.TakeSettingsReadCount() // 归零
	tt.Frame()                  // 画一帧
	reads := app.TakeSettingsReadCount()
	builds := int64(s.builds.playbar)
	if builds <= 0 {
		t.Fatalf("这一帧一遍 view 都没建（读设置 %d 次）——量到的不是「画一遍整窗」",
			reads)
	}
	return reads / builds
}

// ─────────────────────────────────────────────────────────────────────────────
// ① 每帧读设置的次数：卡多起来不再涨
// ─────────────────────────────────────────────────────────────────────────────

// TestTheFollowCardColorsAreNotRecomputedPerCard 钉住「卡片那三个颜色**只算
// 一次**」这件事。
//
// # 判据：卡多起来，**每帧读设置的次数不再涨**
//
// 这是**行为**判据，不是实现细节：「一屏卡片从 4 张变成 20 张，每帧读设置的
// 次数不许跟着涨」——那三个值只依赖主题方案和深浅，而那两样在这一帧里没变，
// 所以多出来的那些卡**不该**多读一次设置。
//
// ⚠️ **为什么不直接断「读了几次」那个绝对数**：那一头还混着别的地方要读的
// 设置（侧栏、顶栏、播放条——它们各读各的，不是这一票的账）。断**差值**断的是
// 这一票真正做的那件事：**每张卡不再多读一次**。
//
// # 基线（实测，2026-10-28，这棵树上，改之前）
//
//	隔离（只画关注页那一块）：
//	  1 张卡   6 次/帧
//	  4 张卡  15 次/帧
//	 20 张卡  48 次/帧   ← 卡够多封顶那一档
//
//	整窗：
//	  1 张卡 220 次/帧
//	  4 张卡 400 次/帧
//	 20 张卡 760 次/帧
//
// ⇒ 隔离那一档「每张卡」的成本是 (48-6)/19 ≈ 2.2 次，而整窗那一档是
// (760-220)/19 ≈ 28 次——**两个口径差得远**，因为整窗里每一张卡还会带出
// 侧栏 / 顶栏 / 播放条那些要读设置的地方（那一段是**别的票**的账）。
func TestTheFollowCardColorsAreNotRecomputedPerCard(t *testing.T) {
	const few, many = 4, 20

	// 隔离口径：只画关注页那一块。
	_, fewTT, _, fewBuilds := cardColorIsolatedShell(t, few)
	fewReads := cardColorFrameReads(t, fewBuilds, fewTT)

	_, manyTT, _, manyBuilds := cardColorIsolatedShell(t, many)
	manyReads := cardColorFrameReads(t, manyBuilds, manyTT)

	// ⛔ **反向验证的锚点**：卡片多的那一帧读设置**不许比卡片少的还多**。
	//
	// 原来的实现是「每一张卡各自去读一遍设置」，所以卡多的那一帧读得**更多**
	// ——实测 15 次 vs 48 次。这一条现在会红（改完之后是同一量级）。
	//
	// ⚠️ **为什么不是「不许涨」而是「不许多」**：改完之后两边应该**一样**
	// （那三个值只算一次），而「不许涨」会漏掉「涨了一点」那种半吊子。
	// 这里断的是「卡多的那帧读设置**不多于**卡少的那帧」。
	if manyReads > fewReads {
		t.Errorf("卡片多的那一帧读设置更多：%d 张卡读 %d 次，%d 张卡读 %d 次——"+
			"卡片那三个颜色（底色 / 圆角 / 悬停底）又被算成「每张卡一次」了",
			many, manyReads, few, fewReads)
	}

	// ⚠️ **还要断一个绝对数**：那三个值只算一次，于是「跟卡片数有关的那部分」
	// 应该是**零**——隔离那一档每帧读设置的次数，卡片从 4 张到 20 张，一个不多。
	//
	// 上限取 **4 次**：那是改完之后实测的那个量（隔离口径，卡片多到封顶），
	// 留一点余量给「别的地方也要读」——那一档实测是 **1 次**（主题自己那一次
	// `applyFrameTheme` 里的读）。
	if manyReads > 4 {
		t.Errorf("隔离口径：%d 张卡时每帧读设置 %d 次，上限 4 次——"+
			"那三个颜色多半还在每张卡上一次（改之前实测是 48 次）", many, manyReads)
	}
}

// TestTheWholeWindowStopsReadingSettingsPerCard 钉住**整窗**那个口径。
//
// ⚠️ **整窗的数不会掉到个位数**：多出来的是侧栏 / 顶栏 / 播放条那些地方要读的
// 设置（那是别的票的账）。这一条断的是**差值**：卡多起来，**每张卡**带来的那
// 一份读设置没了。
//
// 基线（实测，改之前）：1 张卡 220 次、4 张卡 400 次、20 张卡 760 次。
// ⇒ 每张卡约 28 次（整窗口径）。
//
// ⚠️ **「一帧」这个单位在这里是稳的**（夹具开了 `reduceMotion()`）：不���的话
// `tt.Frame()` 会画**好几遍**（切页动画还没走完），而每遍都要读一遍设置——那个
// 遍数是 10 还是 20 取决于动画走到哪，于是「一帧读了几次」在 80 和 160 之间跳。
// 开了它，「一帧」就是一遍（理由同 `framebudget_test.go` 文件头「为什么两条
// 测试都开 ReduceMotion」那一段）。
//
// ⚠️ **上限给得宽**：整窗里那些别的地方要读多少设置，跟这一票无关，而它们
// 那一档（侧栏、顶栏、播放条）**还没被优化**。所以这里断的是「比基线低很多」
// 而不是「某个绝对数」。
func TestTheWholeWindowStopsReadingSettingsPerCard(t *testing.T) {
	const few, many = 4, 20

	fewShell, fewTT, _ := cardColorShell(t, few)
	fewReads := cardColorWholeFrameReads(t, fewShell, fewTT)

	manyShell, manyTT, _ := cardColorShell(t, many)
	manyReads := cardColorWholeFrameReads(t, manyShell, manyTT)

	// 改之前：4 张 400 次、20 张 760 次 ⇒ 每张卡约 28 次（整窗口径）。
	// 改之后：那 28 次/张 没了——**实测（每遍 view 归一之后）**：4 张 8 次、
	// 20 张 8 次，**差 0**。
	//
	// ⚠️ **余量 4 次**：整窗里那些别的地方要读设置（侧栏 / 顶栏 / 播放条 /
	// 主题），而它们**还没被优化**。所以这里断的是「卡多的那
	// 一遍**没有按卡数多出读设置**」，而不是「整窗读得少」。
	//
	// ⚠️ **改之前这个差值是 18**（每遍 view：4 张 20 次、20 张 38 次），
	// 而改完之后是 **0**——所以 4 这个余量照不红那个坏掉的样子。
	if manyReads > fewReads+4 {
		t.Errorf("整窗：%d 张卡读 %d 次，%d 张卡读 %d 次——多出来的那 %d 次是"+
			"卡片那三个颜色每张卡算了一次（改之前每遍 view 多 18 次）",
			many, manyReads, few, fewReads, manyReads-fewReads)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ② 三个值的数值：一个字节都不许变
// ─────────────────────────────────────────────────────────────────────────────

// TestTheCardColorsAreExactlyWhatTheyWere 钉住「三个值和现在**完全一致**」。
//
// # 判据：算出来的三个值和「按老算法现算的」逐字节相同
//
// 老算法就是那三行（方案里是 `surface1` / `radiusLg` / 底色往文字色推 4%；
// 经典是 `Surface` / 圆角 × 1.5 / 底色往黑或白推 `surfaceStep`）——**照抄在
// 下面那个 `wantCardPalette` 里**，而它要跟**生产代码算出来的那一份**比。
//
// ⚠️ **照抄不是为了重复实现**：这是「这一票只改这些值**怎么算出来的**，不改
// 它们**是多少**」那条验收的钉子——照抄的那一份是**独立的一份算术**，而生产
// 那一份是**缓存里存着的**。两者要是对不上，是「算错了」的信号。
//
// ⚠️ **深浅两档都跑**：方案里浅色和深色是**两套独立调校的值**（不是反相），
// 而经典那一档的悬停推的量也不一样（浅色往黑、深色往白）。
func TestTheCardColorsAreExactlyWhatTheyWere(t *testing.T) {
	for _, preset := range []app.ThemePreset{app.ThemePresetClassic, app.ThemePresetWarmPaper, app.ThemePresetInkPaper} {
		for _, dark := range []bool{false, true} {
			st := app.DefaultSettings("")
			st.ThemePreset = preset
			st.ThemeMode = app.ThemeModeLight
			if dark {
				st.ThemeMode = app.ThemeModeDark
			}
			s, tt, _ := cardColorShell(t, 4, withSettings(st))
			th := BuildTheme(st, dark)
			got := s.cardStyleOf(th)
			want := wantCardPalette(th, preset)
			if got != want {
				t.Errorf("%s（dark=%v）：卡片那三个值是 %+v，该是 %+v",
					preset, dark, got, want)
			}
			_ = tt
		}
	}
}

// wantCardPalette 是「按老算法现算的那一份」——照抄 `cardPaletteOf` 之前那三个
// 函数里的算法，用来钉「一个字节都不许变」。
//
// ⚠️ **这是有意重复**：老算法已经不在生产代码里了（它现在住在
// `cardPaletteOf` 里），而这一份是**照着老算法抄的**——两者对不上就是回归。
func wantCardPalette(th *mygoui.Theme, preset app.ThemePreset) cardPalette {
	if sk, shape, ok := presetSkinFor(preset, th.Dark); ok {
		bg := mygoui.Hex(sk.surface1)
		return cardPalette{
			bg:      bg,
			radius:  shape.radiusLg,
			hover:   bg.Mix(th.Text, presetHoverMix),
			sidebar: mygoui.Hex(sk.sidebar),
			preset:  true,
		}
	}
	bg := th.Surface
	mix := mygoui.Color{A: 255}
	step := surfaceStepLight
	if th.Dark {
		mix = mygoui.Color{R: 255, G: 255, B: 255, A: 255}
		step = surfaceStepDark
	}
	return cardPalette{
		bg:      bg,
		radius:  th.Radius * 1.5,
		hover:   bg.Mix(mix, step),
		sidebar: th.Background,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ③ 换了方案 / 系统深浅变了，立刻跟着变
// ─────────────────────────────────────────────────────────────────────────────

// TestTheCardColorsFollowThePresetChange 钉住「换了主题方案，卡片底色和圆角
// **立刻**跟着变」。
//
// # 判据：改完设置的**下一帧**，那三个值就是新方案的那一份
//
// ⚠️ **「立刻」这一条是这一票的风险所在**：卡片那三个值现在住在**缓存**里，
// 而缓存的键是 `themeKey`——它含**方案**和**深浅**（`theme.go` 里 `themeKey`
// 的注释写得很清楚）。于是「键变了就重算」=「改了立刻生效」。
//
// ⚠️ **走 `UpdateSettings` 而不是直接改字段**：那份设置在 `SettingsStore` 里，
// 而 `SettingsSnapshot` 是从那儿拿的——直接改 `s.app` 上的字段改不到它。
func TestTheCardColorsFollowThePresetChange(t *testing.T) {
	st := app.DefaultSettings("")
	st.ThemePreset = app.ThemePresetClassic
	st.ThemeMode = app.ThemeModeLight
	s, tt, a := cardColorShell(t, 4, withSettings(st))

	before := s.cardStyleOf(s.theme.base)
	if before.bg != s.theme.base.Surface {
		t.Fatalf("经典那一档的卡片底该是控件那一层 %v，却是 %v",
			s.theme.base.Surface, before.bg)
	}

	// 换成墨宣：下一帧就该是墨宣的那一份。
	//
	// ⚠️ **为什么是墨宣而不是暖纸**：经典那一档的圆角是「主题圆角 × 1.5」
	// （默认 8 → 12），而**暖纸的 `radiusLg` 恰好也是 12**——换过去圆角**看起来
	// 没变**，于是「换了方案圆角立刻跟着变」这条判据就断不出来了。墨宣的
	// `radiusLg` 是 **8**，和经典的 12 差得开。
	if _, err := a.UpdateSettings(func(cur *app.Settings) {
		cur.ThemePreset = app.ThemePresetInkPaper
	}); err != nil {
		t.Fatalf("换方案：%v", err)
	}
	tt.Frame()

	after := s.cardStyleOf(s.theme.base)
	sk, shape, _ := presetSkinFor(app.ThemePresetInkPaper, false)
	want := mygoui.Hex(sk.surface1)
	if after.bg != want {
		t.Errorf("换了方案之后卡片底是 %v，该是墨宣的 surface1 %v——"+
			"「立刻生效」这条断了（缓存的键里没有方案？）", after.bg, want)
	}
	if after.radius != shape.radiusLg {
		t.Errorf("换了方案之后卡片圆角是 %v，该是 radiusLg %v", after.radius, shape.radiusLg)
	}
	if after.bg == before.bg || after.radius == before.radius {
		t.Errorf("换了方案之后那三个值一个都没变（%+v → %+v）", before, after)
	}
}

// TestTheCardColorsFollowTheSystemDarkChange 钉住「系统在浅色/深色之间切换时，
// 卡片**立刻**跟上」。
//
// ⚠️ **两档是独立调校的**：方案里浅色和深色是**两套值**（不是反相），而经典
// 那一档的悬停推的量也不一样（浅色往黑、深色往白）——所以这一条断的是
// 「换档之后那三个值真的变了」，而不只是「主题变了」。
//
// ⚠️ **`SetDark` 是 Tester 的偏好**（每帧开头框架会照它重置主题），不是系统的
// 真实状态——而主题那一组测试要的就是这一条：设置说浅色、系统说深色，才验得
// 出「设置压过了系统」。这一条反过来：设置说「跟随系统」，系统变了就该跟着变。
func TestTheCardColorsFollowTheSystemDarkChange(t *testing.T) {
	st := app.DefaultSettings("")
	st.ThemePreset = app.ThemePresetWarmPaper
	st.ThemeMode = app.ThemeModeSystem
	s, tt, _ := cardColorShell(t, 4, withSettings(st), systemIsDark(false))

	light := s.cardStyleOf(s.theme.base)
	lightSk, _, _ := presetSkinFor(app.ThemePresetWarmPaper, false)
	if light.bg != mygoui.Hex(lightSk.surface1) {
		t.Fatalf("浅色那一档的卡片底是 %v，该是 %v", light.bg, mygoui.Hex(lightSk.surface1))
	}

	// 系统切到深色。
	tt.SetDark(true)
	tt.Frame()

	dark := s.cardStyleOf(s.theme.base)
	darkSk, darkShape, _ := presetSkinFor(app.ThemePresetWarmPaper, true)
	if dark.bg != mygoui.Hex(darkSk.surface1) {
		t.Errorf("系统切到深色之后卡片底是 %v，该是深色那一档的 surface1 %v——"+
			"「立刻跟上」这条断了（缓存的键里没有深浅？）", dark.bg, mygoui.Hex(darkSk.surface1))
	}
	if dark.radius != darkShape.radiusLg {
		t.Errorf("系统切到深色之后卡片圆角是 %v，该是 radiusLg %v", dark.radius, darkShape.radiusLg)
	}
	if dark.bg == light.bg {
		t.Errorf("系统切到深色之后卡片底没变（%v）", dark.bg)
	}
	if dark.hover == light.hover {
		t.Errorf("系统切到深色之后悬停那一档没变（%v）", dark.hover)
	}
}
