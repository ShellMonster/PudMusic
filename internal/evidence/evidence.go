// Package evidence 是 SPEC「各阶段要有可保存的回归证据」那张清单的代码形态。
//
// # 为什么是一张 Go 表，而不是一份文档
//
// 那张清单以前只活在 SPEC 里，而 SPEC 里的东西没有「会过期」这个属性：
// 测试删了、挪了、改名了，清单上还写着它，而没人会发现——**证据没了，
// 台账还说自己有**。
//
// 写成代码之后它就会过期：这里每一条证据都是**一个测试函数名**，
// `evidence_test.go` 每次跑都去全仓库扫一遍 `func TestXxx(`。
// 少一个、挪了地方、改了名，那一条立刻红。
//
// 台账里**不写「已通过」**——那是报告的事，不是代码的事。代码只回答
// 「这一条有没有证据」，过没过由 `go test` 自己说。
//
// # 怎么用
//
// 往里加一行���需求来自 SPEC 的哪一句、要拿哪几条测试当证据。要是真机上
// 才有意义的（红黄绿、输入法、系统音频），把 RealOnly 标上——
// 那几条在无头环境里量不到，标出来是为了没人把它们算进「已验完」。
package evidence

// 规格里那几组的名字。用它们当分类是为了让台账和 SPEC 一眼对得上。
const (
	// Play 是「播放」：模式、队列、代次、解码失败、快速切歌不留东西。
	Play = "播放"
	// FakeClock 是「假时钟」：该等的时间都不真等。
	FakeClock = "假时钟"
	// HTTP 是「严格 HTTP fixture」：WBI、CSRF、凭据边界、错误分类。
	HTTP = "严格 HTTP fixture"
	// PCM 是「PCM」：声道排列、位深、seek、EOF、截断必须报错。
	PCM = "PCM"
	// Paging 是「分页与路由」。
	Paging = "分页与路由"
	// Platform 是「双平台」：窗口、输入法、音频设备、平台接线。
	Platform = "双平台"
	// Negative 是「负路径」：异步取消、断网、错误、重启。
	Negative = "负路径"
)

// Categories 是必须全部有证据的那几组。
//
// 少一组没人拦得住，而「少了一组」的样子和其他漏项一模一样：
// 台账读起来仍然是完整的一篇。
var Categories = []string{Play, FakeClock, HTTP, PCM, Paging, Platform, Negative}

// Item 是一条规格要求，连同证明它的那几条测试。
type Item struct {
	// Category 是它属于哪一组（见上面那几个常量）。
	Category string
	// Spec 是 SPEC 里那句话的短写。
	Spec string
	// Tests 是证据：测试函数名（不带包名）。
	Tests []string
	// RealOnly 标出「无头环境量不到、只有真机能验」的证据。
	//
	// 标它的理由和 SPEC「真窗口只验三件事」一样：这几条**不标**的话，
	// 「`go test` 全绿」会被读成「全验过了」，而红黄绿、输入法、
	// 系统出声这三样恰好是无头跑不出来的。
	RealOnly bool
}

// Items 是台账本体。
//
// ⚠️ 加条目之前先看一眼 `evidence_test.go`：它要求每一条列出来的测试
// 都真的存在。写错了名字不要紧，那一条会红——**那正是它该做的**。
var Items = []Item{
	// ── 播放 ───────────────────────────────────────────────────────────────
	{
		Category: Play,
		Spec:     "队列顺序：下一首/上一首走到头会绕回来",
		Tests:    []string{"TestQueueMovesBothWaysAndWrapsAround", "TestNextAndPrevWalkTheQueue"},
	},
	{
		Category: Play,
		Spec:     "取消代次：连点五首，只有最后一首能活",
		Tests: []string{
			"TestOnlyTheLastTrackWinsWhenSwitchingFast",
			"TestALateFailureFromAStaleSwitchNeverReachesThePlaybar",
		},
	},
	{
		Category: Play,
		Spec:     "快速切歌之后不留 goroutine 和文件句柄",
		Tests: []string{
			"TestRapidSwitchingLeavesNoGoroutinesNorHandles",
			"TestSwitchingTracksLeaksNoFileHandles",
		},
	},
	{
		Category: Play,
		Spec:     "**解码失败不能被当成播放结束**",
		Tests: []string{
			"TestACorruptFileNeverLooksLikeItFinished",
			"TestAReadFailureIsNotAnEndOfFile",
			"TestMidPlaybackDeathSurfacesOnThePlaybar",
		},
	},
	{
		Category: Play,
		Spec:     "错误不许粘住：坏文件之后放好文件，那句话要清掉",
		Tests:    []string{"TestTheErrorDoesNotStickToTheNextSong"},
	},
	{
		Category: Play,
		Spec:     "系统媒体状态映射（暂停/停止/播完/解码失败不是同一个数）",
		Tests:    []string{"TestPlaybackStateMapping", "TestSmtcStatusIsNotTheMacNumber"},
	},

	// ── 假时钟 ─────────────────────────────────────────────────────────────
	{
		Category: FakeClock,
		Spec:     "假时钟按到点顺序跑，且不等墙钟",
		Tests: []string{
			"TestFakeClockDoesNotWaitInRealTime",
			"TestFakeClockFiresInDueOrder",
			"TestFakeClockStopEndsIt",
		},
	},
	{
		Category: FakeClock,
		Spec:     "Toast 两秒自己消失",
		Tests:    []string{"TestToastGoesAwayOnItsOwn", "TestFakeClockDoesNotWaitInRealTime"},
	},
	{
		Category: FakeClock,
		Spec:     "真时钟那一半也接得上（醒得过来、停得下来）",
		Tests:    []string{"TestRealClockEveryFuncWakesAndStops"},
	},

	// ── 严格 HTTP fixture ──────────────────────────────────────────────────
	{
		Category: HTTP,
		Spec:     "WBI 签名对着公布的那组向量",
		Tests:    []string{"TestWBISignatureMatchesThePublishedVector", "TestWBIKeysAreFetchedOnceAndCached"},
	},
	{
		Category: HTTP,
		Spec:     "CSRF 只在写调用上注入，登出后不注入",
		Tests:    []string{"TestCSRFIsInjectedOnWriteCalls", "TestNoCSRFWhenLoggedOut"},
	},
	{
		Category: HTTP,
		Spec:     "凭据边界：B 站 API 带、外站不带、降级不带",
		Tests: []string{
			"TestTransportKeepsCredentialsOnBiliAPI",
			"TestTransportStripsCredentialsFromForeignHosts",
			"TestTransportDropsCredentialsOnHTTPSDowngrade",
		},
	},
	{
		Category: HTTP,
		Spec:     "错误分类：接口返回非零 → 有哨兵 → 有一句人话",
		Tests: []string{
			"TestErrorCodesMapToSentinels",
			"TestEveryKnownCodeHasAHumanSentence",
			"TestUnknownCodeIsJustAnAPIError",
		},
	},
	{
		Category: HTTP,
		Spec:     "风控响应（非 JSON）被认出来，不当普通错误",
		Tests:    []string{"TestNonJSONResponseIsRiskControl"},
	},
	{
		Category: HTTP,
		Spec:     "取流超时会回来，不是挂着",
		Tests:    []string{"TestSlowServerIsCutOffByTheTimeout", "TestALateStarterDoesNotWaitForTheTimeout"},
	},

	// ── PCM ────────────────────────────────────────────────────────────────
	{
		Category: PCM,
		Spec:     "声道排列（左右必须用**不同**的信号量）",
		Tests:    []string{"TestLeftRightDiffer", "TestPauseIsLeftRightSymmetric"},
	},
	{
		Category: PCM,
		Spec:     "位深与声道数：解码出来的数对得上文件里写的",
		Tests:    []string{"TestAACDecodesToTheDocumentedNumbers", "TestAIFFDecodesBackToTheSameSamples", "TestMono"},
	},
	{
		Category: PCM,
		Spec:     "seek：落点精确、能往回、越界不越",
		Tests:    []string{"TestSeekIsExact", "TestSeekCanGoBackwards", "TestSetPositionRejectsOutOfRange"},
	},
	{
		Category: PCM,
		Spec:     "EOF 与**截断输入必须报错**",
		Tests: []string{
			"TestEOF",
			"TestTruncated",
			"TestAReadFailureIsNotAnEndOfFile",
			"TestACorruptFileNeverLooksLikeItFinished",
		},
	},

	// ── 分页与路由 ─────────────────────────────────────────────────────────
	{
		Category: Paging,
		Spec:     "14 条路由一条不少，参数路由认得出",
		Tests:    []string{"TestFourteenRoutes", "TestParamRoutesMatch", "TestUnknownRouteShowsThe404Page"},
	},
	{
		Category: Paging,
		Spec:     "切路由：上一页的滚动位置留着，回来还在原处",
		Tests:    []string{"TestLeavingTheHomePageAndComingBackKeepsWhereYouWere"},
	},
	{
		Category: Paging,
		Spec:     "翻页中切查询：在途那一页被取消，迟到的结果不落地",
		Tests:    []string{"TestReloadCancelsThePageInFlight", "TestAStaleResultNeverLands"},
	},
	{
		Category: Paging,
		Spec:     "容器没填满会自动补屏，且不会一次补三屏",
		Tests: []string{
			"TestFirstRenderKeepsPullingUntilTheContainerIsFilled",
			"TestTheThreeGuardsDoNotSendThreeRequestsForOnePage",
		},
	},
	{
		Category: Paging,
		Spec:     "翻页失败解锁、给重试，且不会自己绕圈",
		Tests:    []string{"TestAFailedPageUnlocksAndOffersARetry", "TestAFailureOnTheVeryFirstPageLeavesOneWayOut"},
	},
	{
		Category: Paging,
		Spec:     "404 三秒自动回首页，离开就不倒计时",
		Tests:    []string{"TestNotFoundRedirectsHomeAfterThreeSeconds", "TestLeavingThe404PageCancelsTheRedirect"},
	},

	// ── 双平台 ─────────────────────────────────────────────────────────────
	{
		Category: Platform,
		Spec:     "macOS 红黄绿在不在（无边框保留控件，不是真无边框）",
		Tests:    []string{"TestTheWindowKeepsTheSystemControls", "TestTheWindowOpensAtBiuNumbers"},
		RealOnly: true,
	},
	{
		Category: Platform,
		Spec:     "中文输入法：窗口接收合成文本、合成串画在框里、上屏能提交",
		Tests: []string{
			"TestTheSearchBoxTellsTheSystemItTakesComposedText",
			"TestTypingInChineseComposesAndCommits",
			"TestSearchingInChineseGoesThroughTheWholeWay",
		},
		RealOnly: true,
	},
	{
		Category: Platform,
		Spec:     "系统音频真的出声（起播有 300~700ms 延迟，断言轮询不按墙钟）",
		Tests:    []string{"TestSystemAudioOutputOpens"},
		RealOnly: true,
	},
	{
		Category: Platform,
		Spec:     "窗口关掉时在途的封面下载收得干净（不卡、不漏、不崩）",
		Tests:    []string{"TestCloseWhileACoverIsStillLoading", "TestClosingTheWindowWhileTheCoverIsLoading"},
	},
	{
		Category: Platform,
		Spec:     "异步结果**逐窗口**唤醒（只叫醒主窗口的话迷你窗口停在旧内容）",
		Tests:    []string{"TestEveryWindowThatDrewItGetsWoken", "TestEveryWindowGetsWoken"},
	},
	{
		Category: Platform,
		Spec:     "Windows 媒体会话与任务栏缩略图（**要 Windows 真机跑**）",
		Tests: []string{
			"TestThumbbarIsPrevPlayPauseNext",
			"TestSmtcCommandMapping",
			"TestBindWindowGoesToThePlatformThatWantsOne",
		},
		RealOnly: true,
	},

	// ── 负路径 ─────────────────────────────────────────────────────────────
	{
		Category: Negative,
		Spec:     "快速切歌 5 首：只有最后一首，旧的不留句柄和 goroutine",
		Tests: []string{
			"TestOnlyTheLastTrackWinsWhenSwitchingFast",
			"TestRapidSwitchingLeavesNoGoroutinesNorHandles",
		},
	},
	{
		Category: Negative,
		Spec:     "翻页中切查询：在途的那一页作废",
		Tests:    []string{"TestReloadCancelsThePageInFlight", "TestAStaleResultNeverLands"},
	},
	{
		Category: Negative,
		Spec:     "下载中退出：请求被取消、缓存文件删干净、泵和看门狗都退",
		Tests:    []string{"TestQuittingWhileTheStreamIsStillDownloading", "TestClosingTheAppReleasesEverything"},
	},
	{
		Category: Negative,
		Spec:     "封面加载中关窗口：不卡、不叫醒死窗口、之后再画一帧不崩",
		Tests:    []string{"TestCloseWhileACoverIsStillLoading", "TestClosingTheWindowWhileTheCoverIsLoading"},
	},
	{
		Category: Negative,
		Spec:     "播放中断网/恢复：换流之后从原来的位置继续播",
		Tests:    []string{"TestStreamCutMidwayRefetchesAndKeepsPlaying", "TestRefetchContinuesFromTheSameByte"},
	},
	{
		Category: Negative,
		Spec:     "扫码中途断网：不算失败，网络回来自己走完",
		Tests:    []string{"TestScanningSurvivesTheNetworkGoingAway", "TestLogoutSurvivesAnUnreachableBilibili"},
	},
	{
		Category: Negative,
		Spec:     "容器损坏：坏文件要么开口就拒、要么说得出为什么停了",
		Tests:    []string{"TestACorruptFileNeverLooksLikeItFinished", "TestAFileThatCannotBeOpenedSaysWhy"},
	},
	{
		Category: Negative,
		Spec:     "重启：登录状态保持、设置落盘、搜索历史活下来",
		Tests: []string{
			"TestRestartKeepsTheLoginState",
			"TestMountedSettingsSurviveAProcessRestart",
			"TestSearchHistorySurvivesRestart",
		},
	},
	{
		Category: Negative,
		Spec:     "扫码中途取消/退登：在途的结果写不回来",
		Tests: []string{
			"TestCancelLoginStopsPolling",
			"TestLogoutWhileScanningDoesNotWriteTheResultBack",
			"TestLogoutBeforeTheQRCodeComesBackAlsoDoesNotWrite",
		},
	},
	{
		Category: Negative,
		Spec:     "铁律二：主题每帧在 view 最开头装；放在回调里只调一次会被静默丢掉",
		Tests:    []string{"TestSetThemeOnceInACallbackIsSilentlyReset", "TestApplyThemeEveryFrameSurvives"},
	},
	{
		Category: Negative,
		Spec:     "铁律四：动态列表的行键稳定且唯一",
		Tests:    []string{"TestRowKeysMustBeUnique", "TestTheChosenSongSurvivesInsertingOneAtTheHead"},
	},
}
