package imgcache

import "strings"

// B 站图床的裁剪参数。三处封面的尺寸不一样，**不能当成一条规则批量替换**
// —— Biu 原来就是这么写的，参数各不相同。
const (
	// ParamsCover 是歌单/推荐流的大封面，对应 Biu 的 `672w_378h_1c.avif`。
	ParamsCover = "672w_378h_1c.webp"
	// ParamsAvatar 是 UP 主头像，对应 Biu 的 `160w_160h_1c_1s.webp`。
	ParamsAvatar = "160w_160h_1c_1s.webp"
	// ParamsMini 是迷你播放器的小封面，对应 Biu 的 `472w_264h_1c_!web-dynamic.webp`。
	ParamsMini = "472w_264h_1c_!web-dynamic.webp"
)

// CoverURL 按 B 站图床的裁剪参数拼出封面地址。
//
// ⚠️ 格式必须是 **.webp，不能是 .avif**。MyGo 的解码器只有 PNG / JPEG /
// GIF(首帧) / WebP / BMP 五种（`ui/bitmap.go` 的五个 blank import），Go 生态
// 里没有纯 Go 的 AVIF 解码器。B 站图床的裁剪参数与输出格式是解耦的，同一个
// `@672w_378h_1c` 换成 .webp 一样返回 HTTP 200（实测 32KB，AVIF 是 19KB、
// JPEG 48KB、PNG 307KB）。
//
// 拼法照抄 Biu 的 `src/components/image/index.tsx:18-20`：只在地址里有
// `/bfs/` 且还没带过裁剪参数时才拼，已经带 `@` 的一律原样返回。
func CoverURL(src, params string) string {
	s := FormatURLProtocol(src)
	if s == "" || params == "" || !strings.Contains(s, "/bfs/") || strings.Contains(s, "@") {
		return s
	}
	return s + "@" + params
}

// FormatURLProtocol 把 B 站接口返回的协议相对地址补成 https，与 Biu 的
// `src/common/utils/url.ts` 一致：以 http 开头的原样返回，其余补 "https:"。
func FormatURLProtocol(url string) string {
	if url == "" || strings.HasPrefix(url, "http") {
		return url
	}
	return "https:" + url
}
