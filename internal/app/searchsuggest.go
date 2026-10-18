// 搜索联想词的业务层（#97，A25）。界面那一半在 `ui/navbar_suggest.go`。
//
// # 这一层只有两个方法
//
// `SearchSuggest`（问一个词）/ `SuggestState`（拿一帧要画的东西）。两个都是界面上
// 那一个浮层要的，别的一概不管——防抖不在这一层（那是界面那一层的事，见
// `ui/navbar_suggest.go`），真搜索也不在这一层（点一条就是走 `Shell.submitSearch`）。
//
// # 为什么放在这一层而不是界面里发请求
//
// 铁律三：界面线程上一次网络往返都不许有。这一层发出去就立刻返回，回来之后
// 装上状态并叫醒界面——和歌词搜索那一对（`SearchLyrics` / `LyricSearch`）逐字同构。
//
// # 失败是**静默**的，而且静默有具体形状
//
// 规格：「请求失败时静默：浮层保持只有历史那一档，不弹错误。」所以这一层**根本没有**
// 错误字段：拉不到就是 `Items` 为空，画面上和「B 站没给联想」长得一模一样。
//
// ⚠️ 但「静默」不许写成「什么都不做」：失败之后 `query` 仍然记下那个词，于是界面
// 那一支「这个词已经问过了」一直成立，下一帧不会又发一次。少了这一句就是自己养自己
// 的循环——生产里每次投递都叫醒界面，于是浮层底下**每一帧都在刷一次网络**，而用户
// 看到的是「搜索框卡了一下」。和歌词那一支 `failLocked` 留着 `Query` 是同一个道理。
//
// # 代次
//
// 搜索站那一趟是几百毫秒的往返，而用户在这中间还在打字。每问一次加一代，迟到的
// 那些回来时发现代次变了直接走人（`a.closed` 同理：退出之后不许再写）。
//
// ⚠️ 代次是**作废**用的不是**撤销**用的：每一次 `gen++` 都必须紧跟着一次「新的那趟
// 会负责把它该做的事做完」，中间插一个不发新的早退，那一趟就永远没人负责了。
// 「同一个词还在飞」那个早退就排在 `gen++` **之前**。

package app

import (
	"context"
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// SuggestState 是搜索浮层里「联想词」那一档一帧要读的全部状态。
type SuggestState struct {
	// Items 是**已经拿到的**联想词，按 B 站给的顺序。空 = 一个都没有
	// （不是「还在拉」也不是「拉到了但是空」——这一层分不出后两者，因为界面上
	// 三者长得一模一样，见文件头「失败是静默的」）。
	Items []string

	// Query 是**已经问过的**那个词，不是输入框里正在敲的那一个。分成两个是
	// 因为即时模式每一帧都重画：拿框里的字去问的话用户每敲一个字就发一次请求。
	// 理由和 `LyricSearchState.Query` / `ui/librarypage.go` 的 `applied` 一模一样。
	//
	// ⚠️ **失败之后它也照记**——界面上「已经问过了」这一支读的就是它，见文件头。
	Query string

	// Loading 是「这一次还在飞」。
	//
	// 界面上**不用它**摆任何东西（拉的过程中浮层就那么摆着，不转圈、不说「正在搜」）：
	// 用户在打每一个字，联想是一闪一闪地补上来的，中间摆一句「正在搜…」比不给还吵。
	// 它在这儿是因为界面上有一个判断要靠它（「这个词还在飞就别再问一遍」）。
	Loading bool
}

// suggestState 是这一摊的内部状态。**只有界面线程和后台 goroutine 经过 a.mu**。
type suggestState struct {
	query   string
	items   []string
	loading bool
	// gen 是**联想代次**，每问一次加一（见文件头「代次」）。
	gen int
}

// SearchSuggest 问一个词的联想。**立刻返回**：请求在后台发。
//
// 没有词、或者问的还是那个还在飞的词，都一个请求都不发（理由见文件头）。
func (a *App) SearchSuggest(keyword string) {
	keyword = strings.TrimSpace(keyword)

	a.mu.Lock()
	s := &a.suggest
	if keyword == "" {
		// ⚠️ **已经什么都没有了就别动它**：界面那一层是**每帧**调进来的（框里空着
		// 的那几百帧都在走这一支），而每次都 `gen++` 加叫醒就是自己养自己的循环——
		// 生产里每一次唤醒都画一帧，于是每一帧都再唤醒一次，界面永远停不下来。
		if s.query == "" && len(s.items) == 0 && !s.loading {
			a.mu.Unlock()
			return
		}
		// 空词不去问，而且**上一批要收掉**：留着的话用户看着自己刚删干净的那一框
		// 底下还挂着一批和它无关的词。代次照样加——上一次还在路上的那趟回来时
		// 不该盖掉「框里已经空了」这件事。
		//
		// ⚠️ **把 `query` 也清掉是为了让同一个词能再问一遍**：上一次失败时这一格
		// 记着那个词（界面上靠它挡住每帧重发），而用户看不到任何提示，于是他把那
		// 几个字删掉重敲一遍时会一直撞在「这个已经问过了」上——那句话在界面上是
		// 空的（静默是规格要的），用户唯一能做的事就是再敲一遍。
		s.gen++
		s.query, s.items, s.loading = "", nil, false
		a.mu.Unlock()
		a.wakeSuggest()
		return
	}
	// ⚠️ **判「已经在飞」要排在代次加一之前**：排在后面的话，这次调用的唯一副作用
	// 就是把**正在飞的那一趟**作废掉（它回来时发现代次变了就走人、不置
	// loading=false），而这一支自己又不发新的——于是 `loading` 再没有人翻回来。
	// 界面上天天会走到这一支（用户敲完一个词、防抖到点、连按两下回车）。
	if keyword == s.query && s.loading {
		a.mu.Unlock()
		return
	}

	// ⚠️ **同一个词不许问第二遍**，哪怕上一次已经回来了。防抖那一层也有这道闸门，
	// 而那一层防的是「每敲一个字发一次」；这里是兜底——它挡住的是「界面那一支的
	// 判据写错时」那种一帧一发的自激循环。
	if keyword == s.query {
		a.mu.Unlock()
		return
	}

	cli := a.biliClientLocked()
	s.gen++
	gen := s.gen
	s.query, s.items, s.loading = keyword, nil, true
	a.mu.Unlock()
	// 摆上「正在拉」并要一帧：不然上一批词还挂着一个字都没改，而它属于上一个词。
	a.wakeSuggest()

	if cli == nil {
		// 没挂上 B 站客户端（没走过 MountLogin）。**静默**：这就是「拉不到」那一种，
		// 而界面上该表现的是浮层里只剩历史那一档。
		a.mu.Lock()
		if a.suggest.gen == gen && !a.closed {
			a.suggest.loading = false
			a.suggest.items = nil
		}
		a.mu.Unlock()
		a.wakeSuggest()
		return
	}

	go func() {
		got, err := cli.SearchSuggest(context.Background(), keyword)

		a.mu.Lock()
		if a.suggest.gen != gen || a.closed {
			// 又问了一次 / 应用在退。这一趟已经过时，丢掉。
			a.mu.Unlock()
			return
		}
		a.suggest.loading = false
		if err == nil {
			a.suggest.items = got
		}
		// ⚠️ **失败的那一支什么都不做**：`items` 在发出去时就已经被清成 nil 了，
		// 而 `query` 记着这个失败的词（界面上「已经问过了」那一支靠它挡住每帧重发）。
		a.mu.Unlock()
		a.wakeSuggest()
	}()
}

// SuggestState 拿这一摊的拷贝。**每一帧调一次**。
func (a *App) SuggestState() SuggestState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return SuggestState{
		Items:   a.suggest.items,
		Query:   a.suggest.query,
		Loading: a.suggest.loading,
	}
}

// biliClientLocked 取那个 B 站客户端。**必须拿着 a.mu 调**。
//
// ⚠️ 不走 `BiliClient()`：那个方法自己会拿锁，而这一层已经在锁里了。
func (a *App) biliClientLocked() *bili.Client {
	if a.account == nil {
		return nil
	}
	return a.account.cli
}

// wakeSuggest 叫醒界面（铁律三）。空的时候（没挂界面）什么也不做。
func (a *App) wakeSuggest() {
	a.mu.Lock()
	wake := a.wake
	a.mu.Unlock()
	if wake != nil {
		wake()
	}
}
