// 断网与恢复：源在「拉不动」和「等网络」之间的那一段。
//
// # 为什么要有「等网络」这个状态
//
// 原来的收尾很干脆：换几次流还换不动就 `fail()` 报终态错误。那在 B 站
// 那边稳定的时候是对的，但**网络一断就全崩**——用户从电梯里出来发现歌停了，
// 播放条上写着一句「连不上 B 站这条音频流了」，而他只是过了个电梯。
// `spec.md`「播放状态机」要的是「网络恢复后从断点继续」。
//
// # 为什么「等」天然就是缓冲
//
// 网络断着的时候，解码器**阻塞在 `Read` 上**。这时 MyGo 的混音器发现流里
// 暂时没有帧，就填一段静音并且**不推进播放位置**（`mygo/audio/mixer.go`
// 的 `voice.mix`：`next()` 返回 false 就直接 return）。所以「等」在这一层
// 天然就是缓冲的样子：歌停在原地、进度条停住、没有错误、没有崩溃。
//
// 本文件要做的只有三件事：
//
//	拉不动 → 换 CDN 节点 → 换流（WBI 签名，贵）→ 都试过了就进「等网络」
//	字节进来了 → 缓冲态立刻解除，退避档位归零
//	一直等不到字节 → 预算耗尽，如实说「拉不动了」
//
// # 为什么一定要退避
//
// 不退避的话「等网络」就是一个拿毫秒当单位的循环去撞 CDN 和取流接口。
// B 站在风控状态下会让每一次取流都返回一个马上失效的地址，这么撞下去
// 只是把它自己撞封。500ms 起步、翻倍、封顶 8 秒。
//
// # 为什么预算是「累计等过的时长」而不是「连续等过的时长」
//
// 因为**恢复不该重置预算**。时好时坏的网络断十次，一次断三秒，
// 歌会一直卡在半路——用户看到的是「这首歌今天放不出来」，
// 而不是在十次断网之后终于拿到一句真话。给这一首歌的缓冲总预算，
// 断断续续花掉也就花掉了。
//
// ⚠️ **记的是用户真的等着的墙���时长**，不是「退避睡过的那几秒之和」。
// 只记睡眠的话，一次「拉不动」自己花掉的时间不算数：黑洞式网络下每次
// 尝试要等传输层超时（卡死看门狗 20 秒、换流请求 10 秒），于是用户实际
// 等到的是 `MaxWait` 的好几倍——实测 5 秒/次的情况下等出了 2 分 6 秒，
// 而配置写着 1 分钟。**那不是配置说谎，是记账漏了。**
//
// 反过来，**退避档位**是会被进展重置的：网络回来了一次就该重新从
// 500ms 起步，不然一次短暂抖动之后要等 8 秒才敢再试——那不是保守，
// 那是迟钝。

package audio

import (
	"time"
)

// 「等网络」这一摊的默认值。
const (
	// defaultRetryBackoff 是第一次缓冲等多久。之后翻倍，封顶 defaultMaxRetryBackoff。
	//
	// 取 500ms：一次真实的网络抖动（WIFI 切基站、隧道口）通常一两秒就过去，
	// 500ms × 3 次退避还来得及在用户察觉之前把歌接上。
	defaultRetryBackoff = 500 * time.Millisecond

	// defaultMaxRetryBackoff 是单次缓冲等的上限。8 秒是「用户已经意识到
	// 歌不动了」到「他还愿意等」的分界。
	defaultMaxRetryBackoff = 8 * time.Second

	// defaultMaxWait 是**一首歌**在网络上一共能缓冲多久。
	//
	// 超过它就放弃、如实报错。取一分钟：网络恢复通常在几秒内，而一分钟
	// 是「他大概真的断网了、该换个环境听」的时间。
	//
	// 记的是**真的睡过的时长**（退避的那几段之和），不是墙上时钟——
	// 重试本身花掉的时间不算，那部分是网络自己慢，而那时候缓冲态已经亮着了。
	defaultMaxWait = time.Minute
)

// Waiting 报告源现在是不是在「等网络」。
//
// ��是这一层给上层的缓冲态信号，界面上的「缓冲中……」就是它。
//
// ⚠️ 它和 `Err()` 是**两件不同的事**，不要混：
//
//	Waiting() == true  正在试着拉回来，还没放弃，播放位置停在原地
//	Err()     != nil   已经放弃了，而且有一条能说给人听的真实原因
//
// 网络恢复、字节重新进来时它会自己变回 false，上层每帧读一次就够了。
func (s *StreamSource) Waiting() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waiting && !s.closed
}

// WaitCause 返回最近一次拉不动的真实原因。
//
// 缓冲期间 `Err()` 是 nil（没放弃），所以「到底为什么卡住了」只能从这里问。
// 界面上不用显示它（用户要的是「正在缓冲」不是「CDN 403 了」），
// 诊断导出和日志要用。
func (s *StreamSource) WaitCause() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waitErr
}

// hold 是「换不动了，但也还没到放弃」的那一步：亮起缓冲态，退避，然后接着试。
//
// 返回 `(还要不要接着试, 这一轮先等多久)`。两个都零表示「到头了」。
func (s *StreamSource) hold(cause error) (bool, time.Duration) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false, 0
	}
	s.waitErr, s.waiting = cause, true
	now := s.now()
	// ⚠️ 记**墙上的钟**，不是「这一轮要睡多久」。
	//
	// 从上一次记账到现在这一整段——退避睡的、失败的请求自己花掉的、
	// 换流往返的——全是用户真的在等。只加睡眠的话，黑洞式网络下
	// 用户等到的会是 `MaxWait` 的好几倍（实测 5 秒/次 → 2 分 6 秒），
	// 而配置上写着「最多等一分钟」。
	if !s.waitMark.IsZero() {
		s.waited += now.Sub(s.waitMark)
	}
	s.waitMark = now
	d := s.backoffLocked()
	budgetLeft := s.waited <= s.maxWait
	s.mu.Unlock()

	if !budgetLeft {
		// 缓冲预算用完了。老实说「拉不动了」，而且带上最后一次的原因——
		// 一句没有原因的「连不上」对用户没有任何用处。
		s.fail(cause)
		return false, 0
	}
	return true, d
}

// backoffLocked 算这一轮该等多久，然后退一档。**调用方持锁。**
//
// 翻倍封顶。档位封在 24：再往上 `backoff << step` 会溢出 int64 变成负数，
// 而一个负的等待时长会让 `time.NewTimer` 立刻炸开——那正好是「等网络」
// 最不该出错的时刻。
func (s *StreamSource) backoffLocked() time.Duration {
	d := s.maxBack
	if s.waitStep < 24 {
		if v := s.backoff << s.waitStep; v > 0 && v < s.maxBack {
			d = v
		}
	}
	s.waitStep++
	return d
}

// sleep 睡 d。**Close 会立刻把它叫醒**——不叫醒的话切歌要等完整个退避，
// 而退避最长 8 秒：连点五首就是四十秒的卡顿。
//
// 返回 false 表示「别等了」（源被关了）。
func (s *StreamSource) sleep(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-s.ctx.Done():
		return false
	}
}

// noteProgressLocked 记一次「字节进来了」。
//
// 它只做两件事：解除缓冲态、把退避档位归零。**`waited` 不清零**——
// 那是这一首歌的缓冲总预算，见文件头上那段。
//
// ⚠️ 必须在 `append` 持有 `s.mu` 时调：拆出去再拿一次锁的话，
// 「等网络」的那一轮可能刚好在这中间醒过来，把缓冲态又点亮，
// 于是歌已经在走了而界面上还写着「缓冲中」。
func (s *StreamSource) noteProgressLocked() {
	if !s.waiting && s.waitStep == 0 {
		return
	}
	s.waiting = false
	s.waitErr = nil
	s.waitStep = 0
	// ⚠️ **`waited` 不清零**（见文件头那段），但**时钟要重置**：不然下一次
	// 开始等的时候，会把「上一次等完之后到现在的这一段正常播放」也算进预算里。
	s.waitMark = time.Time{}
}
