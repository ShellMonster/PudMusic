// 预取的字节那一半：备好了地址之后，顺手把**字节也下到盘上**。
//
// # 为什么要单独一段
//
// 「备地址」和「备字节」是两种成本：
//
//	备地址	一次带签名的 B 站往返（200~400ms），省的是「问」的时间
//	备字节	一条 CDN 连接 + 盘上写字节，省的是「等首字节 + 等 moov +
//			开解码器」——慢网上那段才是切歌空档的大头
//
// 字节走的是 `StreamSource` 已经会的那一套：`CachePath` 落进缓存目录、
// `OffsetPath` 记前沿、`ResumeFrom` 续传。预热源**登记进 `finishers`**——
// 和被收养的那一种共用「保护不被淘汰 / 收回 / 退出全关」那一套，不另发明。
//
// # 三条纪律
//
//   - **它不是播放器**：不解码、不出声、不碰 `a.player`。它只是「把字节
//     先搬到盘上」，真播那一趟还是 `runOnline` 从头走一遍。
//   - **它和正常播放共用同一份缓存文件**：`CachePath` 是同一个
//     (`track, tier`) 算出来的路径。真播时 `cancelFinishersForCache`
//     会把它收回来（`Close` 时把账本最后一笔写上），新源接着下——
//     「两条源同写一个文件」是绝对不许的，收回顺序和 `buildFromRef`
//     里那一条一模一样。
//   - **它的 `OnComplete` 和正常播放那一份是同一段逻辑**：下完了就
//     `markByteCacheComplete`（`Complete:true` + `SavePref`）。用户没切
//     歌它也会一路下到完——那正是「下一首自动切过去就秒开」的前提。
//
// # 失败退回哪一档
//
// 建不起来（没挂缓存目录、缓存键算不出来、`NewStreamSource` 报错）就
// 退回「只备了地址」——那也比没有强。源建起来之后**拉不动了**（断网、
// 地址过期）由它自己的退避/换流管，拉死了 `drain` 收尸——那是
// finisher 的常态，不用这里操心。
package app

import (
	"context"
	"sync/atomic"

	"github.com/ShellMonster/PudMusic/internal/service/audio"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// startPrefetchBytes 给「备好了地址的下一首」建一条预下载的字节源。
//
// 返回 nil 表示「这一趟不备字节」（没挂缓存 / 键算不出来 / 建源失败）——
// 地址那一半照样有效，只是没省下字节那一段。
//
// 🔴 **它和正常播放那一趟共用 `buildFromRef` 的缓存接线**：同一个
// `CachePath` / `OffsetPath` / `StartBytes` / `markByteCacheComplete`。
// 唯二的不同是**不解码**（没人读它，它只写盘）和**它登记进 finishers**
// （收回路径和被收养的源是同一条）。
func (a *App) startPrefetchBytes(ctx context.Context, s *Streaming, it Item, ref *bili.StreamRef) *audio.StreamSource {
	if !byteCacheKeyReady(it) {
		return nil
	}
	cacheTrack := byteCacheTrack(it)
	cacheTier := byteCacheTier(it, ref)
	if cacheTier == "" {
		return nil
	}
	c := a.byteCacheSnapshot()
	if c == nil {
		return nil
	}
	cachePath := c.BytesPath(cacheTrack, cacheTier)
	if cachePath == "" {
		return nil
	}
	// 🔴 **同一份已经在盘上 / 正在补就不重复建**：完整的那份直接播，
	// 正在补的那份让 finisher 把它下完（两条源同写一个文件是绝对不许的）。
	if _, ok := c.Complete(cacheTrack, cacheTier); ok {
		return nil
	}
	a.mu.Lock()
	_, busy := a.finishers[finisherKey(cacheTrack, cacheTier)]
	playing := a.online != nil && a.online.cacheTrack == cacheTrack && a.online.cacheTier == cacheTier
	a.mu.Unlock()
	if busy || playing {
		return nil
	}
	offsetPath := c.OffsetPath(cacheTrack, cacheTier)
	resumeFrom := resumeOffsetFor(offsetPath, cachePath)
	// 🔴 **和 `buildFromRef` 同一个顺序**：先收掉正在写这个文件的（理论上
	// 上面 `busy` 已经挡住了，但那是**锁外**的看一眼——这里再调一次是为
	// 了兜住「建源期间有人切去放它」的缝），再记「这一份正在下」。
	a.cancelFinishersForCache(cacheTrack, cacheTier)
	_ = c.StartBytes(cacheTrack, cacheTier)

	qualityAtBuild := s.Quality
	var firstAt atomic.Int64
	src, err := audio.NewStreamSource(audio.StreamSourceOptions{
		URLs:       ref.URLs,
		ExpiresAt:  ref.ExpiresAt,
		TempDir:    s.TempDir,
		CachePath:  cachePath,
		ResumeFrom: resumeFrom,
		OffsetPath: offsetPath,
		Client:     s.Media,
		Refetch:    func(ctx context.Context, _ int64) *audio.StreamReply { return a.refetch(ctx, s, it) },
		// 🔴 **换流之后「回来的还是不是这一首」的凭据记在 ref 上**：预热源
		// 换流换到一半被真播收回时，新源拿的也是这条 ref——它得和界面上
		// 「下一首」那个徽章对得上。
		OnRefresh:    func(r *audio.StreamReply) {},
		OnFirstBytes: func(n int64) { firstAt.CompareAndSwap(0, 1) },
		OnComplete: func(size int64) {
			if a.markByteCacheComplete(it, qualityAtBuild, ref, size) {
				a.logf("biu: 预取的那首下完了，缓存记上了：%q（%s）", it.Title, ref.Label)
			}
		},
	})
	if err != nil {
		return nil
	}
	// 🔴 **登记进 finishers**：保护（容量压力下不许 LRU 把它删了）、收回
	// （真播时 `cancelFinishersForCache`）、退出全关（`abortAllFinishers`）
	// 全是现成的那一套。`identity` 记入口无关的，cid 还没问出来那一路
	// 的兜底收回靠它。
	f := &sourceFinisher{
		identity: byteCacheIdentity(it),
		src:      src,
		track:    cacheTrack,
		tier:     cacheTier,
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		src.Close()
		return nil
	}
	if a.finishers == nil {
		a.finishers = map[string]*sourceFinisher{}
	}
	a.finishers[finisherKey(cacheTrack, cacheTier)] = f
	a.mu.Unlock()
	a.protectByteCache(cacheTrack, cacheTier)
	go f.drain(a)
	return src
}
