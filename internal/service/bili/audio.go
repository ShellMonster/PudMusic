// B 站音乐库（`type=12` / sid）这一路：元信息、取流、收藏态。
//
// # 收藏夹里的 type=12 为什么以前放不了
//
// 收藏夹接口给的两条内容类型里，type=2（投稿）走的是 `/x/player/wbi/playurl`，
// 而 type=12（音乐库）走的是**完全另一套接口**——它没有 bvid、没有 cid，
// 只有一个 auid（我们叫它 sid）。那一套以前没接，于是这类条目照常画在收藏夹里、
// 照常进队列，抽屉里还标着「B 站音乐」，按播放才报错。
//
// 判据抄对了（`Media.Playable` 照抄 Biu 的 `[2, 12]`），被它放行的**那一路**
// 没跟上——「可放」于是成了界面上的谎言。这个文件就是补上那一路。
//
// # 🔴 试听片段：这一路最大的一个坑
//
// 问不到会员权限时，B 站**不报错**，而是给一条 `type = -1` 的**试听片段**
// （192K、开头几十秒、`size` 报 0）。地址照样能用、字节照样能下、播放器照样
// 出声——然后在用户以为歌还没放完的地方停住，界面上一个错都没有。
//
// 所以这里把 -1 当成「这一首放不出来」，报一句说清差在哪的话（`ErrPreviewOnly`）。
// 别的档位（自动 / 高音质）会**退到不用会员的那一档再问一次**，退的规矩和
// `pickAudio` 末尾那段一样；点名的「无损」不退——用户为无损付了钱。
//
// # ⚠️ 音质是**另一套编号**
//
// 音乐库的档位是 0/1/2/3（`MusicTier`），投稿那套是 30216/30280（`Quality`）。
// 见 `quality.go` 的文件头。
//
// 接口清单（照 Biu 上游的 service/ 目录）：
//
//	/audio/music-service-c/url              取流（api.bilibili.com）
//	/audio/music-service-c/web/song/info    元信息（**www.bilibili.com**）
//	/medialist/gateway/coll/resource/check  收藏态（api.bilibili.com）
package bili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 这三个接口的路径。
const (
	// pathSongURL 是音乐库的取流接口。上游 `service/audio-web-url.ts` 用的也是它。
	pathSongURL = "/audio/music-service-c/url"
	// pathSongInfo 是音乐库的元信息接口。
	//
	// ⚠️ 它在 **www.bilibili.com** 上而不是 api.bilibili.com：上游
	// `service/audio-song-info.ts` 顶上写的就是
	// `https://www.bilibili.com/audio/music-service-c/web/song/info`，而它用的
	// 是 `biliRequest`（`service/request/index.ts` 里那个 baseURL 是 www 的）。
	pathSongInfo = "/audio/music-service-c/web/song/info"
	// pathCollResourceCheck 是「这条内容收藏了没有」。
	pathCollResourceCheck = "/medialist/gateway/coll/resource/check"
)

// 这几个取流参数里**固定**的那几个。
//
// `privilege` 文档写着「必须为 2」。`platform` 和 `mid` 文档写着「可为任意值」，
// 而上游发的是「web / 用户的 mid」（`common/utils/audio.ts` 的 getAudioUrl）。
const (
	songPrivilege = 2
	songPlatform  = "web"
)

// 错误哨兵。都拿 errors.Is 判。
var (
	// ErrPreviewOnly 是「只拿得到试听片段，不是整首」。
	//
	// 单独一个哨兵是因为它和「网络断了」「这一首要下架」要分开说：这一条
	// 有明确的下一步（开通会员），另两条重试多少次都一样。`Friendly` 靠它
	// 把话说到位上。
	ErrPreviewOnly = errors.New("bili: 只拿得到试听片段")
)

// ─────────────────────────────────────────────────────────────────────────────
// 元信息
// ─────────────────────────────────────────────────────────────────────────────

// SongInfo 是 B 站音乐库一条条目的元信息。
type SongInfo struct {
	// Sid 是这条的 auid（我们叫 sid，B 站那边叫 auid / songid）。
	Sid   string
	Title string
	Cover string
	// Author 是**作者名**，也就是 Biu 那边占 `ownerName` 的那个字段
	// （`store/play-list.ts:157` 的 getAudioData 取的是 `data.author`）。
	Author string
	// Uname 是 UP 主的昵称（`data.uname`）。大多数条目 Author 和 Uname 不同：
	// 一首歌的「作者」和投稿它的 UP 主不是同一个人。
	Uname string
	// Duration 是时长。B 站的单位是**秒**。
	Duration time.Duration
	// Bvid / Cid 是它关联的投稿（很多音乐库条目背后有一个投稿）。
	// 用不上这一条路取流，但「这条音乐是从哪个视频来的」只有这里有。
	Bvid string
	Cid  string
	// CollectFolders 是它已经在哪些收藏夹里。**要登录**（SESSDATA）才有值，
	// 没登录时 B 站给的是空数组——空是「没登录」而不是「没收藏过」。
	CollectFolders []int64
}

// songInfoData 是 `data` 段。字段照上游 `audio-song-info.ts` 的
// `AudioSongInfoData` 取用得着的几项，其余（`crtype` / `msid` / `limit` /
// `uid` … 「作用尚不明确」的那些）不收：抄进来等于把接口的形状再抄一遍
// （同一个道理见 `video.go` 的 `pagelistItem`）。
type songInfoData struct {
	ID         int64   `json:"id"`
	Uname      string  `json:"uname"`
	Author     string  `json:"author"`
	Title      string  `json:"title"`
	Cover      string  `json:"cover"`
	Duration   int     `json:"duration"` // 秒
	Bvid       string  `json:"bvid"`
	Cid        int64   `json:"cid"`
	CollectIDs []int64 `json:"collectIds"`
}

// SongInfo 问 B 站要一条音乐库条目的元信息。
//
// 放在**放歌的那一刻**问（见 `app` 那一层的 `withSongInfo`），不是给收藏夹
// 里的每一条都补一遍——理由和补 cid 那一条一样：用户多半只会听其中一首。
func (c *Client) SongInfo(ctx context.Context, sid string) (*SongInfo, error) {
	id, err := parseSid(sid)
	if err != nil {
		return nil, err
	}
	raw, err := c.Do(ctx, Call{
		Method: "GET",
		URL:    c.hostWWW + pathSongInfo,
		Query:  url.Values{"sid": {strconv.FormatInt(id, 10)}},
	})
	if err != nil {
		return nil, err
	}

	var d songInfoData
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("bili: 音乐库元信息的返回读不懂：%w", err)
	}
	// ⚠️ 拿到的是 `data: null` 时（code 是 0 但歌没了）反序列化出来是零值，
	// 而零值照样能被编成一条「标题空着、时长 0 秒」的元信息。那种东西填进
	// 播放条上就是一行空白，而真因是「这一首已经不在了」。
	if d.ID <= 0 {
		return nil, fmt.Errorf("%w：音频 %d 的元信息是空的（多半是已下架）", ErrNoStream, id)
	}

	out := &SongInfo{
		Sid: strconv.FormatInt(id, 10),
		// ⚠️ B 站的 duration 是**秒**。当毫秒读的话一首三分钟的歌唱成三秒。
		Duration: time.Duration(max(d.Duration, 0)) * time.Second,
		Bvid:     d.Bvid, Author: d.Author, Uname: d.Uname,
		Title: d.Title, Cover: d.Cover,
	}
	if d.Cid > 0 {
		out.Cid = strconv.FormatInt(d.Cid, 10)
	}
	if len(d.CollectIDs) > 0 {
		out.CollectFolders = append([]int64(nil), d.CollectIDs...)
	}
	return out, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 取流
// ─────────────────────────────────────────────────────────────────────────────

// songURLData 是取流返回的 `data` 段。只声明真的要用到的几项。
//
// ⚠️ **Type 是这一路唯一要紧的字段**：它是 B 站实际给回来的那一档，
// 而 -1 是试听片段（见本文件的文件头）。
//
// 🔴 **`sid` 不是可选项**，它是这一路的**身份**：`/audio/music-service-c/url`
// 是**按 sid 问的**，而返回里带一个 `sid`（文档第 2 节「音频 auid」）。这两个
// 号码对不上就意味着 B 站给回来的是**另一首**的流——版权曲换了地址（错误码
// 72010027「版权音乐重定向」）就是这种形态。身份必须从**返回**里读：用请求里
// 那个 sid 填出来的话，`SameTrack` 在这一路上恒为真，而它守的恰恰是
// 「换流之后不许静默换成另一首歌」。
//
// `size` / `title` / `cover` / `info` 上游也声明了，这里不收：这一层只为了拿
// 地址、那一档和身份，把一堆用不上的字段抄进来等于把接口的形状再抄一遍
// （同一个道理见 `video.go` 的 `pagelistItem`）。真要用的时候再声明。
type songURLData struct {
	Sid       int64         `json:"sid"` // 音频 auid；换流之后拿它比对
	Type      int           `json:"type"`
	Timeout   int           `json:"timeout"` // 有效秒数，一般 10800（三小时）
	Cdns      []string      `json:"cdns"`    // 主地址在前，备用的在后
	Qualities []songQuality `json:"qualities"`
}

// songQuality 是 `qualities[]` 里的一项——**B 站告诉我们哪几档要会员**。
//
// 退档退到哪一档是从这里算出来的，所以 `require` 不是可选项：
// 没有它就只能猜，而猜错的表现是用户听到一档他自己没点的音质。
type songQuality struct {
	Type    int `json:"type"`    // 同一套 0..3
	Require int `json:"require"` // 0 不需要会员，1 需要
}

// AudioStreamURL 问 B 站要一条音乐库条目的音频流。
//
// sid 是那个 auid，pref 是用户选的音质档（`AudioAuto` 是默认）。
//
// 返回的 `StreamRef` 里同样没有任何凭据，**不要**写进日志（同 `PlayURL`）。
//
// 🔴 **试听片段不会当成流给出去**：见本文件的文件头。点名的「无损」拿不到就
// 报错（不偷偷降级），别的档位退到不用会员的那一档再问一次。
func (c *Client) AudioStreamURL(ctx context.Context, sid string, pref AudioQuality) (*StreamRef, error) {
	id, err := parseSid(sid)
	if err != nil {
		return nil, err
	}

	want := MusicTierFor(pref)
	d, err := c.fetchSongURL(ctx, id, want)
	if err != nil {
		return nil, err
	}

	if MusicTier(d.Type) == MusicTierPreview {
		// 点名要无损而拿到的是片段：**不偷偷降级**，说法照 `refFrom` 那一句，
		// 用户为无损付了钱。
		if pref == AudioLossless {
			return nil, fmt.Errorf("%w: 这一首没有无损（要的是「%s」），换一档音质或用大会员账号", ErrNoStream, pref.Label())
		}
		// 别的档位按「退到不高于它的那一档里最好的」再问一次。退到哪一档不用猜：
		// B 站在 `qualities` 里带着每一档要不要会员（`require`）。
		if free, ok := freeTier(d.Qualities, want); ok {
			again, err := c.fetchSongURL(ctx, id, free)
			if err != nil {
				// 退档那一趟没成：说它。拿「要开会员」去盖住一次网络错，
				// 会让用户去办一件根本用不着的会员。
				return nil, err
			}
			d = again
		}
		if MusicTier(d.Type) == MusicTierPreview {
			// 退不动（付费曲，**每一档都要会员**）或者压根没有不用会员的档。
			return nil, fmt.Errorf("%w：这首要会员才有完整版（要过的 %s 拿回来的只是试听）",
				ErrPreviewOnly, want.Label())
		}
	}
	return c.refFromSongURL(d, strconv.FormatInt(id, 10)), nil
}

// fetchSongURL 问一次取流接口，quality 就是点名要的那一档。
func (c *Client) fetchSongURL(ctx context.Context, id int64, tier MusicTier) (*songURLData, error) {
	mid := c.SelfMid()
	if mid <= 0 {
		// 文档写着 mid 可为任意值。没登录时 cookie 里没有 DedeUserID，
		// 发 0 会撞上别的校验，所以发一个占位的。
		mid = 1
	}
	q := url.Values{
		"songid":    {strconv.FormatInt(id, 10)},
		"quality":   {strconv.Itoa(int(tier))},
		"privilege": {strconv.Itoa(songPrivilege)},
		"mid":       {strconv.FormatInt(mid, 10)},
		"platform":  {songPlatform},
	}
	raw, err := c.Do(ctx, Call{Method: "GET", URL: c.hostAPI + pathSongURL, Query: q})
	if err != nil {
		return nil, err
	}

	var d songURLData
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("bili: 音乐库取流的返回读不懂：%w", err)
	}
	// 一条没有地址的流不是流（B 站在稿件被删时会回 code 0 + 空 cdns）。
	if len(d.Cdns) == 0 {
		return nil, fmt.Errorf("%w: 音频 %d 没有可播的地址", ErrNoStream, id)
	}
	return &d, nil
}

// freeTier 在 qualities 里挑「不用会员」且**不高于** want 的那一档里最好的。
//
// 「不高于」是 `pickAudio` 末尾那条规矩：用户点了省流量，给更高的档就是
// 多给了流量，而那种错在界面上完全看不出来。
//
// qualities 空着（老接口或者形状变了）时给 false——**宁可说「要会员」也不要
// 瞎猜一档**：猜错的表现是用户听到一段他不想要的音质。
func freeTier(qs []songQuality, want MusicTier) (MusicTier, bool) {
	best, ok := MusicTier(0), false
	for _, q := range qs {
		t := MusicTier(q.Type)
		if q.Require != 0 || t < 0 || t > want {
			continue
		}
		if !ok || t > best {
			best, ok = t, true
		}
	}
	return best, ok
}

// refFromSongURL 把音乐库的取流响应变成一个 StreamRef。
//
// 和 dash 那一路的 `refFrom` 分开：两条路的档位编号不一样（`MusicTier` vs
// `Quality`），合在一起就得在每个字段上判「这一次是哪一路」，而那种判断
// 迟早会漏一处。
//
// requested 是**问的时候**那个 sid（拿它兜底，见下面那一句），而 `StreamRef.Sid`
// 填的是 **B 站回来的那个**。
//
// 🔴 **为什么不用请求里那个**：这一路的身份判据全靠 `Sid`（`SameTrack` /
// `sameItem` 都比它），而它守的是「换流之后不许静默换成另一首歌」。拿请求里
// 那个填出来的话，判据比的是「我请求的 == 我请求的」——**恒为真**，闸门整个是
// 死的，而且恰恰是它最该拦的那一种（版权曲换了地址，B 站给回另一首的流）会
// 一路放过去。
//
// 返回里没有 sid（0 / 负数）时退回请求里那个，并**说清这一条**：那样的回答我们
// 没法核对身份，闸门在这一次上是弱的。让它当场报错更糟——B 站少给一个字段不该
// 换来一首歌放不出来，而真出现那种形状时 `sid` 那一句注释已经写在这了。
func (c *Client) refFromSongURL(d *songURLData, requested string) *StreamRef {
	tier := MusicTier(d.Type)
	urls := append([]string(nil), d.Cdns...)
	sid := requested
	if d.Sid > 0 {
		sid = strconv.FormatInt(d.Sid, 10)
	}
	ref := &StreamRef{
		URLs:      urls,
		MusicTier: tier,
		Label:     tier.Label(),
		Sid:       sid,
		ExpiresAt: c.songExpiry(urls, d.Timeout),
	}
	if tier == MusicTierFLAC {
		ref.Codecs, ref.Container = "flac", "flac"
	} else {
		// 音乐库给的是完整单文件，后缀是 `.m4a`（`.flac` 在无损那一档）。
		// 和 dash 那一路一样，这两个字段**只作记录**：解码器按头几个字节
		// 嗅探（见 `StreamRef.Codecs` 的注释）。
		ref.Container = "m4a"
	}
	return ref
}

// songExpiry 算这条地址什么时候失效。
//
// 音乐库这一路的接口给的是 **`timeout`（有效秒数，一般 10800 = 三小时）**，
// 而投稿那路的地址上带一个 `deadline` 查询参数。两边都认：地址上带 deadline
// 就用它（更准），没有就用 now + timeout。
//
// 都读不到就当**立刻失效**——理由和 `deadlineOf` 一样：误判成「永不过期」
// 会让换流那三条路径一次都不跑，而那种症状（歌播到一半静默停住）比提前
// 换流难查得多。
//
// 走的是 `c.now` 而不是 `time.Now`：和 WBI 签名同一个时钟（`Options.Now`），
// 假时钟下有效期是能算准的。
func (c *Client) songExpiry(urls []string, timeout int) time.Time {
	if len(urls) > 0 {
		if d := deadlineOf(urls[0]); !d.IsZero() {
			return d
		}
	}
	if timeout <= 0 {
		return time.Time{}
	}
	return c.now().Add(time.Duration(timeout) * time.Second)
}

// ─────────────────────────────────────────────────────────────────────────────
// 收藏态
// ─────────────────────────────────────────────────────────────────────────────

// CollResourceCheck 报 rid / type 这一条收藏了没有。
//
// typ 用 `MediaVideo`（2）/ `MediaAudio`（12）那两个，别的地方都这么填。
//
// ⚠️ 这个接口的 `data` **不是一个对象**，而是一个 0/1 的数。所以
// `Client.Do` 给回来的 `json.RawMessage` 要自己解——当成对象解的话症状是
// 「解不出来」，而那句错说的是「返回读不懂」，没人想得到是形状的问题。
//
// 上游那一句注释写的是「**通常**为 0/1 等数值标识」
// （`service/medialist-gateway-coll-resource-check.ts`），「通常」两个字意味着
// 另一种形状是可能的。只认数字的话，真出现 `true` 时这一条收藏了的歌会
// **静悄悄**地答成没收藏——那比报一句「读不懂」糟得多。
func (c *Client) CollResourceCheck(ctx context.Context, rid int64, typ int) (bool, error) {
	if rid <= 0 {
		return false, fmt.Errorf("bili: 收藏态查询的内容 id 不对（%d）", rid)
	}
	raw, err := c.Do(ctx, Call{
		Method: "GET",
		URL:    c.hostAPI + pathCollResourceCheck,
		Query: url.Values{
			"rid":  {strconv.FormatInt(rid, 10)},
			"type": {strconv.Itoa(typ)},
		},
	})
	if err != nil {
		return false, err
	}

	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return false, fmt.Errorf("bili: 收藏态的返回读不懂（data 是 0/1）：%w", err)
	}
	switch x := v.(type) {
	case bool:
		return x, nil
	case float64:
		return x != 0, nil
	}
	return false, fmt.Errorf("bili: 收藏态的 data 认不出来（是 %T，不是 0/1）", v)
}

// parseSid 把一个 sid 字符串变成正整数。空串 / 非数字 / 0 / 负数都算不对。
//
// 参数给错的时候要**当场说、别发请求**：重试一万次还是这一句，而发出去的那一
// 次会在 B 站那边算成一次风控采样。
func parseSid(sid string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(sid), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("bili: 音频 id 不对（%q）", sid)
	}
	return id, nil
}
