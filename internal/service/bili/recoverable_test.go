package bili

// 「这个错重试有没有用」。
//
// # 为什么要有这一条判据
//
// 界面上要给不给「重试」那一枚键，全看它。
//
// **给一个按了也没用的重试比不给更糟**：用户按两次发现还是那样，就不再信
// 这个应用了（而它多半是好的——只是这一首放不了）。
//
// 而「真没有音频流」和「没取到」这两类，用户做的动作正好相反：
// 前者该换一首，后者该再试一次。混成一句的话，方向从头就是错的。
//
// 🔴 **这张表是全项目唯一的一处**（票 #346）：播放条上那枚「重试」和下载记录页
// 那一枚是同一个动作，所以两处必须用同一张表。

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/service/audio"
)

// TestRecoverableTellsWhatIsWorthRetryingAgain 盯这张表的两半。
//
// 🔴 **不可重试的那一半更要紧**：它决定了「不给重试」——
// 「B 站说这一条没有音频流」按一万次还是那一句。
func TestRecoverableTellsWhatIsWorthRetryingAgain(t *testing.T) {
	for _, c := range []struct {
		what string
		err  error
		want bool
	}{
		// ── 不可重试：再问一次 B 站，答案不会变 ──
		{"B 站说这一条没有音频流", ErrNoAudioFlow, false},
		{"查到了但没得放（没有无损）",
			fmt.Errorf("%w: 这一首没有无损", ErrNoStream), false},
		{"要会员才有完整版", ErrPreviewOnly, false},
		{"没登录", ErrNoLoginForOnline, false},
		{"登录态没了", &APIError{Code: -101, Endpoint: "/x"}, false},
		{"稿件不存在", &APIError{Code: 10003, Endpoint: "/x"}, false},
		{"稿件私密", &APIError{Code: 62012, Endpoint: "/x"}, false},
		{"没有权限", &APIError{Code: -403, Endpoint: "/x"}, false},
		// 🔴 **风控不可重试**（票 #316）：界面上那枚「重试」是**立刻**再取一次
		// 流（`App.Retry` 里没有退避），所以判成可重试的话，用户点一次撞
		// 一次风控。它和文件头 `:44-50` 写的那句是同一件事，两边必须一致。
		{"风控", ErrRiskControl, false},
		{"风控的码", &APIError{Code: -352, Endpoint: "/x"}, false},
		{"被拦的码", &APIError{Code: -412, Endpoint: "/x"}, false},
		// 🔴 **限流也不可重试**（票 #346）：同一个判据同样适用于它——立刻再问
		// 还是「太快了」，还是撞同一面墙。
		{"限流", ErrTooFrequent, false},
		{"限流的码", &APIError{Code: -509, Endpoint: "/x"}, false},
		// 「压根不是 JSON」（多半是风控挡的一页 HTML）：和真风控**分开**，
		// 但处置一样——立刻再问还是撞墙。
		{"响应不是 JSON", ErrBadResponse, false},

		// ── 可重试：再问一次，答案可能就变了 ──
		{"网络没通", ErrNetwork, true},
		{"超时", errors.New("context deadline exceeded"), true},
		// 🔴 **截断的响应是可重试的**（票 #346）：它多半是网络抖了一下，
		// 响应没传完——再问一次有可能拿到完整的那一份。
		{"响应被截断了", ErrTruncatedResponse, true},
		{"问不出分 P", ErrNoPart, true},
		{"地址过期", ErrStreamExpired, true},
		{"字节源拉不动", audio.ErrStreamDead, true},
		// 「这首音乐换了地址，已经跟不上了」——再问一次也许就是新地址。
		{"音乐换了地址", &APIError{Code: 72010027, Endpoint: "/x"}, true},
		// ⚠️ **不认识的错按「可重试」算**：用户宁可多点一次重试，也不要被一句
		// 「不可重试」堵死——而这一句的文案本身已经说了出了什么事。
		{"不认识的错", errors.New("别的错"), true},
	} {
		if got := Recoverable(c.err); got != c.want {
			t.Errorf("%s：Recoverable = %v，want %v", c.what, got, c.want)
		}
	}
	if Recoverable(nil) {
		t.Error("nil 也说成可以重试")
	}
}

// TestRiskControlIsNotWorthRetryingAgain 盯「风控不可重试」这一条（票 #316）。
//
// 🔴 **为什么这一条要单独钉**：界面上那枚「重试」是**立刻**再问一次 B 站
// （`App.Retry` 里没有退避），而 B 站的风控要等的是**时间**。所以风控判成
// 可重试的话，用户点一次撞一次，点第二次再撞——他按两遍发现还是那样，
// 就不再看这枚键了。
//
// ⚠️ **限流（-509）跟着一起判死了**（票 #346）：同一个判据同样适用于它——
// 立刻再问还是「太快了」，还是撞同一面墙。两者的**区别在话**上，不在
// 「能不能立刻再问」上。
func TestRiskControlIsNotWorthRetryingAgain(t *testing.T) {
	for _, c := range []struct {
		what string
		err  error
	}{
		{"风控（哨兵）", ErrRiskControl},
		{"风控包在别的错里", fmt.Errorf("%w: 取流那一趟被挡了", ErrRiskControl)},
		{"风控的码", &APIError{Code: -352, Endpoint: "/x"}},
		{"被拦的码", &APIError{Code: -412, Endpoint: "/x"}},
	} {
		if Recoverable(c.err) {
			t.Errorf("%s：判成了可重试——立刻再问一次还是撞同一面墙", c.what)
		}
	}
	// 限流是另一回事：它也要等（等的是 B 站的限流窗口过去），所以同样
	// 不给那枚「立刻重试」的键。
	if Recoverable(ErrTooFrequent) {
		t.Error("限流被判成可重试了——立刻再问还是太快，还是同一面墙")
	}
	if Recoverable(&APIError{Code: -509, Endpoint: "/x"}) {
		t.Error("限流的码（-509）被判成可重试了")
	}
}

// TestTheTruncatedResponseIsNotTheRiskControl 盯「截断」和「真风控」分得开
// （票 #346）。
//
// 🔴 **为什么这一条要单独钉**：上一票（#316）把「风控不可重试」收窄的时候，
// 「响应不是 JSON」被归到了 `ErrRiskControl` 里——于是**被截断的响应**（那
// 是网络抖了一下，多半再问一次就好）跟着风控一起被判成不可重试，丢了「重试」
// 键。那是收窄带出来的副作用，票里只说了「风控不该重试」。
//
// 所以这三件事必须各自分开：
//
//	真风控（-352/-412）	业务码，在 JSON 里面
//	响应不是 JSON	压根没拿到 JSON（多半是风控挡的一页 HTML）
//	响应被截断了	本来是 JSON，没传完 —— **可重试**
func TestTheTruncatedResponseIsNotTheRiskControl(t *testing.T) {
	truncated := fmt.Errorf("%w: GET /x: 响应被截断了", ErrTruncatedResponse)
	notJSON := fmt.Errorf("%w: GET /x: 响应不是 JSON", ErrBadResponse)

	if errors.Is(truncated, ErrRiskControl) {
		t.Error("被截断的响应被认成了风控")
	}
	if errors.Is(notJSON, ErrRiskControl) {
		t.Error("「响应不是 JSON」被认成了风控——它和真风控必须能分开")
	}
	if !errors.Is(truncated, ErrTruncatedResponse) {
		t.Error("被截断的响应没有被认成它自己")
	}
	if !errors.Is(notJSON, ErrBadResponse) {
		t.Error("「响应不是 JSON」没有被认成它自己")
	}

	// 判据那一份：截断可重试（多半是网络抖了一下），不是 JSON 不可重试
	//（多半是风控挡的页，立刻再问还是撞墙）。
	if !Recoverable(truncated) {
		t.Error("被截断的响应被判成不可重试了——多半是网络抖了一下，再问一次有可能好")
	}
	if Recoverable(notJSON) {
		t.Error("「响应不是 JSON」被判成可重试——多半是风控挡的页，立刻再问还是撞墙")
	}
}

// TestNoAudioFlowIsAlsoANoStream 盯两层判据不打架。
//
// `ErrNoAudioFlow` 是「没有音频流」这一种更细的情况，而它**同时**也是
// 「查到了但没得放」——所以 `errors.Is(err, ErrNoStream)` 仍然要成立。
// 不成立的话，别处按粗判据处理的地方会漏掉这一类（而那一类恰恰是最需要
// 说准的）。
func TestNoAudioFlowIsAlsoANoStream(t *testing.T) {
	err := fmt.Errorf("%w：稿件 BV1MbZnYoEk1 的第 29165486316 集没有音频", ErrNoAudioFlow)
	if !errors.Is(err, ErrNoStream) {
		t.Errorf("「没有音频流」没有被认成「没得放」：%v", err)
	}
	if !errors.Is(err, ErrNoAudioFlow) {
		t.Errorf("「没有音频流」没有被认成它自己：%v", err)
	}
}
