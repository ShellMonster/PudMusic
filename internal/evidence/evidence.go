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
		// 「哪一首正在放」和「下一首放哪首」这两个问题的答法，都不能是**位置**。
		//
		// 队列会重排：「下一首播放」插在当前后面、拖拽排序、随机模式，每一个都
		// 在动当前项的下标；而列表里的行号是另一套。用位置答出来的症状是
		// 「高亮跳到旁边那一首」「按了下一首播放，正在放的歌换了一首」——
		// **界面上看不出任何异样**，声音和画面各说各话。
		//
		// 判据是 `app.Item.Identity`（本地按路径、mv 按 bvid、音频按 sid），
		// 也就是 Biu 那个抽屉去重时用的同一把尺子。顺带钉住「认的是**同一首**
		// 而不是同一份」：同一首歌排了两遍时（内容列表不判重）两行都该亮，
		// 而认整项的话两行都不亮——用户在**正在放**的那份上看到「哪首都没在放」。
		Spec: "「正在播放」按歌认（队列重排了也还对，同一首排两遍算一首）；「下一首播放」插在当前项后面而不是队尾",
		Tests: []string{
			"TestIsPlayingFindsTheSongWhereverTheQueuePutIt",
			"TestIsPlayingSurvivesTheQueueBeingRearranged",
			"TestIsPlayingIsAboutTheSongNotAboutThatCopy",
			"TestTheSameSongInTwoRowsIsOneHighlight",
			"TestAppActionsPlayNextLandsRightAfterTheCurrentOne",
			"TestInsertNextLandsRightAfterTheCurrentOne",
		},
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
		Tests: []string{
			"TestTheErrorDoesNotStickToTheNextSong",
			// 同一句话的另一半：换音质失败留下的红字，在随后一次「拿回同一档、
			// 不该打断」的换档里同样会变成一句错的、且不会自己清掉的话。
			"TestAFailedQualitySwitchDoesNotLeaveItsWordsBehind",
		},
	},
	{
		// 频谱挂在解码器输出端，而 MyGo 的解码器**领先放音约一秒**（PLAN 阶段 6
		// 的风险表点名了这一条）。所以「每一段标着源帧号、按播放位置取」不是优化，
		// 是「鼓点晚整整一拍」和「拖完进度条频谱还停在老地方」的病根。
		Category: Play,
		Spec:     "频谱按播放位置取到对应的那一段（解码器领先解约一秒）",
		Tests: []string{
			"TestEachBlockIsTaggedWithItsSourceFrames",
			"TestSeekClearsTheHistory",
			"TestTheRingKeepsAboutFourSeconds",
			"TestTheTapIsTransparent",
		},
	},
	{
		Category: Play,
		Spec:     "系统媒体状态映射（暂停/停止/播完/解码失败不是同一个数）",
		Tests:    []string{"TestPlaybackStateMapping", "TestSmtcStatusIsNotTheMacNumber"},
	},
	{
		Category: Play,
		// 播放模式四档（顺序/循环/随机/单曲，BMG-47 A5）。「放完下一首是哪首」
		// 这一句是**四档各有一条**——顺序停在队尾、循环绕回队尾、随机真的打乱、
		// 单曲一步都不挪，缺一档就有一档没人管，而缺了的那一档在界面上和别的
		// 档**长得一模一样**（都是一个会往下走的按钮）。
		//
		// 队列那一层（纯数据，不碰播放器，所以能逐个钉死）和 App 那一层（真的
		// 放完一首歌看接的是谁）分开量：只写队列那一层，接线漏了也是绿的——
		// 「状态机写得对但没接上播放器」正是这一票原来的样子。
		Spec: "播放模式四档：一首歌放完之后下一首是哪首（顺序到队尾停 / 循环绕回 / 随机真打乱 / 单曲不挪），随机时保持分集顺序，换档立刻生效、模式落盘",
		Tests: []string{
			// 四档各一条「放完下一首是谁」。
			"TestSequenceWalksTheListAndStopsAtTheEnd",
			"TestLoopWrapsFromTheLastBackToTheFirst",
			"TestRandomReallyShuffles",
			"TestSingleReplaysTheSameSongAndNeverMoves",
			// 「真的打乱」的两条边界：只有一首歌时重放而不是抽自己。
			"TestLoopWithOneSongReplaysIt",
			"TestRandomWithOneSongReplaysIt",
			// 随机时保持分集顺序（开着/关着/已经是最后一集）。
			"TestRandomKeepsThePageOrderWhenAskedTo",
			// 四档的档序、文案、以及认不出的档位不许变成空按钮。
			"TestTheFourModesAreInBiuOrder",
			"TestAnUnknownModeFallsBackInsteadOfSayingNothing",
			// 换档立刻生效，不用重开歌。
			"TestSwitchingTheModeTakesEffectAtOnce",
			"TestChangingTheModeTakesEffectWithoutRestartingTheSong",
			// 接线：真的放完一首歌会自己往下走；按了暂停不会。
			"TestASongThatFinishesMovesOnByItself",
			"TestAPausedSongDoesNotMoveOn",
			"TestSequenceStopsAfterTheLastSong",
			"TestSingleReplaysWithoutMovingOn",
			// 「读到头了」和「读坏了」分得清（判据在音频层）。
			"TestEndedAndErrAreComplementary",
			"TestSeekClearsTheEndedLatch",
			// 落盘 + 默认档。
			"TestThePlayModeSurvivesARestart",
			"TestAFreshInstallGetsBiuDefaultMode",
			// 界面：按钮按一下换一格、右键四档齐、保持分集顺序的开关、没挤坏歌名。
			"TestThePlayModeButtonSaysWhichModeItIs",
			"TestClickingThePlayModeButtonWalksTheFourModes",
			"TestThePlayModeMenuListsEveryMode",
			"TestChoosingAModeFromTheMenuTakesIt",
			"TestTheKeepPagesOrderSwitchOnlyShowsUpWhileShuffling",
			"TestThePlayModeButtonDoesNotSquashTheTitle",
			// 拖到末尾的两种结局：队列空的停下来、循环里单曲从头再放。
			"TestDragToEndDoesNotCrash",
			"TestDragAACToEnd",
		},
	},

	// ⚠️ 「界面上标成可放的条目真的放得出来」那一格在下面「画出来的每一行都放得了」
	// 那一条里（`Play` 组，靠后）——它连着那张表一起钉，而那才是它该在的地方。
	// 别在这里再抄一份：台账里有两条说同一句话的，读的人得自己猜哪条才是活的。

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
	{
		// SPEC「回归证据」里点名的三处假时钟之一（另外两处是全屏的 3000ms 隐藏和
		// 歌词的 500ms 防抖）。三处都是「等一会儿」，而真等就是一条测试几秒钟。
		Category: FakeClock,
		Spec:     "全屏播放器：指针不动 3000ms 界面自己藏起来，动一下回来",
		Tests: []string{
			"TestTheInterfaceHidesAfterThreeSeconds",
			"TestTheHiddenInterfaceIsReallyGoneFromTheScreen",
			"TestMovingThePointerBringsTheInterfaceBack",
		},
	},
	{
		// 第二处：点赞的 2500ms 长按。
		//
		// ⚠️ **两个数都要钉**：2500（触发三连）和 500（开始画进度环）不是同一个，
		// 而环画 2000ms 转满——500+2000 正好是 2500，所以「环转满」和「请求发出去」
		// 是同一刻。只钉 2500 的话，把环的起点挪到 100ms 那条照样全绿，而用户看到
		// 的是「按下去立刻开始转圈」。
		Category: FakeClock,
		Spec:     "点赞：按住 2500ms 一键三连，500ms 起进度环；提前松手什么也不发生",
		Tests: []string{
			"TestTheRingIsFullJustAsTheTripleGoesOut",
			"TestTheRingStartsAfterHalfASecond",
			"TestLettingGoEarlyDoesNothing",
		},
	},
	{
		// 那一票的另一半是**手势**而不是时间：长按之后松手补发的那一次 click 必须
		// 吞掉，否则用户什么都没点全屏却开了（Biu 那边漏了它是一次多余的赞）。
		Category: FakeClock,
		Spec:     "长按三连之后补发的那一次 click 被吞掉，而下一次普通点击照旧生效",
		Tests: []string{
			"TestALongPressNeverOpensTheFullScreen",
			"TestAShortPressStillOpensTheFullScreen",
			"TestTheLongPressIsOffOnceTheSongIsLiked",
		},
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
	{
		Category: HTTP,
		// 音乐库（type=12 / sid）是**另一套接口、另一套档位编号**，连错误说明
		// 那个字段都叫 `msg` 而不是 `message`。这一条钉的就是「形状别抄错」。
		//
		// ⚠️ 「试听片段」那一半是这里最要紧的：B 站对没权限的那一档不报错，
		// 回的是一段能下能播、只有开头几十秒的流。放它过去的话用户听到的是
		// 「歌放到一半静默停住」，界面上一个错都没有。
		Spec: "音乐库那一路的形状：元信息 / 取流 / 收藏态，试听片段不算流",
		Tests: []string{
			"TestSongInfoReadsTheFieldsThePlaybarNeeds",
			"TestMusicStreamURLTakesTheDefaultTier",
			"TestCollResourceCheckReadsTheNumber",
			"TestAMusicPreviewIsNeverHandedBackAsAStream",
			"TestAPaidSongFallsBackToAFreeTier",
		},
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
		Category: Platform,
		// 「录进去了，什么也不发生」（BMG-47 §4 A6）——设置页里能录、能存、
		// 冲突看得出来，而按下去什么也不发生。这比没有更坏：用户以为设上了。
		//
		// 钉的是「真的按下去会走那一条命令」和「焦点在输入框里时不抢按键」两半。
		// 后一半是 SPEC 114 点名的，量法是「焦点在搜索框里按那个键，字进了搜索框、
		// 那条命令没有触发」——只验前半句的话，一个把所有快捷键都关掉的实现照样通过。
		//
		// 另有几条钉的是接线本身：存下来的 accelerator 串认得回来（认不回来的症状
		// 是设置页一切正常、按下去没反应），以及**正在录的那一格**优先于窗口登记的
		// 组合键（引擎会先问「有没有人认领这个键」，而录制格靠 HandleInput 吃键）。
		//
		// ⚠️ 「让开」必须收得干净，这一条自己有个反面的坑：让开的状态一旦卡住
		// （录完不清、走开也不清），**六条命令当场全哑而界面上一个字都不提**——
		// 和 BMG-47 A6 一模一样的症状换了个原因。所以后面两条专门钉它。
		Spec: "快捷键按下去真的会触发那条命令；焦点在输入框里打字时快捷键让路（SPEC 110、112、114。BMG-47 A6 说的「录进去了什么也不发生」）",
		Tests: []string{
			"TestAPressedShortcutRunsItsCommand",
			"TestAShortcutWithNoModifierRunsWhenNothingElseHasTheFocus",
			"TestTheSearchBoxGetsTheKeyBeforeAnyShortcut",
			"TestTheRecorderCellStillGetsAKeyThatIsARegisteredShortcut",
			"TestTheShortcutsComeBackTheMomentARecordingFinishes",
			"TestWalkingAwayMidRecordingDoesNotSilenceTheShortcutsForever",
			"TestEveryRecordedKeyComesBackAsItself",
			"TestTheShortcutsHandEditedIntoTheFileStillWork",
			"TestTheSixCommandsCallTheirOwnThing",
			"TestAShortcutRunsTheSameCommandWhicheverWayItArrives",
			"TestTheShortcutPageSaysWhatTheTwoColumnsAreFor",
		},
	},
	{
		Category: Platform,
		// 全局快捷键（SPEC 111）。量的是**两段**：按设置把那一列登记到系统上
		// （用存下来的那一串，一个字不翻译）、以及系统的回调真的走到那条命令。
		//
		// ⚠️ **没有**量到的是最后一跳：系统（macOS 的 Carbon 热键）到底会不会把
		// 一个在别的应用里按下的组合键送到本进程。那一下在框架里（`mygo` 的
		// `internal/darwin/hotkey.go`），我们既没重写也不该重写——重写一遍的结果
		// 是两条热键链，而只有一条被测过。要验它得切到别的应用按一次，那一半留在
		// 人工清单里。
		//
		// 「注册不上要降级」也是这一条的一半：别的应用先占了那个键、Wayland 上没有
		// 桌面 portal、Linux 上没有 X11 会话——这些都得只让那一条失效，而不是让整个
		// 快捷键功能一起瘫掉。
		//
		// 「录制期间要从系统上撤下来」是第三半：Carbon 热键到不了焦点上的元素，
		// 不撤的话那六个默认全局键**在任何一格都录不进去**（真机上量过）。
		Spec: "全局快捷键：按设置登记到系统上、按下来走那一条命令、录制期间从系统上撤下来；**系统真的把别的应用里按下的键送过来**这一跳没有量（在框架里，真机人工清单那一半）。注册不上只让那一条失效，不拖累其余，更不拖累应用内那一列（SPEC 111）",
		Tests: []string{
			"TestTheGlobalColumnIsRegisteredOnStartup",
			"TestTurningGlobalShortcutsOffUnregistersThem",
			"TestRecordingAGlobalShortcutRegistersTheNewKey",
			"TestResettingTheShortcutsRegistersThemAgain",
			"TestAGlobalShortcutThatIsAlsoAnInAppOneIsNotRegistered",
			"TestTwoSpellingsOfTheSameKeyAreNotBothRegistered",
			"TestTheGlobalShortcutsStepAsideWhileRecording",
			"TestOneGlobalShortcutThatCannotRegisterDoesNotTakeTheRestDown",
			"TestEveryGlobalShortcutThatFailsStillLetsTheOthersRun",
			"TestQuittingUnregistersTheGlobalShortcuts",
			"TestSyncingTwiceLeavesNoDuplicate",
		},
		RealOnly: true,
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
		// ✅ 这一条**现在是全过的**：BMG-47 §3 A2 点名的最后一条欠条（「侧栏第二项
		//『我的关注』连壳子都没有，而且连工单都还没有」）已经由 BMG-49 还清——
		// `internal/ui/follow.go`（分组 Tab + 虚拟网格 + 页内搜索）加上它自己
		// 的四个接口（`internal/service/bili/follow.go`）。它走的是**真接口的
		// 假 B 站**，不是往模型里塞数据。
		//
		// ⚠️ 欠条撤掉之后，这张表自己的规矩（「已经能打开却有欠条：数据接上了就
		// 把欠条撤掉」）是唯一还醒着的那道：`TestEveryRouteShowsARealRowOr…`
		// 的「欠着」那一支要求 `Open == nil`，而它**只在 `Debt != ""` 时才检查**
		// 那一支；`TestEveryRouteHasAPageViewOrAnOpenDebt` 又在
		// `builtPage != nil` 时直接 `continue`。两条同时不响的时候，一条过期欠条
		// 还能一直挂着——而它挂着的时候「当场证明自己还欠着」也照样通过，因为
		// 那个空壳画的是「还没接上 B 站接口」那一支，**顺带**含有 `NowEmpty`
		// 那一句。所以这一格下面那行 `Want` 钉的是真数据，不是空状态。
		Spec: "每一页至少要有一条真数据（假 B 站喂了内容之后，画面上出现的是内容而不是那一页的空状态；还欠着的页面必须点名到编号、写清差什么，并且当场证明它现在真的是空的）",
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
		// 「往 B 站写播放记录」这一票（BMG-47 审计 A7）。它修的是一个
		// **壳子做完了、数据源没有**的洞：历史记录页读的是 B 站服务器上的
		// 历史，而我们以前一次都没往里写——只用我们这个应用的用户，那一页
		// 永远是空的。
		//
		// 逐条钉的是**每一个上报时机**（开始播 / 心跳 / 暂停 / 继续 /
		// 切歌 / 停止 / 播完），以及三件「错了就伤到用户真实账号」的事：
		// 退登之后不许再写、换人登录之后上一首的进度不许记到那个人名下、
		// 解码失败不许报成「看完」。
		//
		// ⚠️ **这一票的验收没走真网**：上报是写操作，它写进用户真实的 B 站
		// 账号，而反复上报会在那个账号里留下一堆垃圾播放记录。所以全部用
		// HTTP fixture。真接口的兼容性（参数集对不对、B 站收不收）要真机
		// 回归，那是单独安排的一趟，见任务报告「需要真网验」那一节。
		//
		// 这里量到的是**我们这一层**：什么时机发、发什么、代次守不守得住、
		// 失败会不会影响播放。真 B 站收下之后「历史记录」那一页上出现的是
		// 不是这一条，量不到。
		Spec: "播放记录上报 B 站：开始播/30 秒心跳/暂停/继续/切歌/停止/播完各自发的那一下，参数逐个对着上游；退登与换人之后不许再写（进度不许落到另一个账号名下）；解码失败不许报成播完；上报失败不许影响播放（这一票走 HTTP fixture，真接口要真机回归——上报会写进用户真实的 B 站账号）",
		Tests: []string{
			// 协议层：发出去的形状。
			"TestPlayReportOpenSendsEveryParam",
			"TestPlayReportBeatSendsEveryParam",
			"TestTheFnvalIsTheOneWeAskStreamsFor",
			"TestFinishedSendsMinusOne",
			"TestEveryPlayTypeHasItsOwnNumber",
			"TestReportingNeedsAVideo",
			// 业务层：什么时候发。
			"TestPlayingOpensTheRoundAndSaysItStarted",
			"TestAHeartbeatKeepsGoingWhileItPlays",
			"TestPausingAndResumingAreToldApart",
			"TestSwitchingEndsTheOldRoundAndOpensANewOne",
			"TestTheRoundThatEndsCarriesItsOwnDuration",
			"TestStoppingEndsTheRoundAndDoesNotOpenItAgain",
			"TestFinishingSaysItFinished",
			// 什么不许发。
			"TestAFailedStreamIsNeverReportedAsFinished",
			"TestLocalFilesAreNeverReported",
			"TestTheSwitchOffMeansNothingIsSent",
			// 代次守卫与退登。
			"TestASessionInFlightDiesWithTheRoundThatStartedIt",
			"TestLoggingOutStopsTheReporting",
			"TestTheProgressNeverLandsOnTheNextAccount",
			// 失败不许影响播放。
			"TestAFailedReportDoesNotReachTheUser",
			"TestAFailedOpenDoesNotSilenceThatVideoForever",
			"TestAViewThatFailsIsNotAskedAgainAndAgain",
			// 退出与换客户端：上报那条 goroutine 得收掉。
			"TestClosingTheAppStopsTheReportingGoroutine",
			"TestMountingStreamingAgainRetiresTheOldReporter",
			// 从真的播放链路走到 B 站（真队列 + 真取流 + 真解码 + 真进度条时钟）。
			"TestPlayingARealStreamReachesBilibili",
			"TestStoppingARealStreamTellsBilibiliItIsOver",
			"TestLoggingOutARealStreamStopsTheReporting",
		},
	},
	{
		Category: Play,
		// 「判据说可放、实际放不了」——界面上的谎言。`bili.Media.Playable()`
		// 照抄了 Biu 的 `![2,12].includes(type)`，**判据抄对了**，而那时我们没实
		// 现的那一条路（type=12，B 站音乐库）没有被跟上：收藏夹里那条照常画成一
		// 行、照常进队列、抽屉里还标着「B 站音乐」，用户按播放才被
		// `app.playableOnline` 拒绝。判据一路绿，判据的测试也一路绿，只有真的
		// 按下去那一刻才说话。
		//
		// 钉的就是那一刻：**判据说「能放」的行，照界面那条路翻成队列项再按播
		// 放，同步那一步不许拒绝它**；判据说「放不了」的行不许进队列；判据不
		// 许自己长（改判据就要改那张表），而每一类内容都必须在表上。
		//
		// ⚠️ type=12 那一行**欠过**（BMG-47 §3 A31 → BMG-50），当时写成一条
		// **欠条**而不是一段红的测试，理由和上一条一样：一个永远红的
		// `go test ./...` 没有人再跑它，那比绿着更坏。把欠条删掉之后，这条断言
		// 自己就开始要求它真放得了——而**下面那两条就是它转正之后的样子**：一条
		// 钉判据与播放器认不认（`internal/ui`），一条钉真的取得到流（`internal/app`）。
		// 只钉判据不够，接线断了照样绿，那正是当初漏掉的那一半。
		Spec: "画出来的每一行都放得了：判据与界面那一步必须说同一句话（type=12「B 站音乐库」欠过 A31，BMG-50 已交付，欠条撤掉，断言转为要求它真放得了；判据不许自己长，每一类内容都必须在表上）",
		Tests: []string{
			"TestEveryRowWeDrawHasAVerdictThatMatchesWhatHappens",
			"TestEveryContentTypeInTheCodebaseIsOnTheVerdictLedger",
			"TestEveryListedExtensionIsToldApartFromThePlayableOnes",
			"TestEveryPlayableCollectionEntryIsAcceptedByThePlayer",
			"TestEveryPlayableEntryCanActuallyBeStreamed",
			// 服务层那三条：判据本身，以及「放不了的那一种在界面上必须说清楚」。
			"TestOnlyVideoAndAudioArePlayable",
			"TestAnUnplayableFormatIsNamedAsSuch",
			"TestWMAIsListedButNotPlayable",
		},
	},
	{
		Category: HTTP,
		// 歌词搜索那两个源（Biu 的 `lyrics-search-modal/`）。**参数逐个对着上游抄**：
		// 少一个的症状在界面上像「这个源没有翻译」或「这个源搜不出来」，而那两样都
		// 不是参数写错造成的。
		//
		// ⚠️ 凭据边界钉的是**端到端**那半：`DefaultInjectPolicy` 把两个源列在
		// referer 档那张表本身在 `credentials_test.go` 里；这里验的是新建的那两个客户端
		// 真的走了那一层（忘了递 transport，白名单就完全不起作用，而症状是「歌词源那边
		// 收到了不属于它的 cookie」，没有报错）。
		Spec: "歌词两个源各自的接口与参数：网易云搜（s/type=1/limit=20/offset=0）与取词（那五个 -1 加 _nmclfl）、LRCLib 搜（q）；LRCLib 的预览一个请求都不发；两个源都不带 B 站凭据",
		Tests: []string{
			"TestBothSourcesCanSearch",
			"TestNeteasePreviewSendsThoseFiveParams",
			"TestNeteaseFallsBackToTheKaraokeLyric",
			"TestLRCLibPreviewIssuesNoRequest",
			"TestAnEmptyWordSendsNothing",
			"TestTheLyricSourcesNeverSeeBiliCredentials",
		},
	},
	{
		Category: Negative,
		// 「避免重复请求」那半句的结果缓存。判据是「第二次搜的时候那个源一次都没被叫
		// 到」——只判「界面上出现了一样的候选」抓不住它，缓存和源返回同一样东西时两者
		// 长得完全一样。
		Spec: "歌词搜索结果写进缓存：同一个词不搜第二遍、两个源各一份、换词换源不撞键、失败不缓存（不然按重试弹的还是上一次那句拉不到）",
		Tests: []string{
			"TestTheSameWordIsNotSearchedTwice",
			"TestTheCacheIsKeyedBySourceAndByWord",
			"TestAFailedSearchIsNotRemembered",
			"TestEmptyingTheBoxClearsTheList",
		},
	},
	{
		Category: Negative,
		// 搜/预览都在后台，而用户在那几百毫秒里会切源、关弹窗、换歌。少了代次守卫的症状
		// 是「我搜的是乙，弹出来的是甲的歌」，而按多少次搜索都一样。
		//
		// ⚠️ 「换歌之后不许采用」单独钉：照着当前这一首写下去的话，画面上换到的是**歌名
		// A 配词 B**，而界面上从歌词到播放条每一个字都对得上。
		Spec: "歌词搜索与预览的迟到结果：搜索/预览各有代次守卫；换歌之后不许采用；采用落在正在放的那一首歌的缓存上，且不带走用户调过的偏移和字号",
		Tests: []string{
			"TestAStaleSearchNeverLands",
			"TestAdoptingRefusesAfterTheSongChanged",
			"TestAdoptingLandsOnTheSongThatIsPlaying",
			"TestAcandidateThatCarriesItsLyricsIsPreviewedWithoutAnyRequest",
			"TestAdoptingIsRefusedWhileThePreviewIsStillLoading",
			"TestNoLyricsSourcesMeansNoSearch",
		},
	},
	{
		Category: Negative,
		// 「这一条没有歌词」和「拉不到」必须分开说：混成一句的话，用户读到「网络不通」
		// 会去反复重试，而这一条重试一万次也不会有歌词——那一版在源上就是没有。
		// 而「源返回了成功而正文是空的」是**第三种**，不能摆一个空弹窗搪塞。
		//
		// 这一条的下半段（预览弹窗里那第三种）跟着**界面**走，在下面那条里。
		Spec: "歌词来源分得清三种：拉不到 / 这一条没有歌词 / 成功但正文是空的（判据是哨兵错误 errors.Is(err, ErrNoLyrics)，不是「err != nil」）",
		Tests: []string{
			"TestAFailedSearchIsAnErrorAndNotAnEmptyList",
			"TestAPreviewThatHasNoLyricsSaysSo",
		},
	},
	{
		Category: FakeClock,
		// SPEC 点名的「搜索结果 500 毫秒防抖写缓存，避免重复请求」里的**前半句**：
		// 那 500ms 在界面上（`ui/lyricssearch.go` 的 `lyricsQueryChanged`），后半句
		// 的缓存是 `app.SearchLyrics` 那一半。
		//
		// ⚠️ **回车不等那 500ms**是单列的一条（上游 `onKeyDown Enter` 直接调
		// `handleSubmit`）：用户按下回车就是想现在搜，让他等半秒是另一回事。
		Spec: "歌词搜索的关键词 500ms 防抖：假时钟推、不真等；每敲一个字重新起算；回车不等",
		Tests: []string{
			"TestTheKeywordIsDebouncedBy500ms",
			"TestEnterDoesNotWaitForTheDebounce",
		},
	},
	{
		Category: Negative,
		// 「不许静默」和「不许每帧重发」是同一支的两半。
		//
		// 🔴 后者是自己养自己的循环：`app.failLocked` 失败时**留着** `Query`，界面
		// 这边 `draft == applied` 每一帧都判它「已经搜过了」。少了它，`now - 零值`
		// 恒大于防抖窗口，于是下一帧立刻又发一次、再失败、再发——而生产里每次投递
		// 都叫醒下一帧（实测 20 帧打了 22 次）。
		Spec: "歌词搜索失败之后：界面上有一句交代加一个重试按钮、重试真的再发一次那个词、之后不许每帧重发",
		Tests: []string{
			"TestAFailedSearchSaysItIsAFailureAndDoesNotFireAgain",
			"TestAFailedSearchOffersARetryAndSaysItIsAFailure",
			"TestTheRetryButtonSearchesThatWordAgain",
			// 搜到了但一个候选都没有：说的必须是「没搜到」加一句「怎么才能不空」，
			// 不能是一片空白。
			"TestTheSearchBoxSaysThreeDifferentThings",
			// 关掉搜索弹窗时预览要一起收：预览开着这件事住在应用层，而界面上已经
			// 没有一个弹窗罩着它了——下一次打开就会看到上一次那一份没看完的歌词。
			"TestClosingTheSearchDialogTakesThePreviewWithIt",
		},
	},
	{
		Category: Negative,
		// 预览弹窗里那**第三种**「没有」，在界面上不能变成「网络不通」，也不能是空白。
		// 混成前者用户会去反复重试（这一条重试一万次也不会有歌词），混成后者那句话
		// 和「加载中」一样是在搪塞。而「采用歌词」得是禁着的——按下去不会有任何事
		// 发生，而那种按钮在界面上和坏了的按钮长得一模一样。
		Spec: "预览里没有歌词时说的是「未找到歌词内容」而不是「网络不通」或一片空白，「采用歌词」禁着；换歌之后按采用有一句交代",
		Tests: []string{
			"TestThePreviewSaysItIsNotAFetchFailure",
			"TestAdoptingSaysSoWhenTheSongChanged",
		},
	},
	{
		Category: Negative,
		// 铁律四：候选表的行键是**候选自己的 ID**。
		//
		// 🔴 这一条在界面上**看不出任何异样**（每一行的字都是对的），所以判据只能
		// 是「把同一批候选打乱之后重新搜，某首的行键还是它自己那一个」——按下标
		// 认的话，它换了位置行键就跟着换。
		Spec: "铁律四：候选表的行键是候选的 ID、不是下标",
		Tests: []string{
			"TestTheRowKeyFollowsTheSongAndNotItsPlace",
		},
	},
	{
		Category: Play,
		// #21 的界面那一半（BMG-66）走通一遍：两个源都摆着、**从屏幕上那首歌开始**、
		// 切 Tab 立刻用同一个词重搜那个源、预览里看得到完整歌词再采用、采用之后
		// 两个弹窗一起关；一个源都没挂时弹窗压根不开（开着的弹窗里用户输完词按下
		// 搜索，看到的会是一句「没有歌词源」——而那不是他做了什么导致的）。
		Spec: "歌词搜索弹窗：两个源分别标明、从正在放的那首开始、切 Tab 立刻重搜、预览里看完整歌词再采用、采用之后两个弹窗一起关；一个源都没挂时不开弹窗",
		Tests: []string{
			"TestTheSearchDialogListsBothSourcesAndStartsFromTheSongOnScreen",
			"TestSwitchingTheTabSearchesThatSourceOnItsOwn",
			"TestPreviewThenAdoptPutsTheLyricsOnScreen",
			"TestNoSourcesMeansTheSearchDialogNeverOpens",
		},
	},
}
