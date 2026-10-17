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
// 往里加一行：需求来自 SPEC 的哪一句、要拿哪几条测试当证据。要是真机上
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
		// ⚠️ 这句话里「真的出声」四个字是**人耳**才能确认的，而它量不到。
		// `TestSystemAudioOutputOpens` 量到的是两件事：播放器把系统的输出打开了
		// （没打开的话位置照样走）、真的有采样流到输出去了（位置往前走就是
		// 渲染回调在跑）。**这两件都不等于「听得见」**——
		// 输出接在 null device 上、或者音量被静音成 0，它照样是绿的。
		//
		// 而且它**可能整条跳过**：那台机器没有可用的输出时它 `t.Skip`。
		// 跳过不是通过，这一点在台账里必须写出来，不然读的人会当成绿。
		//
		// 「耳朵」那一份在人工清单里（戴耳机听一遍），台账里不装。
		Spec:     "系统音频**输出被打开、真的有采样流出去**（不等于听得到；没有输出时整条跳过，跳过≠通过。耳朵那一份在人工清单里）",
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
		Category: Platform,
		// SPEC 107。⚠️ 这一条量到的是**我们这一层**：查、说、下载进度、装、退出并
		// 进去、以及落盘那份会不会说谎。
		//
		// **没有**量到的是：ed25519 签名校验、增量补丁、原地替换整个 `.app`
		// bundle——那三件事全在框架里（`mygo/updater.go` 的 `update.Verify` /
		// `ApplyDelta` / `swapBundle`），我们既没重写也不该重写（重写一遍的结果
		// 是两条更新链，而只有一条被测过）。而且它们**一次都还没在真机上跑过**：
		// 要跑需要一把真私钥 + 一个发布源 + 第二台机器。
		//
		// 还有一件更要紧的：今天**任何一个构建都还更新不了**——更新源的地址和
		// 公钥是 `mygo build` 用 `-ldflags` 塞进去的，而 `mygo.json` 里还没有
		// `updates` 那一段（公钥要 `mygo keygen`，是打包发布那一票的活儿）。
		// 所以 `Enabled()` 在开发构建和没打包的构建里都是 false，界面上那一格
		// 老老实实写着「这个版本不能自动更新」。
		Spec: "新版本能被查到、说给用户、能下能装能退出去（SPEC 107。签名校验、增量、原地换整个 bundle 在框架里，没真机端到端；今天任何构建都还更新不了——`mygo.json` 里还没有 `updates` 段，公钥是打包发布那一票的事）",
		Tests: []string{
			"TestACheckFindsTheNewVersionAndKeepsIt",
			"TestAnAppThatIsUpToDateSaysSo",
			"TestABackgroundCheckLeavesANoticeInsteadOfOpeningAnything",
			"TestABackgroundCheckThatFailsKeepsQuiet",
			"TestACheckTheUserAskedForSaysWhatWentWrong",
			"TestInstallingAVersionThatJustGotSupersededGoesBackAndChecksAgain",
			"TestNoCheckSurvivesTheQuit",
			"TestABuildThatCannotUpdateSaysWhy",
			"TestWithoutTheUpdaterEveryEntryPointIsQuiet",
			"TestTheSettingsPageHasAnAboutSectionWithTheVersionAndAButton",
			"TestCheckingFindsANewVersionAndTheRowSaysSo",
			"TestABackgroundCheckShowsAToastInsteadOfOpeningTheDialog",
			"TestTheAboutSectionSaysWhyThisBuildCannotUpdate",
		},
	},
	{
		Category: Platform,
		// SPEC 108。「更新前看到更新日志」这一条，量的是**更新前那一份画面**：
		// 日志渲染成富文本、下载时四个数同时在、下完了给「退出并安装更新」。
		Spec: "更新日志渲染成富文本，空的那一档说「暂无更新日志」；下载时速度/已传/总量/百分比两位小数四个数同时在画面上；下完了给「退出并安装更新」（SPEC 108）",
		Tests: []string{
			"TestTheDialogRendersTheReleaseNotes",
			"TestTheDialogSaysSoWhenThereAreNoNotes",
			"TestTheDialogShowsSpeedTransferredTotalAndPercentWithTwoDecimals",
			"TestThePercentAlwaysHasExactlyTwoDecimals",
			"TestTheProgressCarriesBytesSpeedAndPercent",
			"TestTheSpeedNeedsTwoSamplesBeforeItSaysAnything",
			"TestThePercentIsClampedToTheBox",
			"TestTheReadyDialogOffersToQuitAndInstall",
			"TestTheDialogCannotGoAwayWhileTheUpdateIsDownloading",
			"TestTheDialogCanBeDismissedWhenNothingIsDownloading",
			"TestTheDialogCannotBeDismissedWhileDownloading",
			"TestDismissingAnAvailableUpdatePutsItBack",
			"TestAFailedInstallPutsTheReasonInTheDialogAndOffersARetry",
			"TestAFailedCheckPutsTheReasonUnderTheButtonAndKeepsItPressable",
			"TestAFailedInstallSaysWhy",
		},
	},
	{
		Category: Negative,
		// 票面点名的已知问题：dmg 装来的应用**第一次**增量最容易失败（磁盘镜像
		// 里那份没有公证票据，补丁对不上磁盘上的现状）。框架自己会回退整个
		// 安装包，而**回退有一处用户能看见的表现**——进度条倒退、百分比变小、
		// 速度归零再跳起来。Biu 那套（electron-updater）没有这个现象，所以上游
		// 没有这一支代码。
		//
		// 这一组量的是两半：**认得出**（`total` 变了 / `downloaded` 倒回去了），
		// 以及**不认错**（一次正常下载的第一个采样点也有 `total`，把它算成回退
		// 会让每一次下载都说一句错话）。
		Spec: "增量补丁没拼出来时回退整个安装包，并且**把这件事说给用户听**（票面点名的已知问题：dmg 装来的应用第一次增量最容易走到）",
		Tests: []string{
			"TestADeltaThatFailsFallsBackToTheWholeArchiveAndSaysSo",
			"TestTheFirstSampleOfADownloadIsNotAFallback",
			"TestARestartWithTheSameTotalIsStillAFallback",
			"TestTheDialogSaysSoWhenTheDeltaIsNotUsable",
			"TestTheDialogDoesNotBlameTheDeltaOnANormalDownload",
		},
	},
	{
		Category: Negative,
		// SPEC 118。要点在「**落盘那份里没有任何『正在下』的字段**」：有了它，
		// 中途退出之后读回来的东西必然是自洽的（「上次查到了 0.2.0，但还没装上」）。
		// 反过来写，就得额外去定义「那一份半截下载算不算数」。
		Spec: "更新中途退出应用：下载被取消、磁盘上那份应用一个字节没动、落盘那份里没有任何「正在下」的字段（SPEC 118）",
		Tests: []string{
			"TestQuittingWhileDownloadingLeavesNothingBroken",
			"TestQuittingWhileCheckingIsQuiet",
			"TestThePrefsFileRoundTrips",
			"TestACorruptPrefsFileIsIgnoredNotFatal",
			"TestAPrefsFileFromANewerVersionIsLeftAlone",
			"TestNoUserDataDirMeansMemoryOnly",
		},
	},
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
	{
		Category: HTTP,
		// 首页推荐那一摊。**参数和签名逐个对着上游抄**：少一个的症状是
		// 「B 站给了别的分区的推荐」或者「被风控挡成 -403」，而那两样在
		// 界面上分别长成「切了 Tab 还是那一批」和「没有权限看推荐」。
		//
		// 「流行」那一档走的是**音乐榜**而不是第三个分区，这一条钉的是它。
		Spec: "首页三个 Tab 各自的接口、参数和签名：音乐/鬼畜是分区推荐（1003/1007，带 WBI 签名），流行是音乐榜（不带），新歌速递走那两个带 csrf 的接口",
		Tests: []string{
			"TestRegionFeedSendsEveryParamAndSignsIt",
			"TestTheGuichuRegionIsItsOwnNumber",
			"TestMusicRankSendsItsOwnParams",
			"TestMusicRankPrefersTheRelatedArchive",
			"TestNewMusicSendsItsParamsAndCSRF",
			"TestEachTabAsksForItsOwnThing",
			"TestTheRecommendFailuresSaySomethingUseful",
		},
	},
	{
		Category: Negative,
		// 「没有内容」和「拉不到」是两件事，界面上必须是两句话。合成一句的话，
		// 用户读到「暂无内容」会以为 B 站没有推荐给他，于是永远不会去按那个
		// 本该在那儿的重试。
		//
		// 「失败之后不许每帧重发」这一条是防那个自激循环：实测 300 毫秒能发
		// 138 次，界面停在「打不开」上还在刷网络。
		Spec: "首页拉不到推荐：说的是「拉不到」并给一次重试、失败后不自己绕圈；空列表说的是「没有内容」；新歌速递那一块挂掉不牵连下面那一档",
		Tests: []string{
			"TestAFailedRecommendSaysSoAndOffersARetry",
			"TestAnEmptyRecommendListIsNotAFailure",
			"TestTheNewMusicBlockFailsOnItsOwn",
			"TestTheHomePageSaysItIsLoadingUntilTheFirstPageIsBack",
			"TestNewMusicIsOnlyPulledOnThePopTab",
			"TestLoggingInReloadsTheRecommendations",
		},
	},
	{
		Category: Paging,
		// 「页面壳子做完了、数据源没排票，于是交付时一片绿、用户打开是空的」
		// （BMG-47 §4 查出的模式，七个页面命中）。首页那张票（#13）就是：票面
		// 四条全是交互和布局，没有一条要求有数据。
		//
		// ⚠️ 「有数据」和「空状态文案对不对」是**两件事**，而当年只验了后一件
		//（`TestTheHomePageStillSaysWhyItIsEmpty` 验的正是文案，而空状态正是
		// 当时的真实状态，所以它必然通过）。所以每一页除了真数据要出现在画面上，
		// 还钉一句「这一页的空状态文案不许同时出现」。
		//
		// ⚠️ 「画得出来」和「真去 B 站取回来了」是**两件事**。首页那一行在表上
		// 走的是一台假 B 站（`newUIExplore` + `homeArchives`）而不是往模型里塞歌，
		// 而且三档各走一遍——因为 A1 的原教训正是「画得出来」那一半被验了、
		// 「取数」那一半没人管。这一页钉取数的两条在下面。
		//
		// 今天这一条**不是全过**：侧栏第二项「我的关注」**连壳子都没有**（现有的
		// followings / stat / modify 三个 relation 接口是动态流和 UP 空间在用），
		// 而且**它连工单都还没有**（BMG-47 §3 A2）。它在
		// 表上是一条**欠条**：必须点名到编号、写清差什么，并且**当场证明它现在
		// 真的是空的**——不然欠条会变成万能借口：数据早就接上了它还赖着，谁也
		// 看不出来。排到票之后把欠条换成真数据即可。
		Spec: "每一页至少要有一条真数据（假 B 站喂了内容之后，画面上出现的是内容而不是那一页的空状态；还欠着的页面必须点名到编号、写清差什么，并且当场证明它现在真的是空的——「我的关注」是今天唯一欠着的一条，它连工单都还没有）",
		Tests: []string{
			"TestEveryRouteShowsARealRowOrSaysWhichTicketIsMissing",
			"TestTheRealDataLedgerCoversEveryRoute",
			"TestEveryRouteHasAPageViewOrAnOpenDebt",
			// 各页从空状态落到真数据的那一步。
			"TestTheThreeTabsAllSwitch",
			"TestCollectionShowsTheHeaderAndTheOpsBar",
			// 首页那一页的**取数**那一半（表上那一行走的就是这条链）：
			// 三档各打各的接口，换了档拿到的是另一批。
			"TestTheHomePagePullsTheTabYouAreLookingAt",
			"TestEachTabPullsItsOwnList",
		},
	},
	{
		Category: Play,
		// 「判据说可放、实际放不了」——界面上的谎言。`bili.Media.Playable()`
		// 照抄了 Biu 的 `![2,12].includes(type)`，**判据抄对了**，而我们没实现
		// 的那一条路（type=12，B 站音乐库）没有被跟上：收藏夹里那条照常画成一
		// 行、照常进队列、抽屉里还标着「B 站音乐」，用户按播放才被
		// `app.playableOnline` 拒绝。判据一路绿，判据的测试也一路绿，只有真的
		// 按下去那一刻才说话。
		//
		// 钉的就是那一刻：**判据说「能放」的行，照界面那条路翻成队列项再按播
		// 放，同步那一步不许拒绝它**；判据说「放不了」的行不许进队列；判据不
		// 许自己长（改判据就要改那张表），而每一类内容都必须在表上。
		//
		// ⚠️ type=12 今天**欠着**（BMG-47 §3 A31，还没有工单：取流 / 元信息 /
		// 收藏态三个接口一个都没有）。它写成一条**欠条**而不是一段红的测试，
		// 理由和上一条一样：一个永远红的 `go test ./...` 没有人再跑它，那比绿
		// 着更坏。把欠条删掉，这条断言自己就开始要求它真放得了。
		Spec: "画出来的每一行都放得了：判据与界面那一步必须说同一句话（type=12「B 站音乐库」今天欠着 A31，欠条要点名到编号并钉住那句人话；判据不许自己长，每一类内容都必须在表上）",
		Tests: []string{
			"TestEveryRowWeDrawHasAVerdictThatMatchesWhatHappens",
			"TestEveryContentTypeInTheCodebaseIsOnTheVerdictLedger",
			"TestEveryListedExtensionIsToldApartFromThePlayableOnes",
			// 服务层那三条：判据本身，以及「放不了的那一种在界面上必须说清楚」。
			"TestOnlyVideoAndAudioArePlayable",
			"TestAnUnplayableFormatIsNamedAsSuch",
			"TestWMAIsListedButNotPlayable",
		},
	},
}
