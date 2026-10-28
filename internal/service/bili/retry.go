// 「这个错**现在**能不能再问一次」的判据。**全项目只有这一处**。
//
// # 为什么单独一个文件
//
// 「能不能重试」和「错误码 → 一句人话」（`friendly.go`）是**两件事**：
// 前者说「用户按下去有没有用」，后者说「出了什么事」。混在一个文件里的话，
// 改一句文案会顺手改掉判据——而那两件事的**后果完全不同**（一句是说错了，
// 一句是让用户白按一次）。
//
// 🔴 **它必须是唯一的一处**（票 #346）：播放条上那枚「重试」和下载记录页上
// 那一枚「重试」是**同一个动作**（再问一次 B 站），所以两处必须用同一张表。
// 各判各的的话，同一个错会在一个界面上给「重试」、在另一个界面上不给，
// 而用户看到的现象是「这个应用的规矩看人下菜」。
//
// # 判据只有一条
//
// 「**现在**立刻再问一次，会不会给出不一样的答案」。
//
//	会变   网络断了、超时了、地址过期了、响应被截断了
//	不会变 B 站说没有音频流、稿件被删了、要大会员、风控、限流
//
// ⚠️ **「立刻」这两个字是判据的一部分**：界面上那枚「重试」按下去就是
// **立刻**再问一次（`App.Retry` 里没有退避），所以「等一会儿就好」的那两类
// （风控、限流）判成**不可重试**——不是「再问多少次都没用」，而是「**现在**
// 再问还是撞同一面墙」。用户过一会儿自己再放一次，那是他的动作，不是应用
// 替他按的（票 #316 收窄了风控，票 #346 把限流也收到同一档）。
//
// ⚠️ **给一个按了也没用的重试比不给更糟**：用户按两次发现还是那样，就不再
// 信这个应用了；而「不可重试」那一句自己已经说了还能怎么办（换一档音质 /
// 去 B 站里打开它听 / 等一会儿再放一次）。

package bili

import (
	"errors"
	"strings"

	"github.com/ShellMonster/PudMusic/internal/service/audio"
)

// retryRule 是这张表里的一行。
type retryRule struct {
	// what 是这一类错的名字（测试和日志里用）。
	what string
	// match 判这个错是不是这一类。
	match func(error) bool
	// retry 是这一类**能不能立刻再问一次**。
	retry bool
}

// recoverableTable 是判据本身。**每一行都要能说出「为什么」**。
//
// ⚠️ **顺序有意义**：先认哨兵，再认业务码——`APIError.Is` 会把 -352/-412
// 映射成 `ErrRiskControl`、把 -509 映射成 `ErrTooFrequent`，所以按哨兵判的
// 那几行必须排在按码判的那几行前面。
var recoverableTable = []retryRule{
	// ── 不可重试：立刻再问一次，答案不会变 ──
	{what: "压根没有音频流", match: isErr(ErrNoAudioFlow), retry: false},
	{what: "查到了但没得放", match: isErr(ErrNoStream), retry: false},
	{what: "要会员才有完整版", match: isErr(ErrPreviewOnly), retry: false},
	{what: "没登录", match: isAnyErr(ErrNoLoginForOnline, ErrNotLoggedIn), retry: false},
	{what: "风控（-352/-412）", match: isErr(ErrRiskControl), retry: false},
	// 🔴 **限流也不给「立刻重试」**（票 #346）：同一个判据同样适用于它——
	// 立刻再问还是「太快了」，还是撞同一面墙。它和风控的**区别只在话**上
	// （「等一会儿再试」），不在「能不能立刻再问」上。
	{what: "限流（-509）", match: isErr(ErrTooFrequent), retry: false},
	// 一页 HTML：多半是风控挡的。它和真风控**分开**（票 #346），但处置
	// 一样——立刻再问还是撞同一面墙。
	{what: "响应不是 JSON", match: isErr(ErrBadResponse), retry: false},
	{what: "这一条解不了", match: isErr(audio.ErrUnsupportedFormat), retry: false},

	// ── 可重试：立刻再问一次，答案可能就变了 ──
	// 🔴 **截断的响应归这一档**（票 #346）：它多半是网络抖了一下，响应没
	// 传完——再问一次有可能拿到完整的那一份。
	{what: "响应被截断了", match: isErr(ErrTruncatedResponse), retry: true},
	{what: "问不出分 P", match: isErr(ErrNoPart), retry: true},
	{what: "地址过期", match: isErr(ErrStreamExpired), retry: true},
	{what: "字节源拉不动", match: isErr(audio.ErrStreamDead), retry: true},
	{what: "网络没通", match: isErr(ErrNetwork), retry: true},
}

// retryByCode 是**哨兵之外**的业务码的判据。
//
// ⚠️ **-352/-412/-509 不在这里**：那三个码由 `APIError.Is` 映射成哨兵
// （`ErrRiskControl` / `ErrTooFrequent`），而哨兵在上面的表里已经判过了。
// 在这里再写一遍就是**同一个判据散在两处**——两处一旦不一致，判的是哪一处
// 取决于表的顺序，而没人会去查那个。
//
// ⚠️ **表里没有的码按「不可重试」算**：B 站说的那些「稿件没了 / 要大会员 /
// 没权限」重试多少次都一样，而给一个按了也没用的「重试」比不给更糟。
var retryByCode = map[int]bool{
	// 「这首音乐换了地址，已经跟不上了」——再问一次也许就是新地址。
	72010027: true,
}

// Recoverable 报这个错**现在立刻再问一次有没有用**。
//
// 界面上据此决定给不给「重试」。**给一个按了也没用的重试比不给更糟**——
// 用户按两次发现还是那样，就不再信这个应用了；而「不可重试」那一句自己
// 已经说了还能怎么办。
func Recoverable(err error) bool {
	if err == nil {
		return false
	}
	for _, r := range recoverableTable {
		if r.match(err) {
			return r.retry
		}
	}
	// B 站的业务错误：逐个码判。
	var api *APIError
	if errors.As(err, &api) {
		return retryByCode[api.Code]
	}
	// 超时那一类（不是 ErrNetwork 包住的）。
	if strings.Contains(err.Error(), "timeout") ||
		strings.Contains(err.Error(), "deadline exceeded") {
		return true
	}
	// ⚠️ **不认识的错按「可重试」算**：用户宁可多点一次重试，也不要被一句
	// 「不可重试」堵死——而这一句的文案本身已经说了出了什么事，用户看得见。
	//
	// ⚠️ 这一条也是**下载那一路**的兜底：那边的错多半是这一层自己造的
	// （拉字节断了、写文件失败），它们不认得 B 站的任何一个哨兵。
	return true
}

// isErr 造一个「这个错是不是那一个哨兵」的判据。
func isErr(target error) func(error) bool {
	return func(err error) bool { return errors.Is(err, target) }
}

// isAnyErr 造一个「这个错是不是这几个哨兵之一」的判据。
func isAnyErr(targets ...error) func(error) bool {
	return func(err error) bool {
		for _, t := range targets {
			if errors.Is(err, t) {
				return true
			}
		}
		return false
	}
}
