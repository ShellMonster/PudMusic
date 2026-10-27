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
		//
		// ⚠️ **而且认的是「此刻有声音的那一首」，不是「队列的指针停在哪一项上」**
		// （票 #279）：那一档画的是**跳动的波形**、还会被读屏软件念成「正在播放」，
		// 而指针停着而声音停着的那几档（暂停、停止、启动时恢复出来没起播、取流
		// 失败、换歌的取流窗口）以前都会把那一行标上——用户看到的是「历史记录
		// 第一行在跳，而我没有在播放它」。
		Spec: "「正在播放」按歌认（队列重排了也还对，同一首排两遍算一首），而且按**此刻有声音的那一首**认（指针停着而声音停着时不认）；「下一首播放」插在当前项后面而不是队尾",
		Tests: []string{
			"TestIsPlayingFindsTheSongWhereverTheQueuePutIt",
			"TestIsPlayingSurvivesTheQueueBeingRearranged",
			"TestIsPlayingIsAboutTheSongNotAboutThatCopy",
			"TestIsPlayingSaysNoWhenOnlyTheQueuePointsAtIt",
			"TestIsPlayingSaysNoWhilePaused",
			"TestIsPlayingFollowsTheSoundNotTheQueuePointer",
			"TestIsPlayingRecognisesTheSongThatIsReallyPlaying",
			"TestTheSameSongInTwoRowsIsOneHighlight",
			"TestTheRowThatIsReallyPlayingIsMarked",
			"TestTheHistoryPageDoesNotMarkASongThatIsNotPlaying",
			"TestTheLaterPageDoesNotMarkASongThatIsNotPlaying",
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
		// 🔴 **这一条的错法也是「静默」的**：暂停和停止在界面上**长得一模一样**
		//（都是「播放」那一枚按钮、进度条都停在原地），而顺序播放走到队尾时
		// 界面**一句话都没有**——用户分不出「这一串放完了」和「暂停在开头」。
		//
		// 而「暂停」和「停止」在播放器那一边落在**同一档**上（进度回到开头、
		// 不在放），所以**一栏判据不够**：得把「用户按了停止」和「队列自己走完
		// 了」分开说（票 #319）。
		Category: Play,
		Spec:     "界面上分得出「暂停」和「停止」，而顺序播放走到队尾要说出「这一串放完了」",
		Tests: []string{
			// 快照上的两栏（暂停 / 用户叫停 / 队列走完，三种各一条）。
			"TestPausingIsNotStopping",
			"TestStoppingIsNotTheQueueRunningOut",
			"TestTheQueueRunningOutSaysSo",
			// 播放条上那一句（画面上的字）。
			"TestThePlayBarSaysTheTrackIsStopped",
			"TestThePlayBarSaysTheQueueIsDone",
			"TestAPausedTrackIsNotSaidToBeStopped",
			// 抽屉里那一行：说的是「已停止」，而且不再高亮成「正在播放」。
			"TestTheQueueDrawerSaysTheTrackIsStopped",
			"TestTheStoppedRowIsNotHighlightedLikeAPlayingOne",
		},
	},
	{
		// 🔴 **这一条治的是「静默卡住」**（票 #352）：按过一次暂停再恢复之后，
		// 队列**不往下走**，而整窗画面上**一句话都没有**——既不是「已停止」，
		// 也不是「这一串放完了」。
		//
		// 根因在 `App.Toggle` 的「按回来」那一半：它没把 `wantPlay` 记回 true，
		// 而 `onTrackFinished` 拿它当「要不要往下走」的判据——于是那一行早退，
		// 队列停在那儿，`QueueFinished` 也永远出不来。
		//
		// 另一半是**两处说的话要一致**：队列走完时播放条说「这一串放完了」，
		// 而抽屉里当前那一行的副题原来写「已停止」——**同一屏上两句不同的话**。
		Category: Play,
		Spec:     "按过一次暂停再恢复之后队列照常往下走；队列走完时播放条和队列抽屉说的是同一句（「用户按了停止」和「队列自己走完了」分得开）",
		Tests: []string{
			// 接线（App 那一层）：暂停再恢复之后队列照常往下走，而且队尾那一句
			// 说得出来。
			"TestTogglingPauseDoesNotStallTheQueue",
			"TestTheQueueFinishesAfterAToggleSaysSo",
			// 对照组：快捷键那条路（`App.Pause` / `App.Play`）行为不变。
			"TestPauseAndPlayStillMoveTheQueueOn",
			// 反面：**还停着**的时候不许自己往下走（只钉前半条的话，把
			// `onTrackFinished` 的早退条件整个删掉也照样绿）。
			"TestAPausedSongStaysPutAfterAToggle",
			// 画面（UI 那一层）：抽屉里那一行分得清两种停住，且和播放条不矛盾。
			"TestTheQueueDrawerSaysTheQueueIsDone",
			"TestTheQueueDrawerStillSaysStoppedWhenTheUserStops",
			"TestThePlayBarAndTheDrawerDoNotContradictEachOther",
		},
	},
	{
		Category: Play,
		// 起播失败自动跳过（收藏夹里夹着失效稿子那一类）：**不可重试**的错
		// （稿件没了/没权限，重试一万次也一样）跳到下一首，**可重试**的错
		// （网络那类）留在原地给「重试」——判据就是 `bili.Recoverable` 那张表，
		// 两处共用，不另立一套。
		//
		// 🔴 全失效的收藏夹**只绕一圈**：循环/随机档的 `Advance` 永远给得出
		// 「下一首」，不数着会无限跳下去（`App.skipStreak`，放成一首就清零）。
		Spec: "起播失败（不可重试类）自动跳下一首；可重试类不跳；整张队列全失效只绕一圈就停",
		Tests: []string{
			"TestADeadSongInTheQueueIsSkippedAutomatically",
			"TestTheWholeQueueBeingDeadStopsAfterOneRound",
			"TestARetriableFailureDoesNotSkipToTheNextSong",
		},
	},
	{
		Category: Play,
		// 「跑测试不出声」：macOS 的输出接的是默认输出设备（扬声器），而测试里
		// 有一批真的会 `Play()` 的用例——不哑的话每跑一次 `go test`，喇叭就把
		// 假解码器的台阶波、flacdec 的测试音挨个放一遍。修法是播放链最后一级在
		// `testing.Testing()` 之下换哑的（`audio.muteDecoder`）；tap、增益、位置
		// 都在它上游，各族断言不受影响。
		//
		// 🔴 **哑级必须转发 Close**：MyGo 收尾解码链靠对最后一级做 `io.Closer`
		// 断言——不转发的话 extaf 的句柄全漏（`handles_test.go` 那两条盯着）。
		Spec:  "go test 之下播放链最后一级是哑的（测试不出声），且关资源的链条不断",
		Tests: []string{"TestTheLastStageIsMutedWhenTestsRun"},
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

	{
		// 满屏接上歌词之后（票 #236，UX 报告 F-08 + B3）。这一条盯的是**两枚
		// 开关不再是死的**：以前「显示歌词」和「歌词字体颜色」只改偏好里的值，
		// 画面上什么都不会变——而用户看到的是「按了没反应」，症状与「整个设置
		// 面板坏了」一模一样。
		//
		// 而「没有歌词」必须**说出口**：满屏开着、这一首没有歌词的时候，界面
		// 上一个字都不说的话，用户分不清是「这一首没有」还是「这一版没做」
		// （UX 报告 B3：「没有」和「没显示出来」在画面上长得一样）。
		Category: Play,
		Spec:     "满屏播放器接上歌词：「显示歌词」与「歌词字体颜色」两枚开关真的管画面；这一首没有歌词时是空状态（「暂无歌词」+ 一枚「找歌词」）而不是整屏空白；歌词让开分集面板，不被它从中间切断；关掉歌词时封面回到整窗居中；界面藏起来之后歌词还在；滚过去之后滚进视野的歌词看得见（渐隐按容器上下沿，不按离唱到哪一句多远）",
		Tests: []string{
			"TestTheFullPlayerShowsTheLyrics",
			"TestTurningOffTheLyricsHidesThem",
			"TestTheLyricsColorSwitchChangesTheLyrics",
			"TestTheFullPlayerSaysWhenThereAreNoLyrics",
			"TestTheLyricsStayClearOfThePartsPanel",
			"TestTheCoverIsCentredInTheWindowWhenTheLyricsAreOff",
			"TestTheLyricsStayWhenTheInterfaceHides",
			"TestAShortLyricDoesNotFadeItsLastLine",
			"TestTheLyricsStayVisibleWhenScrolled",
			"TestTheLyricsDoNotJumpWhenTheInterfaceHides",
			"TestTheLyricsTakeTheWholeWidthWhenTheCoverIsOff",
		},
	},
	{
		// 票 #194：右下角那三枚旋钮改成**上游那三枚的样子**。
		//
		// 改的是**形状**，不是位置，也不是功能：位置（右沿 1128、底沿 680、每枚
		// 32、中心间距 44）和三枚各自的动作一个字都没动。
		//
		// 而「图标化」最容易丢的是**名字**：钮上不再有「找歌词」三个字，可它
		// 恰恰是**没有歌词时**唯一摆着的那个键——名字（`Label`）没了，读屏软件
		// 念不出来，无头 Tester 也找不到了。所以这一条把「名字还在」和「当前值
		// 在浮层里」都钉住：值不常驻钮上（上游那两个钮上就只有一个图标），但
		// 展开浮层仍然看得见。
		Category: Play,
		Spec:     "右下角那三枚歌词旋钮是 ⌀32 的圆形图标钮（上游同一份组件挂两处：主窗口「正在播放」那一页与满屏那一屏）：圆里只有一枚图标（上游那三枚 Aa / 时钟 / T 形框）没有字；无障碍名与 Tester 名字都还在；点击行为不变（字号 / 偏移开浮层、找歌词开搜索弹窗）；当前值不常驻钮上、在浮层里看；位置与间距不变（右沿 1128、底沿 680、每枚 32、中心间距 44）",
		Tests: []string{
			"TestTheKnobIsARoundIconButtonWithTheValueInThePopover",
			"TestTheTwoKnobsAreThereOnlyWithLyrics",
			"TestTheFullPlayerHasTheThreeLyricKnobs",
			"TestTheFullPlayerKnobsSitAtTheBottomRightOfTheLyrics",
			"TestTheFullPlayerKnobsActuallyWork",
		},
	},
	{
		// 满屏那个偏好面板的三样（UX 报告 B4）：**有标题**（用户不用靠里面的
		// 开关名猜这是什么面板）、**能关**（除了点外面和 Esc，还有一枚看得见的
		// 「×」）、**和齿轮对齐**（面板的左沿对着齿轮的图形，不对着那枚键的
		// 点击区）。
		Category: Play,
		Spec:     "满屏偏好面板：有标题（一个字都不多一个字都不少地画着）、能关、面板左沿和齿轮对齐",
		Tests: []string{
			"TestTheFullSettingsPanelHasATitleAndCanBeClosed",
			"TestTheFullSettingsPanelLinesUpWithTheGear",
		},
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
		// B 站的 web 接口把文案 HTML 转义过再回（「A &amp; B」），网页端渲染时
		// 顺手解掉，原生端要在映射层自己解——不解的话用户看到的就是
		// 「林俊杰&amp;李健合唱…」。判据两层：helper 本身（标准实体、全角分号
		// 的那一支、无转义原样回）和映射点真的包了（推荐流直接喂纯映射函数）。
		Spec: "接口文案里的 HTML 实体在映射层解掉（标题、UP 名），没有转义的串原样回",
		Tests: []string{
			"TestDecodeEntitiesUnescapesTitleText",
			"TestTheRecommendFeedDecodesTitleEntities",
		},
	},
	{
		Category: HTTP,
		// 设置页那格代理的**消费端**（上游 Biu 的 #250 一类）：四种协议都要
		// 真的接到请求上——http/https/socks5 翻成代理地址，socks4 由隧道拨号
		// 接（http.Transport 不认这个 scheme）；「不使用代理」回落环境变量；
		// 选了协议没填主机要报错而不是安静直连；改设置下一条请求就走新路。
		// 回环（httptest 全在本机）永不代理。
		Spec: "设置页的代理真的被所有出网请求走：四种协议、none 回落环境变量、空主机报错、改了立刻生效",
		Tests: []string{
			"TestProxyForSettingsMapsTheFourProtocols",
			"TestNoProxyFollowsTheEnvironment",
			"TestAnEmptyProxyHostIsAnErrorNotSilence",
			"TestLoopbackRequestsAreNeverProxied",
			"TestTheProxySettingTakesEffectOnTheNextRequest",
			"TestTheSettingsMountFeedsTheProxySnapshot",
			"TestTheSocks4TunnelSpeaksTheProtocol",
			"TestTheSocks4TunnelSendsTheHostnameWhenItCannotResolve",
			"TestTheSocks4TunnelUsesAResolvedAddressWhenItCan",
			"TestTheSocks4ReplyCodeIsSpelledOut",
		},
	},
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
		// 🔴 **「压根不是 JSON」和真风控必须分得开**（票 #346）：真风控是
		// -352/-412（业务码，在 JSON 里面），而这一档是压根没拿到 JSON
		// （多半是风控挡的一页 HTML）。混成一处的话，被截断的响应会跟着风控
		// 一起被判成不可重试。
		Spec: "响应不是 JSON 被认出来，而且和真风控分得开",
		Tests: []string{
			"TestANonJSONResponseIsNotTheRiskControl",
			"TestATruncatedResponseIsNotTheRiskControl",
		},
	},
	{
		Category: HTTP,
		// 🔴 **评论接口那三个静默出错的坑**（票 #571）：
		//
		//	buvid3  请求头里一出现 buvid3，B 站就把评论列表砍到 3 条并宣称「到底了」
		//	w_rid   走的是**不带 WBI** 的那个接口，所以请求串里不该有签名
		//	ps      上限 30（超了 B 站默默截断，翻页从第二页起会漏掉一批）
		//
		// ⚠️ **buvid3 那一条要防的是两处**：「代码里没写 `Buvid: true`」**不够**
		// ——jar 里的 cookie 是在运输层之前加上的（`http.Client.send` 里先
		// `Jar.Cookies` 再 `RoundTrip`），而 jar 里确实会有 buvid3（登录时
		// B 站下发的那批 cookie 全被存了下来）。所以评论请求显式写
		// `NoBuvid: true`，在运输层把 buvid 那一族摘掉（**登录态照带**）。
		//
		// 还有「-404（对象不存在）」与「0 条评论」**必须说得不一样**：混成一句的
		// 话，用户看到「这个对象不在了」会以为歌没了，而歌好好地摆在那儿。
		//
		// 而 **-403 在评论这一处判成「可重试」**（全局那张表仍判它不可重试——
		// 那在别的接口上是对的，在评论接口上是错的）。
		//
		// 而 **评论区被关掉的码是 12002**（2026-10-27 真机回归，票 #592：所有
		// 评论接口一律回 12002，旧码 12061 也要认）——只认旧码的话这一档基本
		// 触发不到，界面上说不出「这个评论区被关掉了」。**只认这两个码**：别的码
		// 不许被吞进这一档（吞了就把真正的原因盖住了）。
		Spec: "评论接口：请求头里不带 buvid3（jar 里的也不带，登录态照带）、不签 WBI、ps 不超过 30；-404 与「0 条评论」说得不一样；-403 在评论这一处可重试；评论区被关掉的码（12002，旧码 12061 也认）说的是「评论区被关掉了」，别的码不被吞进去",
		Tests: []string{
			"TestCommentListNeverSendsBuvid",
			"TestCommentListNeverSendsTheBuvidThatLivesInTheJar",
			"TestTheLoginCookiesDoNotMakeTheCommentRequestCarryBuvid",
			"TestTheNoBuvidPathIsSafeToCallConcurrently",
			"TestCommentListDoesNotAskForBuvid",
			"TestCommentListIsNotSigned",
			"TestCommentListGoesToTheUnsignedEndpoint",
			"TestCommentListNeverAsksForMoreThanThirty",
			"TestCommentListSendsTheTypeAndOid",
			"TestCommentListReadsTheFieldsOffTheRealShape",
			"TestCommentListSaysTheObjectIsGoneWhenBiliSays404",
			"TestTheEmptyListAndTheMissingObjectSayDifferentThings",
			"TestTheClosedCommentSectionCodeIs12002",
			"TestTheOtherCommentCodesAreNotTheClosedSection",
			"TestCommentListReadsTheEmptyPage",
			"TestAnEmptyPageMidwayIsNotNoComments",
			"TestCommentTreatsForbiddenAsWorthRetrying",
			"TestIsUpperMarksOnlyTheUpAuthorsOwnComment",
			"TestUpReplyCommentsAreNotMarkedAsTheUpper",
			"TestAPictureWithAFractionalSizeDoesNotTakeTheWholePageDown",
		},
	},
	{
		Category: HTTP,
		// 🔴 **评论列表的翻页**（票 #591）：
		//
		//	游标    翻页走 `cursor.pagination_reply.next_offset` +
		//	        `pagination_str`，**不是 `cursor.next`**——后者在「评论」那一档
		//	        （`mode=1`）上带上去必挂（B 站回 `-400 min_score … must be <=
		//	        max_score …`，因为 `support_mode` 恒为 `[2, 3]`，不含 1）。
		//	三档    mode=3 / 1 / 2 **都能翻到第二页**，且每一页的内容**互不相同**
		//	        （拿 rpid 比）。
		//	同一份 「评论」（mode=1）与「最新评论」（mode=2）给的是**同一份内容**
		//	        （实测逐条相同，而 `cursor.name` 却是不同的）⇒ 界面上不能把它
		//	        摆成两个一模一样的档。
		//
		// ⚠️ **偏移量一个字都不能改**：改一个字符 B 站**不报错**，它回的是
		// **第一页**——于是「加载更多」按下去什么都不变。
		Spec: "评论列表的翻页：走 pagination_reply.next_offset + pagination_str（不是 cursor.next，那一个在「评论」档上必挂 -400）；三个排序档都能翻到第二页且每页内容互不相同；「评论」与「最新评论」是同一份内容，界面上不能摆成两个一样的档；偏移量原样带回去",
		Tests: []string{
			"TestAllThreeSortsReachTheSecondPage",
			"TestThePagingNeverSendsTheBrokenCursor",
			"TestTheCommentSortGivesTheSameContentAsTheLatestOne",
			"TestTheHotSortGivesItsOwnContent",
			"TestTheOffsetIsSentVerbatim",
			"TestTheSortsOnScreenAreNotTwoOfTheSame",
			"TestLoadMoreCarriesTheOffsetThroughTheCache",
		},
	},
	{
		Category: HTTP,
		// 🔴 **评论这一摊的应用层**（票 #572）：
		//
		//	oid    mv 那一路的 oid 是**数字 avid**（走 `ArchiveID` 换），
		//	       audio 那一路是 **auid**（`type=14`）。漏了换 avid ⇒ **静默
		//	       拿到空列表**（B 站把认不出来的 oid 判成「没有评论」）。
		//	防重入 界面每一帧都调 `Comments()`，每一帧都发一次请求的话 B 站
		//	       会以为我们在刷。
		//	退避  失败之后**要能再问**（不是一次失败就再也不问）。
		//	换歌  迟到的结果回来发现 oid 对不上就丢——不然刚换的歌上面挂着
		//	       上一首的评论。
		//	缓存  同一首歌第二次进是**瞬时的**（不发请求）；TTL 一小时；
		//	       键带 type（`1-12345` 和 `14-12345` 是两个文件）。
		//	时序  零评论的对象**也要落缓存**（不然第二次进又要问一遍）；
		//	       同一对象上有两趟时，**较早的那一趟不许盖掉较新的那一份**
		//	       （界面上的和盘上的都不许）。
		//	local 本地文件不摆入口，**一个请求都不发**。
		Spec: "评论这一摊（应用层）：mv 的 oid 是数字 avid、audio 的 oid 是 auid（type=14）；每帧调不会发第二个请求；失败之后能再问；换歌之后旧的结果作废；缓存命中是瞬时的、TTL 一小时、有刷新能绕过；零评论的对象也落缓存；同一对象上较早的那一趟不许盖掉较新的那一份（界面上的和盘上的）；本地文件不摆入口也不发请求；评论区被关掉时界面上说的是「这个评论区被 UP 主关掉了」，不是一句看不懂的错（票 #592）",
		Tests: []string{
			"TestTheMVCommentOIDIsTheNumericAVID",
			"TestTheAudioCommentOIDIsTheAUID",
			"TestCommentsAreNotAskedTwice",
			"TestCommentsAreAskedAgainAfterAFailure",
			"TestTheClosedSectionIsSaidInPlainWords",
			"TestTheCommentsOfThePreviousSongDoNotShowUp",
			"TestTheSecondVisitIsInstant",
			"TestTheAVIDIsRememberedAcrossRestarts",
			"TestTheRefreshBypassesTheCache",
			"TestALocalItemIsNotWorthAsking",
			"TestLoadMoreUsesTheCursor",
			"TestTheRepliesUseTheSameObject",
			"TestNoCommentsIsNotAFailure",
			"TestTheMVAsksAgainWhenTheAVIDCannotBeFetched",
			"TestTheKeyCarriesTheType",
			"TestTheTwoObjectsGetTwoFiles",
			"TestAnExpiredEntryIsAsGoodAsMissing",
			"TestAnObjectWithNoCommentsIsCached",
			"TestAnOlderRequestDoesNotOverwriteTheRefresh",
			"TestAnOlderRequestDoesNotOverwriteTheCache",
		},
	},
	{
		Category: HTTP,
		// 风控和限流**都不判成「可重试」**：界面上那枚「重试」是立刻再问一次
		// （没有退避），而这两样要等的是时间——判成可重试的话用户点一次撞一次
		// （票 #316 收窄了风控，票 #346 把限流收到同一档）。
		//
		// 而**截断的响应判成可重试**：它多半是网络抖了一下，再问一次有可能好。
		Spec:  "风控和限流都不可重试（立刻再问还是撞墙），截断的响应可重试",
		Tests: []string{"TestRiskControlIsNotWorthRetryingAgain"},
	},
	{
		Category: HTTP,
		// 「能不能重试」这张表是**全项目唯一的一处**（票 #346）：播放条上那枚
		// 「重试」和下载记录页上那枚是同一个动作，两处必须用同一张表。
		Spec:  "「可重试」的判据收在一处（表在那儿）",
		Tests: []string{"TestRecoverableTellsWhatIsWorthRetryingAgain"},
	},
	{
		Category: HTTP,
		Spec:     "取流超时会回来，不是挂着",
		Tests:    []string{"TestSlowServerIsCutOffByTheTimeout", "TestALateStarterDoesNotWaitForTheTimeout"},
	},
	{
		Category: HTTP,
		// 票 #478：「`moov` 盒在文件末尾」的那个容器层修法。
		//
		// 🔴 **这一族的错法全是「静默」的**：末尾那一段取错了偏移（解出来是
		// 噪声）、`moov` 在前面时还去取（白花一条请求）、不是 MP4 的文件被
		// 当成 MP4（把错的字节写进缓存）——**没有一样会报错**。
		//
		// ⚠️ **判据是「形状」不是「毫秒」**：主下载卡在 64 KB 而解码器开口了
		// （它拿到的是并行取回来的那个 `moov`），而「等了多久」那种判据在满载
		// 的机器上会飘（AGENTS.md §2）。
		Spec: "moov 在文件末尾时并行取回末尾那一段，解码不用等整个文件下完",
		Tests: []string{
			// 盒子走法（手造每一种形状）。
			"TestTheTopLevelWalkFindsTheTrailingMoov",
			"TestTheTopLevelWalkFindsTheMoovAtTheFront",
			"TestTheTopLevelWalkGivesUpOnTheOtherShapes",
			"TestTheTopLevelWalkHandlesTheSixtyFourBitSize",
			"TestTheTopLevelWalkOnTheRealFixtures",
			// 源那一层：末尾那一段是并行取回来的，而 `moov` 在前面时不取。
			"TestTheTrailingMoovIsFetchedInParallel",
			"TestTheTrailingMoovIsNotFetchedWhenItIsAlreadyAtTheFront",
			// 🔴 **盒子头在很后面时**：等必须是**阻塞**的——空转会把一个核占满
			// （CPU 那半边要 `getrusage`，所以那一条带 `unix` 标签）。
			"TestTheTailIsFetchedEvenWhenTheBoxHeaderIsFarAway",
			"TestTheTailWaitBlocksInsteadOfSpinning",
			// 整条链路：主下载没下完就能出声，而拿不到尾部时退回老样子。
			"TestTheDecoderOpensBeforeTheWholeFileArrives",
		},
	},
	{
		Category: HTTP,
		// 票 #478：「首次打开特有」——**什么缓存都还没有**那一项。
		//
		// WBI 签名密钥**只缓存在内存里**（两小时），所以**每一次冷启动，
		// 第一次取流都要多付一次 `nav` 往返**——而那正是用户抱怨的
		// 「首次打开取流慢」的那一刻。修法是启动时在后台先把它取回来。
		//
		// 🔴 **这一族有两条边**：服务层那一半（密钥取回来没有、缓存生效
		// 没有）和应用层那一半（**真的会去调它**、**不挡事**）。
		Spec: "冷启动时把 WBI 签名密钥预热起来（第一次取流就不用多付一次 nav 往返），预热失败不挡事",
		Tests: []string{
			"TestWarmWBIKeyFetchesTheKeyAheadOfTime",
			"TestWarmWBIKeyDoesNotSendASecondRequestWhenTheKeyIsThere",
			"TestWarmWBIKeySaysSoWhenItFails",
			"TestTheWarmUpActuallyHappens",
			"TestTheWarmUpDoesNotBlockTheCaller",
			"TestTheWarmUpSurvivesAFailingNav",
		},
	},
	{
		Category: HTTP,
		// 票 #478：冷启动那一段的分段计时。
		//
		// 🔴 **「慢」和「等人」必须分得开**：macOS 会在应用还没有窗口的时候弹
		// 一个系统授权框，用户要点一下才继续——那一段的时长是**用户点的**，
		// 不是我们慢。混进「冷启动一共多久」里，用户读到的是「应用慢」。
		Spec: "冷启动分了段（零件挂好 / 应用就绪 / 首帧 / 第一次取流），钥匙串那一段单独一栏并说清是「等人」",
		Tests: []string{
			"TestTheStartupIsSplitIntoSegments",
			"TestTheKeychainWaitIsNotCalledSlow",
			"TestTheStartupEndsWhenTheFirstTrackSettles",
			"TestTheStartupLineIsOnlyShownWhenThereIsNoTrack",
		},
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
		// 归「播放」那一组：还是顶栏，但说的是头像**下面那一层**（票 #363）。
		//
		// 归这一组是因为它是**界面上的一处交互**（和满屏偏好面板那一条同一类），
		// 而不是一个接口。
		Category: Play,
		// 这一层原来是**系统原生菜单**（`ui.Element.Menu`），这一票换成自绘的
		// 浮层：样式跟设计规范对得上（surface1 / radiusLg / shadow.Modal /
		// accentSoft），而系统菜单是系统外观；菜单项也要能带图标。
		//
		// 🔴 **换成自绘之后，系统菜单那四件好事一件都不能丢**（当初用系统菜单
		// 就是为了它们）：点外面关、Esc 关、键盘能上下走、选中项有反馈。
		//
		// ⚠️ **指针和键盘混着来是最难的那一种**：两者共用「当前是第几项」，而
		// 悬停每一帧都改它——不处理的话，键盘刚走的那一格会被指针改回去，
		// 「键盘能上下走」只剩「指针不在菜单项上」的时候才成立。
		Spec: "头像下拉是自绘浮层（不是系统菜单）：六项按上游的顺序、**按登录态隐藏**（已登录没有「登录」，未登录没有「个人资料」「退出登录」）、每一项带图标、锚在头像下面；点外面关、Esc 关、方向键走得动（指针停在菜单项上也走得动）、当前项有反馈；「退出登录」先问一句，取消什么都不做",
		Tests: []string{
			// 六项都在、顺序照上游、**按登录态隐藏**、而且**不是系统菜单**。
			"TestAvatarMenuHasBiuSixItems",
			// 六项都按得动：换路由、去个人资料、去设置、退登（先问一句）。
			"TestAvatarMenuGoesToItsRoute",
			"TestAvatarMenuProfileUsesTheLoggedInMid",
			"TestAvatarMenuWithoutALoginSaysSo",
			"TestAvatarMenuLogoutActuallyLogsOut",
			"TestAvatarMenuOpensTheIssuePage",
			// 点外面关、Esc 关。
			"TestTheAvatarMenuClosesOnEscape",
			"TestTheAvatarMenuClosesWhenClickingOutside",
			// 键盘那一半：方向键走得动（**指针停在菜单项上时也走得动**）、
			// 回车选的是亮着的那一项。
			"TestTheAvatarMenuWalksWithTheArrowKeys",
			"TestTheAvatarMenuWalksWhileThePointerRestsOnAnItem",
			"TestTheAvatarMenuSelectsWithTheKeyboard",
			// 浮层的样子：锚在头像下面、右沿对齐、底色是 surface1 那一档。
			"TestTheAvatarMenuHangsUnderTheAvatar",
			"TestTheAvatarMenuPanelLooksLikeACard",
			// 每一项前面都有图标。
			"TestEveryAvatarMenuItemHasAnIcon",
			// 当前那一项的危险字按那层底现算（底换了，字要跟着换）。
			"TestTheDangerItemKeepsItsContrastOnTheHighlightedRow",
			// 「退出登录」先问一句，取消什么都不做。
			"TestTheAvatarMenuAsksBeforeLoggingOut",
		},
	},
	{
		// 顶栏**右端**那一块（票 #260）：用户 2026-10-25 说的那一段。
		//
		// 归这一组是因为它和上面那一条是同一块地方——**头像下面那一层**和
		// **头像自己**，说的是同一件事：登录之后那一枚独立的「退出」按钮没了，
		// 而头像本身是上游那个尺寸。
		Category: Play,
		// 顶栏右端：**登录之后没有独立的「退出」按钮**（退登走头像下拉里那一
		// 项），未登录时那枚「登录」还在（登录入口不能没）；头像画出来的那一枚
		// 圆是**上游的 40**（顶栏 64 高的 5/8），命中区装得下它，而顶栏另外
		// 那几枚图标键**没被这一票改到**。
		Spec: "顶栏右端：登录之后没有独立的「退出」按钮（退登走头像下拉），未登录时顶栏右端只有头像（登录走头像下拉，没有独立的二维码键）；头像画出来的那一枚圆是上游的 40，命中区装得下它",
		Tests: []string{
			"TestTheLoggedInTopBarHasNoStandaloneLogoutButton",
			"TestTheGuestTopBarHasNoSeparateLoginKey",
			"TestTheAvatarHitAreaCoversTheCircleItDraws",
			// 画出来的那一枚圆**量出来**是上游那个尺寸（不是照 `size="sm"` 猜的）。
			"TestTheTopBarEndsWithTheUpstreamSizedAvatar",
			// 而顶栏另外那几枚图标键**没被这一票改到**。
			"TestTheOtherTopBarKeysKeptTheirHitArea",
			// 下拉里那一项**点得动**（面板的身份钉死了，见 navbar_avatar.go 的
			// `c.Key("avatar-menu-panel")`）。
			"TestTheAvatarMenuItemIsAlwaysClickable",
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
	{
		Category: HTTP,
		// 票 #362 的三件事：
		//
		//  1. **自动拉**：登录之后、开机时已经登录，各拉一次——用户不该为了
		//     看见自己的收藏夹先去头像下拉里按「刷新数据」。而「一次自动拉
		//     只发一次请求」是「不许每帧都拉」那条规矩的反面判据。
		//  2. **空状态分得开**：「拉失败」和「真的一 个都没有」在画面上不是
		//     同一句话——摆一句「还没有收藏夹」在拉失败的时候是撒谎，而用户
		//     会据此以为自己一个收藏夹都没有。
		//  3. **「我收藏的」那一组有源**：它走 `collected/list`（B 站「我收藏
		//     了别人的哪些收藏夹」那个接口），而它以前在本项目里只有测试填
		//     过——侧栏上那一组永远空着，而用户看到的是一个摆在那儿、永远
		//     不动的分组。
		Spec: "登录之后 / 开机时自动拉一次收藏夹（一次自动拉只发一次请求）；拉失败和「真的没有」在界面上分得开；「我收藏的」那一组有源",
		Tests: []string{
			"TestLoggingInPullsTheFavorites",
			"TestAlreadyLoggedInPullsTheFavoritesAtStartup",
			"TestAutoRefreshSendsNothingWhenLoggedOut",
			"TestAutoRefreshSendsOnlyOneRequest",
			"TestAFailedFetchIsNotAnEmptyList",
			"TestAFetchThatFoundNothingIsStillLoaded",
			"TestTheCollectedFoldersShowUpInTheSidebar",
			"TestCollectedAlbumsAreNotPutInTheSidebar",
			"TestLoggingOutClearsTheFavorites",
			"TestARefreshThatComesBackAfterALogoutIsThrownAway",
			"TestTheOldRefreshDoesNotStealTheNewAccountsRefreshState",
			"TestTheSidebarSaysTheFetchFailedInsteadOfThereAreNoFavorites",
			"TestTheSidebarDoesNotSayThereAreNoFavoritesBeforeAnythingWasFetched",
			"TestRefreshingWithoutALoginDoesNotSayTheFetchFailed",
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
	{
		Category: PCM,
		// 上一条的另一面：**完整的**短文件不是「截断的输入」，不许报错。
		//
		// macOS 上 AudioToolbox 探容器时第一次就要 8192 字节，而整个文件
		// 不足 8192——字节源读到的是「没凑齐就到了头」，那不是失败，是
		// 「流到头了」。以前把它当成了硬错误，于是**总长不足 8192 字节**
		// 的文件（秒开片段、缓存碎片、测试素材）一个样本都解不出来，
		// 界面上是「这歌放不出来」（#312）。
		//
		// 这条证据是 macOS 专属的（`extaf` 只在 macOS 上存在）。
		Spec: "总长不足 8192 字节的**完整**文件也能解出来：样本数与时长都对、样本值对得上、读到头是干净的 EOF（末尾短读不算错）",
		Tests: []string{
			"TestAShortFileStillDecodes",
			"TestAShortFileKeepsItsSamples",
			"TestAShortFileEndsAtEOF",
			"TestAShortCompressedFileStillDecodes",
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
		// 🔴 **这一条钉的是「代码路径上没开着」**：框架 `Router.Transition` 的零值
		// 是 `TransitionAuto`（一条 250ms 滑入 / 150ms 淡入），不显式关掉就是开着，
		// 而 2026-10-25 实测证实**它一直开着**——现象只是「换页滑了一下」，
		// 不报错、不崩溃，评审和测试都看不出来。
		//
		// 那一组里还有一条**对照**（`TestThePageTransitionProbeWouldNoticeItIfItCameBack`），
		// 它故意把动画打开、要求探针**必须**量到差别——没有它，上面那几条有可能
		// 因为「探针根本没生效」而恒绿。
		Category: Paging,
		Spec:     "切路由是硬切：没有过渡动画（规范说不做，代码显式关掉）",
		Tests: []string{
			"TestTheRouterHasNoPageTransition",
			"TestThePageTransitionProbeWouldNoticeItIfItCameBack",
			"TestTheRouterTransitionIsSetExplicitly",
			"TestTheMiniWindowAlsoHasNoPageTransition",
		},
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
		// 首页那三张 Tab 的「无限下滑」（票 #370）。四件事缺一不可：
		//
		//   - **滚到底自动拉下一段**（一直滑一直有，到 B 站没有更多为止）；
		//   - **到头了说清楚**（「没有更多了」那一行——不是无限转圈、不是空白）；
		//   - **切回某档还在原来那一段**（这一条是换列表换出来的，位置记在哪
		//     可以换，但那个行为不能丢）；
		//   - **「回到顶部」那一枚回的是当前这一档**（三档各记各的）。
		Spec: "首页三张 Tab 无限下滑：滚到底自动拉下一段，到头说「没有更多了」；切回某档还在原处；回顶按钮回的是当前这一档",
		Tests: []string{
			"TestTheHomeKeepsLoadingMoreWhenYouScrollToTheEnd",
			"TestTheHomeSaysThereIsNoMoreWhenThereIsNoMore",
			"TestTheHomeBackToTopGoesBackToThatTab",
			"TestEachHomeTabKeepsItsOwnScrollAndItsOwnBackToTop",
			"TestSwitchingBackRestoresThatTabsScrollPosition",
		},
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
		// 设置那一格旁边的预览：三张缩略图各画各的形状，**三张都不带选中信号**
		// （票 #297：那一格里的选中信号只有药丸一个——预览再描一圈主色，同一格里
		// 就是两种「选中」信号了）。
		//
		// 判据是「描边里离主色更近」而不是「有几个主色像素」——边框是圆角加抗锯齿
		// 画出来的，逐字比颜色永远是 0 个（见那条测试的注释）。
		Spec: "「数据显示样式」旁边有预览：三张缩略图各画各的形状，而「现在是哪一档」只由药丸说（预览不带第二重选中信号）",
		Tests: []string{
			"TestTheDisplayStyleSettingShowsAPreviewOfEachStyle",
			"TestThePreviewCarriesNoSelectionSignal",
			"TestTheDisplayModeCellHasOnlyOneSelectedSignal",
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
		// ⚠️ **「菜单栏上真的有那一栏」只有真机量得到**：菜单是系统画的
		// （AppKit 的 NSMenu），它**不在窗口的画面里**，截图也截不到它——
		// 无头 Tester 量得到的是「我们交给框架的那棵树长什么样」，以及
		// 「点了之后有没有反应」（回调走的是我们自己的函数）。
		//
		// 真机那一份是：用辅助功能读菜单栏（项、顺序、每一项的快捷键），
		// 再真点一次「设置…」和「关于布丁音乐」。
		Spec: "macOS 应用菜单：「关于布丁音乐」开的是我们自己的面板（版本号、仓库、第三方许可），「设置…」在**主窗**开设置页（迷你开着也一样）",
		Tests: []string{
			"TestTheAppMenuColumnFollowsTheMacOrder",
			"TestTheAboutItemIsOurOwn",
			"TestTheSettingsItemHasTheMacShortcut",
			"TestTheOtherColumnsAreStillThere",
			"TestTheMenuHasFiveColumns",
			"TestTheAppIsWrittenInChinese",
			"TestTheSettingsMenuItemGoesToTheMainWindowEvenWithTheMiniInFront",
			"TestTheAboutMenuItemBringsTheMainWindowBack",
			"TestTheAboutPanelShowsTheNameTheVersionAndTheTwoEntries",
			"TestTheAboutPanelVersionIsTheSameAsTheSettingsPage",
			"TestTheAboutPanelRepoLinkOpensTheRepository",
			"TestTheAboutPanelLicenseEntryOpensTheShippedNotices",
			"TestTheAboutPanelClosesOnOK",
		},
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
		// Windows 上没有 AudioToolbox，`extaf.Open` 恒返回 `ErrUnsupported`。
		// 而「**平台没有解码器**」和「**这个文件解不了**」是两回事：只有前者
		// 才落回 `flacdec`（`openDecoder`），后者照旧报错——落回会把真错盖成
		// 「不像 FLAC」，而真错才是用户该看到的那句。
		//
		// ⚠️ 这一条是**注入**验的（`internal/service/audio/fallback_test.go` 把
		// 「平台解码器」换成测试自己给的）：真 Windows 上 `extaf` 恒不可用，
		// 而 macOS 上它恒可用——不注入的话，这条路径在**能跑测试的这个平台上**
		// 一条都走不到。
		Spec: "平台解码器不可用时落回 flacdec：MP4 里的 FLAC 仍能解出来（逐样本）；平台解码器可用时不落回；「平台没解码器」和「文件解不了」分得清",
		Tests: []string{
			"TestOpenDecoderFallsBackToFLACWhenThePlatformDecoderIsUnavailable",
			"TestThePlatformDecoderIsUsedWhenItIsAvailable",
			"TestOpenDecoderDoesNotFallBackWhenTheFileItselfIsBroken",
			"TestOpenDecoderSaysBothWhenThePlatformIsUnavailableAndTheFileIsNotFLAC",
		},
	},
	{
		Category: Platform,
		// Windows 上没有 AudioToolbox，**有损那一档（AAC）**靠的是 Media
		// Foundation（`internal/service/audio/mfadec`）。这一条盯的是接线：
		// `openDecoder` 在「平台解码器不可用」时问 Media Foundation，问成了
		// 就用它，问不成**才**落回 `flacdec`（MP4 里的 FLAC）。
		//
		// ⚠️ 这一条是**注入**验的（`internal/service/audio/fallback_test.go` 把
		// 「平台解码器」和「Media Foundation」都换成测试自己给的）：真 Windows
		// 上 Media Foundation 恒可用，macOS 上恒不可用——不注入的话，这条分支
		// 在**能跑测试的这个平台上**一条都走不到。
		//
		// 真机上那一条（逐样本比）是另一条证据（下面那条，RealOnly）。
		Spec: "平台解码器不可用时走 Media Foundation 解 AAC；平台解码器可用时不绕道；Media Foundation 认不出来时仍落回 flacdec",
		Tests: []string{
			"TestOpenDecoderUsesMediaFoundationWhenThePlatformDecoderIsUnavailable",
			"TestOpenDecoderDoesNotUseMediaFoundationWhenThePlatformDecoderIsAvailable",
			"TestOpenDecoderFallsBackToFLACWhenMediaFoundationCannotDecodeTheFile",
			// 认容器那一步：按头几个字节认，而且**不把 MP3 认成 ADTS**
			//（两者都以 FF 开头，只断同步位的话 MP3 也认得出来）。
			"TestTheContainerIsRecognizedByItsHeader",
			// **Media Foundation 的真实原因要进最终那句错**：Windows 上明明有
			// 系统解码器，只报「这台机器没有系统解码器」是句假话。
			"TestOpenDecoderSaysWhyMediaFoundationCouldNotDecodeIt",
		},
	},
	{
		Category: Platform,
		// 「B 站五档音质在两个平台上各能解几档」——这张表是**跑出来的**，
		// 每一格都真的解了一遍。
		//
		// ⚠️ 它在两个平台上跑出的是**两张不同的表**：macOS 走 AudioToolbox，
		// Windows 走 Media Foundation（FLAC 那一档落回 `flacdec`）。而 E-AC-3
		// 那一档在 Windows 上是**划掉**的（授权限制 + Win11 24H2 移除了系统
		// 自带的 AC-3 解码器），在 macOS 上 AudioToolbox 却解得了。
		Spec:  "B 站五档音质（64K / 128K / 192K / 杜比全景声 / Hi-Res 无损）在当前平台上逐档真解一遍，解不出来的把原因记下来",
		Tests: []string{"TestTheBilibiliQualityCoverage"},
	},
	{
		Category: Platform,
		// **产品这一层**解出来的 AAC 和 ffmpeg 的参考**逐样本**比（不是 RMS）。
		//
		// 这一层多做了三件事，每一件都可能把对的解码结果弄坏：认容器、截到
		// 容器说的呈现长度、丢掉编码器延迟。所以它要**单独**验一遍——spike 那
		// 几条验的是解码器本身，碰不到这三件事。
		//
		// ⚠️ 它在 macOS 和 Windows 上**各跑一遍**，走的是两个不同的解码器
		// （AudioToolbox / Media Foundation），比的是同一份参考。而两条线是
		// **不一样**的：Windows（Media Foundation）用票面定的 0.02，macOS
		// 用实测出来的 0.1（AudioToolbox 在同一份素材上是 0.070597）。理由写
		// 在 `internal/service/audio/aac_reference_test.go` 顶上。
		Spec: "产品这一层解出来的 AAC 与另一个解码器**逐样本**对得上（不是 RMS），帧数落在 176400~178176 之间，左右声道没调换；**两种容器都要比**（只比 .m4a 的话 ADTS 那一路解错了也会通过）",
		Tests: []string{
			"TestTheProductPathDecodesAACSampleForSample",
			"TestTheProductPathDecodesADTSSampleForSample",
			"TestTheProductPathKeepsTheChannelsInPlace",
			// ADTS 那一路**自己拼出来的** AudioSpecificConfig，必须和 MP4
			// 那一路从容器读到的**核心部分**一致（拼错了会变调）。
			"TestTheADTSPathDerivesTheSameAudioSpecificConfig",
		},
	},
	{
		Category: Platform,
		// 「开口到第一个采样 ≤ 200 毫秒」——**只量解码那一侧**，不含下载等待
		// （真网上 B 站的音频 60 秒才 1.2 MB，光下载就够这个数了）。
		//
		// 这一条量的是**产品这一层**：拆容器（go-m4a）、认容器、开解码器、解
		// 第一帧，全在里面。口径是五轮里**最快**的一次（和
		// `quality_test.go` 的 `TestAAC320FromTheStreamIsAudibleFast` 一样：
		// 测的是解码器的能力，不是调度），而最慢的那一次照样打出来。
		Spec: "产品这一层开口到第一个采样不超过 200 毫秒（不含下载等待）",
		Tests: []string{
			"TestTheFirstFrameComesOutFast",
		},
	},
	{
		Category: Platform,
		// **编码器延迟不能丢两遍**。AAC 的 MDCT 有重叠，解码器**自己吞掉**它
		// 输出的第一帧（那一帧是「重叠不完整」的冷启动帧 = 编码器的预热），
		// 所以解码器的输出**本来就从呈现音频的开头起**。而 MP4 的编辑列表里
		// 还写着同一个 `media_time = 1024`——那是给「不知道解码器会吞它」的
		// 解码器用的（AudioToolbox / ffmpeg 都照它剪）。
		//
		// 再按 `EncoderDelay` 丢一遍就是**丢了两遍**：凭空砍掉 1024 个真实
		// 采样（23 毫秒），比容器说的呈现长度少 272 个采样，**判据①会因此红**。
		Spec: "解码器的输出本来就从呈现音频开头起：产品层不再按 EncoderDelay 丢一遍",
		Tests: []string{
			"TestTheDecoderDoesNotDropTheEncoderDelayTwice",
		},
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
			// 静音在小窗里**看得出来**（票 #345）：滑块停在用户设的那一档
			// （静音是它自己的一格），所以能说明「现在是静音」的只有那枚图标。
			"TestTheMiniWindowShowsTheMuteState",
			"TestClickingTheMiniMuteIconUnmutes",
			"TestTheMiniMuteIconFollowsTheApp",
			"TestTheMiniMuteIconSaysItIsMuted",
			"TestTheMiniMuteIconMatchesThePlayBar",
			"TestTheMuteGlyphDiffersFromTheLoudOne",
			"TestTheMiniMuteStateSurvivesARestart",
			"TestTheMiniMuteIconIsThereWithoutATrack",
			"TestTheMiniVolumeTipSaysItIsMuted",
			"TestTheMiniMuteIconSaysCancelMuteWhenThereIsNoSound",
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
		Spec:     "迷你播放器这一块本身：装得下 320×84、那几枚键是图标键（框对、图形在）、控件身份稳（铁律四）",
		Tests: []string{
			"TestTheMiniCardFitsInThreeHundredAndTwentyByEightyFour",
			"TestTheMiniKeysAreIcons",
			"TestTheMiniControlsKeepTheirIdentityWhenTheLabelChanges",
		},
	},
	{
		Category: Platform,
		// 图标键**只有一档配方**：悬停时图形变主色（上游 HeroUI 的
		// `hover:text-primary`）。改之前顶栏那几枚、队列抽屉头那两枚、新歌速递的
		// 翻页箭头走的是「只有淡底、图形不变色」那一档——同一个「图标键」在应用
		// 里有两套画法，用户从一处移到另一处会觉得「这一枚不亮」。
		//
		// 顺带把队列抽屉行尾那枚「⋯」对齐到列表那一档：点击区 32×32（它原来
		// 是 44×28，把歌名那一列截短了 12px）、无障碍名带上歌名（一屏 N 枚都叫
		// 「这一首的操作」，读屏分不出是哪一行）。
		//
		// ⚠️ **满屏播放器那一行不在这一档里**：它的底是压暗过的封面，主色压在
		// 上面只有 1.0:1（等于看不见）——那是另一票的事，钉在
		// `TestTheFullPlayerRowKeepsItsOwnRecipe` 那一条上。
		Spec: "图标键统一成一档：顶栏那几枚、队列抽屉头那两枚、抽屉行尾那枚「⋯」、新歌速递的翻页箭头，悬停时图形变主色；「⋯」的点击区是 32×32、无障碍名带上歌名；满屏播放器那一行不在这一档里",
		Tests: []string{
			"TestTheTopBarIconKeysTurnAccentOnHover",
			"TestTheQueueDrawerHeaderKeysTurnAccentOnHover",
			"TestTheQueueRowOperationKeyTurnsAccentOnHover",
			"TestTheQueueRowOperationKeyIsNamedAfterItsSong",
			"TestTheQueueRowOperationKeyIsThirtyTwoSquare",
			"TestTheNewMusicPagerTurnsAccentOnHover",
			"TestTheTopBarKeysKeepTheirTipsAndNames",
			"TestTheFullPlayerRowKeepsItsOwnRecipe",
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
			"TestQuittingJustAsTheCheckComesBackIsQuiet",
			"TestThePrefsFileRoundTrips",
			"TestACorruptPrefsFileIsIgnoredNotFatal",
			"TestAPrefsFileFromANewerVersionIsLeftAlone",
			"TestNoUserDataDirMeansMemoryOnly",
		},
	},
	{
		Category: Negative,
		// 🔴 **两处界面的「重试」用同一张表**（票 #346）：播放条上那枚和下载
		// 记录页上那枚是**同一个动作**（立刻再问一次 B 站），所以风控和限流
		// 在两处都**不给**那枚键——按下去就是再撞一次墙。
		Spec: "「重试」键按同一张判据给：风控和限流两处都不给，被截断的响应给",
		Tests: []string{
			"TestThePlayBarDoesNotOfferARetryThatWouldHitTheRateLimitAgain",
			"TestARateLimitedFailureIsNotOfferedARetry",
			"TestARiskControlDownloadIsNotOfferedARetry",
			"TestAFailedDownloadThatCannotBeRetriedHasNoRetryButton",
			"TestATruncatedResponseIsOfferedARetry",
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
		// 票 #315：CDN 抖一下的时候三次重试是**瞬间打完**的（`recover` 里
		// 一个 sleep 都没有），用户只看到一句红字；而重连的那几秒里界面上
		// **一个字也没有**——「歌停着、界面不动、没有字」。
		//
		// 这一组盯三件事：**退避是真的**（重试之间隔了、随次数变长、关掉
		// 能打断）、**「正在重连」看得见**（它住在源那边、应用问的是**当前
		// 在管的那一条**——一条被丢掉的源抢不走它）、以及**失败有交代**
		// （第二次重开被挡掉时必须报错，不许一声不吭地亮着）。
		//
		// 🔴 **端到端那四条在 `internal/app/reconnect_e2e_test.go`**：手工调
		// 应用的内部函数证明不了「源→应用层→界面」这条接线。
		//
		// 票 #378 补了两条：**每一段流的状态各归各**（切歌之后旧那一首的
		// 重连状态不许留在新歌上——代次守卫），以及**「第 N 次」的 N 是
		// 「这一轮里的次数」**（一次断流恢复之后，下一轮从第 1 次重新数）。
		Spec: "断流重连：重试之间有退避（1 秒 / 2 秒 / 4 秒，总 ≤ 7 秒），重连期间界面上说「正在重连」，成功或失败都有明确结果",
		Tests: []string{
			"TestTheDefaultBackoffIsOneTwoFour",
			"TestARetryWaitsBeforeItRetries",
			"TestTheBackoffGrowsWithEveryRetry",
			"TestTheBackoffStopsAtItsLastValue",
			"TestTheSourceGivesUpAfterTheBackoffIsUsedUp",
			"TestCloseCancelsTheBackoffWait",
			"TestTheReconnectingFlagReachesTheSnapshot",
			"TestOnlyTheSourceTheAppCaresAboutCounts",
			"TestAStalePendingSourceDoesNotCount",
			"TestTheReopenFromThePlaybackPositionSaysSo",
			"TestClearingTheErrorDoesNotClearTheReconnecting",
			"TestTheSecondReopenSaysItGaveUp",
			"TestTheStreamBreakShowsUpAsReconnecting",
			"TestTheBreakDuringTheFirstLoadIsStillVisible",
			"TestAStaleSourceDoesNotStealTheReconnectState",
			"TestThePlayBarSaysItIsReconnecting",
			"TestTheErrorWinsOverTheReconnectingLine",
			"TestTheRetryNumberCountsThisRoundOnly",
			"TestTheOldSongsReconnectStateDoesNotStayOnTheNewSong",
			"TestTheOldSongsReconnectStateDoesNotStealTheNewOnesLine",
		},
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
		// 票 #282：「点了歌一直不开始、也不说为什么」。
		//
		// 那一首和别的歌不一样的地方在于**它的流一直没准备好**，而应用这一层
		// 对「还在准备」和「已经完了」分都不分：既没有一句「正在取流」，也没有
		// 一个「等够了就当它失败」的上限。于是用户面对的是一个不动的界面和
		// 一片沉默——比「报错」更糟。
		//
		// 这一组盯三件事：**有反馈**（取流时播放键上有那一圈光，票 #581 之后
		// 界面上不再有「正在取流」的文字）、**有上限**（超时按失败处理，
		// 给一句能看懂的话）、**有痕迹**（代次变了和失败都写进日志，
		// 而日志真的落在文件里）。
		Spec: "点了歌一直不开始：取流时播放键上有光圈、超过阈值按失败处理、代次变了和失败都写进日志",
		Tests: []string{
			"TestAPlayThatNeverStartsTurnsIntoASentence",
			"TestAnotherSongsFailureDoesNotCancelTheWatchdog",
			"TestTheDiscardedPlayIsWrittenDown",
			"TestTheFailureIsWrittenDown",
			"TestThePlayBarStaysQuietWhileLoading",
			"TestThePauseKeyIsStillThereWhileLoading",
			"TestAFailedPlayGivesASentenceInsteadOfASpinner",
			"TestTheFailureSentenceFitsOnOneLine",
			"TestFriendlySaysTheFormatCannotBeDecoded",
			"TestTheLogFileActuallyReceivesWhatIsLogged",
			"TestTheLogFileIsAppendedTo",
			// 票 #478：冷启动那一段界面上也有一句话（而「还没有打开文件」
			// 在那一段是**一句假话**——应用不是没有文件，它是在等上一首）。
			"TestTheStartupSaysItIsPreparing",
			"TestTheStartupLineGoesAwayWhenTheAppIsReady",
			"TestTheStartupLineIsNotShownWhenThereIsATrack",
		},
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
		// 音量三件套（票 #311）。这一组盯的是三件**静默**的事：
		//
		//  1. 音量不落盘 → 每次启动回到 100%。用户为了不吵把音量调到 20%，
		//     重启之后突然全速——耳机上那是真的惊吓。
		//  2. 静音还是「音量 0」的话，一旦开始落盘音量，「静音后退出再启动，
		//     音量永远是 0」就成了必然（Clementine 的老毛病）。所以静音是它
		//     自己的一格，而音量那一格记的是用户设的值。
		//  3. 「这份设置里没有音量这一格」（老版本写的文件）和「用户自己把音量
		//     拉到 0」分不开的话，升级上来的用户打开应用是**一点声都没有**，
		//     而界面上不会说任何一个字。
		//
		// 第四件是「音量不一步跳过去」：开始放 / 恢复时从 0 走到那一档（淡入），
		// 调音量和按静音时从当前值渐变到新值（渐变 / 淡出）。
		Spec: "音量：落盘记住（重启还是上次那一档）；静音是独立的一格（静音后退出再启动，音量不是 0）；开始放 / 恢复时淡入，调音量时渐变，不是瞬间跳",
		Tests: []string{
			// 落盘与读回。
			"TestTheVolumeSurvivesARestart",
			"TestMuteIsItsOwnFlag",
			// 「没有这一格」和「这一格是 0」分得开。
			"TestAnOldSettingsFileGetsTheDefaultVolume",
			"TestAVolumeOfZeroIsNotTheSameAsNoVolume",
			// 静音那一格自己的语义。
			"TestUnmutingRestoresTheVolume",
			"TestSettingANonZeroVolumeUnmutes",
			"TestDraggingTheVolumeToZeroIsMute",
			"TestAMutedPlayerStillKeepsItsVolume",
			"TestMutedAlwaysDrawsTheMuteIcon",
			// 淡入 / 渐变 / 淡出。
			"TestTheVolumeFadesInOnStart",
			"TestSettingTheVolumeRamps",
			"TestMutingFadesOut",
			"TestSettingTheVolumeRetargetsTheRamp",
			"TestCloseStopsTheRamp",
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
		// 票 #358 补了第三条：**画面上正画着的那一张也不许挤**。开队列抽屉的
		// 时候，抽屉里那二十几行第一次露出来、全都要封面，字节上限一下子超了，
		// 挤的循环一路挤到画面上正画着的那几张——挤掉的那一张下一帧就退回占位，
		// 于是「开抽屉时中间那一列的封面来回闪」。
		//
		// **同一件事的另一个表现是「历史记录页一直闪」**：那一页自己的一屏封面
		// 就把上限顶破了，于是挤 → 退占位 → 重新下 → 又被挤……一直循环。
		//
		// 全部用 `httptest` 验（假图床 + 假 B 站接口），不连真网。
		Spec: "封面不再在有图和占位之间反复切换：一屏封面按显示尺寸存得下、字节账不漏、还在下的那一张不参与淘汰、画面上正画着的那一张不参与淘汰（首页、历史记录、稍后再看三处）；普通大 PNG 逐行、基线 JPEG 按 MCU 行缩放，不分配整张原图且像素不变；仅 EXIF 旋转图保留框架方向处理；缓存创建和重置均遵循环境预算",
		Tests: []string{
			"TestTheSameCoverStaysReadyAcrossFrames",
			"TestTheHomeCoversDoNotGoBackToPlaceholder",
			"TestStoredCoversAreScaledDown",
			"TestStreamingPNGMatchesExistingPixels",
			"TestStreamingPNGFiltersAndChunks",
			"TestStreamingPNGRejectsCorruption",
			"TestStreamingPNGFallsBackForOtherImages",
			"TestStoredPNGDoesNotAllocateFullSizeImage",
			"TestStreamingJPEGMatchesExistingPixels",
			"TestStoredJPEGDoesNotAllocateFullSizeImage",
			"TestStreamingJPEGRejectsTruncation",
			"TestStreamingJPEGUnsupportedVariantsFallBack",
			"TestJPEGEXIFOnlyRotationsBypassScaling",
			"TestJPEGRowCorpusMatchesStandardDecoder",
			"TestImageCacheBudgetFromEnvironment",
			"TestAnEntryEvictedBeforeItIsDecodedIsNotAccounted",
			"TestTheEvictionSkipsEntriesThatHaveNoBytesYet",
			"TestACoverThatIsStillLoadingIsNotEvicted",
			"TestTheByteAccountDoesNotLeak",
			"TestTheCoverOnScreenIsNotEvicted",
			"TestACoverThatIsNoLongerOnScreenIsEvicted",
			"TestOpeningTheQueueDrawerDoesNotMakeTheListCoversFlash",
			"TestTheHistoryPageCoversDoNotKeepFlashing",
			"TestTheLaterPageCoversDoNotKeepFlashing",
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
			// 快退 / 快进那两条（`inAppOnlyShortcuts`）：键 → 分发 → seek，
			// 以及没有歌 / 越过末尾时的钳制。
			"TestTheSeekShortcutsMoveThePosition",
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
			// 本地音乐这一边的**另一半**：上面那几条钉的是「表上写着的和
			// 界面上说的一致」，这一条钉的是「表上写着可放的**真的放得出声**」。
			//
			// ⚠️ 少了它，「界面说可放、实际报不支持」没人管：`.ogg` 在扫描
			// 白名单里、也在「放不放得了」那张表里，而解码器从来没注册过——
			// 判据一路绿，用户点进去才撞上「不支持这个音频格式」。
			"TestOggVorbisReallyDecodes",
			"TestTheOggDurationIsProbedFromItsOwnContainer",
			// MP3 这一边：换了解码器之后，「放得了」这件事要重新验一遍——
			// 而且验的是**三件具体的事**，不是「注册表里有它」。
			//
			// ⚠️ 少了这几条，「MPEG-2.5 放不了」「编码器延迟留在歌的开头」
			// 「打开一个 MP3 要把整个文件读一遍」这三件事**都不会有人知道**：
			// 它们全都不报错，只是悄悄不对。
			"TestMPEG25ReallyDecodes",
			"TestTheLameGaplessTrimIsApplied",
			"TestTheGaplessDecodeStartsWhereTheSourceStarts",
			"TestOpeningAnMp3DoesNotReadTheWholeFile",
			"TestTheMp3DurationIsProbedFromItsOwnContainer",
			"TestTheMp3DecoderIsOurs",
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
		Category: HTTP,
		// B 站那一份歌词是**取流那一趟顺带回来的**（响应里的 `data.subtitle`）——
		// 以前 `playurlData` 只声明了三个字段，那一段被**静悄悄地**丢掉了，于是
		// 歌词只能靠用户在「找歌词」里搜（上游 Biu 走的就是这一段，
		// `get-lyrics.ts` 的 `getLyricsByBili`）。
		//
		// ⚠️ 判据是**解析**那几项：时间 `round(from*1000)` 毫秒、文字去掉首尾的
		// `♪♫`、空的丢掉、按时间排序。抄错时间那一项的症状是「歌词整体早/晚
		// 几秒」，而歌词本身看着完全正常——用户只会以为「这首歌的词对不上」。
		Spec: "B 站那一份歌词自己来：取流响应里的字幕地址被读出来、后台下载并按上游的判据解析、存进缓存（键与别的歌词缓存一致）、界面上说清它是自动来的",
		Tests: []string{
			"TestPlayURLHandsBackTheSubtitleItWasGiven",
			"TestTheSecondSubtitleAddressIsKeptToo",
			"TestTheBiliSubtitleIsParsedTheWayUpstreamDoes",
			"TestTheBiliSubtitleTimeIsRoundedToMilliseconds",
			"TestTheBiliSubtitleSurvivesTheRoundTripThroughLRC",
			"TestTheBiliSubtitleComesOnItsOwn",
			"TestTheBiliSubtitleSurvivesARestart",
			"TestTheSubtitleIsFetchedInTheBackground",
			"TestTheLyricsSayWhereTheyCameFrom",
			"TestTheOriginLineSitsWhereTheLyricsSit",
		},
	},
	{
		Category: Negative,
		// 🔴 **「取到了地址但下载 403」这一档**：B 站给同一份字幕两个地址，而
		// **不带鉴权参数的那个可能要登录态**——拿它去请求会 403。而登录态在字幕
		// 那条路上根本到不了（cookie 的域是 `.bilibili.com`，字幕文件在
		// `.hdslb.com`）。所以**唯一能授权这一趟的就是地址自己带的那段
		// `auth_key`**——也就是第二个地址。
		//
		// 只试第一个地址的话，这一整条自动歌词就**静悄悄地不出现**（界面上照旧是
		// 「暂无歌词」，日志里只有一行）。上游那个 `||` 兜底只管「地址为空」那一种，
		// 所以兜底必须落在**下载**那一层。
		Spec: "字幕的第一个地址下载失败时改试第二个（B 站给同一份字幕两个地址，带 auth_key 的那个才是能下的）",
		Tests: []string{
			"TestTheSecondAddressIsTriedWhenTheFirstOneFails",
			"TestTheBiliSubtitleFallsBackToTheSecondAddress",
		},
	},
	{
		Category: Negative,
		// 「B 站没给字幕」是**正常**的一种（很多歌压根没有），不是错误：歌照放，
		// 歌词区照旧是「暂无歌词」加一个「找歌词」的入口——那一条路留着。
		//
		// ⚠️ 少了这一条，界面上的「暂无歌词」就会变成一句**兑现不了的承诺**：
		// 用户按了「找歌词」，而 B 站那份自动的其实没来。
		Spec: "B 站没给字幕时照旧走搜索：歌词区是「暂无歌词」加一个「找歌词」的入口，那两个搜索源还在",
		Tests: []string{
			"TestNoSubtitleMeansNoAddress",
			"TestNoSubtitleMeansTheSearchStaysAvailable",
			"TestAnUnknownOriginSaysNothing",
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
		// B 站那一份弹幕（`/x/v1/dm/list.so`）。**这一条回的不是 JSON**——
		// 它回的是**原始 deflate 的 XML**（实测 2026-10-27：响应头
		// `Content-Type: text/xml` + `Content-Encoding: deflate`，而 deflate 是
		// **裸的**，头两字节 `c5 55`，不是 zlib 的 `78 xx`）。
		//
		// 🔴 **「解不出 JSON」是一个静默的失败**：症状是「一条弹幕都没有」，
		// 而界面上看不出任何异样。所以另开了 `DoRaw`（拿原文不解），而**错误
		// 分类仍走 `Do` 那一条**——「能不能重试」那张表是全项目唯一的一处
		// （`retry.go`），`DoRaw` 自己造一套的话，弹幕那条路上的「重试」会给错。
		//
		// ⚠️ **压缩那一档是静默的**：拿 `compress/zlib` 去解会报一句看不懂的错
		// （`flate: corrupt input before offset 5`），而症状是「一条弹幕都没有」。
		// ⇒ 有一条测试专门钉「deflate 不是 zlib」。
		//
		// ⚠️ **`p` 那 9 段第 8 段是 19 位的大整数**（弹幕 id）——走过 `float64`
		// 会掉精度。而 `p` 段数不够的时候，后面几段是空的 ⇒ 颜色变成 0（全黑），
		// **一条黑字弹幕在界面上看着完全正常**。所以段数不够的那一条要丢掉。
		//
		// ⚠️ **一条读不懂的不许把整份丢掉**：弹幕的正文是**用户自己打的字**，
		// 什么字符都可能有，而一个**控制字符**（实测 U+0001）在 XML 里是
		// **报错**的——报错的位置在那一条上。
		//
		// 🔴 **这一档最省事的写法是错的**：用一个 `xml.Decoder` 读到底的话，
		// 那个错**让整个 decoder 卡住**——后面的合法弹幕**一条都读不到**
		// （症状是「这个视频只有一条弹幕」）。⇒ 解析要**一条一条单独解**：
		// 解不开的**只丢那一条**。
		//
		// ⚠️ **而没定义的实体要「原样留着」**（`&foo;`）——那是用户打的字，
		// 非严格模式把它原样带出来，**不丢那一条**。
		Spec: "B 站那一份弹幕自己来：原始 deflate（不是 zlib）解开、XML 里 `p` 那 9 段逐段切对（时间/模式/字号/十进制颜色）、正文里的 XML 实体解开、读不出来的一条只丢那一条；拉不到要有话说",
		Tests: []string{
			"TestDanmakuReadsEveryFieldOfEveryLine",
			"TestDanmakuCutsTheNineFieldsOfThePAttribute",
			"TestDanmakuUnwrapsTheXMLEntities",
			"TestDanmakuUnwrapsIsDeflateNotZlib",
			"TestDoRawHandsBackTheBytesUndecoded",
			"TestDoRawReportsTheSameErrorsAsDo",
			"TestDanmakuAsksTheDanmakuEndpoint",
			"TestDanmakuWithoutACidDoesNotAsk",
			"TestDanmakuWithNoDanmakuIsNotAnError",
			"TestDanmakuKeepsWhatItCouldReadWhenOneLineIsBroken",
			"TestDanmakuIgnoresLinesItCannotUnderstand",
			"TestDanmakuUnwrapsAWholeResponseThatIsDeflated",
			"TestDanmakuKeepsTheRowIDAsAString",
			"TestDanmakuGivesEveryLineItsOwnID",
			"TestDanmakuKeepsTheOrderBiliGave",
			"TestDanmakuSaysSoWhenTheAnswerIsNotDanmaku",
			"TestDanmakuWithTheFeatureTurnedOffIsNotAnError",
			"TestDanmakuKeepsGoingAfterALineWithAControlCharacter",
			"TestTheRealResponsesGiveTheExpectedNumberOfLines",
			"TestTheFixtureMatchesTheServerBytes",
			"TestDanmakuWithAnEmptyBodyIsNotAnError",
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
		//   - 请求失败时**不吵**（不弹错误、不转圈、不变红、提示条也不弹），而**那一档
		//     要说一句自己的话**（「没能取到联想词」）——「B 站没给联想」和「这一趟没
		//     拉到」在画面上必须分得开（票 #455 改了原来「一个字都没有」那半句）。
		//
		// ⚠️ 「设置里的『显示搜索历史』不许把联想词一起关掉」也单列：那个设置管的
		// 是「我不想看见我搜过什么」，而联想词是 B 站这次给的。
		Spec: "搜索浮层里多一档「联想词」：输入时出来、和搜索历史在界面上分得开、选中等于选中那次的搜索、拉不到时不吵而那一档说一句自己的话（两种「没有」分得开）、和历史在同一个浮层里（Esc 一起收）、设置里的「显示搜索历史」不管它；两档都摆满时（最小窗口 + 10 条长历史）最下面那几条也在屏幕里、滚得动、点得中",
		Tests: []string{
			"TestTypingBringsUpTheSuggestTerms",
			"TestTheTwoSectionsAreToldApart",
			"TestChoosingOneSearchesThatWord",
			"TestAFailedSuggestSaysSoInsteadOfLookingEmpty",
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
		// 票 #455（BMG-252）：把「失败」和「没有」分开说。
		//
		// 搜索页拉失败原来被说成「没有结果」——用户读到的是「B 站没有这个结果，去换
		// 个词」，而真实原因可能是网络断了、登录态过期、风控。「换个词」这个建议在网
		// 络故障时是错的，而且那一屏**没有任何办法重来**。
		//
		// 联想词那一档拉失败原来**一个字都没有**——「联想这一栏空着」读出来的意思是
		// 「没有联想词」，不是「没拉到」。现在那一档说一句自己的话（「没能取到联想
		// 词」），而**不吵**（不弹错误、不转圈、不变红）那一半没变。
		//
		// ⚠️ **「用户」那一档那一行不是歌**：那一圈反馈原来三样都在说放歌（图形是播
		// 放三角、提示是「从这一首往后听」、无障碍名也是那一句），而用户按下去做的
		// 是跳到那个 UP 主的主页。现在三样都改成「进他的 UP 空间」，而**歌那一档的
		// 「从这一首往后听」不许被改**——那是另一件事（要改另开票）。
		Spec: "搜索页拉失败：失败有自己的说法（不是「没有结果」）且有一个能按的重试；翻页失败不把已经拿到的结果换成一句错；联想词那一档拉失败：那一档说自己的话（不是空着）且不吵；搜索页「用户」那一档：封面那一圈反馈的图形、提示、无障碍名三样说的都是「进他的 UP 空间」，而歌那一档的「从这一首往后听」没被改",
		Tests: []string{
			"TestTheSearchPageSaysItFailedInsteadOfSayingThereIsNoResult",
			"TestTheSearchPageKeepsTheResultsWhenALaterPageFails",
			"TestAFailedSuggestSaysSoInsteadOfLookingEmpty",
			"TestTheUserRowSaysGoToHisSpaceInsteadOfPlay",
			"TestTheProfileGlyphIsNotAPlayTriangle",
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
		// ⚠️ **「静默」改成了「不吵」**（票 #455）：失败不许弹错误、不许转圈、不许变红，
		// 但界面**能说一句自己的话**（那一档写「没能取到联想词」）——「B 站没给联想」
		// 和「这一趟没拉到」在画面上必须分得开。
		//
		// 迟到的结果不许盖掉新的（代次守卫）、退出了不许再往状态上写、框里空了上一
		// 批要收掉（它属于上一个问题）、没挂上 B 站客户端也要安静。
		Spec: "联想词的边界：失败之后不吵（不弹错误、不转圈、不变红，而那一档说自己的话）且不每帧重发、重敲同一个词还能再问一次（界面上用户能做的就是再敲一遍）、空框不许自己养自己（每一帧都请求下一帧 = 窗口永远停不下来）、迟到的结果不盖掉新的、退出之后不写回来、框空了收掉上一批、同一个词不搜第二遍、没挂客户端时不吵",
		Tests: []string{
			"TestAFailedSuggestSaysNoErrorAndDoesNotFireAgain",
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
		Category: HTTP,
		// 弹幕这一条路（应用层 + 缓存那一层）：
		//
		//   - **代次**：换歌之后旧的那一趟回来时必须被丢掉——症状是「在听 B，
		//     屏幕上滚着 A 的弹幕」，而弹幕盖住封面的时候本来就不好认。
		//   - **防重入**：界面每一帧问一次，不挡住的话一次播放能发几百个请求
		//     （B 站会风控，而症状是「怎么问不出弹幕」）。
		//   - **缓存**：7 天之内再进是瞬时的（不发请求），过期了会重新拉。
		//   - **没有 cid**：快照为空，且**一个请求都不发**（本地文件和音乐库条目
		//     走这一支，而它们永远没有弹幕）。
		Spec: "弹幕：换歌之后旧的那一趟作废、界面每帧问一次只发一趟、7 天内再进是瞬时的（不发请求）、没有 cid 时快照为空且不发任何请求",
		Tests: []string{
			"TestALateDanmakuFromTheOldSongNeverLands",
			"TestEveryFrameAsksButOnlyOneGoesOut",
			"TestTheSecondTimeIsInstant",
			"TestAnExpiredCacheIsFetchedAgain",
			"TestNoCidMeansNoSnapshotAndNoRequest",
			"TestSevenDaysLaterItIsGone",
			"TestTheFileIsNamedAfterTheBvidAndTheCid",
			// 生产那一档（真 `bili.Client`）：前面那些都替掉了那一趟。
			"TestTheProductionPathActuallyAsksBili",
			// 端到端：cid 是取流那一趟问出来的（搜索结果那一路）。
			"TestTheDanmakuComesForASongWhoseCidHadToBeAsked",
			// 评审提的三条：
			//  - 同一个 bvid 的新选择（cid 还没问出来）不许拿上一分 P 的弹幕
			//  - 同一个 key 上慢的那一趟不许盖掉新写的那份缓存
			//  - Close 之后缓存不再被写
			"TestReviewUnresolvedSameBVidShowsThePreviousPartsDanmaku",
			"TestReviewAStaleSameKeyFetchOverwritesTheFreshCache",
			"TestReviewCloseStillWritesTheCache",
			// 同一个 bvid 的**另一分 P** 的在途那一趟也不许把自己的 cid 写上
			// （bvid 对得上不够——多 P 的投稿 bvid 是同一个）。
			"TestTheInFlightRunOfAnotherPartDoesNotWriteItsCid",
		},
	},
	{
		Category: Play,
		// 满屏播放器左边那张封面上的弹幕（**界面**那一层；取弹幕那一半是
		// 上面那一条）。
		//
		// 钉的是五个「界面上看不出坏了」的点：
		//
		//   - **画在浮层里**：满屏整个是浮层，而浮层画在主树**之上**——弹幕
		//     画在主树里会被满屏那层不透明的底盖住。🔴 **那个症状是静默的**：
		//     文字**在树里**（`Texts()` 查得到），而画面上一个像素也没有。
		//     ⇒ 「树上有没有那几行字」验不出这个坑，得看画面。
		//   - **位移走 pose**：位置要是算在 view 里（`Absolute().Left(x)`），它
		//     只在重建时更新——而满屏开着时界面 20 帧/秒重建，于是弹幕是 20fps
		//     跳着走的。走 pose（只重绘那一族）是 60fps 平滑。
		//   - **按当前位置起步**：弹幕的时间是**视频时间**，我们放的就是这个视频
		//     的音轨 ⇒ 没有额外 offset。「从第 3 分钟开始听」就从第 3 分钟那一条
		//     开始滚（**不是**按「播放了多久」滚）。
		//   - **key 用弹幕自己的 rowid**：动态列表的 key 必须稳定且唯一——用下标
		//     的话「前面插一条」会让后面所有条的状态整体错配。
		//   - **它是装饰**：不给 Role / Label（读屏软件不念），「减少动态效果」
		//     开着时按 20fps 跳着走，设置里能整个关掉（默认开着）。
		//   - **同一条轨道上两条不能叠字**：两条同速从右边沿进来，不叠字当且
		//     仅当「后一条晚的时间 × 速度 ≥ 前一条的字宽」——报告 §3.3 那条
		//     「走出 30% 宽度」只当**时间门槛**用，它对长字不够（一条 196
		//     像素宽的字滚了 1.8 秒之后尾巴还在封面里，下一条就进来了 ⇒
		//     重叠 88 像素，两句话没法读）。
		//   - **长弹幕不会提前消失**：一条弹幕待到「整个出界」为止（(封面宽
		//     + 字宽) ÷ 速度），**没有时长上限**——早该出界的那些靠「按字数
		//     估的宽上界」粗筛掉，而那个上界只粗筛、不砍。
		Spec: "满屏播放器左边那张封面上的弹幕：画在封面上（浮层里，不是主树）、按当前播放位置滚、位移走 pose（60fps 平滑）、每条弹幕一个稳定且唯一的 key、同一条轨道上两条不叠字、长弹幕不会提前消失、装饰不进无障碍树、设置里能整个关掉（默认开）",
		Tests: []string{
			"TestTheDanmakuIsDrawnOnTheCoverOfTheFullPlayer",
			"TestTheDanmakuUnderTheFullPlayerWouldNotBeSeen",
			"TestTheDanmakuStartsFromTheCurrentPosition",
			"TestTheDanmakuGlidesInsteadOfTeleporting",
			"TestTheDanmakuJumpsWhenMotionIsReduced",
			"TestEveryDanmakuHasItsOwnKey",
			"TestTheDanmakuCanBeTurnedOff",
			"TestTheFullPlayerSettingsHasTheDanmakuSwitch",
			"TestTwoDanmakuOnTheSameLaneNeverOverlap",
			"TestTheSameLaneWaitsForThePreviousDanmakuToClearTheEdge",
			"TestALongDanmakuStaysUntilItHasLeftTheCover",
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
		//   · 正在放那一集**整行淡主色底**（上游 `bg-primary/20`，票 #323），而没在放
		//     的不许染——两半都钉，染了这一半而不钉另一半的话，「标出来」就只是
		//     「有一行不一样」。
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
			// 影的**值只写一处**（票 #239）：界面层一处都不许现调元素级的影，
			// 而那个包里**每一档都必须有调用点**（「定义了没人用的档比没有档更
			// 坏」）。
			//
			// ⚠️ 两条都是**源码级**的判据：影是「落在元素外面那一圈」上的，
			// 那一圈在整页上是别的内容，量它量出来的是「它有没有出现」，量不出
			// 「它的值写在哪」。
			"TestNoElementShadowIsWrittenAtTheCallSite",
			"TestEveryShadowTierHasACallSite",
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
		Spec: "配色：危险/警告/成功三个语义色是上游那一档（深浅两套同一个值）；分隔线深色是 15% 白叠在当前底色上、浅色是 #dbdbdb；深色下卡片和页面底色同源、浮起来靠的是那圈影，浅色下卡片不是一块灰；**被选中的选项**（设置页那些：主题三档 / 窗口关闭两档 / 下拉项的高亮）是实心主色底 + 主色上的字，只有一种画法",
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
			// 「被选中的选项」只有一种画法（票 #239 / 审计 F-14）：设置页的档位
			// 按钮、窗口关闭那两档、下拉项的高亮，三处都是**实心主色底 +
			// 主色上的字**。
			//
			// ⚠️ 和上面「列表选中行不刷底色」不冲突：那一条说的是**行**（点一
			// 行不刷绿），这一条说的是**选项**（选中的那一个亮起来）——两个是
			// 不同的东西。
			"TestTheSelectedOptionLooksTheSameEverywhere",
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
		Spec: "播放条：进度条随窗口变长、三键是上一首·播放·下一首、播放键 48 像素、旋钮只在悬停或按下时画（键盘焦点上时也画）、填过的那一段平时是前景色、悬停/按下才变主色、音量收成图标加浮层（浮层里一根竖滑块和百分比，百分比和滑块居中对齐、字号放大时不折行，点图标静音、滚轮 ±0.05）、音量轨道填的是已经调上去的那一段、浮层走远了会自己收起、旋钮不探出滑块的框、按住时多一层更深的影子、浮层里的滑块不跟着百分比字数横向漂、三键落在窗口的竖直中轴上、中间那一列是 1:3:1、进度行占那一列的 3/4 且居中、三键之间的空隙是 24",
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
			// 浮层底下那个百分比**和那根竖滑块居中对齐**（票 #485：数字看着偏左）。
			"TestTheVolumePercentSitsUnderTheSlider",
			// 字号放大时那个百分比**不许折行**（「100」换行「%」既难看又撑高浮层）。
			"TestTheVolumePercentDoesNotWrapAtBigTextScales",
		},
	},
	{
		// 归「双平台」那一组：还是播放条那一块，但说的是**播放键那一枚的形状**
		//（issue 217 / BMG-129，票 B）。
		Category: Platform,
		// issue 217 原来把播放键做成上游那一枚（无底、前景色图形）。设计规范 v0.1 §5
		// 之后（应用未发版）改成**实心强调色圆**：播放条 44×44、满屏 64×64，图形用
		// AccentText，悬停 AccentHover、按下 AccentPressed，禁用不变色。
		//
		// 🔴 **判据全是画面上的像素**（圆边缘、方盒角、图形色），播放 / 暂停两档、
		// 浅深两套都断。
		Spec: "播放键是实心强调色圆（播放条 44、满屏 64）：圆边缘是强调色、方盒角是底色、图形是 AccentText、悬停与按下换底色、禁用时悬停不变；「正在播放」那一档一样",
		Tests: []string{
			// 盒子 44×44、圆本身够大、图形落在圆心。
			"TestThePlayKeyIsBigEnoughToHit",
			// 圆是实心强调色、图形是 AccentText（深浅 × 播放/暂停）。
			"TestThePlayKeyIsASolidAccentCircle",
			// 悬停 → AccentHover，按下 → AccentPressed。
			"TestThePlayKeyFillShiftsOnHoverAndPress",
			// 满屏那一枚同一种，64×64。
			"TestTheFullPlayerPlayKeyIsTheSolidAccentCircle",
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
		// 归「双平台」那一组：说的是**播放条上那枚音质徽章**念的是哪一档
		// （issue 255 / BMG-155）。
		Category: Platform,
		// issue 255：投稿侧的音质 id ↔ 标签要对上。Hi-Res 无损流（`dash.flac`
		// 里的 FLAC）以前挂着「杜比 Hi-Res」——30251 这个 id 被当成了杜比的
		// 高解析版，于是 `IsLossless()` 判它**不是**无损、`qualityRank` 把它
		// 排在有损档下面。
		//
		// 🔴 **这一族全是「静默」的坏法**：徽章上写错一个字、`IsLossless()`
		// 判错一次，**没有一样会报错**，界面只是「不太对」——用户看到的是
		// 「无损流被标成杜比」，而下载、容器判定、挑流排序全跟着错。
		//
		// ⚠️ **标签、`IsLossless()`、`qualityRank` 三者必须一致**：它们是同一
		// 件事的三种说法，各说各的就会出现「徽章说无损、机器说不无损」。
		//
		// ⚠️ **id 表会过期**（B 站会加档改档），所以这一条钉的是「**已知这
		// 几档**」而不是「**只有这几档**」：认不出来的 id 有兜底（按码率说
		// 话），不会报错、不会挂错标签。
		Spec: "投稿侧的音质 id ↔ 标签：64K / 128K / 192K / 杜比全景声 / Hi-Res 无损；Hi-Res 无损流（dash.flac）挂「Hi-Res 无损」而不挂「杜比 Hi-Res」，且 IsLossless() 判它无损、qualityRank 把它排在杜比之上；认不出来的 id 按码率说话而不报错",
		Tests: []string{
			// 这几档分别是什么标签（认得出的档位不吃码率）。
			"TestKnownQualityLabelsArePinned",
			// 标签 / IsLossless() / qualityRank 三者一致，且 rank 严格递增。
			"TestTheThreeViewsOfAQualityAgree",
			// 30251 是 Hi-Res 无损，不是杜比 Hi-Res。
			"TestHiResLosslessIsNotDolbyHiRes",
			// B 站加新档时的兜底：按码率说话，不报错、不挂错标签。
			"TestAnUnknownQualityFallsBackToTheBitrate",
			// 播放条上那枚 chip 的短版（宽度有上限，见上面那一条）。
			"TestChipLabelShortensTheQualityNames",
			// 「字段比 id 权威」：`dash.flac` 里的 id 换了，档位不能跟着乱。
			"TestPlayURLTrustsTheFlacFieldNotTheIdInsideIt",
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
		// 归「双平台」那一组：还是播放条那一块，但说的是它上面那一句**错**
		// （票 #320）。
		Category: Platform,
		// 票 #320：播放条上那一句错被压成**一行 + 省略号**（76 高的播放条装不
		// 下两行——这是布局的账），而 `bili.Friendly` 那张表里的话信息量很大
		//（「B 站没有给这一条单独的音频流（歌单、活动那一种多半是这样）。在 B 站
		// 里打开它听，或者换一首放」四十来个字）——用户常常只读到半句话。
		//
		// 🔴 **这一族的坏法全是静默的**：完整那一句拿不到，界面上看不出坏了
		// （那一行的**文本节点里**就是完整那一句，只是画的时候被截了——所以
		// 「它在树上」证明不了任何事）；而播放条要是被撑成两行，最后一行会被
		// 76 高裁掉，**界面上也看不出坏了**。
		//
		// ⚠️ **判据要分两半**：一是「完整那一句拿得到」（排开了、不止一行），
		// 二是「播放条一个像素都没动」（打开详情前后那一行的框一模一样）。
		Spec: "播放条上那一句错：屏幕上还是一行 + 省略号（76 高的播放条装不下两行），完整那一句在悬停提示和点开的详情层里都拿得到（详情层多行排开、能选中复制），打开详情时播放条一个像素都不动，错没了详情层就收",
		Tests: []string{
			// 详情层里是**完整那一句**，而且**排开了**（不止一行）。
			"TestClickingTheErrorOpensTheWholeSentence",
			// 打开详情前后，播放条上那一句的框**一模一样**。
			"TestThePlayBarDoesNotMoveWhenTheDetailOpens",
			// 错没了，详情层就收（不然它写着「打不开」而其实能播了）。
			"TestTheDetailClosesWhenTheErrorIsGone",
			// 悬停那一条路没被弄坏（提示框里也是完整那一句）。
			"TestHoveringTheErrorStillShowsTheWholeSentence",
			// 面板开着的时候提示框不挂（两块摆在同一个位置，叠在一起像坏了）。
			"TestTheTooltipDoesNotStackOnTopOfTheDetail",
			// 那块面板**关得掉**：按 Esc、点外面（浮层自己管那两个手势）。
			"TestTheDetailClosesWhenTheUserPressesEscape",
			"TestTheDetailClosesWhenTheUserClicksOutside",
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
		// 归「双平台」那一组：这一票动的是**名字型可点击元素**的样子。
		//
		// 🔴 **判据是像素**：那个名字被一个**胶囊**包着（左右各 14 像素的内边距
		// + 圆角，指针落上去时再压一层半透明前景色），改完是**一句话字 +
		// 悬停时一道下划线**——而「胶囊」这件事在代码里看不出来，换一种写法
		// 一样编译得过、点得动、无障碍名字也还叫得出来，只有画面上才差得出来。
		//
		// ⚠️ **反面也断**：同一张卡片上那几枚**动作键**（赞 / 下一首播放 /
		// 下载音频）悬停时**仍然要**有那一层淡底——它们是按钮，不是名字链接。
		// 把它们一起拍平的话，「哪一个是动作」就没有了。
		Category: Platform,
		// issue 417：名字型可点击元素改成下划线（上游是 `hover:underline`）。
		Spec: "名字型可点击元素（动态条目上的作者名、播放条上的 UP 名、列表行里的 UP 名）是一句话字，悬停时带一道下划线，没有胶囊底；下划线的颜色是主题算出来的那一档主色，深浅两套都读得出来；它仍然点得动、键盘走得到；同一处的动作键仍然是按钮",
		Tests: []string{
			"TestTheDynamicAuthorNameIsALinkNotAPill",
			"TestThePlaybarOwnerNameIsALinkNotAPill",
			"TestTheListOwnerNameIsALinkNotAPill",
			"TestTheActionButtonsAreStillButtons",
			"TestTheNameLinkIsKeyboardReachable",
			"TestTheUnderlineColorFollowsTheTheme",
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
		// 🔴 **这一条的错法是「静默」里最静的那种**：少一层蒙版、字色跟着
		// 主题翻——**没有一样会报错**，界面只是「有时候读不清」。而头图是
		// **用户上传的任意图片**：用户传一张白底图，浅色主题下白字就化在
		// 白底里（上游靠 `drop-shadow` 补，我们没有那一层）。
		Category: Platform,
		// 票 #295（UI 审计 L-13）：UP 空间头图区那一层黑蒙版。
		//
		// ⚠️ **上游那一档是平的**（浅 30% / 深 50%）：压在**纯白**头图上，
		// 白字只有 2.1:1（浅色）/ 4.0:1（深色）——所以有字的那一带**再压
		// 一档**，压到最坏情况也有 4.5:1（WCAG 正文 AA）。
		//
		// ⚠️ **这一条不放在表末尾**：表末尾那处已经撞过好几回（这一轮就是
		// 第三次），挨着上面那条「收藏夹头图区」读得顺——两条讲的都是
		// 「头图区」。
		//
		// ⚠️ **票 #268（2026-10-25）把这一块按参考图重排了**：头图区不再
		// 压黑蒙版、字不再压在照片上（票 #357 之后就是那一版），所以下面
		// 那句 spec 记的是**现在这一版**的样子：横幅 132 高、头像 112 压在
		// 横幅下沿、名字 / 签名 / 统计写在**页面底色**上。
		Spec: "UP 空间头图区（设计规范 v0.1 §7.18，取代上游压黑蒙版 + 白字的做法）：横幅 132 高、底部淡进页面底色；头像 112 压在横幅下沿；名字、认证徽章、签名、统计写在**页面底色**上，字色用主题自己的字色，不再依赖头图是什么图",
		Tests: []string{
			// 头像 112、左右留白 32、头像压在横幅下沿、昵称与徽章与签名的排布。
			"TestTheSpaceAvatarIs112AndTheHeaderPads32",
			"TestTheHeaderPutsTheNameTheBadgeAndTheSignInOneRow",
			"TestTheFollowButtonIsAnOutlinedBox",
			"TestTheHeaderShowsTheBadgeOnlyWhenThereIsOne",
			// 「关注」那一枚**键盘走得到、回车按得动**（它换成了自绘的描边
			// 方块，而自绘的那一版一开始用的是 `ui.Row`——那是个容器，
			// Tab 走不到它）。
			"TestTheFollowButtonIsKeyboardReachable",
		},
	},
	{
		// 票 #268（2026-10-25）：UP 空间的动态条目与稿件卡按 B 站 UP 主页
		// 那张参考图重排。
		//
		// ⚠️ **静默的那几种错法都在这儿**：条目头一行的时间回到昵称右边
		// （看着还是一行，只是顺序反了）、「赞」掉回下面那一排、稿件卡的封面
		// 又变回方的——**没有一样会报错**，每一处都还是「能看」。
		Category: Platform,
		Spec:     "UP 空间的动态条目：左边头像 + 昵称，下面一行「时间 · 动作」，右边「赞 + 数」与「⋯」；稿件卡横排（封面 192×128 + 右下角时长角标，右边标题 / 描述 / 播放量），带圆角、描边和卡片底",
		Tests: []string{
			"TestTheDynamicItemPutsTheTimeUnderTheNameAndTheLikeOnTheRight",
			"TestTheMoreMenuHoldsTheMusicActions",
			"TestTheArchiveCardIsHorizontalWithADurationBadge",
			"TestTheDynamicPaneCardAndCoverMatchUpstream",
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
		Spec: "确认框是应用自绘的模态（按类型变色的警告图标 + 标题 + text-sm 正文 + **带壳的取消**（常驻底 + 描边，票 #228）+ 按类型变色的确认键）；日期范围是**双月**选择器（256 宽的触发键，弹出来是两个月并排，能挑起始和结束，没在筛的时候日历上什么都不亮）；下载进度条是**蓝**的（不是主色），出错的那一条是红的，完成那一格有一枚**绿勾**",
		Tests: []string{
			// 确认框：图标在不在、是不是按类型变色；确认键的颜色；正文的字号；
			// 取消是不是带壳；**那个框在无障碍树里有没有名字**。
			"TestTheConfirmDialogHasAToneColoredWarningIcon",
			"TestTheConfirmButtonTakesTheToneColor",
			"TestTheConfirmBodyIsTextSmall",
			"TestTheCancelButtonKeepsItsShell",
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
		// 这一族动的是**过渡本身**：曲线、时长、位移与淡入的错开。
		//
		// 🔴 **这一族的错法全是「静默」的**：曲线换了没有任何东西会报错，界面
		// 只是「不太顺」；滑块退回定时长曲线也不报错，只是连点三个 Tab 时
		// 顿一下；淡入和位移又同步了也不报错，只是「啪」地一下出现。
		Category: Platform,
		// 票 #292（DESIGN.md §五）：一条统一曲线 + 主 Tab 滑块换成弹簧。
		Spec: "所有过渡走同一条曲线 `CubicBezier(0.16, 0.9, 0.21, 0.98)`（DESIGN.md §五《一条统一曲线》）；主 Tab 滑块走**弹簧**（ζ=0.9923，接近临界阻尼；不是定时长曲线）；Toast 入场的**淡入与位移错开**（位移先走、淡入后到）",
		Tests: []string{
			// 曲线和弹簧的值不许漂（唯一出处就是那一个包）。
			"TestTheUnifiedCurveIsTheOneWeCopied",
			"TestTheTabSlideIsASpring",
			// 滑块走的是弹簧：100ms 时只走到约一半（定时长曲线那时已经 78%）。
			"TestTheTabSliderFollowsTheSpring",
			// 淡入与位移错开：同一时刻量到的两个进度，淡入那个明显更小。
			"TestTheToastFadesInAfterItSlides",
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
		//
		// ⚠️ **队列抽屉「正在播放」那一行不在这条里了**（票 #294，2026-10-24）：
		// 它后来单独改成了**淡主色底**（上游 `bg-primary/20`），见本表里票 #294
		// 那一条（分集抽屉那一行是票 #323 那一条）。
		// 这一条说的是**左栏那四处**——侧栏菜单当前项、侧栏收藏夹当前项、关注页
		// 分组当前项、动态页作者栏当前项。
		//
		// ⚠️ **「字」那一档是压深的主色，不是正文色**（票 #323 改对）：票 #239 把浅色
		// 下的底从「主色 14%」换成中性灰之后，主色当字也得跟着换成「按新底算」的
		// 那一档（默认主色下 #107032）——代码里 `flatbutton.go` 的 `flatCurrentText`
		// 自己写着「**不是正文色**」。
		Spec: "「当前在哪」在应用里只有**一种底色**：侧栏菜单当前项、侧栏收藏夹当前项、关注页分组当前项、动态页作者栏当前项，四处的**底**都是 flat 那一档的**中性灰底**（深浅两套是同一个数：浅色 #dcdcdf / 深色 #3e3e41），**没有实心主色大块**；**字**分两档：深色下是主色字、浅色下是**从主色现算的、压到 ≥4.5:1 的那一档**（默认主色下 #107032，**不是正文色**）；搜索页**没有页内输入框**，也没有「搜索」那一行标题，顶上第一行就是「搜索【词】的结果」（24/400），当前搜的词显示在**顶栏**那个框里（填上去的那个词，用户点进去敲字是**替换**不是追加；在搜索页上按那枚「清空」清掉的是这一场搜索，不只是框里那几个字）；满屏播放器顶栏的设置齿轮**紧跟在歌名后面**（上游那一组的构图），不是浮在窗口中间",
		Tests: []string{
			// 左栏那四处「当前在哪」是同一个底色配方（深浅两套都量，底色和字色都量）。
			"TestTheCurrentItemIsFlatEverywhere",
			// 搜索页的减法，以及顶栏那个框显示当前关键词。
			"TestTheSearchPageHasOneTitleAndNoSecondSearchBox",
			"TestTheSearchPageShowsTheCurrentKeyword",
			// 顶栏那个框的三个行为：填上去的词敲字是替换、「清空」清掉这一场搜索、
			// 空词回车回到「还没输入」那一支。
			"TestTheFilledSearchBoxReplacesItsWordWhenYouType",
			"TestClearingTheSearchBoxOnTheSearchPageClearsTheResults",
			"TestAnEmptySearchGoesBackToTheIdleState",
			// 满屏顶栏那一组的构图（齿轮紧跟歌名）。
			"TestTheFullPlayerGearSitsRightAfterTheTitle",
		},
	},
	{
		// 🔴 **这一族的错法也全是「静默」的**：控件列参差、登录键是一块实心
		// 主色的大方块、标题是粗的——**没有一样会报错**，界面只是「不精致」。
		Category: Platform,
		// 票 #235（审计 F-04 / F-05 / F-07）：视觉一致性第二批（轻量一批）。
		//
		// ⚠️ **控件列那条说的是文本缩放为 1 的时候**：缩放 > 1 时「主题」「窗口
		// 关闭」那两行会跟着字号变宽（它们的字要放得下），那一列的左边缘于是有
		// 两条线——这不是回归，是那一行字要放得下（见 `settings_page.go` 的
		// `settingsControlW` 与 DESIGN.md §三）。
		Spec: "设置页的控件列有**一条共同的左边缘**（**文本缩放为 1 的时候**）：基准宽度 300，每一行的控件都从同一条线上开始并填满那一格（此前左边缘落在 762 / 801 / 822 / 942 四个位置，预览那三张右边空 16、色卡那两块右边空 91）；**粗体只出现在刻度写明的那几处**（页标题 / 分组标题 / 弹窗标题 / 空状态标题 / 列表里的名字一律 400；粗体只剩品牌字、收藏夹头图区那个大标题、404 那个数字、歌词浮层里那两个读数，和正在唱的那一行歌词——上游的 `font-extrabold`）",
		Tests: []string{
			// 控件列的左边缘是一条线（每一个控件都从同一条线上开始）。
			"TestTheSettingsControlsShareOneBaseline",
			// 那一列里的控件填满那一格（色卡两块、预览三张、下拉本身）。
			"TestTheSettingsControlColumnIsFilled",
			// 粗体只在刻度写明的那几处（AST：**递归扫整个 internal/ui**，找四样
			// 东西——`.Bold()`、`FontWeight(字面量 ≥ 700)`、**字面量 ≥ 700 传给
			// 一个字重参数**（调用点）、**字重常量 ≥ 700**）。
			// ⚠️ 后两样是评审补上的：更新弹窗那一行 13/700 的字面量在**调用点**，
			// 而正在唱的那一行歌词 800 藏在**常量**里——只认 `FontWeight(700)`
			// 的那条判据这两处一个都看不见。
			"TestOnlyTheScaleIsBold",
			// **页标题**那几个字号的字重是 400（AST：`pageTitleSize` 与
			// `emptyPageTitle`）。
			// ⚠️ **播放器那一页的歌名（`playerTitleSize`）不在这条里**：那是**歌名**
			// 不是页标题，而票 #169 的 `TestTheFullPlayerTitleIsNotBold` 钉的
			// 就是「满屏那一句**要写** `.FontWeight(400)`」——加进去会把那一处判红。
			// 那一处由上一条（全局白名单）盖着。
			"TestPageTitlesAreNotBold",
		},
	},
	{
		// 🔴 **这一族的错法也全是「静默」的**：同一页上两个分段控件长得不一样、
		// 说明列被挤扁——**没有一样会报错**，界面只是「不精致」。
		Category: Platform,
		// 票 #240（审计 F-17 / F-27）：设置页的分段控件归一 + 说明列的最小宽度。
		//
		// ⚠️ **那一档是「实心主色底」，不是灰轨 + 白滑块**：框架 `ui.Segmented`
		// 没有滑块动画，宽度还按内容算（填不满控件列那一格），而「数据显示样式」
		// 那一格下面就是预览图、选中那张描主色边——见 DESIGN.md §四。
		Spec: "设置页只有**一种**分段控件：三处分段控件（主题四档 / 数据显示样式三档 / 窗口关闭两档）画法一致——选中的那一档是**实心主色底**，没有灰底轨道；说明列有**最小宽度**（只在说明存在的行生效——#530 之后设置页的说明全部删了，下限休眠）；控件列在默认配置下拿到**完整的基准宽**（340，装得下主题四档），放不下的极限档控件收窄而不是溢出",
		Tests: []string{
			// 三处分段控件的**选中档**底色都是实心主色、**没选中的那一档**都是
			// 页面底色（深浅两套）。
			"TestTheSettingsPageHasOnlyOneSegmentedControl",
			// 默认配置下控件列拿到完整基准宽（1200 / 900 / 侧栏 320 / 字号 1.4）。
			// 前身是钉「说明列有最小宽度」的 TestTheSettingsDescriptionColumnKeepsItsWidth
			// ——#530 把说明删了之后那一列不复存在，这一条反过来护控件列。
			"TestTheSettingsControlColumnKeepsItsFullWidth",
			// **控件列不会被推出内容区**（七档配置：900 / 1200 / 870 × 默认侧栏 /
			// 侧栏 480 / 侧栏 320 / 字号 1.4 倍 / 侧栏 480 + 1.4 倍）。
			// ⚠️ 这一条是评审之后加的：框架的 flex 没有收缩，放不下就是溢出，
			// 而第一版给说明列的是**无条件**的下限——内容区窄过
			// 「320 + 24 + 260」时控件列被顶出窗口右缘。
			"TestTheSettingsControlColumnIsNeverCutOff",
			// 代理页那个下拉还是 240 宽（上游的 `w-[240px]`）：它不归设置页那一列
			// 的下限管。
			"TestTheProxyTypeSelectKeepsItsOwnWidth",
		},
	},
	{
		// 🔴 **这一族的错法是「静默」的**：跳过去了可那一行还在视野外（用户
		// 看见的是「设置页打开了，可这里没有下载目录」）、或者那一行被切掉半截、
		// 或者**设置页的行距被改了**（往下滚一屏就看得见）。
		Category: Platform,
		// 票 #318（把设置页那段手写的跨帧「滚进视野」换成上游那一行
		// `Element.ScrollIntoView()`）+ UX 报告 C1（那一行要指、要滚进视野）。
		//
		// ⚠️ **「指着一行」不许改这一页的排版**：上游滚的是「刚好露出这一行的
		// 盒子」，我们从前滚完留 16 像素——那 16 像素**只能补在滚动位置上**，
		// 补在盒子上（垫内边距）的话就是**内容高度**的一部分，被指着那一行下面
		// 每一行都低了 16 像素。
		Spec: "从下载记录页跳到设置页的「下载目录」那一项：切到「常规设置」那一格、**指**着那一行（标题是主色）、**把那一行滚进视野**（标题与说明都整个在可视区里，离下沿还留着 16 像素）；而**指着一行不许改这一页的排版**——被指着那一行下面每一行的位置与内容总高都和没指着时一样",
		Tests: []string{
			// 点得动、跳得到那一格、指得着那一行。
			"TestTheMissingDownloadDirectoryTakesYouToTheSetting",
			// 那一行真的滚进视野了，而且**说明那一行也整个在可视区里**。
			"TestTheDownloadDirectoryIsScrolledIntoView",
			// ⚠️ **指着那一行不许把这一页的内容撑高**（垫内边距那一种做法就是
			// 撑高了 16 像素，于是下面每一行都被压低）。
			"TestTheFocusedSettingDoesNotPushTheRowsBelowIt",
		},
	},
	{
		// 🔴 **这一族的错法是「静默」的**：默认落点算错的时候，歌**照样下得完**，
		// 界面上**每一句话都是真的**，只是它们说的不是同一个地方——用户在自己的
		// 下载目录里翻不到刚下的那几首，而设置页上那一格写着「未设置」。
		//
		// 票 #361：默认落点从**系统下载目录**（`~/Downloads`，那儿是浏览器和
		// 安装包的地方）改成**「音乐」目录下的应用子目录**（macOS `~/Music/BiuGo`，
		// Windows `C:\Users\<用户名>\Music\BiuGo`——框架的 `PathMusic` 只把
		// 英文名拼在家目录下面）——同类音乐软件（网易云、QQ 音乐、Spotify）都是
		// 这么放的。
		//
		// ⚠️ **「老歌不搬家」也是这一条的一部分**：已经在 `~/Downloads` 下的歌留在
		// 原地（迁移是另一件事，要人拍板），而这一条只改**新**下载的落点。
		Category: Platform,
		Spec:     "默认下载目录是「音乐」目录下的应用子目录（macOS ~/Music/BiuGo、Windows C:\\Users\\<用户名>\\Music\\BiuGo），落盘那一刻目录不存在会建；**查路径的那一层不建目录**（开应用不会在用户磁盘上留下文件夹）；设置页和下载记录页上显示的是**歌真正落下来的地方**而不是空串；用户在设置里手动选过的目录照旧；已经在旧目录下的老歌不搬家",
		Tests: []string{
			// 路径本身：它在音乐目录下面，子目录名是显示名（不是用户数据目录那个
			// 老名字），拿不到家目录时退回系统下载目录而不是给一个相对路径。
			"TestTheDefaultDownloadDirectoryIsTheAppFolderUnderTheMusicDirectory",
			"TestTheDownloadDirectoryIsNamedAfterTheAppNotAfterTheLegacyUserDataDirectory",
			"TestTheDefaultDownloadDirectoryIsNotARelativePath",
			// ⚠️ **查路径这一层不建目录**：它被开应用时和设置页读着，而它一旦
			// 建目录，光是打开应用（或者跑一条 internal/ui 的测试）就会在**用户
			// 自己的** ~/Music 里留下一个文件夹。
			"TestTheDefaultDownloadDirectoryIsNotCreatedJustForLooking",
			// 建目录是**落盘那一刻**的事。
			"TestTheDownloadDirectoryIsCreatedWhenTheSongLands",
			// 优先级：设置里没选过是默认落点，选过的是用户选的那个。
			"TestTheDownloadDirectoryIsTheDefaultOneUntilTheUserPicksAnother",
			// ⚠️ **一个目录都没有时不许入队**：空目录会让歌落到进程的当前工作
			// 目录里（相对路径），而界面上没有一句话说它去哪儿了。
			"TestTheDownloadIsRefusedWhenThereIsNowhereToPutIt",
			// 界面上显示的那条路径（两页说的是同一个字符串）。
			"TestTheSettingsPageShowsWhereTheSongsWillLand",
			"TestTheSettingsPageStillShowsTheDirectoryTheUserPicked",
			"TestTheDownloadPageShowsWhereTheSongsWillLand",
		},
	},
	{
		// 🔴 **这一族的错法也全是「静默」的**：两端对齐空着三百像素、迷你窗里
		// 两根滑块分不出哪个是哪个、同一个动作两个页面一个有一个没有、
		// 超长歌名占满整行中段、深色下黑封面和底色融成一片——**没有一样会
		// 报错**，界面只是「不好用」。
		Category: Platform,
		// 票 #244（UX 报告 §2 的 D1 / B5 / B6 / E1 / E2）。
		Spec: "布局一批：设置页每一行的说明和控件之间**不空着几百像素**；迷你窗的音量滑块**有悬停提示**（而且那枚提示**看得见**）；「全部播放」在两个页面上是**同一枚**（画法统一；语义是票 #337 拍板的那一条）；列表里的**超长歌名不占满整行中段**；**深色下的封面有边**（不再和行的底色融成一片）",
		Tests: []string{
			// 设置页那一段空档（1200 / 900 两档窗口，量的是画面上的字迹）。
			// **字号放大时说明不折行**（1.0 / 1.25 / 1.4 / 1.5 / 1.75 五档）——
			// 折行会让整行变高、右边那列控件往下挪半格。
			// 迷你窗音量滑块那枚提示：它在，而且**在窗内**（窗只有 320×100，
			// 而滑块在第一行——摆在上面的话整个提示落在窗外）。
			"TestTheMiniVolumeSliderSaysWhatItIs",
			// 两个页面上那两枚按钮是同一枚（盒子一模一样）。
			"TestThePlayAllButtonIsTheSameOnBothPages",
			// 两个页面上按下去，进去队列的都是这一页的歌。
			"TestThePlayAllButtonReplacesQueueOnBothPages",
			// **放不了的不要塞进队列**（判据 `app.CanPlay`，和右键菜单里那三个
			// 入口是同一个）——而且跳过了几首要说一声。
			"TestThePlayAllButtonSkipsTheUnplayableOnes",
			// 超长歌名的墨迹有上限，不占满整行中段（量的是像素）。
			"TestALongTitleDoesNotFillTheWholeRow",
			// 短歌名一个字都不多让（上限不许反过来咬短歌名）。
			"TestAShortTitleIsNotAffected",
			// 「图取到了」那一档也描一圈边（深浅两套）。
			"TestTheLoadedCoverHasAnEdgeOfItsOwn",
		},
	},
	{
		// 🔴 **这一条的错法是「静默」的**：顶图被冻在列表上面，滚动列表时它一动
		// 不动——**没有一样会报错**，界面只是「不像上游那一页」。
		//
		// 上游那一页（`music-recommend/index.tsx`）的顶图和下面的列表在**同一个
		// 滚动容器**里，所以顶图跟着滚走；而 Tab 那一排在滚动容器**外面**，滚
		// 列表时它不动。
		Category: Platform,
		// 票 #369。
		Spec: "首页「流行」那一屏：顶图（新歌速递）**跟着列表一起滚走**，而 Tab 那一排**留在滚动容器外面**（滚列表时它不动）；另外两档不受影响",
		Tests: []string{
			// 顶图滚一段之后不在画面上了，而下面那一档的歌还在。
			"TestTheTopImagesScrollAwayWithTheList",
			// 网格那一档（设置里的「数据显示样式」）也是一样的。
			"TestTheTopImagesScrollAwayInGridModeToo",
			// Tab 那一排的 y 在滚动前后一模一样。
			"TestTheTabRowStaysPutWhileTheListScrolls",
			// 另外两档没动：没有顶图，而且照样能滚。
			"TestTheMusicAndGuichuTabsAreNotTouched",
			// 下面那一档没歌的时候顶图还在（它是这一屏的内容，不是列表的装饰）。
			"TestTheTopImagesStayWhenTheListIsEmpty",
		},
	},
	{
		// 🔴 **这一条的错法是「静默」的**：窗口一矮，新歌速递那一块就减成**一行**
		// ——用户看到的是「4 张图」，而上游那一块永远是**两行**。没有一样会报错，
		// 界面只是「不像上游那一页」。
		//
		// 上游 `new-music-top.tsx:157` 的 `pageSize = colCount * 2`：那个文件**只量
		// `window.innerWidth`、从不量高度**，所以它永远是两行；列数也是按**窗口
		// 宽**分档的（`:135` 与 `:209` 那组 Tailwind 视口断点），所以同一个窗口
		// 宽度下我们摆几列、一页摆几首都和上游一样。
		//
		// ⚠️ **「下面那一档的歌在首屏之外」是上游的样子**，不是缺陷：上游那一块和
		// 列表在同一个滚动容器里（`music-recommend/index.tsx:261`），矮窗口下这一
		// 块占满首屏，歌要往下滚才看得到。
		Category: Platform,
		// 票 #475。
		Spec: "首页「流行」那一档顶部的新歌速递：**永远两行**（不因窗口矮就减成一行），列数按**窗口宽**分档（上游那张表），默认窗口下画着 10 张",
		Tests: []string{
			// 窗口从最小高到很高，行数一个都不许变。
			"TestTheBlockIsAlwaysTwoRows",
			// 默认窗口（1200×800）下画着 10 张（上游 5 列 × 2 行）。
			"TestTheDefaultWindowShowsTenCards",
			// 改宽度列数跟着换（上游的断点），改高度行数不动。
			"TestResizingTheWindowChangesTheColumnsNotTheRows",
			// 列数那张表本身（上游的断点）。
			"TestNewMusicColumnsFollowItsOwnBreakpoints",
			// 封面永远是格子的宽、宽高相等（上游 `aspect-square w-full`）。
			"TestTheCoverStaysSquareAtTheMinimumWindowHeight",
			// 翻页箭头在列表里点得动（上游有这两枚箭头：`new-music-top.tsx:182-199`）。
			"TestTheNewMusicPagerIsClickableInsideTheList",
		},
	},
	{
		// 🔴 **这一条的错法也是「静默」的**：那一枚按钮悬在 Tab 右边半截、
		// 矮一截（28 而不是 40）、又没有那枚 ▶——**没有一样会报错**，界面
		// 只是「不像这一页的操作，也不像全局操作」。
		//
		// 而它是首页和每一页列表上**唯一的主色实心块**，用户第一眼先看它。
		Category: Platform,
		// 票 #288（UX 报告 C-04 + C-05）。
		Spec: "「全部播放」那一枚贴在内容区右沿、尺寸 114×40、带一枚 18 的 ▶ 图标（上游 HeroUI `Button size=\"md\"` 的逐像素实测值）",
		Tests: []string{
			// 右沿 = 内容区右沿、尺寸 114×40（1200 与 900 两档窗口，深浅两套）。
			"TestThePlayAllButtonSitsAtTheRightEdgeOfTheContentArea",
			// 那一枚里有一枚 18 的 ▶ 图标，而且它在字的**前面**（上游的
			// `startContent`）。
			"TestThePlayAllButtonHasThePlayIcon",
		},
	},
	{
		// 票 #337 覆盖 #299：单首保留队列，显式批量/区段替换队列。
		// 票 #608 覆盖 #337 的那一处「同一首已经在队列里时照样再加一份」：
		// 照上游 Biu 的 `play()` 改成**定位已有那一份**，不再重复入队。
		Category: Play,
		Spec:     "点一首歌保留旧队列（已在队列里就定位、不在才追加）；批量入口替换这一批并从第一首播；从这一首往后听/下一首播放/添加到队列保持区段替换/当前后插入/队尾追加的语义",
		Tests: []string{
			"TestEnqueueAndPlayNowAppendsAndStartsTheNewSong",
			// 同一首已经在队列里时**定位过去，不再追加一份**（照上游）。
			"TestEnqueueAndPlayNowSelectsTheSameSongInsteadOfAppending",
			// 已经在队列里的那一首：长度不变、当前项指过去、放的是它（票 #608）。
			"TestEnqueueAndPlayNowSelectsAnItemThatIsAlreadyInTheQueue",
			// 不在队列里的那一首：追加到队尾并立即播（票 #608）。
			"TestEnqueueAndPlayNowAppendsASongThatIsNotInTheQueue",
			// 队列里同一首排了两遍时，定位到用户点的那一份（票 #608）。
			"TestEnqueueAndPlayNowLocatesTheDuplicateNotTheFirstCopy",
			// 点一首歌不清空周围的歌（票 #608）。
			"TestEnqueueAndPlayNowDoesNotClearTheQueueAroundIt",
			// E2E：投稿页 112 首「全部播放」之后双击第 59 首，队列还是 112 首，
			// 当前项是第 59 首；点一首不在队列里的会追加（票 #608）。
			"TestDoubleClickingAnUpSubmissionKeepsThePlaylist",
			"TestDoubleClickingASongOutsideTheQueueAppendsIt",
			"TestEnqueueAndPlayNowKeepsTheOldSelectionOnFailure",
			"TestThePlayNowEntryAppendsAndStartsThatSong",
			"TestThePlayAllButtonReplacesQueueAndStartsAtItsFirstSong",
			"TestThePlayAllButtonReplacesQueueOnBothPages",
			"TestPlayAllReplacesTheQueueWithSearchResults",
			"TestTheUpSpacePlayAllReplacesTheQueue",
			"TestOtherBatchEntriesReplaceQueueAndStartFirst",
			"TestTheFourRowQueueActions",
			"TestLibraryPlayNowPreservesTheQueue",
			"TestSingleSongCardsPreserveTheQueue",
			"TestChoosingPlayNowHandsOverJustThatSong",
			"TestBothPlayEntriesAreOnTheRowMenu",
			// 应用层那三条：**替换队列**这条路的契约（换掉 + 从第一首 + 起播失败
			// 把原队列换回去）。
			"TestPlayFromSwapsTheQueueAndStartsAtTheFirst",
			"TestPlayFromWithNothingToPlayLeavesTheQueueAlone",
			"TestPlayFromKeepsTheQueueWhenTheFirstSongWillNotPlay",
			// 新歌速递那两枚播放钮（这一票把它们从 Play 改成 PlayNow）。
			"TestClickingANewMusicCardQueuesExactlyThatSong",
			"TestClickingTheHoverPlayButtonQueuesExactlyThatSong",
			// 两页的行菜单**共用一份**（这一票把「立即播放」那一档收进共用那份），
			// 各自最后那一项（移除 / 删除）仍各归各的。
			"TestBothLibraryPagesOfferTheirOwnLastMenuItem",
			"TestBothLibraryPagesOfferPlayNextInTheRowMenu",
		},
	},
	{
		// 🔴 **这一条治的是「延迟」**：切歌到出声之间有一段空档——取流要跑一趟
		// 带 WBI 签名的网络往返（实测 200~400ms），再叠上开解码器、等第一批样本。
		// 用户听到的是「点下一首 → 空档 → 出声」。
		//
		// 提前备地址的那一趟**只解析地址、不预下载字节**：我们的 `StreamSource`
		// 会**在盘上落一个临时文件**，预下载 = 占磁盘 + 多一条连接（那是另一件事）。
		//
		// ⚠️ **这一条的错法是「静悄悄白费」**：备错了（备的和真播的不是同一首）
		// 的话，放出来的歌**还是对的**（对不上就重新取），而省下来的那几百毫秒
		// 白费了——**界面上看不出任何异样**。
		Category: Play,
		// 票 #366（2026-10-24）。
		Spec: "下一首的流地址在当前这首快结束时就备好（只解析地址，不预下载字节）；真播下一首不再重新取流；手动按下一首也吃备好的那一份（票 #581）；队列一改就作废；预取失败无声退回且不重试；地址快过期就放弃；暂停时不预取；备的和真播的是同一首（含随机模式）",
		Tests: []string{
			// 主线：备好的那一份在切歌那一刻**被用掉**（取流接口的次数停在 2）。
			"TestTheNextTrackIsPreparedBeforeTheSwitch",
			// 手动按下一首吃备好的那一份（次数停在 2，票 #581）。
			"TestAManualNextUsesThePreparedAddress",
			// 队列一改 → 已备好的立刻作废（会重新备一份）。
			"TestReorderingTheQueueThrowsThePreparedAddressAway",
			// 失败无声退回（真播时按正常路径取一次），且不会每 50ms 重试。
			"TestAPrefetchThatFailsFallsBackSilently",
			// 备好的地址快过期了就放弃。
			"TestAPreparedAddressThatExpiresIsThrownAway",
			// 暂停时不预取。
			"TestNothingIsPreparedWhileTheTrackIsPaused",
			// 随机模式：备的和真播的是同一首。
			"TestThePreparedTrackIsTheOneThatActuallyPlays",
			// 队列内部那两半：预告和真抽是同一件事、预告不动队列。
			"TestPeekNextAgreesWithAdvance",
			"TestPeekNextDoesNotTouchTheQueue",
			// 只解析地址：备好了也是一个字节都没下（CDN 一次都没被碰）。
			"TestThePrefetchDoesNotDownloadAnything",
			// **提前量真的挂在「还剩多久」上**（这一票最核心的那条验收）：
			// 歌长到 15 秒之后才放完，量的是「备地址那一刻还剩多少」。
			"TestTheAddressIsPreparedWhileTheTrackStillHasTimeLeft",
			// 音乐库那一路（sid）也备得到——那是**另一条取流接口**。
			"TestAMusicLibraryEntryIsPreparedToo",
			// 作废的**真正保证**：下一首换成谁就放谁，陈旧的那份一个字节都不下。
			"TestAStaleAddressIsNeverPlayed",
		},
	},
	{
		// 🔴 **这一条的错法也是「静默」的**：读屏软件用户**听不见**任何东西——
		// 界面画得出来、测试全绿，而「下载下完了」「这一首放起来了」「搜出结果
		// 了」这些事**一句也没有人告诉他**。
		//
		// 而它们在画面上**一个字都不占**：下载完成只是下载记录页上那一列多了一
		// 枚绿勾，「这一首放起来了」只是播放条上多了一行歌名——而焦点不在那儿。
		//
		// 框架给的那个口子就是 `Context.Announce`（MyGo 自己的 toast 会自动念
		// 一句，但我们用的是自己那套 toast，见 `internal/ui/toast.go`），而我们
		// 此前**一个都没用**。
		Category: Platform,
		// 票 #321（2026-10-24）。
		//
		// ⚠️ **念和看是两件事**：`.Label(...)` 给的是「**这一枚是什么**」（那一枚
		// 按钮叫什么、那一行是谁），播报给的是「**刚刚发生了什么**」。`internal/ui`
		// 里那 95 处 `.Label`（带参数的那些才是给读屏软件的名字）一处都不许改成
		// 播报——那会让读屏软件用户听到一屏控件名字，而**一件正在发生的事一句
		// 也没有**。
		Spec: "状态变化被接到平台的无障碍播报上：加载完成、下载完成、播放失败、搜索完成各念一遍；同一件事一帧里只念一遍；`.Label`（这一枚是什么）一处都没改成播报",
		Tests: []string{
			// 加载完成（在线那一段真的走完了取流 + 开解码器）和播放失败。
			"TestALoadedTrackIsReadOut",
			"TestAPlaybackFailureIsReadOut",
			// 下载完成：下载队列那一头报一声，应用层念一句。
			"TestTheCallbackIsToldWhenAnItemIsDone",
			"TestADownloadThatFinishedIsReadOut",
			// 搜索完成（**真的那一支**：B 站给的命中总数，不是屏幕上那几行）。
			"TestASearchThatFoundSomethingIsReadOut",
			"TestTheSearchTotalIsTheOneThatIsAnnounced",
			// 快捷键录上了（界面上一个字都没有的那一种）。
			"TestARecordedShortcutIsReadOut",
			// 「一句只念一遍」（一帧建好几遍视图）+ 「提示也念一遍」。
			"TestTheSameThingIsNotReadOutTwice",
			"TestAToastIsAlsoReadOut",
			// **接线那一条**：应用层发生了一件事 → 界面层念出来（中间就靠
			// `AddAnnounce` 那一行，删掉它编译照样过、别的用例照样绿）。
			"TestAPlayingFailureReachesTheScreenReaderThroughTheRealWiring",
			// 看不见的那一扇窗不攒着；攒着的那几摊**有上限也有过期**。
			"TestAnAnnouncementIsNotKeptWhileTheWindowIsInvisible",
			"TestAnOldAnnouncementIsNotReadOut",
			"TestAtMostThreeAnnouncementsAreKept",
			// 念出去的是**人话**，不是英文系统错（读屏软件是逐字念的）。
			"TestTheAnnouncedErrorIsNotTheRawGoError",
			// **`.Label` 一处都没动**（这是这一票的另一半）。
			"TestTheLabelsAreNotTurnedIntoAnnouncements",
		},
	},
	{
		// 🔴 **这一条的错法也是「静默」的**：页头右侧那枚开关**一个字都没有**
		// （`Label` 是给读屏和测试器找它用的名字，屏幕上不画），于是用户看见
		// 一枚绿绿的旋钮，却不知道拨它会不会把自己的播放记录报上去——那是**写
		// 操作**，是隐私。界面上没有任何一样东西会报错。
		Category: Platform,
		// 票 #296（UX 报告 L-05）。
		Spec: "历史记录页头右侧是「开关 + 「记录播放历史」文案」，文案照抄上游原文；**没有清空键**（2026-10-24 拍板的取舍：要摆它得先把 `/x/v2/history/clear` 那条接口链接上）",
		Tests: []string{
			// 那句话**看得见**（画面上有两处叫这句话的节点，而且开关右边
			// 那一段真的有深色的字——只判 `HasText` 的话，开关自己的 `Label`
			// 就够数了）。
			"TestTheHistoryHeaderSaysWhatTheSwitchIs",
			// **没有**清空键（守成：别有一天顺手把它加回来）。
			"TestTheHistoryHeaderHasNoClearKey",
		},
	},
	{
		// 🔴 **这一条的错法也是「静默」的**：底色是中性灰而不是淡主色底，界面
		// 不会报错、不会崩——只是「哪一首在放」少了一个一眼就认出来的信号。
		//
		// 而它是从**上游对齐审计**里查出来的一条（报告 §1 的 S-11）：上游那一行
		// 是主色 20% 的淡绿底，我们一直是中性灰底。
		Category: Platform,
		// 票 #294（报告 §1 的 S-11，2026-10-24 拍板按上游改）。
		//
		// ⚠️ **DESIGN §七保护的是「主色当字色」的对比度，不是「主色当底色」**：
		// §七 不该被扩大解释。淡主色底是**状态识别**，底上放的是普通文字。
		Spec: "队列抽屉里「正在播放」那一行的**底色是主色 20% 那一层淡底**（浅色 #d2f7df / 深色 #193e29），**不是实心主色底**；底上的字（歌名 16 / 副题 14）**对比度都 ≥4.5:1**（深浅两套都量，且跟着用户自定义的主色重算）",
		Tests: []string{
			// 那一行的底就是那一层淡主色底（画面上量的，深浅两套），而且不是
			// 实心主色底、也和旁边那一行不一样。
			"TestTheQueuePlayingRowUsesAPalePrimaryBackground",
			// 底上的字够读（歌名 + 副题，深浅两套 × 六种主色）。
			"TestTheTextOnThePalePrimaryRowIsReadable",
			// 那几个数钉死（浅 #d2f7df / 深 #193e29，底是不透明的）。
			"TestThePalePrimaryRowKeepsItsNumbers",
		},
	},
	{
		// 🔴 **这一条的错法也是「静默」的**：那一行是实心主色底，界面不报错、不崩
		// ——只是「哪一集在放」和上游差着一个数，而**队列抽屉的「正在播放」那一行
		// 早就按上游改过了**（票 #294）：同一件事，两个抽屉差着两种画法。
		Category: Platform,
		// 票 #323（2026-10-24；BMG-173 的评审留的小账）。
		//
		// ⚠️ **它和票 #294 那一条是同一档**：上游那两处都是 HeroUI 的
		// `variant="flat" color="primary"`，展开成 `bg-primary/20 text-primary-600`。
		// 两个抽屉现在**共用同一份配方**（`internal/ui/playingrow.go`）。
		Spec: "分集抽屉里「正在放」的那一集的**底色是主色 20% 那一层淡底**（浅色 #d2f7df / 深色 #193e29），**不是实心主色底**；底上的字（标题 13 / 编号 12）**对比度都 ≥4.5:1**（深浅两套都量，且跟着用户自定义的主色重算）",
		Tests: []string{
			// 那一行的底就是那一层淡主色底（画面上量的，深浅两套），而且不是
			// 实心主色底、也和旁边那一行不一样。
			"TestTheDrawerMarksTheEpisodeThatIsPlaying",
			// 底上的字够读（标题 + 编号，深浅两套 × 六种主色）。
			"TestTheTextOnThePalePartRowIsReadable",
			// 那几个数钉死（浅 #d2f7df / 深 #193e29，标题 #127d38 / 编号 #6b6b74）。
			"TestThePartDrawerPlayingRowKeepsItsNumbers",
		},
	},
	{
		// 🔴 **这一条的错法也是「静默」的**：抽屉「啪」地一下出现、或者弹到窗口
		// 外面去就再也不回来——界面不报错、不崩，只是「来得很生硬」。
		//
		// 而「动的是布局位置、不是 transform」那一条在**软件渲染回退**时会差两个
		// 数量级（DESIGN §五），所以这一条把它一起钉住。
		Category: Platform,
		// 票 #298（2026-10-24 拍板：**分两类，不一刀切**）。
		//
		// ⚠️ **「小浮层不加」说的是这一票不加**：Toast 那 250ms 的入场是 #207
		// 自己定的（票 #180「Toast 语义色与入场」），而 #292 还要改它的曲线——
		// 本票没动它（下面最后一条证据把它钉住）。
		Spec: "队列抽屉从**右侧滑入**、往右滑出，**200ms** + `CubicBezier(0.16, 0.9, 0.21, 0.98)`，**不做回弹**；**开的时候先建面板再动，关的时候动完再拆**（退场那 200ms 里面板还在树上，滑完了才拆）；动画**不挡交互**（滑进去那 200ms 里面板和底下那片蒙版都点得动，退场途中再点一次外面**不会**把抽屉弹回来，而**退场那 200ms 里底下那片蒙版不吃点击**——它是透明的，看不见的东西吃点击就是「点不动」）；**动的是布局位置，不是 transform**；**每帧分配不涨**；菜单 / 小浮层 / 确认框 / Toast **不加**进出场",
		Tests: []string{
			// 方向和终点（画面上量的：面板左缘那道 1 像素的边线）。
			"TestTheQueueDrawerSlidesInFromTheRight",
			// 曲线和时长（滑到 100ms 时面板已经走完 95.4%）+ 不做回弹
			//（整条轨迹单调、不越过终点）。
			"TestTheQueueDrawerSlidesInOnThatCurveInThatTime",
			"TestTheQueueDrawerDoesNotBounce",
			// 开合和动画对齐：关掉之后**动完才拆**。
			"TestTheQueueDrawerIsTornDownOnlyAfterItSlidOut",
			// 退场那条曲线和时长（退到一半时退到 1182.5）——进场那一侧量了，
			// 退场这一侧也得量：不然「退场瞬移」测试照样绿。
			"TestTheQueueDrawerSlidesOutOnThatCurveInThatTime",
			// 动画不挡交互 + 关是幂等的（退场途中再点外面不弹回来）。
			"TestTheQueueDrawerDoesNotBlockClicksWhileItSlides",
			// 退场那 200ms 里底下那片蒙版**不吃点击**（它是透明的，看不见的
			// 东西吃点击就是「点不动」）。
			"TestTheQueueDrawerBackdropStopsEatingClicksWhileItSlidesOut",
			// 每帧分配没涨（进场那 200ms 和稳态一个数）。
			"TestTheQueueDrawerSlideDoesNotAllocatePerFrame",
			// 小浮层不加进出场（读语法树：确认框 / 菜单 / 弹窗 / 浮层那一批，
			// 一个动画调用都没有）。
			"TestTheSmallOverlaysHaveNoEnterOrExitAnimation",
			// Toast 那 250ms 的入场**没被这一票动过**（它是 #207 定的，#292 要改
			// 的是它的曲线不是它的时长）。
			"TestTheToastKeepsItsOwnEntryAnimation",
		},
	},
	{
		// 归「播放」那一组：还是侧栏，但说的是**两组收藏夹**（票 #371）。
		//
		// 归这一组是因为它是**界面上的一处交互**（和头像下拉那一层同一类），
		// 而不是一个接口。
		Category: Play,
		// 这一片是 2026-10-24 那次界面重设计（PR #357）之后**没跟上的一处**：
		// 重设计改了侧栏本身（底色用方案的 sidebar、选中项底是 accentSoft 淡底），
		// 而这一片还是老样子——它跟同一根侧栏里的菜单项放在一起是两套风格。
		//
		// ⚠️ **这一票只动样式**：取数、刷新、拖拽排序那一摊一个字没改（那是
		// #362 的地盘）。
		//
		// 🔴 **判据是「和同一根侧栏里的参照物对齐」，不是「某个具体的数」**：
		// 方案换了、主题的间距变了，那些数会跟着变，而「是不是一套」不会。
		//
		// ⚠️ **票 #270 之后「这一片的字都在同一列」只对「分组标题那一列」成立**：
		// 收藏夹那一行左边多了一枚方形封面，**名字让开它**（起在封面右边）。
		// 仍然是同一列的是**封面**——它和分组标题的字左对齐，正是参考图上的
		// 样子（「创建的歌单」那几个字和下面那些封面）。
		Spec: "侧栏里那两组收藏夹按当前风格画：左缩进和侧栏分组标题同一列、**封面和分组标题的字同一列**；分组标题和侧栏分组标题同一档（12 / 500 / 次要色）；**每一行是一枚方形封面 + 名称 + 「N 首」**（票 #270：封面和数量都是那两个列表接口本来就给的，不另发请求；「N 首」是 B 站上次统计的 `media_count`，不是实时的）；收藏夹名 14 的正文色、选中那一行是 accentSoft 淡底、悬停是 flat.Feel；折叠态那枚图标在收藏夹页上亮起来，点开的那层浮层是卡片那一档（surface1 / radiusLg）",
		Tests: []string{
			// 这一片和侧栏是同一块：左沿一样、封面和标题的字同列、名字让开了封面。
			"TestTheFavoritesAreaLinesUpWithTheSidebar",
			// 每一行是「图 + 名称 + N 首」（票 #270 的正题）。
			"TestEachFavoriteRowHasACoverAndACount",
			// 空状态那句也在那一列（它替下面那些收藏夹名说话）。
			"TestTheFavoritesEmptyLineLinesUpWithTheNames",
			// 分组标题和侧栏的分组标题是同一档（字号 + 字色）。
			"TestTheFavoritesGroupTitleIsTheSameKindOfTitleAsTheSidebarOne",
			// 折叠态那层浮层是**卡片**那一档（底 surface1，不是页面底）。
			"TestTheCollapsedFavoritesPanelIsACard",
			// 折叠态那枚图标在收藏夹页上要亮（当前在哪唯一的指示）。
			"TestTheCollapsedFavoritesIconLightsUpOnACollectionPage",
			// 这一片的字在侧栏底上 ≥ 4.5:1（四个方案 × 深浅两套）。
			"TestTheFavoritesTextIsReadableOnTheSidebar",
			// 这一片上面留了一截（它是侧栏的第三段）。
			"TestTheFavoritesAreaHasRoomAboveIt",
			// 选中那一行是 accentSoft 淡底 + 按那层底现算的字（既有那条）。
			"TestTheCurrentItemIsFlatEverywhere",
		},
	},
	{
		// 归「分页与路由」那一组：这一票动的是**翻页的闸门**。
		//
		// 🔴 **三个症状全是静默的**：页面停住不动、那个数偏大、界面上说一句
		// **对用户是错的**话——没有一样会报错，界面只是「不太对」。
		Category: Paging,
		// 票 #344（issue #344，BMG-179 / PR #330 的评审留的三条 minor）。
		Spec: "UP 空间「动态」那一页：丢掉的理由**分了类**（界面按类别说，「读不懂」不说成「形态不认识」，「投稿没有稿件号」不说成「什么也没有」）；丢掉的计数**按不同的动态算**（B 站翻页重叠时同一条只算一次）；**「到底了」看入口有没有前进**——0 条 + 入口前进了 = 继续拉下一页，0 条 + 入口没前进 = 到底",
		Tests: []string{
			// 分类（服务层那一层）：四类各数各的。
			"TestTheDroppedOnesAreToldApartByReason",
			// 分类（画面上那句话）：「读不懂」不许被说成「形态不认识」。
			"TestWhatWeCannotReadIsNotCalledAnUnknownShape",
			// 「投稿没有稿件号」是**另一类**（它带着正文），服务层和画面上各一条。
			"TestAnArchiveWithoutABvidIsItsOwnReason",
			"TestAnArchiveWithoutABvidIsNotCalledEmpty",
			// **正文也是空的那几条**仍算「什么也没有」（不算「投稿没有稿件号」）。
			"TestAnArchiveWithoutABvidAndWithoutTextIsEmpty",
			"TestAnArchiveWithoutABvidAndWithoutTextIsCalledEmpty",
			// 加了一类而忘了加一档时，计数**不许静默少一条**。
			"TestAnUnknownDropReasonIsStillCounted",
			// 计数去重：两页里重叠的一条，计数是 1 不是 2。
			"TestTheDroppedCountCountsDifferentDynamicsNotHowManyTimesTheyAppear",
			// 跨页去重靠的是**身份**（服务层那一层）。
			"TestTheSameDroppedDynamicInTwoPagesIsTheSameOne",
			"TestADroppedOneWeCannotReadAtAllStillHasAStableIdentity",
			// 闸门：0 条 + 入口前进了 → 继续拉；0 条 + 入口没前进 → 到底。
			"TestAnEmptyPageWhoseOffsetMovesStillLoadsTheNextPage",
			"TestAnEmptyPageWhoseOffsetStaysIsTheEnd",
			// 闸门的**界面那一半**：「0 条 + 入口没前进」要从界面这一侧也收手
			//（入口前进了的那一次必须问出去，入口没前进就停）。
			"TestThePagerStopsWhenAnEmptyPageKeepsTheSameOffset",
		},
	},
	{
		// 票 #387：随机档的洗牌袋是**池子**，而「下一首播放」压过随机。
		//
		// 随机不是「排好一张表」，是「下一首从还没放过的里面随机挑」（持续随机——
		// 行业做法：Spotify / Apple / QQ音乐 / 网易云都是这一个东西）：新加进队列的
		// 歌**立刻就在候选里**，这一轮就轮到它，而不是等下一轮重装。
		//
		// 而「下一首播放」是**用户显式的意图**：它压过随机（Spotify 社区管这叫
		// "queue overrides"）——随机档下按它，**下一首就是它**。
		//
		// ⚠️ **顺序 / 循环 / 单曲三档一个字都不许动**：袋子是随机档的东西，而那一
		// 档的下一首是纯按索引算的。
		Category: Play,
		Spec:     "随机档的洗牌袋是池子（这一轮中途加进来的歌这一轮就轮到它，不重复也不遗漏）；「下一首播放」压过随机（下一首就是它）；其余三档按索引算，一个字都不变",
		Tests: []string{
			// 池子：中途加进来的歌这一轮就轮到它，而且只轮到一次。
			"TestRandomPlaysASongAddedMidRound",
			// 「下一首播放」压过随机：手动按下一首、自动切歌两条路都认。
			"TestRandomPlayNextIsTheNextOne",
			"TestRandomPlayNextIsTheNextOneOnAutoAdvance",
			// 反面：其余三档一个字都没变（回归的护栏）。
			"TestPlayNextOutsideRandomIsStillByIndex",
			// 界面上说得清（随机档才说，其余三档一句话都不说）。
			"TestTheEnqueueNoticeIsOnlyForRandom",
			"TestAddingASongInRandomModeSaysItWillPlayThisRound",
			"TestAddingASongOutsideRandomModeSaysNothing",
		},
	},
	{
		// 🔴 **这一条的错法也是「静默」的**：用户右键封面按了「复制歌曲信息」，
		// 什么都没发生——而他以为复制好了（按下去没有报错、没有弹窗、没有声音）。
		// 而「复制封面」那一档更坏：写进去的是**空字节或者一段坏字节**，用户
		// 在别的应用里粘出来的是**一张打不开的图**。
		//
		// 而**没有封面时按不动那一项、也不说为什么**是同一种坏处：用户以为
		// 程序坏了。
		Category: Platform,
		// 票 #556（issue #556）。
		//
		// ⚠️ **这一组量的是「剪贴板里到底有什么」**，不是「调了哪个函数」——
		// 后者是实现，前者是用户看见的东西（文本要逐字，图要能解开）。
		Spec: "当前这一首的信息能复制成可读文本（歌名 / UP 主 / 时长 / BVID / 链接 / 路径，**没有的字段整行不出现**，没有多余前缀，歌名和界面上摆的是同一个）；当前封面能复制成一张**能解开的 PNG**；**没有封面时那一项按不动且说清楚为什么**，也不往剪贴板里写图；复制失败有**看得见的反馈**；键盘（菜单键 / Shift+F10）够得着这一份菜单",
		Tests: []string{
			// 文本：逐字（**多一个前缀用户粘进聊天框还得手删一行**）。
			"TestTheSongInfoTextHasEveryField",
			// 文本：**没有的字段整行不出现**（留一行空的「UP主：」更难读）。
			"TestTheSongInfoTextOmitsTheFieldsThatAreNotThere",
			// 文本：**音频 id 不是 BVID**（报错了用户拿着它搜会搜到别的东西）。
			"TestTheSongInfoTextDoesNotCallAnAudioIDSomethingItIsNot",
			// 文本：歌名**和界面上摆的是同一个**（清洗过的那一份）。
			"TestTheSongInfoTextUsesTheNameOnScreen",
			// 文本：没有多余前缀。
			"TestTheSongInfoTextHasNoExtraPrefix",
			// 菜单：那两项**真的在菜单里**（少建一项那一项就永远点不到）。
			"TestTheCoverMenuOffersTheTwoCopyActions",
			// 走菜单按下去，文本**真的进了剪贴板**。
			"TestChoosingCopySongInfoPutsTheTextOnTheClipboard",
			// 封面：剪贴板里那张**能被解开**（别的应用要能贴）。
			"TestCopyCoverPutsAPNGOnTheClipboard",
			// 封面：**交出去的是原图**（不是缓存里缩到 768 的那一份），
			// 而且**EXIF 摆正了**（不摆正的话交出去的是一张横躺着的图）。
			"TestSourceGivesTheOriginalBytes",
			"TestSourceImageIsNotScaled",
			// 封面：**交出去的和缓存里存着的不一样**（只断「尺寸是原图的尺寸」
			// 的话，缓存没真的存下缩过的那一份，就没东西可比）。
			"TestSourceImageIsNotTheStoredOne",
			"TestSourceImageStraightensTheEXIFOrientation",
			// 摆正的结果**和框架自己摆的是同一个**（对着框架比，而不是对着
			// 一份抄过来的映射比——而且是**逐像素**比：只比宽高的话，方向 2/4
			// （翻转那一批）和方向 6/8（转置那一批）全都分不出来）。
			"TestTheOrientationMatchesTheFrameworksOwn",
			// 取不到的时候**报错**（而不是给一段空字节——空字节粘出来
			// 是一张打不开的图）。
			"TestSourceOnAnEmptyURLSaysSo",
			"TestSourceAfterCloseSaysSo",
			// 没有封面：**不写图**，那一项是灰的，而且说清楚为什么。
			"TestTheCopyCoverItemIsGreyWithoutACover",
			"TestCopyCoverWithoutACoverWritesNothing",
			// 失败**不静默**：文本那一档和封面那一档各一条。
			"TestAFailedCopySaysSo",
			"TestAFailedCoverCopySaysSo",
			// 失败**不静默**：**编码**那一档（取到了图但编不成 PNG）——
			// 「取不到」那一档在 `png.Encode` 之前就返回了，所以它盖不住这一档。
			"TestAFailedCoverEncodeSaysSo",
			// **后按的那一次赢**：封面复制在后台跑，完成顺序和按下去的顺序
			// 可以不一样——先发起的封面那一趟**不许覆盖**后来的复制内容。
			"TestTheLastCopyActionWinsOverAnEarlierCover",
			"TestAnEarlierCoverDoesNotOverwriteALaterCover",
			// **判号和写在同一个主线程段里**：旧封面的段**还在队列里排队**
			// 的时候，用户按的文本**先**落地；旧封面的段随后执行时，判号
			// 已经不通过了——所以它**一个字都不写**（不是落地了再改回来）。
			"TestACoverAlreadyInsideTheWriteDoesNotWinOverALaterText",
			// **一轮里只有一段在跑**：主线程是单线程的，所以一个段跑着的
			// 期间另一个段进不来（这一条量的是「串行」这个前提本身）。
			"TestOnlyOneSectionRunsAtATimeOnTheMainThread",
			// **两档真的是两档**：主线程档**绕过队列**直接执行，后台档
			// **排队**——而那个差别**就是那个竞态的机制**（桩把两档混成
			// 一样的话，那个交错压根不会发生，而测它的用例照样是绿的）。
			"TestTheMainThreadSectionGoesStraightThroughWhileABackgroundOneWaits",
			// 键盘：Tab 走到封面上，菜单键能开出这一份菜单。
			"TestTheMenuKeyOpensTheCopyMenuOnTheCover",
		},
	},
	{
		// 票 #536：迷你播放器点封面展开歌词卷帘。
		//
		// ⚠️ **这一组是「双平台」那一类**：它动的是**窗口**（改运行时尺寸）、
		// **状态落盘**（`StateKey` 记宽高）和**设置**（那一格开关）。
		//
		// ⚠️ **下面那几条只断「我们交给框架的东西」**：无头 Tester 里**没有真
		// 窗口**，所以量不到「窗真的变高了」「`window-state.json` 里真的写着
		// 84」「卷帘真的被工作区夹住了」「Esc 到不到得了这一扇窗」——那四样
		// 记在 `internal/ui/miniplayer_test.go` 末尾那一节「真窗口才量得到」
		// 里（人工清单，不在台账里装）。
		Category: Platform,
		Spec:     "迷你播放器点封面展开歌词卷帘（点封面 / Esc / 收起键三条路都收；开关默认开着，关着时点封面什么也不发生；展开着时把开关关掉 ⇒ 立刻收起）",
		Tests: []string{
			// 展开 / 收起的三条路。
			"TestTheMiniCurtainOpensWhenTheCoverIsClicked",
			"TestTheMiniCurtainClosesWhenTheCoverIsClickedAgain",
			"TestTheMiniCurtainClosesWithTheCollapseButton",
			"TestTheMiniCurtainClosesWithEscape",
			// 窗口高度 = 84 + 卷帘高（那一扇窗是透明的，多出来的那一块照样吃
			// 点击）。
			"TestTheMiniCurtainHeightFollowsTheAnimation",
			// 设置开关：默认开着 / 关着时什么也不发生 / 展开着时关掉立刻收起 /
			// 落盘。
			"TestTheMiniLyricsCurtainSwitchIsOnByDefault",
			"TestTheMiniCurtainSwitchIsOnTheSettingsPage",
			"TestTheMiniCurtainDoesNothingWhenTheSwitchIsOff",
			"TestTheMiniCurtainCollapsesWhenTheSwitchIsTurnedOff",
			"TestTheMiniLyricsCurtainSwitchSurvivesARestart",
			// 卷帘不超出工作区（纯逻辑那一半）。
			"TestTheMiniCurtainIsClampedToTheWorkArea",
			// 「立刻收起」真的是一下子到位（而不是先弹回 320 再滑下去）——
			// 而那 240ms 里窗比内容高（透明的那一块在吃点击）。
			"TestTheMiniCurtainCollapsesAtOnceWithoutBouncingBack",
			// 窗口状态落盘：展开着关窗 ⇒ 落盘的尺寸是 320×84（`StateKey` 记
			// 宽高，而抓的是关窗那一刻）。
			"TestTheMiniCurtainLeavesACollapsedHeightInTheWindowState",
			// 展开的那一下把键盘抢到这一扇窗上（不然 Esc 到不了它）。
			"TestTheMiniCurtainFocusesItsWindowWhenItOpens",
			// 兜底：应用真的成了前台的时候，再抢一次 key 窗口
			// （`activateIgnoringOtherApps:` 是异步的，第一下可能落空）。
			"TestTheMiniCurtainTakesTheKeyWindowWhenTheAppBecomesActive",
			// 收起了就不再惦记抢键盘（不然用户在做别的事时会被抢走）。
			"TestTheMiniCurtainStopsTakingTheKeyWindowAfterItCollapses",
		},
	},
	{
		// 票 #557：系统把一个音频文件交给本应用（双击、拖到程序坞图标）。
		//
		// 🔴 **这一族全是「静默」的坏法**：登记漏了、回调装晚了、扩展名对不上，
		// 三样都不会报错——症状都是「双击一个文件，应用起来了而什么也没放」。
		//
		// ⚠️ **三份口径必须一样**：`mygo.json` 的 `fileAssociations`（系统按它
		// 登记）、框架按它过滤命令行（`mygo/deeplink.go` 的 `fileArgs`）、界面
		// 按它判收不收（`internal/platform` 的 `AssociatedAudioExts`）。
		//
		// ⚠️ **登记的那一份是「真放得出声」的那一份**，不是扫描白名单——
		// 扫描白名单里有 `.wma`，而我们没有 ASF 解码器（PLAN §5.4）。
		//
		// ⚠️ **队列语义是行级「立即播放」那一档**：追加到队尾并打断，**不清空**
		// 用户已经排好的队列（DESIGN.md §十第 4 条）。
		Category: Platform,
		Spec:     "系统把一个音频文件交给本应用：已有的实例被叫回，主窗打开并放它（追加队尾并打断，不清空队列），不起第二个实例；收不下的（文件不在了、不是普通文件、格式放不了）有可见提示，而且不打断正在放的那首",
		Tests: []string{
			// 登记的那一份（`internal/platform`）：内容、`.wma` 不在里面、
			// 带点和不带点是同一个格式。
			"TestTheAssociatedExtensionsAreThePlayableOnes",
			"TestWMAIsNotAssociated",
			"TestIsAssociatedAudioExtAcceptsBothSpellings",
			"TestAudioExtOfTakesTheExtensionWithoutTheDot",
			// `mygo.json` 与代码里那一份**是同一份**，且扩展名不带点
			//（带点会被 MyGo 的配置校验器拒绝，打包在读配置那一步就失败）。
			"TestMygoJSONDeclaresTheSameExtensions",
			"TestMygoJSONExtensionsCarryNoDot",
			"TestMygoJSONStillDeclaresTheURLScheme",
			// 框架按什么过滤命令行里的文件（照抄框架那三行）。
			"TestTheFrameworkFiltersCommandLineFilesByTheseExtensions",
			// 界面那一半：放起来了、队列没被清空、界面说了话。
			"TestOpeningAFilePlaysItAndKeepsTheQueue",
			// 迷你开着时收到文件：让开迷你，主窗口上放。
			"TestOpeningAFileMakesRoomForTheMainWindow",
			// 回调在 `Run` 之前装上了（装晚了那一次就永远收不到）。
			// ⚠️ **走生产那条安装点**（`installLifecycle`）：删掉它里面那一行
			// → 这一条红（第二轮评审实测过：上一版这一条是手工装钩子、
			// 手工调回调，删掉生产那一行照样绿）。
			"TestTheOpenFileHookIsInstalledBeforeRun",
			// 第二个实例把命令行转交过来那一档也装上了（它同样要认下框架
			// 丢掉的那一部分）。
			"TestTheSecondInstanceHookIsInstalledBeforeRun",
			// 三种没收下要说成三件不同的事，而且不打断正在放的那首。
			"TestCheckOpenFileTellsTheThreeRejectionsApart",
			"TestOpeningAMissingFileSaysSoAndKeepsPlaying",
			"TestOpeningAnUnsupportedFormatSaysSo",
			"TestOpeningADirectorySaysSo",
			// 「格式放不了」那一句要说清楚支持哪些格式。
			"TestTheUnsupportedTextSaysWhichFormatsWeTake",
			"TestTheMissingTextSaysTheFileIsGone",
			// 🔴 **「看得见」那一条**：不是「状态里记着」，而是**画面上真的有**
			//（`repaint` 那一格要了帧）。主窗口那一扇、迷你那一扇各断一遍——
			// 迷你**不画 toast**，而它常常就是用户眼前那一扇。
			"TestARejectedFileIsVisibleOnScreen",
			"TestARejectedFileIsVisibleWithTheMiniPlayerOpen",
			// 提示写进状态之后**自己会要一帧**（不然界面停在旧帧上）。
			"TestTheToastIsPaintedOnTheNextFrame",
			// 命令行那一侧：框架在 `OnOpenFile` 之前丢掉的那一部分，
			// **我们自己在命令行这一侧认下来**，而用户看得见。
			"TestTheFrameworkDropsThosePathsBeforeOnOpenFile",
			"TestTheLaunchArgsPickUpWhatTheFrameworkDrops",
			"TestTheCommandLineRejectionsAreVisible",
			"TestTheSecondInstanceArgsAreCheckedForTheOnesTheFrameworkDrops",
			// 框架会交的那些我们不重复处理（重复了队列里两份同一首歌）。
			"TestALaunchArgThatTheFrameworkDeliversIsNotHandledTwice",
			// ⚠️ **Windows 的盘符不是 scheme**：`C:\歌.flac` 被 `url.Parse`
			// 认成 scheme `c`，而按 `u.Scheme != "file"` 判的话**整个
			// Windows 的文件关联这一档会被自己扔掉**。
			"TestTheLaunchArgKeepsTheWindowsDriveLetter",
		},
	},
	{
		// 票 #559（板子任务 #297，2026-10-26）：后台下载完成时弹一条系统通知。
		//
		// ⚠️ **判据是「一扇窗都没有『有焦点**且**看得见』」**：「窗口全部隐藏」是
		// 一档**独立**的状态，不许靠「隐藏必然先失焦」那个隐含顺序——`OnHide` 与
		// `OnBlur` 是两个回调，谁先到不由我们决定。
		//
		// ⚠️ **迷你播放器那一扇的焦点要算**：它是 Panel（点它不激活应用），可它
		// 在 macOS 上照样能拿 key（实测 2026-10-26，真窗口）。不算它的话，迷你
		// 模式下每一次后台下载完成都会弹一条通知，而用户正看着迷你播放器听歌。
		//
		// ⚠️ **「互相切换」那一瞬靠「等一等」而不是「判得准」**：主窗失焦到迷你窗
		// 拿到焦点之间有一个「两扇都没有焦点」的瞬间（实测 35~60 毫秒），而通知
		// 是立刻弹的——所以判定延后一小会儿再判一次。
		Category: Platform,
		Spec:     "用户不在应用里（没有任何一扇窗『有焦点且看得见』，或窗口全部隐藏）时，后台下载完成弹一条系统通知；用户在应用里时不弹；主窗与迷你窗互相切换不误判；点通知回主窗的下载记录页；权限拒绝 / 不支持时静默降级成应用内提示而不丢事件（那句话要等用户回来还在）；同一任务用稳定 ID，同一版本不重复提醒；「有更新了」那一档另有一个独立开关；任一扇窗重新拿到焦点时清掉通知中心里的那些（**包括上一次运行留下的**——哪怕这一轮一次通知都没发过）",
		Tests: []string{
			// 判据本身：聚焦 / 失焦 / 迷你那一扇 / 全隐藏 / 窗口还没建出来 /
			// 关掉的那一扇窗要摘掉。
			"TestTheFocusSnapshotSaysWhenTheUserIsAway",
			"TestTheMiniPlayerCountsAsTheUserBeingHere",
			"TestAllWindowsHiddenMeansTheUserIsAway",
			"TestAHiddenWindowDoesNotCountAsTheUserBeingHere",
			"TestAHiddenWindowIsAwayEvenWhileItStillHasFocus",
			"TestNoWindowYetIsNotTheUserBeingAway",
			"TestAClosedWindowIsNotCounted",
			// 互相切换那一瞬不误判（靠宽限期）。
			"TestSwitchingBetweenOurOwnWindowsIsNotLeavingTheApp",
			// 该发的时候真发了、该静默的时候真静默。
			"TestDownloadDoneIsAnnouncedWhenTheUserIsAway",
			"TestDownloadDoneIsSilentWhileTheUserIsHere",
			// 稳定 ID（同一任务不堆两条）。
			"TestTheNotificationIDIsStable",
			// 点通知回到对应页面。
			"TestTheNotificationGoesToTheDownloadList",
			"TestTheUpdateNotificationGoesToTheSettings",
			"TestAnUnknownNotificationGoesNowhere",
			// 任一扇窗重新拿到焦点就把通知清掉（**包括上一次运行留下的那些**）。
			"TestReturningToTheAppClearsTheNotifications",
			"TestAFreshProcessStillClearsTheNotificationsOfEarlierRuns",
			// 发不出去时不丢事件。
			"TestAFailedNotificationStillSaysItInTheApp",
			"TestAFailedNotificationStillSaysItWhenTheUserReturns",
			"TestTheStickyNoticeIsReleasedWhenTheUserComesBack",
			"TestTheStickyNoticeIsOnScreenWhenTheUserComesBack",
			// 同一个版本只提醒一次（稳定 ID 不代替它）。
			"TestTheSameVersionIsNotNotifiedTwice",
			"TestANewVersionIsNotifiedAgain",
			// 用户在前台发现新版本，等他离开之后**仍然**能收到那一条（两层
			// 合起来量：应用层那一份记账也得退回来）。
			"TestAVersionFoundWhileTheUserIsHereIsNotifiedWhenTheyLeave",
			// 「更新提醒」那一格在设置页上摆着。
			"TestTheUpdateNotificationSwitchIsOnTheSettingsPage",
			// 应用层那一段接线（下载完成 / 后台查到更新会递出去）。
			"TestADownloadThatFinishedReachesTheNotifier",
			"TestABackgroundCheckTellsTheNotifier",
			"TestTwoBackgroundChecksDoNotNotifyTheSameVersionTwice",
			// 退回来那一份之后，同一个版本还能再提醒一次。
			"TestAReleasedVersionIsNotifiedAgain",
			"TestAReleaseDoesNotClearAnotherVersion",
			"TestTheUpdateNotificationSwitchTurnsItOff",
			"TestTheUpdateNotificationSwitchIsOnByDefault",
			// 平台层那一段（问过没有 / 拒了不再问 / 失败交给上层）。
			"TestANotificationIsNotSupportedBeforeTheAppIsReady",
			// 「刚起来的进程」那一份账是「还没问过」那一档——
			// 而**零值**就是那一档（否则打包的 macOS 应用一次都不会去问
			// 框架，于是弹不出系统通知，而它静默降级成应用内提示）。
			"TestAFreshProcessHasNotAskedYet",
			"TestTheZeroValueOfTheSupportStateMeansNotAskedYet",
			"TestTheSupportAnswerIsAskedOnlyOnce",
			"TestADeniedNotificationIsNeverAskedForAgain",
			"TestTheFailureIsHandedToTheUpperLayer",
		},
	},
	{
		// 票 #558：删本地音乐文件**改成移到系统废纸篓**（原来是永久删除）。
		//
		// ⚠️ **语义变了，而那一半是静默的**：以前界面上写着「不可恢复」，
		// 现在文件进的是废纸篓、用户找得回来——留着那句话就是**对用户说假话**。
		// 而**反过来**的那一种错更贵：悄悄回退到永久删除的话，界面上一个字
		// 都不会变，用户以为文件在废纸篓里、而它已经没了。
		//
		// 🔴 **所以「文件还在不在」是这一条的主判据**，不是「有没有报错」：
		// 成功那一条断「原处没了**而且**废纸篓里有」，失败那几条断「**原处
		// 还在**」。只断前者的话，一个「偷偷 os.Remove」的实现照样全绿。
		//
		// ⚠️ **失败那一条不许有「回收站不可用就永久删」的兜底**：框架那一侧
		// 自己就把「本来会被永久删除」的那一次拒掉了（Windows 的 progress
		// sink），而用户看到的是「文件未删除」——那句话是**真话**。
		Category: Negative,
		Spec:     "删本地音乐文件是**移到系统废纸篓**而不是永久删除：成功时原处没了、废纸篓里有（找得回来）、界面上说「已移到系统废纸篓」；失败时**文件还在原处**、界面上说「文件未删除」、那一行还在列表里；**文件本来就不在了**（用户先在访达里删了）时那一行照样没掉、而界面上说「已经不在了」——**三种结果不许混成两种**，「已移到系统废纸篓」只在真的搬进去的时候说；回收站不可用（网络盘 / 跨卷 / 系统不提供）时报一个用户看得懂的错而**不删**；弹窗上不再承诺「不可恢复」",
		Tests: []string{
			// 成功那一条：搬走 + 找得回来 + 话说了。
			"TestDeleteMovesTheFileIntoTheTrash",
			"TestDeletingMovesTheFileToTheTrashAndSaysSo",
			// 失败那几条：文件还在原处（安全线），而且话说了。
			"TestDeleteLeavesTheFileAloneWhenTheTrashFails",
			"TestATrashThatIsNotAvailableKeepsTheFileAndTheRow",
			"TestATrashThatFailsSaysTheFileIsStillThere",
			// 🔴 **失败的时候不许说「已移到系统废纸篓」**：原来错误分类对整段
			// 系统原话做子串匹配，而 Windows 那边每一句失败的话都带同一截
			// 「to the recycle bin」——拿它当判据的话，权限失败 / 已存在 /
			// 文件不存在全被归成同一档；而一个叫「Not Found.mp3」的文件更
			// 是被判成「文件已经不在了」，于是界面说成功了、列表那一行也
			// 消失了，而文件好好地躺在原处。
			"TestATrashThatFailsNeverSaysItMovedToTheTrash",
			"TestDeleteDoesNotCallAMissingFileASuccess",
			"TestClassifyTrashErrorSortsTheWindowsHRESULTs",
			"TestClassifyTrashErrorDoesNotLetTheFileNameDecide",
			"TestClassifyTrashErrorDoesNotReadTheWordsInsideQuotes",
			// 🔴 **「文件本来就不在了」不许说成「已移到系统废纸篓」**：那句话
			// 承诺用户能从废纸篓里恢复，而它压根没进过废纸篓。
			"TestAFileThatIsAlreadyGoneNeverSaysItMovedToTheTrash",
			"TestDeleteSaysTheFileIsAlreadyGoneWhenTheTrashFails",
			"TestDeleteSaysTheTrashIsNotAvailable",
			"TestDeleteSaysTheNameIsTaken",
			// 「没装回收站」：报错，**不删**。
			"TestDeleteWithoutATrashIsAnError",
			// 界面上不许说谎。
			"TestTheDeleteDialogNoLongerPromisesItIsUnrecoverable",
			// 交给回收站的是**词法上的那一条**（符号链接那条安全线的另一半）。
			"TestDeleteHandsTheLexicalPathToTheTrash",
		},
	},
}
