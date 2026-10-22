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

		// ── 可重试：再问一次，答案可能就变了 ──
		{"网络没通", ErrNetwork, true},
		{"超时", errors.New("context deadline exceeded"), true},
		{"风控", ErrRiskControl, true},
		{"被限速", ErrTooFrequent, true},
		{"问不出分 P", ErrNoPart, true},
		{"地址过期", ErrStreamExpired, true},
		{"字节源拉不动", audio.ErrStreamDead, true},
		{"风控的码", &APIError{Code: -352, Endpoint: "/x"}, true},
		{"被拦的码", &APIError{Code: -412, Endpoint: "/x"}, true},
	} {
		if got := Recoverable(c.err); got != c.want {
			t.Errorf("%s：Recoverable = %v，want %v", c.what, got, c.want)
		}
	}
	if Recoverable(nil) {
		t.Error("nil 也说成可以重试")
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
