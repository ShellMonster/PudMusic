package app_test

// 这一组测的是**下一首提前备地址**（票 #366）。
//
// # 这一票的收益是什么
//
// 是**延迟**收益：切歌到出声之间的那段空档短了多少。判据是「取流请求在
// 什么时候发出」和「真播下一首时有没有重新取流」——**不是**每帧分配
// （那一条是 `internal/ui/framebudget_test.go` 管的，报告 P8 明确说了这一票
// 不在那一条的范围里）。
//
// # 全部走夹具
//
// 一律打本地的假 B 站（`streamFixture`），**不连真 B 站**——钥匙串里的是用户
// 真实的登录态，而预取是「多打一次请求」这种动作，最不该拿真账号去试
// （AGENTS.md §6）。

import (
	"bytes"
	"encoding/binary"
	"math"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// ---- 小工具 ----

// shortWav 造一段**很短**的 WAV（真的 WAV，不是假字节）。
//
// 为什么不用那份 FLAC 素材：它有四秒长，而这一组要等「这一首放完了」才会
// 发生自动切歌（自然放完才触发）。四秒乘以十来条用例就是一分多钟——而这一组
// 要验的东西跟「音频内容是什么」毫无关系。一秒就够：预取在第一拍就发出去
// 了，而这一首放完之后自动切歌才发生。
//
// ⚠️ **它得是真的 WAV**：解码器要真的能解开它，「放完了」这个事件才真的会发生
// （假字节解不开，那首歌压根放不起来）。
func shortWav(seconds float64) []byte {
	const rate = 44100
	n := int(rate * seconds)
	data := make([]byte, n*2) // 16 位单声道
	for i := 0; i < n; i++ {
		v := int16(3000 * math.Sin(2*math.Pi*440*float64(i)/rate))
		binary.LittleEndian.PutUint16(data[i*2:], uint16(v))
	}
	buf := &bytes.Buffer{}
	buf.WriteString("RIFF")
	_ = binary.Write(buf, binary.LittleEndian, uint32(36+len(data)))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(buf, binary.LittleEndian, uint16(1)) // PCM
	_ = binary.Write(buf, binary.LittleEndian, uint16(1)) // 单声道
	_ = binary.Write(buf, binary.LittleEndian, uint32(rate))
	_ = binary.Write(buf, binary.LittleEndian, uint32(rate*2)) // 字节率
	_ = binary.Write(buf, binary.LittleEndian, uint16(2))      // 块对齐
	_ = binary.Write(buf, binary.LittleEndian, uint16(16))     // 位深
	buf.WriteString("data")
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(data)))
	buf.Write(data)
	return buf.Bytes()
}

// playURLBySong 装一个「**按 bvid/cid 回地址**」的取流接口。
//
// 为什么不用固定的那几条 dash：这一组要验的是「备好的那份和真播的那一首是
// 同一首」，而那要靠**地址**认出来——每首歌的地址不一样，CDN 被打了哪一条
// 就说明放的是哪一首。
//
// ⚠️ 它自己数调用次数（`f.playCalls`），因为它把默认那个 handler 换掉了。
func playURLBySong(f *streamFixture, expiresIn time.Duration) {
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		f.playCalls.Add(1)
		q := r.URL.Query()
		name := q.Get("bvid") + "-" + q.Get("cid")
		writeStreamJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{"timelength": 1000,
				"dash": dashFor(cdnURL(f.URL, name, expiresIn), int(bili.QualityHigh192), 110836)}})
	})
}

// watchCDN 数某一条地址被下了几次。
//
// 返回的那个计数就是「这一首歌真的被播过」的凭据：预取**只解析地址**，
// 压根不去碰 CDN（这一票不做预下载字节），所以 CDN 被打了 = 真的在放它。
func watchCDN(f *streamFixture, path string) *atomic.Int32 {
	var n atomic.Int32
	f.on(path, func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		f.serveMedia(w, r)
	})
	return &n
}

// ---- 用例 ----

// TestTheNextTrackIsPreparedBeforeTheSwitch 盯这一票的主线。
//
// 两件事：
//
//  1. **下一首的地址在当前这首还在放的时候就备好了**（不是切歌那一刻才取）。
//  2. **真播下一首时不再重新取流**——用的是备好的那一份。
//
// ⚠️ 判据是**取流接口被打了多少次**：备好的那一份被用掉时，接口的次数**停在
// 2**（第一首自己那一次 + 备下一首那一次）；真播时又取了一次的话就是 3。
func TestTheNextTrackIsPreparedBeforeTheSwitch(t *testing.T) {
	f := newStreamFixture(t)
	f.media = shortWav(1.0)
	playURLBySong(f, 2*time.Hour)

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "第一首"), onlineItem("BV2", "2", "第二首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "第一首")

	// 下一首的地址在**第一首还在放**的时候就备好了。
	waitFor(t, 15*time.Second, "第二首的地址备好了", func() bool {
		return f.playCalls.Load() >= 2
	})
	if st := a.Snapshot(); st.Title != "第一首" {
		t.Fatalf("备地址的时候当前这一首已经换了：%q", st.Title)
	}

	// 这一首放完 → 自动切歌 → **不再取流**（用的是备好的那一份）。
	//
	// ⚠️ **等它自然放完**，而不是拖到末尾：自然放完才是自动切歌真正的触发点，
	// 而且它给「预取的结果已经落好」留出了足够的余量（拖到末尾之后的那一拍可能
	// 紧跟着预取的结果落盘，差几毫秒就会偶发）。
	playing(t, a, "第二首")
	if n := f.playCalls.Load(); n != 2 {
		t.Errorf("切歌那一刻又取了一次流（取流接口一共被打了 %d 次，want 2）", n)
	}
	if st := a.Snapshot(); st.Err != "" {
		t.Errorf("播放条上有错：%s", st.Err)
	}
}

// TestAManualSwitchResolvesTheAddressAgain 盯「手动切歌重新取地址」。
//
// 自动切歌用备好的那一份；**用户自己按了下一首**的时候重新取——他要的是
// 「现在」这一首的地址，而备好的那一份是几十秒前算的（报告 B12 的分工）。
//
// ⚠️ 手动切歌会把备好的那一份作废（队列变了，见 queue.go 的 onInvalidate），
// 所以这一次预取是白做了——**多打的那一次请求就是这一条的代价**，而它换来的是
// 「用户按下一首时拿到的一定是新鲜的地址」。
func TestAManualSwitchResolvesTheAddressAgain(t *testing.T) {
	f := newStreamFixture(t)
	f.media = shortWav(1.0)
	playURLBySong(f, 2*time.Hour)

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "第一首"), onlineItem("BV2", "2", "第二首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "第一首")
	waitFor(t, 15*time.Second, "第二首的地址备好了", func() bool {
		return f.playCalls.Load() >= 2
	})

	// 用户自己按了「下一首」。
	a.Next()
	playing(t, a, "第二首")
	if n := f.playCalls.Load(); n != 3 {
		t.Errorf("手动切歌没有重新取流（取流接口一共被打了 %d 次，want 3）", n)
	}
}

// TestReorderingTheQueueThrowsThePreparedAddressAway 盯「队列一改就作废」。
//
// 用户拖了一下队列的顺序 → 下一首变了 → **之前备好的那一份立刻作废**。
//
// ⚠️ 这里的判据是「作废之后会重新备一份」：作废了的话，下一拍就会为新的队列
// 重新备一份（接口的次数从 2 变成 3）；没作废的话，那一份还在，次数停在 2。
//
// ⚠️ **重排之后下一首仍然是同一首**（挪的是顺序，不是内容）——这正是最该验的
// 那种：目标没变，但队列变了，而备好的那一份**照样作废**（队列变了之后，
// 「下一首是谁」这件事要重新算一遍，不能拿旧的答案）。
func TestReorderingTheQueueThrowsThePreparedAddressAway(t *testing.T) {
	f := newStreamFixture(t)
	f.media = shortWav(1.0)
	playURLBySong(f, 2*time.Hour)

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "第一首"), onlineItem("BV2", "2", "第二首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "第一首")
	waitFor(t, 15*time.Second, "第二首的地址备好了", func() bool {
		return f.playCalls.Load() >= 2
	})

	// 把第二首挪到第一首前面：队列的顺序变了。
	if !a.Queue().MoveSongBefore("mv:BV2", "mv:BV1") {
		t.Fatal("挪不动那首歌")
	}
	// 作废了 → 下一拍重新备一份。
	waitFor(t, 15*time.Second, "重排之后重新备了一份", func() bool {
		return f.playCalls.Load() >= 3
	})
}

// TestChangingThePlayModeThrowsThePreparedAddressAway 盯「换模式也作废」。
//
// 四档的「下一首是谁」是四套算法（随机那一档更是从洗牌袋里抽），所以换档
// 之后之前备好的那一份**一定**不能用了。
//
// ⚠️ 这里换的是**顺序**那一档：换到随机那一档的话，洗牌袋是空的（换档时清掉），
// 预取压根答不出下一首是谁（见 `bagPeekLocked`）——那样就量不到「作废」了。
func TestChangingThePlayModeThrowsThePreparedAddressAway(t *testing.T) {
	f := newStreamFixture(t)
	f.media = shortWav(1.0)
	playURLBySong(f, 2*time.Hour)

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "第一首"), onlineItem("BV2", "2", "第二首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "第一首")
	waitFor(t, 15*time.Second, "第二首的地址备好了", func() bool {
		return f.playCalls.Load() >= 2
	})

	// 换档 → 下一首整个变了 → 之前备好的那份作废。
	a.SetPlayMode(app.PlayModeSequence)
	// 作废了 → 下一拍重新备一份。
	waitFor(t, 15*time.Second, "换档之后重新备了一份", func() bool {
		return f.playCalls.Load() >= 3
	})
}

// TestAPrefetchThatFailsFallsBackSilently 盯「预取失败无声退回」。
//
// 三件事：
//
//  1. 备不出来的时候**界面上一句话都不冒**（用户没点任何东西）。
//  2. **不会每 50ms 重试一次**（那是打给 B 站的请求风暴）。
//  3. 真播那一首的时候**按正常路径再取一次**——这一首照放不误。
func TestAPrefetchThatFailsFallsBackSilently(t *testing.T) {
	f := newStreamFixture(t)
	f.media = shortWav(1.0)

	// 备第二首的那一次取流会失败（500），真播它的那一次会成功。
	var failed atomic.Bool
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		f.playCalls.Add(1)
		q := r.URL.Query()
		if q.Get("bvid") == "BV2" && failed.CompareAndSwap(false, true) {
			http.Error(w, "这一首取不到", http.StatusInternalServerError)
			return
		}
		name := q.Get("bvid") + "-" + q.Get("cid")
		writeStreamJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{"timelength": 1000,
				"dash": dashFor(cdnURL(f.URL, name, 2*time.Hour), int(bili.QualityHigh192), 110836)}})
	})

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "第一首"), onlineItem("BV2", "2", "第二首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "第一首")
	waitFor(t, 15*time.Second, "预取失败了一次", func() bool {
		return f.playCalls.Load() >= 2
	})

	// ⚠️ **不重试**：这一拍（50ms 一拍）之后不该再有新的取流请求。
	time.Sleep(600 * time.Millisecond)
	if n := f.playCalls.Load(); n != 2 {
		t.Errorf("预取失败之后还在重试（取流接口一共被打了 %d 次，want 2）", n)
	}
	// 失败是**无声**的：播放条上不该有那句话。
	if st := a.Snapshot(); st.Err != "" {
		t.Errorf("预取失败在播放条上冒了一句：%s", st.Err)
	}

	// 真播那一首的时候按正常路径再取一次 → 这一首照放。
	playing(t, a, "第二首")
	if n := f.playCalls.Load(); n != 3 {
		t.Errorf("真播的时候没有按正常路径再取一次（取流接口一共被打了 %d 次，want 3）", n)
	}
}

// TestAPreparedAddressThatExpiresIsThrownAway 盯「备好的地址快过期了就放弃」。
//
// B 站的地址带一个 deadline，而 `Usable` 留了两分钟提前量（见
// `bili.StreamRef.Usable`）。备到一条**马上就要过期**的地址时，正确做法是
// **放弃它**——真播那一趟重新取一条新的。
//
// ⚠️ 放弃之后**不许立刻再备一次**：那会变成「每 50ms 重建一次失败的管线」。
func TestAPreparedAddressThatExpiresIsThrownAway(t *testing.T) {
	f := newStreamFixture(t)
	f.media = shortWav(1.0)

	// 备第二首的那一次给一条「一分钟后过期」的地址（提前量是 2 分钟，所以它
	// 从拿到那一刻起就已经算过期了）；真播它的那一次给一条新的。
	var calls atomic.Int32
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		f.playCalls.Add(1)
		q := r.URL.Query()
		exp := 2 * time.Hour
		if q.Get("bvid") == "BV2" && calls.Load() == 2 {
			exp = time.Minute // 一分钟后过期 —— 已经越过提前量了
		}
		name := q.Get("bvid") + "-" + q.Get("cid")
		writeStreamJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{"timelength": 1000,
				"dash": dashFor(cdnURL(f.URL, name, exp), int(bili.QualityHigh192), 110836)}})
	})

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "第一首"), onlineItem("BV2", "2", "第二首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "第一首")
	waitFor(t, 15*time.Second, "预取了一次", func() bool {
		return f.playCalls.Load() >= 2
	})

	// 放弃之后不重试。
	time.Sleep(600 * time.Millisecond)
	if n := f.playCalls.Load(); n != 2 {
		t.Errorf("放弃那条地址之后还在重试（取流接口一共被打了 %d 次，want 2）", n)
	}

	// 真播的时候重新取了一条新的。
	playing(t, a, "第二首")
	if n := f.playCalls.Load(); n != 3 {
		t.Errorf("真播的时候没有重新取流（取流接口一共被打了 %d 次，want 3）", n)
	}
}

// TestNothingIsPreparedWhileTheTrackIsPaused 盯「暂停时不预取」。
//
// 用户停着，备好了也白备（那十几秒里他可能换歌、退出），而多打一次请求就是
// 多一分风控面。
//
// ⚠️ **怎么验它不是「碰巧没触发」**：暂停之后队列变了（下一首换成第三首），
// 于是「该备」这个条件成立了——**只有暂停**能挡住它。
func TestNothingIsPreparedWhileTheTrackIsPaused(t *testing.T) {
	f := newStreamFixture(t)
	f.media = shortWav(1.0)
	playURLBySong(f, 2*time.Hour)

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "第一首"), onlineItem("BV2", "2", "第二首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "第一首")
	waitFor(t, 15*time.Second, "第二首的地址备好了", func() bool {
		return f.playCalls.Load() >= 2
	})

	// 暂停。
	a.Toggle()
	// 队列变了 → 下一首换成第三首 → 「该备」这个条件成立了。
	if err := a.EnqueueNext(onlineItem("BV3", "3", "第三首")); err != nil {
		t.Fatal(err)
	}

	// 暂停期间**不该**再备一份。
	time.Sleep(600 * time.Millisecond)
	if n := f.playCalls.Load(); n != 2 {
		t.Errorf("暂停的时候还在备地址（取流接口一共被打了 %d 次，want 2）", n)
	}

	// 按回来 → 这一份就备上了（而且是在这一首还在放的时候备的）。
	a.Toggle()
	waitFor(t, 15*time.Second, "恢复之后备上了第三首", func() bool {
		return f.playCalls.Load() >= 3
	})
	if st := a.Snapshot(); st.Title != "第一首" {
		t.Errorf("备地址的时候当前这一首已经换了：%q", st.Title)
	}
}

// TestThePreparedTrackIsTheOneThatActuallyPlays 盯「随机模式里备的和真播的是
// 同一首」。
//
// 随机那一档的「下一首」是**从洗牌袋里抽**的（#322 / BMG-191），而预取算的
// 和真抽的必须是**同一首**——判据必须和 `Queue.Advance` 同一个来源。
//
// ⚠️ 这一条只量「用掉了备好的那一份」（取流接口的次数停在 2）；「算的和抽的
// 是同一首」那一条由 `TestPeekNextAgreesWithAdvance`（队列内部）钉着。
func TestThePreparedTrackIsTheOneThatActuallyPlays(t *testing.T) {
	f := newStreamFixture(t)
	f.media = shortWav(1.0)
	playURLBySong(f, 2*time.Hour)

	a := newOnlineApp(t, f, t.TempDir())
	a.SetPlayMode(app.PlayModeRandom)
	if err := a.Enqueue(onlineItem("BV1", "1", "第一首"),
		onlineItem("BV2", "2", "第二首"), onlineItem("BV3", "3", "第三首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "第一首")

	// 第一首放完 → 随机抽一首。抽到的那一首放的时候，它的地址**已经备好了**
	// （这一轮洗牌袋是空的，所以第一首放的时候没备——那是袋空的那一首）。
	var second string
	waitFor(t, 15*time.Second, "抽到的那一首放起来了", func() bool {
		st := a.Snapshot()
		if !st.HasTrack || st.Title == "第一首" {
			return false
		}
		second = st.Title
		return true
	})
	// 放第二首的时候，备的是第三首（洗牌袋里的下一首）。
	//
	// ⚠️ **这里数到 3 而不是 2**：随机那一档的第一轮洗牌袋是空的，所以第一首
	// 放的时候**备不了**（袋空答不出下一首是谁，见 `bagPeekLocked`）——于是
	// 第二次取流是**第二首自己**那一次，第三次才是预取。
	waitFor(t, 15*time.Second, "第三首的地址备好了", func() bool {
		return f.playCalls.Load() >= 3
	})

	// 第二首放完 → 抽到的那一首**用掉了备好的那份**（接口的次数不再涨）。
	waitFor(t, 15*time.Second, "第三首放起来了", func() bool {
		st := a.Snapshot()
		return st.HasTrack && st.Title != "第一首" && st.Title != second
	})
	if n := f.playCalls.Load(); n != 3 {
		t.Errorf("随机模式下真播下一首时又取了一次流（取流接口一共被打了 %d 次，want 3）", n)
	}
}

// TestThePrefetchDoesNotDownloadAnything 盯「只解析地址，不预下载字节」。
//
// 我们的 `StreamSource` 会**在盘上落一个临时文件**（`stream_source.go`），
// 预下载字节 = 占磁盘 + 多一条 CDN 连接——那是另一件事，这一票不做。
//
// 判据是 **CDN 被打了多少次**：预取只解析地址，压根不去碰 CDN；CDN 被打了
// = 真的在放那一首。
func TestThePrefetchDoesNotDownloadAnything(t *testing.T) {
	f := newStreamFixture(t)
	f.media = shortWav(1.0)
	playURLBySong(f, 2*time.Hour)
	second := watchCDN(f, "/cdn/BV2-2")

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "第一首"), onlineItem("BV2", "2", "第二首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "第一首")
	waitFor(t, 15*time.Second, "第二首的地址备好了", func() bool {
		return f.playCalls.Load() >= 2
	})

	// 备好了，但**一个字节都还没下**。
	if n := second.Load(); n != 0 {
		t.Errorf("预取把字节也下了（CDN 被打了 %d 次，want 0）", n)
	}

	// 真播它的时候才去下。
	playing(t, a, "第二首")
	waitFor(t, 15*time.Second, "第二首真的开始出声", func() bool {
		return second.Load() > 0
	})
}

// TestTheAddressIsPreparedWhileTheTrackStillHasTimeLeft 盯「提前量真的挂在
// 「还剩多久」上」。
//
// 这一条是这一票**最核心**的验收：「取流请求在还剩 N 秒时就发出，而不是切歌
// 那一刻才发」。
//
// ⚠️ **歌必须比提前量长**：这一首要长到 15 秒之后才放完，否则「还剩多久」
// 这个条件从一开始就成立，量不出「提前量」是多少。
func TestTheAddressIsPreparedWhileTheTrackStillHasTimeLeft(t *testing.T) {
	f := newStreamFixture(t)
	f.media = shortWav(18.0)
	playURLBySong(f, 2*time.Hour)

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "第一首"), onlineItem("BV2", "2", "第二首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "第一首")

	// 刚开播：这一首还剩 18 秒 > 15 秒 → **不该**已经备好了。
	time.Sleep(300 * time.Millisecond)
	if n := f.playCalls.Load(); n != 1 {
		t.Fatalf("一开播就备了（取流接口被打了 %d 次，want 1）——提前量没生效", n)
	}

	// 盯着取流接口，看它第一次被第二次打是在**还剩多久**的时候。
	var at atomic.Value
	waitFor(t, 20*time.Second, "下一首的地址备好了", func() bool {
		if f.playCalls.Load() < 2 {
			return false
		}
		st := a.Snapshot()
		at.Store(st.Duration - st.Position)
		return true
	})
	left := at.Load().(time.Duration)
	if left > 16*time.Second {
		t.Errorf("备得太早了：那一刻还剩 %v（提前量是 15 秒）", left)
	}
	if left < 5*time.Second {
		t.Errorf("备得太晚了：那一刻还剩 %v（提前量是 15 秒）", left)
	}
}

// TestAMusicLibraryEntryIsPreparedToo 盯**音乐库那一路**也预取得到。
//
// 投稿那一路（bvid/cid）和音乐库那一路（sid）是**两条不同的取流接口**
// （见 `bili.streamFor`），而预取那一层是两边都走的——所以这一条要单独验：
// 音乐库那一路的 sid 是**接口给的**（`bili.refFromSongURL`），预取拿它去比
// 也比得了。
func TestAMusicLibraryEntryIsPreparedToo(t *testing.T) {
	f := newStreamFixture(t)
	f.media = shortWav(1.0)

	// 取流接口回**要的那一首**的 sid（夹具默认永远回 1293494，那样两首会认成
	// 同一首 —— 而预取要比身份，比错了就不备了）。
	f.on("/audio/music-service-c/url", func(w http.ResponseWriter, r *http.Request) {
		f.songCalls.Add(1)
		sid := r.URL.Query().Get("songid")
		writeStreamJSON(w, map[string]any{"code": 0, "msg": "success", "ttl": 1,
			"data": map[string]any{
				"sid":     mustAtoi(t, sid),
				"type":    2,
				"timeout": 10800,
				"cdns":    []string{cdnURL(f.URL, "song-"+sid, 2*time.Hour)},
				"qualities": []any{
					map[string]any{"type": 3, "require": 1},
					map[string]any{"type": 2, "require": 0},
					map[string]any{"type": 1, "require": 0},
					map[string]any{"type": 0, "require": 0},
				},
			}})
	})
	f.on("/audio/music-service-c/web/song/info", func(w http.ResponseWriter, r *http.Request) {
		f.songInfoCalls.Add(1)
		sid := r.URL.Query().Get("sid")
		writeStreamJSON(w, map[string]any{"code": 0, "msg": "success", "ttl": 1,
			"data": map[string]any{
				"id": mustAtoi(t, sid), "uid": 900, "uname": "某某音乐台",
				"author": "夜灯", "title": "歌" + sid, "cover": "", "duration": 1,
				"bvid": "", "cid": 0, "collectIds": []int{},
			}})
	})

	a := newOnlineApp(t, f, t.TempDir())
	song := func(sid, title string) app.Item {
		return app.Item{Source: "online", Type: "audio", Sid: sid, Title: title}
	}
	if err := a.Enqueue(song("111", "第一首"), song("222", "第二首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "第一首")

	waitFor(t, 15*time.Second, "第二首的地址备好了", func() bool {
		return f.songCalls.Load() >= 2
	})
	if st := a.Snapshot(); st.Title != "第一首" {
		t.Fatalf("备地址的时候当前这一首已经换了：%q", st.Title)
	}
	// 真播第二首时不再取流。
	playing(t, a, "第二首")
	if n := f.songCalls.Load(); n != 2 {
		t.Errorf("音乐库那一路真播时又取了一次流（取流接口一共被打了 %d 次，want 2）", n)
	}
}

// TestAStaleAddressIsNeverPlayed 盯「队列改了之后，陈旧的那份地址**不会被播
// 出去**」。
//
// 这是「作废」那一条**真正要保证的东西**：作废不是「把那份地址扔掉」这么一句
// 话，而是**下一首换成谁，就放谁**——陈旧的那一份（第二首的地址）一个字节都
// 不许下。
//
// 判据是 **CDN 被打了哪一条**：预取只解析地址、不碰 CDN，所以 CDN 被打了 =
// 真的在放它。
func TestAStaleAddressIsNeverPlayed(t *testing.T) {
	f := newStreamFixture(t)
	f.media = shortWav(3.0)
	playURLBySong(f, 2*time.Hour)
	second := watchCDN(f, "/cdn/BV2-2")
	third := watchCDN(f, "/cdn/BV3-3")

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "第一首"), onlineItem("BV2", "2", "第二首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "第一首")
	waitFor(t, 15*time.Second, "第二首的地址备好了", func() bool {
		return f.playCalls.Load() >= 2
	})

	// 队列变了：下一首从「第二首」变成「第三首」。
	if err := a.EnqueueNext(onlineItem("BV3", "3", "第三首")); err != nil {
		t.Fatal(err)
	}

	// 这一首放完 → 自动切歌 → 放的一定是**第三首**。
	playing(t, a, "第三首")
	if n := second.Load(); n != 0 {
		t.Errorf("陈旧的那份地址被播出去了（第二首的 CDN 被打了 %d 次，want 0）", n)
	}
	if n := third.Load(); n == 0 {
		t.Errorf("第三首压根没被播（CDN 一次都没被碰）")
	}
}

// mustAtoi 解析一个十进制数（夹具里回 sid 要用）。
func mustAtoi(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("解析 %q 失败：%v", s, err)
	}
	return n
}
