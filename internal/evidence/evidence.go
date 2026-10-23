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
	{
		// 封面的暂时失败（断网、超时、5xx、限流）按指数退避自己再要一次；
		// 永久失败（404、解码不了）不再要第二遍。所有等待都在假钟上推进，
		// 「到点」是确定的，不受机器负载影响。
		Category: FakeClock,
		Spec:     "封面暂时失败按指数退避自己再试（间隔递增、封顶、有次数上限），永久失败不重试；重试走后台路径、守住在途上限，成功后唤醒窗口",
		Tests: []string{
			"TestTransientFailureRetriesWithGrowingBackoff",
			"TestPermanentFailureIsNotRetried",
			"TestFailureClassification",
			"TestRetrySuccessFlipsStateAndWakes",
			"TestTheCoverBecomesAnImageAfterARetry",
			"TestFailureRecordsAreBounded",
			"TestTouchReplacesAForgottenEntry",
			"TestRetriesRespectTheInflightLimit",
			"TestCloseDropsAWaitingRetry",
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
	{
		Category: HTTP,
		// 顶栏右上角那个头像：**登录了画我自己的，未登录画「没有人」**。
		//
		// 三样东西各钉一件事，合起来才是一个完整的交代：
		//
		//   1. **形状**：地址在服务层就补好了协议、拼好了裁剪参数。照原样透传
		//      的话，28 见方的一格会去下几百 KB 的正方形原图——不报错、不变慢。
		//   2. **闸门**：未登录**一个请求都不发**。这一格是整个应用重画最勤的
		//      一处，而凭据过期之后每一条都是 -101——所以判据是「那台假 B 站
		//      收到了几个请求」，不是「回来的答案是什么」：那台 fake 明确回了
		//      「登录了、头像在这儿」，偷偷发出去的话回来的是一个**看起来能用**
		//      的答案。
		//   3. **形态不许合并**：合并之后未登录的用户在顶栏上看见别人的脸，而
		//      上面那几条**一条都不会红**。所以另有一条量「三档任意两档画出来
		//      差得够多」。
		//   4. **它画的是人，不是字母**：「不是一整块底色」这种问法钉不住——
		//      边框和圆角裁剪本来就让它不是纯色，实测把人形换成恒定的字母「B」
		//      或者干脆什么都不画，那一条都照样绿。所以量的是**形状**：最长的
		//      那一段墨（人形 17 px，字母 7 px）。
		//   5. **作废的那一趟不许落地**：退登时在途的那一趟带着上一个账号的头像
		//      回来，写进去的话退登之后顶栏上又出现一个人的脸。
		//
		// ⚠️ 第 3 条的判据是「不一样的比例」而不是「逐像素不相等」：那两格不在
		// 同一个位置（「登录」和「退出登录」宽度不同），而同一个形状落在不同的小数
		// 位置上，圆边抗锯齿的颜色就不一样——实测两种形态明明是同一张，逐像素比有
		// 44/784 个点不同。理由见 `internal/ui/navbar_avatar_test.go`。
		Spec: "头像：登录画真头像（走封面缓存），未登录画「没有人」而不是字母占位符，图下不来有交代；未登录一个请求都不发",
		Tests: []string{
			"TestNavCarriesMyOwnFace",
			"TestNavWithoutAFaceGivesNothingToShow",
			"TestNobodyLoggedInAsksNobodyAnything",
			"TestARealAccountGetsItsOwnFace",
			"TestADeadSessionIsNotTheSameAsNobodyBeingLoggedIn",
			"TestTheAvatarShowsMyOwnPictureOnceLoggedIn",
			"TestTheAvatarOnlyAsksOnceEvenThoughEveryFrameAsks",
			"TestTheAvatarSaysNobodyIsLoggedIn",
			"TestTheAvatarShowsAPersonAndNotALetterWhenNobodyIsLoggedIn",
			"TestTheAvatarSaysSomethingWhenThePictureDoesNotComeBack",
			"TestAFaceThatComesBackForTheWrongAccountNeverLands",
			"TestTheAvatarNeverLooksTheSameTwoWays",
		},
	},
	{
		Category: HTTP,
		// 「刷新数据」那一项以前按下去只弹一句「还没有数据可刷新」——而接口一直
		// 都在（`created/list`，收藏夹的新建 / 修改 / 拉全量都靠它）。这一条钉的是
		// 「按下去真的重新拉一次」：喂一个**会变**的假服务器，点了之后画面上那一列
		// 换了内容，而旧的消失。
		//
		// 后两条是它的另一半：未登录时**不许发请求**（发了只会换来一片 -101，而
		// 界面上看不出异样），以及在飞的时候连点不重复发。
		Spec: "「刷新数据」真的重新拉一次收藏夹；在飞时连点不重复发；未登录不发请求",
		Tests: []string{
			"TestRefreshingReallyPullsTheFoldersAgain",
			"TestRefreshingTwiceSendsOneRequestAndSaysItIsBusy",
			"TestRefreshingWithoutALoginSaysSoAndSendsNothing",
			"TestRefreshingWithoutTheFoldersMountedSaysSo",
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
	{
		Category: Paging,
		// 「数据显示样式」要几件事同时成立，缺一件就退化成「这个设置没用」或者
		// 「改了只有一页变了」：
		//
		//  1. **每一页**都跟着变（逐页一条子测试，少接一页就少一条）
		//  2. **紧凑真的更矮**（量的是画出来的那一块，不是某个变量）
		//  3. **网格那一档照样翻得了页**
		//  4. **网格那一档翻页失败之后还能重试**——这一条才是网格那半边必须有哨兵
		//     的理由（`blocked` 闩住的是自动重试，解开它的只有哨兵上那个按钮）。
		//     少了它症状是静默的：歌还在、能点，只是某一页失败之后再也不动了
		//  5. **哨兵落在某一行的中间格时也画得出来**（歌数除不尽列数就是这种情况）
		//     ——只判「整行从哨兵开始」那一支的话，网格里看不见它
		//
		// ⚠️ 第 3 条和第 4 条**是两件事**，别合成一句「网格没哨兵就翻不了页」——那句话
		// 是错的（翻页的判据是「末尾那一行进没进画面」，而网格里最后一行就是歌的行）。
		// 把网格的哨兵拿掉，第 3 条那两条照样绿，只有第 4 条会红（外加
		// `TestEveryViewShowsTheSentinelWhileThereIsMore`——它钉的是 `SetFooter`
		// 扇出到两份那一处，其余那些要么只量列表那半边、要么直接对 `*Grid` 调
		// `SetFooter` 绕过了这一层）。
		Spec: "「数据显示样式」在三档之间换：每一页都跟着换、紧凑行高真的更矮、网格那一档照样翻页，翻页失败了也还有重试（哨兵落在行中间时同样画得出来）",
		Tests: []string{
			"TestEveryPageFollowsTheDisplayStyleSetting",
			"TestTheCompactRowsAreShorterThanTheNormalOnes",
			"TestTheGridCountsTheSentinelAsARow",
			"TestTheGridKeepsPagingWorking",
			"TestTheGridOffersTheRetryThatTheListOffers",
			"TestTheGridStillOffersARetryOnARealPage",
			"TestEveryViewShowsTheSentinelWhileThereIsMore",
			"TestTheSentinelShowsEvenWhenItLandsMidRow",
			// 上面那条引出来的另一半：最后一行没排满时，那几格必须空着而不是去越界
			// 取 `songs[i]`。
			"TestAnUnfilledLastCellStaysEmpty",
			"TestTheGridStillPagesOnARealPage",
			"TestSwitchingViewsKeepsTheSongs",
			// 多选接线要装到**两份**控件上：只装列表那半边的话网格那一档一格勾选框都
			// 没有，而全选走 `Len()` / `At()` 不经过画法、照样能用——症状是「九成
			// 用法没了，界面上看着多选还是好的」。
			"TestTheBatchOpsStillWorkInEveryView",
		},
	},
	{
		Category: Paging,
		// 设置那一格旁边的预览：三张缩略图各画各的形状，选中的那一张用主色描边。
		// 判据是「描边里离主色更近」而不是「有几个主色像素」——边框是圆角加抗锯齿
		// 画出来的，逐字比颜色永远是 0 个（见那条测试的注释）。
		Spec: "「数据显示样式」旁边有预览：选哪一档一眼看得出来",
		Tests: []string{
			"TestTheDisplayStyleSettingShowsAPreviewOfEachStyle",
			"TestThePreviewHighlightsTheChosenStyle",
		},
	},
	{
		Category: Paging,
		// 播放量与投稿时间两列（票 #142）。
		//
		// 这一张是**功能票**，所以两头都要有证据：
		//
		//   - 数据层：各页接口返回里的那个数真的解析进了条目——收藏夹是
		//     `cnt_info.play` / `fav_time` / `pubtime`，搜索是 `pubdate`，
		//     UP 空间是 `created`（**不是 pubdate**）；没有这个数的那一条是
		//     零值，不是编出来的 0。
		//   - 界面层：列表行里有那一格、表头那两列、网格卡片封面底部渐变条的
		//     左半边也有它，而且**收藏夹那一路也有**（票面点名最关键的一条）。
		//   - 时间那一列的格式定在 `PubTimeText`（今天 / 昨天 / N天前 /
		//     MM-DD / YYYY-MM-DD），拿假时钟判，不跟墙钟走。
		Spec: "播放量与投稿时间：各页接口字段解析进条目（没有的页是零值不是 0）、列表行与表头有这两列、网格渐变条左半边有播放量、收藏夹一路也有、时间列的显示格式与「几天前」的日历日判据；窄窗口（最小窗口 900 + 侧栏 480）下两列依次让位给标题和行尾「⋯」，紧凑档播放量列装得下最长的一折",
		Tests: []string{
			"TestCollectionMediasCarriesThePlayCountAndTheTimes",
			"TestSearchVideosReadsTheResult",
			"TestSpaceVideosReadsBothShapesOfDurationAndCid",
			"TestEachPageThatHasAPlayCountCarriesItIntoTheRow",
			"TestTheUpSpaceVideosCarryTheirPlayCount",
			"TestTheRowsAndTheHeaderCarryThePlayCountAndTheTime",
			"TestTheColumnsStayOffWhenThePageDoesNotAskForThem",
			"TestTheTimeColumnTitleCanBeChangedPerPage",
			"TestPubTimeTextFoldsLikeTheTicket",
			"TestPubTimeTextCountsCalendarDays",
			"TestTheCollectionPageShowsThePlayCountAndTheCollectionTime",
			"TestTheCollectionGridCarriesThePlayCountIntoTheGradient",
			"TestTheCollectionTimeTitleFollowsTheContainer",
			"TestTheSearchVideoTabShowsThePlayCountAndTheUploadTime",
			"TestTheHomeTabShowsThePlayCountButNoUploadTime",
			"TestTheLaterPageShowsTheUploadTimeAsWellAsTheAddTime",
			// 评审返工：窄窗口下让位（先收时间列、再收播放量），标题与「⋯」不许被挤掉。
			"TestTheColumnsGiveWayToTheTitleWhenTheListIsNarrow",
			"TestTheCompactPlayCountColumnFitsTheLongestFold",
			"TestTheColumnsLeaveTheTitleVisibleInTheNarrowestWindow",
			"TestTheSearchListGivesWayToTheTitleAtTheWidestSidebar",
			"TestTheLaterRowsKeepTheirOpButtonAtNarrowWidths",
		},
	},
	{
		Category: Paging,
		// 满屏播放器开着的时候，底下那一页**一帧都不建**（票 #161），而它的
		// 每帧副作用（收后台结果、起该起的那一拉）照跑。
		//
		// 三条各自钉一件事，**少一条就有一半是对的**：
		//
		//   - 只钉构建次数 → 副作用跟着一起停了（关掉满屏之后那一页停在打开
		//     那一刻的样子，要再等一趟才更新）；
		//   - 只钉副作用 → 页面还在每帧被建（用户看不见，机器白干）；
		//   - 只钉前两条 → 「关掉满屏之后的第一帧」没人管（可能慢一帧才画出来）。
		//
		// 判据是**构建次数**（探针数 `Shell.page` 那一次分派）而不是「快了多少」
		// ——后者在这台机器上量不出来，而前者是确定的。
		Spec: "满屏播放器开着的时候底下那一页一帧都不建；它的每帧副作用照跑；关掉满屏之后的第一帧就是当前状态",
		Tests: []string{
			"TestTheUnderlyingPageIsNotBuiltWhileTheFullPlayerIsOpen",
			"TestBackgroundResultsStillLandWhileTheFullPlayerIsOpen",
			"TestTheFirstFrameAfterClosingTheFullPlayerShowsTheCurrentState",
			"TestEscapeReturnsFromTheFullPlayerAtOnce",
			// 「每一页的副作用都在那张表里」那一条：加了新页而忘了加它，那一页
			// 在满屏开着的时候副作用就停更了——症状是静默的。
			"TestEveryPageWithContentHasASync",
			// 「跳过绘制不能连带跳过每帧的副作用」那三条：满屏期间那个 125ms 的
			// 自激钟（防抖钟没人推进 → 每 125ms 醒一次，而那一帧画的是满屏）、
			// 「正在录」的快捷键被静默取消（录制期间窗口一个组合键都不登记）。
			"TestTheSearchWatchStopsWhileTheFullPlayerIsOpen",
			"TestTheShortcutRecordingSurvivesTheFullPlayer",
			"TestTheCollectionSearchClockStopsWhileTheFullPlayerIsOpen",
		},
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
		// ⚠️ **Windows 那一半的解码能力**（GitHub issue #128）：macOS 走
		// AudioToolbox（`extaf`），Windows 走 Media Foundation
		// （`internal/service/audio/mfadec`，`IMFTransform` +
		// `CLSID_CMSAACDecMFT`）。
		//
		// 下面这些证据**只在 Windows 上跑**（`//go:build windows`）。在 macOS 上
		// 它们连编译都不进，所以「台账说它们在」和「它们上一次真的跑过」是两件
		// 不同的事——这正是 RealOnly 那一位要说明的。跑它们的地方是 Actions 上的
		// `windows-2022`（`.github/workflows/windows-aac-spike.yml`）。
		//
		// HE-AAC 那一档在台账里**故意不列**：没有素材可验，列进去就是拿一条
		// 跳过的测试当证据。
		Spec: "Windows 上 AAC（MP4 和裸 ADTS 两种容器）能解出 PCM；首帧在 200 毫秒内；解出来的 PCM 与另一个解码器**逐样本**对得上（不是 RMS）；左右声道没调换；COM apartment 在负载下反复跑不出错",
		Tests: []string{
			"TestSpikeAACLCDecodesOutOfMediaFoundation",
			"TestSpikeFirstFrameLatency",
			"TestSpikeWhichProfilesWeCanActuallyTest",
			"TestSpikePCMIsWithinTolerancesOfTheReference",
			"TestSpikeTheChannelsAreNotSwapped",
			"TestSpikeTheCOMApartmentSurvivesLoad",
		},
		RealOnly: true,
	},
	{
		Category: Platform,
		Spec:     "异步结果**逐窗口**唤醒（只叫醒主窗口的话迷你窗口停在旧内容）",
		Tests: []string{
			"TestEveryWindowThatDrewItGetsWoken", "TestEveryWindowGetsWoken",
			// 「改一次状态叫醒**所有**窗口」那一半（`App.AddWake` / `AddLocalWake`
			// 是挨个叫醒的）。
			"TestEveryRegisteredWindowIsWokenWhenTheStateChanges",
			"TestTheLocalLibraryWakesEveryWindowThatRegistered",
			// 「迷你窗口真的接上了自己那一条」那一半。
			"TestTheMiniWindowIsWokenWhenTheStateChanges",
		},
	},
	{
		Category: Platform,
		Spec:     "迷你播放器：320×100 的透明置顶小窗（43）",
		Tests: []string{
			// 窗口那几个开关，含「不许装回 `DisableResize`」——`isMaximized` 恒为真
			// 的话 `StateKey` 一条都存不下来，用户拖到哪都不会被记住。
			// ⚠️ 这一条证的是**开关摆对了**，不是「真存下来了」：真落盘要真窗口
			// （见本节末尾「真机才量得到」那条）。
			"TestTheMiniWindowIsAtBiuNumbers",
			"TestTheMiniWindowParksInTheTopRightCorner",
			// 透明窗口自己给了不透明底——不画的话它在桌面上是隐形的。
			"TestTheMiniCardCoversTheWindow",
			// 第二扇窗上铁律二（每帧开头装主题）和铁律四（view 包 Guard）也成立。
			"TestTheMiniFramesCarryTheChosenTheme",
			"TestAPanicInsideTheMiniViewLeavesTheProcessAlive",
		},
	},
	{
		Category: Platform,
		// 票 #162（规格票 #156 底下的一条）：迷你播放器是**面板语义**——显示和
		// 聚焦能拿键盘，但点它不激活应用，用户不会被从别的应用里拽出来。
		//
		// ⚠️ 这一条证的是**选项摆对了**（`Panel: true`，以及位置记忆的那几个
		// 前提还在：面板不能最大化，而 `isZoomed` 恒为真就会让 `StateKey` 一条
		// 都存不下来）。真的「点下去不激活」和「关掉再开停在原处」要真窗口 +
		// 真鼠标，量到的结果记在 `internal/ui/miniplayer.go` 的「本机实测」里，
		// 不在这儿假装验过。
		Spec: "迷你播放器是面板语义：点它不激活应用（票 #162），位置记忆仍然有效",
		Tests: []string{
			"TestTheMiniWindowIsAPanel",
		},
	},
	{
		Category: Platform,
		Spec:     "迷你播放器里能换歌、上下一首（44）、拖进度条（45）、调音量",
		Tests: []string{
			"TestTheMiniWindowStepsThroughTheQueue",
			"TestDraggingTheMiniProgressSeeksOnlyOnRelease",
			"TestTheMiniVolumeSliderSetsTheVolume",
			// 小窗里认的是**用户配的那一份**而不是出厂默认值；以及在小窗里按
			//「切换mini/完整模式」是关掉而不是再开一个。
			// （设置页写着「窗口在前台时按它就有用」，而小窗是最常停在前面的那一扇。）
			"TestTheMiniWindowRegistersTheInAppShortcuts",
			"TestTheMiniWindowUsesTheUsersShortcuts",
			"TestTheMiniShortcutDoesNotOpenASecondMiniWindow",
			// 迷你开着时来 biu:// 链接：先让开迷你再跳路由（路由表是主窗口的）。
			"TestADeepLinkMakesRoomForTheMainWindow",
			// 关掉迷你只关一扇窗（收尾走共用那一段而不是 `Shell.Close`）。
			"TestTheMiniShellTeardownIsTheSharedOne",
		},
	},
	{
		Category: Platform,
		Spec:     "迷你播放器和主窗口显示同一份播放状态（46），关掉它只关一扇窗",
		Tests: []string{
			"TestTheMiniWindowShowsWhatTheMainWindowShows",
			"TestClosingTheMiniWindowLeavesTheAppPlaying",
			"TestOnlyOneMiniWindowIsEverBuilt",
			"TestTheMiniBackButtonGoesBackToTheMainWindow",
		},
	},
	{
		Category: Platform,
		Spec:     "迷你播放器这一块本身：装得下 320×100、那四个键是图标键（框对、图形在）、控件身份稳（铁律四）",
		Tests: []string{
			"TestTheMiniCardFitsInThreeHundredAndTwentyByOneHundred",
			"TestTheMiniKeysAreIcons",
			"TestTheMiniControlsKeepTheirIdentityWhenTheLabelChanges",
		},
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
		// 封面落盘缓存（#158 / BMG-98）。这一组盯的都是**静默**的错法：
		// 没有上限就是磁盘一直长；删到目录外面就是删用户文件；半截文件被当成
		// 命中就是「每次启动都先闪一张坏图」；读盘放到主线程就是「滚列表卡住」。
		// 全部用临时目录 + `httptest` 验，不碰用户真正的缓存目录，也不连图床。
		Spec: "封面落盘缓存：目录有字节上限、按最久未用淘汰、启动清一次上次留下的超额；删除只发生在缓存目录内；半截文件按没有处理；空选项不落盘；重启（新实例、同一目录）不发请求就能拿到图；读盘在后台、写盘不挡主线程",
		Tests: []string{
			"TestDiskCacheEvictsTheLeastRecentlyUsed",
			"TestStartupSweepTrimsWhatTheLastRunLeft",
			"TestDeletionNeverLeavesTheCacheDir",
			"TestAHalfWrittenFileIsNeverDecoded",
			"TestACrashedWriteIsJustAMiss",
			"TestEmptyDirMeansNoDisk",
			"TestRestartServesFromDiskWithNoRequest",
			"TestDiskReadsDoNotBlockTheCaller",
			"TestDiskWritesDoNotBlockTheCaller",
			"TestCoverCacheDirIsUnderTheAppsCachePath",
			"TestTheMiniWindowSharesTheMainWindowsCoverCache",
			"TestACorruptDiskEntryThatCannotBeDeletedIsNotReadForever",
			"TestTheCacheIsClosedOnlyAfterTheLastUserReleasesIt",
			"TestTheMiniWindowKeepsItsCoversAfterTheMainWindowIsClosed",
		},
	},
	{
		Category: Negative,
		// 封面在「有图 ↔ 占位」之间反复切换（#208）。这一组盯的是一个**静默**
		// 的坏法：封面闪起来没有一句报错，用户读到的是「这个应用加载不动」。
		//
		// 根因是两条，**加在一起**才闪：存下来的那一张图太大（B 站的封面是
		// 1920×1080 的原图，解码之后 11MB，一屏十几张就是 170MB，超出缓存的
		// 字节上限），于是 LRU 把**正在显示着的**封面挤掉；而 LRU 的字节账还会
		// 漏（一条在解码回来之前被挤掉的条目，解码回来之后又记一笔，而那一笔再也
		// 挤不掉），挤的循环于是最后把整张表清空还挤不下——之后每来一张图就把
		// 表里剩下的全挤掉。
		//
		// 全部用 `httptest` 验（假图床 + 假 B 站接口），不连真网。
		Spec: "首页推荐的封面不再在有图和占位之间反复切换：一屏封面按显示尺寸存得下、字节账不漏、还在下的那一张不参与淘汰",
		Tests: []string{
			"TestTheSameCoverStaysReadyAcrossFrames",
			"TestTheHomeCoversDoNotGoBackToPlaceholder",
			"TestStoredCoversAreScaledDown",
			"TestAnEntryEvictedBeforeItIsDecodedIsNotAccounted",
			"TestTheEvictionSkipsEntriesThatHaveNoBytesYet",
			"TestACoverThatIsStillLoadingIsNotEvicted",
			"TestTheByteAccountDoesNotLeak",
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
		Category: Negative,
		// **同一个词还在转的时候再提交一次**（搜索框里再按一次回车、或者点一下
		// 「搜索」按钮——上游那两处都直接调 `handleSubmit`）。
		//
		// 🔴 这一条钉的是**根因那一半**：`SearchLyrics` 里「已经转着就别再发一遍」
		// 那个早退排在 `s.gen++` 之后的话，那一次重复提交会把**正在飞的那一趟**作废掉
		// （它回来时发现代次变了，直接走人、**不置 `loading = false`**），而早退自己
		// 又不发新的——于是 `loading` 从此再没有人翻回来。界面上是「正在搜…」停到进程
		// 退出：一条候选、一个错误、一个重试按钮都没有，用户唯一能做的就是关掉弹窗
		// 重开一次（而重开时网络还慢的话就再卡一次）。
		//
		// 而代次是**作废**用的不是**撤销**用的：每一次 `s.gen++` 都必须紧跟着一次
		// 「新的那趟会负责把它该做的事做完」，中间插一个不发新的早退就不成立。
		//
		// 后一条（界面那一条）钉的是同一件事的另外一半：根因改没改、界面上坏成
		// 什么样，两边都要能验。
		Spec: "同一个词还在搜索中时再提交一次：是一次纯粹的空操作——不重复发请求，也不许作废在飞的那一趟（否则 loading 再没有人翻回来，界面永远停在「正在搜…」）",
		Tests: []string{
			"TestSubmittingTheSameWordAgainWhileItIsStillRunningDoesNotStrandIt",
			"TestSubmittingTheSameWordAgainWhileItIsStillRunningStillShowsTheCandidates",
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
		Category: FakeClock,
		// 搜索联想词（#97 A25）那 500ms。⚠️ **和界面上另外两处「等用户把词敲完」用的是
		// 同一个常量**（`ui.searchDebounce`：稍后再看 / 历史记录的搜索框、歌词搜索、
		// 这一档），所以这里量的是「一个钟、三个地方用」的那一个。
		//
		// 「收掉浮层之后那个钟跟着停」也单列：它是**每帧**在 `syncSearchWatch` 里
		// 收的，而醒着的定时器醒到进程退出——用户看不出任何异样，只是这个应用从那
		// 以后再没省过电。
		Spec: "搜索联想词 500ms 防抖：假时钟推、不真等；每敲一个字重新起算；浮层收掉之后那个钟跟着停",
		Tests: []string{
			"TestTheSuggestKeywordIsDebouncedBy500ms",
			"TestTheSuggestDebounceRestartsOnEveryKeystroke",
			"TestTheSearchWatchStopsWhenTheOverlayGoesAway",
		},
	},
	{
		Category: HTTP,
		// 联想词那个接口（Biu 的 `getSearchSuggestMain`）。**参数逐个对着上游抄**，
		// 少一个的症状在界面上像「联想词永远是空的那一档」，而那和「拉不到」长得
		// 一模一样。
		//
		// 🔴 **「发去哪一台」单列两条，因为它靠 fixture 量不出来**：假 B 站把**每一个**
		// 主机都指到同一个本地 URL（那正是 `TestOnlyPointHostsAtFixture` 干的事），
		// 所以「打到了 `/main/suggest`」这一句对 `api.bilibili.com` 和
		// `s.search.bilibili.com` 一样成立——把 `hostSearch` 写成 `hostAPI`，整包
		// 测试照样全绿（评审实测）。所以那两条**不走 fixture**：一层只记 `URL.Host`
		// 的网络，量的是请求真正的那个 host；另一条量的是常量本身不许和 `HostAPI`
		// 一样。
		//
		// ⚠️ **签名（不发 w_rid、不先跑一趟 nav）是另一条**：WBI 是 `api` 那台的规矩，
		// 这一台不认，多带只是**赌**它不介意。
		//
		// 「两套字」（`term` 干净、`name` 带标签）是界面上最要紧的一半：拿 `name`
		// 显示是一排尖括号，拿它去搜是把标签当关键词发出去。
		//
		// ⚠️ **这一票没有走真网**：接口的地址、参数和回包形状都要真机回归，那是另
		// 一次安排；这里量到的是**我们这一层**（发出去的是什么、回来的怎么认、
		// 失败有没有被当成「一个都没有」）。
		Spec: "搜索联想词的接口：发去搜索站那一台（s.search.bilibili.com，不许打成 api 那台——fixture 量不出来，所以用一层记 host 的网络钉）、只发 term 且不带 WBI 签名；回包里 term/name 两套字分开认；空的和重复的丢掉、顶上那 10 条（两件事各一条，混在一起时上限会把重复的那条切掉）；拉不到要报错而不是回一个空列表",
		Tests: []string{
			// 地址：这两条不过 fixture，量的是请求真正的 host。
			"TestSearchSuggestGoesToTheSearchHostNotTheApiOne",
			"TestTheSuggestHostIsNotTheApiHost",
			// 参数与签名。
			"TestSearchSuggestSendsTheWordOnTheSearchHost",
			"TestSearchSuggestGivesBackThePlainWords",
			"TestSearchSuggestDropsTheOnesThatAreNotWords",
			"TestSearchSuggestKeepsOnlyTheFirstFew",
			"TestSearchSuggestFallsBackToTheNameWhenThereIsNoTerm",
			"TestSearchSuggestAsksForNothingWithoutAWord",
			"TestSearchSuggestSaysSoWhenItCannotAsk",
			"TestSearchSuggestSaysSoWhenTheAnswerIsUnreadable",
		},
	},
	{
		Category: Platform,
		// 规格的四条验收（#97）。逐条钉：
		//
		//   - 输入时浮层里多一档「联想词」（**浮层里的另一档，不是另一个浮层**——
		//     两套浮层的关闭时机不一样，并排会出现「关了一个另一个还在」，所以
		//     Esc 那一条钉的是两档一起收）；
		//   - 和历史建议在界面上区分得开（判据是**画出来的像素**：chip 有底色，
		//     联想那一行没有；另外还有「清除全部」只清历史那一档这个行为上的后果）；
		//   - 选中一条等于选中那次的搜索（页面上、URL 上、**框里**都是那一个词）；
		//   - 请求失败时静默（浮层保持只有历史那一档，一个错字都没有、提示条也不弹）。
		//
		// ⚠️ 「设置里的『显示搜索历史』不许把联想词一起关掉」也单列：那个设置管的
		// 是「我不想看见我搜过什么」，而联想词是 B 站这次给的。
		Spec: "搜索浮层里多一档「联想词」：输入时出来、和搜索历史在界面上分得开、选中等于选中那次的搜索、拉不到时静默（浮层保持只有历史那一档）、和历史在同一个浮层里（Esc 一起收）、设置里的「显示搜索历史」不管它；两档都摆满时（最小窗口 + 10 条长历史）最下面那几条也在屏幕里、滚得动、点得中",
		Tests: []string{
			"TestTypingBringsUpTheSuggestTerms",
			"TestTheTwoSectionsAreToldApart",
			"TestChoosingOneSearchesThatWord",
			"TestAFailedSuggestLeavesOnlyTheHistory",
			"TestEscapeTakesBothSectionsAway",
			"TestTheHistorySettingDoesNotTakeTheSuggestWithIt",
			// 框里一个字都没有时一个请求都不发（点开搜索框是在翻历史，不是在要联想）。
			"TestNoWordMeansNoRequest",
			// 联想词那一档没有 ×：那是 B 站这次给的，删不掉。
			"TestTheSuggestTermsCannotBeDeleted",
			// 两档都摆满时（最小窗口 + 10 条长历史 + 10 条联想）最下面那几条**点得着**：
			// 面板能滚、高度上限跟着窗口走（评审钉过：没有滚动容器时最后一条被排在
			// y=735，而屏幕只有 560 高——看不见、点不着、也滚不到）。
			"TestThePanelFitsInASmallWindowAndScrolls",
			"TestReopeningTheOverlayStartsAtTheTop",
		},
	},
	{
		Category: Negative,
		// 联想词那一摊的边界。
		//
		// 🔴 **失败之后不许每帧重发**是这一条里最要紧的：业务层失败时仍然记着那个
		// 词，界面上「已经问过了」那一支才成立；少了它就是自己养自己的循环（生产里
		// 每次投递都叫醒界面），于是浮层底下一直刷网络而用户只看到搜索框卡一下。
		//
		// 迟到的结果不许盖掉新的（代次守卫）、退出了不许再往状态上写、框里空了上一
		// 批要收掉（它属于上一个问题）、没挂上 B 站客户端也要安静。
		Spec: "联想词的边界：失败之后静默且不每帧重发、重敲同一个词还能再问一次（失败是静默的，界面上用户唯一能做的就是再敲一遍）、空框不许自己养自己（每一帧都请求下一帧 = 窗口永远停不下来）、迟到的结果不盖掉新的、退出之后不写回来、框空了收掉上一批、同一个词不搜第二遍、没挂客户端时不吵",
		Tests: []string{
			"TestAFailedSuggestSaysNothingAndDoesNotFireAgain",
			"TestTypingTheSameWordAgainAsksAgain",
			"TestAnEmptySearchBoxDoesNotKeepTheWindowRedrawing",
			"TestAStaleSuggestNeverLands",
			"TestClosingTheAppStopsTheSuggestFromComingBack",
			"TestEmptyingTheBoxTakesTheSuggestListAway",
			"TestTheSameWordIsNotAskedForSuggestTwice",
			"TestAskingForSuggestWithoutABiliClientIsQuiet",
			"TestAskingForSuggestGivesBackWhatBilibiliSaid",
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
	{
		Category: Play,
		// issue 93：播放条和顶栏上的按钮从文字变成图标。
		//
		// 🔴 **判据全是像素，不是「调了 SetIcon」**。MyGo 的 `ui.Icon` 没有元素树
		// 可以查，无头 Tester 的 `Find` 只认画出来的文字和 `Label` 起的名字——而
		// 图标两样都没有。所以「图标画出来了没有」是从那一块的像素上看的。
		//
		// 深浅两套主题都要验：跟着主题走这件事，浅色下画得出来、深色下糊在背景
		// 里是两种完全不同的坏，只验一套等于没验。
		Spec: "播放条和顶栏的按钮都有图标：单色跟着主题走、深浅两套都不糊在背景里、点击区比图形大、每一处都有无障碍标签",
		Tests: []string{
			// 资源本身：名字都在、都解析得出、都有尺寸、都没有写死颜色。
			"TestEveryIconWeNamedIsThereAndItParses",
			"TestTheIconResourcesAreAllMonochrome",
			// 画面：播放条 / 顶栏上每一个动作那一块都画出了东西，深浅各一次。
			"TestThePlayBarDrawsAnIconForEveryAction",
			"TestTheNavBarDrawsAnIconForEveryAction",
			// 「跟着主题走」的那一半：不糊在背景里（亮度差要够）。
			"TestTheGlyphsAreNotSwallowedByTheBackground",
			// 「不用瞄得准」的那一半：点击区比图形大，而且留出来的那一圈按得中。
			"TestTheHitAreaIsBiggerThanTheGlyph",
			"TestTheRoundKeyKeepsItsEdge",
			// 状态变了图要跟着换：播放/暂停、赞没赞、播放模式四档各画各的。
			"TestThePlayAndPauseKeysSwapGlyphs",
			"TestTheLikeKeyDrawsItsOwnIcon",
			"TestEveryPlayModeHasItsIcon",
			"TestEveryVolumeLevelHasItsIcon",
			// 「**哪一档配哪一张**」——上面那几条断的是「两张不一样」，断不了
			// 「对调」（对调之后照样两两不同，而用户看到的是反的那一张）。这一条
			// 拿一张抄自 Biu 源码的表逐项钉住，那一条堵死「在 view 里绕过」。
			"TestWhichStateGetsWhichIcon",
			"TestTheStateIconsOnlyReachTheScreenThroughTheirFunction",
			// 满屏那一行三枚都是图标（以前是「图标 — 文字 — 图标」半新半旧）。
			"TestTheFullPlayerControlsAreAllIcons",
			// 唯一没换成图标资源的那两枚（框架那一支）也仍然有图形、点击区更大。
			"TestTheHistoryKeysStillHaveAGlyphAndAHitArea",
			// 资源里不许画透明边撑点击区（换尺寸就错位，且界面上看不出坏了）。
			"TestTheResourceFilesCarryNoTransparentMargin",
		},
	},
	{
		Category: Paging,
		// 下载记录页那一排「全部 / 音频 / 视频」（#99 / SPEC 的 A26，#91 的用户
		// 故事 21~23）。音频和视频**本来就是同一张表**里的两类行，下了一堆 MV
		// 之后用户要在一列表的歌里翻才找得到。
		//
		// ⚠️ **「筛出来的是哪几个」才是判据，「剩下几个」不是**：一个把两档都
		// 认成同一类的实现，剩下来的**行数**照样对得上，而屏幕上摆着一支 MV。
		// 反过来那一半同样要钉：那一位是从**真队列**（队列文件读回来的任务）
		// 里读出来的——只喂快照的话，「`Snapshot.Video` 谁填谁自便」的接线
		// 照样绿，而读错了的症状是屏幕上多出一列一模一样的歌行，看不出任何异样。
		//
		// ⚠️ **筛出来的空和真的没下载必须是两句话**：混成一句的话，用户在「视频」
		// 那一档看到「暂无内容」，会以为自己白下了几十首——而那些歌就在下面一档
		// 里躺着。这两支还要分开钉「那一排摆不摆」：真的没下载不摆（没得可回），
		// 筛出来的空要摆（用户得回得去），只钉一支的话「一空就收起来」过一半。
		//
		// ⚠️ **纯前端**：判据是那台记请求的 http.Client 上仍然是零**加上**队列
		// 前后一模一样，而不只是「界面上那几行变了」——后者一个「每帧重拉一次
		// 队列」的写法照样绿，而那个写法的症状是切一次档就被叫一次网络。
		//
		// ⚠️ 顺带钉一条**界面上的话不许说谎**：「清空记录」清的是整个队列，所以
		// 筛着的时候弹窗里报的必须是整个队列的条数。按屏幕上那几行报的话，用户
		// 看到「确认清空这 1 条」而点下去删掉的是 4 条——不可撤销。
		Spec: "下载记录页「全部 / 音频 / 视频」三档筛选：各档留下的是**该留的那些行**（判据是真队列里的那一类，不是快照）；纯前端（不发请求、不动队列）；切路由再回来保留；筛出来的空和真的没下载说的不是同一句话，而且那一排在筛空的界面上还回得去；筛着点「清空记录」报的是整个队列的条数",
		Tests: []string{
			"TestTheThreeFiltersAreOnScreenAndDefaultToEverything",
			"TestEachFilterShowsOnlyThatKind",
			"TestTheFilterReadsTheKindOutOfTheRealQueue",
			"TestTheFilterSurvivesLeavingAndComingBack",
			"TestTheEmptyCopyAfterFilteringIsNotTheCopyForNoDownloads",
			"TestTheEmptyPageHasNoFilterToPoke",
			"TestTheFilterIsStillThereAfterFilteringEverythingOut",
			"TestChangingTheFilterSendsNoRequest",
			"TestClearingWhileFilteredStillCountsTheWholeQueue",
		},
	},
	{
		Category: Negative,
		// 下完一首歌之后在它旁边存一份封面（#99 / #91 的用户故事 6）。存这一份是
		// 为以后的本地音乐库备料，而音乐库读的是歌——**视频那一路不存**：B 站
		// 给的是稿件的封面，和交织出来的那一帧画面不是一回事。
		//
		// 🔴 **文件名必须从落盘的音频名推出来**，不能从歌名重新算一遍：
		// 同名避让（`歌名 (1).m4a`）会让两边算出两个不同的名字，而用户看到的是
		// 两个文件、一首歌。而入队时给的扩展名只是个临时值（`prepare` 会按取流
		// 地址上的真后缀重算），所以「用入队时那个名字」也不对——这一条单独钉。
		//
		// ⚠️ **失败不许牵连这一首**：歌已经在 `Dest` 上了，把它标成「出错」是拿
		// 一整首歌去换一个几百 KB 的图，而界面上那一行的红字只有这一件事可说。
		// 「已经有了一份就不重下」也是——判据是**图床被要了几次**，不是「文件还
		// 在」：重下一遍内容一样时这两者长得完全一样，而覆盖会删掉用户自己换上
		// 去的那张。
		//
		// 🔴 **两条「不许」是这个引擎里最贵的两条，都要钉**：存封面**不许**挡住队列
		// （`loop` 一次下一首，而 `Options.Client` 没有 `Timeout`、图床接了连接不
		// 回包时那个挂住不会自己解开 —— 后面排着的歌一个字节都下不了），也**不许**
		// 让一个失败的写留下「下一次会被当成已经有了」的东西（那会让这一首从此没有
		// 封面，而界面上那一行还写着「下载完成」）。所以落盘走「临时文件 → rename」
		// （和队列文件同一条约定，`store.go` 顶上写着理由），而「已经有一份」那个
		// 判据必须是「是个非空的**普通文件**」而不是 `exists()`——同名处是一个目录时
		// `exists()` 一样为真。
		Spec: "下载歌曲时封面单独存一份：文件名就是音频名换个后缀（同名避让和落盘时的后缀重算都要跟上）、盘上已有就不再下一次也不覆盖、拿不到不把这一首算成失败、视频那一路不存；存封面不许挡住队列或退出（后台走 + 自带超时 + Close 收得掉）、不许让失败的写留下挡掉之后每一次重试的东西（临时文件 → rename，且「已有」只认非空的普通文件）",
		Tests: []string{
			"TestTheCoverLandsNextToTheSongAndSharesItsName",
			"TestTwoSongsWithTheSameNameGetTheirOwnCover",
			"TestTheCoverNameFollowsTheNameThatActuallyLanded",
			"TestACoverAlreadyOnDiskIsNotFetchedAgain",
			"TestACoverThatCannotBeFetchedDoesNotFailTheSong",
			"TestAVideoDoesNotGetACoverThatIsNotItsCover",
			// 不许挡住队列 / 不许挡住退出。
			"TestACoverHostThatNeverAnswersDoesNotStallTheQueue",
			"TestClosingTheAppDoesNotWaitOnAHangingCover",
			// 不许留下挡掉之后每一次重试的东西。
			"TestTheCoverIsWrittenThroughATemporaryFile",
			"TestTheFinalNameOnlyEverAppearsWhole",
			"TestAFailedCoverSaveLeavesNothingBehindThatWouldBlockEveryRetry",
			"TestADirectoryInTheCoversPlaceIsNotMistakenForACover",
		},
	},
	{
		Category: HTTP,
		// 收藏夹 ⋯ 菜单里「清除失效内容」和「删除收藏夹」这两个项**早就在位**、
		// 文案也写好了，而按下去只弹一句「还没接上」。这一条钉的是它们变成真
		// 动作之后的几件事——每一样都是「做了一半比不做更糟」的那一种：
		//
		//   - **清除失效内容是服务端的一个动作**，不是本地按 attr 筛一遍再删。
		//     筛的那条路在三个地方各自会漏（`Attr` 认不全 / 翻页期间列表在变 /
		//     几千首要几十趟请求），而三个症状长得一模一样：清完还剩几个。
		//   - **清完之后**列表才重来。点完立刻刷的话刷回来的是还带着失效条目的
		//     列表——刷新确实发生了，只是发生得太早，症状是「按完什么都没变」。
		//   - **只有自己创建的收藏夹**摆得出删除（别人建的 B 站没这个接口、
		//     默认收藏夹删不掉、没登录时 `0 == 0` 会误判通过）。三个少一个都是
		//     让用户按出一个注定失败的按钮。
		//   - **「删 B 站上那个」和「只删本地记录」是两个选项、两套措辞**：前者
		//     点名收藏夹、点名里面几首、说撤不回来；后者明确说 B 站上的还在，
		//     而它**一个请求都不发**。
		//
		// ⚠️ **这一票没走真网**：两个都是写操作，写的是用户真实的收藏夹，而
		// 「删 B 站上那个」不可逆。所以全部用 HTTP fixture。真接口的兼容性
		// （参数集对不对、B 站收不收）要真机回归，见任务报告「需要真网验」。
		Spec: "收藏夹 ⋯ 菜单的「清除失效内容」和「删除收藏夹」：清除是服务端的一个动作（本地不筛）、清完之后列表才重来、只有自己建的收藏夹摆得出删除、取消则一个请求都不发；「删 B 站上那个」和「只删本地记录」分成两个选项且措辞不同，后者一个字节都不往 B 站发（这一票走 HTTP fixture，两个都是会动用户真实收藏夹的写操作）",
		Tests: []string{
			// 服务层：发出去的形状。少一个字段 B 站不报错，只是清不干净。
			"TestCleanSendsOnlyTheFolderID",
			"TestCleanRefusesNobody",
			"TestCleanSaysWhichFolderItWas",
			// 业务层：谁有资格删。
			"TestOnlyYourOwnFolderOffersDeletion",
			"TestTheHeaderInfoCarriesWhatDeletionJudgesOn",
			// 业务层：清完之后界面才知道该重来。
			"TestClearingInvalidAsksTheServerToDoIt",
			"TestAFinishedClearTellsTheListToRedraw",
			"TestAFailedClearDoesNotTellTheListToRedraw",
			// 业务层：两个删除共用一条路 / 本地那个不发请求。
			"TestDeletingFromThePageGoesThroughTheSameCallAsTheSidebar",
			"TestForgettingAFolderNeverTouchesBilibili",
			// 界面层：摆得出来的项、确认框、取消、刷新、走开。
			"TestOnlyTheOwnerGetsTheOwnerActions",
			"TestNoTwoMenuItemsShareAKeyOrALabel",
			"TestClearingTheInvalidContentAsksFirst",
			"TestCancellingAnyOfTheThreeSendsNothing",
			"TestACleanedFolderRedrawsItsList",
			"TestDeletingAFolderSaysWhichOneAndLeavesThePage",
			"TestDeletingFromThePageIsTheSameCallTheSidebarMakes",
			"TestTheTwoDeletionsDoNotReadTheSame",
			"TestForgettingTheFolderNeverTouchesBilibili",
			"TestAFolderNobodyOwnsCanStillBeForgotten",
			// 失败路径：删不掉时不许把用户踢走，更不许把失败原因一起擦掉。
			"TestAFolderThatWontBeDeletedKeepsTheUserWhereTheyAre",
			// 「从侧栏移除」要清的是**它所在的那一组**（否则提示就是谎话）。
			"TestForgettingClearsTheRowOutOfWhicheverGroupItIsIn",
			"TestForgettingTakesTheRowOutOfBothGroups",
		},
	},
	{
		Category: Play,
		// 「多 P 的投稿只能播第一集」（BMG-47 的 A12）那一张票的界面那一半：
		// 播放条上有分集入口 → 打开是浮层列表、每一集有编号和标题 → 标出正在播的
		// 那一集 → 点一集就切过去 → **单 P 不摆这个入口**。
		//
		// 逐条钉的是**六个容易被做成「界面上看不出坏了」的点**，而它们合起来才是
		// 「功能接上了」：
		//
		//   · 编号是**画出来的字**——量的是 `Texts()` 里有一条逐字等于 "1"/"2"/"3"，
		//     不是字段里有 page（`partRowsFor` 填对了而 view 没画，别条照样绿）。
		//   · 正在放那一集**整行染主色**，而没在放的不许染——两半都钉，染了这一半
		//     而不钉另一半的话，「标出来」就只是「有一行不一样」。
		//   · **单 P 不摆入口**：摆一个点了没反应的键，用户会以为应用坏了。
		//   · 问不到时说「问不出分集」而不是留一片空白——空白在界面上和「这一首
		//     没有分集」一模一样，而空列表**进都进不去**（入口在「没问到」时也得
		//     摆着，那句人话才有地方显示）。
		//   · 摆在哪一行是**量出来的**：单 P 与多 P 两档量同一块播放条，歌名和进度条
		//     必须一样宽（UP 名让位是它该做的，它是那一行里唯一 `Grow(1)` 的）。
		Spec: "分集抽屉：播放条上有入口、多 P 才摆、浮层里每集有编号和标题、标出正在播的那一集、点一集就切过去并收起、问不到时说得出为什么、摆这个键不花掉歌名和进度条的宽度",
		Tests: []string{
			"TestThePlayBarOpensTheEpisodesAndSwitchesToOne",
			"TestEveryEpisodeRowCarriesItsNumber",
			"TestTheDrawerMarksTheEpisodeThatIsPlaying",
			"TestThereIsNoPartsKeyForASinglePartVideo",
			"TestTheDrawerSaysWhyWhenTheyCannotBeAsked",
			"TestPickingTheEpisodeAlreadyPlayingStillClosesTheDrawer",
			"TestOpeningTheFullPlayerClosesThePartsDrawer",
			"TestTheDrawerDoesNotComeBackOnItsOwn",
			"TestThePartsKeyCostsNothingElseOnThePlayBar",
		},
	},
	{
		// 归「双平台」那一组：这一票动的三块（顶栏 / 侧栏 / 内容区）都是**窗口
		// 的骨架**，而播放条那一半在下面「播放」那组里另有一条（issue 93 的图标
		// 化、issue 93 的边界）。一处规格两个入口会让台账读起来像两件事。
		Category: Platform,
		// issue 127（SPEC 需求 19~26）那一票：界面骨架和上游对上的那几条。
		//
		// 🔴 **判据全是画面上的像素和横向位置**，没有一条是「调了某个函数」。
		// 这一票的三个症状**全都是静默的**：三处底色各自合法，页标题重复两遍也
		// 每一遍都对，五枚键都在而次序不对也点得动——只有读像素和量左右沿
		// 才分得出来。
		//
		// 深浅两套都要量这一条的理由写在测试里：浅色下 `#f4f4f5` 和 `#ffffff`
		// 只差 11，而深色下 `#27272a` 和 `#18181b` 各跳一级，**三带只有深色下
		// 才裂得开**。
		//
		// ⚠️ **括号里那半句在 #172 之后改了**：原来写的是「卡片弹窗仍是
		// Surface」——那时候下载记录那张卡和队列抽屉面板的底色确实取
		// `t.Surface`（比页面底色亮一档的「板」）。#172 按上游改了：上游那张
		// `Card` 走 `bg-content1`、那一枚 `Drawer` 走 `bg-background`，两处
		// **都等于页面底色**，浮起来靠的是那圈影。所以括号里现在写的是「和页面
		// 同源，浮起来靠影」。**这一句不改，台账就开始说谎**——而台账的
		// 自检只查「那个测试函数还在不在」，查不出它断言的东西变了。
		Spec: "界面骨架：侧栏/内容区/播放条三处底色同源且仍分得开（卡片与抽屉面板和页面同源，浮起来靠的是影）；设置内容收在中间一段宽度、输入框不横跨整行；页标题只出现一次；顶栏是上游那个高度；播放条右边那一组次序照上游（模式/下载/队列/音量/倍速），队列开关只在播放条上",
		Tests: []string{
			// 三处底色同源 + 分界仍在 + 卡片弹窗**仍浮得起来**（**互为反面，
			// 缺一不可**：只钉第一条的话，「把整窗刷成一个颜色」会全绿，而浅色下
			// 侧栏和内容区会完全一样）。
			//
			// ⚠️ 第三条那半句跟着 #172 改了：它原来断的是「面板 = `t.Surface`
			// （比底色亮一档）」，现在断的是「面板 = **页面底色**，而面板左缘
			// 那一列 ≠ 底色」——**分层从「靠底色亮一档」换成了「靠影和那条线」**。
			"TestTheChromeSharesOneBackground",
			"TestTheChromeStillHasASeparationFromTheBackground",
			"TestCardsAndDialogsStillStandOffTheBackground",
			// 播放条和内容区之间的**那道影**（票 #221）：上游是两层——外层 div
			// 的 `shadow-2xl` + 里面 Card 的 `shadow="sm"`——值在
			// `internal/ui/widgets/shadow` 里。
			//
			// 两条都要：一条量**画面上那两道线**（深色下认出它的是 Card 那档的
			// 1 像素**内白边**，浅色下是条外面那道 1 像素外黑边），一条钉
			// **值不许写在调用处**（外层那档朝下洒，在真窗口里几乎看不见，
			// 画面上量不出「它是不是两档」）。
			"TestThePlayBarHasTheUpstreamShadow",
			"TestThePlayBarShadowComesFromTheShadowPackage",
			// 设置页：封顶 900 + 居中（两档窗口都量），控件不横跨整行。
			"TestTheSettingsContentIsCappedAndCentered",
			"TestTheSettingsInputsAreNotFullRowWide",
			// 顶栏：高度照上游、上面没有页名，而页面自己那一份还在。
			"TestTheTopBarIsBiuHeight",
			"TestTheTopBarShowsNoPageTitle",
			"TestThePageStillShowsItsOwnTitle",
			// 播放条右边那一组：次序、没歌时不摆下载、队列开关只在播放条上。
			"TestThePlayBarRightRowFollowsBiuOrder",
			"TestTheDownloadKeyIsOnlyThereWhenThereIsATrack",
			"TestThePlaylistKeySitsOnThePlayBarNotTheTopBar",
		},
	},
	{
		// 归「双平台」那一组：这一票动的是**配色**（语义色、分隔线、卡片层次），
		// 和上面 issue 127 那一条同属窗口的视觉骨架，但说的不是同一件事——
		// 那一条讲「三处底色同源」，这一条讲「卡片浮起来靠影，不靠亮一级的板」。
		Category: Platform,
		// issue 163 的批次 1（票 #172）那一票：语义色与深色层次对齐上游色相。
		//
		// 🔴 **判据全是画面上那个像素是不是那个数**，没有一条是「调了某个
		// 函数」。这一票的三个症状**全都是静默的**：三个语义色换成另一套色相
		// 一样合法，分隔线用实色还是叠出来一样合法，卡片用不用亮一级的板也
		// 一样合法——只有逐像素读才分得出来。
		//
		// ⚠️ **每一件事都两头断**：主题里那个字段是什么值，只是「打算画成什么」；
		// 画面上那个像素是什么值，才是「用户看见的是什么」。所以下面这几条
		// 每一件都有「主题里那个值」和「画面上那个像素」两半。
		//
		// ⚠️ **深浅两套都要跑**：深色下这三件事的差别最大（卡片与背景同色、
		// 分隔线是 15% 白叠出来的），只跑浅色的话深色下那一面一次都没被验过。
		Spec: "配色：危险/警告/成功三个语义色是上游那一档（深浅两套同一个值）；分隔线深色是 15% 白叠在当前底色上、浅色是 #dbdbdb；深色下卡片和页面底色同源、浮起来靠的是那圈影，浅色下卡片不是一块灰",
		Tests: []string{
			// 三个语义色：主题里的值 + 画面上那个像素。
			"TestTheSemanticColorsMatchUpstream",
			"TestTheSemanticColorsReachThePixels",
			// 分隔线：主题里的值（深色那档跟着用户挑的底色重算）+ 画面上那一列。
			"TestTheDividerMatchesUpstream",
			"TestTheDividerReachesThePixels",
			// 卡片层次：卡里那一块 = 页面底色、且不是「亮一级的板」。
			"TestTheCardIsThePageColor",
			// 「那张卡真的在」——卡和页面同色之后，**影才是它存在的证据**。
			"TestTheDownloadTableSitsOnACard",
		},
	},
	{
		// 归「双平台」那一组：这一票动的是**列表的选中态**（点一行不再整行刷实心
		// 主色底），和上面两条同属「窗口长什么样」，但说的是另一件事——那两条讲
		// 配色和层次，这一条讲**点了之后那一行长什么样**。
		Category: Platform,
		// issue 163 的批次 1（票 #197）那一票：列表选中行去掉整行实心主色底，
		// 只保留「正在播放」那一行的主色高亮（flat，不是实心），键盘走到的那一行
		// 有焦点环。
		//
		// 🔴 **判据全是画面上的像素**：这一票的症状**全都是静默的**——主色底去掉
		// 之后，选中的那一行和没选中的那一行**长得一模一样**，两件事在画面上都不
		// 报错、不崩溃、不打日志。而「选中」这件事本身还在（右键菜单、双击播放、
		// 键盘走位都还指着它），所以「看起来没变」和「坏了」只有逐像素读才分得开。
		//
		// ⚠️ **每一件事都两头断**：主题/代码里那个值是什么，只是「打算画成什么」；
		// 画面上那个像素是什么值，才是「用户看见的是什么」。
		//
		// ⚠️ **深浅两套都要跑**：主色在两套下是同一个值，而「没有实心主色块」在两套
		// 下长得完全不一样（浅色下是白底上没东西，深色下是深灰底上没东西）。
		Spec: "列表选中行：点一行不再整行刷实心主色底（选中的那一行和没选中的长得一样，列表 / 网格 / 稍后再看与历史记录 / 本地音乐表格四处都是）；「正在播放」那一行仍是 flat 的主色高亮；键盘走到的那一行有焦点环（鼠标点过之后再按方向键也照样有，四处实现都是）",
		Tests: []string{
			// 列表：点一行 → 没有实心主色块（控件那一层 + 外壳那一层，深浅两套）。
			"TestClickingARowLeavesNoSolidAccentBlock",
			"TestClickingARowOnThePageLeavesNoSolidAccentBlock",
			// 网格：点一格 → 同样没有实心主色块（两套控件各写各的，两边都要断）。
			"TestClickingACardLeavesNoSolidAccentBlock",
			// 本地音乐的「表格」那一档：点一行 → 同样没有实心主色块（框架的
			// `ui.Table` 铺主色底的方式和列表那一档不同，两处都要断）。
			"TestTheLocalTableRowHasNoSolidAccentBlock",
			// 本地音乐的「表格」那一档：键盘游标那一行有焦点环（框架那一圈只认
			// `focusVisible`，方向键不会设回来，所以那一圈是自己画的）。
			"TestTheLocalTableCursorHasAFocusRing",
			// 「正在播放」那一行：主色高亮还在，**而且是 flat 不是实心**。
			"TestThePlayingRowKeepsTheFlatPrimaryHighlight",
			// 键盘：走到哪一行，那一行有焦点环（而鼠标点过的那一行没有）。
			"TestTheKeyboardRowHasAFocusRing",
			"TestClickingARowLeavesNoFocusRing",
			// 「鼠标点过之后再按方向键」那一路也得有指示——框架那个 `focusVisible`
			// 方向键不会设回来，靠它那一路是哑的（控件那一层 + 外壳那一层）。
			"TestTheKeyboardRingComesBackAfterAMouseClick",
			"TestTheKeyboardRingComesBackAfterAMouseClickOnThePage",
			"TestTheLaterRowKeepsItsRingAfterAMouseClick",
			// 选中跟着**那一首**走，不跟着位置走（铁律四）——焦点环就是「选中画出来了」
			// 唯一的证据，所以这一条现在量的是环的位置。
			"TestInsertingARowAtTheHeadKeepsTheChosenSong",
		},
	},
	{
		// 归「双平台」那一组：这一票动的也是**窗口的骨架**（播放条那一块），
		// 而它和上面 issue 127 那一条是同一处规格的两个入口——所以两条分开写，
		// 但说的是同一件事的两半。
		Category: Platform,
		// issue 135（票 #134 的第一单元）那一票：播放条从「能用」改成「对」。
		//
		// 🔴 **判据全是画面上的位置和像素**：这一票的五个症状**全都是静默的**——
		// 进度条永远是 80 像素照样能拖，三键次序反了照样点得动，旋钮不画也照样
		// 能 seek，音量图标是个样子也照样有音量——只有量得出来的那几样才算数。
		//
		// ⚠️ **「进度条随窗口变长」断的是单调，不是某个像素数**：进度条该有多长
		// 取决于窗口有多宽、也取决于右边那一组有多宽（音量收成图标之后它窄了
		// 141），所以钉死一个数的话，改动右边任何一枚键都会把它顶红。
		Spec: "播放条：进度条随窗口变长、三键是上一首·播放·下一首、播放键 48 像素、旋钮只在悬停或按下时画（键盘焦点上时也画）、填过的那一段平时是前景色、悬停/按下才变主色、音量收成图标加浮层（浮层里一根竖滑块和百分比，点图标静音、滚轮 ±0.05）、音量轨道填的是已经调上去的那一段、浮层走远了会自己收起、旋钮不探出滑块的框、按住时多一层更深的影子、浮层里的滑块不跟着百分比字数横向漂、三键落在窗口的竖直中轴上、中间那一列是 1:3:1、进度行占那一列的 3/4 且居中、三键之间的空隙是 24",
		Tests: []string{
			// 进度条：900 / 1200 / 1600 三档单调变长（**中间那档 1200 必须在**：
			// 只断两端的话，「中间那一列不参与分配」这种坏法在两端看不出来）。
			"TestTheProgressBarGrowsWithTheWindow",
			// 三键的左右次序（**断的是位置，不是「三个键都在」**）。
			"TestTheTransportKeysArePrevPlayNext",
			// 三键落在**窗口的竖直中轴**上（票 #216）——上游把三键和进度条都摆
			// 在中轴上，我们原来三键靠左、进度条铺满整列，两者的中轴对不上。
			"TestTheTransportKeysSitOnTheVerticalCenterLine",
			// 三键之间的空隙是 24（上游的 `space-x-6`）。
			"TestTheTransportKeysAre24Apart",
			// 中间那一列是 1:3:1，而进度行是它的 3/4 且居中。
			"TestTheMiddleColumnIsThreePartsWide",
			// 左边那一列是五份里的一份（歌名的框宽 = 那一列的宽）。
			"TestTheLeftColumnIsOneFifthOfTheBar",
			// 右边那一组贴在内容区的右沿，**一枚键都没有跑到窗口外面去**
			//（跑出去那一枚看不见也点不着，而界面上看不出坏了）。
			"TestTheRightGroupHugsTheRightEdge",
			// 播放键的尺寸：自己够大（48 起），而且比上下首大。
			"TestThePlayKeyIsBigEnoughToHit",
			// 旋钮：没有悬停时不画、悬停时画（深浅两套）。
			"TestTheProgressKnobIsOnlyDrawnWhileHovered",
			// 填过的那一段：平时是**前景色**、悬停/按下才变**主色**（票 #218，
			// 逐像素采样，深浅两套）。
			"TestTheFilledPartOfTheProgressIsForegroundUntilHovered",
			// 音量浮层：一根**竖**滑块 + 底下的百分比。
			"TestTheVolumePopoverHasAVerticalSlider",
			// 点图标静音 / 取消静音放回原来的音量。
			"TestClickingTheVolumeIconMutes",
			// 滚轮 ±0.05。
			"TestTheWheelOverTheVolumeIconChangesTheVolume",
			// 音量轨道填的是**已经调上去的那一段**（0% 空、100% 满）。
			"TestTheVolumeTrackIsFilledFromTheBottom",
			// 浮层**自己**收掉（不播放的时候没有人要帧，所以那一段必须自己
			// 排一个到点的唤醒）。
			"TestTheVolumePopoverClosesOnItsOwn",
			// 竖着那根旋钮**不许探出滑块的框**：它的中心要走**内容框**的算法，
			// 和框架的竖直映射（`1-(y-st.cy)/st.ch`）同一个坐标系。
			"TestTheVerticalKnobStaysInsideItsSlider",
			// 键盘 Tab 过去的时候旋钮（和画在它上面的焦点圈）要画出来。
			"TestTheProgressKnobShowsWithKeyboardFocus",
			// 按住的时候旋钮底下要有一层**更深的**影子（票面原话）。
			"TestTheKnobGetsDeeperWhileHeld",
			// 音量浮层里那根滑块**不跟着百分比的字数横向漂**（它就在指针底下）。
			"TestTheVolumeSliderDoesNotDriftWithThePercentText",
		},
	},
	{
		// 归「双平台」那一组：还是播放条那一块，但说的是**播放键那一枚的形状**
		//（issue 217 / BMG-129，票 B）。
		Category: Platform,
		// issue 217：播放键改成**上游那一枚**——48×48 的盒子、**底下什么都没有**、
		// 图形是**前景色**（深色下白、浅色下近黑），悬停时图形变主色。
		//
		// 🔴 **判据全是画面上的像素**：改之前那一枚是「一块 58×58 的实心主色
		// 圆角方块，里面一枚深色的圆」，改之后是「底色上的一枚圆」——**明暗正好
		// 反过来**，而「还是那枚键」这件事在代码里看不出来（签名一个字没改）。
		//
		// ⚠️ **「正在播放」那一档也要对**（形状换成 `RiPauseCircleFill`）：两档
		// 都断，「只改了播放那一档」这种坏法就抓得住。
		Spec: "播放键是上游那一枚：48×48 的盒子、图形正好填满、底下没有实色、图形是前景色（不是主色）、悬停时图形变主色；「正在播放」那一档一样",
		Tests: []string{
			// 盒子是 48×48、图形本身够大、图形落在盒子中间。
			"TestThePlayKeyIsBigEnoughToHit",
			// 底下没有压着实色、图形是前景色（深浅两套 × 播放/暂停两档）。
			"TestThePlayKeyHasNoSolidBackground",
			// 悬停时图形变主色（上游的 `hover:text-primary`）。
			"TestThePlayKeyTurnsAccentOnHover",
		},
	},
	{
		// 归「双平台」那一组：还是播放条那一块，但说的是**左边那一块的结构**
		//（issue 219 / BMG-131）。
		Category: Platform,
		// issue 219：播放条左边改成上游那**三段**——封面 │ 歌名·UP名 │
		// 分集·收藏·赞（**横着并排**的一行）；UP 名**左对齐**；第二行与歌名
		// 之间的空隙是上游那个（`space-y-1` = 4）；三枚键的图标是 **18**。
		//
		// 🔴 **判据全是画面上的位置和尺寸**：改之前三枚键在**歌名下面那一行**，
		// 而那也是一个「三枚键」的样子——键还是那三枚、也还点得动，**界面上
		// 看不出坏了**，只是和上游不像（上游那三枚是封面右边**横着并排**的）。
		//
		// ⚠️ **UP 名那一格原来也是「看不出坏了」**：它是一块 `Grow(1)` 的按钮，
		// 而框架的按钮内容**默认居中**——那句话飘到了那一段的中间（实测歌名在
		// x=80、UP 名在 x=219），而上游是 `items-start` 的普通 `span`。
		Spec: "播放条左边是三段：封面 │ 歌名·UP名 │ 分集·收藏·赞（横着并排的一行）；UP 名和歌名左对齐；第二行与歌名之间的空隙是 4；三枚键的图标是 18；音质徽章是短版（它挤在歌名右边，被摊扁就点不准了）",
		Tests: []string{
			// 三段横着摆（三枚键在文字列**右边**、在同一条水平线上、次序是
			// 分集 → 收藏 → 赞）。
			"TestTheLeftBlockIsThreeSegments",
			// UP 名左对齐（和歌名同一个左沿）。
			"TestTheOwnerNameIsLeftAlignedWithTheTitle",
			// 第二行与歌名之间的空隙是 4（上游的 `space-y-1`）。
			"TestTheGapBetweenTheTitleAndTheOwnerName",
			// 三枚键的图标是 18（画面上的墨迹大小）。
			"TestTheThreeLeftKeysUseTheUpstreamIconSize",
			// 音质徽章**够短**（它是控件：挤窄它就是「点不准」和「读不出来」）。
			"TestTheQualityChipIsShortEnoughToSitNextToTheTitle",
			// 分集那一枚出现/消失的时候，收藏和赞**不挪位置**。
			"TestTheLeftKeysStayPutWhenThePartsKeyAppears",
		},
	},
	{
		// 归「双平台」那一组：还是播放条那一块，但说的是它上面的**状态**和
		// 几样杂项（issue 222 / BMG-134）。
		Category: Platform,
		// issue 222：播放条的状态与杂项（BMG-124 报告 §5.2 §5.3 §4.2 §3.12
		// §6.5）。
		//
		// 🔴 **这一族全是「静默」的坏法**：没歌时多摆一个封面占位、加载中的
		// 封面是一块和背景几乎一样的纯色、超一小时的时长写成「75:00」、本地
		// 文件照样摆着「下载」、歌名点不动——**没有一样会报错**，界面只是
		// 「不太对」，而其中几样（加载中那一档、时间读数）用户得盯着看才
		// 说得清哪里不对。
		//
		// ⚠️ **「歌名可点」那一族断的是判据的每一半**：真实状态里本地文件
		// 压根没有 B 站地址，于是「点不动」可以是「没有地址」造成的——只断
		// 真实状态的话，**把本地那一半判据删掉照样全绿**。所以有两条：一条
		// 走画面（喂「本地文件 + 一个地址」这个人工组合），一条表驱动地断
		// 四种组合。
		Spec: "播放条的状态与杂项：没歌时左边那一块不画（不摆封面占位）；封面那一格的三档分得开（没有地址=描边框+音符、正在加载=无描边框+更淡的音符、拉失败=描边框+图片图标）；进度读数超一小时写成 hh:mm:ss、读不到写 -:--（一个减号，两处的写法一致）；没封面时是音乐符号；本地文件不摆「下载」；歌名可点（跳 B 站视频页，悬停有下划线）、本地文件不跳",
		Tests: []string{
			// 没歌时左边那一块没有封面（画面上那一块是干净的底色）。
			"TestNoTrackMeansNoCoverPlaceholder",
			// 没封面时那一格要的是**音符**（AST：像素上分不出是哪一枚图形）。
			"TestThePlayBarAsksForTheMusicNote",
			// 没封面时那一格画得出东西（有图形 + 有描边框）。
			"TestThePlayBarCoverWithoutOneShowsSomething",
			// 加载中那一格**画得出东西**、且**没有描边框**（那是「拿不到」的标记）。
			"TestPlayBarCoverWhileLoadingShowsThePlaceholder",
			// 超一小时写成 hh:mm:ss（画面上找得到那个读数）。
			"TestTheTrackTimeTellsTheHourWhenItIsPastAnHour",
			// 读不到写 -:--（两个读数都是）。
			"TestTheTrackTimeReadsDashWhenThereIsNone",
			// 播放条和历史/稍后看那两处写的是同一种时长。
			"TestTheTwoDurationWritersAgree",
			// 本地文件不摆「下载」、B 站那一首摆。
			"TestTheDownloadKeyIsNotThereForALocalFile",
			// **真的**本地文件那一档：播放条上没有「下载」。
			"TestTheHitAreaIsBiggerThanTheGlyph",
			// 点歌名跳的是**那个地址**（不是「按下去没报错」）。
			"TestClickingTheTitleOpensTheBiliPage",
			// 本地文件的歌名点了什么也不发生（喂的是「本地文件 + 一个地址」）。
			"TestTheTitleIsNotClickableForALocalFile",
			// 判据的三个条件各断一遍（表驱动）。
			"TestTheTitleClickRule",
			// 悬停时歌名底下长出一道下划线。
			"TestHoveringTheTitleUnderlinesIt",
			// 快照里的来源和正在放的那一首对得上（界面以前判不出来源）。
			"TestTheSnapshotTellsTheSource",
		},
	},
	{
		// 归「双平台」那一组，和上面 issue 127 那一条是同一件事的两半：那一票动的是
		// 外壳（侧栏 / 顶栏 / 播放条），这一票动的是**内容那一块**（列表与网格）。
		Category: Platform,
		// issue 134 的第二单元（列表与网格）：表头、列表行、回顶。
		//
		// 🔴 **判据全是画面上的像素、位置和真的菜单**：这一票的四个症状**全都是
		// 静默的**——表头底下没有线、列名比行里的字还弱、行里没有「⋯」、页面滚到
		// 底没有回顶，这四样没有一样会报错，界面只是「不太对」。
		//
		// 「⋯」那一条钉的是**两条路给的是同一批动作**（行内的和右键的）：写两份菜单
		// 的话，两边迟早会对不上，而那种错在界面上看不出来（两边都能点，只是点的
		// 不是同一件事）。
		Spec: "列表与网格：表头底下有一条线且列名的字号不比行里的字小；每一行末尾有一枚「⋯」，点开它和右键给出的是同一批动作；列表行的歌名只占一行、行高是定死的一个值；每一页滚到底都有「回到顶部」",
		Tests: []string{
			// 表头：底下那条线（**逐像素采样**）+ 列名的字号。
			"TestTheHeaderHasALineUnderIt",
			"TestTheHeaderColumnNamesAreNoSmallerThanTheRowsText",
			// 行内那枚「⋯」：两条路同一批动作、做的是**它那一行**的事、每一行都有。
			"TestTheOperationButtonOpensTheSameMenuAsTheRightClick",
			"TestTheOperationButtonActsOnTheSongItSitsOn",
			"TestEveryRowHasAnOperationButton",
			// 行高定死：歌名长也不变高（列表那一档 + 稍后再看/历史记录那两页）。
			"TestEveryRowIsTheSameHeightNoMatterHowLongTheTitle",
			"TestTheLibraryRowsAreTheSameHeightNoMatterHowLongTheTitle",
			// 回顶：首页 / 本地音乐 / 我的关注 / 下载记录 / 设置。
			"TestTheHomePageHasABackToTop",
			"TestTheLocalMusicPageHasABackToTop",
			"TestTheFollowPageHasABackToTop",
			"TestTheDownloadListHasABackToTop",
			"TestTheSettingsPageHasABackToTop",
			// 那两张表格的行高（回顶按钮换算像素时用的两个数）。
			"TestTheTableRowHeightsAreWhatTheOffsetsAssume",
		},
	},
	{
		Category: Platform,
		// 票 #167（#163 的第一批之一）：列表与网格的排版对齐 DESIGN.md。
		//
		// 这一票的错法**全是静默的**：字号小一档、列间距窄一半、表头「标题」比封面
		// 往右多让 64、紧凑档还画着一块封面——没有一样会报错，画面上只是「和上游不是
		// 同一个产品」。所以判据是量出来的位置、逐像素采样的中心和画出来的字谁高，
		// 不是「调了哪个常量」。
		Spec: "列表与网格的排版照 DESIGN.md：列表歌名 16 / UP 名 14 / 列间距 16 / 序号 32 居中；表头「标题」从封面左缘起（不多让一列封面空位）；网格卡歌名 16 / UP 14；紧凑档不画封面",
		Tests: []string{
			// 列表那一行的四个数 + 能直接量出来的两样：UP 名比时长高一档、
			// 序号那一块的墨迹落在这一列的正中（逐像素采样）。
			"TestTheRowScaleFollowsTheDesign",
			"TestTheRowTextIsBiggerThanTheNumbers",
			"TestTheRowIndexIsCentredInItsColumn",
			// 序号 / 列间距 / 封面三者摆出来的横坐标。
			"TestTheCoverSitsNextToTheTitle",
			// 表头：「标题」落在封面左缘（不是歌名的左缘），右边那几列照样对齐。
			"TestTheHeaderColumnsLineUpWithTheRows",
			// 网格卡：歌名 16 > UP 名 14 > 时长 12（画出来的三块字谁高）。
			"TestTheGridCardTextIs16And14",
			// 紧凑档：整块不画封面（位置 + hover 反馈都没了）。
			"TestTheCompactRowHasNoCover",
			"TestTheCompactRowsAreShorterThanTheNormalOnes",
		},
	},
	{
		// 归「双平台」那一组：侧栏是**窗口的骨架**（理由同 issue 127 那一条）。
		Category: Platform,
		// issue 137（#134 的第三单元）那一票：折叠态的侧栏 + 收藏夹名。
		//
		// 🔴 **判据全是画面上的东西**：提示那一格要**悬停 0.6 秒**才出，
		// 收藏夹名那一格要**量高度**。两者都是静默的——名字换成单字不会报错，
		// 名字换行也不会报错，只有提示和高度分得出来。
		Spec: "侧栏折叠后每一项悬停说出自己是什么（提示贴在那一项右边）且有可及的名字；收藏夹名单行 + 省略号，长名字不换行也不把下面那一行顶下去",
		Tests: []string{
			"TestTheCollapsedSidebarItemSaysItsName",
			"TestTheCollapsedSidebarItemHasItsFullName",
			"TestTheCollapsedSidebarTipSitsToTheRightOfTheItem",
			"TestALongFavoriteNameDoesNotMakeTheRowTaller",
		},
	},
	{
		// 归「双平台」那一组：这一票动的是**侧栏本身**（图标 / 行高 / 品牌），
		// 理由同上面那一条。
		Category: Platform,
		// issue 164（#163 的批次 1）那一票：菜单项的图标、行高与左上角的品牌。
		//
		// 🔴 **判据全是画面上的东西**：图标有没有画、选中那一项换没换实心、
		// 那一行多高、折叠态那一格是不是 40×40 的图标格、左上角是不是
		// 40×40 的 logo 加 24px 的「BiuGo」——**没有一条会报错**：图标没画
		// 出来，那一项只是「比别人少点东西」；行高不对，只是「矮了一点」。
		//
		// ⚠️ **这一票还把图标键的命中区从 28 改成了上游那一档的 32**，而命中区
		// 是**全局默认**（播放条、顶栏、队列抽屉头、收藏夹行尾、下载表那一列共用
		// 那两个数）。所以下面那几条也守着它涨上去之后的样子——尤其是
		// `TestTheSideMenuRowsAreFortyHigh`（行高 40 是「图标格 40 + 命中区
		// 涨上去」之后仍然成立的那一档）。
		Spec: "侧栏菜单每一项前面有一枚 18px 的线框图标（选中那一项换实心的）、菜单行高 40、折叠态每一项是 40×40 的图标格且悬停说出全名、左上角是 40×40 的 logo 加 24px 主色粗体的应用名",
		Tests: []string{
			"TestEverySideMenuEntryHasAnIcon",
			"TestTheSelectedSideMenuEntryDrawsTheFilledIcon",
			"TestTheSideMenuRowsAreFortyHigh",
			"TestTheCollapsedSideMenuItemIsAFortySquareIconCell",
			"TestTheBrandIsALogoAndTheAppName",
			// 路由表里那两枚图标逐条照上游的源码（表里写的是上游的字面文件名）。
			"TestTheSideMenuIconsMatchBiu",
		},
	},
	{
		// 归「双平台」那一组：这一票动的是**六处的动作键**，而六处画得不一样就
		// 是「用户从播放条走进满屏播放器，控件库换了」。理由同 #134 第二单元那一条：
		// 判据全在画面上。
		Category: Platform,
		// issue 141（#134 的第七单元）那一票：铺开扁平按钮。
		//
		// 🔴 **判据是像素**：满屏播放器 / 迷你播放器 / 下载记录 / 本地音乐 /
		// 收藏夹 / 动态卡片这六处里的动作键，不再是 `ui.Button` 那枚「一圈 1px
		// 描边 + 一块常驻底色」的药丸——换成它一样编译得过、点得动、无障碍名字
		// 也还叫得出来，只有画面上多出那两样。反过来也断：对话框里的确定/取消、
		// 空状态和错误页上的重试**仍然是**真按钮（主次分明，票面点名要保）。
		//
		// 另外两样也在这一票里：顶栏那枚「迷你」从文字键换成 `iconPiP`；
		// 队列抽屉和分集抽屉那两条手搓的分割线换成框架的 `ui.Divider`。
		Spec: "那六处里没有带描边、带常驻底的按钮（图标键框内框外同色，文字键上下就是页面底色）；对话框里的确定/取消和错误页的重试仍然带描边和常驻底；顶栏的「迷你」是一枚图标键；两处抽屉头上那条 1 像素的分割线还在",
		Tests: []string{
			// 六处各一条（深浅两套都在里面）。
			"TestTheFullPlayerKeysAreFlat",
			"TestTheFullPlayerPartsKeysAreFlat",
			"TestTheMiniPlayerKeysAreFlat",
			"TestTheDownloadListKeysAreFlat",
			"TestTheLocalMusicKeysAreFlat",
			"TestTheCollectionKeysAreFlat",
			"TestTheDynamicCardKeysAreFlat",
			// 反面：该保留描边的那一批。
			"TestTheDialogButtonsStayOutlined",
			"TestTheEmptyStateRetryStaysOutlined",
			// 顶栏那枚「迷你」。
			"TestTheNavBarMiniKeyIsAnIcon",
			// 迷你那四个键**是图标键**（不是被挤扁的文字键）。
			"TestTheMiniKeysAreIcons",
			// 两处手搓的分割线换成了框架的 `ui.Divider`，画出来还得是那一条线。
			"TestTheTwoRulesAreDrawnAsOnePixelLines",
		},
	},
	{
		// 归「分页与路由」那一组：按歌名找是**接口**筛的（词发给
		// `/x/v3/fav/resource/list`），而列表是按那个词重新拉的。
		Category: Paging,
		// issue 137（#134 的第三单元）那一票：收藏夹页那个按歌名找的输入框。
		//
		// ⚠️ **请求和画面各断一半**：请求带了词而界面没变，是界面没做；
		// 界面变了而请求没带词，是这一层没做。合成一条的话，两种坏里坏一种
		// 也能过。
		Spec: "收藏夹页有一个按歌名找的输入框，和排序并排；输入能筛出歌（筛是接口筛的，回车立刻搜、否则防抖 500ms），搜不出东西时有「换个词再搜一次」而不是一块空白；最小窗口下**每一个侧栏宽度（160~480）**上排序下拉都是完整的、且不出窗口（挤不下的时候让位的是输入框）",
		Tests: []string{
			"TestTheCollectionPageHasASearchBox",
			"TestTheCollectionSearchFiltersTheSongs",
			"TestTheCollectionSearchReachesTheInterface",
			"TestTheCollectionSearchGoesOutOnEnter",
			"TestTheCollectionSearchSurvivesLeavingThePage",
			"TestTheCollectionSearchSaysWhenThereIsNoResult",
			"TestTheCollectionOpsRowStillFitsAtTheMinWindow",
		},
	},
	{
		// 归「双平台」那一组：这一票动的是**页面的节奏**（边距、标题、Tab、加载态）。
		//
		// 🔴 **这一族每一个症状都是静默的**：七页的横向页边距是四种值、页标题是
		// 两种字号、「加载中」和「没有结果」不是同一个格子——没有一样会报错，
		// 界面只是「不太对」。所以判据全是**画面上的位置和尺寸**。
		Category: Platform,
		// issue 138（#134 的第四单元）那一票：页面节奏与加载态。
		//
		// ⚠️ **「加载中」那一档在票 #178 换过画法**：它原来是一行 15px 的淡字
		// （和「没有结果」同一个出口、同样的大小），现在是一枚**居中的转圈**
		// （上游那几页的首屏，见 `internal/ui/loading.go` 的文件头）。所以这一条
		// 里关于加载态的两句都改了：不再是「同一个格子」，而是「同一枚转圈 +
		// 40vh 那一格」。
		Spec: "页面节奏：七页的横向页边距是同一个数、页标题是同一个字号（收藏夹那一页的大标题另算，是 30）；主 Tab 与次级排序不是同一个样子；搜索页**没有页内输入框**，当前关键词显示在**顶栏**那个框里（且切页回来不覆盖用户正在敲的字）；「加载中」在每一页上都是**同一枚居中的转圈**（32，落在 40vh 那一格的正中），而它和「没有结果」是**两档**（转圈 vs 插画 + 两行字）；没登录时那一页说「先登录」而不摆空列表；小字的行高是定好的；搜索联想浮层里命中的那几个字被标出来",
		Tests: []string{
			// 页边距（七页逐页量内容区最靠左的那一笔）+ 页标题（代码里同一个常量，
			// 画面上墨迹一样宽）。
			"TestEveryPageHasTheSameHorizontalMargin",
			"TestEveryPageTitleUsesTheSharedSize",
			"TestEveryPageTitleIsTheSameSizeOnScreen",
			// 收藏夹那一页的大标题：30，而且两行放得下 168 那个封面。
			"TestTheCollectionTitleIsTheBigOne",
			// 主 Tab 底下没有那条主色的横条，次级排序底下有。
			"TestTheMainTabsAndTheSortTabsAreNotTheSame",
			// 搜索页的输入框里有当前关键词。
			"TestTheSearchPageShowsTheCurrentKeyword",
			// 「加载中」走同一个出口：八个页面是**同一枚转圈**（同一个直径）。
			"TestEveryLoadingStateIsDrawnTheSameWay",
			// 那一枚转圈落在 40vh 那一格的正中（票 #178）。
			"TestTheLoadingSpinnerSitsInTheTopFortyPercent",
			// 每一页的首屏在拉的时候是居中的转圈（票 #178）。
			"TestEveryFirstScreenIsACenteredSpinner",
			// 「加载中」和「没有结果」是两档，不混（票 #178；改之前那一条叫
			// `TestTheLoadingStateLooksLikeTheEmptyState`，量的是「两行字一样高」，
			// 换掉加载态的画法之后它不再成立）。
			"TestTheLoadingStateIsNotTheEmptyState",
			// 没登录时那一页说「先登录」，不摆空列表。
			"TestThePagesThatNeedLoginSaySo",
			// 九处小字的行高是定好的。
			"TestTheSmallCopyHasAFixedLineHeight",
			// 本地音乐那一页的「正在扫描…」也走共享出口（那一页的加载态在画面
			// 上抓不住——扫硬盘一帧就完事——所以是代码那一层钉的）。
			"TestTheLocalMusicLoadingComesFromTheSharedDrawer",
			// 联想浮层里命中的那几个字被标出来。
			"TestTheSuggestTermsShowWhichCharsWereHit",
		},
	},
	{
		// 归「双平台」那一组：这一票动的是**三页的控件形态**（收藏夹头图区、
		// 下载记录页、本地音乐页），而这一族每一个症状都是**静默的**——字号小一
		// 档、控件该是图标键却是一排字、hover 反馈没有，**没有一样会报错**，
		// 界面只是「不太对」。所以判据全是**画面上的位置 / 尺寸 / 颜色**。
		Category: Platform,
		// 票 #175（#163 的第二批之一）：收藏夹 / 下载 / 本地音乐的排版与按钮形态。
		//
		// ⚠️ **这一票的错法全是「少了一档」那种**：头图区那三行字小一档、创建者
		// 那一行没有头像、搜索是一个常驻输入框而不是会展开的按钮、下载表是一张
		// 裸表格、文件名 hover 不长出外链、工具条是一排字、行 hover 时序号不变
		// 播放图标——每一件单看都「像个界面」，只有并排和上游比才看得出不对。
		Spec: "收藏夹头图区：标题下面那三行字都是 14、行与行之间是 16、创建者有一枚 24 的头像（四块左缘对齐；一行标题时那一列不越过封面的下缘）；操作条那枚主按钮是 40 高；「按歌名找」是一枚**会展开的按钮**（收起是图标键，展开是输入框，点别处收回去，切页回来不抢键盘焦点），且它在每一个侧栏宽度和系统字号缩放下都不压到「多选」上；下载记录的表有一张卡片底，文件名 hover 长出外链图标并能点开 B 站页面，筛选是横排的单选圆点；本地音乐的工具条是图标键，行 hover 时序号换成播放图标",
		Tests: []string{
			// 收藏夹头图区：字号（AST）、竖距与左缘（画面）、头像的大小。
			"TestTheCollectionHeaderCopyIsFourteen",
			"TestTheCollectionTitleIsTheBigOne",
			"TestTheCollectionHeaderStaysInsideTheCoverOnOneLine",
			"TestTheCollectionHeaderShowsTheCreatorAvatar",
			// 头像那个地址要**抓得到**（B 站给的是协议相对地址，不补协议就是
			// 一块永远抓不到图的空圆）。
			"TestTheOwnerFaceGetsItsProtocol",
			// 操作条：主按钮 40 高（像素）；搜索是会展开的按钮（收起 / 展开 /
			// 点别处收起 / 切页回来不抢焦点）；两个分组在每一个侧栏宽度和字号
			// 缩放下都不重叠。
			"TestThePlayAllButtonIsFortyHigh",
			"TestTheCollectionSearchIsACollapsedButton",
			"TestTheCollectionSearchDoesNotStealTheFocusWhenComingBack",
			"TestTheCollectionOpsRowStillFitsAtTheMinWindow",
			// 下载记录：卡片底（深浅两套逐像素采样）、文件名 hover 的外链图标与
			// 它点开的地址、筛选是横排单选圆点（像素）。
			"TestTheDownloadTableSitsOnACard",
			"TestHoveringAFileNameRevealsTheExternalLink",
			"TestTheFilterIsHorizontalRadioButtonsNow",
			// 本地音乐：工具条上的搜索是会展开的按钮；行 hover 时序号换成播放图标。
			"TestTheLocalSearchIsACollapsedButton",
			"TestHoveringALocalRowTurnsItsNumberIntoAPlayIcon",
		},
	},
	{
		// 归「双平台」那一组：这一票动的是**播放条自己的刻度**，而播放条是
		// **每一屏都在**的那一条（理由同 issue 127 那一条）。
		Category: Platform,
		// 票 #173（#163 的第二批之一）：播放条的字号与图标尺寸。
		//
		// 🔴 **判据全是画面上的尺寸**：这一族每一个症状都是**静默**的——字号小一
		// 档、字重重一档、图标小一圈，**没有一样会报错**，界面只是「跟那边不像」。
		// 所以每一条都量：文字盒高（字号）、墨迹的多少（字重）、图形的墨迹（图标）。
		//
		// ⚠️ **字重只能由代码钉住一半**：400 和 700 的盒高一样，只有笔画粗细不同
		// ——所以「哪一句用哪一档字重」由 AST 钉（不许出现 `Bold()`），而「画出来
		// 的确实不是粗的那一版」由墨迹钉（同一串字在同一个进程里按 400 / 700 各
		// 画一次，比谁的墨迹更接近）。两样都要，缺一样就有一类坏没人管。
		//
		// ⚠️ **最后一钉的是宽度账的前提**：右边那一组是「有歌」的那一档（没有歌时
		// 少一枚「下载」），而「倍速」那一枚贴字宽（随当前倍率变）——这两样都写
		// 在 `internal/ui/playbar.go` 的注释里，量出来的那张表也靠这一钉。
		Spec: "播放条：歌名 16/400、UP 名 14、进度条两头的读数 14、倍速 16；封面 hover 的箭头 32；上一首 / 下一首 32/22（图标 22）；右边那一组是「有歌」的那一档，而它不占中间那一列的宽度；左列撑出轨道的那一截上的键点得动",
		Tests: []string{
			// 四个字号（文字盒高折算，深浅两套）。
			"TestThePlayBarUsesTheBiuTypeScale",
			// 歌名的字重：AST（不许 `Bold()`）+ 墨迹（不是粗的那一版）。
			"TestThePlayBarTitleIsNotBold",
			// 封面 hover 的箭头（亮像素的多少）。
			"TestTheCoverHoverArrowIsBigEnough",
			// 上一首 / 下一首：命中区 32 + 图形 22（墨迹的多少）。
			"TestTheSkipKeysDrawBiggerGlyphs",
			// 宽度账的前提：有歌 / 没歌右边那一组差 40，而**进度条两档一样宽**
			//（那一组是绝对定位的，不占中间那一列——票 #216 之后）。
			"TestTheRightGroupDoesNotTakeWidthFromTheMiddleColumn",
			// 左列撑出轨道的那一截上的键（收藏 / 赞 / 音质徽章）**点得动**。
			"TestTheLeftColumnKeysAreNotCoveredByTheMiddleColumn",
		},
	},
	{
		// 归「双平台」那一组：这一票动的是**三处控件的形态**（确认框、日期
		// 范围、下载状态），而这一族的错法全是「长得不像上游」——系统原生那个
		// 弹窗、两个并排的日期输入框、绿色的进度条，**没有一样会报错**，
		// 界面只是「不是那个产品」。
		Category: Platform,
		// 票 #176（#163 的第二批之一）：弹窗 / 日期 / 下载状态。
		//
		// ⚠️ **判据全是画面上的东西**：图标是什么颜色、确认键是什么颜色、正文
		// 的行高（= 字号）、日历上是几个月、进度条涂的是哪一块蓝。
		Spec: "确认框是应用自绘的模态（按类型变色的警告图标 + 标题 + text-sm 正文 + light 取消 + 按类型变色的确认键）；日期范围是**双月**选择器（256 宽的触发键，弹出来是两个月并排，能挑起始和结束，没在筛的时候日历上什么都不亮）；下载进度条是**蓝**的（不是主色），出错的那一条是红的，完成那一格有一枚**绿勾**",
		Tests: []string{
			// 确认框：图标在不在、是不是按类型变色；确认键的颜色；正文的字号；
			// 取消是不是 light；**那个框在无障碍树里有没有名字**。
			"TestTheConfirmDialogHasAToneColoredWarningIcon",
			"TestTheConfirmButtonTakesTheToneColor",
			"TestTheConfirmBodyIsTextSmall",
			"TestTheCancelButtonIsLight",
			"TestTheConfirmDialogHasANameInTheAccessibilityTree",
			// 双月日期选择器：两个月、一起翻、挑过的那天被高亮、点更早的一天
			// 从头再挑、没在筛的时候什么都不亮、挑完真的换条件、**挑的那一段
			// 恰好等于默认摆着的那一天时不会被静默丢掉**、最窄窗口下两块
			// 都点得到。
			"TestTheRangePickerShowsTwoMonths",
			"TestTheTwoMonthsMoveTogether",
			"TestThePickedDayIsHighlighted",
			"TestPickingAnEarlierDayStartsOver",
			"TestNothingIsHighlightedWhileNothingIsFiltered",
			"TestTheRangePickerDrivesTheFilter",
			"TestAManualRangeThatEqualsTodayIsNotDropped",
			"TestTheDateRangeOpensItsCalendarAtTheMinimumWindowWidth",
			// 下载状态：条是蓝的（且不是主色）、出错的是红的、完成的有一枚绿勾。
			"TestTheDownloadBarIsBlueAndNotTheAccent",
			"TestTheFailedBarIsRed",
			"TestTheFinishedCellHasAGreenCheck",
		},
	},
	{
		// 归「双平台」那一组：这一票动的是**加载态的画法**（页面首屏那一枚转圈
		// 和按钮「在忙」那一档）。
		//
		// 🔴 **这一族的错法全是「静默」的**：少一枚转圈、转圈落在别的地方、
		// 按钮在忙的时候不转——**没有一样会报错**，界面只是「不太对」。所以判据
		// 全是**画面上的东西**：转圈在不在、多大、落在哪，以及按钮在忙的时候
		// 是不是真的在转、是不是真的灰了。
		Category: Platform,
		// 票 #178（#163 的第三批）：加载态铺开——居中 Spinner 与按钮 loading。
		//
		// ⚠️ **这一票把加载态从「一行 15px 的淡字」换成了「一枚居中的转圈」**
		// （上游那几页的首屏就是 `h-[40vh]` 那一格里居中的 `<Spinner>`），所以
		// 票 #138 那一条「加载中和没有结果是同一个格子」的判据**不再成立**——
		// 那一族现在是**两档**：转圈 vs 插画 + 两行字。
		Spec: "页面首屏在拉的时候是**一枚居中的转圈**（32 见方，落在 40vh 那一格的正中，横向在内容区的正中）；「加载中」和「没有结果」是两档不混（转圈 vs 插画 + 两行字）；按钮在忙的时候**转起来 + 灰下去**（不是只换 label），字留着不缩成小方块",
		Tests: []string{
			// 转圈落在 40vh 那一格的正中（离那一格的顶 20vh）。
			"TestTheLoadingSpinnerSitsInTheTopFortyPercent",
			// 每一页的首屏在拉的时候是居中的转圈（同一枚 32 见方）。
			"TestEveryFirstScreenIsACenteredSpinner",
			// 「加载中」和「没有结果」是两档，不混。
			"TestTheLoadingStateIsNotTheEmptyState",
			// 按钮在忙的时候：里面有转圈、它真的在转、按钮灰成主色的一半、字还在。
			"TestTheBusyButtonTurnsAndGreysOut",
		},
	},
	{
		// 归「双平台」那一组：这一票动的是**右下角那条提示**（它的颜色、尺寸
		// 和入场）。
		//
		// 🔴 **这一族的错法全是「静默」的**：失败也是绿的、卡片窄 36 像素、
		// 提示「啪」地一下出现——**没有一样会报错**，界面只是「不太对」。
		// 而「用户是看颜色才知道结果的」（规格 #163 的用户故事 74），所以
		// 颜色错这一件比别的都贵。
		Category: Platform,
		// 票 #180（#163 的第三批）：Toast 语义色与入场。
		//
		// ⚠️ **这一票把「所有提示都是主色绿」改成了按语义分色**（上游那 164
		// 处调用是按调用给 `color` 的：danger 84 / success 58 / warning 22），
		// 同时把尺寸与位置对齐上游（356 宽 / `p-3` / 14/500 / 抬 90 / 离右边
		// 4~8），并补上 250ms 的入场。
		Spec: "右下角那条提示**按语义分色**（成功绿 #17c964 / 失败红 #f31260 / 警告橙 #f5a524 / 普通主色，四种颜色互不相同，「下载失败」不再是绿的）；**尺寸与位置**照上游：宽 356、`p-3`（四周各 12）、字 14/500、抬过底部 90、离右边 4~8；**入场有 250ms 的一段**（淡入 + 从右边滑进来，中间态推钟验得到），「减少动态效果」时一次到位",
		Tests: []string{
			// 四种语义色：画出来是四个不同的颜色（深浅两套都跑）。
			"TestTheToastTakesTheToneColor",
			// 「下载失败」不再是绿的（成功绿 / 失败红，各是各的）。
			"TestAFailedDownloadIsNotGreen",
			// 入场有中间态（推钟验），走完之后是满色。
			"TestTheToastSlidesIn",
			// 「减少动态效果」时一次到位。
			"TestTheToastEnterHonoursReduceMotion",
			// 尺寸与位置：356 宽、p-3、14、离右边 4~8、抬过底部 90。
			"TestTheToastUsesTheUpstreamSize",
			// 字重 500（AST：字重在画面上量不到盒子）。
			"TestTheToastTextUsesTheMediumWeight",
			// 提示那句话没缺字（改写调用点时少打一个字符，用户看到的是半截话）。
			"TestTheToastMessagesKeepTheirBrackets",
		},
	},
	{
		// 归「双平台」那一组：这一票动的是**播放条上那些控件的颜色与反馈**
		// （悬停、圆角、倍速浮层、音量浮层、两枚图形的区别）。
		//
		// 🔴 **这一族的错法全是「静默」的**：图形悬停不变色、圆角差了两像素、
		// 当前档还是灰底绿字、音量浮层比上游小一号——**没有一样会报错**，
		// 界面只是「不太对」。
		Category: Platform,
		// 票 #220（报告 §3.3 §3.4 §3.5 §3.6 §3.7 §3.9 §3.10）：播放条上的
		// 颜色与反馈一批。
		//
		// ⚠️ **图标键悬停变主色只给播放条上那些**（满屏播放器那一行的底是压暗过
		// 的封面，主色当字色那一档压在上面等于看不见，见票 #217 的评审）。
		Spec: "播放条上的**图标键悬停时图形变主色**（不是只多一层淡底；禁用着的键不变）；**封面和它悬停那层黑罩的圆角是 6**、罩上的箭头是主色（全局圆角令牌不动）；**倍速浮层**：浮层 60、那七行 44（左右没有内边距，「0.75x」看得全）、当前那一档是实心主色底 + `t.AccentText` 的字；**音量浮层**：滑块 32×160、旋钮 16、百分比 12 号字；**「播放列表」和「分集」是两个不同的图形**",
		Tests: []string{
			// 悬停变主色（深浅两套），以及禁用的那一枚不变。
			"TestTheIconKeysTurnAccentOnHover",
			"TestTheDisabledIconKeysDoNotTurnAccent",
			// 封面那一块：圆角 6、箭头是主色。
			"TestTheCoverHoverOverlayIsAccentAndRoundedSix",
			"TestThePlayBarCoverIsRoundedSix",
			// 倍速浮层：60 / 44 / 实心当前档；「0.75x」看得全（既有那条）。
			"TestTheRateMenuIsSixtyWideWithASolidCurrentRow",
			"TestRateMenuLabelsAreNotTruncated",
			// 音量浮层的尺寸。
			"TestTheVolumePopoverMatchesTheUpstreamSizes",
			// 两枚入口不是同一张图。
			"TestThePlaylistKeyAndThePartKeyAreDifferentGlyphs",
		},
	},
	{
		// 归「双平台」那一组：这一票动的是**「当前在哪」的画法**、搜索页的减法、
		// 满屏顶栏那一组的构图。
		//
		// 🔴 **这一族的错法全是「静默」的**：左栏当前项是一块实心主色大块、齿轮
		// 浮在窗口中间、搜索页摆着第二个输入框——**没有一样会报错**，界面只是
		// 「不太对」。而「同一屏上两种画法」那一类，用户不用比较两个界面就能看出
		// 这个应用没做完。
		Category: Platform,
		// 票 #234（审计 F-01 / F-02 / F-03 / F-06 + UX B1 / B2）：视觉一致性第一批。
		Spec: "「当前在哪」在应用里只有**一种**画法：侧栏菜单当前项、侧栏收藏夹当前项、关注页分组当前项、动态页作者栏当前项、队列抽屉「正在播放」那一行，五处都是 flat（淡主色底 + 深色字 / 中性灰底 + 主色字），**没有实心主色大块**；搜索页**没有页内输入框**，也没有「搜索」那一行标题，顶上第一行就是「搜索【词】的结果」（24/400），当前搜的词显示在**顶栏**那个框里；满屏播放器顶栏的设置齿轮**紧跟在歌名后面**（上游那一组的构图），不是浮在窗口中间",
		Tests: []string{
			// 五处「当前在哪」是同一个配方（深浅两套都量）。
			"TestTheCurrentItemIsFlatEverywhere",
			// 搜索页的减法，以及顶栏那个框显示当前关键词。
			"TestTheSearchPageHasOneTitleAndNoSecondSearchBox",
			"TestTheSearchPageShowsTheCurrentKeyword",
			// 满屏顶栏那一组的构图（齿轮紧跟歌名）。
			"TestTheFullPlayerGearSitsRightAfterTheTitle",
		},
	},
}
