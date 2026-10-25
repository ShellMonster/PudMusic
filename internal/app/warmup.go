// 冷启动的「预热」：那些**第一次用才要花时间**的东西（票 #478）。
//
// # 它解决的是什么
//
// B 站的取流接口（`/x/player/wbi/playurl`）要 **WBI 签名**，而签名的密钥
// 要单独取一次（`GET /x/web-interface/nav`，见 `internal/service/bili/wbi.go`）。
// 那份密钥**只缓存在内存里**（两小时），所以——
//
//	**每一次冷启动，第一次取流都要多付一次往返。**
//
// 而用户抱怨的正是「**首次打开**取流慢」：那多出来的一次往返，正好
// 落在「第一次点歌」的那一刻。
//
// # 修法
//
// 启动的时候**在后台**把密钥取回来。它和「开窗口」是并行的，于是
// 用户点歌的时候密钥已经在手边——第一次取流回到**一次**往返。
//
// # 为什么这是「预热」而不是「预取」
//
// 预取是「替用户提前备好下一首歌的地址」（见 `prefetch.go`），而这一件
// 是「把一把**所有签名请求都要用**的密钥先拿到手」。后者没有「替谁准备」
// 那一层，也没有「代次」和「过期」的问题（密钥两小时换一次，而
// `wbiKeyCache` 自己管那个）。
//
// # 为什么**不**预热 CDN
//
// CDN 那条路要先知道地址才知道主机名，而地址要先问 playurl 才有——
// 于是「预连接 CDN」在冷启动这一段里**没有对象**。而**恢复上次那一首**
// 时那条取流**在开窗口之前就发出去了**（`RestorePlayback` 排在
// `openWindow` 前面），所以冷启动那一段的 CDN 连接**本来就是热的**
// （它和「零件挂好」「建窗口」并行）。
//
// 这一条是查过之后记下来的结论，不是没查（票 #478 的验收项：
// 「首次打开特有那几项逐项查了」）。

package app

import (
	"context"
	"time"
)

// warmUpTimeout 是「预热」自己的上限。
//
// ⚠️ **它不该拖住任何东西**：预热是**后台**的，失败了就失败���—
// 真正要签名的时候 `wbiKey` 会自己再取一次（见 `internal/service/bili/wbi.go`
// 的 `wbiKey`：并发去取会去重，不会发两次）。
//
// 取 10 秒：它要走一次真实的 B 站往返，而传输层的响应头超时是 20 秒。
const warmUpTimeout = 10 * time.Second

// StartWarmUp 开始预热。**在挂好在线播放之后调**（它要那个客户端）。
//
// ⚠️ **它不挡任何东西**：这一句是「起一个后台 goroutine 就回来」，
// 调用方（`main.go`）接着往下走。
func (a *App) StartWarmUp() {
	s := a.streamingSnapshot()
	if s == nil || s.Client == nil {
		return
	}
	cli := s.Client
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), warmUpTimeout)
		defer cancel()
		if err := cli.WarmWBIKey(ctx); err != nil {
			// ⚠️ **失败不算错**：签名密钥取不到时，签名接口会自己再取一次
			//（`wbiKey` 的重取路径）。这里只留一句日志，**不许**让用户看见。
			startupLog("biu: 预热签名密钥没成功（不影响播放，签名接口会自己再取一次）：%v", err)
		}
	}()
}
