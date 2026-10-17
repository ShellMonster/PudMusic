// 下载：把收藏夹里的一首变成下载目录里的一个文件。
//
// # 这一层只做接线
//
// 分块、并发、续传、206 检查、文件名净化全在 `service/download`。这一层
// 做三件它做不了的事：
//
//	1. **它是唯一知道 `bili.Client` 在哪的地方**（`Streaming.Client`）。
//	   download 只认一个 `Resolver` 函数，所以「问 B 站要一条流」这件事
//	   由这一层翻译成 `download.Source`——依赖单向，download 不 import bili。
//	2. **下载目录来自设置**（`Settings.DownloadPath`），不是某一层的常量。
//	3. **拿取流和拉字节的 http.Client 必须是同一个**，各建一套的话，
//	   「凭据注入有明确边界」这条就漏了——漏在哪一天没人知道。
//
// # 保存队列的两条路，都要
//
//	`SaveDownloads`  由界面层挂在 `OnWillQuit` 上（退出前那一次）
//	`App.Close`      测试和「窗口全关但进程还在」的收尾
//
// 只挂一条的话，另一条路上退出会丢掉队列：用户下次开应用看到空队列，
// 盘上却躺着一堆 `.part`。

package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
)

// errDownloadsMissing 是「下载没挂上」。见 `DownloadMounted`。
var errDownloadsMissing = errors.New("下载还没接上")

// Downloads 是下载这一摊的依赖。
type Downloads struct {
	// Media 是拉音频字节用的 http.Client。
	//
	// ⚠️ 它**必须**和在线播放用的是同一个：`app.CredentialTransport.Client(nil)`
	// ——带凭据边界但**不挂 jar**。B 站 CDN 跟 B 站 API 不是同一个凭据域，
	// 把 SESSDATA 发给一台存音频文件的机器没有任何理由。Referer 照给，
	// 不给直接 403（真机验过）。
	Media *http.Client

	// QueueFile 是队列落盘的位置（退出时写、重开时读）。
	QueueFile string

	// FallbackDir 是「用户还没在设置里选过下载目录」时下到哪儿。
	//
	// ⚠️ **不能省**：设置里那个 `downloadPath` 的默认值是**空串**
	// （`DefaultSettings("")`，设置文件读不出来时也是它），而
	// `filepath.Join("", "歌.m4a")` 出来的是一个**相对路径**——歌会落到
	// 进程的当前工作目录里。对一个双击图标启动的 macOS 应用来说那是
	// `/`，用户不会在那儿找歌，而在 Finder 里也看不见。
	FallbackDir string
}

// MountDownloads 挂上下载。**必须在开窗口之前调**（`download.New` 会读盘）。
//
// 取流零件（`Streaming`）**可以**晚挂：真正要取流是用户点了下载之后，
// 那时缺零件会给出一句人话而不是开窗就报错。
func (a *App) MountDownloads(d Downloads) error {
	if d.Media == nil {
		return errDownloadsMissing
	}
	m, err := download.New(download.Options{
		Client:  d.Media,
		Resolve: a.resolveDownload,
	}, d.QueueFile)
	if err != nil {
		return err
	}

	a.mu.Lock()
	a.downloads = m
	a.downloadQueueFile = d.QueueFile
	a.downloadFallbackDir = d.FallbackDir
	wake := a.wake
	a.mu.Unlock()

	// SetWake 可能在挂下载**之前**就调过了（界面层装早），所以这里补一次。
	if wake != nil {
		m.SetWake(wake)
	}
	return nil
}

// DownloadMounted 报告下载挂上没有。
func (a *App) DownloadMounted() bool { return a.downloadsSnapshot() != nil }

// Download 把一首加进下载队列，返回它的快照。
//
// 返回的错误只包含「同步就能看出来」的那几种（条目不完整、没挂下载、
// 同一首已经在队列里还下完了）。网络上的失败走记录页上那一行的红字。
func (a *App) Download(it Item) (download.Snapshot, error) {
	return a.download(it, download.AddRequest{})
}

// DownloadVideo 把一个视频条目加进下载队列。落盘的是**交织完的单个
// `.mp4`**，不是两条 dash 流。
//
// qn 是要的视频画质档。传 0 表示让 B 站按它自己的默认给。
//
// 队列里**同一首只能有一个任务**（`Task.ID` 是 `<bvid>|<cid>`，与音质
// 无关，见 `download.Task.ID` 的注释），所以「下载音频」和「下载视频」
// 撞在同一个 `<bvid>|<cid>` 上时，后者拿回的是前者的那一项。要让两条
// 各自成一项，得把「要哪一路」放进键里——那是记录页那一票的事。
func (a *App) DownloadVideo(it Item, qn bili.VideoQuality) (download.Snapshot, error) {
	return a.download(it, download.AddRequest{Video: true, VideoQ: int(qn)})
}

// download 是上面两者的共同那一半。
//
// **扩展名留空**：让 download 从取流地址里取。B 站给的 URL 后缀就是
// `.m4a` / `.flac`（任务里点名了这一条），按扩展名直接落盘，**不封装**。
// 给空是因为入队这一刻还没有地址——取流是几百毫秒的网络往返，不能在
// 界面线程上做。
//
// （视频是例外：落盘的既不是任何一条源流，也不是改个后缀就行的东西，
// 所以 `AddRequest.Video` 一说，`download` 那边直接给 `.mp4`。）
func (a *App) download(it Item, req download.AddRequest) (download.Snapshot, error) {
	if err := playableOnline(it); err != nil {
		return download.Snapshot{}, err
	}
	m := a.downloadsSnapshot()
	if m == nil {
		return download.Snapshot{}, errDownloadsMissing
	}
	req.Bvid = it.Bvid
	req.Cid = it.Cid
	req.Title = it.Title
	req.Cover = it.Cover
	req.Dir = a.downloadDir()
	return m.Enqueue(req)
}

// DownloadDir 是这一首现在会下到哪儿。
//
// 界面上那个「打开下载目录」要的就是它：按钮旁边写着的路径、按下去打开的目录、
// 歌真正落下来的地方，**必须是同一个**。写一个设置里的原始值再按下去的话，
// 设置还空着时按钮上写的是空串、按下去打开的是别处——用户在自己的下载目录
// 里找不到刚下的那几首，而界面上每一个字都是真的。
func (a *App) DownloadDir() string { return a.downloadDir() }

// downloadDir 是这一首该下到哪儿。
//
// 设置里的 `DownloadPath` 为空时退回 `FallbackDir`（系统下载目录）——
// 见 `Downloads.FallbackDir` 的注释，那条路走过一次歌就落到 `/` 里去了。
//
// 每次入队时读一次设置：用户在设置页换了下载目录之后点下载，下的就该是
// 新的那个。
func (a *App) downloadDir() string {
	if p := a.SettingsSnapshot().DownloadPath; p != "" {
		return p
	}
	a.mu.Lock()
	fallback := a.downloadFallbackDir
	a.mu.Unlock()
	return fallback
}

// DownloadSnapshots 返回队列的一份拷贝。记录页每帧读它。
func (a *App) DownloadSnapshots() []download.Snapshot {
	m := a.downloadsSnapshot()
	if m == nil {
		return nil
	}
	return m.Snapshots()
}

// DownloadSnapshot 返回一项的快照。记录页点某一行做操作之后要立刻
// 重画那一行，而重画读的还是队列 —— 别让它为了重画去遍历整个队列。
func (a *App) DownloadSnapshot(id string) (download.Snapshot, bool) {
	m := a.downloadsSnapshot()
	if m == nil {
		return download.Snapshot{}, false
	}
	return m.Get(id)
}

// PauseDownload 停下一项。
func (a *App) PauseDownload(id string) error {
	m := a.downloadsSnapshot()
	if m == nil {
		return errDownloadsMissing
	}
	return m.Pause(id)
}

// ResumeDownload 接着下一项（暂停和出错都归它）。
func (a *App) ResumeDownload(id string) error {
	m := a.downloadsSnapshot()
	if m == nil {
		return errDownloadsMissing
	}
	return m.Resume(id)
}

// RemoveDownload 从队列里拿掉一项并删掉它的半成品。已经下完的那首歌留着。
func (a *App) RemoveDownload(id string) error {
	m := a.downloadsSnapshot()
	if m == nil {
		return errDownloadsMissing
	}
	return m.Remove(id)
}

// ClearDownloads 清空队列并停掉所有在下的。磁盘上已下完的歌留着。
func (a *App) ClearDownloads() {
	if m := a.downloadsSnapshot(); m != nil {
		m.Clear()
	}
}

// SaveDownloads 把队列落盘。退出前调。
func (a *App) SaveDownloads() error {
	a.mu.Lock()
	m, path := a.downloads, a.downloadQueueFile
	a.mu.Unlock()
	if m == nil || path == "" {
		return nil
	}
	return m.Save(path)
}

// downloadsSnapshot 拿一份 Manager。没挂上是 nil。
func (a *App) downloadsSnapshot() *download.Manager {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.downloads
}

// resolveDownload 是 `download.Resolver` 的实现：拿这一首的身份去 B 站
// 要一条现在能用的流。
//
// ⚠️ **每次都重新取**：B 站的地址带 `deadline`，实测两小时就失效。一个
// 下到一半的歌很可能下超过两小时，拿着上次的地址续必然 403。
func (a *App) resolveDownload(ctx context.Context, want download.Identity) (*download.Source, error) {
	s := a.streamingSnapshot()
	if s == nil || s.Client == nil {
		return nil, errStreamingMissing
	}
	if want.Video {
		return a.resolveVideoDownload(ctx, s, want)
	}
	ref, err := s.Client.PlayURL(ctx, want.Bvid, want.Cid, s.Quality)
	if err != nil {
		return nil, err
	}
	// ⚠️ 这句今天**不可能触发**：`PlayURL` 把请求里的 bvid/cid 原样抄进
	// `StreamRef`（`bili/stream.go` 的 `refFrom`），所以它俩必然相等。
	// 留着是因为 `online.go` 的 `refetch` 守着同一道（那边也带一句同样
	// 的注释），而这一句守的是「下载拿到的东西是不是这一首」——哪天
	// `refFrom` 改成从响应里读身份，这里就是拦住「拼出坏文件」的那道门，
	// 而下载比播放更怕这个（播放错了用户还能听出来）。
	if ref.Bvid != want.Bvid || ref.Cid != want.Cid {
		return nil, fmt.Errorf("%w: 取流回来的不是这一首", bili.ErrNoStream)
	}
	return &download.Source{
		Bvid:    ref.Bvid,
		Cid:     ref.Cid,
		Quality: int(ref.Quality),
		Label:   ref.Label,
		URLs:    ref.URLs,
		// 地址上没带扩展名时的后备。容器名是取流结果里记着的，download
		// 不认识 B 站，所以这个换算在这一层做（见 `download.extFor`）。
		// ⚠️ `ref.Container` 只作记录、播放那一侧不用它选解码器
		// （`bili.StreamRef.Container` 的注释），但**落盘文件名**用它是对的：
		// 它记的就是这一段字节的容器，而文件后缀要说的正是这件事。
		Ext: extOfContainer(ref.Container),
	}, nil
}

// resolveVideoDownload 是「下载视频」那一条的取流：拿 dash 的**一对**流。
//
// 画质用的是 `want.VideoQ`——**入队那一刻用户点的那一档**，存在身份里。
// 那一档得跟着这一项走完整个下载过程：`.mp4` 是「视频流 + 音频流」交织
// 出来的，而两条流必须来自同一档（理由见 `download.Identity.VideoQ`）。
//
// 画质偏好**从哪来**是设置页的事，而设置页还没有视频画质那一项
// （记录页那一票会把它一起接上）。在那之前调用方传什么就是什么，
// 传 0 就是让 B 站给它的默认——在这里硬编一个「只下 360P 省流量」或者
// 「无脑 4K」都是替用户做决定。
//
// ⚠️ 每次都重新取，和音频那条同一个理由：B 站的地址带 `deadline`，
// 实测两小时就失效，而一个几百 MB 的 MV 很可能下超过两小时。
func (a *App) resolveVideoDownload(ctx context.Context, s *Streaming, want download.Identity) (*download.Source, error) {
	pair, err := s.Client.PlayDash(ctx, want.Bvid, want.Cid, bili.VideoQuality(want.VideoQ), s.Quality)
	if err != nil {
		return nil, err
	}
	// 两条流必须都是这一首。`PlayDash` 自己也会查一遍，而这里查的是
	// **另一件事**：交织那一步会不会把两个 cid 的流拼在一起。
	// （真网上取错的表现是「能播、播两分钟就没声了」——见 mux 的注释。）
	for _, ref := range []*bili.StreamRef{pair.Video, pair.Audio} {
		if ref.Bvid != want.Bvid || ref.Cid != want.Cid {
			return nil, fmt.Errorf("%w: 取流回来的不是这一首（要 %s/%s，拿到 %s/%s）",
				bili.ErrNoStream, want.Bvid, want.Cid, ref.Bvid, ref.Cid)
		}
	}
	return &download.Source{
		Bvid:          pair.Video.Bvid,
		Cid:           pair.Video.Cid,
		Quality:       int(pair.Video.Quality),
		Label:         pair.Video.Label,
		URLs:          pair.Video.URLs,
		CompanionURLs: pair.Audio.URLs,
		// ⚠️ `Ext` **故意不给**：它是「地址上没带后缀时的后备」，而视频
		// 落盘的既不是视频流也不是音频流，是交织出来的那个 `.mp4`
		// （见 `download.extFor`）。给一个 `.m4s` 会让落盘名错掉。
	}, nil
}

// extOfContainer 把容器名换成文件扩展名。
func extOfContainer(container string) string {
	if container == "flac" {
		return ".flac"
	}
	return ".m4a"
}
