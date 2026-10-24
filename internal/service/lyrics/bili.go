// B 站那一份字幕：**下载、解析、编回 LRC**。
//
// # 它不是 LRC
//
// B 站给的是 JSON（`{"body":[{"from":…, "to":…, "content":"…"}]}`），而缓存里存的是
// LRC 文本（`Entry.Lyrics`）——所以这里**解析成行、再编成 LRC**，不是把 JSON 塞进
// `Parse`（那条路只认 `[mm:ss.xx]` 那种标签；JSON 喂进去会一行都出不来）。
//
// # 判据逐条照上游
//
// 上游 `src/components/lyrics/get-lyrics.ts` 的 `getLyricsByBili`：
//
//	时间   `Math.round(from * 1000)` 毫秒
//	文字   去掉首尾的 `♪♫` 再 trim
//	空的   丢掉
//	次序   按时间排序
//
// ⚠️ **抄错时间那一项的症状是「歌词整体早/晚几秒」**，而歌词本身看着完全正常
// ——用户只会以为「这首歌的词对不上」，不会想到是解析错了。
//
// # 那一趟网络往返
//
// 字幕文件在 `aisubtitle.hdslb.com`（静态文件），**不在 B 站的 API 域上**，
// 而且它是**光秃秃的一份 JSON**（没有 `{"code":…,"data":…}` 那个外壳）——所以
// 它**不走** `bili.Client.Do`，走这里自己发一次 GET。
//
// 凭据边界那边它是 **referer 档**（`.hdslb.com` 在白名单里）：拿得到 Referer、
// 拿不到 Cookie，与 CDN 音频那一档同一条规则。
package lyrics

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"
)

// 歌词的**来路**。空串 = 不知道（老缓存里没有这一栏）。
//
// ⚠️ **它不是「歌词源」**：那两个（`SourceNetease` / `SourceLRCLib`）是用户搜的
// 时候挑的**哪一个源**，而这两个说的是**这一份歌词是怎么来的**——B 站自动带出来
// 的，还是用户在搜索里找来的。界面上要把这两件事分开说（见 `ui.lyrics.go`）。
const (
	// OriginBili 是「B 站自己带出来的那一份」。
	OriginBili = "bili"
	// OriginSearch 是「用户在歌词搜索里找来的那一份」。
	OriginSearch = "search"
)

// BiliSubtitle 是 B 站那份字幕的 JSON 形状。
//
// ⚠️ **只声明用得到的字段**：B 站这一份里还有 `id` / `lan` / `lan_doc` 等等，
// 抄进来等于把接口的形状再抄一遍（同一个道理见 `bili/quality.go` 的 `dashInfo`）。
type BiliSubtitle struct {
	// Body 是那些句子。**空数组 = 这一份没有词**（不是「读不出来」）。
	Body []BiliSubtitleLine `json:"body"`
}

// BiliSubtitleLine 是其中一句。
type BiliSubtitleLine struct {
	// From 是这一句开始的时刻（**秒**，带小数）。
	From float64 `json:"from"`
	// To 是这一句结束的时刻（秒）。**不用它**——界面只问「哪一句正在唱」，
	// 而上游也没有用它（`get-lyrics.ts` 只读 `from` 和 `content`）。
	To float64 `json:"to"`
	// Content 是这一句的字。
	Content string `json:"content"`
}

// ParseBiliSubtitle 解析 B 站那份字幕，**按时间排好序**。
//
// 空的、没有句子的、字全是空的，都产不出东西（返回空切片，不是错误）。
//
// ⚠️ **读不出来才报错**：那是「B 站改了格式」，而「这一份没有词」是**正常**
// 的一种——很多歌压根没有字幕，那不是错。
func ParseBiliSubtitle(raw []byte) ([]Line, error) {
	var s BiliSubtitle
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("lyrics: B 站那份字幕读不懂: %w", err)
	}
	out := make([]Line, 0, len(s.Body))
	for _, b := range s.Body {
		// ⚠️ **先去掉首尾的 `♪♫`、再 trim**（上游 `get-lyrics.ts` 就是这个次序）：
		// 顺序反了的话，「♪ 词 ♫」这种两边都带音符的句子会只剩下音符。
		text := strings.TrimSpace(strings.Trim(b.Content, "♪♫"))
		// ⚠️ **换行拍平**：缓存里存的是 LRC 文本，而 LRC 是**一行一句**的——
		// 内容里带着换行的话，那一句会被劈成两半，后半截成了没有时间标签的
		// 一行（读的时候整个丢掉），用户看到的是「这一句说到一半没了」。
		//
		// 上游那边不用管这件事：它把内容原样放进一个数组，界面上那一句自己换行。
		text = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(text)
		if text == "" {
			// 空的那几句丢掉：上游也是丢掉的，而留着它们的话歌词区会多出几行
			// 什么都没有的行——用户看到的是歌词中间夹着几个空行。
			continue
		}
		out = append(out, Line{At: biliAt(b.From), Text: text})
	}
	// 排序是**稳定**的：同一时刻的两句保留 JSON 里的先后（上游那边是
	// `toSorted`，现代 JS 里是稳定的）。
	slices.SortStableFunc(out, func(a, b Line) int { return cmp.Compare(a.At, b.At) })
	return out, nil
}

// biliAt 把 B 站的「秒（带小数）」换成时刻。
//
// ⚠️ **`Math.round(from * 1000)`**（上游 `get-lyrics.ts`）：毫秒取整，**不是**
// 截断。差这一毫秒看不出来，而差一整秒（截断 vs 取整差得少时是 0，多时是 1ms）
// ——真正会出事的是**秒与秒之间那一段的累加**：每一句都少 0.4ms，听不出来；
// 但每一句都少 0.5 秒（有人会拿 `int(from*1000)` 写）的话，整首歌是**越唱越早**。
func biliAt(from float64) time.Duration {
	return time.Duration(math.Round(from*1000)) * time.Millisecond
}

// ToLRC 把几行歌词编成 LRC 文本。
//
// ⚠️ **编出来的文本要能被 `Parse` 原样读回去**：时间戳写成 `[mm:ss.mmm]`，而
// `Parse` 的毫秒是**不足三位补零**的（`[00:12.5]` 读成 12500 毫秒）。补错一位
// 的话整首歌的词都差一个数量级，而歌词本身看着完全正常。
//
// ⚠️ **分钟只有两位**（`Parse` 的正则也是 `\d{1,2}`）：一首歌超过 99 分钟的
// 那种，时间戳会读不出来（那一行于是整个丢掉）。上游也有同一个限制
// （`index.tsx:29` 的正则一模一样），而歌词到那儿早该唱完了。
func ToLRC(lines []Line) string {
	var b strings.Builder
	for _, l := range lines {
		ms := l.At.Milliseconds()
		if ms < 0 {
			ms = 0
		}
		fmt.Fprintf(&b, "[%02d:%02d.%03d]%s\n", ms/60000, (ms/1000)%60, ms%1000, l.Text)
	}
	return b.String()
}

// ErrNoSubtitle 是「B 站这一条没有字幕」。
//
// ⚠️ **它不是错误**：那是**正常**的一种（很多歌没有字幕）。留着它是为了让调用方
// 能把「没有」和「拉不到」分开说——前者不用重试，后者可以再试一次。
var ErrNoSubtitle = errors.New("lyrics: B 站这一条没有字幕")

// FetchBiliSubtitle 下载并解析 B 站那一份字幕。
//
// ⚠️ **地址空就是没有字幕**（不是错误）：B 站给这一条没带字幕的时候，
// 调用方压根不该发这个请求。
//
// ⚠️ **超时在这一层的 ctx 上**（10 秒，和两个歌词源那一份一样，见 `search.go` 的
// `requestTimeout`），而不是 `http.Client.Timeout`——那样的话两个超时哪一个先到
// 说不清，而「说不清」的那一种最难复现。
func FetchBiliSubtitle(ctx context.Context, client *http.Client, rawURL string) ([]Line, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, ErrNoSubtitle
	}
	if client == nil {
		return nil, errors.New("lyrics: 没有网络层")
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lyrics: B 站字幕返回 %s", resp.Status)
	}
	// 限长：见 maxResponseBytes 的注释（这一份文件其实只有几 KB）。
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("lyrics: B 站字幕超过 %d 字节", maxResponseBytes)
	}
	return ParseBiliSubtitle(body)
}
