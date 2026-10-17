// 列表会做的那些操作：接在哪儿。
//
// 为什么要一摊函数而不是让列表直接调 app.App：菜单项是**界面**的一部分，
// 「每一项接的是什么操作」是**接线**的事。接线的默认写法是 AppActions；
// 测试里换成记动作的桩，就能断言「点了『播放』，换进队列的正是这一首往后那
// 几首」——断言的是行为，不是「调了哪个函数」。
package list

import (
	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo"
)

// Actions 是这个列表会做的操作。
//
// 零值是可以的，但按下去什么都不会发生：Play / Add / Open 为 nil 时按了是
// 没反应，不是报错。**要接就给全**——「按了没反应」比「还没接」更难解释。
type Actions struct {
	// Play 从这一首往后接进队列并开始播。给的是从这一首起的全部。
	Play func(items []app.Item)

	// Add 把这一首追加到队列末尾，不打断正在放的那首。
	Add func(items []app.Item)

	// Open 在系统里打开这一项的来处：本地文件在访达里显示，在线的在浏览器
	// 里打开 B 站。
	Open func(item app.Item)

	// FavSelect 打开「把这一首放进哪些收藏夹」那个**选择弹窗**
	// （`internal/ui/folderselect.go`）。
	//
	// 是**弹窗**不是直接收藏：B 站那边一次能放进几个收藏夹，而收藏状态是
	// 「在哪些收藏夹里」——直接收藏只能二选一，用户想同时放进「通勤」和
	// 「循环」就得收藏两次再手动去两个收藏夹里删。
	//
	// ⚠️ **名字是 `FavSelect` 而不是 `Favorite`，是为了和 BMG-25（播放列表抽屉）
	// 那张票错开**：人裁定「把这一首收藏起来」这个**动作**归它做，而它多半会
	// 在这个结构体上**加一个同形状的字段**。两个字段都叫 `Favorite` 时，Git 在
	// 同一个位置各加一行 → 要么文本冲突，要么合并后两个 `AppActions` 都在给
	// 同一个字段赋值、后者盖掉前者，而那种合并是**编译通过、行为静默错**的。
	//
	// BMG-25 那一票应该**调这一条**（它要开的正是这个弹窗，Biu 的抽屉也是这么
	// 开的：`music-playlist-drawer/index.tsx:40`），而不是自己再写一个开弹窗的
	// 入口——那一份在 `internal/ui/folderselect.go`。
	FavSelect func(item app.Item)

	// Notify 说一句话。刚接的操作（比如还没接 B 站接口的收藏）按不动就是靠它。
	Notify func(text string)
}

// AppActions 把 Actions 接在应用状态上，界面上真正用的那一份。
//
// notify 是说一句话的地方：界面层传它自己的 Toast（Biu 是右下角那张小卡片）。
func AppActions(a *app.App, notify func(string)) Actions {
	return Actions{
		Play: func(items []app.Item) {
			if err := a.PlayFrom(items...); err != nil && notify != nil {
				notify("放不了这一首：" + err.Error())
			}
		},
		Add: func(items []app.Item) {
			if err := a.Enqueue(items...); err != nil && notify != nil {
				notify("加不进队列：" + err.Error())
			}
		},
		Open:      func(item app.Item) { open(item, notify) },
		FavSelect: nil,
		Notify: func(text string) {
			if notify != nil {
				notify(text)
			}
		},
	}
}

// open 在系统里打开这一项的来处。
//
// 两分支是有先后的：**本地文件先判**。一个从 B 站下下来又存到本地的音频，
// 两边都有值，这时候「在访达里显示」才是用户想按的那个（Biu 的本地音乐行
// 就是这么做的）。
func open(item app.Item, notify func(string)) {
	if item.Source == "local" && item.Path != "" {
		mygo.Shell.ShowItemInFolder(item.Path)
		return
	}
	if link := BiliLink(item); link != "" {
		if err := mygo.Shell.OpenExternal(link); err != nil && notify != nil {
			notify("打不开：" + err.Error())
		}
		return
	}
	if notify != nil {
		notify("这一项还没有可以打开的来处")
	}
}

// BiliLink 是这一项在 B 站上的地址，照 Biu 的 `src/utils/link.ts` 的
// getBiliVideoLink 拼法：mv 走 video/{bvid}，音频走 audio/au{sid}。
//
// 什么都没有就返回空串——调用方拿空串不去开浏览器（开一个空白页比什么都不
// 做更让人以为界面卡了）。
func BiliLink(item app.Item) string {
	switch {
	case item.Bvid != "":
		return "https://www.bilibili.com/video/" + item.Bvid
	case item.Sid != "":
		return "https://www.bilibili.com/audio/au" + item.Sid
	}
	return ""
}
