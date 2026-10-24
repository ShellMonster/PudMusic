// 边下边播的字节源：解码器要哪一段，就从 B 站的 CDN 取哪一段。
//
// # 为什么不用 `ExtAudioFileOpenURL`
//
// 那条路只接受有文件系统支撑的 `file://`，而且实测要求文件完整到 **98.3%**
// 才肯打开（`verify-streaming.md` §1.2）。走 `AudioFileOpenWithCallbacks`
// 之后字节从哪来完全由我们说了算——这正是本文件存在的理由。
//
// # 三件事必须做对
//
//  1. **阻塞要能取消**。下载中的 `Read` 会一直等，等到有人来填或者等到天亮。
//     MyGo 的解码 goroutine 就阻塞在这里，而它的 `close()`（`audio/source.go`
//     的 `stream.close`）**只置自己的标记并 Signal 自己的 cond，不会解除外部
//     reader 的等待**。所以 `Close` 必须自己把这个源叫醒，否则快速切歌
//     或退出会遗留解码 goroutine 和缓存文件。
//
//  2. **换流要接得上**。B 站的地址带 `deadline`，过期是常态。下载中途换了
//     地址之后从**同一个字节偏移**接着下（HTTP Range），解码器那边读到的
//     就是一条连续的字节流，播放位置一点不动——这是 `spec.md` 里
//     「换流后从原进度继续」那条的实现。
//
//  3. **短读只在文件末尾合法**。`extaf` 的读回调碰到中途短读一律
//     `OSStatus -50`（`verify-streaming.md` §3.3）。所以这里的 `Read`
//     永远阻塞到凑满 buf 或真的到了末尾，绝不返回半截。

package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// StreamSource 的哨兵错误。
var (
	// ErrSourceClosed 是源已经关了。上层看到它就知道是切歌/退出，
	// 不是播放出错——界面上不该给用户一句红字。
	ErrSourceClosed = errors.New("audio: 字节源已关闭")

	// ErrStreamDead 是「这条流拉不下来了」。
	//
	// 它和 `io.EOF` 是两件事：EOF 是歌放完了，ErrStreamDead 是中途拉不动了。
	// 上层要能把这两者分开说给用户听——「歌放完了」和「歌断了」在界面上
	// 是完全不同的两句话，用户对它们的反应也完全不同。
	ErrStreamDead = errors.New("audio: 在线流拉不下来了")

	// ErrStreamChanged 是「换流换到了另一条流」。
	//
	// 同一首歌同音质重新取流，拿回来的是**同一段字节**，所以能从原偏移接上。
	// 但 B 站偶尔会换掉（会员过期、稿件被替换），这时新旧两条流的**长度
	// 或容器不一样**——从旧偏移读下去会读到错位的数据，解出来是噪声。
	//
	// 这时候必须老实说「这条流换了」，让上层重新打开并跳回原来的**播放位置**
	// （那是时间，不是字节偏移），而不是硬接。
	ErrStreamChanged = errors.New("audio: 换流换到了另一条流，接不上原来的位置")
)

// 默认值。
const (
	// defaultStallTimeout 是「多久没收到新字节就算这次下载废了」。
	//
	// 不是网络抖动的容忍度，是**卡死**的判定：TCP 断了之后连接可能既不报错
	// 也不再有数据（NAT 超时、代理静默丢弃），不设这个上限，解码器会一直
	// 等下去，界面上表现成「歌停在那儿不动，也不报错」。
	//
	// 取 20 秒：一首 128K 的歌一秒 16 KB，20 秒连一个音频帧都下不完就说明
	// 真的断了。B 站的 CDN 正常时不会这么慢。
	defaultStallTimeout = 20 * time.Second

	// stallCheckInterval 是看门狗的检查间隔。取超时的一半，
	// 这样一次卡死最多在超时的一半之后被发现。
	stallCheckInterval = 5 * time.Second

	// defaultMaxRefresh 是一次播放最多换几次流。
	//
	// 上限是为了防住「换流 → 新的地址也 403 → 再换」的循环：B 站在风控
	// 状态下会让每一次取流都返回一个马上失效的地址，没有上限就是死循环。
	// 三次之后老实报错，用户看到的仍然是那句人话。
	defaultMaxRefresh = 3

	// copyBufSize 是下载时的拷贝缓冲。
	copyBufSize = 64 << 10
)

// defaultBackoff 是**默认的退避序列**：1 秒、2 秒、4 秒。
//
// # 为什么是这三个数
//
//	1 秒	CDN 抖一下（地铁进隧道、Wi-Fi 换信道）通常一秒之内就缓过来了
//	2 秒	再宽一档，覆盖「刚被风控挡了一下」那种一秒不够的情况
//	4 秒	最后一次，给它一个真有可能会成功的窗口
//
// # 为什么**总时长封顶在 7 秒**
//
// 再长用户就走了。他看到的是「歌不动了」，而「等 20 秒」和「等 4 秒」
// 在他的感受里没有区别——他只会以为应用死了，然后去按暂停。
//
// ⚠️ **这是「指数退避 + 封顶」**，不是「固定间隔」也不是「随机抖动」：
// 市面上量级相近的几种做法是 mpv 的 FFmpeg 后端（0/1/3/7 秒）、
// Nuclear 的换源退避（1 秒起、×2、封顶 30 秒，但只用于换源）、
// VLC 的 EOS 突发退避（100ms→3.2 秒）。我们取最保守的那一档：**总等待
// 不超过 7 秒**，而退避期间界面上一直写着「正在重连」（票 #315）。
func defaultBackoff() []time.Duration {
	return []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
}

// StreamReply 是换流之后拿到的新一条流。
//
// 刻意**不**用 `bili.StreamRef`：服务层之间不互相认识（`spec.md` 的四层
// 依赖是单向的），`app` 那一层负责把两者对上。
type StreamReply struct {
	// URLs 是同一段字节的多个地址，主地址在前。
	URLs []string
	// Size 是字节数。**-1 或 0 表示不知道**——B 站通常给 Content-Length，
	// 但不给时源会在读完之后自己把总长定下来。
	Size int64
	// ExpiresAt 是这组地址的失效时刻。
	ExpiresAt time.Time
	// Quality 是这条流的音质徽章文字（「Hi-Res 无损」/「192K」）。
	// 换流之后界面上的徽章要跟着变：降级了用户得看见。
	Quality string
}

// StreamSourceOptions 是建源要的零件。
type StreamSourceOptions struct {
	// URLs 是第一条流的地址（主 + 备）。空则源一建好就是坏的。
	URLs []string
	// Size 是第一条流的长度，已知时填；0 或 -1 表示不知道（源会从
	// 响应头的 Content-Length 学）。取流接口的返回里没有长度，所以生产
	// 路径上这一项永远是「不知道」——留着是因为本地调用方可能已经知道
	// （比如刚从磁盘缓存里读过一遍）。
	Size int64
	// ExpiresAt 是这组地址的失效时刻。零值表示不预判。
	ExpiresAt time.Time

	// Refetch 在这组地址都读不动时换一条新的。参数是「已经下到哪个字节」，
	// 新流要能从那里接着下。返回 nil 表示换不到。
	//
	// **不要在这里做网络请求以外的事**：它是下载 goroutine 里调的，
	// 卡住就是整首歌卡住。
	Refetch func(ctx context.Context, from int64) *StreamReply

	// OnRefresh 每次换流成功后调一次，让上层把音质徽章更新掉。
	OnRefresh func(*StreamReply)

	// OnRetry 每次**准备重试**时调一次。
	//
	// 🔴 **它在退避开始之前调**：界面上的「正在重连」得从**第一秒**就亮着，
	// 而不是等到重试真的跑起来了才说——等到那时候用户已经对着一个不动的
	// 界面等了好几秒了（票 #315）。
	//
	// 参数是「第几次重试」（从 1 起）和「这一次要等多久」。
	OnRetry func(attempt int, wait time.Duration)

	// OnReconnected 重试之后**字节又开始来了**时调一次。
	//
	// 第一次就下成功的话不调——那时候压根没有重连过。
	//
	// ⚠️ **它报的是「数据回来了」，不是「这一趟成功了」**：换流可能换到了
	// 另一条流（那要上层重新开解码器并跳回播放位置），而数据回来只说明
	// 连接恢复了。
	OnReconnected func()

	// Backoff 是**每一次重试之前要等的时长**序列。
	//
	// 零值用默认：1 秒、2 秒、4 秒（见 `defaultBackoff`）。
	//
	// 为什么要有它：CDN 抖一下的时候三次重试在几毫秒里就打完了一——那不叫
	// 「重试」，那是连着撞了同一堵墙三次。中间等一等，网络缓过来了就救得
	// 回来；网络没缓过来，等下去也只是把同一堵墙多撞两次。
	//
	// ⚠️ **序列用完了用最后一个**（不是变成 0）：重试次数的上限是
	// `MaxRefresh`，过了那个上限源就报终态错误了，所以「用完」这个状态
	// 压根到不了。
	Backoff []time.Duration

	// Client 是取字节用的客户端。
	//
	// ⚠️ **必须带凭据边界**（`app.CredentialTransport`），而且**不要挂 jar**：
	// B 站 CDN 跟 B 站 API 不是同一个凭据域，把 SESSDATA 发给存音频文件的
	// CDN 没有任何理由（`spec.md`「网络」一节）。Referer 照给，B 站不给就 403。
	Client *http.Client

	// Now 是时钟。nil 用 time.Now。给假时钟是为了让「卡死判定」能被测。
	Now func() time.Time
	// StallTimeout 是卡死判定。零值用 defaultStallTimeout。
	StallTimeout time.Duration
	// MaxRefresh 是最多换几次流。零值用 defaultMaxRefresh。
	MaxRefresh int
	// TempDir 是缓存文件落在哪。空 = 系统临时目录。
	TempDir string
}

// StreamSource 是一个「能播、能拖、能换流、能取消」的在线字节源。
//
// 它实现了 `io.ReadSeeker`，所以本地文件那条解码链（MyGo 的格式表 →
// extaf / flacdec / AIFF）**一个字都不用改**就能吃在线流。
//
// 零值不可用，请用 `NewStreamSource`。
type StreamSource struct {
	// opMu 串行化「游标 + 读」这一对。
	//
	// ⚠️ AudioToolbox 的读回调是**从它自己的线程**调进来的，
	// `extaf.source.readAt` 又是「先 Seek 再 Read」。用共享游标的
	// `io.ReadSeeker` 就要假设这两步不会被拆开——用互斥锁把它变成事实，
	// 比「反正 AudioToolbox 只用一条线程」这种假设稳。
	opMu sync.Mutex

	mu     sync.Mutex
	cond   *sync.Cond
	file   *os.File
	path   string // 缓存文件路径，Close 时删
	filled int64  // 已经下到缓存里的字节数（下载前沿）
	size   int64  // 总长；-1 是还不知道
	cur    int64  // 读游标
	err    error  // 终态错误；io.EOF 是「正常读完」
	closed bool

	// expectTotal 是「换流之后，新流**应该**有多长」。
	//
	// 为什么需要它：换流拿回来的新流可能根本不是同一段字节（会员过期、
	// 稿件被替换）。长度对不上还硬接，解出来的是噪声，而且**不在开头出错**——
	// 要播到那个偏移才炸，比直接失败难查一百倍。
	//
	// 取流接口的返回里没有长度（`bili.StreamRef.Size` 因此拿不到东西），
	// 所以这个数是靠**两个响应各自报出来的 Content-Length 相比**得到的：
	// 旧流的长度在换流之前就知道了，新流的长度在收到它的响应头时才知道。
	expectTotal int64

	urls      []string
	urlIdx    int
	quality   string
	refreshes int
	maxRefr   int
	expiresAt time.Time

	client  *http.Client
	refetch func(ctx context.Context, from int64) *StreamReply
	onRefr  func(*StreamReply)
	now     func() time.Time
	stall   time.Duration

	// backoff 是**每一次重试之前要等的时长**序列（见 `defaultBackoff`）。
	backoff []time.Duration
	// onRetry / onReconn 是「正在重试」和「字节又回来了」这两个回调
	// （见 `StreamSourceOptions`）。它们给上层一个能显示在界面上的状态。
	onRetry  func(attempt int, wait time.Duration)
	onReconn func()
	// retryPending 是「上一次是重试来的，字节还没回来」。
	//
	// 字节回来的那一刻（`append`）就报一次「恢复了」——判据是**真的有
	// 字节到了**，不是「请求没报错」：连上了却一个字节都下不动（半死不活
	// 的连接）在用户听来和没恢复一样。
	retryPending bool

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// reqCancel 是**当前这一次** HTTP 请求的取消函数，看门狗用它掐断卡死的请求。
	reqMu     sync.Mutex
	reqCancel context.CancelFunc

	// bytesAtLastCheck 只被看门狗写、只被泵读，用来判断「有没有进展」。
	bytesAtLastCheck int64
}

// NewStreamSource 建一个源并立刻开始下载。
//
// 返回的源**已经在下载中**：解码器马上就能读到头几个字节，不用等下完。
// 下载在后台的 goroutine 里跑，`Read` 阻塞到字节到齐。
//
// 建源失败只有一种：临时文件建不出来（磁盘满、没权限）。那是要往界面上
// 报的实打实的错误。
func NewStreamSource(o StreamSourceOptions) (*StreamSource, error) {
	if o.Client == nil {
		return nil, errors.New("audio: 在线字节源需要一个 http.Client")
	}
	if len(o.URLs) == 0 {
		return nil, errors.New("audio: 在线字节源至少要有一个地址")
	}

	f, err := os.CreateTemp(o.TempDir, "biu-stream-*"+streamExt(o.URLs))
	if err != nil {
		return nil, fmt.Errorf("audio: 建不了在线流的缓存文件：%w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &StreamSource{
		file:      f,
		path:      f.Name(),
		size:      o.Size,
		urls:      append([]string(nil), o.URLs...),
		quality:   "",
		maxRefr:   orDefaultInt(o.MaxRefresh, defaultMaxRefresh),
		expiresAt: o.ExpiresAt,
		client:    o.Client,
		refetch:   o.Refetch,
		onRefr:    o.OnRefresh,
		onRetry:   o.OnRetry,
		onReconn:  o.OnReconnected,
		backoff:   append([]time.Duration(nil), o.Backoff...),
		now:       o.Now,
		stall:     orDefaultDuration(o.StallTimeout, defaultStallTimeout),
		ctx:       ctx,
		cancel:    cancel,
	}
	// Options.Size 的 0 和 -1 都表示「还不知道」。不归一的话 size 会停在 0，
	// 于是 Seek(0, SeekEnd) 立刻答 0——extaf 拿到的 AudioFileID 长度是 0，
	// 不报错，只是永远没有声音（`verify-streaming.md` §5.4 记的那个坑）。
	if s.size <= 0 {
		s.size = -1
	}
	if s.now == nil {
		s.now = time.Now
	}
	s.cond = sync.NewCond(&s.mu)

	s.wg.Add(2)
	go s.pump()
	go s.watchdog()
	return s, nil
}

// Err 返回这条流为什么停下了。正常读完是 `io.EOF`，中途拉不动是
// 一个包着 `ErrStreamDead` 的错。
//
// **为什么要单独问它**：解码器看到的是**后果**——字节断了之后它多半
// 先报「这一帧的 CRC 对不上」，而真正的原因是网络断了或者地址失效。
// 用户看到的应该是后者。
func (s *StreamSource) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Quality 返回当前这条流的音质徽章文字。换过流的话是新的那条。
func (s *StreamSource) Quality() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quality
}

// Size 返回总长。还不知道时是 -1。
func (s *StreamSource) Size() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.size
}

// Filled 返回已经下到缓存里的字节数。
func (s *StreamSource) Filled() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.filled
}

// Read 从当前游标读，读不到就**阻塞**到字节下到或者下载结束。
//
// ⚠️ 阻塞是可取消的：`Close` 会把它叫醒。返回的 `(n, err)` 里 n 大于 0 时
// err 一定是 nil——`extaf` 的读回调不允许中途短读。
func (s *StreamSource) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()

	for {
		s.mu.Lock()
		switch {
		case s.closed:
			s.mu.Unlock()
			return 0, ErrSourceClosed

		case s.cur < s.filled:
			n := int64(len(p))
			if room := s.filled - s.cur; n > room {
				n = room
			}
			// 游标先挪好再解锁：这一次读的是哪一段在锁内就定下来，
			// 不然解码器的读回调中途改游标会读到错位的段。
			var off int64
			off, s.cur = s.cur, s.cur+n
			s.mu.Unlock()

			got, err := s.file.ReadAt(p[:n], off)
			if got > 0 {
				return got, nil
			}
			if err != nil && !errors.Is(err, io.EOF) {
				return 0, fmt.Errorf("audio: 读在线流的缓存：%w", err)
			}
			// 一个字节都读不到却没报错：缓存文件被外力动过了。
			// 记成终态，别在这儿空转。
			s.mu.Lock()
			s.cur = off
			s.mu.Unlock()
			s.fail(fmt.Errorf("audio: 在线流的缓存读不到第 %d 字节", off))

		case s.err != nil:
			e := s.err
			s.mu.Unlock()
			if errors.Is(e, io.EOF) {
				return 0, io.EOF
			}
			return 0, e

		default:
			// 前沿还没到，等下载追上来。Close 会 Broadcast 把这里叫醒。
			//
			// ⚠️ **`Wait` 返回时锁是拿着的**。循环开头又是一句 `s.mu.Lock()`，
			// 不在这里先放开就成了自己等自己——表现是「所有人都在等一把没人
			// 放的锁」，而 dump 里看不出任何人在写东西，最难查的一种挂死。
			s.cond.Wait()
			s.mu.Unlock()
		}
	}
}

// Seek 挪游标。**不阻塞**，除非是 `SeekEnd`——那个必须等总长出来。
//
// 只有 `SeekEnd` 会等是有理由的：`extaf.Open` 开口第一件事就是
// `Seek(0, SeekEnd)` 问长度（`decoder_darwin.go`），而它拿到的那个
// AudioFileID 的长度之后要一直用。早一步拿到反而好。
func (s *StreamSource) Seek(off int64, whence int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrSourceClosed
	}

	var target int64
	switch whence {
	case io.SeekStart:
		target = off
	case io.SeekCurrent:
		target = s.cur + off
	case io.SeekEnd:
		for s.size < 0 && s.err == nil && !s.closed {
			s.cond.Wait()
		}
		if s.closed {
			return 0, ErrSourceClosed
		}
		if s.size < 0 {
			return 0, s.terminalErr("问不到在线流的总长")
		}
		target = s.size + off
	default:
		return 0, fmt.Errorf("audio: 不知道的 Seek 方式 %d", whence)
	}

	if target < 0 {
		return 0, errors.New("audio: 位置不能是负的")
	}
	s.cur = target
	return target, nil
}

// Close 停掉下载、叫醒所有等着的人、删掉缓存文件。可以重复调用。
//
// 这一个函数是「快速切歌不留东西」的全部保证：解码 goroutine 阻塞在 Read
// 上、下载 goroutine 阻塞在 HTTP 上、缓存文件躺在临时目录里，三样都被它
// 一次收拾掉。
func (s *StreamSource) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	// 先把等着的都叫醒，再关门——反过来会有人被唤醒之后又睡回去。
	s.cond.Broadcast()
	f := s.file
	path := s.path
	s.mu.Unlock()

	s.cancel()
	s.cancelRequest()
	s.wg.Wait()

	// 关文件要在两个 goroutine 都退之后：它们可能正在 WriteAt。
	var errs []error
	if f != nil {
		if err := f.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	// 缓存文件删不掉不是错误，只是会留一个临时文件在磁盘上——
	// 报给用户毫无意义。
	if path != "" {
		_ = os.Remove(path)
	}
	return errors.Join(errs...)
}

// fail 记一个终态错误并叫醒所有等着的人。可以重复调用，第一条赢。
//
// 非 EOF 的终态错误都会包一层 `ErrStreamDead`——那是「拉不动了」的标记，
// 上层靠它把「歌放完了」和「歌断了」分开说（见 ErrStreamDead）。
func (s *StreamSource) fail(err error) {
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, ErrStreamDead) {
		err = fmt.Errorf("%w：%w", ErrStreamDead, err)
	}
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.cond.Broadcast()
	s.mu.Unlock()
}

// terminalErr 在没有别的错可报时给一个交代。
func (s *StreamSource) terminalErr(what string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		if errors.Is(s.err, io.EOF) {
			return fmt.Errorf("audio: %s", what)
		}
		return s.err
	}
	return fmt.Errorf("audio: %s", what)
}

func orDefaultInt(v, d int) int {
	if v <= 0 {
		return d
	}
	return v
}

func orDefaultDuration(v, d time.Duration) time.Duration {
	if v <= 0 {
		return d
	}
	return v
}

// streamExt 从地址里猜一个扩展名，只为让临时文件的名字能看出来是什么。
//
// 猜错了没有任何后果：这个文件只是解码器的字节来源，容器是认**头几个字节**
// 的（`containers.go` 里的 `????ftyp` / `fLaC`），不是认后缀。
func streamExt(urls []string) string {
	if len(urls) == 0 {
		return ".bin"
	}
	// 只看**路径**那一段：B 站的地址后面拖着一长串查询串
	//（`&upsig=…&uparams=…`），从头扫到尾根本扫不到文件名那一段，
	// 于是每个缓存文件都叫 .bin —— 排查时看着像一堆没用的东西。
	path := urls[0]
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if i := strings.LastIndexByte(path, '.'); i > strings.LastIndexByte(path, '/') {
		return path[i:]
	}
	return ".bin"
}
