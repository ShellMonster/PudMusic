package ui

// 联想面板的**键盘导航**（2026-11 重设计）：↑↓ 在历史 chip 和联想词两档之间
// 上下走，回车搜选中的那一条，Esc 收面板（这一条原来就有）。
//
// 这一组判的是**行为和选中态**：
//
//   - ↓ 从「没选中」走到第一条，再往下走到下一档（历史 → 联想），↑ 退回去；
//   - 选中的那一条换「正在输入」/「选中行」那一档——chip 是 `accentSoft` 的底，
//     联想行是 `t.SurfaceHover`（和悬停同一片底，理由见实现注释）；
//   - 回车搜选中的那一条（不是框里敲的那半截词），词填回框里、面板收起、落到
//     搜索页；
//   - 敲了一个字选中态就清掉（新的一摊词里没有「这一条」）。
//
// 顺序是**画出来的顺序**：历史那一排在上、联想词那一档在下，↓ 从上往下走。

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// keysFixture 是键盘导航这一组的壳：两条历史 + 两条联想词，框里敲着「周杰」。
//
// ⚠️ **历史是「最近的在前」**：`hist.add` 把新加的顶到最前，所以先加「周杰伦」
// 再加「林俊杰」，画出来的顺序是「林俊杰、周杰伦」（和 B 站一致）。面板上那一摊
// （从上到下）：「历史搜索」标题、chip「林俊杰」chip「周杰伦」、「搜索联想」
// 标题、行「周杰伦 晴天」行「周杰伦 新歌」。
func keysFixture(t *testing.T) (*Shell, *ui.Tester) {
	t.Helper()
	f := newBiliFixture(t, 1)
	f.setSuggestTerms("周杰", "周杰伦 晴天", "周杰伦 新歌")
	s, tt := newSuggestShell(t, f)
	s.hist.add("周杰伦")
	s.hist.add("林俊杰")
	tt.Frame()
	typeInSearch(t, tt, "周杰")
	settleSuggestDebounce(s, tt)
	waitFrames(t, tt, "联想词那一档出来", func() bool { return tt.HasText("晴天") })
	settleSuggestEnter(t, tt)
	return s, tt
}

// keyNavDown / keyNavUp 在框里敲一下方向键（焦点在输入框上，方向键走 OnTextKey
// 那条路）。敲完画一帧。
func keyNavDown(tt *ui.Tester) { tt.Key(0, ui.KeyDown); tt.Frame() }
func keyNavUp(tt *ui.Tester)   { tt.Key(0, ui.KeyUp); tt.Frame() }

// ⚠️ **历史和联想的「选中底」不是同一个色**，而且两处的 `Find` 拿到的也不是
// 同一个矩形：chip 的 Find 拿到的是**标签按钮**那一格（chip 里还混着 × 的墨迹，
// 「有没有底」问不出来），行的 Find 拿到的是**整行**。所以两档各量一处：
//
//	chipSelected(chip 名) —— 标签按钮左边那 4 像素（chip 的底）是不是 accentSoft
//	rowSelected(联想词)   —— 行右端（词右边的空白）是不是 SurfaceHover
//
// 选中底和「正在输入」高亮是同一个 accentSoft：选中的那一条视觉上就是它。
func chipSelected(t *testing.T, tt *ui.Tester, name string, soft ui.Color) bool {
	t.Helper()
	r, ok := tt.Find(name)
	if !ok {
		t.Fatalf("浮层上没有 %q，文字有 %q", name, tt.Texts())
	}
	img := tt.Image()
	got := img.RGBAAt(clampX(img, int(r.X)-4), int(r.Y+r.H/2))
	return maxDiff(got, rgb(soft)) <= 3
}

func rowSelected(t *testing.T, tt *ui.Tester, term string, hover ui.Color) bool {
	t.Helper()
	r, ok := tt.Find(term)
	if !ok {
		t.Fatalf("浮层上没有 %q，文字有 %q", term, tt.Texts())
	}
	img := tt.Image()
	// 行右端那一列（padding 里）：整行的底色，不会被字画到。
	got := img.RGBAAt(clampX(img, int(r.X+r.W)-4), int(r.Y+r.H/2))
	return maxDiff(got, rgb(hover)) <= 3
}

// TestSuggestArrowKeysWalkThePanel 钉住 ↓ 从上往下走、↑ 退回去：
// 先按一下 ↓ 选中第一条历史，再按一下选中第二条，再往下走进联想那一档。
//
// ⚠️ 这一组里「周杰伦」chip **不匹配**框里的词（框里是「周杰」），所以它的灰底
// 不会被「正在输入」那档盖住——选中前后量得到差别。
func TestSuggestArrowKeysWalkThePanel(t *testing.T) {
	s, tt := keysFixture(t)
	th := BuildTheme(s.app.SettingsSnapshot(), false)
	soft := tok.Of(th).AccentSoft
	// 原型 `.sug-row.active`：`hover` 底（半透明，压在面板 `surface` 上）。
	panelBg := tok.Of(th).Surface
	hover := ui.RGB(over(tok.Of(th).Hover, panelBg).R, over(tok.Of(th).Hover, panelBg).G, over(tok.Of(th).Hover, panelBg).B)

	// ① 还没按：一条都没选。
	if chipSelected(t, tt, "林俊杰", soft) {
		t.Fatalf("还没按方向键「林俊杰」就选中了——前提没立起来")
	}

	// ② 按一下 ↓：选中第一条历史「林俊杰」（最近的在前）。
	keyNavDown(tt)
	if !chipSelected(t, tt, "林俊杰", soft) {
		t.Errorf("按了一下 ↓ 之后「林俊杰」没亮——第一条没被选中")
	}

	// ③ 再按一下 ↓：选中第二条历史「周杰伦」（「林俊杰」退回没选）。
	keyNavDown(tt)
	if !chipSelected(t, tt, "周杰伦", soft) {
		t.Errorf("按了两下 ↓ 之后「周杰伦」没亮——没有往下走")
	}
	if chipSelected(t, tt, "林俊杰", soft) {
		t.Errorf("按了两下 ↓ 之后「林俊杰」还亮着——选中态没有跟着走")
	}

	// ④ 再按一下 ↓：跨过那道空隙，选中联想第一条「周杰伦 晴天」。
	keyNavDown(tt)
	if !rowSelected(t, tt, "周杰伦 晴天", hover) {
		t.Errorf("按了三下 ↓ 之后「周杰伦 晴天」没亮——历史没走到联想那一档")
	}

	// ⑤ 按一下 ↑：退回联想第一条上面的「周杰伦」。
	keyNavUp(tt)
	if !chipSelected(t, tt, "周杰伦", soft) {
		t.Errorf("按了一下 ↑ 之后「周杰伦」没亮——没有退回去")
	}
}

// TestSuggestEnterSearchesThePickedRow 钉住回车搜**选中的那一条**，不是框里敲的
// 那半截词：词填回框里、面板收起、落到搜索页。
func TestSuggestEnterSearchesThePickedRow(t *testing.T) {
	s, tt := keysFixture(t)

	// 走到联想第一条「周杰伦 晴天」（历史两条 + 联想第一条 = 三下 ↓）。
	keyNavDown(tt)
	keyNavDown(tt)
	keyNavDown(tt)

	tt.Key(0, ui.KeyEnter)
	tt.Frame()

	if got := s.searchKeyword; got != "周杰伦 晴天" {
		t.Errorf("回车之后框里是 %q，want %q——回车搜的不是选中的那一条",
			got, "周杰伦 晴天")
	}
	if s.searchSuggest {
		t.Errorf("回车之后浮层还开着（searchSuggest=true）")
	}
	if got := s.router.Path(); got != searchPath {
		t.Errorf("回车之后在 %q，want %q——没有落到搜索页", got, searchPath)
	}
	if got := s.router.Query("q"); got != "周杰伦 晴天" {
		t.Errorf("回车之后 URL 上的 q 是 %q，want %q", got, "周杰伦 晴天")
	}
}

// TestSuggestEnterWithoutAPickSearchesTheTypedWord 钉住**没选中任何一条**时回车
// 搜的还是框里敲的那个词——键盘导航不许改掉原来「回车搜框里的词」。
func TestSuggestEnterWithoutAPickSearchesTheTypedWord(t *testing.T) {
	s, tt := keysFixture(t)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if got := s.searchKeyword; got != "周杰" {
		t.Errorf("没选中任何一条时回车，框里是 %q，want %q", got, "周杰")
	}
	if got := s.router.Query("q"); got != "周杰" {
		t.Errorf("没选中任何一条时回车，URL 上的 q 是 %q，want %q", got, "周杰")
	}
}

// TestSuggestTypingClearsThePick 钉住「又敲了一个字」选中态清掉：新的一摊词里
// 没有「这一条」，留着那个高亮会指着一条已经不对的词。
func TestSuggestTypingClearsThePick(t *testing.T) {
	s, tt := keysFixture(t)
	th := BuildTheme(s.app.SettingsSnapshot(), false)
	soft := tok.Of(th).AccentSoft

	keyNavDown(tt) // 选中第一条「林俊杰」
	if !chipSelected(t, tt, "林俊杰", soft) {
		t.Fatalf("按了 ↓ 之后「林俊杰」没选中——前提没立起来")
	}

	// 再敲一个字：那一摊词换了（防抖之后那一档会重拉），选中态先清掉——
	// 不等联想回来，敲字那一帧就该清。
	typeInSearch(t, tt, "x")
	if chipSelected(t, tt, "林俊杰", soft) {
		t.Errorf("敲了一个字之后「林俊杰」还亮着——旧的选中态没清")
	}
}

// mustFind 是 Find 的「找不到就直接红」版：这一组后面那几步都假设某一条还在。
func mustFind(t *testing.T, tt *ui.Tester, s string) ui.Rect {
	t.Helper()
	r, ok := tt.Find(s)
	if !ok {
		t.Fatalf("浮层上没有 %q，文字有 %q", s, tt.Texts())
	}
	return r
}
