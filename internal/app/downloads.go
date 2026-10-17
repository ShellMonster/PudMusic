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
	"log"
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
//
// ## 重复挂（导入完重新读队列）时的三步顺序
//
// 队列文件会话期间**只在退出时写一次**（`ui.MountDownloads` 挂的那个
// `OnWillQuit → SaveDownloads`，加上 `App.Close` 那一道），全项目没有第三处。
// 所以「换掉这个 Manager」这一步必须处理掉内存里那些**还没写进文件**的任务：
// 用户在这次会话里点了下载，那一首此刻只在内存里，文件里没有。
//
// ⚠️ **三步的顺序是有讲究的**（下面的代码就是这个顺序，不要动）：
//
//	① 把**旧**的内存队列**并进**文件（不是覆盖）
//	② 建新的 Manager——它 Restore 的是 ① 之后那份最全的文件
//	③ 关掉旧的（不落盘）
//
// 两条走错都会丢东西：
//
//   - **①换成「先建新的」**：`download.New` 一进来就 Restore，而那一刻文件还是
//     陈旧的，于是新 Manager 不知道本次会话加的那几首。② 之后再合并也来不及了——
//     它的内存里没有，而退出时落盘的是**它**那份。于是用户点的歌当场消失，并在
//     退出时从队列文件里被抹掉。
//   - **①换成「直接落盘」**：`old.Close(path)` 拿内存快照盖住文件，而快照里没有
//     刚被导入并进来的那几十条，于是**它们**被抹掉。
func (a *App) MountDownloads(d Downloads) error {
	if d.Media == nil {
		return errDownloadsMissing
	}

	// ① 旧的那份并进文件。没有旧的（第一次挂）时这一步什么都不做。
	old := a.downloadsSnapshot()
	if old != nil && d.QueueFile != "" {
		if err := download.MergeIntoQueue(d.QueueFile, old.Persistable()); err != nil {
			// 失败只记日志：这一刻旧的那份还在内存里继续用，而新 Manager 会从
			// 文件 Restore（少的那几条用户按一次「继续」就补上了）。
			log.Printf("app: 换挂下载前并队列失败，本次会话新加的歌可能没接上：%v", err)
		}
	}

	// ② 建新的：它 Restore 的是 ① 之后那份最全的文件。
	m, err := download.New(download.Options{
		Client:  d.Media,
		Resolve: a.resolveDownload,
	}, d.QueueFile)
	if err != nil {
		return err
	}

	// ③ 关旧的，不落盘（内存已经并进文件了，再落一次会和那份打架）。
	if old != nil {
		_ = old.Close("")
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
//
// ⚠️ **cid 在这一条路上仍然是同步必填的**（`online.go` 的在线播放已经改成「缺了
// 就去问一次」，两条路的时序不一样）：`download.Manager.Enqueue` 是**同步**的，而
// `Download` 跑在界面线程上——在这里问一次分 P 就是一次冻窗口的网络往返。下载队列
// 的判重（`<bvid>|<cid>`）和落盘名也都靠 cid。
//
// 所以搜索结果（只有 bvid）现在**放得了、还下不了**。补下载这一条要动 download 那一
// 层（问分 P + 展开成几个任务），不是这一处能顺手带上的。
func (a *App) Download(it Item) (download.Snapshot, error) {
	if err := playableOnline(it); err != nil {
		return download.Snapshot{}, err
	}
	if it.Cid == "" {
		return download.Snapshot{}, errors.New("这一首还不能下：还没取到分 P id（下载要按集存文件）")
	}
	m := a.downloadsSnapshot()
	if m == nil {
		return download.Snapshot{}, errDownloadsMissing
	}
	return m.Enqueue(download.AddRequest{
		Bvid:  it.Bvid,
		Cid:   it.Cid,
		Title: it.Title,
		Cover: it.Cover,
		Dir:   a.downloadDir(),
		// 扩展名留空：让 download 从取流地址里取。B 站给的 URL 后缀就是
		// `.m4a` / `.flac`（任务里点名了这一条），按扩展名直接落盘，
		// **不封装**。给空是因为入队这一刻还没有地址——取流是几百毫秒的
		// 网络往返，不能在界面线程上做。
	})
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

// extOfContainer 把容器名换成文件扩展名。
func extOfContainer(container string) string {
	if container == "flac" {
		return ".flac"
	}
	return ".m4a"
}
