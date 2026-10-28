// 搜索框热词的业务层：网易云热搜词的拉取与缓存，给顶栏搜索框那个轮换的
// 占位词当词池。界面那一半在 `ui/navbar_search.go`。
//
// # 和热榜歌名（`recommend.go` 的 RefreshHotSongs）是同一对形状
//
// 界面层不发网络请求（铁律三），所以拉取住在这层：`Refresh` 每帧可调——已经
// 有一份且没过期、或者上一趟还在飞、或者刚失败过不久，都直接返回，不发请求。
// 拉完装上词并叫醒界面。
//
// # 失败是静默的
//
// 占位词是一块环境装饰，不是一次用户发起的动作：拉不到的时候界面上没有任何
// 属于它的位置可以说「为什么没有」（不弹错误、不转圈），占位词停在「搜索」
// 就是它的失败态。和联想词那一档不同——那一档在浮层里有自己的一句话
// （`suggestFailedNote`），这里连那句话都没有，所以失败之后 `at` 照记
// （退避期内白问是白问），界面上什么也不变。
package app

import (
	"context"
	"sync"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/hotwords"
)

// 热词的节奏。TTL / 失败退避和热榜歌名（`hotSongsTTL` / `hotSongsRetry`）同一档：
// 热搜词的更新比那个还慢，取同一档是不为它多想一层。
const (
	// hotWordsTTL 是一份词用多久再换。
	hotWordsTTL = 10 * time.Minute
	// hotWordsRetry 是失败之后多久才再问一次。
	hotWordsRetry = time.Minute
	// hotWordsShown 是词池最多留几条：占位词一次只显示一个，十条轮起来
	// 两分多钟才见一圈回环，再多是用户记不住的白拉。
	hotWordsShown = 10
	// hotWordMaxRunes 是单条词最多几个字。搜索框 240 宽，左边放大镜和内边距
	// 占掉 42，13 号字每个汉字 13px——240 里塞得下 15 个，但这个框最窄那档
	// 能缩到八十来像素（票 #286，`MinWidth(0)`），留一档余量，12 以上的词
	// 直接不要：被框边硬切一半的词比没有词更糟。
	hotWordMaxRunes = 12
)

// Hotwords 是搜索框热词这一摊。零值不挂——界面要判空（`Mounted`）。
type Hotwords struct {
	client *hotwords.Client

	mu    sync.Mutex
	words []string
	// at 是最近一次拉取**落定**的时刻（成功失败都算）：失败也记，退避才有起点。
	at      time.Time
	loading bool
}

// Mounted 报这一摊挂上没有。
func (h *Hotwords) Mounted() bool { return h != nil && h.client != nil }

// Words 是此刻的词池（已经按显示长度筛过、封过顶）。没拉到时为空。
func (h *Hotwords) Words() []string {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.words
}

// Refresh 在需要时去后台拉一份热词，拉完调 wake。
//
// 它是每帧可调的：见文件头「和热榜歌名是同一对形状」。
func (h *Hotwords) Refresh(wake func()) {
	if !h.Mounted() {
		return
	}
	h.mu.Lock()
	ttl := hotWordsTTL
	if len(h.words) == 0 {
		ttl = hotWordsRetry
	}
	if h.loading || (!h.at.IsZero() && time.Since(h.at) < ttl) {
		h.mu.Unlock()
		return
	}
	h.loading = true
	h.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		got, err := h.client.Fetch(ctx)
		h.mu.Lock()
		h.loading, h.at = false, time.Now()
		// ⚠️ **只有「拉到了至少一条」才动 `words`**：拉到空（网易云这一趟没给，
		// 但这一趟是成功的）或者失败，都**保留上一份**——占位词是环境装饰，
		// 上一份哪怕放久了也还是能搜的词，把它们换成一个空池子再等下一趟，
		// 用户看到的是占位词闪回「搜索」，那一下没有任何信息量。空词池在
		// 退避那一格有另一个后果：TTL 按 `hotWordsRetry` 走，问得比平时勤。
		if err == nil && len(got) > 0 {
			h.words = shapeHotwords(got)
		}
		h.mu.Unlock()
		if wake != nil {
			wake()
		}
	}()
}

// shapeHotwords 把接口给的那批词筛成「摆得上搜索框」的词池：
// 长的丢（`hotWordMaxRunes`，按 rune 数——汉字一个就是一 rune，按字节切会把
// 一半的字算进去）、封顶 `hotWordsShown`。顺序不动：那是热度顺序。
func shapeHotwords(in []string) []string {
	out := make([]string, 0, hotWordsShown)
	for _, w := range in {
		if len([]rune(w)) > hotWordMaxRunes {
			continue
		}
		out = append(out, w)
		if len(out) == hotWordsShown {
			break
		}
	}
	return out
}

// TestOnlySetWords 直接摆一份词池。只给界面层的测试用——不指 fixture 的话
// 那些测试会打到真的 music.163.com 上去（同一个先例：`hotwords.Client` 的
// `TestOnlyPointHostsAtFixture`）。名字里带 `TestOnly` 是 Go 生态的约定：
// 编译器不会拦，但 grep 得到。
func (h *Hotwords) TestOnlySetWords(words []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.words = words
}

// ─────────────────────────────────────────────────────────────────────────────
// 挂到 App 上
// ─────────────────────────────────────────────────────────────────────────────

// mountedHotwords 是每个 App 挂着的这一摊。
var mountedHotwords sync.Map // *App → *Hotwords

// MountHotwords 把热词这一摊挂到 App 上。**在开窗口之前调**（`main.go`，和
// MountLyrics 同一批——它不发请求，挂上只是把零件递进去）。
func (a *App) MountHotwords(c *hotwords.Client) {
	mountedHotwords.Store(a, &Hotwords{client: c})
}

// HotwordsOf 是这个 App 的热词这一摊。还没挂过就现建一个空的：Refresh 会
// 直接返回，界面据此把占位词停在「搜索」。
func (a *App) HotwordsOf() *Hotwords {
	if v, ok := mountedHotwords.Load(a); ok {
		return v.(*Hotwords)
	}
	h := &Hotwords{}
	mountedHotwords.Store(a, h)
	return h
}
