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
	// StatusDone 下完了，文件已经在 `Task.Dest` 上。
	StatusDone Status = "done"
	// StatusError 出错了。`Task.Err` 是人话。
	StatusError Status = "error"
)

// CanPause 报告这个状态现在能不能暂停。
func (s Status) CanPause() bool { return s == StatusRunning }

// CanResume 报告这个状态现在能不能继续。
func (s Status) CanResume() bool { return s == StatusPaused || s == StatusError }

// Running 报告这个任务此刻是不是占着网络。
func (s Status) Running() bool { return s == StatusRunning }

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
	// Length 是总字节数。**0 是「还没探到」，不是「零字节」**。
	Length int64 `json:"length"`
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
	// ID 是这一项在队列里的唯一身份：`<bvid>|<cid>|<quality>`。
	//
	// 用内容身份而不是随机 UUID（B 站上游用的是 UUID）：同一首歌重复点两次
	// 下载，用户要的是「就一个」，而不是队列里躺着两个下同一首歌的任务。
	// 音质进键是因为「同一首要 320K 和要无损」在 B 站那边确实是两段字节。
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

	// Chunks 是已经完成的分块下标。
	Chunks []int `json:"chunks,omitempty"`

	Status Status `json:"status"`
	// Err 是出错时的人话。成功后清空。
	Err string `json:"err,omitempty"`

	// AddedAt 是入队时刻（Unix 毫秒），记录页按它排序。
	AddedAt int64 `json:"addedAt"`
}

// totalChunks 是这个资源一共几块。
func (t *Task) totalChunks() int {
	if t.Identity.Length <= 0 {
		return 0
	}
	return int((t.Identity.Length + chunkSize - 1) / chunkSize)
}

// chunkRange 给第 i 块算出**闭区间**的 [start, end]。
//
// 闭区间是 HTTP Range 的写法（`bytes=0-10485759` 是前 10 MB），也是
// B 站上游 `splitChunks` 的写法（`download-core.ts:371`）。用开区间的话
// 每处都要多写一次 `-1`，而那种「少一个 1」的错误在 10 MB 那一档上
// 只会让最后一块多一个字节——很难发现。
func (t *Task) chunkRange(i int) (start, end int64, ok bool) {
	if i < 0 || i >= t.totalChunks() {
		return 0, 0, false
	}
	start = int64(i) * chunkSize
	end = start + chunkSize - 1
	if last := t.Identity.Length - 1; end > last {
		end = last
	}
	return start, end, true
}

// Done 返回已经下到盘上的字节数。
//
// 只算**整块**下完的：进行中的那一块现在算进去会让进度条来回跳
// （三个分块各完成一半时是 50%，下一个瞬间某个分块完成变成 100% 又回去），
// 而用户看到的是进度条自己往回走。
func (t *Task) Done() int64 {
	var n int64
	for _, i := range t.Chunks {
		start, end, ok := t.chunkRange(i)
		if !ok {
			// 下标越界：这份记录是旧版本或坏记录留下的（见 `Manager.Restore`）。
			// 宁可少算也不能算错——算多了会报出「下完了」而实际没下完。
			continue
		}
		n += end - start + 1
	}
	return n
}

// Complete 报这一项是不是已经下完。
func (t *Task) Complete() bool {
	n := t.totalChunks()
	if n == 0 {
		return false
	}
	seen := make(map[int]bool, len(t.Chunks))
	for _, i := range t.Chunks {
		seen[i] = true
	}
	for i := range n {
		if !seen[i] {
			return false
		}
	}
	return true
}

// markChunk 记下一块下完了。返回它是不是**刚刚**记上的（已经记过的返回
// false）——重复记会让 Done() 多算一遍，进度直接超过 100%。
func (t *Task) markChunk(i int) bool {
	for _, x := range t.Chunks {
		if x == i {
			return false
		}
	}
	t.Chunks = append(t.Chunks, i)
	return true
}

// resetChunks 清掉分块记账，表示「这块东西要重下了」。
//
// 用在两个地方：资源身份对不上（本地那些字节是**另一段内容**的，留着只会
// 拼出坏文件），以及落盘时发现文件已经被删了。
func (t *Task) resetChunks() {
	t.Chunks = nil
}

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
	// Percent 是 0~100 的整数进度。总长还不知道时是 -1，界面据此显示
	// 「准备中」而不是一个假的 0%。
	Percent int
}

// SnapshotOf 把一项拷成给界面看的快照。**必须在持锁时调**。
func SnapshotOf(t *Task) Snapshot {
	s := Snapshot{
		ID: t.ID, Title: t.Title, Cover: t.Cover, Label: t.Label,
		Dest: t.Dest, Status: t.Status, Err: t.Err, AddedAt: t.AddedAt,
		Total: t.Identity.Length, Percent: -1,
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
func TaskID(bvid, cid string) string {
	return bvid + "|" + cid
}

// DestFor 算出落盘的完整路径，并让开同名的文件。
//
// 冲突处理照 B 站上游（`ffmpeg-processor.ts:17`）：`名字 (1).m4a`、
// `名字 (2).m4a` 这样往后排。**不做**的话第二首同名歌会把第一首**直接
// 盖掉**，而记录页上两行都显示「已完成」，用户点开发现少了一首。
//
// 只在**还没有任何进度**时调（见 `Manager.start`）：一旦下过了就换名字，
// 等于把已经下了一半的字节挪到另一个文件名下面，而续传记的是分块下标
// 不是文件内偏移——换个文件那些下标全指错了位置。
func DestFor(dir, title, ext, id string) string {
	stem := FileName(title, "")
	if stem == "" {
		// 歌名实在净化不出东西。拿内容 id 当名字：至少能看出是哪一首，
		// 而编一个「未命名」在记录页上一眼看得出是名字丢了。
		stem = idSanitized(id)
	}
	if _, err := os.Stat(filepath.Join(dir, stem+ext)); err != nil {
		return filepath.Join(dir, stem+ext)
	}
	for i := 1; i < 10000; i++ {
		cand := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Stat(cand); err != nil {
			return cand
		}
	}
	return filepath.Join(dir, stem+ext)
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
