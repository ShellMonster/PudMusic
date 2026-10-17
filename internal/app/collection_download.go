// 下载这一摊和「界面上的下载按钮」之间的接线。
//
// # 这一层为什么存在
//
// 下载队列本身在 `internal/service/download`，加队列的两个入口
// （`Download` / `DownloadVideo`）在 `downloads.go`。缺的是**中间那一段**：
// 界面上的「下载音频」「批量下载」按下去要变成队列里的几行任务，而这一段要
// 补一样东西——**分 P id**。
//
// # cid 是这一层唯一要补的东西
//
// `download.Manager.Enqueue` 是**同步**的，任务的身份是 `<bvid>|<cid>`、落盘名
// 也带 cid，而 B 站的收藏夹内容列表**不给 cid**（见 `bili.Media`）。在线播放
// 那一侧是靠 `withCid` 补的（`online.go`），下载这一侧原本没人补，于是
// `App.Download` 直接拒绝——界面上的表现是「按了说下不了」。
//
// 补 cid 是一次**网络往返**，而界面线程上一次都不许有（铁律三）。所以这里分成
// 两条路，区别只在**谁补**：
//
//	Download / DownloadVideo   同步、不补。缺 cid 就如实拒绝。调用方在界面线程上。
//	DownloadItem(Video)        后台补。**立刻返回**，结果走回调。
//
// 选哪一条由**那一页手里有没有 cid**决定，不是由「哪个更方便」决定：搜索结果
// 的条目只有 bvid（`bili.SearchVideo` 不给 cid），所以它走同步那条，界面上
// 如实说「还没有分 P id」——而不是偷偷多发一次请求去问出一个「看起来能下」
// 的答案。
package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
)

// ItemOfMedia 把一条 B 站内容翻成队列认的那一项。
//
// 🔴 **全项目只有这一份**：界面层画列表时也要用（`ui.onlineItemOf`），两份
// 各写一遍的话，改了字段只改一处，症状是「收藏夹里的歌在列表里有、在队列里
// 打不开」。
//
// ⚠️ **Type 非空的那些必须真的放得出来**：`bili.Media.Playable` 判「可放」的
// 类型（2 / 12）在这里都要变成一条能取到流的项（见 `app.streamFor`）。少一条
// 取流路径就会让「可放」在界面上变成一句谎话——条目照常画、照常进队列，
// 按播放才报错。`ui.TestEveryPlayableCollectionEntryIsAcceptedByThePlayer`
// 盯着这件事。
func ItemOfMedia(m bili.Media) Item {
	it := Item{
		Source: "online",
		Title:  m.Title,
		Cover:  m.Cover,
		// 收藏夹接口已经给了 UP 主名，而这一栏之前一直空着——于是系统媒体
		// 控制中心上的「艺人」永远是空的（见 `Item.Artist` 的注释）。
		Artist: m.Owner,
		// Sid 一直带着：Type=12（音频）靠它，Type=2 的条目也指着它
		// （`list.BiliLink` 有 bvid 时优先用 bvid，所以不会串）。
		Sid: strconv.FormatInt(m.ID, 10),
	}
	switch m.Type {
	case bili.MediaVideo:
		it.Type, it.Bvid = "mv", m.Bvid
	case bili.MediaAudio:
		it.Type = "audio"
	}
	return it
}

// EnqueueDownload 实现 `collection.go` 里的 `Downloader`：**批量下载**那一路。
//
// **同步**（调用方在后台 goroutine 里，见 `ui.startCollectionDownload`），
// 所以这一层可以发网络请求——补 cid 就发生在这里。
//
// folder 现在只是记进日志：下载任务的界面上还没有「来自哪个收藏夹」这一栏，
// 而 `Task.ID` 是 `<bvid>|<cid>`、同一个容器里的同一首和别处的同一首是同一个
// 任务。哪天真要显示来源了，那时再把它放进队列的键里（见 `downloads.go`
// `DownloadVideo` 上面那段关于「要哪一路」的注释）。
func (a *App) EnqueueDownload(ctx context.Context, folder CollectionRef, medias []bili.Media) error {
	if a.downloadsSnapshot() == nil {
		return errDownloadsMissing
	}
	if len(medias) == 0 {
		return nil
	}
	log.Printf("biu: 收藏夹 %s 要下载 %d 条", folder.Key(), len(medias))

	var added, missed int
	var firstErr error
	for _, m := range medias {
		it, err := a.downloadableItem(ctx, m)
		if err == nil {
			_, err = a.download(it, download.AddRequest{})
		}
		if err != nil {
			missed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		added++
	}

	// 🔴 **返回的话必须是一句已经说全了的话**：这一条的错误直接进 Toast，而
	// Toast 前面不会再加任何前缀。少说一句就是「勾了 20 首、只下了 3 首、
	// 界面上写『下载任务已添加』」——那比报错难查得多。
	switch {
	case added == 0 && missed == 0:
		return nil
	case added == 0:
		return fmt.Errorf("一首也没能加进下载队列：%w", firstErr)
	case missed == 0:
		return nil
	}
	return fmt.Errorf("已加进 %d 首的下载队列，另有 %d 首没加进去：%w", added, missed, firstErr)
}

// downloadableItem 把一条内容变成「可以进下载队列」的那一项，缺 cid 就去问一次。
//
// ⚠️ **网络往返在这一步**，所以调用方**必须在后台 goroutine 里**（铁律三）。
// 界面线程上直接调 `download` 的那一路（`App.Download`）不经过这里。
//
// 「这一首下不了」的两种：合集 / 电影（`Playable` 判掉，它们没有音频流），
// 以及 B 站音乐库里的音频条目（有 sid 没有 bvid，而下载按稿件存文件）。
func (a *App) downloadableItem(ctx context.Context, m bili.Media) (Item, error) {
	if !m.Playable() {
		return Item{}, errors.New("合集和剧集那一类没有单独的音频流")
	}
	it := ItemOfMedia(m)
	if it.Bvid == "" {
		return Item{}, errors.New("B 站音乐库里的条目还没有下载通道")
	}
	return a.withCid(ctx, a.streamingSnapshot(), it)
}

// DownloadItem 把一首排进下载队列，缺 cid 时**在后台问一次分 P**。
// **立刻返回**，结果走 done（done 可能在另一个 goroutine 上被调）。
//
// 给没有 cid 的界面用——收藏夹那一页的行（`bili.Media` 不带 cid）和动态卡片
// （`major.archive` 不带 cid）走这一条。用户在这两边按下下载时，那一栏界面
// 线程上一次网络往返都不该有。
//
// 已经有 cid 的项也走这一条：多一次 goroutine 换一份统一的回调形状，值。
func (a *App) DownloadItem(it Item, done func(error)) {
	a.downloadLater(it, download.AddRequest{}, done)
}

// DownloadItemVideo 是 `DownloadItem` 的视频那一路：落盘的是交织完的单个 mp4。
//
// 画质那一档传 `VideoQ`，传 0 是让 B 站按它自己的默认给（理由见
// `queue_drawer.go` 的 startDownloadVideo）。
func (a *App) DownloadItemVideo(it Item, done func(error)) {
	a.downloadLater(it, download.AddRequest{Video: true}, done)
}

// downloadLater 是上面两条背后那一个。
func (a *App) downloadLater(it Item, req download.AddRequest, done func(error)) {
	// ⚠️ **同步就能看出来的错在这里就报掉**，不等 goroutine：它们一次网络都
	// 不用发，用户按下去的那一帧就该有话说。
	if it.IsLocal() {
		finishDownload(done, errors.New("这一首已经在你磁盘上了，没有东西可下载"))
		return
	}
	if err := playableOnline(it); err != nil {
		finishDownload(done, err)
		return
	}
	if a.downloadsSnapshot() == nil {
		finishDownload(done, errDownloadsMissing)
		return
	}
	s := a.streamingSnapshot()
	if s == nil || s.Client == nil {
		// cid 只能问客户端：没有它就补不上，异步也没用。
		finishDownload(done, errStreamingMissing)
		return
	}

	go func() {
		filled, err := a.withCid(context.Background(), s, it)
		if err == nil {
			_, err = a.download(filled, req)
		}
		finishDownload(done, err)
	}()
}

// finishDownload 是「结果回来了，说一句话」那一步。
//
// done 是 nil 的调用方（测试里那些不想听结果的）什么都不用做。
func finishDownload(done func(error), err error) {
	if done != nil {
		done(err)
	}
}
