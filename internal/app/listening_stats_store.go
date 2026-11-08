// 听歌统计的落盘：读、写、以及「什么时候写」。
//
// # 为什么是独立的一份文件，而不在 `settings.json` 里
//
// 三个理由：
//
//  1. **`settings.json` 只认当前版本**（见 settings.go 的
//     `ErrSettingsFromNewer`）：版本对不上，整份设置文件变成**只读**。
//     而统计数据会演进（加字段、改格式），塞进去迟早把用户的设置搞成只读。
//  2. **`settings.json` 是每帧读的**（`SettingsSnapshot()`），而统计数据
//     不需要每帧读——它只在进统计页时读一次。
//  3. **写盘不互相顶**：设置存储有 120ms 的防抖窗口，而统计每 30 秒要写
//     一次——写在一起会把那个窗口一直顶掉（见 playback_state.go 那段警告）。
//
// # 原子写用 `diskcache.WriteAtomic`
//
// ⚠️ **别再抄一份**：仓库里已经有五份手抄的「临时文件 → fsync → rename」
// （settings.go / searchhistory.go / shortcuts.go / credentials / download），
// 而 `internal/service/diskcache` 那个包**就是为了消除这个重复**抽出来的。
//
// 依赖方向：`internal/app → internal/service` 是允许的（见
// `docs/experience/architecture.md`《依赖单向》）。

package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/diskcache"
)

// statsFileName 是这份数据在盘上的名字。
//
// ⚠️ **它是独立的一份文件**，不是 `settings.json` 的一部分——理由见这个
// 文件的头。
const statsFileName = "listening-stats.json"

// statsMaxFileBytes 是这份文件能有多大。
//
// ⚠️ **超了就放弃读**：读盘是在开窗口之前做的（和设置文件一样），而一个
// 几百 MB 的坏文件会把启动卡住好几秒。
//
// ⇒ 8MB 足够装几十万首歌的明细，而真到了那个量级，用户也已经不需要
// 更细的统计了。
const statsMaxFileBytes = 8 << 20

// statsSaveEvery 是「多久落一次盘」。
//
// ⚠️ **30 秒**：统计的精度不需要更高，而写盘会占磁盘 IO。真按 20Hz 写就是
// 每秒 20 次文件写——那是在拿 IO 换没人看得出来的精度。
const statsSaveEvery = 30 * time.Second

// ErrStatsFromNewer 是「这份统计来自更新的版本」。
var ErrStatsFromNewer = errors.New("app: 这份听歌统计来自更新的版本")

// statsStore 是统计的落盘那一摊。
//
// ⚠️ **它不拥有数据**：数据在 `listeningTracker` 里，store 只负责读写。
// 这样「累加」和「落盘」是分开的两件事——累加是 20Hz 的热路径，落盘是
// 30 秒一次的冷路径。
type statsStore struct {
	// path 是文件在哪。空串 = 只在内存里（拿不到应用数据目录时）。
	path string
	// readOnly = 这份文件是别的版本写的，一个字都不许写回去。
	readOnly bool

	// mu 护着下面那几个字段。
	mu sync.Mutex
	// tracker 是数据在哪儿。
	tracker *listeningTracker
	// lastSave 是上一次落盘的墙钟时刻。
	lastSave time.Time
	// closed = 关掉了。
	closed bool
	// lastErr 是最近一次落盘的错。
	lastErr error

	// wsem 把「拍快照 → 写文件」串成一次：后拍的快照一定后落地。容量为 1 的
	// 通道而不是 sync.Mutex，是为了测试里 synctest.Wait 能认出「在等写」。
	wsem chan struct{}
	// afterSnapshot 只给测试用：拍完快照、写文件之前调一次。
	afterSnapshot func()
}

// newStatsStore 开一个 store。
//
// ⚠️ **它不读盘**：读盘由 `loadInto` 显式调——因为读盘是阻塞 IO，而调用方
// 需要决定在哪儿读（启动时同步读，还是在后台 goroutine 里读）。
func newStatsStore(path string) *statsStore {
	return &statsStore{path: path, tracker: newListeningTracker(), wsem: make(chan struct{}, 1)}
}

// Path 是这个 store 的文件在哪。
func (s *statsStore) Path() string { return s.path }

// ReadOnly 报这个 store 是不是只读的。
func (s *statsStore) ReadOnly() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readOnly
}

// LastError 是最近一次落盘的错（没有就是 nil）。
func (s *statsStore) LastError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

// Tracker 是数据在哪儿（累加器）。
func (s *statsStore) Tracker() *listeningTracker { return s.tracker }

// loadInto 把盘上那一份读进来。
//
// ⚠️ **返回的错只有两个值得当回事**：文件坏了（已经留了 `.bak`）和文件
// 来自更新的版本（这一版只读）。没有文件不是错——那是第一次运行。
func (s *statsStore) loadInto(tr *listeningTracker) error {
	s.mu.Lock()
	s.tracker = tr
	path := s.path
	s.mu.Unlock()

	if path == "" {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		// 没有文件 = 第一次运行。不是错。
		return nil
	}
	// 🔴 **太大的文件不读**：见 `statsMaxFileBytes` 那段。
	//
	// ⚠️ **而它要留一份 `.bak`**：不读它是为了不让启动被拖死，可**下一次
	// 落盘会把这个文件整个换掉**（原子写写到同一个路径）——于是用户几个月的
	// 统计**无声无息地没了**。
	//
	// ⇒ 所以先把它挪成 `.bak`：数据还在那儿（用户能自己捞回来），而这一版
	// 从空的那一份重新开始。
	if fi.Size() > statsMaxFileBytes {
		_ = os.Rename(path, path+".bak")
		s.mu.Lock()
		s.readOnly = false
		s.mu.Unlock()
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var data listeningStatsData
	if err := json.Unmarshal(b, &data); err != nil {
		// 坏文件：留一份 `.bak`，用空数据继续。
		// ⚠️ **挪不动也不报错**——已经出过一次问题了，不能因为留个证据
		// 再报一次（和 searchhistory.go 的 `backupSearchHistory` 一个规矩）。
		_ = os.Rename(path, path+".bak")
		return nil
	}
	_ = b
	if data.SchemaVersion != statsSchemaVersion {
		// 来自更新的版本：只读。
		s.mu.Lock()
		s.readOnly = true
		s.mu.Unlock()
		return ErrStatsFromNewer
	}
	tr.replaceWith(data)
	return nil
}

// save 立刻落一次盘。
func (s *statsStore) save(tr *listeningTracker) error {
	s.mu.Lock()
	path := s.path
	readOnly := s.readOnly
	closed := s.closed
	s.mu.Unlock()

	if path == "" || readOnly || closed {
		return nil
	}
	// 🔴 **快照和写文件在同一次 wsem 里**：快照放在锁外的话，定时那一次和退出
	// 那一次撞在一起时，先拍的旧快照可能后落地，把新数据盖掉。
	s.wsem <- struct{}{}
	data := tr.toData()
	if s.afterSnapshot != nil {
		s.afterSnapshot()
	}
	// ⚠️ **编码在累加器的锁外面**：编码一份几百 KB 的 JSON 要几毫秒，而累加是
	// 20Hz 的热路径，不能被写盘堵住。
	b, err := json.MarshalIndent(data, "", " ")
	if err == nil {
		err = diskcache.WriteAtomic(path, b)
	}
	<-s.wsem

	s.mu.Lock()
	// ⚠️ **失败也更新 `lastSave`**：理由见 `saveIfDue` 那一段——不然下一拍
	// 又会重试一次（而每次都是整份数据重新编码）。
	s.lastSave = time.Now()
	if err != nil {
		s.lastErr = err
	} else {
		s.lastErr = nil
	}
	s.mu.Unlock()
	return err
}

// saveIfDue 到点了就落一次盘。
//
// ⚠️ **它由 tick 调**（每 50ms 一拍），所以它自己判「到点没有」——
// 而那一拍**不做 IO**（IO 在 `save` 里，而 `save` 只在真的到点时才走到）。
//
// 🔴 **失败之后不要每一拍都重试**：写盘失败（磁盘满了、目录变成只读、
// 权限变了）会让 `lastSave` 停在旧值，于是**下一拍**又过了那个 30 秒的
// 闸——而每一次都是**整份数据重新编码一遍**（几千首歌时是几毫秒）——
// 20Hz 下就是每秒 20 次，几分钟后 CPU 就一直忙着，而**界面上一个字都不提**。
//
// ⇒ 所以失败也**把 `lastSave` 记上**：下一次重试是 30 秒之后，而不是下一拍。
// 那 30 秒里丢的是「最后 30 秒的统计」——而退出前还有一次强制落盘。
func (s *statsStore) saveIfDue(tr *listeningTracker, now time.Time) {
	s.mu.Lock()
	path := s.path
	readOnly := s.readOnly
	closed := s.closed
	last := s.lastSave
	s.mu.Unlock()

	if path == "" || readOnly || closed {
		return
	}
	if now.Sub(last) < statsSaveEvery {
		return
	}
	if !tr.dirtyLocked() {
		// 没有改动就不写——省一次 IO。
		// ⚠️ **但 `lastSave` 也要更新**：否则「没改动」这一支会每一拍都走到
		// 这里（那个闸是 30 秒，所以其实还好）——而更重要的是失败之后
		// 下一拍就会重试。
		s.mu.Lock()
		s.lastSave = now
		s.mu.Unlock()
		return
	}
	_ = s.save(tr)
}

// lastSaveAt 是上一次落盘（成功**或**失败）的时刻。
//
// ⚠️ **它只给测试用**：生产代码判「到点没有」用的是 `saveIfDue` 里的那一栏。
func (s *statsStore) lastSaveAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSave
}

// close 关掉这个 store。
//
// ⚠️ **它不落盘**：落盘由调用方显式调 `FlushListeningStats`（退出前那一次
// 必须是同步的）。
func (s *statsStore) close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

// setErr 记下最近一次落盘的错。
func (s *statsStore) setErr(err error) {
	s.mu.Lock()
	s.lastErr = err
	s.mu.Unlock()
}

// ─────────────────────────────────────────────────────────────────────────────
// 数据和 tracker 之间的搬运
// ─────────────────────────────────────────────────────────────────────────────

// toData 把累加器里的数据抄成「能落盘的那一份」。
func (tr *listeningTracker) toData() listeningStatsData {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	data := listeningStatsData{
		SchemaVersion: statsSchemaVersion,
		Days:          make(map[string]*StatsDayStat, len(tr.days)),
		Tracks:        make(map[string]*StatsTrackStat, len(tr.tracks)),
		Artists:       make(map[int64]*StatsArtistStat, len(tr.artists)),
	}
	for day, d := range tr.days {
		cp := *d
		if d.Tracks != nil {
			cp.Tracks = make(map[string]*StatsTrackStat, len(d.Tracks))
			for k, v := range d.Tracks {
				t := *v
				cp.Tracks[k] = &t
			}
		}
		data.Days[day] = &cp
	}
	for k, v := range tr.tracks {
		t := *v
		data.Tracks[k] = &t
	}
	for k, v := range tr.artists {
		a := *v
		data.Artists[k] = &a
	}
	return data
}

// replaceWith 把读盘读进来的那一份装进累加器。
func (tr *listeningTracker) replaceWith(data listeningStatsData) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.days = map[string]*StatsDayStat{}
	tr.tracks = map[string]*StatsTrackStat{}
	tr.artists = map[int64]*StatsArtistStat{}
	for day, d := range data.Days {
		if d == nil {
			continue
		}
		cp := *d
		if d.Tracks == nil {
			cp.Tracks = map[string]*StatsTrackStat{}
		} else {
			cp.Tracks = make(map[string]*StatsTrackStat, len(d.Tracks))
			for k, v := range d.Tracks {
				t := *v
				cp.Tracks[k] = &t
			}
		}
		tr.days[day] = &cp
	}
	for k, v := range data.Tracks {
		if v == nil {
			continue
		}
		t := *v
		tr.tracks[k] = &t
	}
	for k, v := range data.Artists {
		if v == nil {
			continue
		}
		a := *v
		tr.artists[k] = &a
	}
	tr.dirty = false
}

// replaceWithSnapshot 把一份**公开的**快照装进累加器。
//
// ⚠️ **它是给测试用的那个入口**（见 `SeedListeningStatsForTest`）：公开的那
// 一份和内部落盘的那一份形状一样，但字段类型是导出的。
func (tr *listeningTracker) replaceWithSnapshot(s ListeningStats) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.days = map[string]*StatsDayStat{}
	tr.tracks = map[string]*StatsTrackStat{}
	tr.artists = map[int64]*StatsArtistStat{}
	for day, d := range s.Days {
		if d == nil {
			continue
		}
		cp := *d
		if d.Tracks == nil {
			cp.Tracks = map[string]*StatsTrackStat{}
		} else {
			cp.Tracks = make(map[string]*StatsTrackStat, len(d.Tracks))
			for k, v := range d.Tracks {
				t := *v
				cp.Tracks[k] = &t
			}
		}
		tr.days[day] = &cp
	}
	for k, v := range s.Tracks {
		if v == nil {
			continue
		}
		t := *v
		tr.tracks[k] = &t
	}
	for k, v := range s.Artists {
		if v == nil {
			continue
		}
		a := *v
		tr.artists[k] = &a
	}
	tr.dirty = false
}

// dirtyLocked 报有没有该落盘的改动。
func (tr *listeningTracker) dirtyLocked() bool {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.dirty
}

// StatsPath 是某个目录下的统计文件路径。
func StatsPath(dir string) string { return filepath.Join(dir, statsFileName) }
