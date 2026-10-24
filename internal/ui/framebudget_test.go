package ui

// 逐帧分配预算（票 #156 那一组里的一条）。
//
// # 这条测试量的是什么
//
// 「界面变慢了只能靠人感觉」——而**时间**类的判据在这台机器上会被负载淹没
// （AGENTS §2：不要用固定帧数 / 固定毫秒当判据）。所以这里量的是**分配**：
//
//	每帧分配多少次 · 每帧分配多少字节 · 每帧存活堆涨多少 · 每帧建了几遍
//
// **分配次数不受机器快慢影响**：它是「这一帧建了多少个对象」的计数，不是
// 「建它们花了多久」。机器忙一点慢一点，这个数是同一个——满载的 CI 上跑
// 出来也是同一个数。
//
// # 四个界面，四条上限
//
// 先只覆盖最常用的四个（首页列表 / 播放条 / 队列抽屉 / 满屏播放器），不全量。
// 全量的意思是「每一个界面都有一条预算」，那要等这些界面的画法稳定下来再说。
//
// # 「隔离的那一块」和「整窗」是两处，量的不是同一件事
//
//   - **分配预算**在**隔离**里量：只画那一块（`withView`），不画整窗。理由是
//     **归因**——整窗的分配里混着侧栏、顶栏和别的页面，超了以后报不出
//     「是哪一块多分配的」，而那条信息正是要它的人需要的。
//   - **「一帧建了几遍」**在**整窗**里量：「一帧建两遍」坏的是**两个调用方
//     都建了它**（满屏开着的时候抽屉由满屏那一支建，`viewMain` 必须让开），
//     隔离起来就看不见那一半了。
//
// # 那两档要放歌的界面，**不放真音频**
//
// 播放条和满屏播放器喂的是一份**手摆的状态**（`budgetState`），不是真放一首
// 文件：真放的话解码器那条 goroutine 会在测量窗口里分掉一大笔（一次性缓冲，
// 实测约 130 KB），落在哪一帧还是随机的——那是**音频**的成本，却会让「这一帧
// 花了多少」由它决定，而判据要分得清「噪声」和「回归」。
//
// # 与真窗口的帧耗时**不挂钩**
//
// 这里一个字都没量时间。真窗口一帧多少毫秒跟机器、跟显示器、跟后台负载都
// 有关系，是**另一张票**的事；而那条判据天生会在满载的 CI 上飘红，而它红的
// 时候和被测的逻辑一点关系都没有。这一条只数「建了多少」。
//
// # 为什么两条测试都开 ReduceMotion
//
// **因为 Tester 的钟站着不动**（框架的说明写在 `mygo/ui/headless.go`）：
// 凡是「动一下才到位」的东西——共享元素转场（满屏那个封面从播放条长过去）、
// 滑块的滑动、渐隐渐现——在 Tester 里**永远走不完**，于是那一帧会被反复画
// 二十遍（MyGo 的 Tester 一次 `Frame()` 最多画二十帧），「一帧」这个单位
// 就废了。
//
// `ReduceMotion` 是系统里本来就有的那个偏好（macOS 的「减弱动态效果」）：
// 开了它，框架让那些值**立刻到位**。它只改「值怎么过去」，不改「建了
// 什么」——所以它不会影响这一组要量的任何数，而它让「一帧就是一帧」成立。
//
// ⚠️ 真机上是用户自己开不开这个偏好的事；这里开它是为了让**测量的单位**
// 成立，不是为了让界面少动。

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	mygoui "github.com/egoist/mygo/ui"
)

// budgetFrames 是每一块量多少帧。
//
// 一百帧：够把「偶发一次」的噪声摊平（背景线程偶尔分一点、GC 的时机），
// 又不至于让这条测试跑成一条慢测试（实测四个界面各一百帧，加起来不到一秒）。
const budgetFrames = 100

// warmFrames 是量之前先画多少帧。
//
// 头几帧里有几笔**只此一次**的开销：字形进图集、元素池涨到这一屏的规模、
// 首页那排 Tab 的滑块第一帧才量得到位置（它第一帧会主动要一帧，见 tabs.go）。
// 不先付掉这一笔，量出来的就是「第一帧的成本」而不是「稳态的成本」。
const warmFrames = 8

// liveHeapBudget 是每一帧**不许留下**的字节数（四个界面共用一个）。
//
// **基线是 0**：稳态下画一百帧，强制 GC 之后活着的堆和之前**一样多**
// （实测四个界面都在 0 附近，多数时候是**负的**——GC 收走的比画帧留下的多；
// 头一轮量到的负数能到几 KB，那是预热那几帧的垃圾被收走了）。
//
// 上限 2 KB：噪声的十几倍，而「每帧留下 2 KB」在 60 帧每秒下是每分钟 7 MB——
// 那已经够把一台机器的内存吃掉了，值得拦。
const liveHeapBudget = 2048

// frameBudget 是一块界面每一帧的分配上限。
//
// 两个数都是**上限**（不是基线）：基线写在各自的注释里，上限是基线加上
// 一截余量。为什么要余量：分配次数是**别的线程**也会动的全局计数
// （后台线程偶尔分一点），而界面自己的分配是死数。
type frameBudget struct {
	// allocs 是每帧分配次数的上限。
	allocs float64
	// bytes 是每帧分配字节的上限。
	bytes float64
	// floor 是每帧分配次数的**下限**。
	//
	// 它防的是「这一屏几乎没画东西」：上限是「不许超过」，而没有下限的话
	// 「一帧什么都不画」也是过（分配次数掉到零，谁都拦不住）——那样这三条
	// 预算就成了空话，而「这一屏还在画」这件事恰恰是它们唯一的前提。
	//
	// 取基线的一半：真的少画了一半（比如某一整块被摘掉了）会红，而机器负载
	// 那点抖动不会（实测四个界面的分配次数是死数，一模一样）。
	floor float64
}

// frameCost 是量出来的「每一帧花了多少」。
type frameCost struct {
	// allocs 是每帧分配次数。
	allocs float64
	// bytes 是每帧分配字节数。
	bytes float64
	// live 是每帧存活堆增量（正数 = 留下了东西）。
	live float64
}

// budgetScreen 是一块界面的量法。
type budgetScreen struct {
	// name 是这一块叫什么（子测试的名字、报错时的那句话）。
	name string
	// budget 是这一块每一帧的分配上限。
	budget frameBudget
	// setup 把这一块摆成「用户看得见的样子」（造好外壳之后、量之前）。
	setup func(t *testing.T, s *Shell, tt *mygoui.Tester)
	// draw 是**只画这一块**的那一支（`withView` 用它替掉整窗）。
	draw func(s *Shell) func(*mygoui.Context)
}

// budgetScreens 是这四条预算。
//
// ⚠️ **每一档数字都写着取基线时的实测值和「为什么是这个数」**——数字本身
// 没有意义，意义在它和基线之间的距离。改代码之后如果超了，先看注释里
// 那个基线：基线也涨了（这一屏真的多画了东西）就把上限跟着抬，并在这里
// 写清新的实测值；基线没涨而超了，就是**多分配了**——那是这条测试要抓的。
func budgetScreens() []budgetScreen {
	return []budgetScreen{
		{
			// 首页列表：三个 Tab 那一屏（默认在「音乐」档），底下是推荐单。
			//
			// **基线（实测）**：每帧 123 次分配、3464 字节。
			// 摆的是 1200×800 的窗口、音乐档 30 行歌。
			//
			// ⚠️ **基线从 151 降到 123（票 #313）**：那一票把「每帧每行现算」的
			// 两样东西**记好了一份**——行身份（装进 `any` 那一步）和三处显示
			// 文字（时长 / 播放量 / 行尾那枚「⋯」的无障碍名字）。它们原来是
			// **每帧每一行各算一次**，而这一屏一屏就是十来行：省下来的是
			// 28 次（十来行 × 两样 + 那一行自己那份）。
			//
			// ⚠️ **基线从 149 涨到 151 那一次**：票 #244 给了首页 Tab 那一排右边
			// 一枚「全部播放」（Biu 那一页本来就有，我们此前故意没摆）。多出来的
			// 是**那枚按钮**（一枚 `ui.PrimaryButton` 每帧两个分配），不是别的——
			// 票 #244 同时给封面加的那一圈边**没有多分配**：它是加在图片元素
			// 自己身上的（`Border(1, …)`），不是另建一个盒子，而封面是列表里
			// **每一行**都有的（另建一个就是每帧多十来次，队列抽屉那一档的上限
			// 只比基线多 6 次）。
			//
			// **为什么上限是 138 次 / 4400 字节**：次数只比基线多 15（**一成
			// 出头**），字节多两成七。这一屏是四个里最贵的一块（一屏十来行歌，
			// 每行十几个元素），而回归的形状是「每帧重算一遍什么」——**每行
			// 格式化一次歌名**（十来行就是二十来次）就该超，而实测那种回归是
			// 179 次：红 ✓。所以这一档的次数余量是**按「一行一次」那档回归
			// 定的**，不是按「几十上百次」定的——后一种（每帧重建一份列表），
			// 一成出头的余量照样照得出来。
			//
			// 字节那一档留两成七：它管的是**少而大**（每帧新建一块大缓冲），而
			// 那种回归一次就是几 KB 到几十 KB（实测 16 KB 那一块：20336 字节，
			// 红 ✓）。
			name: "首页列表",
			budget: frameBudget{
				allocs: 138,
				bytes:  4400,
				floor:  60,
			},
			setup: func(t *testing.T, s *Shell, _ *mygoui.Tester) {
				// 三十行：听了一晚上的量级。列表是虚拟的（只建看得见的那
				// 几行），所以这个数是「一屏」而不是「三十行」。
				s.home.SetSongs(homeSongs{
					tabs: [homeTabCount][]list.Music{0: homeSongsOf("a", 30)},
				})
			},
			draw: func(s *Shell) func(*mygoui.Context) {
				return func(c *mygoui.Context) {
					// ⚠️ **首页自己装主题**（铁律二：页面的第一件正事），
					// 所以这里不用再装一遍。
					s.viewHome(c, nil)
				}
			},
		},
		{
			// 播放条：窗口底下那一条。
			//
			// **基线（实测）**：每帧 13 次分配、594 字节。
			// 摆的是 `budgetState()` 那副样子（一首 B 站的歌，见它的注释）。
			//
			// **为什么上限是 16 次 / 780 字节**：次数只比基线多 3。这一块是四个
			// 里最省的（封面走缓存，几个图标是画好的 SVG），所以基线小、**余量
			// 看起来很小**——但基线本身也小：这一屏一共就建了十三个东西，多出
			// 三个就已经是「多了两成的活」。而回归多半是「把某个图标改成每帧
			// 新建一张 SVG」或者「每帧重新算一次进度条的位置」——那一次就是
			// 几十次。
			name: "播放条",
			budget: frameBudget{
				allocs: 16,
				bytes:  780,
				floor:  5,
			},
			setup: func(t *testing.T, s *Shell, _ *mygoui.Tester) {
				// 这一档**不摆真音频**（理由见 budgetState）。
			},
			draw: func(s *Shell) func(*mygoui.Context) {
				return func(c *mygoui.Context) {
					// 播放条不是页面：主题由 `viewMain` 装（铁律二），隔离
					// 出来画的时候得自己装上，否则量的是框架默认那套主题。
					s.applyFrameTheme(c)
					s.viewPlayBar(c, budgetState())
				}
			},
		},
		{
			// 队列抽屉：右边浮出来的那一列。
			//
			// **基线（实测）**：每帧 60 次分配、10048 字节。
			// 摆的是二十首的队列（抽屉里一屏放得下十来首）。
			//
			// ⚠️ **基线从 60 降到 44（票 #313）**：那一票把行身份（`rows[i].key`
			// 装进 `any` 那一步）**记好了一份**，不再每帧每一行现算——而抽屉
			// 那一屏十来行，省下来的是十六次。
			//
			// ⚠️ **字节只从 9640 降到 9400**：省下来的那十六次都是**小对象**
			//（一个 `any` 的头），而字节的大头是**每一行封面那一块**——它跟
			// 这一票没关系。
			//
			// ⚠️ **基线从 58 涨到 60（票 #322）**：那一票给抽屉加了**拖拽重排**，
			// 而它**没有用框架的 ListState.Reorder**——那一套每一行每帧都要新建
			// 一个闭包（实测是每帧十四次分配，58 → 72，超了这一档的上限）。
			// 自己接之后每帧只多两次：框架的 Drop / DragOver 各问一次（问这一列，
			// 不问每一行）——那两样每问一次都要新建一个闭包。
			//
			// **为什么上限是 64 次 / 11800 字节**：次数只比基线多 4，字节多一成
			// 八。抽屉里的行比首页那一行便宜（没有播放量那一列），但
			// **行数更多**——一屏十来行，每行都要画歌名和时长。所以「每行多
			// 格式化一个字段」那十来次就该超（实测那种回归 70 次：红 ✓），
			// 而「每帧重建一份队列」那种（几百次）差得更远。
			name: "队列抽屉",
			// 上限 50 → 64：队列里的歌没有封面时，每行画「首字 + 渐变」（设计规范 §7.3），
			// 比原来的灰图标多一个渐变列和一段字；基线实测 58，留 10% 余量。
			budget: frameBudget{
				allocs: 64,
				bytes:  11800,
				floor:  22,
			},
			setup: func(t *testing.T, s *Shell, _ *mygoui.Tester) {
				s.app.Queue().Reset(queueItems(20)...)
				s.drawerOpen = true
			},
			draw: func(s *Shell) func(*mygoui.Context) {
				return func(c *mygoui.Context) {
					// ⚠️ 抽屉**自己不装主题**（铁律二：它在 `viewMain` 的同一
					// 个 build pass 里建），隔离出来画就得自己装。
					s.applyFrameTheme(c)
					s.viewQueueDrawer(c)
				}
			},
		},
		{
			// 满屏播放器：盖住整窗的那一屏。
			//
			// **基线（实测）**：每帧 29 次分配、1297 字节。
			// 摆的是 `budgetState()` 那副样子（见它的注释）**加一份歌词**。
			//
			// ⚠️ **歌词是这一档里最贵的一样，而它是 2026-10-23 那一票
			// （票 #236，满屏接歌词）加进来的**：歌词区是**逐帧重画**的——每一句
			// 每一帧都重新建一遍（一行一个列 + 一个文本 + 可能的译文）。所以这一
			// 档必须摆着歌词：只摆封面的话量到的是「没有歌词那一屏」，而真正会
			// 出现在用户眼前的是**有歌词**那一屏。
			//
			// 十二句是「一屏放得下的十来行」（歌词区的高度是窗口高减去顶栏底栏，
			// 一行 20px 加上下留白，一屏十来行）——摆三句的话量出来的是这一屏
			// **最便宜**的那一档。
			//
			// **为什么上限是 32 次 / 1850 字节**：次数只比基线多 3、字节多四成。
			// 这一屏看着内容多，其实大部分是**画好的东西**（封面走缓存、图标是
			// SVG、频谱默认关着），真正每帧新建的就是那十来行歌词**加右下角那
			// 三枚旋钮**（三枚键 + 各自的字，一共 6 次）——而余量留三个就够：
			// 「歌词画两遍」那种回归一次就是二十来次（多一整份歌词），
			// 「每帧多建两三个东西」那种一次也就两三个。
			//
			// ⚠️ **那三枚旋钮是 2026-10-24 那一票（票 #166，满屏补上那三枚旋钮）
			// 加进来的**：它们是**控件**，界面藏起来的时候不画——但量这一档的
			// 时候界面是露着的，所以它们每一帧都在（见上面那个 setup）。
			name: "满屏播放器",
			budget: frameBudget{
				allocs: 32,
				bytes:  1850,
				floor:  14,
			},
			setup: func(t *testing.T, s *Shell, tt *mygoui.Tester) {
				// ⚠️ **直接开，不走 `openFullPlayer`**：那一条会装一个三秒的
				// 隐藏定时器（界面三秒不动就藏起来），而这里量的是「界面露
				// 着」的满屏——藏起来那一帧是另一回事，不该混进来。
				s.full.open = true
				// ⚠️ **要有一首歌**：歌词挂在「当前这一首」上（`PutLyrics` 认的
				// 是它），而没有歌的时候那一屏画的是空状态——量不到歌词那一列的
				// 每帧成本。
				//
				// 这一首是**暂停**的（理由见 `loadPausedTrack`：解码器在放歌时
				// 会一块一块地读，那是后台线程的分配）。
				loadPausedTrack(t, s, tt)
				s.app.PutLyrics(budgetLRC, "")
			},
			draw: func(s *Shell) func(*mygoui.Context) {
				return func(c *mygoui.Context) {
					s.applyFrameTheme(c)
					s.viewFullPlayer(c, budgetState())
				}
			},
		},
	}
}

// TestEachScreenStaysInsideItsFrameBudget 是这一票的主判据：四个界面每一帧
// 的分配都在自己的上限内。
//
// ⚠️ **它不量时间**：判据全是「数量」，而数量不随机器快慢变（见文件头）。
func TestEachScreenStaysInsideItsFrameBudget(t *testing.T) {
	for _, sc := range budgetScreens() {
		t.Run(sc.name, func(t *testing.T) {
			// 只画这一块：整窗的分配里混着侧栏、顶栏和别的页面，超了以后
			// 报不出「是哪一块多分配的」。
			s, tt := newShell(t, reduceMotion(), withView(func(s *Shell) func(*mygoui.Context) {
				return sc.draw(s)
			}))
			sc.setup(t, s, tt)

			got := measureFrames(t, tt, budgetFrames)
			// 把实测值打出来：`-v` 跑一次就能看到这一屏现在每帧花多少，
			// 而注释里那个基线就是从这里对上的。
			t.Logf("%s：每帧 %.0f 次分配 / %.0f 字节 / 存活堆 %.0f 字节"+
				"（上限 %.0f 次 / %.0f 字节）",
				sc.name, got.allocs, got.bytes, got.live,
				sc.budget.allocs, sc.budget.bytes)
			assertWithinBudget(t, sc.name, got, sc.budget)
		})
	}
}

// TestEachScreenIsBuiltOncePerFrame 钉住「一帧建一遍」。
//
// # 它挡的是什么
//
// 「一帧建两遍」是**静默**的坏处：画面上看着没差（两遍画的是同一块东西，
// 后建的盖住先建的），而**帧成本翻倍**——用户能感到的是「界面有点粘」。
//
// 已知的一处：播放列表抽屉在满屏播放器开着的时候**由满屏那一支建**
// （`viewFullPlayer` 末尾那一句），而 `viewMain` 必须让开。两处都建的话
// 抽屉会被建两遍——**而没有任何东西会报错**（两遍建出来的抽屉长得一模一样）。
//
// ⚠️ **这一条在整窗里量**：坏的是「两个调用方都建了它」，隔离起来就只看得到
// 一个调用方。计数钩子在 `builds.go`。
func TestEachScreenIsBuiltOncePerFrame(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(t *testing.T, s *Shell, tt *mygoui.Tester)
		want  buildTally
	}{
		{
			// 首页那一屏。播放条也在（满屏没开着的时候它一直画）。
			name: "首页",
			setup: func(t *testing.T, s *Shell, _ *mygoui.Tester) {
				s.home.SetSongs(homeSongs{
					tabs: [homeTabCount][]list.Music{0: homeSongsOf("a", 30)},
				})
			},
			want: buildTally{home: 1, playbar: 1},
		},
		{
			// 有歌在放：播放条那一块照样只建一遍。
			name: "播放条",
			setup: func(t *testing.T, s *Shell, tt *mygoui.Tester) {
				loadPausedTrack(t, s, tt)
			},
			want: buildTally{home: 1, playbar: 1},
		},
		{
			// 抽屉开着：它由 `viewMain` 建，建一遍。
			name: "队列抽屉",
			setup: func(t *testing.T, s *Shell, _ *mygoui.Tester) {
				s.app.Queue().Reset(queueItems(20)...)
				s.drawerOpen = true
			},
			want: buildTally{home: 1, playbar: 1, drawer: 1},
		},
		{
			// ⚠️ **这一档是本条测试真正要抓的那一处**：满屏开着、抽屉也开着
			// 的时候，抽屉只能建一遍——由满屏那一支建，`viewMain` 让开。
			// 两处都建的话 `drawer` 会是 2。
			//
			// 而播放条是 **0**：满屏开着的时候它整个不建（共享元素转场要的是
			// 「上一帧没被摆过的那个」封面，播放条那个封面下一帧还会被摆一次，
			// 满屏的封面就找不到「上一个」了——见 fullplayer.go 顶上那一段）。
			//
			// ⚠️ **首页也是 0**（票 #161）：满屏播放器是一层铺满整窗的不透明
			// 浮层，底下那一整页（顶栏 + 侧栏 + 路由页 + 播放条）一个像素都
			// 看不见——而它自己还在每一帧重新布局、重新绘制。#161 把「画」和「每帧
			// 的副作用」拆开了：满屏开着的时候**不画**，而**副作用照跑**（后台
			// 结果照落、该起的拉照起）。
			//
			// 所以这一档的判据是「**没有一块被建两遍**」，而底层那一页是
			// **0 遍**（不是 2 遍）——这一条要抓的坏处是「两个调用方都建了它」，
			// 「压根不建」是另一件事（#161 要的就是它）。
			name: "满屏播放器",
			setup: func(t *testing.T, s *Shell, tt *mygoui.Tester) {
				loadPausedTrack(t, s, tt)
				s.full.open = true
				s.drawerOpen = true
			},
			want: buildTally{home: 0, playbar: 0, drawer: 1, full: 1},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, tt := newShell(t, reduceMotion())
			c.setup(t, s, tt)
			// 稳态：先画几帧（首页那排 Tab 的滑块第一帧才量得到位置）。
			for range warmFrames {
				tt.Frame()
			}
			if tt.FramePending() {
				t.Fatalf("画了 %d 帧还在要帧——稳态下不该有逐帧动画", warmFrames)
			}

			// 量一帧：清零 → 画一帧 → 读。
			s.builds = buildTally{}
			tt.Frame()
			if tt.FramePending() {
				t.Fatal("这一帧之后还在要帧——一帧画了两遍以上，"+
					"「一帧建几遍」这个单位已经不是一帧了")
			}
			// 这一帧画完了吗？崩了的帧画的是**错误页**（`s.crash` 非 nil），
			// 那一刻各个界面一个都没建——数字全是 0，而真正的原因在日志里。
			//
			// ⚠️ 这一句是「一帧建两遍」最常见的下场：两遍建出来的东西撞了同一个
			// key，框架当场 panic（见 shell.go 里 viewMain 那段注释），于是这一帧
			// 是半张，错误页接手——而界面上的症状是「忽然跳到错误页」，离真因
			// 隔着三层。
			if s.crash != nil {
				t.Fatalf("这一帧画的是错误页：界面在画的过程中崩了（%s）：%v\n"+
					"（真的原因在上面那几行日志里：biu: 界面崩溃）",
					s.crash.where, s.crash.got)
			}
			if s.builds != c.want {
				t.Errorf("这一帧建的是 %s，want %s——\n"+
					"「一帧建两遍」是静默的坏处：画面上看着没差，而帧成本翻倍。\n"+
					"（两个调用方都建了同一块的话，这里会是 2。）",
					s.builds.describe(), c.want.describe())
			}
		})
	}
}

// measureFrames 量 n 帧稳态的每帧成本。
//
// ## 它为什么不看时间
//
// 机器快慢只影响「花多久」。这里量的全是**数量**：分配次数是「这一帧建了
// 多少个对象」，字节数是「这些对象多大」。两者都不随机器快慢变——满载的
// 机器上跑出来是同一个数。
//
// ## 那三条「不依赖机器」的约定
//
//   - **单核**（GOMAXPROCS=1）：免得后台线程在测量窗口里被调度到，把**别的
//     线程**的分配记到界面这一帧的账上。
//   - **先强制 GC**：不然量到的是上一轮留下的垃圾。
//   - **取平均**：n 帧一起量，除以 n。
func measureFrames(t *testing.T, tt *mygoui.Tester, n int) frameCost {
	t.Helper()

	// ⚠️ **单核要装在热身之前**：热身里的 `runtime.Gosched` 是为了让后台
	// 线程跑得到（单核下它才是真的「让出」），而后台线程那一笔开销必须在
	// **测量窗口之前**落地。
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)

	// 稳态：先画几帧，把「只有头几帧才有」的一次性开销付掉（见 warmFrames）。
	//
	// ⚠️ **每一帧之后要让出 CPU**（`runtime.Gosched`）：后台线程的一次性开销是
	// 在**它自己那条 goroutine** 上分配的，而那条 goroutine 只有在当前 goroutine
	// 让出 CPU 时才跑得到（单核下尤其如此）。不让出的话，那一笔就落在**测量
	// 窗口**里——而它落在哪一帧是随机的，于是「这一帧花了多少」由它决定。
	//
	// 这一条现在**只是保险**：这四档都不放真音频（见 budgetState），所以眼下
	// 没有后台线程会在测量窗口里分东西。留着它是为了以后再加一档要真放歌的
	// 界面时不用重新踩一遍（实测过的那一笔约 130 KB，摊到一百帧上是每帧多
	// 一千三百字节——比界面自己每帧花的钱还多一倍）。
	for range warmFrames {
		tt.Frame()
		runtime.Gosched()
	}
	// ⚠️ **画完还在要帧，「一帧」就不是一帧**：MyGo 的 Tester 会一直画到这一屏
	// 不再要帧（最多二十帧），于是「一帧」这个单位被摊薄成好几帧，量出来的
	// 每帧成本会小得离谱——而那条会「绿」，比红还坏。
	//
	// 稳态下不该有这种事：逐帧动画在 Tester 里靠钟推，而钟站着不动，于是它
	// 要的那一帧画完就停（见 hover.go 的 wavePhase 那段注释）。
	if tt.FramePending() {
		t.Fatalf("画了 %d 帧还在要帧——这一屏在稳态下还在逐帧要帧，"+
			"「一帧」这个单位已经不是一帧了（量出来的数会小得离谱）", warmFrames)
	}

	var before, after, afterGC runtime.MemStats
	// 先强制 GC：不然量到的是上一轮留下的垃圾（那是上一帧的账）。
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := range n {
		tt.Frame()
		if tt.FramePending() {
			t.Fatalf("第 %d 帧之后还在要帧——一帧画了两遍以上，「每帧」这个单位"+
				"已经不是一帧了（这一帧的分配被摊到了好几帧上）", i)
		}
	}
	runtime.ReadMemStats(&after)
	// 存活堆：画完再强制 GC，看**还活着**的有多少——那是这一批帧留下的。
	runtime.GC()
	runtime.ReadMemStats(&afterGC)

	return frameCost{
		allocs: float64(after.Mallocs-before.Mallocs) / float64(n),
		bytes:  float64(after.TotalAlloc-before.TotalAlloc) / float64(n),
		live:   float64(int64(afterGC.HeapAlloc) - int64(before.HeapAlloc)) / float64(n),
	}
}

// assertWithinBudget 比一对，报错要说清「多了多少」和「为什么这条重要」。
func assertWithinBudget(t *testing.T, what string, got frameCost, want frameBudget) {
	t.Helper()
	if got.allocs < want.floor {
		t.Errorf("%s：每帧只分配了 %.0f 次（下限 %.0f 次）——\n"+
			"这一屏**几乎没画东西**（或者画出来的是另一副样子）。\n"+
			"上限那几条在没有下限的时候是空话：什么都不画也是「没超」。",
			what, got.allocs, want.floor)
	}
	if got.allocs > want.allocs {
		t.Errorf("%s：每帧分配了 %.0f 次，上限 %.0f 次。\n"+
			"这一屏每一帧都在建的东西比该建的多——多半是有一处「每帧重新算一遍」"+
			"（重新建一份列表、每帧格式化一次歌名、每帧重新查一次缓存）。\n"+
			"分配次数是这一组里唯一**不受机器快慢影响**的数，所以这条红了就是真红了。",
			what, got.allocs, want.allocs)
	}
	if got.bytes > want.bytes {
		t.Errorf("%s：每帧分配了 %.0f 字节，上限 %.0f 字节。\n"+
			"分配次数没超而字节超了：多半是**少而大**——每帧新建了一个大对象"+
			"（一整张位图、一个大的切片），而次数上看不出来。\n"+
			"（另一种可能：后台线程的一次性开销正好落进了测量窗口——音频解码器"+
			"那一笔约 130 KB，摊到一百帧上是每帧多一千三百字节。那是噪声不是"+
			"回归，热身里让出 CPU 那一步就是防它的。）",
			what, got.bytes, want.bytes)
	}
	if got.live > liveHeapBudget {
		t.Errorf("%s：每一帧都留下了东西——画完强制 GC 之后活着的堆每帧涨了 %.0f 字节"+
			"（上限 %d）。\n"+
			"这一屏每一帧都往堆上留东西，几十帧之后就是几兆。",
			what, got.live, liveHeapBudget)
	}
}

// describe 把一份计数说成人话（报错时用）。
func (b buildTally) describe() string {
	return fmt.Sprintf("首页 %d 遍、播放条 %d 遍、抽屉 %d 遍、满屏 %d 遍",
		b.home, b.playbar, b.drawer, b.full)
}

// reduceMotion 是「让那些动一下才到位的东西立刻到位」那个偏好。
//
// 理由见文件头那一段：Tester 的钟站着不动，共享元素转场（满屏那个封面从
// 播放条长过去）在里面**永远走不完**，于是那一帧会被反复画二十遍，
// 「一帧」这个单位就废了。
//
// 它只改「值怎么过去」，不改「建了什么」——所以不影响这一组要量的任何数。
func reduceMotion() shellOpt {
	return withPrefs(mygoui.Preferences{ReduceMotion: true})
}

// budgetLRC 是满屏那一档要摆的那份歌词：十二句。
//
// ⚠️ **十二句是「一屏放得下的十来行」**：歌词区的高度是窗口高减去顶栏底栏，
// 一行 20px 加上下留白，一屏十来行——摆三句量到的是这一屏最便宜的那一档。
const budgetLRC = "[00:00.00]第一句\n[00:05.00]第二句\n[00:10.00]第三句\n" +
	"[00:15.00]第四句\n[00:20.00]第五句\n[00:25.00]第六句\n" +
	"[00:30.00]第七句\n[00:35.00]第八句\n[00:40.00]第九句\n" +
	"[00:45.00]第十句\n[00:50.00]第十一句\n[00:55.00]第十二句\n"

// budgetState 是量预算时喂给播放条/满屏的那份「正在放什么」。
//
// # 为什么是手摆的，而不是真放一首歌
//
// 真放歌的话，**解码器那条 goroutine** 会在测量窗口里分掉一大笔（一次性缓冲，
// 实测约 130 KB），而它落在哪一帧是随机的——那是**音频**的成本，不是界面的，
// 却会让「这一帧花了多少」由它决定。实测那一笔摊到一百帧上是每帧多一千三百
// 字节：比界面自己每帧花的钱（几百字节）还多一倍。
//
// 仓库里别处也是这么喂的（见 `playbar_meta_test.go` 的 `metaBarTester`：
// 「喂 State 而不是真开一首文件」）。
//
// # 这一份摆的是哪一副样子
//
// 一首 **B 站的歌**：有 UP 名（那一行才画得出来）、有音质徽章、封面**还没到**
// （`CoverURL` 是空串——真填一个地址的话封面缓存会去拉，那又是一条后台
// goroutine，又是一笔噪声）。
//
// 摆得比「本地文件」那一副**多**（多 UP 名那一行、多一枚徽章），所以量出来的
// 是**上限**：本地文件那副更省，不会超。
func budgetState() app.State {
	return app.State{
		HasTrack:    true,
		Title:       "一首歌",
		Artist:      "某个 UP 主",
		ArtistMid:   12345,
		Duration:    3 * time.Minute,
		Position:    42 * time.Second,
		Playing:     false,
		Volume:      1,
		Rate:        1,
		Quality:     "无损 FLAC",
		QualityPref: app.AudioQualityLossless,
		PlayMode:    app.PlayModeSequence,
		// 🔴 **来源是 B 站**（票 #222）：这一副摆的是「一首 B 站的歌」，而
		// `Source` 决定了两件事——「下载」那一枚摆不摆（本地文件不摆），和
		// **歌名可不可点**（B 站那一首点了跳 B 站视频页）。
		//
		// ⚠️ **它量的是「最贵的那一档」**：歌名可点要多建一个闭包（每帧一次
		// 分配），而本地文件那一副（不可点）更省——所以量出来的数是上限。
		Source: app.SourceOnline,
		// 有 B 站地址，歌名才**真的可点**（本地文件和没有稿件身份的条目都不可点）。
		BiliURL: "https://www.bilibili.com/video/BV1xx411c7mD",
	}
}

// loadPausedTrack 放一首歌进去，然后**暂停**。
//
// ## 为什么要暂停
//
// 解码器在放歌时会一块一块地读，那是**后台线程**的分配——记到界面这一帧
// 的账上就是冤枉（而且那一笔随机器负载变，这条测试就不该再依赖机器）。
// 暂停之后解码器不读了，这一帧的分配就只是界面的。
//
// 画面上和歌在放的时候一模一样（进度条、歌名、那几个键都在），只有播放/
// 暂停那一枚换了个字样。
func loadPausedTrack(t *testing.T, s *Shell, tt *mygoui.Tester) {
	t.Helper()
	if err := s.app.Enqueue(app.LocalItem(flacPath(t))); err != nil {
		t.Fatalf("把歌放进队列：%v", err)
	}
	// 本地文件的 Open 是同步的，一帧就够（见 fullplayer_test.go 的 fullTester）。
	tt.Frame()
	if !s.app.Snapshot().HasTrack {
		t.Fatalf("歌没开起来：%q", s.app.Snapshot().Err)
	}
	s.app.Pause()
}

// queueItems 造 n 首本地歌（队列里的歌不需要真的能放）。
func queueItems(n int) []app.Item {
	items := make([]app.Item, n)
	for i := range items {
		items[i] = app.Item{
			Source: "local",
			Type:   "audio",
			Title:  fmt.Sprintf("第 %d 首", i+1),
		}
	}
	return items
}
