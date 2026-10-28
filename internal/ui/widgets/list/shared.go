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

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
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

// playFrom 把队列**换成**第 i 首往后的那一段并从第 i 首开始播。
//
// 给的是**从这一首起的全部**，和 Biu 的「在 B 站打开这一首从这儿听」一致：
// 点了一首，接下来的队列就应该是它和它后面那些，不是整个歌单从头。
// i 不在范围里是什么都不做——双击事件偶尔会落在已经被换掉的那一帧上。
//
// ⚠️ **它只接「从这一首往后听」那一档**（右键菜单，票 #337）：双击 / 回车 /
// 封面播放钮走的是 `playNow`（票 #608），那是「放这一首」，不换队列。
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

// playNow 让第 i 首成为正在放的那首——**不换队列**。
//
// 这是双击一行 / 回车 / 点封面上那枚播放三角的手势（票 #608，照上游 Biu 的
// `play()`）：用户已经排好的队列要留着，这一首已经在里面就定位过去，不在就追加。
//
// ⚠️ **和 `playFrom` 的分界**：这一条只动「这一首」，那一条换掉整个队列。
// 用错地方的后果差得很远——用户在订阅里点了一首歌，队列里原来那 112 首全没了。
func playNow(songs []Music, acts Actions, i int) {
	if i < 0 || i >= len(songs) {
		return
	}
	if acts.PlayNow != nil {
		acts.PlayNow(songs[i].Item)
	}
}

// rowPlay 是**一行的主操作**（双击 / 回车 / 封面播放钮）该做的那件事。
//
// 分两档，判据和那两处的菜单、封面反馈**是同一个**（`isUserItem`）：
//
//	歌     → `playNow`（放这一首，不换队列，票 #608）
//	UP 主  → `Play`（进他的空间，`searchresults.go` 的 `openSearchUser`）
//
// ⚠️ **别把两档合成一个 `PlayNow`**：搜索页「用户」那一档的 `Play` 是**进空间**，
// 而它的 `PlayNow` 是一句「UP 主不是歌」的说明（菜单里「立即播放」用的）。合成
// 一处的话，双击一个用户行会弹出那句话，而不是进他的空间——用户想进进不去。
func rowPlay(songs []Music, acts Actions, i int) {
	if i < 0 || i >= len(songs) {
		return
	}
	if isUserItem(songs[i]) {
		// ⚠️ **只给这一项**，不是「从这一项往后那一段」：用户 Tab 的 `Play` 是
		// `openSearchUser`，它只读 `items[0]`（那是 `playFrom` 的协议，但用户行
		// 只有「进哪一个空间」这一件事可做）。给整段的话，一页 112 行点第一个
		// 就要**白分配 112 项的切片**——而那一份随后被丢进垃圾桶。
		if acts.Play != nil {
			acts.Play([]app.Item{songs[i].Item})
		}
		return
	}
	playNow(songs, acts, i)
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

// isUserItem 报这一项是**一个 UP 主**，不是一首歌。
//
// 搜索页「用户」那一档的行是 UP 主：那一行按下去是**进他的空间**（上游
// `user-card.tsx:51` 的 `navigate("/user/" + mid)`），不是放歌。
//
// ⚠️ **封面那一圈反馈因此换一套**（图形、提示、无障碍名三样都换，理由见
// `hover.ProfileTip`）——那一圈原来三样都在说「放歌」，而用户按下去发现的是
// 跳到别人的主页。
//
// ⚠️ **判据和菜单那一处是同一个**（`menu.go` 的 `menuItems`）：两处各写一份
// 的话，改了一处另一处还在说「放歌」。
func isUserItem(m Music) bool { return m.Item.Type == app.ItemTypeUser }
