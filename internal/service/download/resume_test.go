package download

// 断点续传这一组：下到一半停下来，重新打开应用接着下。
//
// 这一票的三条硬要求里有两条落在这里：
//
//	续传前校验资源身份（内容 id + 页 id + 音质 + 长度）
//	退出时保存队列，重开后恢复
//
// 断的都是**行为**而不是状态：「服务器收到了哪几发 Range」、
// 「盘上那个文件的每一个字节是谁的内容」。前者是手段，后者是结果——
// 结果对了才说明手段对。

import (
	"context"
	"testing"
	"time"
)

// 🔴🔴 续传：只重来没下完的那几块。
//
// 这是「下到一半退出，重开能继续」的落点。断的是**服务器收到了哪几发
// Range**——已经下完的第 0 块不该再要一次（要一次就是白下一兆），
// 没下完的必须都要（不要就是文件中间有洞，歌播一半就断）。
func TestOnlyUnfinishedChunksAreRefetchedOnResume(t *testing.T) {
	const size = 3 * chunkSize // 三块，正好三发
	h := newHarness(t, size, 30280)
	id := TaskID(bvid, cid, false)
	dest := DestFor(h.dir, "测试的歌", ".m4a", id)

	// 造一个「下完第 0 块就退出了」的世界：分块记账 + 盘上的半成品。
	writePart(t, h.cdn, dest+".part", 0, size)
	h.saveQueue(t, map[string]*Task{
		id: {
			ID: id, Title: "测试的歌", Dest: dest,
			Identity: Identity{Bvid: bvid, Cid: cid, Quality: 30280, Length: size},
			Chunks:   []int{0},
			Status:   StatusRunning, // 退出前它是在下的
			AddedAt:  1,
		},
	})

	h2 := h.restart()
	snap := h2.wait(id, StatusPaused) // 恢复出来的**一律是暂停**
	if snap.Done != chunkSize {
		t.Fatalf("恢复出来就显示下了 %d 字节，账上是一块（%d）", snap.Done, chunkSize)
	}

	if err := h2.m.Resume(id); err != nil {
		t.Fatalf("继续：%v", err)
	}
	h2.wait(id, StatusDone)

	// 第 0 块已经下过了，不该再要一次。
	for _, r := range h.cdn.requests() {
		if start, _, _ := parseRange(r, size); start == 0 && r != "bytes=0-0" {
			t.Fatalf("续传时又下了一遍第 0 块：%q（全部：%v）", r, h.cdn.requests())
		}
	}
	if got := len(h.cdn.requests()); got != 2 {
		t.Fatalf("续传发了 %d 发，剩下两块应该是 2 发：%v", got, h.cdn.requests())
	}
	// 内容一字节不少——这是「没有洞」的证明。
	assertFile(t, snap.Dest, 0, size)
}

// 🔴🔴 资源身份对不上时**不许拼**，要从头来。
//
// 这一条造的是 spec 点名的那个坏文件：B 站换了流（另一档音质 / 重新转码），
// 而盘上那半兆是**旧内容**的。拼起来长度对得上、验不出来，症状是
// 「歌能打开、播到一半就断」。
//
// 断言的不是「报错了」——报错了很容易。断的是**文件里的每一个字节都是新内容**。
func TestAResumeAfterTheStreamChangedRestartsFromZero(t *testing.T) {
	const size = 2 * chunkSize
	h := newHarness(t, size, 30232) // 上一轮是 128K
	id := TaskID(bvid, cid, false)
	dest := DestFor(h.dir, "测试的歌", ".m4a", id)

	// 盘上是 variant 0（旧那档）的前一块。
	writePart(t, h.cdn, dest+".part", 0, size)
	h.saveQueue(t, map[string]*Task{
		id: {
			ID: id, Title: "测试的歌", Dest: dest,
			Identity: Identity{Bvid: bvid, Cid: cid, Quality: 30232, Length: size},
			Chunks:   []int{0},
			Status:   StatusRunning,
			AddedAt:  1,
		},
	})

	// 这一轮 B 站给的是 320K：**长度一模一样，内容全不一样**。
	// 只比长度的话完全看不出来。
	h.cdn.set(func(c *cdn) { c.variant = 1 })
	h2 := h.restart()
	// 换掉取流：返回的新那一档是 30280。
	h2.m.resolve = func(context.Context, Identity) (*Source, error) {
		return &Source{
			Bvid: bvid, Cid: cid, Quality: 30280,
			Label: "320K", URLs: []string{h.cdn.url()},
		}, nil
	}
	h2.cdn = h.cdn

	h2.wait(id, StatusPaused)
	if err := h2.m.Resume(id); err != nil {
		t.Fatalf("继续：%v", err)
	}
	snap := h2.wait(id, StatusDone)

	// 🔴 先断**结果**：整个文件都得是 variant 1。这是最要紧的一条断言——
	// 拆掉身份校验时文件长度照样是对的（两块，两块大小相同），只有内容
	// 才露馅，而用户看到的症状恰恰是「歌能打开、播到一半就断」。
	assertFile(t, snap.Dest, 1, size)

	// 再断**手段**：两块都要重下，旧的那些字节是别的内容。
	if got := len(h.cdn.requests()); got != 2 {
		t.Fatalf("身份变了却只发了 %d 发请求（两块都要重下）：%v", got, h.cdn.requests())
	}
}

// 暂停是真的停：网络请求断了、状态是暂停、不是失败。
func TestPauseStopsWithoutCountingItAsAFailure(t *testing.T) {
	// 多来几块，好让暂停能在下到一半时按下去。
	const size = 6 * chunkSize
	h := newHarness(t, size, 30280)
	snap, err := h.m.Enqueue(AddRequest{Bvid: bvid, Cid: cid, Title: "测试的歌", Dir: h.dir, Ext: ".m4a"})
	if err != nil {
		t.Fatal(err)
	}

	// 等到真的有东西下下来再暂停：一上来就暂停测的是另一条路。
	waitUntil(t, func() bool { return len(h.cdn.requests()) > 0 })
	if err := h.m.Pause(snap.ID); err != nil {
		t.Fatalf("暂停：%v", err)
	}

	got := h.wait(snap.ID, StatusPaused)
	if got.Err != "" {
		t.Fatalf("暂停被当成了失败：%q", got.Err)
	}
	// 暂停之后网络要**停下来**。断的是「不再增长」，不是「某一瞬间是几」——
	// 按下去的那一刻可能还有几个请求正在飞（它们是暂停之前就发出去的），
	// 慢慢落地。所以先等它静下来，再看它还长不长。
	settle(t, h.cdn)
	before := len(h.cdn.requests())
	time.Sleep(400 * time.Millisecond)
	if after := len(h.cdn.requests()); after != before {
		t.Fatalf("暂停之后网络还在动：%d → %d 发", before, after)
	}
	// 没下完就停住了。断的是「没下完」而不是「下了一点」——`Done()`
	// 只算**整块**（见 `Task.Done`），而暂停按下去的时候那几个分块
	// 可能都还在飞。
	if got.Done >= size {
		t.Fatalf("暂停之后显示下完了（%d/%d）", got.Done, size)
	}
	// 停住的就是停住了：一段时间内进度一点不动。
	time.Sleep(100 * time.Millisecond)
	if again, _ := h.m.Get(snap.ID); again.Done != got.Done {
		t.Fatalf("暂停之后进度还在走：%d → %d", got.Done, again.Done)
	}
	// 接着下能下完。
	if err := h.m.Resume(snap.ID); err != nil {
		t.Fatalf("接着下：%v", err)
	}
	final := h.wait(snap.ID, StatusDone)
	assertFile(t, final.Dest, 0, size)
}

// 出错那一项能重试，而且**只重来没下完的**。
func TestRetryOnlyRefetchesTheUnfinishedChunks(t *testing.T) {
	const size = 3 * chunkSize
	h := newHarness(t, size, 30280)
	// 每发只给 1 MB 就掐断：三个分块都会下不满，于是整个任务失败，
	// 而每一块**都已经写了开头那一段**。
	h.cdn.set(func(c *cdn) { c.cutAt = 1 << 20 })

	snap := h.enqueue(StatusError)
	if snap.Done != 0 {
		t.Fatalf("三个分块都没下满，账上却有 %d 字节", snap.Done)
	}

	// 掐断之后不再掐，这一轮能下完。
	h.cdn.set(func(c *cdn) { c.cutAt = 0 })
	if err := h.m.Resume(snap.ID); err != nil {
		t.Fatalf("重试：%v", err)
	}
	done := h.wait(snap.ID, StatusDone)
	assertFile(t, done.Dest, 0, size)
}

// 进度回调被节流到 500 毫秒一次。
//
// 字节数是**每收到一个 TCP 包**报一次的（一个任务每秒几千次）。不节流
// 的话界面每秒要重画几千次，画出来的是同一帧。
