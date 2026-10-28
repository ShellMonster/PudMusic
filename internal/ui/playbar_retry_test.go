package ui

// 播放条上那一句错的「**还能怎么办**」。
//
// # 为什么是「一枚键」而不是「一句更长的说明」
//
// 「为什么 + 还能怎么办」两半加起来有一二十个字——而播放条上那一列只有
// 75.7 像素宽（实测），一句 26 个字的话上屏只剩十来个。所以：
//
//	能重试	「重试」——网络断了、地址过期了，再试一次就有可能好
//	不能重试但有 B 站地址	「在 B 站打开」——B 站说这一条没有音频流，
//	                    用户能做的就是去 B 站里听它
//
// 🔴 **这两枚搬进「详情」面板了**（票：左下角出错那三行太碎）：错误文案从
// 那一列里搬走了，而它们原来和那句话挤在同一行（75.7 + 62 像素）。面板由
// **播放键那一枚**（出错时它变成红色的）点开——完整那一句 + 能做的事都在
// 里面（`playbar_err.go`）。
//
// # 三条规矩
//
//  1. **不能重试的时候不给「重试」**：给一个按了也没用的重试比不给更糟——
//     用户按两次发现还是那样，就不再信这个应用了。
//  2. **出错之前播放条一个像素都不动**：那一枚键只在出错的时候才建。
//  3. **「重试」和「在 B 站打开」不同时摆**：可重试的那一档给「重试」，而
//     不可重试的那一档给「在 B 站打开」（和原来一样）。

import (
	"net/http"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestThePlayBarOffersARetryWhenTheFailureIsWorthRetrying 取流失败（可恢复）。
func TestThePlayBarOffersARetryWhenTheFailureIsWorthRetrying(t *testing.T) {
	b := newUIExplore(t)
	// 取流那一趟服务端炸了——这就是「没取到」那一类，重试有可能好。
	b.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	s, tt, a := newSpaceShell(t, b, true, "77")
	defer func() { _ = s.hist.Close() }()

	if err := a.PlayFrom(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1retryTest", Cid: "1", Title: "这一首",
	}); err != nil {
		t.Fatalf("放不进去：%v", err)
	}

	// 🔴 **可重试的那一档，播放键那一枚**就是「重试」**（点它直接重试，一步
	// 到位——网络抖了一下，用户要的就是「再试一次」）。完整那一句在它的悬停
	// 提示里。
	waitFor(t, "播放条上出现「重试」那一枚", func() bool {
		tt.Frame()
		return showsText(tt, "重试这一首")
	})
}

// TestThePlayBarDoesNotOfferARetryThatWouldDoNothing 真没有音频流的那一类。
//
// 这一条钉的是**不给**：给一个按了也没用的重试，比不给更糟。
//
// 假 B 站对取流那一趟回的是 `{"code":0,"data":{}}`——dash 是空的，
// 于是 B 站说的是「这个稿件没有音频流」，而它重试一万次还是这一句。
func TestThePlayBarDoesNotOfferARetryThatWouldDoNothing(t *testing.T) {
	b := newUIExplore(t)
	s, tt, a := newSpaceShell(t, b, true, "77")
	defer func() { _ = s.hist.Close() }()

	if err := a.PlayFrom(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1noFlowTest", Cid: "1", Title: "这一首",
	}); err != nil {
		t.Fatalf("放不进去：%v", err)
	}

	waitFor(t, "播放条上出现那个出错键", func() bool {
		tt.Frame()
		return showsText(tt, errPlayKeyLabel)
	})
	// ⚠️ **「还能怎么办」在「详情」面板里**（理由见文件头那��段）。
	openErrDetail(t, tt, "")
	waitFor(t, "详情面板里出现「在 B 站打开」", func() bool {
		tt.Frame()
		return showsText(tt, "在 B 站打开")
	})
	if showsText(tt, "重试") {
		t.Error("给了一枚按了也没用的「重试」")
	}
}

// TestThePlayBarDoesNotOfferARetryThatWouldHitTheRiskControlAgain 盯风控
// 那一类（票 #316）。
//
// 🔴 **这一条钉的是「不给」**：假 B 站对取流那一趟回 `{"code":-352}`（风控），
// 而那枚「重试」是**立刻**再取一次流（`App.Retry` 里没有退避）——所以按下去
// 就是**再撞一次风控**。用户点一次撞一次，点第二次再撞，于是他不再信这枚键。
func TestThePlayBarDoesNotOfferARetryThatWouldHitTheRiskControlAgain(t *testing.T) {
	b := newUIExplore(t)
	b.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, -352, nil)
	})
	s, tt, a := newSpaceShell(t, b, true, "77")
	defer func() { _ = s.hist.Close() }()

	if err := a.PlayFrom(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1riskTest", Cid: "1", Title: "这一首",
	}); err != nil {
		t.Fatalf("放不进去：%v", err)
	}

	waitFor(t, "播放条上出现那个出错键", func() bool {
		tt.Frame()
		return showsText(tt, errPlayKeyLabel)
	})
	openErrDetail(t, tt, "")
	waitFor(t, "详情面板出来", func() bool {
		tt.Frame()
		return showsText(tt, "错误详情")
	})
	if showsText(tt, "重试") {
		t.Error("给了一枚按下去只会再撞一次风控的「重试」")
	}
}

// TestThePlayBarDoesNotOfferARetryThatWouldHitTheRateLimitAgain 盯限流
// 那一类（票 #346）。
//
// 🔴 **限流和风控是同一档**：那枚「重试」是**立刻**再取一次流（没有退避），
// 而限流要等的是 B 站的限流窗口过去——立刻再问还是「太快了」，还是撞同一
// 面墙。上一票（#316）只收窄了风控，限流留在「可重试」那一档，于是「点一次
// 撞一次」的循环原样留着。
func TestThePlayBarDoesNotOfferARetryThatWouldHitTheRateLimitAgain(t *testing.T) {
	b := newUIExplore(t)
	b.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, -509, nil)
	})
	s, tt, a := newSpaceShell(t, b, true, "77")
	defer func() { _ = s.hist.Close() }()

	if err := a.PlayFrom(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1rateTest", Cid: "1", Title: "这一首",
	}); err != nil {
		t.Fatalf("放不进去：%v", err)
	}

	waitFor(t, "播放条上出现那个出错键", func() bool {
		tt.Frame()
		return showsText(tt, errPlayKeyLabel)
	})
	openErrDetail(t, tt, "")
	waitFor(t, "详情面板出来", func() bool {
		tt.Frame()
		return showsText(tt, "错误详情")
	})
	if showsText(tt, "重试") {
		t.Error("给了一枚按下去只会再撞一次限流的「重试」")
	}
}
