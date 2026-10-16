// 保调变速（WSOLA）。
//
// # 为什么不能直接用 MyGo 的 SetSpeed
//
// `mygo/audio/audio.go` 的 `SetSpeed` 是**重采样**：它把每个源帧多吐几个或少吐
// 几个输出帧，音高跟着速度一起上去（文档里写得很直白：「as a record's speed
// does，2 倍速就是一个八度」）。1.5 倍速的人声是降了半个调的尖嗓，听两分钟
// 就受不了——这就是 Biu 用 `<audio>.playbackRate`（浏览器同样变调）之后我们
// 必须自己补的那一段。
//
// # 这一层做什么
//
// 它是一个套在解码器外面的 `mygoaudio.Decoder`：源帧照旧按时钟进来，它按倍率
// 决定每一跳从源里多读一点还是少读一点，而**窗形的相位和内容的先后关系原样
// 保留**——所以音高不动。
//
// 快慢的账在这里结清：输出一跳固定是合成跳那么多帧，源那一跳走合成跳×倍率。
// 2 倍速就是源走两跳、输出一跳，于是同样的源被压缩成一半长的输出；而输出
// 的采样率照旧是源的采样率，MyGo 那边按「输出帧 / 采样率」的速度消费它——
// 两秒的歌于是真的两秒放完。
//
// # 时间轴：**源帧就是歌里的时间**
//
// 这一层报给上层的帧号是**源帧**，不是吐出去的帧：
//
//	Len()           = 源的帧数        → Duration() 恒等于歌长，不随倍率变
//	SetPosition(f)  = 跳到源第 f 帧   → 拖进度条拖到哪儿就是歌里的哪儿
//
// 所以「变速中 seek 准确」是结构上成立的，不是靠事后校准的。
//
// 代价是 MyGo 的 `Player.Position()` 到这一层之前已经不可用了：它数的是**吐出去
// 的帧**，除以采样率之后永远是「已经播了多少秒」，跟倍率无关。歌里的位置得自己
// 算，见 `SourceAt`。
package audio

import (
	"io"
	"math"
	"sort"
	"sync"
	"sync/atomic"

	mygoaudio "github.com/egoist/mygo/audio"
)

// 倍率的两端和档位。
const (
	// MinRate 是最慢的一档：再慢下去人声已经开始拖音，字都跟不上嘴。
	MinRate = 0.5
	// MaxRate 是最快的一档：再快下去鼓点和歌词都糊成一片。
	MaxRate = 2.0
	// DefaultRate 是原速。
	DefaultRate = 1.0
)

// RateSteps 是倍率档位，一个不多一个不少地抄 Biu 的
// `PlayRate`（`biu/src/common/constants/audio.tsx:3`）。
//
// 为什么是**固定的七档**而不是一根随便拖的滑块：Biu 就是这七档，它那个浮层也
// 按这七档列的（`biu/src/components/music-rate/index.tsx:31`）。用户能选的就是
// 用户能预料到的——0.625 倍这种值在「音量」那一栏能拖，在「倍速」上拖不准。
var RateSteps = []float64{0.5, 0.75, 1, 1.25, 1.5, 1.75, 2}

// ClampRate 把倍率夹进 [MinRate, MaxRate]。
//
// 0、负数、NaN 一律落回 1：倍率算错时宁可原速，也不能是「不动」或者「倒着播」。
func ClampRate(v float64) float64 {
	if math.IsNaN(v) || v <= 0 {
		return DefaultRate
	}
	return min(max(v, MinRate), MaxRate)
}

// SnapRate 把倍率吸到最近的一档。
//
// 只在**读盘**时用：设置文件是用户能手改的，一个 `playbackRate: 1.13` 不该
// 原样进到算法里。运行中改的一律先经过 `App.SetRate`，那边本来就是按档位给的。
func SnapRate(v float64) float64 {
	v = ClampRate(v)
	best, bestDiff := DefaultRate, math.Abs(v-DefaultRate)
	for _, s := range RateSteps {
		if d := math.Abs(v - s); d < bestDiff {
			best, bestDiff = s, d
		}
	}
	return best
}

// 变速器的几个尺寸。
const (
	// readChunkFrames 是一次往源里要多少帧。
	readChunkFrames = 2048

	// windowSeconds 是一帧窗子的目标长度。46ms 不是挑的：
	//
	//	短了：60Hz 的周期是 17ms，窗里转不满三个周期，相似度会把八度错的那个
	//      当成最像的（差一个周期相关性还在 0.9 以上），低音听起来是嗡嗡的。
	//	长了：seek 之后要等更久才能重新铺满一段，拖进度条会「弹一下」。
	//
	// 按采样率算而不是写死帧数：44.1k 和 48k 的 46ms 帧长不一样，写死的话
	// 换一个采样率窗子就不是 46ms 了。
	windowSeconds = 0.046

	// searchDivisor 是相似度搜索**最大**半径占窗长的比例。
	//
	// 为什么要有上限，见 `searchRadius`：搜索半径必须小于 |分析跳 − 合成跳|，
	// 而这个差值在倍率靠近 1 时会缩到零。窗长 2048 时上限 256。
	searchDivisor = 8

	// corrDivisor 是拿来做比对的片段长度占窗长的比例。
	//
	// 一整跳（1024 帧 = 23ms）拿来比太贵，而 11ms 已经足够把「同一段」和
	//「差半个周期的另一段」分开。
	corrDivisor = 4

	// markCap 是位置对照表的容量（一跳一条）。
	//
	// 必须盖得住「MyGo 预解约一秒」那个提前量（`mygo/audio/source.go` 的
	// `frames := max(d.SampleRate(), 4096)` 那一圈缓冲）。44100Hz 下一跳 1024
	// 帧，一秒 43 跳，128 跳 ≈ 3 秒。留一倍多的余量：宁可多记几条。
	markCap = 128
)

// wsolaFrames 算一帧该多少帧，向上取整到 256 的倍数，夹在 512~8192 之间。
func wsolaFrames(rate int) int {
	if rate <= 0 {
		rate = 44100
	}
	n := int(math.Round(float64(rate) * windowSeconds))
	n = (n + 255) / 256 * 256
	return min(max(n, 512), 8192)
}

// hann 是周期汉宁窗。
//
// **周期的**（分母是 n 而不是 n−1）不是随手选的：它和 50% 重叠的重叠相加
// 恒等于 1，也就是 `w[i] + w[i+hop] == 1`。于是 1 倍速下这一层是**逐样本
// 透明**的（测试见 rate_test.go 的 TestUnityRateIsSampleForSample），
// 而非周期的窗会让每个采样点上有一个周期性的 ±1e−8 起伏——听不出来，但
// 「1 倍速就是原样」这句话就只能是大概而不是事实。
func hann(n int) []float32 {
	w := make([]float32, n)
	for i := range w {
		w[i] = float32(0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(n))))
	}
	return w
}

// mark 是「输出帧 ↔ 源帧」的一次对照。
//
// 它记的是**事实**（这一跳里源走了多远），不是模型（按倍率推算源该走多远）。
// 这两者的差别在长歌上是能听出来的：WSOLA 每跳都在名义位置附近找一个「接得
// 最像」的起点，对周期性的声音（人声、贝斯）常常偏一个周期，连着几百跳就
// 走成了一段随机的漂移。记事实就不会把这段漂移算进位置里。
type mark struct {
	out  int64   // 输出帧（这一跳的头）
	src  int64   // 那时候源在第几帧
	rate float64 // 这一跳里，输出前进一帧对应源前进多少帧
}

// varispeed 是套在解码器外面的那一层。
//
// 它是 `io.Closer`：转发给内层，这样 MyGo 关流的时候照样能把解码器和字节源
// 一起关掉（链条见 player.go 的 `owned`）。⚠️ 转发是必须的，方法不会被
// `mygoaudio.Decoder` 这个**接口**字段提升上去。
type varispeed struct {
	inner  mygoaudio.Decoder
	hop    int // 合成跳 = 窗长的一半
	search int // 相似度搜索半径的最大值
	corr   int // 比对片段的长度
	look   int // 往前要备多少帧
	ch     int

	// rate 是倍率，用一个原子量存。
	//
	// 不能用读写锁：SetRate 从界面线程调，而 Read 在解码 goroutine 上、还可能
	// 阻塞在网络上（在线流正在下那一段）。拿锁去改的话，用户点一下倍速，界面
	// 会卡到这一段下完为止。
	rateBits atomic.Uint64

	mu         sync.Mutex
	win        []float32
	first      []float32 // 这一帧的第一半（加窗后）
	second     []float32 // 这一帧的第二半（加窗后）
	tail       []float32 // 上一帧的第二半，也就是下一跳的前半
	out        []float32 // 已经铺好、还没被取走的输出
	outPos     int
	in         []float32
	inFrom     int64   // in[0] 是源的第几帧
	inEnd      int64   // in 的末尾后面是源的第几帧
	a          int64   // 这一帧的分析位置（源帧）
	anchor     int64   // 名义位置的原点（源帧）
	anchorRate float64 // anchor 是在哪一档倍率下定的
	hops       int64   // 从 anchor 起铺了几跳：名义位置是 anchor + hops×分析跳
	outFrames  int64   // 一共铺出去多少输出帧了
	bias       int64   // 见 SourceAt
	marks      []mark
	eof        bool
	err        error
	flush      bool // 源到头了，停之前还得把上一帧的第二半放完
	scratch    []float32
}

func newVarispeed(d mygoaudio.Decoder) *varispeed {
	ch := max(d.Channels(), 1)
	f := wsolaFrames(d.SampleRate())
	v := &varispeed{
		inner:  d,
		ch:     ch,
		hop:    f / 2,
		search: f / searchDivisor,
		corr:   f / corrDivisor,
		win:    hann(f),
		marks:  make([]mark, 0, markCap),
	}
	// 最远的读取是一次搜索的尾巴「a+分析跳+半径+比对长度」；铺满一帧要 a+2*hop。
	// 分析跳最大时是 2*hop（2 倍速），两者取大就是这里这个数。
	v.look = 2*v.hop + v.search + v.corr

	n := v.hop * ch
	v.first = make([]float32, n)
	v.second = make([]float32, n)
	v.tail = make([]float32, n)
	v.out = make([]float32, 0, n)
	v.scratch = make([]float32, readChunkFrames*ch)
	v.in = make([]float32, 0, (v.look+3*readChunkFrames)*ch)
	v.setRate(DefaultRate)
	return v
}

func (v *varispeed) rate() float64 { return math.Float64frombits(v.rateBits.Load()) }

// setRate 存下倍率。
//
// ⚠️ 存的是**吸过档的**值，不是原样：搜索半径要够不着「一模一样的那一段」
// （见 `searchRadius`），而那个间隔是 合成跳×|1−倍率|。倍率离 1 越近，间隔
// 越小，能搜的范围就越窄——0.999 倍时只剩一帧可搜，出来的东西勉强能听而
// 已。既然档位就那七个（`RateSteps`），这一层直接吸过去，别让调用方送来一个
// 「对得上但没人会选」的值。
func (v *varispeed) setRate(r float64) { v.rateBits.Store(math.Float64bits(SnapRate(r))) }

// ── mygoaudio.Decoder ─────────────────────────────────────────────────────

func (v *varispeed) SampleRate() int { return v.inner.SampleRate() }
func (v *varispeed) Channels() int   { return v.inner.Channels() }

// Len 是**源**的帧数。所以歌的时长不随倍率变，进度条的天花板不会因为开了 2 倍速
// 就变成两倍。
func (v *varispeed) Len() int64 { return v.inner.Len() }

// SetPosition 跳到源的第 frame 帧，并把这一层整个状态清干净重来。
//
// 坐标是**源帧**：上层（MyGo 的 Player.Seek）给的也是源帧——它拿的是歌的时长乘
// 采样率，而歌的时长恒等于源长。所以「变速中 seek 准确」到这里就完了，后面没有
// 任何一处需要按倍率换算。
func (v *varispeed) SetPosition(frame int64) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.reset(frame)
	return v.inner.SetPosition(frame)
}

// Close 转发给内层。见类型上那段注释：不转发的话这一层会把整条关资源的链条断开。
func (v *varispeed) Close() error {
	if c, ok := v.inner.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// Read 铺出尽量多的输出帧。
//
// 只会返回 (0, 错误) 或者 (n>0, nil/错误)：
//
//	(0, nil) 在 MyGo 那边等于「流到头了」（`mygo/audio/source.go` 的
//	`if err != nil || n == 0`），一首好好的歌会被当场截断。所以宁可多绕一圈
//	也不能这么返回。
func (v *varispeed) Read(buf []float32) (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	want := len(buf) - len(buf)%v.ch
	if want == 0 {
		// 调用方给了一个装不下整帧的缓冲。真按 want=0 返回的话就是
		// (0, nil)，MyGo 那边读成「流到头了」——整首歌被一个畸形缓冲截断。
		want = v.ch
	}
	n := 0
	for n < want {
		if v.outPos < len(v.out) {
			c := copy(buf[n:want], v.out[v.outPos:])
			n += c
			v.outPos += c
			continue
		}
		if !v.step() {
			break
		}
	}
	if n == want {
		return n, nil
	}
	if v.err == nil {
		v.err = io.EOF
	}
	return n, v.err
}

// SourceAt 报「输出的第 voiceFrames 帧，对应源的第几帧」。
//
// # 为什么需要它
//
// MyGo 的 `Player.Position()` 数的是这一层**吐出去**的帧，除以采样率之后永远
// 等于「已经播了多少秒」——它是个墙钟，倍率再大也不变。而进度条要的是歌里的
// 位置，那得是 `∫倍率 dt`。
//
// # 为什么是对照表而不是 `∫倍率 dt`
//
// 对照表记的是每一跳里**源真的走了多远**。WSOLA 每跳都会在名义位置附近找一个
// 接得最像的起点，对周期性声音常常偏一整个周期；一百跳之后，源的位置和
// 「按倍率线性推算」已经差出好几十毫秒，长歌上差到几百毫秒。按倍率积分会把这
// 段漂移原样算进位置里，而漂移本身是听得不出来的——进度条却在往一个「歌里根本
// 不存在的时刻」跑。
//
// # bias 为什么在那儿
//
// 对照表用的是「一共铺出去多少输出帧」这个计数器，它在 seek 时**不清零**；
// 上层（MyGo 的 voice.idx）却在 seek 时被重置到 seek 到的那个**源**帧。两者差着
// 一个常数，就是 bias。少了它，拖完进度条之后位置会莫名其妙地跳回歌头。
func (v *varispeed) SourceAt(voiceFrames int64) int64 {
	v.mu.Lock()
	defer v.mu.Unlock()

	p := voiceFrames + v.bias
	if len(v.marks) == 0 {
		// 一跳都还没铺：还在起点上。
		return v.a
	}
	i := sort.Search(len(v.marks), func(i int) bool { return v.marks[i].out > p }) - 1
	if i < 0 {
		// 比最早一条还早（刚开始播的那一下）：拿最早那条往回推。
		i = 0
	}
	m := v.marks[i]
	return m.src + int64(math.Round(float64(p-m.out)*m.rate))
}

// ── 铺帧 ──────────────────────────────────────────────────────────────────

// step 往 v.out 里铺一跳。铺不出来（源到头了、尾巴也放完了）就返回 false。
func (v *varispeed) step() bool {
	v.fill()

	if v.avail() >= int64(2*v.hop) {
		v.emit()
		return true
	}
	if v.avail() >= int64(v.hop) {
		// 只够铺一半：另一半按 0 算。窗子在那里本来就收到 0，所以这一跳是
		// 歌尾的一次淡出而不是咔哒。
		v.emit()
		v.flush = true
		return true
	}
	// 源里没有下一段了。把上一帧的第二半放完：那一段窗子正好从满收到 0，
	// 于是这一下就是歌尾的淡出尾。
	if v.flush {
		v.flush = false
		v.pushMark(mark{out: v.outFrames, src: v.a})
		v.out = append(v.out[:0], v.tail...)
		v.outPos = 0
		v.outFrames += int64(v.hop)
		return true
	}
	return false
}

// emit 铺一跳：加窗、重叠相加、再决定下一跳从源哪儿取。
func (v *varispeed) emit() {
	a, next := v.a, v.aNext()

	// 铺不满一帧时，缺的那一半就是 0——窗子在那儿也正好收到 0。
	seg := v.in[int(a-v.inFrom)*v.ch:]
	n, half := len(seg), v.hop*v.ch
	for i := range v.first {
		var x, y float32
		if i < n {
			x = seg[i]
		}
		if half+i < n {
			y = seg[half+i]
		}
		v.first[i] = v.win[i/v.ch] * x
		v.second[i] = v.win[v.hop+i/v.ch] * y
	}

	// 重叠相加：上一帧的第二半 + 这一帧的第一半，就是这一跳。
	v.out = append(v.out[:0], v.tail...)
	for i, s := range v.first {
		v.out[i] += s
	}
	// 这一帧的第二半就是下一跳的前半。交换指针就省掉一次拷贝。
	v.tail, v.second = v.second, v.tail

	v.pushMark(mark{out: v.outFrames, src: a, rate: float64(next-a) / float64(v.hop)})
	v.a = next
	v.hops++
	v.outFrames += int64(v.hop)
	v.outPos = 0
	v.trim()
}

// aNext 决定下一跳从源的哪儿取。
//
// 名义位置是**绝对的**：`anchor + hops × 分析跳`，不是「上一跳取到的位置 +
// 分析跳」。分析跳 = 合成跳 × 倍率：2 倍速时源走两跳（跳着读），0.5 倍速时
// 源只走半跳（重复着读）。
//
// 这里是**下一个**帧的位置，而 `hops` 数的是已经铺出去的帧数，所以要 +1：
// 差这一跳，「1 倍速」会在第一跳就把起点挪走一个搜索窗，之后再也回不来。
//
// # 为什么名义位置必须是绝对的
//
// 这一层要在名义位置附近找一个和**上一帧的第二半**最像的起点——那一段正是上
// 一跳正在淡出的内容，新的一跳从一段「长得像它」的地方起头，重叠相加时两个
// 波形首尾对得上，相位不跳。异响（金属味、颤音）就是从这一步漏出来的。
//
// 如果名义位置写成「上一跳的位置 + 分析跳」，搜索出来的偏移就会**逐跳累加**。
// 而信号只要有点周期性（一段人声、一个贝斯音），候选里就有好几个同样像的位
// 置（差一整个周期的那几处），挑哪一个纯看浮点数末位——于是每一跳都像掷一次
// 骰子，跑上几千跳就是一段随机的漂移。实测最坏的一档：2 倍速听一首 440Hz 的
// 单音，实际只走了 1.64 倍速（差 18%），歌放完了进度条还差一小截。
//
// 名义位置绝对了之后，每一跳的实际位置始终落在 `anchor + hops×分析跳 ± 半径`
// 里，越不过去：漂移被**夹住**了，而不是被放出去。
//
// ⚠️ **比的是余弦相似度，不是点积**。除以候选段自己的能量不是可有可无的：
// 点积最大的那一段是「能量最大的那一段」，于是一段渐强的音乐（开头渐入、
// 鼓进来）会把搜索点一路往后拽。1 倍速时该选的就是「和模板一模一样的那一段」
// （余弦 = 1），不归一化的话它未必赢——那一层就**不再逐样本透明**了，
// 「1 倍速就是原样」这句话随之变成大概。
//
// ⚠️ 半径必须够不着「一模一样的那一段」（见 `searchRadius`）：够得着就等于
// 选中它，分析跳退回合成跳，2 倍速听成 1 倍速。
func (v *varispeed) aNext() int64 {
	ha := v.analysisHop()
	if r := v.rate(); r != v.anchorRate {
		// 换倍率的那一刻**重新定原点**。
		//
		// 前面那些跳是按旧倍率走的，名义位置得从**当前实际位置**接上。不重定
		// 的话新名义位置会落到 a 的后面——2 倍速切 0.5 倍速时能差出几百跳，
		// 于是 a 被一路拽着往回走：用户听到的是刚才那一段被反复重放，而输入
		// 缓冲越涨越大，最后撑爆。
		//
		// 重定之后漂移的上界也重新从这一刻起算（±半径），不接着旧倍率那段算。
		v.anchor = v.a - int64(v.hops)*ha
		v.anchorRate = r
	}
	// 夹住下沿：分析位置只许从缓冲里已经有的地方往后走。少了这一句，
	// 任何一处「名义位置算到了缓冲之前」都会变成 `emit` 里 `v.in[a-inFrom:]`
	// 的越界 panic——那是在解码 goroutine 上，整个进程跟着没。
	nom := max(v.anchor+int64(v.hops+1)*ha, v.inFrom)
	rad := v.searchRadius()

	// 不够比对的：没得选，名义位置就是最好的选择。
	if v.avail() < int64(v.hop+v.corr) {
		return nom
	}
	lo := max(nom-rad, v.inFrom)
	hi := min(nom+rad, v.inEnd-int64(v.corr))
	if lo > hi {
		return nom
	}

	base := int(v.a-v.inFrom)*v.ch + v.hop*v.ch
	tmpl := v.in[base : base+v.corr*v.ch]

	best, bestScore := lo, math.Inf(-1)
	off := int(lo-v.inFrom) * v.ch
	for c := lo; c <= hi; c++ {
		seg := v.in[off : off+v.corr*v.ch]
		var dot, energy float64
		for i, x := range tmpl {
			y := float64(seg[i])
			dot += float64(x) * y
			energy += y * y
		}
		// 模板的能量对每个候选都一样，除不除都一样；除候选这一段就够把
		// 「和模板一模一样」变成唯一的满分 1。
		score := dot / (math.Sqrt(energy) + 1e-12)
		if score > bestScore {
			best, bestScore = c, score
		}
		off += v.ch
	}
	return best
}

// analysisHop 是这一跳源该走多远（帧）。
//
// ⚠️ 是**乘**不是除。2 倍速要「一秒听完两秒的歌」，源就得跑得比输出快两倍：
// 输出一跳 Hs 帧，源要走 2·Hs。
//
// 方向搞反了不会编译报错，也不会报错运行——它只是**把 2 倍速听成 0.5 倍速**：
// 源跑得比输出慢，歌就放长了。而音高仍然是对的，于是「音高没变」这条验收照样
// 通过，只有「进度条和耳朵对不上」能看出来（见 `TestTheClockFollowsTheRate`）。
//
// 倍率在这一跳中间变了就按新的算：用户改倍率就是让「后面读得快一点」。
func (v *varispeed) analysisHop() int64 {
	return max(int64(math.Round(float64(v.hop)*v.rate())), 1)
}

// searchRadius 是这一跳的搜索半径。
//
// ⚠️ 原速和变速要的是**相反**的东西：
//
//	原速（r=1）:要把「和模板一模一样的那一段」找回来——那才是逐样本透明的
//	           地基。半径给满。
//	变速（r≠1）:那一段**必须落在搜索窗之外**。它就在 a+合成跳 上，而名义位置
//	           是 a+合成跳×r；落在窗里就等于选中它，于是分析跳退回合成跳——
//	           源和输出一样快，2 倍速听成 1 倍速。
//
// 所以变速时把半径压到「刚好够不着它」为止：合成跳 × |1−r| × 0.8。
// Biu 那七档里只有 0.75 和 1.25 会被压（压到 204 帧 ≈ 4.6ms），其余的
// （|1−r| ≥ 0.5 → 409 帧以上）都还是满半径 256。
func (v *varispeed) searchRadius() int64 {
	r := v.rate()
	if r == DefaultRate {
		return int64(v.search)
	}
	limit := 0.8 * float64(v.hop) * math.Abs(1-r)
	return min(int64(v.search), max(int64(limit), 1))
}

// avail 是从当前分析位置起还有多少帧可用。
func (v *varispeed) avail() int64 { return v.inEnd - v.a }

// fill 往输入缓冲里补，补到够铺帧为止或者源到头。
func (v *varispeed) fill() {
	if v.eof {
		return
	}
	for v.avail() < int64(v.look) {
		n, err := v.inner.Read(v.scratch)
		n -= n % v.ch
		if n > 0 {
			v.in = append(v.in, v.scratch[:n]...)
			v.inEnd += int64(n / v.ch)
		}
		if err != nil || n == 0 {
			// 到头了。n==0 且 err==nil 是「源卡住了还不肯结束」，也当到头处理，
			// 不然这里会空转到天荒地老。
			v.eof = true
			v.err = err
			if v.err == nil {
				v.err = io.EOF
			}
			return
		}
	}
}

// trim 丢掉分析位置之前的输入。
//
// ⚠️ **留一个搜索半径的余量**：换倍率的那一下，名义位置会往前跳（1 倍速切
// 2 倍速时名义位置落到实际位置后面半跳多），搜索窗的下沿就可能在 a 前面。
// 丢掉的那一段再也读不回来，`emit` 里 `v.in[a-inFrom:]` 会当场越界。
func (v *varispeed) trim() {
	keep := max(v.a-int64(v.search), v.inFrom)
	drop := int(keep - v.inFrom)
	if drop <= 0 {
		return
	}
	if n := drop * v.ch; n < len(v.in) {
		// 复制回开头而不是重新切一份：切出去的那截会让 append 一直往前长，
		// 一个三分钟的在线流能攒出几百 MB 的垃圾。
		v.in = append(v.in[:0], v.in[n:]...)
	} else {
		clear(v.in)
		v.in = v.in[:0]
	}
	v.inFrom = keep
}

func (v *varispeed) pushMark(m mark) {
	if len(v.marks) == cap(v.marks) {
		copy(v.marks, v.marks[1:])
		v.marks = v.marks[:cap(v.marks)-1]
	}
	v.marks = append(v.marks, m)
}

// reset 把这一层清到「正要读源第 frame 帧」的状态。
func (v *varispeed) reset(frame int64) {
	clear(v.in)
	v.in = v.in[:0]
	clear(v.first)
	clear(v.second)
	clear(v.tail)
	v.out, v.outPos = v.out[:0], 0
	v.inFrom, v.inEnd, v.a = frame, frame, frame
	v.anchor, v.hops = frame, 0
	v.anchorRate = v.rate()
	v.eof, v.err, v.flush = false, nil, false
	// 见 SourceAt 那段：outFrames 不清零，所以偏移要在这里对上。
	v.bias = v.outFrames - frame
	v.marks = v.marks[:0]
}

var (
	_ mygoaudio.Decoder = (*varispeed)(nil)
	_ io.Closer         = (*varispeed)(nil)
)
