// 队列里的一项：这一首是谁、下到哪儿了、还差多少。
//
// 「下到哪儿了」记的是**已完成的分块下标**，不是「已经写了多少字节」。
// 原因很实际：并发 3 时三个分块同时在写，落盘的那段前缀是**断续**的
// （第 0 块还没下完，第 1、2 块已经齐了），而 `Range: bytes=N-` 这种续传
// 只认「前面 N 个字节是全的」。记一个前缀长度出来，重启之后只能从头再来。
// 记下标就准：哪些块齐了是确定的事实，不受写入顺序影响。

package download

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// chunkSize 是一个分块多大。10 MB。
//
// 照抄 B 站上游（`electron/ipc/download/download-core.ts:371` 的
// `chunkSize = 10 * 1024 * 1024`）。三个数里只有这一个是有道理的：分块太大
// 时并发 3 的意义被摊薄（一个 30 MB 的歌三个请求抢着下最后一块），
// 太小时每个请求的往返开销占比上来，而且分块信息本身要落盘。
const chunkSize = 10 << 20

// concurrency 是同一个任务同时发几个 Range 请求。
//
// 照抄任务要求的 3。**不是** B 站上游的 5（`download-core.ts:434`）：那 5 是
// 「每任务的分块并发」，而 B 站另外还有一个「任务并发 3」
// （`download-queue.ts:23`）——两个叠起来最坏是 15 个在飞的请求。
// 我们这一票只做队列不做交织，一次下载一个任务，所以并发放在这里，取 3。
const concurrency = 3

// progressInterval 是进度回调的最小间隔。
//
// 500 毫秒，照抄 B 站上游（`download-queue.ts:29` 的 `throttle(..., 500)`）。
// 不加节流的话一个任务每秒能触发几千次回调（每收到一个 TCP 包一次），
// 而界面每一次都要重画，画出来的是同一帧内容。
const progressInterval = 500 * time.Millisecond

// Status 是一个任务的状态。
//
// 用字符串而不是整数：这份状态要落盘成一个 JSON 文件，排查的时候人直接
// 看得到 `"status": "downloading"`，而不是去翻哪个 3 是哪个。
type Status string

const (
	// StatusQueued 排着，还没开始。
	StatusQueued Status = "queued"
	// StatusRunning 正在下。
	StatusRunning Status = "running"
	// StatusPaused 用户暂停了，或者应用退出时正在下的那些（见 `Restore`）。
	StatusPaused Status = "paused"
	// StatusMuxing 字节全下完了，正在交织成一个能播的文件（视频那一路）。
	//
	// ⚠️ 它是**在飞**的一种，不是「下完了」：两条几百 MB 的流要读一遍
	// 再写一遍，那十几秒里界面上不能显示成完成。而它**不是** running：
	// 那一刻网络已经空出来了，`Running()` 要说真话（这一条是它算在飞
	// 的理由，见下面）。
	//
	// 这个状态位是记录页那一票（#77）先占好的，那一版还没有代码会置它；
	// 视频下载那一票（#32/#39）接上交织之后它才真的有地方来。
	//
	// 重开应用之后不可能「正在交织」——那一轮的进程都没了，所以恢复时
	// 它一律变成暂停（见 `store.go` 的 `inflight`）。而「继续」是对的：
	// 两条流在盘上、分块也是齐的，按「继续」会一路走到交织那一步，
	// 一条字节都不重下，几秒钟就出结果。
	StatusMuxing Status = "muxing"
	// StatusDone 下完了，文件已经在 `Task.Dest` 上。
	StatusDone Status = "done"
	// StatusError 出错了。`Task.Err` 是人话。
	StatusError Status = "error"
)

// CanPause 报告这个状态现在能不能暂停。
//
// ⚠️ 交织不算能暂停。**不是**因为停不下来——`mux.Mux` 认 ctx，删掉这一项 /
// 退出应用都能把它掐掉（`runOne` 那条取消分支就是靠这个把状态收回暂停）。
// 真正的原因是**暂停对它没有意义**：两条流已经在盘上、分块记账是齐的，
// 而「暂停」答应用户的是「之后能从断的地方接着来」——交织没有断的地方，
// 要么做完，要么重来（重来时两条流一条字节都不用重下）。
func (s Status) CanPause() bool { return s == StatusRunning }

// CanResume 报告这个状态现在能不能继续。
//
// 交织中同样不算：字节已经全下完了，「继续」对它没有可继续的东西。真出错了
// 那一路会落在 `StatusError` 上，由重试从头走（那时交织会重来）。
func (s Status) CanResume() bool { return s == StatusPaused || s == StatusError }

// Running 报告这个任务此刻是不是占着资源。
//
// 交织算在飞，虽然它不再占着网络：它占着这一项，而且它**正在往盘上写**。判据
// 是「用户还没法对这一项动手」，不是「有没有 HTTP 请求在飞」。
func (s Status) Running() bool { return s == StatusRunning || s == StatusMuxing }

// inflight 报告这个状态是不是「在飞的」——退出或重开时它就该变成暂停，因为
// 那个活儿已经不存在了（进程都没了）。
func (s Status) inflight() bool { return s == StatusQueued || s.Running() }

// Identity 是「这一段字节属于谁」。
//
// 续传之前必须比一次。**不比的后果不是报错，是一个坏文件**：B 站的稿件
// 随时可能被替换、删除、被 UP 主重传，取流接口这时候会**静默**给一条看着
// 完全正常的流（`code` 仍然是 0）。而本地那个下了一半的文件里躺着的是
// **旧内容**的前 N 个字节——把它和**新内容**的后半段拼起来，长度对得上、
// 内容对不上，表现为「歌能打开、播到一半就断」。那种问题极难查：
// 长度对得上，所以没人会去怀疑拼接。
//
// 四个字段一起比，少一个都有洞：
//
//	Bvid / Cid  稿件被换掉、或者取到了别的分 P
//	Quality     会员过期、音质偏好变了，同一首拿到的是另一档
//	Length      上面都没变，但 B 站重新转码了（同样时长不同字节）
type Identity struct {
	Bvid    string `json:"bvid"`
	Cid     string `json:"cid"`
	Quality int    `json:"quality"`
	// Video 报「这一项要下的是视频」。
	//
	// 它在身份里而不在 `Task` 上，是**因为重开应用之后还得知道**：
	// 取流是 `resolve(ctx, want Identity)` —— 只看得到 `want`，
	// 而重开时那个 goroutine 还没起、队列里除了这份身份什么都没有。
	// 不在这一位上记着，重开之后取到的就还是一条音频流，于是任务在
	// 「下音频」和「交织」之间来回横跳。
	Video bool `json:"video,omitempty"`
	// VideoQ 是点名要的那一档视频画质（B 站的 `qn` 号）。
	//
	// 它在身份里，是因为**每一轮取流都得用同一个档**：这一条 `.mp4` 是
	// 「视频流 + 音频流」交织出来的，而两条流必须来自同一档、同一个 cid。
	// 续传时若换了档，视频流的长度会变，于是续出来的文件里一半是旧档的
	// 画面、一半是新档的——**能播**，播到一半画质就变了（而 `Identity` 只
	// 比 bvid/cid/音质/长度，长度变了会被拦下，可「同档不同编码」这种
	// 长度相同内容不同的换法拦不住）。
	//
	// 0 = 不点名，让 B 站给它的默认。
	VideoQ int `json:"videoQ,omitempty"`
	// Length 是**主那条流**的总字节数。**0 是「还没探到」，不是「零字节」**。
	//
	// 视频任务有两条流（视频 + 音频），第二那条的长度在 `Companion.Length`
	// ——**分两条记而不是求和**，是因为续传要逐条比：B 站重新转码的
	// 时候两条流的字节数**各自**都可能变，只比合计会漏掉「视频没变、
	// 音频变了」那一种。
	Length int64 `json:"length"`
}

// Companion 是第二那条流。
//
// # 为什么是「挂在旁边的一条」而不是「一个流数组」
//
// 绝大多数任务只有一条流，而那条流的所有记账（`Task.Chunks`、
// `Task.Identity.Length`）本来就是按「这一项要下的东西」记的。要把它
// 套进一个 `[]Stream`，每一处 `chunkRange`/`Done`/`Complete` 都得跟一个
// 下标走，落盘的格式也得换，而**换来的是视频这一种类型**。
//
// 所以两条流时第二条挂在这儿：一条流的任务（绝大多数）完全不受影响，
// `Companion` 是 nil，两条流的任务多算一次同样的账。
//
// # 它恒定是音频
//
// 因为视频下载永远是「一条视频 + 一条音频」交织成一个 MP4
// （`mux.Mux`）。B 站的音频不封装、下载下来直接是 `.m4a`，所以主那条流
// 永远是音频、伴流永远是 nil；视频这一路反过来。
type Companion struct {
	// Length 是这一条一共多少字节。**0 是「还没探到」**（同上）。
	Length int64 `json:"length"`
	// Chunks 是它已经完成的分块下标。语义和 `Task.Chunks` 一模一样，
	// 而且**必须分开记**：并发 3 时两条流各下各的，谁先下完谁先记，
	// 混在一起会算出「一半」的进度。
	Chunks []int `json:"chunks,omitempty"`
}

// completeOf 报这条流下完了没有。
func completeOf(length int64, chunks []int) bool {
	n := totalChunks(length)
	if n == 0 {
		return false
	}
	seen := make(map[int]bool, len(chunks))
	for _, i := range chunks {
		seen[i] = true
	}
	for i := range n {
		if !seen[i] {
			return false
		}
	}
	return true
}

// totalChunks 是这条流一共几块。
func totalChunks(length int64) int {
	if length <= 0 {
		return 0
	}
	return int((length + chunkSize - 1) / chunkSize)
}

// chunkRangeOf 给这条流的第 i 块算出**闭区间**的 [start, end]。
func chunkRangeOf(length int64, i int) (start, end int64, ok bool) {
	if i < 0 || i >= totalChunks(length) {
		return 0, 0, false
	}
	start = int64(i) * chunkSize
	end = start + chunkSize - 1
	if last := length - 1; end > last {
		end = last
	}
	return start, end, true
}

// doneOf 是这条流已经下到盘上的字节数。
func doneOf(length int64, chunks []int) int64 {
	var n int64
	for _, i := range chunks {
		start, end, ok := chunkRangeOf(length, i)
		if !ok {
			// 下标越界：这份记录是旧版本或坏记录留下的（见 `Manager.Restore`）。
			// 宁可少算也不能算错——算多了会报出「下完了」而实际没下完。
			continue
		}
		n += end - start + 1
	}
	return n
}

// markChunkOf 记下一块下完了，返回它是不是**刚刚**记上的（已经记过的返回
// false）——重复记会让 Done() 多算一遍，进度直接超过 100%。
func markChunkOf(chunks []int, i int) ([]int, bool) {
	for _, x := range chunks {
		if x == i {
			return chunks, false
		}
	}
	return append(chunks, i), true
}

// adoptSource 用源里的事实更新身份，**但保住这一项自己带着的那两样**。
//
// `prepare` 要靠它做资源身份的校验（见 `Identity` 那段注释），而它校验
// 完之后要把新身份写回去——**整份**写回去。于是这一项自己带着的
// `Video` / `VideoQ` 会被顺带抹掉，除非在这里把它们捞回来。
//
// 那两样是**用户点的**，不是源告诉我们的：源不知道这一项是要音频还是要
// 视频（它只是「给了两条地址」），也不知道用户点名的是哪一档。抹掉它们的
// 后果是「暂停一下再继续」和「退出重开」这两种最普通的操作之后取流问错了
// 问题（`want.Video` 变成 false → 只问了一条音频），而界面上没有任何提示。
func (t *Task) adoptSource(src *Source, length int64) {
	video, videoQ := t.Identity.Video, t.Identity.VideoQ
	t.Identity = src.IdentityOf(length)
	t.Identity.Video, t.Identity.VideoQ = video, videoQ
	// `Video` 在 Task 上也有一份（落盘名和三个半成品都看它）。那份是入队时
	// 定的，源改不了它——所以这里以它为准，把两份对齐。
	t.Identity.Video = t.Video || video
}

// SameResource 报两个身份说的是不是同一段字节。
//
// **长度只在两边都知道的时候比**：0 是「还没探到」，拿它去比任何数都会
// 得到「不一样」，那会让刚入队、还没探过长度的任务永远续不下去。
func (i Identity) SameResource(o Identity) bool {
	if i.Bvid != o.Bvid || i.Cid != o.Cid || i.Quality != o.Quality {
		return false
	}
	return i.Length == 0 || o.Length == 0 || i.Length == o.Length
}

// Task 是队列里的一项。
//
// 值类型 + 互斥锁：状态（进度、状态、错误）改动很频繁（每 500 ms 一次
// 起步），让它跟着引用到处传只会把锁的边界搅乱。`Snapshot` 拷一份出去，
// 界面拿着看。
type Task struct {
	// ID 是这一项在队列里的唯一身份：`<bvid>|<cid>`（见 `TaskID`）。
	//
	// 用内容身份而不是随机 UUID（B 站上游用的是 UUID）：同一首歌重复点两次
	// 下载，用户要的是「就一个」，而不是队列里躺着两个下同一首歌的任务。
	//
	// ⚠️ 音质**不在键里**，这是有原因的：入队那一刻还不知道 B 站会给哪
	// 一档（要问一次网络，而入队在界面线程上）。音质在续传那一步单独比
	// （`Identity`），对不上就重下——那才是真正要拦住「两段不同字节拼在
	// 一起」的地方。
	//
	// 代价：同一首歌换一档音质再点下载，会被当成同一项静默忽略。
	// 解开它要一次取流（网络）或者一个显式的「换音质重下」动作，都不是
	// 这一票的范围——留给记录页那一票（#31）在界面上把话说清楚。
	ID string `json:"id"`

	// Title 是歌名。落盘前要过一遍 `FileName`。
	Title string `json:"title"`
	Cover string `json:"cover,omitempty"`

	// Identity 是资源身份，见上面。
	Identity Identity `json:"identity"`
	// Label 是音质徽章文字（「无损 FLAC」/「320K」），纯给人看。
	Label string `json:"label,omitempty"`

	// Dest 是最终落盘的完整路径。
	//
	// 入队时先算一个**临时**的（那一刻还没有地址、不知道扩展名，见下面
	// 的 `Ext`），真正开始下之前会按地址上真的后缀重算一次。
	// **下过之后就不再改了**：改了等于把已经下了一半的字节挪到另一个文件名
	// 下面，而续传记的是分块下标，不是「文件里的偏移」——换个文件，
	// 那些下标就全指错了位置。
	Dest string `json:"dest"`

	// Ext 是落盘的扩展名（带点）。
	//
	// 入队时给的是个**临时值**（调用方没给就是 `.m4a`，B 站绝大多数是
	// AAC-in-MP4），真正开始下之前按取流地址上的后缀重算。之所以分两步：
	// 入队在界面线程上调，那一刻**还没有地址**——取流是一次几百毫秒的
	// 网络往返——而落盘名要等到取完才知道。
	Ext string `json:"ext,omitempty"`

	// Chunks 是**主那条流**已经完成的分块下标。
	Chunks []int `json:"chunks,omitempty"`

	// Video 报这一项是视频下载：主那条流是视频，`Companion` 是它的音频，
	// 两条都下完之后交织成一个 `.mp4`（见 `runMux`）。
	//
	// 音频任务它恒定是 false，而 `Companion` 是 nil。
	Video bool `json:"video,omitempty"`
	// Companion 是第二那条流。只有 `Video` 的任务才有它。
	Companion *Companion `json:"companion,omitempty"`

	Status Status `json:"status"`
	// Err 是出错时的人话。成功后清空。
	Err string `json:"err,omitempty"`

	// AddedAt 是入队时刻（Unix 毫秒），记录页按它排序。
	AddedAt int64 `json:"addedAt"`
}

// totalChunks 是主那条流一共几块。
func (t *Task) totalChunks() int { return totalChunks(t.Identity.Length) }

// chunkRange 给主那条流的第 i 块算出**闭区间**的 [start, end]。
//
// 闭区间是 HTTP Range 的写法（`bytes=0-10485759` 是前 10 MB），也是
// B 站上游 `splitChunks` 的写法（`download-core.ts:371`）。用开区间的话
// 每处都要多写一次 `-1`，而那种「少一个 1」的错误在 10 MB 那一档上
// 只会让最后一块多一个字节——很难发现。
func (t *Task) chunkRange(i int) (start, end int64, ok bool) {
	return chunkRangeOf(t.Identity.Length, i)
}

// streamLen 是指定那条流的长度。`companion` 为 true 时取第二那条。
func (t *Task) streamLen(companion bool) int64 {
	if companion {
		if t.Companion == nil {
			return 0
		}
		return t.Companion.Length
	}
	return t.Identity.Length
}

// streamChunks 是指定那条流已完成的分块下标。返回的是**头**不是拷贝：
// 只在锁下调，而且只在紧接着要改的那一处用。
func (t *Task) streamChunks(companion bool) []int {
	if companion {
		if t.Companion == nil {
			return nil
		}
		return t.Companion.Chunks
	}
	return t.Chunks
}

// Total 是这一项一共要下多少字节（两条流加起来）。
func (t *Task) Total() int64 {
	n := t.Identity.Length
	if t.Companion != nil {
		n += t.Companion.Length
	}
	return n
}

// Done 返回已经下到盘上的字节数（两条流加起来）。
//
// 只算**整块**下完的：进行中的那一块现在算进去会让进度条来回跳
// （三个分块各完成一半时是 50%，下一个瞬间某个分块完成变成 100% 又回去），
// 而用户看到的是进度条自己往回走。
func (t *Task) Done() int64 {
	n := doneOf(t.Identity.Length, t.Chunks)
	if t.Companion != nil {
		n += doneOf(t.Companion.Length, t.Companion.Chunks)
	}
	return n
}

// Complete 报这一项是不是已经下完。
//
// 视频任务**两条流都下完**才算完：少一条就交织不出一个能播的文件，
// 而那种文件（只有画面没声音）**照样能打开**。
func (t *Task) Complete() bool {
	if !completeOf(t.Identity.Length, t.Chunks) {
		return false
	}
	if t.Companion == nil {
		return true
	}
	return completeOf(t.Companion.Length, t.Companion.Chunks)
}

// markChunk 记下**主那条流**的一块下完了。
func (t *Task) markChunk(i int) bool {
	var fresh bool
	t.Chunks, fresh = markChunkOf(t.Chunks, i)
	return fresh
}

// resetChunks 清掉主那条流的分块记账，表示「这块东西要重下了」。
//
// 用在两个地方：资源身份对不上（本地那些字节是**另一段内容**的，留着只会
// 拼出坏文件），以及落盘时发现文件已经被删了。
func (t *Task) resetChunks() { t.Chunks = nil }

// Snapshot 是给界面看的一份拷贝。
//
// 界面**只**认这个类型：直接给它一个 `*Task` 就等于让界面能改到下载引擎
// 的内部状态，而那一层没有锁、没有字段不变量——这是那种「改着改着下载就
// 停了」的 bug 的来处。
type Snapshot struct {
	ID      string
	Title   string
	Cover   string
	Label   string
	Dest    string
	Status  Status
	Err     string
	Done    int64
	Total   int64
	AddedAt int64
	// Video 报这一项下的是视频。记录页那一排「全部 / 音频 / 视频」筛的就是它
	// （见 `ui.downloadsPage.viewFilter`）。
	//
	// ⚠️ **这里有一份，不许让界面从 `ID` 的 `|video` 后缀去猜**：那个后缀是队列
	// 文件的**落盘格式**（`TaskID` 为了兼容老队列，让音频那一支逐字保持
	// `bvid|cid` 不变）。拿它当业务判据的话，哪天有人为了整齐给两边都补上后缀，
	// 这一排筛选就会静默地把两类都认成「视频」——而界面上一个错都看不出来。
	// 也不许从 `Dest` 的扩展名猜：`.mp4` 现在只来自视频那一路，可那是一条
	// 巧合而不是约定，而巧合迟早会被人「顺手统一」掉。
	Video bool
	// Percent 是 0~100 的整数进度。总长还不知道时是 -1，界面据此显示
	// 「准备中」而不是一个假的 0%。
	Percent int
}

// SnapshotOf 把一项拷成给界面看的快照。**必须在持锁时调**。
func SnapshotOf(t *Task) Snapshot {
	s := Snapshot{
		ID: t.ID, Title: t.Title, Cover: t.Cover, Label: t.Label,
		Dest: t.Dest, Status: t.Status, Err: t.Err, AddedAt: t.AddedAt,
		Video: t.Video, Total: t.Total(), Percent: -1,
	}
	s.Done = t.Done()
	if s.Total > 0 {
		// 先算完再截到 100：`Done()` 只在分块全齐时才会等于总长，但万一
		// 记账多了一块，进度就该显示 100 而不是 103——界面上没有 103 那个
		// 格子。
		if s.Done > s.Total {
			s.Done = s.Total
		}
		s.Percent = int(s.Done * 100 / s.Total)
	}
	return s
}

// TaskID 拼出队列里这一项的身份。
//
// **音质不在键里**：入队的那一刻还不知道 B 站会给哪一档（要问一次网络，
// 而入队是在界面线程上调的）。音质在续传那一步会单独比（`Identity`），
// 对不上就重下——那才是真正要拦住「两段不同字节拼在一起」的地方。
// TaskID 是队列里一项的身份（也���落盘队列文件当键用）。
//
// 🔴 **音频和视频是两项**：同一首歌的音频（.m4a/.flac）和视频（交织出来的 .mp4）
// 是两个文件、两次下载，判重不能只看 `bvid|cid`。少了这一位的后果是「先点下载音频、
// 再点下载视频」拿到**音频那一个**任务——两次都返回成功，抽屉照样提示「已加进下载
// 队列」，而用户的下载目录里只有一个 .m4a。
//
// 音频那一支**保持旧格式**（就是 `bvid|cid`）：落盘的队列文件按这个字符串当键，
// 改了格式的话升级过的用户重启之后整个队列一个都认不出来（看起来像队列空了）。
func TaskID(bvid, cid string, video bool) string {
	if !video {
		return bvid + "|" + cid
	}
	return bvid + "|" + cid + "|video"
}

// DestFor 算出落盘的完整路径，并让开同名的文件。
//
// 冲突处理照 B 站上游（`ffmpeg-processor.ts:17`）：`名字 (1).m4a`、
// `名字 (2).m4a` 这样往后排。
//
// # 「占着」有三样，缺一样就撞车
//
//  1. 盘上的**最终文件**——别的歌已经下完落在这儿了
//  2. 盘上的 **`.part`**——另一首正在下，它还没改名，所以只 stat 最终名
//     会漏掉它
//  3. **队列里别的任务已经认领的** `Dest`——它可能一个字节都还没下，
//     盘上什么都没有，再过几秒就有了
//
// 少了第 3 样就是评审第一轮那个 critical：
//
//	① 首（30MB）出错退出，盘上留着 同名.m4a.part
//	② 第二首（40MB）同名入队 → DestFor 只 stat 同名.m4a（不存在）→ 也拿到
//	   同名.m4a → 下完落在 同名.m4a，41943040 字节
//	③ 回头重试第一首 → 它的 Dest 还是 同名.m4a → os.Rename 把它那 31457280
//	   字节盖上去
//	④ 第二首那条记录还写着「已完成 41943040/41943040」——而那个文件现在是
//	   别的歌。目录里只剩一个文件，界面上显示一切正常。
//
// 所以 `claimed` 由 `Manager.destFor` 从队列里现算：`DestFor` 自己看不到队列，
// 而撞车恰恰发生在队列里。
//
// # 255 的预算含扩展名、含同名避让的 ` (1)`、含 `.part`
//
// 之前这里是 `FileName(title, "")` 先把词干截到 255 再在外面拼，于是 84 个
// 汉字（252 字节）+ `.m4a` = 256 字节。而 `.part` 之后还要再加 5——
// 评审第二轮量到：251 个 ASCII 字符的歌名，落盘名 255 字符、`.part`
// 260 字符，`os.Create` 报 `file name too long`，**这一首一个字节都下不了**。
//
// （macOS 的真实上限是 255 个**字符**不是字节，评审实测过。但按字节算是
// 更严的那一边，所以守住字节预算就一定守得住字符上限。）
func DestFor(dir, title, ext, id string) string {
	return destFor(dir, title, ext, id, false, nil)
}

// destFor 是挑名字的纯逻辑。`claimed` 是队列里别的任务已经认领的路径，
// 可以是 nil（外面没有队列的场景，比如测试）。`video` 决定要不要多给
// 第二那条流的半成品留位置。
func destFor(dir, title, ext, id string, video bool, claimed map[string]bool) string {
	// 末尾那一段之外，还要给半成品留出位置——它和正式文件名同名同目录，
	// 用户在下载目录里一眼就能看出「这是在下的那一首」，所以不能另起一个
	// 截短的名字，只能让正式名让出这几个字节。
	//
	// 视频要多留一段：它有**两个**半成品（见 `companionPartPath`）。
	// 不留的话长歌名的伴流半成品会顶破文件名上限，而那种错在下载快
	// 结束的时候才暴露。
	reserve := len(partSuffix)
	if video {
		reserve += len(companionSuffix)
	}
	base := fileName(title, ext, reserve)
	if base == "" {
		// 歌名实在净化不出东西。拿内容 id 当名字：至少能看出是哪一首，
		// 而编一个「未命名」在记录页上一眼看得出是名字丢了。
		base = fileName(idSanitized(id), ext, len(partSuffix))
	}
	taken := func(p string) bool {
		if claimed != nil && claimed[p] {
			return true
		}
		// 别人的 `.part` 也算占着：它正在往那个位置里写。
		return exists(p) || exists(partPath(p)) || exists(companionPartPath(p))
	}
	if !taken(filepath.Join(dir, base)) {
		return filepath.Join(dir, base)
	}
	stem := strings.TrimSuffix(base, ext)
	for i := 1; i < maxNameTries; i++ {
		// 同名避让的 ` (1)` 也要算进预算：词干再长的话它得让出这几个字节。
		cand := fileName(stem, fmt.Sprintf(" (%d)%s", i, ext), reserve)
		if cand == "" {
			continue
		}
		if p := filepath.Join(dir, cand); !taken(p) {
			return p
		}
	}
	// 避让不出来（同名多到离谱）：拿内容 id 顶上，它在队列里是唯一的。
	return filepath.Join(dir, fileName(idSanitized(id), ext, len(partSuffix)))
}

// maxNameTries 是同名避让最多试几个。
//
// 上游是 9999（`ffmpeg-processor.ts:17`）。这里是 1000：挑名字是**在队列的锁
// 下面**做的（它要看别的任务占了谁），9999 次 `stat` 会把锁按到毫秒级，而
// 界面读队列快照也要这把锁。真的避让不出来时还有内容 id 兜底。
const maxNameTries = 1000

// exists 报这个路径上有没有东西。
func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// idSanitized 把内容 id 变成能当文件名的样子（`/` 和 `|` 都要换掉）。
func idSanitized(id string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == '|' || r < 0x20 {
			return '_'
		}
		return r
	}, id)
}
