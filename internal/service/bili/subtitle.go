// B 站那一份字幕：它就**藏在取流响应里**。
//
// # 为什么不用另外的接口
//
// 上游 Biu 是从**取流那一趟**的响应里拿字幕的（`src/components/lyrics/get-lyrics.ts`
// 的 `getLyricsByBili`）：
//
//	const res = await getWebPlayerInfo(params)      // ← 就是取流那一趟
//	const subTitleUrl = res?.data?.subtitle?.subtitles?.[0]?.subtitle_url
//	                  || res?.data?.subtitle?.subtitles?.[0]?.subtitle_url_v2
//
// 而 Biu 那个 `getWebPlayerInfo` 打的是 `/x/player/wbi/playurl`——**和我们取流
// 打的是同一个接口**。所以不需要多发一次请求：同一趟响应里已经带着它了，我们
// 以前只是没读那一段（`playurlData` 只声明了三个字段，见 `stream.go`）。
//
// # 地址是**协议相对**的
//
// B 站给的是 `//aisubtitle.hdslb.com/…`，前面**没有** `https:`。原样拿它去发请求
// 的话，`http.NewRequest` 会把整串当成一个**路径**而不是一个地址——症状是
// 「请求发不出去」，而错消息里看不出是地址的问题。
//
// # 它不在 B 站的 API 域上
//
// 字幕文件在 `aisubtitle.hdslb.com`（静态文件），而 B 站的接口在 `api.bilibili.com`。
// 凭据边界那边它是 **referer 档**（`.hdslb.com` 在白名单里，见
// `app.DefaultInjectPolicy`）——拿得到 Referer、拿不到 Cookie，与 CDN 音频
// 那一档是同一条规则。
//
// # 🔴 **两个地址是一个一个试的，不是「取一个」**
//
// B 站给同一份字幕**两个**地址：`subtitle_url`（不带鉴权参数）与
// `subtitle_url_v2`（带一段 `auth_key`）。而**不带鉴权的那个可能要登录态**——
// 拿它去请求会 403。
//
// ⚠️ **而登录态在这一条路上根本到不了**：`SESSDATA` 的域是 `.bilibili.com`，
// 而字幕文件在 `.hdslb.com`——jar 按域匹配（cookie 发不过去），凭据边界也
// 明确只给 referer 档（`app.DefaultInjectPolicy` 的 `biliSuffixes` 只有
// `.bilibili.com`）。上游那个 Electron 客户端也一样：浏览器的 cookie 同样
// 按域匹配，发不到 `aisubtitle.hdslb.com` 上。
//
// 所以**唯一能授权这一趟的就是地址自己带的那段 `auth_key`**——也就是
// `subtitle_url_v2`。只试第一个地址的话，「取到了地址但下载 403」这一档就变成
// 「这一整条自动歌词静悄悄地不出现」（界面上照旧是「暂无歌词」，日志里只有一行）。
//
// 而**上游的 `||` 兜底只管「地址为空」那一种**：两个地址都有的时候它只给第一个。
// 所以「取地址」和「下载」必须分开——**这里给的是一个列表，下载的时候一个不行
// 就换下一个**（见 `lyrics.FetchBiliSubtitle`）。
package bili

import (
	"context"
	"strings"
)

// subtitleBlock 是取流响应里的 `data.subtitle`。
//
// ⚠️ **B 站不给这一段的时候整个字段都没有**（不是给一个空的 `subtitles`），
// 所以指针是 nil 的那一支要认。
type subtitleBlock struct {
	// Subtitles 是 B 站给的那几份。**通常只有一份**。
	Subtitles []subtitleItem `json:"subtitles"`
}

// subtitleItem 是其中一份。
type subtitleItem struct {
	// SubtitleURL 是那份字幕的地址（**协议相对**的）。
	//
	// ⚠️ **它可能不带鉴权参数**，而那一个可能要登录态（见文件头「两个地址是一个
	// 一个试的」）。所以它**不是**唯一的那一个地址。
	SubtitleURL string `json:"subtitle_url"`
	// SubtitleURLV2 是同一个地址的另一个形状（多带一段鉴权参数 `auth_key`）。
	//
	// ⚠️ **两个给的是同一份 JSON**，而**带鉴权的那个才是能下的那一个**。
	SubtitleURLV2 string `json:"subtitle_url_v2"`
}

// subtitleURLsOf 从取流响应里找出 B 站给的那份字幕的地址。空列表 = 这一条没有字幕。
//
// ⚠️ **只看 `subtitles[0]`**（上游 `get-lyrics.ts` 就是这么取的）：
// `subtitles[0].subtitle_url || subtitles[0].subtitle_url_v2`。往后翻是为了在
// 头一份没有地址时还能找到一份——而那已经是**另一份**字幕（多半是另一种语言），
// 拿来当这一首的词，用户看到的是一段对不上的字。
//
// ⚠️ **两个都留着，按该试的顺序**：第一个是不带鉴权的那个，第二个是带
// `auth_key` 的那个（见文件头）。**只给一个**的话，下载 403 就没有下一趟了。
func subtitleURLsOf(d *playurlData) []string {
	if d == nil || d.Subtitle == nil || len(d.Subtitle.Subtitles) == 0 {
		return nil
	}
	s := d.Subtitle.Subtitles[0]
	var out []string
	for _, raw := range []string{s.SubtitleURL, s.SubtitleURLV2} {
		if u := absoluteSubtitleURL(raw); u != "" {
			out = append(out, u)
		}
	}
	return out
}

// absoluteSubtitleURL 把协议相对的地址补成完整的 https 地址。
//
// ⚠️ **只补 `//` 开头的那一种**：别的形态（B 站有时候给的是完整地址）原样留着。
// 而一个**不像地址**的串（相对路径、HTML 片段）在这里就该当没有——它发出去
// 只会得到一个看不懂的错，而「这一条没有字幕」是这一层能说清的那一句。
func absoluteSubtitleURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return ""
	}
	return raw
}

// SubtitleURLs 问 B 站要一个 bvid+cid 的字幕地址（**按该试的顺序**）。
//
// ⚠️ **它是给「音乐库那一路」用的**：音乐库的取流走的是另一个接口
// （`/audio/music-service-c/url`，见 `audio.go`），那个返回里**没有**字幕。
// 而音乐库条目背后多半有一个投稿（`SongInfo` 里的 `Bvid` / `Cid`），那份投稿的
// 字幕就是这一首的词——所以这里替它问一次**同一个**取流接口，只看 `subtitle`
// 那一段。
//
// 投稿那一路**不用**这个方法：取流的那一趟已经带回来了（见
// `StreamRef.SubtitleURLs`）。
//
// 空列表 = 那一投稿没有字幕（**不是错误**）。
func (c *Client) SubtitleURLs(ctx context.Context, bvid, cid string) ([]string, error) {
	if bvid == "" || cid == "" {
		// 不是网络错也不是重试能救的：调用方没给全。
		return nil, nil
	}
	d, err := c.fetchPlayurl(ctx, bvid, cid, VideoQualityUnknown)
	if err != nil {
		return nil, err
	}
	return subtitleURLsOf(d), nil
}
