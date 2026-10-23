// 列表和网格共用的那几件东西。
//
// 这两个视图是同一份数据、同一套操作的两种摆法：`List` 一行一首，`Grid` 一行
// 几首。所以换歌、查重、兜住画崩的那一行、从第几首往后放队列、说一句话——
// 都只在这里写一遍。谁想改行为就改这一处，别在两个 view 里各写一份，改漏一边
// 那种错最难查（列表里能播，网格里放不出来）。
//
// 为什么不干脆让 `Grid` 嵌一个 `List` 的字段：那要把已经落进主干的 `List`
// 整个改一遍（`.songs` 变成嵌套字段，card.go、menu.go 和一整批测试都得跟着
// 动）。这里共享的是逻辑，两个类型各自的字段一个没动。
package list

import (
	"log"
	"runtime/debug"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
)

// warnDuplicateIDs 把重复的行标识指出来。
//
// ⚠️ 重复 ID 的后果在**真窗口里是静默的**：MyGo 的 Tester 会当场 panic 并报出
// 代码行号（`ui/diagnostics.go`），真窗口里它只打一行日志，然后那几行**共用
// 一份状态**——选中一个，另一个也亮着。铁律四要的正好是反过来。
//
// 这里只说不动：偷偷改掉某一行的 ID，界面上画的那首和调用方拿到的那首就对不上
// 了，那比报错更难查。要改就改 Music.ID，让它真的唯一。
func warnDuplicateIDs(songs []Music) {
	seen := make(map[string]struct{}, len(songs))
	for _, m := range songs {
		if _, dup := seen[m.ID]; dup {
			log.Printf("biu: 列表里有几行用了同一个 ID（%q），它们的选中状态会串在一起", m.ID)
		}
		seen[m.ID] = struct{}{}
	}
}

// guardFailed 兜住一行（或网格里一行）画崩的那一下，只废掉这一行。
//
// 🔴 行里 panic 会一路穿出 Cocoa 事件循环把整个进程带走（框架的 Dispatch 和
// drain 都没有 recover），所以每一行自己兜一层：
//
//   - 界面上是「几千行里坏一行」，不是整页变成错误页——页级的 `internal/ui.Guard`
//     兜的是整棵树，那是另一回事，两者不重复。
//   - recover 是在 build 中间接住的，MyGo 下一遍会从头重建一次，所以这一帧
//     剩下的部分照常画得出来，不用整帧作废。
//
// 崩过的行记进 failed（按 ID 记），下一帧不重试：否则同一行每一帧崩一次，日志
// 被刷爆、帧率拖垮，比「这一行画不出来」糟得多。
//
// ⚠️ failed 是**指针**：它是 nil 时要就地建出来，值传进去的话新表建在局部变量
// 上，调用方那份还是 nil——下一帧照样重试，上面那一半就白写了。
//
// ⚠️ 这里不能 import `internal/ui` 去用它的 `Guard`：internal/ui 里的内容页
// 要 import 本包（PLAN 阶段 5 的那 14 个页面），本包再 import 回去就是编译期
// 的循环依赖。页级那一层归页面自己包，本包这一层归这里。
func guardFailed(failed *map[string]bool, what, id string, draw func()) {
	defer func() {
		if rec := recover(); rec != nil {
			if *failed == nil {
				*failed = map[string]bool{}
			}
			(*failed)[id] = true
			log.Printf("biu: %s画崩了（%s）：%v\n%s", what, id, rec, debug.Stack())
		}
	}()
	draw()
}

// playFrom 把第 i 首往后接进队列并开始播。
//
// 给的是**从这一首起的全部**，和 Biu 的「在 B 站打开这一首从这儿听」一致：
// 点了一首，接下来的队列就应该是它和它后面那些，不是整个歌单从头。
// i 不在范围里是什么都不做——双击事件偶尔会落在已经被换掉的那一帧上。
func playFrom(songs []Music, acts Actions, i int) {
	if i < 0 || i >= len(songs) {
		return
	}
	items := make([]app.Item, len(songs)-i)
	for k := i; k < len(songs); k++ {
		items[k-i] = songs[k].Item
	}
	if acts.Play != nil {
		acts.Play(items)
	}
}

// notify 说一句话。给按不动的操作用：按了没反应最难解释，说一句就好解释。
//
// ⚠️ **语气由说这句话的那一处给**（票 #180）：「现在还不能用」是橙的（要用户
// 知道），「加载更多失败」是红的（真的没成）。
func notify(acts Actions, text string, tone notice.Tone) {
	if acts.Notify != nil {
		acts.Notify(text, tone)
	}
}

// isPlaying 报这一项是不是正在放的那首。列表的一行和网格的一格都用它。
//
// 🔴 **判据在接线里（`Actions.IsPlaying`），不在这里**：本包不许拿行的下标或者
// `Music.ID` 去认「正在放的那首」——队列会重排（下一首播放、拖拽排序、随机模式
// 哪一个都在动它），而 `Music.ID` 是**行的**身份（铁律四），不是「是不是同一
// 首歌」。真的判据是 `app.App.IsPlaying`，按 `app.Item.Identity` 比。
//
// 没装判据（测试里那份接线）时一律当不在放。
func isPlaying(acts Actions, item app.Item) bool {
	if acts.IsPlaying == nil {
		return false
	}
	return acts.IsPlaying(item)
}
