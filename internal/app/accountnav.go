// 「我是谁、我的头像在哪」——顶栏右上角那一格要的那一份。
//
// # 为什么是 nav，而不是别的接口
//
// B 站把「当前登录用户是谁」放在 `GET /x/web-interface/nav`（`bili/wbi.go`）。
// 那份返回里已经有 `face`（头像地址）、`uname`（昵称）、`mid`、`isLogin`。
//
// 而这个接口**早就在了**——`fetchWBIKey` 取签名密钥时每两小时打它一次，登录态
// 判定也用它。把它扩一个字段，比新起一个「取用户资料」的接口少一个往返、少一份
// fixture、少一次风控额度。
//
// # 未登录时**一个请求都不发**
//
// 顶栏右上角那一格**每一帧**都会被 view 问一次（`internal/ui/navbar_avatar.go`
// 的 `avatarSync`）。而 `nav` 虽然没登录也回 `code:0`（B 站的设计，见
// `fetchWBIKey` 那段注释），顶栏是**整个应用每一帧都在重画**的那一处——
// 「没登录也去问」在别的页面上是一次多余的往返，在这儿是每秒几十次。
//
// 更要紧的是它会**写出 401 噪声**：凭据过期之后 B 站开始回 -101，那份噪声会
// 淹掉真正的报错，而真机联调时看到满屏 401 却看不出是哪一个功能在问，就是从
// 这条路来的。所以闸门在这里，不在调用方——**调用方漏判一次就是一次噪声**。
//
// # 只问「我」，不问别人
//
// 别人的头像走的是别的接口（关注、动态、UP 空间），那一族**不带 Cookie 也照样
// 能取**，所以它们不需要这把闸门。这一份不一样：它问的是「当前登录的是谁」，
// 没有登录态就没有答案。
package app

import (
	"context"
	"errors"
)

// AccountNav 是「B 站那边怎么看我这个账号」。
//
// ⚠️ **`LoggedIn` 和 `App.LoggedIn()` 不是同一件事**，这两个字段说的是两个
// 判据，缺一不可：
//
//   - `App.LoggedIn()`：钥匙串里**有** SESSDATA。那是「本地认为自己登录了」。
//   - 这里的 `LoggedIn`：B 站**认**这个会话（`nav` 的 `isLogin`）。那是
//     「远端还认」。
//
// 它们不一致就是**登录态失效**：本地有凭据、B 站说没有。这时候头像不能显示成
// 「未登录」——用户明明登录着，那样他会以为是应用把他退了；而也不能显示上一轮的
// 头像——那是上一个有效会话的东西。所以这一份和「问不出来」一起归到界面上那个
// **登录失效**的形态（见 `internal/ui/navbar_avatar.go`）。
type AccountNav struct {
	// LoggedIn 是 B 站那边认不认这个登录态。
	//
	// **本地没登录时这个方法压根不发请求**，于是这个字段恒为 false——那一支
	// 和「B 站说不认」在结果上长得一样，而界面分得清它们（见 ui 那边的
	// `App.LoggedIn()`），所以不构成谎报。
	LoggedIn bool
	// Face 是头像地址。服务层已经补好协议、拼好裁剪参数，空串表示 B 站没给。
	//
	// ⚠️ **空串分不出「没设头像」和「没登录」**：B 站对这两种都回空。界面别拿
	// 它判登录态——`LoggedIn` 才是那一位判据。
	Face string
}

// errAccountNavUnavailable 是「这个 App 没挂登录 / 没挂 B 站客户端」。
//
// 单独一个哨兵而不是直接回 `AccountNav{}`：那两种「空」在界面上长得一样
// （都画成未登录的样子），但**日志里要能分**——真机上报说「头像一直没出来」时，
// 「压根没挂登录」和「请求失败了」是完全不同的两件事。
var errAccountNavUnavailable = errors.New("app: 没挂登录，头像问不了")

// AccountNav 问一次「B 站怎么看我这个账号」。
//
// 闸门见文件头：**没登录一个请求都不发**，直接回零值。于是调用方拿到零值
// 分不出「没登录」和「挂漏了」，那是调用方自己的事（ui 那边的判据是
// `App.LoggedIn()`，见 `AccountNav` 的注释）。
func (a *App) AccountNav(ctx context.Context) (AccountNav, error) {
	acc := a.account
	if acc == nil {
		return AccountNav{}, errAccountNavUnavailable
	}
	// ⚠️ 这一句在 `cli` 判空**之前**，是有意的：没登录时那个客户端是什么、存不存在，
	// 都不该影响「不发请求」这条——不然哪天有人把闸门挪到下面去，未登录就会开始
	// 打 B 站，而症状是日志里多出来的 401，没有任何一条测试会红。
	if !acc.LoggedIn() {
		return AccountNav{}, nil
	}
	if acc.cli == nil {
		return AccountNav{}, errAccountNavUnavailable
	}

	n, err := acc.cli.Nav(ctx)
	if err != nil {
		return AccountNav{}, err
	}
	return AccountNav{LoggedIn: n.LoggedIn, Face: n.Face}, nil
}
