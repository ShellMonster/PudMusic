// 列表会做的那些操作：接在哪儿。
//
// 为什么要一摊函数而不是让列表直接调 app.App：菜单项是**界面**的一部分，
// 「每一项接的是什么操作」是**接线**的事。接线的默认写法是 AppActions；
// 测试里换成记动作的桩，就能断言「点了『播放』，换进队列的正是这一首往后那
// 几首」——断言的是行为，不是「调了哪个函数」。
package list

import (
	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
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

	// PlayNext 把这一首排在**正在放的那首后面**——「下一首播放」那一下。
	//
	// 和 Add 的分界就是插在哪：Add 接在**队尾**（用户下次翻到队尾才听得到），
	// 这一条插在**当前的下一位**（这一首放完就是它）。摆一个接队尾的按钮
	// 标着「下一首播放」比不摆更坏：按下去一小时后才听得见，而界面上
	// 看不出它其实排到了最后。
	PlayNext func(item app.Item)

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

	// Download 把这一首排进下载队列。
	//
	// ⚠️ **缺 cid 的条目按不动，这是真的「下不了」而不是「还没接」**：下载按
	// 分 P 存文件，而 B 站的搜索结果只给 bvid。缺 cid 的条目按下去会得到一句
	// 「还没取到分 P id」——那句话说清了原因，不是假装成功。
	//
	// 需要 cid 的界面（收藏夹的行、动态卡片）传的是 `App.DownloadItem`：
	// 它在后台去问一次分 P，见 `AppActions` 下面那一段。
	Download func(item app.Item)

	// Notify 说一句话。刚接的操作（比如还没接 B 站接口的收藏）按不动就是靠它。
	//
	// ⚠️ **语气跟着话一起走**（票 #180）：成功是绿的、失败是红的、警告是橙的，
	// 而这一句话是什么语气，**说这句话的那一处知道**——本包只把话和语气递出去。
	Notify func(text string, tone notice.Tone)

	// IsPlaying 报这一首是不是**正在放的那首**，列表据此给那一行画高亮。
	//
	// 为 nil 时不高亮任何一行（每一行都跟别的行长得一样）——那是列表在测试里
	// 用的那份接线，**不是漏接**：真窗口上的每一份都经 `AppActions` 装上了。
	//
	// 🔴 **判据在接线里，不在本包里**：本包不许拿行的下标或者 `Music.ID` 去认
	// 「正在放的那首」——队列会重排（下一首播放、拖拽排序、随机模式），而
	// `Music.ID` 是**行的**身份（铁律四），不是「这是不是同一首歌」。
	// 真正的判据是 `app.App.IsPlaying`（按 `app.Item.Identity`）。
	IsPlaying func(item app.Item) bool
}

// AppActions 把 Actions 接在应用状态上，界面上真正用的那一份。
//
// notify 是说一句话的地方：界面层传它自己的 Toast（Biu 是右下角那张小卡片）。
//
// 🔴 **这里的 Download 是「同步、不补 cid」的那一条**（`App.Download`）：界面
// 线程上一次网络往返都不许有，而补 cid 就是一次往返。所以这一份只对**手里已经
// 有 cid 的条目**有用——首页推荐、稍后再看、历史记录、UP 空间的投稿都是。
//
// 拿不到 cid 的两处（收藏夹的行、动态卡片）各自在那一页把这一项换掉成
// `App.DownloadItem`（后台补 cid，见 `ui` 里的两处），**别在这里改成后台的**：
// 搜索结果的条目也只有 bvid，而它**要**如实拒绝——见 `Actions.Download`。
func AppActions(a *app.App, notify func(string, notice.Tone)) Actions {
	return Actions{
		Play: func(items []app.Item) {
			if err := a.PlayFrom(items...); err != nil && notify != nil {
				notify("放不了这一首："+err.Error(), notice.Danger)
			}
		},
		Add: func(items []app.Item) {
			if err := a.Enqueue(items...); err != nil && notify != nil {
				notify("加不进队列："+err.Error(), notice.Danger)
			}
		},
		PlayNext: func(item app.Item) {
			if err := a.EnqueueNext(item); err != nil && notify != nil {
				notify("排不进下一首："+err.Error(), notice.Danger)
			}
		},
		Open:      func(item app.Item) { open(item, notify) },
		FavSelect: nil,
		Download: func(item app.Item) {
			if item.IsLocal() {
				// 本地文件已经在磁盘上了，没有东西可下载。**先说这一句**，
				// 别让用户读到「这一首还不能放：缺稿件 id」——那是放的说法，
				// 会让他以为这首本地歌坏了。
				if notify != nil {
					notify("这一首已经在你磁盘上了，没有东西可下载", notice.Warning)
				}
				return
			}
			if _, err := a.Download(item); err != nil {
				if notify != nil {
					notify("下载不了："+err.Error(), notice.Danger)
				}
				return
			}
			// 成了一样要说一句：按下去之后界面上没有任何变化的话，用户会以为
			// 这一项是死的（下载记录页确实多了一行，但他在别的页面上）。
			if notify != nil {
				notify("已加进下载队列", notice.Success)
			}
		},
		Notify: func(text string, tone notice.Tone) {
			if notify != nil {
				notify(text, tone)
			}
		},
		// 🔴 **每帧现读**（闭包拿的是 `a`）：正在放的那首会换，缓存住的话
		// 换歌之后列表上的高亮还钉在上一首——而那一首已经不在放了。
		IsPlaying: a.IsPlaying,
	}
}

// open 在系统里打开这一项的来处。
//
// 两分支是有先后的：**本地文件先判**。一个从 B 站下下来又存到本地的音频，
// 两边都有值，这时候「在访达里显示」才是用户想按的那个（Biu 的本地音乐行
// 就是这么做的）。
func open(item app.Item, notify func(string, notice.Tone)) {
	if item.Source == "local" && item.Path != "" {
		mygo.Shell.ShowItemInFolder(item.Path)
		return
	}
	if link := BiliLink(item); link != "" {
		if err := mygo.Shell.OpenExternal(link); err != nil && notify != nil {
			notify("打不开："+err.Error(), notice.Danger)
		}
		return
	}
	if notify != nil {
		notify("这一项还没有可以打开的来处", notice.Warning)
	}
}

// OpenLabel 是菜单上「打开来处」那一项写的字：本地文件写「在访达中显示」，
// 其余写「在 B 站打开」（Biu 的 `menu.tsx` 就是本地项 hidden 掉这一项，我们
// 不藏而是换个说法——本地文件也有来处，只是它在访达里）。
//
// ⚠️ 内容列表那一套菜单和播放列表抽屉的行菜单用的是**同一个**判断（两边都调
// 这里）。照着再写一份的话，同一项在两个菜单上会写得出两种字。
func OpenLabel(item app.Item) string {
	if item.Source == "local" && item.Path != "" {
		return "在访达中显示"
	}
	return "在 B 站打开"
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
