// 听歌统计落盘这一摊的测试。
//
// 这一组钉的是**「这份数据存得住、存不坏、存得进去」**：
// 原子写、读回来一样、文件坏了不崩、版本不对只读、文件太大不拖死启动。

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// writeStatsFile 直接写一份文件（用来造「坏的」「新的版本」那些情况）。
func writeStatsFile(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, statsFileName)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("写测试文件：%v", err)
	}
	return p
}

// readStatsFile 读一份文件，还原成快照。
func readStatsFile(t *testing.T, path string) ListeningStats {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读文件：%v", err)
	}
	var data listeningStatsData
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatalf("文件不是合法 JSON：%v", err)
	}
	tr := newListeningTracker()
	tr.replaceWith(data)
	return tr.Snapshot()
}

// TestStatsRoundTrip 钉住「写进去，读出来还是那样」。
func TestStatsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	tr := newListeningTracker()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)
	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(0, 3*time.Minute).at(base))
	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(30*time.Second, 3*time.Minute).
		at(base.Add(30 * time.Second)))

	store := newStatsStore(filepath.Join(dir, statsFileName))
	store.save(tr)

	// 重新读。
	got := readStatsFile(t, filepath.Join(dir, statsFileName))
	if got.DayMs("2026-11-01") != 30_000 {
		t.Errorf("读回来的当天时长 = %d 毫秒，want 30000", got.DayMs("2026-11-01"))
	}
	if v := got.TrackMs("mv:1|c1"); v != 30_000 {
		t.Errorf("读回来的这一首时长 = %d 毫秒，want 30000", v)
	}
}

// TestStatsSurviveARestart 钉住「退出再启动，数据还在」。
//
// 🔴 **这是这一票最要紧的一条**：统计是「本机一份」的东西，用户重启应用
// 之后那些数必须还在——不然它就不是统计，是个临时计数器。
func TestStatsSurviveARestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, statsFileName)

	tr := newListeningTracker()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)
	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(0, 3*time.Minute).at(base))
	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(45*time.Second, 3*time.Minute).
		at(base.Add(45 * time.Second)))

	store := newStatsStore(path)
	store.save(tr)
	store.close()

	// 「重启」：新建一个统计器，从盘上读。
	tr2 := newListeningTracker()
	store2 := newStatsStore(path)
	store2.loadInto(tr2)

	got := tr2.Snapshot()
	if v := got.DayMs("2026-11-01"); v != 45_000 {
		t.Errorf("重启之后的当天时长 = %d 毫秒，want 45000", v)
	}
	if v := got.TrackMs("mv:1|c1"); v != 45_000 {
		t.Errorf("重启之后的这一首时长 = %d 毫秒，want 45000", v)
	}
	// 名字的快照也要在（Top 歌那一栏要显示它）。
	top := got.TopTracks(5)
	if len(top) != 1 || top[0].Title != "一首歌" {
		t.Errorf("重启之后歌名快照没了：%+v", top)
	}
}

// TestACorruptStatsFileDoesNotCrash 钉住「文件坏了不崩」。
//
// ⚠️ **坏文件要留一份 `.bak`**（和设置文件一个规矩，见 settings.go 的
// `backupSettings`）——用户还能从里面捞回数据。
func TestACorruptStatsFileDoesNotCrash(t *testing.T) {
	dir := t.TempDir()
	path := writeStatsFile(t, dir, "{这不是 JSON")

	tr := newListeningTracker()
	store := newStatsStore(path)
	store.loadInto(tr)

	// 不崩，而且数据是空的（不是半截的）。
	got := tr.Snapshot()
	if len(got.Days) != 0 {
		t.Errorf("坏文件读出了 %d 天，want 0", len(got.Days))
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Errorf("坏文件没有留 .bak：%v", err)
	}
}

// TestStatsFromANewerVersionAreReadOnly 钉住「版本不对就只读」。
//
// ⚠️ **一份更新的版本写的文件，这一版一个字都不许写回去**——宁可统计停
// 在那儿，也不要把它降级写坏（和 `settings.go` 的 `ErrSettingsFromNewer`
// 一个规矩）。
func TestStatsFromANewerVersionAreReadOnly(t *testing.T) {
	dir := t.TempDir()
	body := `{"schemaVersion":999,"days":{},"tracks":{},"artists":{}}`
	path := writeStatsFile(t, dir, body)

	tr := newListeningTracker()
	store := newStatsStore(path)
	store.loadInto(tr)

	if !store.readOnly {
		t.Fatalf("更新的版本没有被当成只读")
	}
	// 写回去也不能改那个文件。
	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(0, time.Minute).
		at(time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)))
	store.save(tr)

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回文件：%v", err)
	}
	if string(after) != body {
		t.Errorf("只读的那一份被改了：\n got %s\nwant %s", after, body)
	}
}

// TestAnOversizedStatsFileIsNotRead 钉住「太大的文件不读」。
//
// 🔴 **这是防「一个坏掉的巨型文件把启动拖死」**：读盘是在开窗口之前做的
// （和设置文件一样），一个几百 MB 的文件会把启动卡住好几秒。
//
// ⇒ 超过上限就**放弃读这一份**，用空数据继续。
func TestAnOversizedStatsFileIsNotRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, statsFileName)
	// 造一个「声称很大」的文件：真的写几 MB 太慢，所以用稀疏文件。
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("建测试文件：%v", err)
	}
	if err := f.Truncate(statsMaxFileBytes + 1); err != nil {
		t.Fatalf("造大文件：%v", err)
	}
	f.Close()

	tr := newListeningTracker()
	store := newStatsStore(path)
	store.loadInto(tr)

	got := tr.Snapshot()
	if len(got.Days) != 0 {
		t.Errorf("太大的文件读出了 %d 天，want 0", len(got.Days))
	}
	// ⚠️ **不能是只读**：那一份已经放弃了，统计要从空的那一份重新开始。
	if store.readOnly {
		t.Errorf("太大的文件被当成了只读（应该放弃读、从空数据开始）")
	}
	// 🔴 **原文件要留一份 `.bak`**：下一次落盘会把那个路径整个换掉——用户
	// 几个月的统计就**无声无息地没了**。
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Errorf("太大的文件没有留 .bak（下一次落盘就会把它覆盖掉）：%v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Errorf("太大的文件还在原处（落盘会把它整个换掉）")
	}
}

// TestAFailedSaveIsNotRetriedEveryTick 钉住「写盘失败不要每一拍都重试」。
//
// 🔴 **这一条挡住的是一个「CPU 一直忙而界面上什么都不提」的 bug**：写盘失败
// （磁盘满了、目录只读）会让「上次落盘的时刻」停在旧值，于是**下一拍**又过
// 了那道 30 秒的闸——而每一次都是**整份数据重新编码一遍**。
//
// ⇒ 所以失败也**记上落盘的时刻**：下一次重试是 30 秒之后。
func TestAFailedSaveIsNotRetriedEveryTick(t *testing.T) {
	dir := t.TempDir()
	// ⚠️ **路径指向一个不存在的目录**：写盘一定失败。
	store := newStatsStore(filepath.Join(dir, "没有这个目录", "stats.json"))

	tr := newListeningTracker()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)
	tr.Tick(mvIn("1", "c1", "一首歌", "UP甲", 1).withPos(0, time.Minute).at(base))

	// ⚠️ **连续调 20 次**（20 拍）：只有**第一次**该真的去写。
	attempts := 0
	for i := 0; i < 20; i++ {
		before := store.lastSaveAt()
		store.saveIfDue(tr, base.Add(time.Duration(i)*50*time.Millisecond))
		if store.lastSaveAt() != before {
			attempts++
		}
	}
	if attempts > 1 {
		t.Errorf("20 拍里落盘了 %d 次，want ≤ 1（失败之后要等 30 秒才重试）", attempts)
	}
}

// TestStatsAreWrittenAtomically 钉住「写的时候不留半截文件」。
//
// ⚠️ **原子写**：临时文件 → fsync → rename。所以任何时刻打开那个文件
// 要么是旧的完整内容，要么是新的完整内容，**不会是半截**。
func TestStatsAreWrittenAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, statsFileName)

	tr := newListeningTracker()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)
	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(0, time.Minute).at(base))

	store := newStatsStore(path)
	store.save(tr)

	// 目录里不该有临时文件剩着。
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读目录：%v", err)
	}
	for _, e := range entries {
		if e.Name() != statsFileName && e.Name() != statsFileName+".bak" {
			t.Errorf("写完之后目录里多了一个 %q", e.Name())
		}
	}
}

// TestTheStoredFileIsValidJSON 钉住「写出去的是合法 JSON，且带版本号」。
func TestTheStoredFileIsValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, statsFileName)

	tr := newListeningTracker()
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)
	tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(0, time.Minute).at(base))

	store := newStatsStore(path)
	store.save(tr)

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读文件：%v", err)
	}
	var raw struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("写出去的不是合法 JSON：%v", err)
	}
	if raw.SchemaVersion != statsSchemaVersion {
		t.Errorf("版本号 = %d，want %d", raw.SchemaVersion, statsSchemaVersion)
	}
}

// TestAMissingFileIsNotAnError 钉住「第一次运行没有那个文件，不是错」。
func TestAMissingFileIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	tr := newListeningTracker()
	store := newStatsStore(filepath.Join(dir, statsFileName))
	store.loadInto(tr)
	if len(tr.Snapshot().Days) != 0 {
		t.Errorf("没有文件却读出了数据")
	}
	// 写一份也该不报错。
	store.save(tr)
}

// TestAConcurrentOlderSaveDoesNotOverwriteANewerOne 钉住「定时落盘和退出落盘撞在
// 一起时，盘上留下的是后拍的那份」：快照曾经在写锁外面拍，先拍的旧快照可能
// 后落地，把退出前那几秒的统计盖掉。
func TestAConcurrentOlderSaveDoesNotOverwriteANewerOne(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		path := filepath.Join(t.TempDir(), statsFileName)
		tr := newListeningTracker()
		base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)
		tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(0, 3*time.Minute).at(base))
		tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(30*time.Second, 3*time.Minute).
			at(base.Add(30 * time.Second)))

		store := newStatsStore(path)
		olderTaken := make(chan struct{})
		releaseOlder := make(chan struct{})
		var calls atomic.Int32
		store.afterSnapshot = func() {
			if calls.Add(1) == 1 {
				close(olderTaken)
				<-releaseOlder
			}
		}

		olderDone := make(chan error)
		go func() { olderDone <- store.save(tr) }()
		<-olderTaken

		tr.Tick(mvIn("1", "c1", "一首歌", "某个UP", 42).withPos(60*time.Second, 3*time.Minute).
			at(base.Add(60 * time.Second)))
		newerDone := make(chan error)
		go func() { newerDone <- store.save(tr) }()
		// 后开始的那次要么已经写完，要么在排队等写；放行旧的那次之后，盘上必须是新的。
		synctest.Wait()

		close(releaseOlder)
		if err := <-olderDone; err != nil {
			t.Fatalf("先开始的那次落盘：%v", err)
		}
		if err := <-newerDone; err != nil {
			t.Fatalf("后开始的那次落盘：%v", err)
		}

		if got := readStatsFile(t, path).DayMs("2026-11-01"); got != 60_000 {
			t.Errorf("盘上的当天时长 = %d 毫秒，want 60000（后拍的那份）", got)
		}
	})
}
