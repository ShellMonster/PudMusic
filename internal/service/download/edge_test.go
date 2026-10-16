package download

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 这一组是第一轮评审挖出来的四个洞。每一个都是「错了不报错」那一类：
// 界面上看着像成功了，盘上的东西是坏的、或者什么都没发生。

// 🔴 探长度那一发，服务器不肯说总长时**不能**拿 Content-Length 顶上。
//
// RFC 7233 允许 `Content-Range: bytes 0-0/*`（「实例长度未知」）。而
// `probeOne` 发的是 `Range: bytes=0-0`，所以那一发的 `Content-Length`
// 说的是**「这一个字节」**，不是整首歌。
//
// 拿它当总长会发生什么：总长=1 → 只规划出一个分块 `bytes=0-0` → 下载
// 「成功」→ 落盘一个 1 字节的 `.m4s` → 状态报「已完成」。一首 30 MB 的
// 歌变成 1 字节，而歌名、大小、状态全都看着像那么回事。
//
// 评审第一轮就是这么发现它的：它的探针打出来的日志是
// `源有 31457280 字节 → 引擎报告总长 1、落盘 1 字节、状态 "done"`。
func TestAServerThatWillNotSayTheLengthIsRefused(t *testing.T) {
	const size = 3 * chunkSize // 一首 30 MB 的歌
	h := newHarness(t, size, 30280)
	h.cdn.set(func(c *cdn) { c.starTotal = true })

	snap := h.enqueue(StatusError)
	if !strings.Contains(snap.Err, "不肯说") {
		t.Fatalf("报的不是「问不出长度」，是 %q", snap.Err)
	}

	// 🔴 断的是**盘上有什么**，不是「报没报错」——报错了很容易，
	// 难的是保证没有一首歌被悄悄造出来。
	entries, err := os.ReadDir(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != QueueFileName {
			t.Fatalf("服务器不肯说长度，目录里却多了 %q", e.Name())
		}
	}
	if snap.Total != 0 {
		t.Fatalf("报出去的总长是 %d（应该是「没有」）", snap.Total)
	}
	// 一个分块请求都不该发出去：总长都没有，规划不出分块来。
	if got := len(h.cdn.requests()); got != 0 {
		t.Fatalf("问不出长度却还是发了 %d 发分块请求：%v", got, h.cdn.requests())
	}
}

// 正常那一条不受影响：肯说长度时总长和内容都对。
func TestAServerThatSaysTheLengthIsUsedAsIs(t *testing.T) {
	const size = 3 * chunkSize
	h := newHarness(t, size, 30280)
	snap := h.enqueue(StatusDone)
	if snap.Total != size {
		t.Fatalf("总长是 %d，期望 %d", snap.Total, size)
	}
	assertFile(t, snap.Dest, 0, size)
}

// 🔴 已经下完的歌，重开之后还是「已完成」。
//
// 评审第一轮的第二个洞：`Restore` 把恢复出来的任务**一律**改成暂停，于是
// 记录页上每一行都写着「暂停」，而那一行上唯一能按的「继续」会把已经下完
// 的整首歌重新下一遍（`prepare` 看到 `.part` 已经不在了 → 清空分块记账）。
//
// 它探出来的日志是 `重开后：status=paused 已记进度=20971520/20971520`，
// 跟着 `点「继续」之后又发了 2 发分块请求（一共 20971520 字节）`。
//
// 在飞的（running / queued）仍然要变成暂停——那个在飞的 HTTP 请求重启之后
// 已经不存在了，记成 running 会让界面上永远转圈。
func TestAFinishedSongIsStillFinishedAfterARestart(t *testing.T) {
	const size = 2 * chunkSize
	h := newHarness(t, size, 30280)
	snap, err := h.m.Enqueue(AddRequest{Bvid: bvid, Cid: cid, Title: "下完的歌", Dir: h.dir})
	if err != nil {
		t.Fatal(err)
	}
	h.wait(snap.ID, StatusDone)

	h2 := h.reopen() // 真实的退出流程：退出前存一次，再读回来
	got, ok := h2.m.Get(snap.ID)
	if !ok {
		t.Fatal("重开后队列里没有这一项")
	}
	if got.Status != StatusDone {
		t.Fatalf("已经下完的歌，重开之后是 %q —— 记录页上写着「暂停」", got.Status)
	}
	if got.Done != size {
		t.Fatalf("已记进度 %d/%d", got.Done, size)
	}

	// 🔴 断的是「点继续之后会怎样」，不是「状态字段是什么」——
	// 状态对了但 `Resume` 真的重下一遍，跟状态错了是同一种用户可见的病。
	if err := h2.m.Resume(snap.ID); err != nil {
		t.Fatalf("对一首下完的歌按「继续」：%v", err)
	}
	time.Sleep(300 * time.Millisecond) // 给后台那条线一点时间「要是它会重下」
	if got := len(h2.cdn.requests()); got != 0 {
		t.Fatalf("对一首已经下完的歌按「继续」，又发了 %d 发请求", got)
	}
	if after, _ := h2.m.Get(snap.ID); after.Status != StatusDone {
		t.Fatalf("按了继续之后状态变成了 %q", after.Status)
	}
	// 下完的那首歌还在盘上。
	if _, err := os.Stat(got.Dest); err != nil {
		t.Fatalf("下完的歌不见了：%v", err)
	}
}

// 在飞的那些**仍然**要变成暂停：那个请求重启之后已经不存在了。
func TestStillInFlightTasksBecomePausedOnRestart(t *testing.T) {
	const size = 4 * chunkSize
	h := newHarness(t, size, 30280)
	// 每发只给 1 MB 就掐断：任务会失败，但仍在队列里、仍有 `.part`。
	h.cdn.set(func(c *cdn) { c.cutAt = 1 << 20 })
	id := TaskID(bvid, cid)
	if _, err := h.m.Enqueue(AddRequest{Bvid: bvid, Cid: cid, Title: "半首", Dir: h.dir}); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, h.m, id, StatusError)

	h2 := h.reopen()
	got, ok := h2.m.Get(id)
	if !ok {
		t.Fatal("重开后队列里没有这一项")
	}
	// 出错了就是出错了，别顺手改成「暂停」——那是两件事：
	// 暂停是用户干的，出错是网络干的，而记录页上要给的话术不一样。
	if got.Status != StatusError {
		t.Fatalf("出错的歌重开之后是 %q", got.Status)
	}
}

// 🔴 落盘名在真正落盘的那条路上也要守住 255 字节。
//
// `FileName` 自己的注释写着「255 字节是含末尾那一段的预算」，而 `DestFor`
// 当时是 `FileName(title, "")` 先把**词干**截到 255 再在外面拼扩展名：
// 84 个汉字（252 字节）+ `.m4a` = 256 字节——词干自己没超，整个名字超了。
// 同名避让再加 ` (1)`（4 字节），`.part` 再加 5。
//
// 评审第一轮实测：84 个汉字 → 256 字节，86 个 → 259，300 个 → 259。
//
// ⚠️ 这在 macOS 上**下不下来也不会报错**（APFS 的上限实测是 255 个**字符**
// 不是字节，87 个汉字建得出来）。但那个不变量本身没守住，所以在 Windows /
// 同步盘 / 别的引擎上是会炸的——而且炸点在下载快结束的时候。
func TestTheNameThatActuallyLandsOnDiskIsWithin255Bytes(t *testing.T) {
	dir := t.TempDir()
	exts := []string{"", ".m4a", ".flac"}
	for _, n := range []int{1, 84, 85, 86, 300} {
		for _, ext := range exts {
			got := filepath.Base(DestFor(dir, strings.Repeat("歌", n), ext, "BV1|c1"))
			if len(got) > maxNameBytes {
				t.Fatalf("%d 个汉字 + %q → 落盘名 %d 字节：%q", n, ext, len(got), got)
			}
		}
	}
	// 同名避让那一条路也要守住：先占住 `歌.m4a`。
	d1 := DestFor(dir, strings.Repeat("歌", 300), ".flac", "BV1|c1")
	if err := os.WriteFile(d1, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	d2 := DestFor(dir, strings.Repeat("歌", 300), ".flac", "BV2|c2")
	if d1 == d2 {
		t.Fatalf("同名避让没起作用：%s", d2)
	}
	if base := filepath.Base(d2); len(base) > maxNameBytes {
		t.Fatalf("同名避让之后 %d 字节：%q", len(base), base)
	}
	if want := " (1).flac"; !strings.HasSuffix(d2, want) {
		t.Fatalf("第二首叫 %q，期望以 %q 结尾", filepath.Base(d2), want)
	}
}

// 真下一首（不是只调 `DestFor`）：盘上那个名字也得守住。
func TestALongTitleLandsOnDiskWithinTheLimit(t *testing.T) {
	h := newHarness(t, 1024, 30280)
	snap, err := h.m.Enqueue(AddRequest{
		Bvid: bvid, Cid: cid,
		Title: strings.Repeat("歌", 300), Dir: h.dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := h.wait(snap.ID, StatusDone)
	if n := len(filepath.Base(got.Dest)); n > maxNameBytes {
		t.Fatalf("落盘名 %d 字节：%q", n, filepath.Base(got.Dest))
	}
	if _, err := os.Stat(got.Dest); err != nil {
		t.Fatalf("按这个名字建不出来：%v", err)
	}
}

// 🔴 换音质时扩展名变了，旧名字下的半成品不能变成孤儿。
//
// `prepare` 会按这一轮取流地址上的后缀重算 `Dest`，而 `Remove` / `Clear`
// 只删当前 `Dest` 那一个。于是上一轮的 `换档的歌.m4a.part` 就永远留在用户的
// 下载目录里——几百 MB，界面上再没有任何东西指向它。
//
// 评审第一轮的实测：下完目录里是 `[download-queue.json 换档的歌.flac
// 换档的歌.m4a.part]`，删掉队列项之后 `.part` 还在。
func TestAnOrphanedPartIsRemovedWhenTheExtensionChanges(t *testing.T) {
	const size = 2 * chunkSize
	h := newHarness(t, size, 30232) // 上一轮：128K，落盘名 .m4a
	id := TaskID(bvid, cid)
	oldDest := DestFor(h.dir, "换档的歌", ".m4a", id)
	writePart(t, h.cdn, partPath(oldDest), 0, size)
	h.saveQueue(t, map[string]*Task{
		id: {
			ID: id, Title: "换档的歌", Dest: oldDest, Ext: ".m4a",
			Identity: Identity{Bvid: bvid, Cid: cid, Quality: 30232, Length: size},
			Chunks:   []int{0}, Status: StatusRunning, AddedAt: 1,
		},
	})

	// 这一轮 B 站给的是无损：URL 的后缀是 `.flac`，身份也对不上
	// （质量变了），所以分块记账被清空 → 走到重算 `Dest` 那一条路。
	h.cdn.set(func(c *cdn) { c.variant = 1 })
	flacURL := h.cdn.srv.URL + "/audio.flac?upsig=x&deadline=9999999999"
	h2 := h.restart()
	h2.m.resolve = func(context.Context, Identity) (*Source, error) {
		return &Source{Bvid: bvid, Cid: cid, Quality: 30280, Ext: ".flac", URLs: []string{flacURL}}, nil
	}

	h2.wait(id, StatusPaused)
	if err := h2.m.Resume(id); err != nil {
		t.Fatal(err)
	}
	snap := h2.wait(id, StatusDone)
	if !strings.HasSuffix(snap.Dest, ".flac") {
		t.Fatalf("这一轮应该落在 .flac，实际 %q", snap.Dest)
	}
	if _, err := os.Stat(partPath(oldDest)); !os.IsNotExist(err) {
		t.Fatalf("旧扩展名的半成品变成了孤儿：%s", partPath(oldDest))
	}
	// 删掉队列项之后目录里一个 `.part` 都不该有。
	if err := h2.m.Remove(id); err != nil {
		t.Fatal(err)
	}
	for _, n := range listDir(t, h.dir) {
		if strings.HasSuffix(n, ".part") {
			t.Fatalf("删掉队列项之后还有半成品：%s", n)
		}
	}
}

// 🔴🔴 两首同名歌不能共用一个落盘路径。
//
// 评审第二轮的 critical。原来的 `DestFor` 只对着**盘**做避让，看不见队列：
//
//	① 首（30MB）出错退出，盘上留着 同名.m4a.part
//	② 第二首（40MB）同名入队 → DestFor stat 同名.m4a（不存在！）→ 也拿到
//	   同名.m4a → 下完落在 同名.m4a，41943040 字节
//	③ 回头重试第一首 → 它的 Dest 还是 同名.m4a → os.Rename 把 31457280
//	   字节盖上去（POSIX 的 Rename 在目标存在时直接盖）
//	④ 第二首那条记录还写着「已完成 41943040/41943040」，而那个文件现在
//	   装的是别的歌。目录里只剩一个文件，界面上显示一切正常。
//
// 三道防线各自挡一部分：
//
//	入队/开下时  `destFor` 避开了队列里别的任务认领的名字，也避开了盘上的
//	            `.part`（别人正在往那儿写的东西）
//	落盘时      `destToLand` 再看一眼：那个位置上已经有东西了就避让。
//	            这是最后一道——`Dest` 是入队那一刻定的，而那一刻到下载完
//	            之间世界会变
//
// 断的是「第二个文件还在、内容还是它自己的」，不是「报没报错」。
func TestTwoSongsWithTheSameNameNeverShareALandingPath(t *testing.T) {
	const sizeA, sizeB = 3 * chunkSize, 4 * chunkSize // 30 MB / 40 MB
	cdnA := newCDN(t, sizeA)
	cdnB := newCDN(t, sizeB) // 第二首：更大的另一段内容
	dir := t.TempDir()

	// 一台假 B 站，按 cid 分流到两台假 CDN。
	resolver := func(_ context.Context, want Identity) (*Source, error) {
		if want.Cid == "c2" {
			return &Source{Bvid: bvid, Cid: "c2", Quality: 30280, Label: "320K", URLs: []string{cdnB.url()}}, nil
		}
		return &Source{Bvid: bvid, Cid: "c1", Quality: 30232, Label: "128K", URLs: []string{cdnA.url()}}, nil
	}
	m, err := New(Options{Client: http.DefaultClient, Resolve: resolver}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close("") })

	// ① 第一首：下了一块就退出，盘上留着它的 .part，队列里记着分块 0。
	idA := TaskID(bvid, "c1")
	destA := DestFor(dir, "同名", ".m4a", idA)
	writePart(t, cdnA, partPath(destA), 0, sizeA)
	seedQueue(t, dir, map[string]*Task{
		idA: {
			ID: idA, Title: "同名", Dest: destA, Ext: ".m4a",
			Identity: Identity{Bvid: bvid, Cid: "c1", Quality: 30232, Length: sizeA},
			Chunks:   []int{0}, Status: StatusRunning, AddedAt: 1,
		},
	})
	if err := m.Restore(filepath.Join(dir, QueueFileName)); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.Get(idA); got.Done != chunkSize {
		t.Fatalf("第一首恢复出来的进度是 %d 字节", got.Done)
	}

	// ② 第二首：同名，40 MB。**它入队的那一刻，第一首还占着 `同名.m4a`**。
	snapB, err := m.Enqueue(AddRequest{Bvid: bvid, Cid: "c2", Title: "同名", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	gotB := waitStatus(t, m, snapB.ID, StatusDone)
	if gotB.Dest == destA {
		t.Fatalf("第二首和第一首拿到了同一个路径：%s（入队时没避开队列里别人认领的名字）", gotB.Dest)
	}
	if n, _ := fileSize(gotB.Dest); n != sizeB {
		t.Fatalf("第二首落盘 %d 字节，期望 %d", n, sizeB)
	}

	// ③ 回头重试第一首（界面上就是「继续 / 重试」）。
	if err := m.Resume(idA); err != nil {
		t.Fatalf("重试第一首：%v", err)
	}
	gotA := waitStatus(t, m, idA, StatusDone)

	// 🔴 断的是「第二首那个文件还在、内容还是它自己的」。
	if gotA.Dest == gotB.Dest {
		t.Fatalf("重试之后两首还是同一个路径：%s", gotA.Dest)
	}
	if n, err := fileSize(gotB.Dest); err != nil || n != sizeB {
		t.Fatalf("第二首下完的那首现在是 %d 字节（%v），期望 %d —— 被静默覆盖了", n, err, sizeB)
	}
	assertFile(t, gotB.Dest, 0, sizeB)
	if n, _ := fileSize(gotA.Dest); n != sizeA {
		t.Fatalf("第一首落盘 %d 字节，期望 %d", n, sizeA)
	}
	assertFile(t, gotA.Dest, 0, sizeA)
}

// 🔴 两项**都还没开始下**的时候，也不能认领同一个名字。
//
// 上面那条是靠「第一首的 `.part` 已经在盘上」挡住的（`destFor` 会 stat
// `partPath`）。但队列里认领的名字和盘上有没有文件是两件事：刚入队、后台
// 还没轮到它的那一刻，盘上什么都没有。这时候第二首要是也拿到同一个名字，
// 就等着第一首落盘时 `os.Rename` 把它盖掉。
//
// 这里让两首都**立刻失败**（取流直接报错），于是它们各自认领了名字却什么
// 都没下——正是那个窗口，而且不依赖任何时序。
func TestTwoQueuedTasksNeverClaimTheSameLandingPath(t *testing.T) {
	m, err := New(Options{
		Client: http.DefaultClient,
		Resolve: func(context.Context, Identity) (*Source, error) {
			return nil, errors.New("取流失败，这一首什么都没下")
		},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close("") })

	dir := t.TempDir()
	a, err := m.Enqueue(AddRequest{Bvid: bvid, Cid: "c1", Title: "同名", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, a.ID, StatusError)

	b, err := m.Enqueue(AddRequest{Bvid: bvid, Cid: "c2", Title: "同名", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, b.ID, StatusError)

	if a.Dest == b.Dest {
		t.Fatalf("两首都没下，盘上什么都没有，却认领了同一个名字：%s", a.Dest)
	}
	// 挑名字的纯逻辑也是同一条不变量。
	claimed := map[string]bool{a.Dest: true}
	if got := destFor(dir, "同名", ".m4a", "BV3|c3", claimed); got == a.Dest {
		t.Fatalf("避让没有躲开别人认领的名字：%s", got)
	}
}

// 落盘的那一刻，正式名的位置上多出一个**不是我们下出来的**文件时：
// 要避让，不能盖掉。
//
// 这一条专门盯 `destToLand` 那道防线——入队时那个位置是空的（所以
// `destFor` 没有避开它），文件是下载中途才出现的。
func TestLandingNeverOverwritesAFileThatIsNotOurs(t *testing.T) {
	// 三块，好让「下载途中」这个窗口宽到能往里插一个文件。
	const size = 3 * chunkSize
	h := newHarness(t, size, 30280)
	snap, err := h.m.Enqueue(AddRequest{Bvid: bvid, Cid: cid, Title: "歌", Dir: h.dir})
	if err != nil {
		t.Fatal(err)
	}
	squatter := filepath.Join(h.dir, "歌.m4s")

	// 等到 `.part` 建起来（`prepare` 建完它就开始下），再抢占正式名。
	waitUntil(t, func() bool { return exists(partPath(squatter)) })
	if err := os.WriteFile(squatter, []byte("这是别人放在这儿的文件"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := h.wait(snap.ID, StatusDone)
	if got.Dest == squatter {
		t.Fatalf("把一个不是我们的文件盖掉了：%s", got.Dest)
	}
	b, err := os.ReadFile(squatter)
	if err != nil {
		t.Fatalf("那个文件不见了：%v", err)
	}
	if string(b) != "这是别人放在这儿的文件" {
		t.Fatalf("那个文件的内容被改了：%q", b)
	}
	assertFile(t, got.Dest, 0, size)
}

// 🔴 `.part` 也要算进 255 的预算。
//
// 评审第二轮实测（这台 macOS 上真实上限是 255 个**字符**）：
//
//	251 个 ASCII 字符的歌名 → 落盘名 255 字符、`.part` 260 字符
//	                       → status=error，err=「建 ….part：file name too long」
//
// 也就是说**这一首一个字节都下不了**，而且断的是「落不了盘」不是「下错了」，
// 用户看到的是一句他完全没料到的技术话。
//
// 这一条**真的去建那个文件**——评审那个洞就是在这里炸的，只量字符串长度
// 会漏掉「长度对但建不出来」。
func TestThePartNameAlsoStaysWithinTheLimit(t *testing.T) {
	dir := t.TempDir()
	check := func(title, ext string) {
		t.Helper()
		dest := DestFor(dir, title, ext, "BV1|c1")
		part := partPath(dest)
		// 量的是**文件名本身**，不是整条路径——目录名不占文件名的预算。
		if n := len(filepath.Base(part)); n > maxNameBytes {
			t.Fatalf("落盘名 %d 字节、.part %d 字节（上限 %d）：%q",
				len(filepath.Base(dest)), n, maxNameBytes, filepath.Base(part))
		}
		f, err := os.Create(part)
		if err != nil {
			t.Fatalf("建不出 %q（%d 字节）：%v", filepath.Base(part), len(part), err)
		}
		f.Close()
		os.Remove(part)
	}
	for _, n := range []int{200, 245, 250, 251, 255, 300, 1000} {
		check(strings.Repeat("a", n), ".m4a") // ASCII：一个字符一个字节
	}
	for _, n := range []int{84, 85, 100, 300} {
		check(strings.Repeat("歌", n), ".flac") // 汉字：一个字符三个字节
	}
}

// 端到端：长歌名要真的下得完，而不是「建不出 .part」。
func TestALongTitleActuallyDownloadsEndToEnd(t *testing.T) {
	h := newHarness(t, 1024, 30280)
	snap, err := h.m.Enqueue(AddRequest{
		Bvid: bvid, Cid: cid,
		Title: strings.Repeat("a", 251), Dir: h.dir, Ext: ".m4a",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := h.wait(snap.ID, StatusDone)
	if n := len(filepath.Base(got.Dest)); n > maxNameBytes {
		t.Fatalf("落盘名 %d 字节", n)
	}
	if n := len(filepath.Base(partPath(got.Dest))); n > maxNameBytes {
		t.Fatalf(".part %d 字节", n)
	}
	assertFile(t, got.Dest, 0, 1024)
}

// seedQueue 往 dir 里写一份队列文件（不经过 Manager）。
//
// 用它而不是 harness 那套：这一组要自己拼一个**两台 CDN 按 cid 分流**的
// 场景，而 harness 的 resolver 是写死的。
func seedQueue(t *testing.T, dir string, tasks map[string]*Task) {
	t.Helper()
	list := make([]*Task, 0, len(tasks))
	for _, v := range tasks {
		list = append(list, v)
	}
	b, err := json.MarshalIndent(queueFile{Version: storeVersion, Tasks: list}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, QueueFileName), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func fileSize(p string) (int64, error) {
	st, err := os.Stat(p)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}
