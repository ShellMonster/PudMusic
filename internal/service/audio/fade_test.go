package audio

import (
	"testing"
	"time"
)

// 这一组盯的是「音量的渐变」（票 #311 的 C2）：
//
//   - 开始放 / 恢复时**淡入**（增益从 0 走到那一档，不是「一上来就全速」）；
//   - 调音量时**渐变**（增益从当前值走到新值，不是「炸」着出来的）；
//   - 按静音是**淡出**（渐变到 0）。
//
// 治的是两件静默的事：音量落盘治好了「每次启动跳回 100%」，但那 20% 是**一下子**
// 出来的，耳机上仍然是惊吓；而拖滑块拖到最后那一下是「炸」着出来的。

// fadePlayer 造一个用假解码器的播放器（不碰真文件，也不出声）。
func fadePlayer(t *testing.T) *Player {
	t.Helper()
	p := New(newFakeDec(44100, 2, 44100*30))
	t.Cleanup(p.Close)
	return p
}

// TestTheVolumeFadesInOnStart 验「开始放的那一下是淡入的」。
//
// 三件事：
//   - 开口那一刻增益**不是**目标值（那正是「一上来就全速」）；
//   - 走的时候**经过**中间值（一步跳过去的不是淡入）；
//   - 走完之后到位。
func TestTheVolumeFadesInOnStart(t *testing.T) {
	p := fadePlayer(t)
	p.SetVolume(0.8)

	p.Play()

	// 开口那一刻：增益还贴着 0，不是 0.8。
	//
	// ⚠️ **这里不能断「正好是 0」**：淡入的第一步在 10 毫秒之后，而这一步和
	// 断言之间隔着一个调度——机器忙起来的时候那 10 毫秒早就过去了。断的是
	// 「没有一下子跳到目标」，那才是要治的毛病。
	if got := p.gainNow(); got >= 0.8 {
		t.Errorf("开口那一刻增益就是 %v（目标 0.8）——它是一上来就全速", got)
	}

	// 走的时候要经过中间值。
	//
	// ⚠️ **判据是「看见了中间值」，不是「等了多久」**：机器慢一点渐变就慢一点，
	// 而慢一点的时候它红得出来却和被测的逻辑一点关系都没有。
	sawMiddle := false
	waitUntil(t, 3*time.Second, func() bool {
		g := p.gainNow()
		if g > 0 && g < 0.8 {
			sawMiddle = true
		}
		return g >= 0.8
	})
	if !sawMiddle {
		t.Error("从头到尾没有经过中间值——那不是淡入，是一步跳过去的")
	}
	if got := p.gainNow(); got != 0.8 {
		t.Errorf("淡入走完之后增益是 %v，want 0.8", got)
	}
}

// TestSettingTheVolumeRamps 验「调音量是渐变的」。
//
// 把滑块从 20% 一路拖到 100%，最后那一下是**炸**着出来的——增益走一段斜坡就
// 没有那一下了（报告 §3.3 的 C2）。
//
// ⚠️ **它不是「淡入」**：声音一直在那儿，只是变响。所以判据是「增益从当前值
// **朝着**新值走」，不是「从 0 走」。
func TestSettingTheVolumeRamps(t *testing.T) {
	p := fadePlayer(t)
	p.SetVolume(0.8)
	p.Play()
	waitUntil(t, 3*time.Second, func() bool { return p.gainNow() >= 0.8 })

	p.SetVolume(0.2)

	// 刚开始那一瞬间：还没到 0.2（那才是「炸」着出来的）。
	if got := p.gainNow(); got <= 0.2 {
		t.Errorf("刚设完音量增益就是 %v——它一步就跳过去了", got)
	}
	// 走完之后到位。
	waitUntil(t, 3*time.Second, func() bool { return p.gainNow() <= 0.2 })
	if got := p.gainNow(); got != 0.2 {
		t.Errorf("渐变走完之后增益是 %v，want 0.2", got)
	}
}

// TestMutingFadesOut 验「按静音是淡出」。
//
// 增益渐变到 0，而不是「咔」地一下没了（票 #311 的「淡入淡出」那半边）。
func TestMutingFadesOut(t *testing.T) {
	p := fadePlayer(t)
	p.SetVolume(0.8)
	p.Play()
	waitUntil(t, 3*time.Second, func() bool { return p.gainNow() >= 0.8 })

	// 静音 = 增益渐变到 0。
	p.SetVolume(0)
	if got := p.gainNow(); got <= 0 {
		t.Errorf("刚静音增益就是 %v——它一步就切过去了（那正是「咔」的一下）", got)
	}
	waitUntil(t, 3*time.Second, func() bool { return p.gainNow() <= 0 })
	if got := p.gainNow(); got != 0 {
		t.Errorf("淡出走完之后增益是 %v，want 0", got)
	}
}

// TestSettingTheVolumeRetargetsTheRamp 验「连着动音量，那一趟斜坡只留最新的一趟」。
//
// 不作废的话：用户把音量拉到 30%，150 毫秒之后那一趟斜坡会把增益写成 80%——
// 听感上是「音量自己往回爬」。
func TestSettingTheVolumeRetargetsTheRamp(t *testing.T) {
	p := fadePlayer(t)
	p.SetVolume(0.8)
	p.Play()
	waitUntil(t, 3*time.Second, func() bool { return p.gainNow() >= 0.8 })

	p.SetVolume(0.3)

	// 那一趟斜坡要是没被作废，它会在 150 毫秒之后把增益写成 0.8。
	//
	// ⚠️ **这里等的是「后台那一趟有机会跑完」**（400 毫秒 > 渐变的 150 毫秒），
	// 判据是最后那个值：增益必须**还是** 0.3。
	time.Sleep(400 * time.Millisecond)
	if got := p.gainNow(); got != 0.3 {
		t.Errorf("渐变走完之后增益是 %v，want 0.3——用户动的音量被那一趟斜坡顶掉了", got)
	}
}

// TestCloseStopsTheRamp 验「关掉播放器之后，那一趟斜坡不再写增益」。
//
// 不停掉的话：切歌之后旧播放器还在往自己身上写增益——而它已经关了。
func TestCloseStopsTheRamp(t *testing.T) {
	p := New(newFakeDec(44100, 2, 44100*30))
	p.SetVolume(0.8)
	p.Play()
	p.Close()

	// 那一趟斜坡要是还在跑，它会继续往上走。走完了就不再变。
	before := p.gainNow()
	time.Sleep(400 * time.Millisecond)
	if got := p.gainNow(); got != before {
		t.Errorf("关掉之后增益还在变（%v → %v）——那一趟斜坡没收掉", before, got)
	}
}

func TestALongFadeInOnlyAppliesToTheNextPlay(t *testing.T) {
	p := fadePlayer(t)
	p.SetVolume(0.8)
	p.SetFadeIn(2 * time.Second)
	p.Play()
	time.Sleep(300 * time.Millisecond)
	if got := p.gainNow(); got <= 0 || got >= 0.4 {
		t.Errorf("2 秒淡入走了 0.3 秒，增益 %v，want 在 (0, 0.4) 之间", got)
	}
	p.Pause()
	p.SetVolume(0.8)
	p.Play()
	waitUntil(t, 3*time.Second, func() bool { return p.gainNow() >= 0.8 })
}

func TestFadeOutGoesToZeroAndSetVolumeBringsItBack(t *testing.T) {
	p := fadePlayer(t)
	p.SetVolume(0.8)
	p.Play()
	waitUntil(t, 3*time.Second, func() bool { return p.gainNow() >= 0.8 })
	p.FadeOut(400 * time.Millisecond)
	waitUntil(t, 3*time.Second, func() bool { return p.gainNow() <= 0 })
	p.SetVolume(0.8)
	waitUntil(t, 3*time.Second, func() bool { return p.gainNow() >= 0.8 })
}
