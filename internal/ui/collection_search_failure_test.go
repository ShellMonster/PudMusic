package ui

// 「搜了没搜到，而那一趟**失败**了」（票 #624 评审的评论第 1 条）。
//
// # 这一条钉的是什么
//
// 收藏夹页的「搜了没搜到」那一档，判据是「**词已经发出去了、列表一行都没有、
// 而且不在拉**」。而**失败之后 `Loading` 就是 false**——于是失败落进了
// 「没有结果」那一档。
//
// ⚠️ **后果**：那一档画的是「暂无内容 / 换个词再搜一次」，而**列表的失败态
// （那个「加载失败，点此重试」按钮）就画不出来了**——它只在 `p.list.View`
// 里画，而那一支不调 `p.list.View`。
//
// 于是用户看到的是「暂无内容」，**没有重试的入口**：网络断了、风控了、
// 登录态过期了——全是「等一会儿会好」的事，而界面上一点提示都没有。
//
// ⚠️ **这一条不是 #624 引入的**（基线 `596dd9de` 的判据一模一样），但那一票
// 把判据提取成了 `collectionEmpty`（三态），所以现在要在这里补上「失败」
// 那一档。
import (
	"testing"
	"time"
)

// TestTheCollectionSearchFailureOffersARetry 钉住「失败时给重试」。
//
// ⚠️ **判据是「那个重试按钮在画面上」**：失败和「真的没有结果」在界面上
// 必须是**两件事**——前者要给用户一个重试的机会，后者给一句「换个词」。
func TestTheCollectionSearchFailureOffersARetry(t *testing.T) {
	clock := newFakeClock(time.Unix(2026, 0))
	b := newCollectionBili(t)
	b.setMedias(mediaOf(1, "正常的一首"))
	_, s, tt := collectionShellFrom(t, b, []shellOpt{withClock(clock)}, "", "12345")
	waitSongs(t, tt, s, 1)

	// ⚠️ **先加载成功，再让内容接口开始失败**：那才是「搜了没搜到、而那一趟
	// 失败了」——首屏就失败是另一件事（那一档的判据是 `keyword == ""`，
	// 走的是列表自己那一支，重试按钮照画）。
	b.mu.Lock()
	b.failList = -500
	b.mu.Unlock()

	// 搜一个词。
	typeInto(t, tt, collectionSearchLabel, "找不到")
	tt.Frame()
	tt.Frame()
	clock.Advance(searchDebounce + time.Millisecond)
	tt.Frame()

	// 等那一趟失败。
	waitFrames(t, tt, "搜索那一趟失败", func() bool {
		return s.collection.list.Paged().Err != nil
	})

	// 🔴 **重试按钮要在**。
	if !tt.HasText("加载失败，点此重试") {
		t.Errorf("搜索那一趟失败之后，画面上没有「加载失败，点此重试」"+
			"——用户没有重试的入口：文字有 %q", tt.Texts())
	}
	// ⚠️ **而且不该说「没有结果」**：失败和「真的没有」是两件事，而后者的话
	// 会让用户以为他搜的词真的不存在——于是他去改词，而真正的问题（网络）
	// 被他绕过去了。
	if tt.HasText("换个词再搜一次") {
		t.Error("搜索那一趟失败之后，画面上说的是「换个词再搜一次」——" +
			"那是「真的没有结果」才该说的话")
	}
}

// TestTheCollectionSearchWithNoResultsStillSaysSo 是上面那一条的**另一半**。
//
// 🔴 **判据要分得开**：加了「失败」那一档之后，「真的没有结果」那一档**不许
// 跟着一起坏**——它得照旧说「换个词再搜一次」。
func TestTheCollectionSearchWithNoResultsStillSaysSo(t *testing.T) {
	clock := newFakeClock(time.Unix(2026, 0))
	_, s, tt := collectionShellOpts(t, []shellOpt{withClock(clock)}, "", "12345",
		mediaOf(1, "歌 一"), mediaOf(2, "歌 二"))
	waitSongs(t, tt, s, 2)

	typeInto(t, tt, collectionSearchLabel, "没有这个词")
	tt.Frame()
	tt.Frame()
	clock.Advance(searchDebounce + time.Millisecond)
	tt.Frame()

	// ⚠️ **等它真的筛完**（列表空了、且不在拉）。
	waitFrames(t, tt, "筛完一条都没有", func() bool {
		pg := s.collection.list.Paged()
		return pg.Loaded == 0 && !pg.Loading && pg.Err == nil
	})

	// ⚠️ **判据是 Title（「暂无内容」）**：那句「换个词再搜一次」是 `Hint`，
	// 而 `Hint` **默认不画**（要 `Info: true` 才画，见 `empty.go` 的
	// `emptyCopy`）——它压根不在这张画面上，拿它当判据量不到任何东西。
	if !tt.HasText(noSearchResults.Title) {
		t.Errorf("搜了个没有结果的词，画面上没说「暂无内容」：文字有 %q", tt.Texts())
	}
	// ⚠️ **而失败那个按钮不该在**（这一档是「真的没有结果」，不是失败）。
	if tt.HasText("加载失败，点此重试") {
		t.Error("搜了个没有结果的词，画面上却有一个「加载失败，点此重试」——" +
			"那是失败才该有的")
	}
}

// TestTheCollectionFirstFrameFailureOffersARetry 钉住**首屏**那一档。
//
// ⚠️ **失败那一档不只在「搜了之后」出现**：首屏第一页就失败时也是它。而那一
// 档原来走的是 `default`（列表自己画），所以它一直有重试——**这一条防的是
// 我加的那一档把它弄坏**。
//
// 🔴 **判据同样是「那个重试按钮在画面上」**：用户第一次打开收藏夹就撞上
// 网络断了，而界面上没有重试的入口，那是最容易骂人的一种。
func TestTheCollectionFirstFrameFailureOffersARetry(t *testing.T) {
	b := newCollectionBili(t)
	b.failList = -500
	_, s, tt := collectionShellFrom(t, b, nil, "", "12345")

	waitFrames(t, tt, "第一页失败", func() bool {
		return s.collection != nil && s.collection.list.Paged().Err != nil
	})

	if !tt.HasText("加载失败，点此重试") {
		t.Errorf("首屏那一趟失败之后，画面上没有「加载失败，点此重试」"+
			"——用户没有重试的入口：文字有 %q", tt.Texts())
	}
}
