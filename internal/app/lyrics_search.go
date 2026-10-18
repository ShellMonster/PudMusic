// 歌词搜索与预览：Biu 的 `lyrics-search-modal/` 在业务层的样子。
//
// 这一层只管**记住**「在搜哪个源、搜的是哪个词、搜到了什么、拉到了没」，把搜索和
// 预览放到后台去发，把迟到的结果按代次丢掉，再加一层**结果缓存**。请求长什么样是
// `service/lyrics`（search.go）的事——界面层连「网易云要多发一次请求」都不用知道。
//
// # 结果缓存：同一个词不搜第二遍
//
// 「搜索结果 500 毫秒防抖写缓存，避免重复请求」的后半句在这儿（前半句那 500ms 防抖
// 在界面层，`ui/lyricssearch.go`）。上游没有这一层：切走再切回弹窗它就重新搜一遍，
// 而用户在那几十毫秒里看着候选一行一行冒出来。
//
// **失败不记**：记了的话用户按了重试，界面上弹的还是上一次那句「拉不到」。
//
// # 为什么有一个代次
//
// 和 `lyrics.go` 里的 `lyricGen` 是同一回事：搜索和预览都在后台，而用户等的那几百
// 毫秒里会切源、关弹窗、换歌。少了它就是「我搜的是乙，弹出来的是甲的歌」，而按
// 多少次搜索都一样。
package app

import (
	"context"
	"errors"
	"log"
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
)

// lyricSearchMemoMax 是结果缓存留多少条。
//
// 20 = 网易云 / LRCLib 各搜十次。够一个人连着试十首歌，界面上一个词的候选最多
// 20 条，**再往上留就是几十 MB 的字符串常驻内存**。超了从最老的开始丢（见
// `rememberSearch`）。
const lyricSearchMemoMax = 40

// LyricSource 是「歌词源」在界面上的那一份：机器名 + Tab 上的字。
//
// ⚠️ 两个字段都要：机器名是行身份和缓存键（跨版本不变），Tab 上那个字是给用户
// 看的（「网易云」/「LrcLib」，照抄上游）。拿显示名当身份的话，改一次文案就
// 把所有缓存键作废了。
type LyricSource struct {
	ID   string
	Name string
}

// LyricSearchState 是搜索弹窗一帧要读的全部状态。
type LyricSearchState struct {
	// Candidates 是当前这一批候选。**空 = 一个都没有**（不是「拉到了空的」）。
	Candidates []lyrics.Candidate
	// Query 是**已经搜过的**那个词（生效的），不是输入框里正在敲的那一个：分成两个
	// 是因为即时模式每一帧都重画，直接拿框里的字去搜的话用户每敲一个字就发一次请求。
	// 理由和 `ui/librarypage.go` 的 `draft` / `applied` 一模一样。
	Query string
	// Tab 是当前在哪个源上（`MountLyricSources` 给的顺序下标）。
	Tab int
	// Loading 是「这一次还没回来」。
	Loading bool
	// Err 是最近一次搜索失败的原因，**空串 = 没有失败**。「搜不到」和「拉不到」
	// 在界面上必须是两句话（见 `lyricsErrText`）。
	Err string
	// Searched 报「已经搜过一次了」。没搜过时界面说的是「输入歌名开始搜索」而不是
	// 「没搜到」——那两句说的是完全不同的事。
	Searched bool
}

// LyricPreviewState 是预览弹窗一帧要读的全部状态。
type LyricPreviewState struct {
	// Open 是预览弹窗开着没有。关掉时下面那些都不作数。
	Open bool
	// Title 是弹窗顶上那一句：「歌名 - 歌手」（上游的
	// `setLyricsTitle(\`${song.name}-${renderArtists(...)}\`)`）。
	Title string
	// LRC / Translated 是这次预览的那两份原文和译文。
	LRC        string
	Translated string
	// Loading 是「正在拉这一条」。
	Loading bool
	// Err 是拉失败的原因，空串 = 没有失败。
	Err string
	// Adoptable 是「现在按「采用歌词」有东西可采用」。
	//
	// 拉失败、没有歌词、拉的那份原文是空的，三种都关掉这个按钮：按下去不会有
	// 任何事发生，而那种按钮在界面上和坏了的按钮长得一模一样。
	Adoptable bool
}

// lyricSearchState 是「歌词搜索」那一摊的内部状态。**只有界面线程和后台 goroutine
// 经过 a.mu**。
type lyricSearchState struct {
	// sources 是挂上来的源，顺序就是界面上 Tab 的顺序。
	sources []lyrics.Source
	// results 是当前这一批候选。
	results []lyrics.Candidate
	// query / tab 是**生效**的那一次搜索。
	query string
	tab   int
	// loading / err 是这一次的结果。
	loading bool
	err     string
	// searched 报「搜过一次了」。
	searched bool
	// gen 是**搜索代次**。每发一次搜索加一，迟到的结果发现代次变了直接走人。
	gen int
	// memo 是「源 + 词 → 候选」的缓存，见文件头「结果缓存」。
	memo map[string][]lyrics.Candidate
	// memoOrder 是 memo 的键的插入顺序，超上限时从这里最老的开始丢。
	memoOrder []string
}

// lyricPreviewState 是「预览」那一摊。
type lyricPreviewState struct {
	open       bool
	title      string
	lrc        string
	translated string
	loading    bool
	err        string
	// key 是**开始预览时**当前那首歌的缓存键。采用的时候拿它对一遍（见
	// `AdoptLyricPreview`）。
	key string
	// gen 是预览代次，道理和搜索那一支一样。
	gen int
}

// MountLyricSources 把歌词源挂到 App 上。**在开窗口之前调**（它不发请求，但挂上
// 之后界面上那两个 Tab 才在）。一个源都没挂时搜索弹窗是**关着的**：没有源还摆一个
// 弹窗的话，用户输完词按下搜索，看到的是一句「没有歌词源」。
func (a *App) MountLyricSources(srcs ...lyrics.Source) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lyricSearch.sources = srcs
	a.lyricSearch.memo = map[string][]lyrics.Candidate{}
}

// LyricSources 返回挂上来的是哪几个源，界面上照这个顺序摆 Tab。
func (a *App) LyricSources() []LyricSource {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]LyricSource, 0, len(a.lyricSearch.sources))
	for _, s := range a.lyricSearch.sources {
		out = append(out, LyricSource{ID: s.ID(), Name: s.Name()})
	}
	return out
}

// LyricSearch 拿搜索那一摊的拷贝。**每一帧调一次**。
func (a *App) LyricSearch() LyricSearchState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return LyricSearchState{
		Candidates: a.lyricSearch.results,
		Query:      a.lyricSearch.query,
		Tab:        a.lyricSearch.tab,
		Loading:    a.lyricSearch.loading,
		Err:        a.lyricSearch.err,
		Searched:   a.lyricSearch.searched,
	}
}

// SearchLyrics 搜第 tab 个源上的 query。**立刻返回**：请求在后台发，回来之后装上
// 状态并叫醒界面（铁律三，见 `loadLyrics` 那一段）。
//
// 已经有结果的那个词**一个请求都不发**：先问缓存，缓存里有就立刻摆出来。
func (a *App) SearchLyrics(tab int, query string) {
	query = strings.TrimSpace(query)

	a.mu.Lock()
	s := &a.lyricSearch
	// ⚠️ 上一轮生效的那一次**在覆盖之前**留一份：下面那个「已经转着就别再发一遍」
	// 的判据认的是它，而 `s.tab`/`s.query` 下一行就被这次覆盖了。
	prevQuery, prevTab := s.query, s.tab
	s.gen++
	gen := s.gen
	s.tab = tab
	if query == "" {
		// 空词不去搜（上游的 `if (query === "") return`），而**上一批候选要收掉**：
		// 留着的话用户看着自己刚删干净的那一框下面还挂着一批和它无关的歌。
		s.clearLocked()
		a.mu.Unlock()
		a.wakeLyrics()
		return
	}

	src := pickSource(s.sources, tab)
	if src == nil {
		a.mu.Unlock()
		return
	}
	if cands, ok := s.memo[searchKey(src.ID(), query)]; ok {
		// 命中缓存。代次照样在开头加过了——上一次还在路上的那趟回来时不该盖掉它。
		s.query, s.results, s.err, s.loading, s.searched = query, cands, "", false, true
		a.mu.Unlock()
		a.wakeLyrics()
		return
	}
	if prevQuery == query && prevTab == tab && s.loading {
		// 生效的那一次就是这一次，还转着呢：别再发一遍。
		a.mu.Unlock()
		return
	}
	s.query, s.results, s.err, s.searched = query, nil, "", true
	s.loading = true
	a.mu.Unlock()
	// 摆上「正在搜」并要一帧：不然界面上停在上一批候选，用户看着像按了没反应。
	a.wakeLyrics()

	key := searchKey(src.ID(), query)
	go func() {
		cands, err := src.Search(context.Background(), query)

		a.mu.Lock()
		if a.lyricSearch.gen != gen || a.closed {
			// 又搜了一次 / 应用在退。这一趟已经过时，丢掉。
			a.mu.Unlock()
			return
		}
		a.lyricSearch.loading = false
		if err != nil {
			a.lyricSearch.failLocked(lyricsErrText(src, "搜不到", err))
		} else {
			a.lyricSearch.results = cands
			a.rememberSearchLocked(key, cands)
		}
		a.mu.Unlock()
		a.wakeLyrics()
	}()
}

// clearLocked 把搜索清成「没搜过」（框被清空时走它）。**必须拿着 a.mu 调**。
func (s *lyricSearchState) clearLocked() {
	s.results, s.query, s.err, s.loading, s.searched = nil, "", "", false, false
}

// failLocked 记一次失败。**必须拿着 a.mu 调**。
//
// 🔴 **`query` 和 `searched` 都留着**：界面上那个防抖的「已经搜的就是它」分支读的
// 就是它们。一起清掉的话 `now - 零值`（一大截）恒大于防抖窗口，于是**下一帧立刻
// 又发一次**、再失败、再发——而生产里每次投递都 `wake()` 叫醒下一帧，那个循环
// **自己养自己**。实测 20 帧打了 22 次，界面上只剩「拉不到」两个字来回闪。
func (s *lyricSearchState) failLocked(msg string) {
	s.results, s.err, s.loading = nil, msg, false
}

// rememberSearchLocked 把搜到的东西记进结果缓存。**必须拿着 a.mu 调**。超上限时从
// **最老的**开始丢：新搜的留着、先搜的丢（丢的是哪一条从界面上看不出来，而用户
// 刚搜的那一个是他唯一还记得要的那一个）。
func (a *App) rememberSearchLocked(key string, cands []lyrics.Candidate) {
	if a.lyricSearch.memo == nil {
		a.lyricSearch.memo = map[string][]lyrics.Candidate{}
	}
	if _, dup := a.lyricSearch.memo[key]; dup {
		return
	}
	a.lyricSearch.memo[key] = cands
	a.lyricSearch.memoOrder = append(a.lyricSearch.memoOrder, key)
	for len(a.lyricSearch.memoOrder) > lyricSearchMemoMax {
		oldest := a.lyricSearch.memoOrder[0]
		a.lyricSearch.memoOrder = a.lyricSearch.memoOrder[1:]
		delete(a.lyricSearch.memo, oldest)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 预览
// ─────────────────────────────────────────────────────────────────────────────

// OpenLyricPreview 打开预览弹窗并在后台拉这一条的歌词。**立刻返回。**
//
// 🔴 **界面线程上一个网络请求都不许有**：候选自带歌词的那一种（`Candidate.HasLyrics`，
// LRCLib 的搜索结果里就有）当场填上，其余的一律进后台。写成「先问一次源、成了就用」
// 的话，网易云那一次就是**在界面线程上等一个网络往返**——症状是点一条候选之后整块
// 界面卡住到它回来，而点第二下的时候它还卡着。
func (a *App) OpenLyricPreview(tab int, c lyrics.Candidate) {
	title := c.Title + " - " + c.Artist

	if c.HasLyrics() {
		a.mu.Lock()
		key := a.lyricKey
		if key == "" {
			a.mu.Unlock()
			return
		}
		a.lyricPreview.gen++
		a.lyricPreview.open = true
		a.lyricPreview.title = title
		a.lyricPreview.lrc = c.LRC
		a.lyricPreview.translated = c.Translated
		a.lyricPreview.loading = false
		a.lyricPreview.err = ""
		a.lyricPreview.key = key
		wake := a.wake
		a.mu.Unlock()
		if wake != nil {
			wake()
		}
		return
	}

	a.mu.Lock()
	src := pickSource(a.lyricSearch.sources, tab)
	key := a.lyricKey
	if src == nil || key == "" {
		// 没有源 / 没有在放的歌：没有能采用给谁。这一支什么都不做——
		// 界面上搜索弹窗本来就是从「正在放的歌」那儿进来的。
		a.mu.Unlock()
		return
	}
	// 先开一个空弹窗，标着「正在拉」。界面上用户立刻看得见自己点中了哪一条，
	// 而不是在候选列表里点了一下什么都没发生。
	a.lyricPreview.gen++
	gen := a.lyricPreview.gen
	a.lyricPreview.open = true
	a.lyricPreview.title = title
	a.lyricPreview.lrc = ""
	a.lyricPreview.translated = ""
	a.lyricPreview.loading = true
	a.lyricPreview.err = ""
	a.lyricPreview.key = key
	wake := a.wake
	a.mu.Unlock()
	if wake != nil {
		wake()
	}

	go func() {
		doc, err := src.Preview(context.Background(), c)

		a.mu.Lock()
		if a.lyricPreview.gen != gen || a.closed {
			a.mu.Unlock()
			return
		}
		a.lyricPreview.loading = false
		switch {
		case err != nil:
			a.lyricPreview.err = lyricsErrText(src, "歌词拉不到", err)
		case strings.TrimSpace(doc.LRC) == "":
			// ⚠️ **源返回了「成功」而正文是空的**：这是**第三种**情况，和「拉不到」
			// 以及「这一条没有歌词」都要分开说。摆一个空弹窗的话，用户看到的是
			// Tab 和两个按钮之间一片空白——那句话和「加载中」一样是在搪塞。
			// 上游那句是「未找到歌词内容」（`netease-tab.tsx:55`）。
			a.lyricPreview.err = lyricsErrText(src, "歌词", lyrics.ErrNoLyrics)
		default:
			a.lyricPreview.lrc = doc.LRC
			a.lyricPreview.translated = doc.Translated
		}
		wake := a.wake
		a.mu.Unlock()
		if wake != nil {
			wake()
		}
	}()
}

// LyricPreview 拿预览那一摊的拷贝。**每一帧调一次**。
func (a *App) LyricPreview() LyricPreviewState {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := &a.lyricPreview
	return LyricPreviewState{
		Open:       p.open,
		Title:      p.title,
		LRC:        p.lrc,
		Translated: p.translated,
		Loading:    p.loading,
		Err:        p.err,
		Adoptable:  p.open && !p.loading && p.err == "" && strings.TrimSpace(p.lrc) != "",
	}
}

// CloseLyricPreview 关掉预览弹窗。
func (a *App) CloseLyricPreview() {
	a.mu.Lock()
	a.lyricPreview.gen++
	a.lyricPreview.open = false
	a.lyricPreview.lrc = ""
	a.lyricPreview.translated = ""
	a.lyricPreview.loading = false
	a.lyricPreview.err = ""
	wake := a.wake
	a.mu.Unlock()
	if wake != nil {
		wake()
	}
}

// AdoptLyricPreview 采用预览的那一份歌词：写进**当前这首歌**的缓存，界面上立刻
// 换上去，弹窗关掉。返回一句给用户看的话（**空串 = 成功**）。
//
// 🔴 **歌换了就不许采用**：预览是给「按下预览那一刻在放的那首歌」看的，而这中间用户
// 可能切了歌。照着当前这一首写下去的话，画面上换到的是**歌名 A 配词 B**，而界面上
// 从歌词到播放条每一个字都对得上——那种错用户永远查不出来。
func (a *App) AdoptLyricPreview() string {
	a.mu.Lock()
	p := &a.lyricPreview
	switch {
	case !p.open:
		a.mu.Unlock()
		return ""
	case a.lyricKey != p.key:
		a.mu.Unlock()
		return "歌已经换了，这份歌词不是它的"
	case p.loading:
		a.mu.Unlock()
		return "这一条还在拉"
	case strings.TrimSpace(p.lrc) == "":
		a.mu.Unlock()
		return "这一条没有歌词"
	}
	// ⚠️ **在锁里把这两份拷出来**：锁外读 `p.lrc` 是数据竞争（防抖落盘那个 goroutine
	// 在写同一份状态），撕裂读出来是一个野指针，`-race` 抓得到、产线上是随机崩。
	lrc, translated := p.lrc, p.translated
	a.lyricPreview.gen++
	a.lyricPreview.open = false
	a.lyricPreview.lrc = ""
	a.lyricPreview.translated = ""
	a.mu.Unlock()

	// PutLyrics 自己写缓存、自己叫醒界面（见 lyrics.go）。
	a.PutLyrics(lrc, translated)
	return ""
}

// ─────────────────────────────────────────────────────────────────────────────

// pickSource 拿下标越界时返回 nil 而不是拿最后一个。越界只发生在「界面上摆着三个
// Tab、用户正要点第三个，而那一刻源被换了」这一种情形上——那种情形下正确的做法是
// **什么都不做**：发出去的请求没人要结果，回来的结果也没人认。
func pickSource(list []lyrics.Source, i int) lyrics.Source {
	if i < 0 || i >= len(list) {
		return nil
	}
	return list[i]
}

// searchKey 是结果缓存的键。⚠️ 分隔符用 `\x00`：歌名里什么字符都可能有（`\x00`
// 除外——那是 LRC 和文件名里都不允许的），用空格或 `/` 分隔的话「网易云 / 晴天」和
// 「网易云/晴天」会撞成同一个键，于是用户搜到的结果是**另一个词**的。
func searchKey(sourceID, query string) string { return sourceID + "\x00" + query }

// lyricsErrText 是失败时给用户看的那一句。
//
// ⚠️ **「这一条没有歌词」和「拉不到」必须分开**：混成一句的话，用户读到「网络不通」
// 会去反复重试，而这一条重试一万次也不会有歌词——那一版在源上就是没有。
//
// ⚠️ **点名是哪个源**：面板上此刻摆着两个 Tab，只说「拉不到」的话用户不知道该换哪
// 一个再试，而「换源」正是那个空状态里点名要给用户的下一步。
//
// ⚠️ **不把底层错误整句打给用户**：那是「dial tcp: lookup interface.music.163.com
// on 10.0.0.1:53: no such host」，用户读完不知道自己能做什么，而「拉不到」后面跟着
// 一个重试按钮才是他能做的事。细节记日志。
func lyricsErrText(src lyrics.Source, what string, err error) string {
	switch {
	case err == nil:
		return ""
	case err == lyrics.ErrNoQuery:
		return "还没输歌名"
	case errors.Is(err, lyrics.ErrNoLyrics):
		return src.Name() + " 未找到歌词内容（换一条候选，或者换个源）"
	default:
		log.Printf("biu: 歌词源请求失败：%v", err)
		return src.Name() + " " + what + "：网络不通或者对方没理"
	}
}

// wakeLyrics 叫醒界面（铁律三）。空的时候（没挂界面）什么也不做。
func (a *App) wakeLyrics() {
	a.mu.Lock()
	wake := a.wake
	a.mu.Unlock()
	if wake != nil {
		wake()
	}
}
