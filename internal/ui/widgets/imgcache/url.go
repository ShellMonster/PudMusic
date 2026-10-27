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
	// ParamsSquare 是**方形小图**（侧栏收藏夹那一枚封面，32 见方）。
	//
	// ⚠️ **它和 `ParamsAvatar` 是同一串参数**，但**不是一件事**：头像按正方形
	// 裁是因为头像是方的，收藏夹的封面本来可能是方的也可能是别的形状——
	// B 站图床这一档把它裁成正方形，正好是侧栏那一枚要的样子。
	//
	// 另起一个名字是为了「这一格是给谁的」写在代码里，而不是让调用处去猜
	// 「头像那一档能不能拿来用」。
	ParamsSquare = "160w_160h_1c_1s.webp"
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

// absoluteURL 是**每一条下载路径上的兜底**：把协议相对的地址补成 https。
//
// ⚠️ **为什么缓存自己还要补一遍**：补协议的活儿本来归服务层（`bili` 那一层
// 的 `normalizeImageURL` / `avatarURL`）。可它是**逐接口手写**的，漏一处就是
// 那一个接口的图永远出不来——而且症状最阴：`http.NewRequest` 对
// `//i0.hdslb.com/…` 报 `unsupported protocol scheme ""`，那被 `isPermanent`
// 判成**永久失败**（一次都不重试），界面上只是那一格一直是占位。
//
// 所以这里再兜一道：补协议这一步**变成幂等的**（已经有 `http` 前缀的原样返回），
// 服务层补过也不会被改坏。它不替服务层收拾其余的不规范（比如相对路径 `/x.png`
// 仍会失败并如实报错）——那一种是真数据问题，应该看得出来。
func absoluteURL(url string) string {
	return FormatURLProtocol(url)
}
