package ui

// 播放条上那一枚「**还能怎么办**」。
//
// # 为什么是「一枚键」而不是「一句更长的说明」
//
// 播放条上那句话只有一行宽（还带省略号），而「为什么 + 还能怎么办」两半
// 加起来有一二十个字——写在那里必然被截掉一半，而截掉的那一半恰恰是
// **用户下一步要做的事**。所以：
//
//	能重试	「重试」——网络断了、风控拦了、地址过期了，再试一次就有可能好
//	不能重试但有 B 站地址	「在 B 站打开」——B 站说这一条没有音频流，
//	                    用户能做的就是去 B 站里听它
//
// # 两条规矩
//
//  1. **不能重试的时候不给「重试」**：给一个按了也没用的重试比不给更糟——
//     用户按两次发现还是那样，就不再信这个应用了。
//  2. **出错之前播放条一个像素都不动**：那一枚键只在出错的时候才建。

import (
	"net/http"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
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

	waitFor(t, "播放条上出现「重试」", func() bool {
		tt.Frame()
		return showsText(tt, "重试")
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

	waitFor(t, "播放条上出现那句话", func() bool {
		tt.Frame()
		return showsText(tt, "打不开：B 站没有给这一条单独的音频流（歌单、活动那一种多半是这样）。在 B 站里打开它听，或者换一首放")
	})
	if showsText(tt, "重试") {
		t.Error("给了一枚按了也没用的「重试」")
	}
	// ⚠️ **「还能怎么办」要给成一枚能按的键**：这一首在 B 站上是有的，
	// 而「去 B 站里听它」正是用户能做的事。
	waitFor(t, "播放条上出现「在 B 站打开」", func() bool {
		tt.Frame()
		return showsText(tt, "在 B 站打开")
	})
}
