// 把 B 站的错误码变成人话。
//
// # 为什么单独一个文件
//
// 「错误码 → 怎么办」是这个包的领域知识。散到各个调用点去写，同一个 -101
// 会在登录页说「请重新登录」、在取流处说「网络错误」——那是同一件事被说成
// 两种话，用户只会觉得应用自己也不知道出了什么事。
//
// 集中在这里还有一个好处：**这张表是可以被穷举测的**。`client_test.go` 里
// 已经测了「错误码映射到哨兵」，这里测的是「哨兵映射到一句用户看得懂的话」，
// 两张表都必须全，每个码都要有话可说。
//
// # 「没查到码怎么办」
//
// 往人类可读的方向退：把 B 站自己给的那句 message 拿出来当兜底。
// 那句话是中文的（"稿件不可见"这种），比把 `code: -400` 显示给用户强。
// 但它也可能是一句英文或者空串，那时才退到「B 站没告诉我们为什么」。

package bili

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/service/audio"
)

// codeMessages 是错误码到中文说明的表。
//
// 键是 B 站的业务 code（`APIError.Code`），值是**用户看得到**的话：
// 说清楚发生了什么、并且尽量带上「该怎么办」。
//
// ⚠️ 这里面出现的每个码都对应一次真实遇到的界面状态。不要为了「表更完整」
// 把没见过的码也填进去——填错的提示比没有提示更糟：用户会照着一条不存在的
// 建议去做事。
var codeMessages = map[int]string{
	// 登录态。
	-101: "登录已经过期了，重新扫码登录一下",
	-111: "登录状态有问题，重新登录一次就好",

	// 请求本身。
	-400: "B 站拒绝了这次请求",
	-403: "没有权限看这一项",
	-404: "这个稿件已经不在了",

	// 风控与限流。这三个的处置完全不同，所以话也不同：
	// 风控不该重试（重试就是继续撞墙），限流可以等。
	-352: "被 B 站的风控拦下了，等一会儿再试",
	-412: "请求被 B 站拦下了，换个网络或者等一会儿",
	-509: "操作太快了，等一会儿再试",

	// 稿件状态。这几个是 B 站对**内容**的判断，重试多少次都一样。
	10003: "这个稿件不存在",
	62002: "这个稿件设为私密了",
	62004: "这个稿件还在审核中",
	62012: "这个稿件只有 UP 主自己看得到",
	11010: "这个内容不存在",

	// 音乐库（B 站音乐）侧的码。
	7201006:  "这首音乐不存在或者已下架",
	72000000: "B 站说这个请求它没看懂",
	72010027: "这首音乐换了地址，已经跟不上了",
}

// Friendly 把一个错误变成一句能直接显示在界面上的话。
//
// 它**永远不返回空串**：认不出来的错误也要给用户一句能看的东西，
// 因为空串在播放条上表现为「打不开：」，那比任何一句兜底都更让人困惑。
//
// ⚠️ 不要把原始错误里的地址打出来：B 站的错误消息有时带着 URL 片段，
// 而取流的地址带签名。
func Friendly(err error) string {
	if err == nil {
		return ""
	}

	// 先看是不是我们自己分类过的那几种。
	switch {
	case errors.Is(err, ErrNotLoggedIn):
		return codeMessages[-101]
	case errors.Is(err, ErrRiskControl):
		return codeMessages[-352]
	case errors.Is(err, ErrTooFrequent):
		return codeMessages[-509]
	case errors.Is(err, ErrCSRF):
		return codeMessages[-111]
	}

	// 取流自己的几种。
	switch {
	case errors.Is(err, ErrNoPart):
		// 🔴 **要排在 `ErrNetwork` 前面**：问不出分 P 底下多半真的是网络/风控，可
		// 用户看到「连不上 B 站」不知道能做什么，而这一条有明确的下一步。
		return "问不出这一集是哪一集，等网络好一些，或者换个词重新搜一次"
	case errors.Is(err, ErrNoAudioFlow):
		// 🔴 **「真的没有音频流」和「没取到」必须说成两句话**。
		//
		// 用户分不清这两件的时候，做的动作正好相反：以为「没取到」就会去
		// 查网、换网、等它好，而这一条等一万年还是没有音频流。所以这一句
		// 既说「为什么」（B 站压根没给这一条音频流），也说「还能怎么办」
		// （打开里面的单集 / 在 B 站里打开它听）。
		return "B 站没有给这一条单独的音频流（合集、剧集和歌单那一类多半是这样）。打开里面的单集，或者在 B 站里打开它听"
	case errors.Is(err, ErrStreamExpired):
		return "播放地址过期了，正在换一条新的"
	case errors.Is(err, ErrNoLoginForOnline):
		return "在线播放要先登录 B 站账号"
	case errors.Is(err, ErrPreviewOnly):
		// 试听片段要说清「为什么只有这么一点」和「怎么办」。不说的话用户听到
		// 半首歌停住，只会觉得是应用坏了。
		return "这一首要会员才有完整版，现在只有试听片段"
	}

	// 字节源拉不动了。**先说「不是歌坏了」**——这一条很容易被误读成
	// 「文件损坏了」，而用户的正确反应是换网或者等一会儿，不是清缓存重下。
	if errors.Is(err, audio.ErrStreamDead) {
		if errors.Is(err, audio.ErrStreamChanged) {
			return "B 站换了这条流，正在从刚才的位置重开"
		}
		return "连不上 B 站这条音频流了（网络或地址的问题，不是歌坏了）"
	}

	// B 站的业务错误：查表。
	var api *APIError
	if errors.As(err, &api) {
		if msg, ok := codeMessages[api.Code]; ok {
			return msg
		}
		if m := cleanMessage(api.Message); m != "" {
			return m
		}
		return fmt.Sprintf("B 站返回了一个看不懂的错误（%d）", api.Code)
	}

	// 取到流了但没有可播的：那句话里已经带了「为什么」（没大会员 / 稿件已下架）。
	if errors.Is(err, ErrNoStream) {
		// 🔴 **只留后半句**：前半句「没能取到可播的音频流」是**我们自己的内部
		// 说法**，而播放条上只有一行（还带省略号），挤掉的正是后半句——
		// 后半句才是用户看得懂、也照着做得动的话（「换一档音质或用大会员
		// 账号」）。
		return trimStreamPrefix(err.Error())
	}

	if errors.Is(err, ErrNetwork) {
		return "连不上 B 站，检查一下网络"
	}
	if strings.Contains(err.Error(), "context canceled") {
		// 用户自己切歌/退出的，不该在界面上留一句红字。
		return ""
	}
	if strings.Contains(err.Error(), "timeout") ||
		strings.Contains(err.Error(), "deadline exceeded") {
		return "等 B 站回应超时了，网络可能不通"
	}
	// 兜底。**绝不把地址原样打出去**：B 站的错误消息有时带 URL 片段，
	// 而取流地址带 `upsig` 签名，出现在界面上等于把这条流公开了。
	msg := trimBiliPrefix(err.Error())
	if msg == "" {
		return "出了点问题"
	}
	if strings.Contains(msg, "http://") || strings.Contains(msg, "https://") {
		return "出了点问题（详情看日志）"
	}
	return msg
}

// trimBiliPrefix 去掉我们自己加的包名前缀。
//
// `bili: 没能取到可播的音频流：这一首没有无损……` 里的 `bili: ` 对用户没有
// 意义，但整句是有意义的，所以只剥前缀不砍句子。
func trimBiliPrefix(s string) string {
	return strings.TrimSpace(strings.TrimPrefix(s, "bili: "))
}

// trimStreamPrefix 去掉取流错误里的**内部说法**，只留用户看得懂的那半句。
//
// 「bili: 没能取到可播的音频流: 这一首没有无损（要的是「无损」），换一档音质
// 或用大会员账号」里，前半句说的是**我们内部的状态**，后半句才是**用户的
// 下一步**。播放条上那一行还带省略号，所以前半句挤掉的正是后半句。
func trimStreamPrefix(s string) string {
	s = trimBiliPrefix(s)
	for _, p := range []string{"没能取到可播的音频流：", "没能取到可播的音频流: "} {
		s = strings.TrimSpace(strings.TrimPrefix(s, p))
	}
	return s
}

// Recoverable 报这个错**再试一次有没有用**。
//
// 判据只有一条：「再问一次 B 站，会不会给出不一样的答案」。
//
//	网络断了、超时了、风控拦了、地址过期了	会变 → 可重试
//	B 站说没有音频流、稿件被删了、要大会员	不会变 → 不可重试
//
// 界面上据此决定给不给「重试」。**给一个按了也没用的重试比不给更糟**——
// 用户按两次发现还是那样，就不再信这个应用了；而「不可重试」那一句自己
// 已经说了还能怎么办（换一档音质 / 去 B 站里打开它听）。
func Recoverable(err error) bool {
	if err == nil {
		return false
	}
	switch {
	// 「压根没有音频流」：B 站这么说了，再问一次还是这一句。
	case errors.Is(err, ErrNoAudioFlow):
		return false
	// 「查到了但没得放」：换一档音质、换个账号也许有戏，但**原样重试**没有用。
	case errors.Is(err, ErrNoStream):
		return false
	// 要会员才有完整版：等不来。
	case errors.Is(err, ErrPreviewOnly):
		return false
	// 没登录：重试还是没登录，要的是重新登录（那句话已经说了）。
	case errors.Is(err, ErrNoLoginForOnline), errors.Is(err, ErrNotLoggedIn):
		return false
	// 风控和限流：等一会儿会变（`Friendly` 已经说了「等一会儿再试」）。
	case errors.Is(err, ErrRiskControl), errors.Is(err, ErrTooFrequent):
		return true
	// 问不出分 P：底下多半是网络抖了一下。
	case errors.Is(err, ErrNoPart):
		return true
	// 地址过期：换一条新的就好。
	case errors.Is(err, ErrStreamExpired):
		return true
	// 字节源拉不动：网络或地址的问题，换张网 / 再试一次。
	case errors.Is(err, audio.ErrStreamDead):
		return true
	// 没拿到 HTTP 响应（断网、超时、DNS）。
	case errors.Is(err, ErrNetwork):
		return true
	}
	// B 站的业务错误：逐个码判。
	var api *APIError
	if errors.As(err, &api) {
		switch api.Code {
		case -352, -412, -509:
			// 风控 / 被拦 / 太快：等一会儿再来。
			return true
		case 72010027:
			// 「这首音乐换了地址，已经跟不上了」——再问一次也许就是新地址。
			return true
		default:
			// -101（登录过期）、-111、-400、-403、-404、10003、62002、
			// 62004、62012、11010、7201006、72000000 ……：重试多少次都一样。
			return false
		}
	}
	// 超时那一类（不是 ErrNetwork 包住的）。
	if strings.Contains(err.Error(), "timeout") ||
		strings.Contains(err.Error(), "deadline exceeded") {
		return true
	}
	// ⚠️ **不认识的错按「可重试」算**：用户宁可多点一次重试，也不要被一句
	// 「不可重试」堵死——而这一句的文案本身已经说了出了什么事，用户看得见。
	return true
}

// cleanMessage 把 B 站给的消息收拾干净再用。
//
// 只认**确实是人话**的消息：空串、纯数字、"success"、"null" 都要退掉。
// 直接把 `message` 显示出去的风险是它可能是一段英文或者一句没人看得懂的
// 内部话术，而那比兜底文案更糟。
func cleanMessage(s string) string {
	s = strings.TrimSpace(trimBiliPrefix(s))
	if s == "" || s == "success" || s == "null" || s == "OK" {
		return ""
	}
	for _, r := range s {
		if r > 0x7f { // 至少有一个非 ASCII 字符，够粗但够用
			return s
		}
	}
	return ""
}
