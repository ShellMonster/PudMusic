// 音量渐变：增益从当前值平滑地走到目标值。
//
// ## 治的是什么
//
// 两件：
//
//  1. **「一上来就全速」**：音量落盘治好了「每次启动跳回 100%」，但**另一半**还在
//     ——用户为了不吵把音量调到 20%，重启之后那 20% 是**一下子**出来的，耳机上
//     那一下仍然是惊吓。所以「开始放」的那一下是从 0 往上走的。
//  2. **「调音量时突然大声」**：把滑块从 20% 一路拖到 100%，最后那一下是**炸**着
//     出来的。增益走一段斜坡就没有那一下了（报告 §3.3 的 C2）。
//
// ## 为什么在这一层做
//
// 增益是**这一层**的事（`Player.play.SetVolume`），而「什么时候开始放」也只有
// 这一层知道（`Player.Play`）。放在界面层的话，每一个入口（播放键、快捷键、
// 系统媒体控件、队列自动下一首……）都得自己记得淡一下——**漏一个就是「一上来
// 就全速」**，而漏掉的那一个 nobody 会去查（症状只是「有点突然」）。
//
// ## 「淡入」和「渐变」是同一件事的两种用法
//
// 增益**从来不从中间跳到别处**：往上是渐变，往下也是渐变。所以：
//
//   - 调音量（拖滑块 / 滚滚轮 / 按音量键）→ 从当前值渐变到新值（**不是淡入**：
//     声音一直在那儿，只是变响变轻；报告 §6「否掉」那一栏里的「拖滑条时淡入
//     淡出」说的是这个，而它没被否掉的是「别突然大声」这一半）。
//   - 按静音 → 渐变到 0（**淡出**）。
//   - 取消静音 → 渐变回用户设的那一档（**淡入**）。
//   - 开始放 / 恢复 → 从 0 渐变到那一档（**淡入**，那正是「一上来就全速」要治的）。
//
// ## 为什么「淡出走完才真停」没做
//
// 那一档（暂停时滑到 0，等淡出走完才真停）会和「拖到队尾就停」打架：
// `App.AdvanceStop` 是 `Seek(0)` + `Pause()`，淡出的话用户会听见**歌的开头**
// 淡出去。报告 §3.3 的 B27 把那一档列为「观察」而不是「采纳」，所以这一票没做。
package audio

import "time"

const (
	// rampDuration 是一次增益斜坡要走多久。
	//
	// 150 毫秒：报告 §3.3 的 C2 说 100~200ms（MusicPlayer2 的 BASS 默认 500ms
	// 偏长——拖滑块会「粘」；jellyfin-web 固定 100ms），取中间那个——短到听不出
	// 是渐变，长到不像「咔」一声。
	rampDuration = 150 * time.Millisecond

	// rampStep 是斜坡的步长。
	//
	// 10 毫秒：15 步走完 150 毫秒。MyGo 自己的 mixer 另有一次 5 毫秒的斜坡
	//（`mygo/audio/mixer.go` 的 `rampFrames`），两级叠起来是平滑的。
	rampStep = 10 * time.Millisecond
)

// setGain 设当前生效的增益，**立刻**。
func (p *Player) setGain(g float64) {
	p.gainMu.Lock()
	p.gain = g
	p.applyGainLocked(g)
	p.gainMu.Unlock()
}

// startRamp 让增益从当前值走到 target，花 d。
//
// ⚠️ **它会顶掉正在跑的那一趟斜坡**：用户连着动音量的时候，两趟斜坡同时写增益
// 的话，听感上是「音量自己往回爬」。
func (p *Player) startRamp(target float64, d time.Duration) {
	p.gainMu.Lock()
	from := p.gain
	stop := p.fadeStop
	p.fadeStop = nil
	p.gainMu.Unlock()
	if stop != nil {
		close(stop)
	}
	if from == target {
		p.setGain(target)
		return
	}

	steps := max(1, int(d/rampStep))
	stop = make(chan struct{})
	p.gainMu.Lock()
	p.fadeStop = stop
	p.gainMu.Unlock()

	go func() {
		for i := 1; i <= steps; i++ {
			select {
			case <-stop:
				return
			case <-time.After(rampStep):
			}
			if !p.applyRampGain(stop, from+(target-from)*float64(i)/float64(steps)) {
				return
			}
		}
		p.applyRampGain(stop, target)
	}()
}

// stopRamp 停掉正在跑的那一趟斜坡。
func (p *Player) stopRamp() {
	p.gainMu.Lock()
	stop := p.fadeStop
	p.fadeStop = nil
	p.gainMu.Unlock()
	if stop != nil {
		close(stop)
	}
}

// applyRampGain 落一个斜坡途中的增益值。
//
// ⚠️ **这一趟不是当前那一趟就放弃**（被 `SetVolume` 顶掉了，或者被 `Close` 收
// 掉了）：一个走完了的斜坡再去写增益，听感上是「音量自己往回爬」。
func (p *Player) applyRampGain(stop chan struct{}, g float64) bool {
	p.gainMu.Lock()
	defer p.gainMu.Unlock()
	if p.fadeStop != stop {
		return false
	}
	p.gain = g
	p.applyGainLocked(g)
	return true
}

// applyGainLocked 把增益用到底下的播放器和频谱挂点上。
//
// **必须在持 gainMu 时调**：斜坡那一趟和界面线程上的 `SetVolume` 会撞车，两边
// 各写各的增益时，最后落地的那一个不一定是对的。
//
// ⚠️ **频谱挂点也拿一份**：它挂在解码器上，看不见播放器这一层的音量（见
// tap.go）。渐变的时候频谱跟着一起变——画面上的频谱和耳朵听到的声音是一件事。
func (p *Player) applyGainLocked(g float64) {
	p.tap.SetVolume(float32(g))
	p.play.SetVolume(g)
}

// gainNow 返回**当前生效**的增益。给测试看：渐变途中它比目标小。
func (p *Player) gainNow() float64 {
	p.gainMu.Lock()
	defer p.gainMu.Unlock()
	return p.gain
}
