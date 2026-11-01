// 听歌统计这一摊：累加、聚合、以及「现在到底在听什么」。
//
// # 它记的是什么
//
// **净时长**——播放器的位置走了多少，扣掉暂停、拖进度条那些不算的。
// 这是「听了多久」这件事唯一诚实的口径：墙钟算不出暂停，而位置算得出
// 拖动（拖动那一拍位置会跳，而那一跳要被钳掉）。
//
// # 它和「往 B 站写播放记录」是**两件事**
//
// `playreport.go` 那一份是**写给 B 站看的**（心跳、上报、账号），
// 这一份是**写给用户自己看的**（本机一份，登录与否都记）。
//
// ⚠️ **两者不共享代码，也不共享口径**：`reportTick` 只在挂了在线流之后才
// 跑（`r == nil` 就 return），所以**本地文件在那一条路上根本不被记**。
// 而这一份要把本地文件算进去——所以它是**另一路**。
//
// # 「不新增依赖」
//
// 这一层只有标准库。落盘那一半在 `listening_stats_store.go`。

package app

import (
	"sort"
	"sync"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/audio"
)

// statsSchemaVersion 是这份数据当前的格式版本。
//
// ⚠️ **版本不对就只读**（和设置文件一个规矩，见 settings.go 的
// `ErrSettingsFromNewer`）：一份更新的版本写的文件，这一版一个字都不许
// 写回去——宁可这一版的统计停在那儿，也不要把它降级写坏。
const statsSchemaVersion = 1

// 几个判据的常量。
//
// 🔴 **它们都是「口径」，不是实现细节**：改了任何一个，界面上显示的数就
// 变了，而用户看不出为什么——所以每一个都写在这里，且都写清出处。
const (
	// statsMinTrackSecs 是「多短的歌不算一次收听」的门槛：30 秒。
	//
	// ⚠️ **这是 Last.fm 的口径**（一首歌要长于 30 秒才算一次 scrobble）。
	// 没有这条闸，一个 10 秒的短歌反复播就会刷出一堆假的「听完」。
	statsMinTrackSecs = 30

	// statsFourMinSecs 是「听完」的第二道判据：4 分钟。
	//
	// ⚠️ **Last.fm 是「一半 或 4 分钟，取先到的」**——一首 20 分钟的歌听
	// 4 分钟就算一次。
	statsFourMinSecs = 240
)

// statsMaxRateFactor 是「墙钟 × 几」算「位置增量」的钳制上限。
//
// ⚠️ **它比应用允许的最大倍率（2）宽一点**：2 倍速时位置增量**正好**是墙钟
// 的两倍，而缓冲区补数据、停顿时解码器追上来——那些会让某一拍的位置**多走
// 一点点**。钳到正好 2 的话，那些多走的一点点**每次都被砍掉**，于是 2 倍速
// 听二十分钟会少记好几秒——而每首歌都少一点，加起来就是「统计比真实听的
// 时间少」。
//
// ⇒ 所以是 2.25：宽到容下那点抖动，又窄到挡住「拖进度条」那种一下跳几
// 分钟的。
//
// ⚠️ **拖进度条也在这个钳制下**：用户把进度从 0 拖到 3 分钟，墙钟只过了
// 5 秒，于是这一拍记 5×2.25 ≈ 11 秒。**那 11 秒是误差**——但比虚增
// 3 分钟好得多。
//
// ⚠️ **它是整数**（不是 2.25）：那一栏是**整数纳秒**的乘法，写成小数会
// 被截断（而截断成 2 就没有上面说的那点余量了）。
//
// ⇒ 所以是 9/4 = 2.25，用「先乘 9 再除 4」算。
const statsMaxRateNum = 9
const statsMaxRateDen = 4

// ─────────────────────────────────────────────────────────────────────────────
// 数据模型
// ─────────────────────────────────────────────────────────────────────────────

// StatsTrackStat 是一首歌的累计。
type StatsTrackStat struct {
	// Ms 是累计净时长（毫秒）。
	Ms int64 `json:"ms"`
	// Plays 是起播次数。
	Plays int64 `json:"plays"`
	// Title 是**听的时候**的歌名。
	//
	// ⚠️ **它是快照**：B 站的歌名和 UP 名会变、稿件会删。只存 id 的话，
	// 三个月后 Top 歌那一栏全是「未知作者」。存快照 = 名字是听的时候的样子。
	Title string `json:"title,omitempty"`
	// Artist 是 UP 名（本地文件没有这一栏）。
	Artist string `json:"artist,omitempty"`
	// ArtistMid 是 UP 主的编号。0 = 没有编号。
	ArtistMid int64 `json:"artistMid,omitempty"`
	// Local = 这是本地文件。本地文件没有 UP 主，也不进艺人榜。
	Local bool `json:"local,omitempty"`
	// FirstHeard 是第一次听到它的那一天（"2026-11-01"）。
	FirstHeard string `json:"firstHeard,omitempty"`
}

// StatsDayStat 是一天的累计。
type StatsDayStat struct {
	// Ms 是这一天累计的净时长（毫秒）。
	Ms int64 `json:"ms"`
	// Plays 是这一天起播的次数。
	Plays int64 `json:"plays"`
	// Completed 是这一天「听完」的次数。
	Completed int64 `json:"completed"`
	// Tracks 是这一天每一首的明细。
	//
	// ⚠️ **它是必要的**：Top 歌要按「这一段时间内」排，而全局那份是「所有
	// 时间」的——周和月要的是区间内的量。
	Tracks map[string]*StatsTrackStat `json:"tracks,omitempty"`
}

// StatsArtistStat 是一个 UP 主的累计。
type StatsArtistStat struct {
	// Name 是 UP 名。
	Name string `json:"name"`
	// Ms / Plays 是累计。
	Ms    int64 `json:"ms"`
	Plays int64 `json:"plays"`
}

// listeningStatsData 是落在盘上的那一份。
type listeningStatsData struct {
	// SchemaVersion 是格式版本。
	SchemaVersion int `json:"schemaVersion"`
	// Days 是按天存的。
	Days map[string]*StatsDayStat `json:"days"`
	// Tracks 是全局聚合（Top 歌从这儿算）。
	Tracks map[string]*StatsTrackStat `json:"tracks"`
	// Artists 是全局聚合（Top 艺人从这儿算）。
	//
	// ⚠️ **键是 int64**：Go 的 `encoding/json` 支持整型键的 map（编码成
	// JSON 对象时键是字符串），而用字符串键的话每次聚合都要
	// `strconv.FormatInt` 一次——那是分配。
	Artists map[int64]*StatsArtistStat `json:"artists"`
}

// ─────────────────────────────────────────────────────────────────────────────
// 快照（给界面层读的那一份）
// ─────────────────────────────────────────────────────────────────────────────

// StatsTrackRow 是 Top 歌的一行。
type StatsTrackRow struct {
	// Key 是这一首的聚合键。
	Key string
	// Title / Artist / ArtistMid 是快照里的那几样。
	Title     string
	Artist    string
	ArtistMid int64
	// Ms 是这一首的净时长（毫秒）。
	Ms int64
	// Plays 是起播次数。
	Plays int64
	// Local = 这是本地文件。
	Local bool
	// FirstHeard 是第一次听到它的那一天。
	FirstHeard string
}

// StatsArtistRow 是 Top 艺人的一行。
type StatsArtistRow struct {
	// Mid 是 UP 主的编号。
	Mid int64
	// Name 是 UP 名。
	Name string
	// Ms / Plays 是累计。
	Ms    int64
	Plays int64
}

// ListeningStats 是统计的**只读快照**。
//
// ⚠️ **它是一次值拷贝**：界面层要读的那些数从这儿拿，而每帧都在
// 拷贝整份 map 会让「进统计页」这一下的开销变成 O(n)。
//
// ⇒ 所以界面层**不该每帧拿一份**——它只在进页面时拿一次（见
// `App.ListeningStats`），那一页自己留着。
type ListeningStats struct {
	// Days / Tracks / Artists 是那三份。
	Days    map[string]*StatsDayStat
	Tracks  map[string]*StatsTrackStat
	Artists map[int64]*StatsArtistStat
}

// DayMs 是某一天的净时长（毫秒）。没有那一天就是 0。
func (s ListeningStats) DayMs(day string) int64 {
	if d, ok := s.Days[day]; ok {
		return d.Ms
	}
	return 0
}

// DayPlays 是某一天的起播次数。
func (s ListeningStats) DayPlays(day string) int64 {
	if d, ok := s.Days[day]; ok {
		return d.Plays
	}
	return 0
}

// DayCompleted 是某一天「听完」的次数。
func (s ListeningStats) DayCompleted(day string) int64 {
	if d, ok := s.Days[day]; ok {
		return d.Completed
	}
	return 0
}

// TrackMs 是某一首的净时长（毫秒）。
func (s ListeningStats) TrackMs(key string) int64 {
	if t, ok := s.Tracks[key]; ok {
		return t.Ms
	}
	return 0
}

// DayRangeMs 是「从 from 到 to（含）」这段里的净时长（毫秒）。
//
// ⚠️ **两端都是「这一天」的键**（"2026-11-01" 那种），不是时刻。
func (s ListeningStats) DayRangeMs(from, to string) int64 {
	var sum int64
	for day, d := range s.Days {
		if day < from || day > to {
			continue
		}
		sum += d.Ms
	}
	return sum
}

// DayRangePlays 是「从 from 到 to（含）」这段里的起播次数。
func (s ListeningStats) DayRangePlays(from, to string) int64 {
	var sum int64
	for day, d := range s.Days {
		if day < from || day > to {
			continue
		}
		sum += d.Plays
	}
	return sum
}

// DayRangeCompleted 是「从 from 到 to（含）」这段里「听完」的次数。
func (s ListeningStats) DayRangeCompleted(from, to string) int64 {
	var sum int64
	for day, d := range s.Days {
		if day < from || day > to {
			continue
		}
		sum += d.Completed
	}
	return sum
}

// TopTracks 是「听得最久的那些歌」，按时长倒序。
//
// ⚠️ **limit <= 0 给全部**。
func (s ListeningStats) TopTracks(limit int) []StatsTrackRow {
	rows := make([]StatsTrackRow, 0, len(s.Tracks))
	for key, t := range s.Tracks {
		rows = append(rows, StatsTrackRow{
			Key: key, Title: t.Title, Artist: t.Artist, ArtistMid: t.ArtistMid,
			Ms: t.Ms, Plays: t.Plays, Local: t.Local, FirstHeard: t.FirstHeard,
		})
	}
	// 🔴 **排序要稳定**：两首时长一样的时候，顺序不能每次都变——那个会
	// 让「Top 歌」那一栏在用户刷新时跳来跳去。
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Ms != rows[j].Ms {
			return rows[i].Ms > rows[j].Ms
		}
		if rows[i].Plays != rows[j].Plays {
			return rows[i].Plays > rows[j].Plays
		}
		return rows[i].Key < rows[j].Key
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

// TopArtists 是「听得最多的那些 UP 主」，按时长倒序。
//
// ⚠️ **本地文件不进这里**（它们没有 UP 主）。
func (s ListeningStats) TopArtists(limit int) []StatsArtistRow {
	rows := make([]StatsArtistRow, 0, len(s.Artists))
	for mid, a := range s.Artists {
		rows = append(rows, StatsArtistRow{Mid: mid, Name: a.Name, Ms: a.Ms, Plays: a.Plays})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Ms != rows[j].Ms {
			return rows[i].Ms > rows[j].Ms
		}
		if rows[i].Plays != rows[j].Plays {
			return rows[i].Plays > rows[j].Plays
		}
		return rows[i].Mid < rows[j].Mid
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

// Streak 是「到 today 为止连续听了几天」。
//
// ⚠️ **今天还没听不算断签**：从昨天往前数（见 `TestStreakFromYesterdayCounts`）。
// 那是连续天数的通行口径——用户是「今天还没听」而不是「昨天听了一天然后断了」。
//
// ⚠️ **「有没有听」判的是「有没有净时长」**，不是「有没有起播」——
// 装上一首歌又立刻切走，那不算「今天听了歌」。
func (s ListeningStats) Streak(today time.Time) int {
	day := today.Format("2006-01-02")
	// 今天还没有时长 → 从昨天开始数。
	if !s.dayHasTime(day) {
		day = today.AddDate(0, 0, -1).Format("2006-01-02")
	}
	n := 0
	for s.dayHasTime(day) {
		n++
		t, err := time.ParseInLocation("2006-01-02", day, today.Location())
		if err != nil {
			break
		}
		day = t.AddDate(0, 0, -1).Format("2006-01-02")
	}
	return n
}

// dayHasTime = 这一天有没有真的听过。
func (s ListeningStats) dayHasTime(day string) bool {
	d, ok := s.Days[day]
	return ok && d.Ms > 0
}

// ActiveDays 是「有听歌记录的天数」——一共有多少天在听。
func (s ListeningStats) ActiveDays() int {
	n := 0
	for _, d := range s.Days {
		if d.Ms > 0 || d.Plays > 0 {
			n++
		}
	}
	return n
}

// FirstDay 是「第一次听歌的那一天」（"2026-11-01" 那种）。没有就是空串。
func (s ListeningStats) FirstDay() string {
	first := ""
	for day := range s.Days {
		if first == "" || day < first {
			first = day
		}
	}
	return first
}

// ─────────────────────────────────────────────────────────────────────────────
// 累加器
// ─────────────────────────────────────────────────────────────────────────────

// statsIdentity 是「正在放的是哪一首」的判别材料。
//
// ⚠️ **它分开存而不是存一个算好的键**：那个算好的键（`Item.Identity()`）
// 是**拼出来的字符串**，每 20Hz 拼一次就是每秒 20 次分配。
//
// ⇒ 这里存的是**原件**（bvid / cid / sid / path），比较它们是**零分配**的
// （字符串比较不分配），而只有真的换了歌时才拼一次键。
type statsIdentity struct {
	kind string
	bvid string
	cid  string
	sid  string
	path string
}

// equal 是「还是同一首吗」。
func (i statsIdentity) equal(o statsIdentity) bool {
	return i.kind == o.kind && i.bvid == o.bvid && i.cid == o.cid &&
		i.sid == o.sid && i.path == o.path
}

// key 把身份拼成聚合键。
//
// ⚠️ **拼法照 `Item.trackKey`**（见 queue.go）：`mv:<bvid>|<cid>` /
// `audio:<sid>` / `local:<path>`。别自己另写一份——那会让「这一首」在
// 统计里和在别处认成两首。
func (i statsIdentity) key() string {
	switch i.kind {
	case "mv":
		if i.bvid == "" {
			return ""
		}
		if i.cid == "" {
			return "mv:" + i.bvid
		}
		return "mv:" + i.bvid + "|" + i.cid
	case "audio":
		if i.sid == "" {
			return ""
		}
		return "audio:" + i.sid
	case "local":
		if i.path == "" {
			return ""
		}
		return "local:" + i.path
	}
	return ""
}

// statsTickState 是「这一拍到上一拍之间」的标量状态。
//
// ⚠️ **它全是标量**——这是「tick 上零分配」的实现基础。见
// `TestTheTrackerDoesNotAllocateOnTick`。
type statsTickState struct {
	// id 是正在放的那一首的身份原件。
	id statsIdentity
	// key 是算好的聚合键。空串 = 没有在放的东西。
	key string
	// title / artist / artistMid / local 是这一首的快照。
	title     string
	artist    string
	artistMid int64
	local     bool
	// kind 是这一首的类型（"mv" / "audio" / "local"）。
	kind string
	// lastPos 是上一拍的位置。
	lastPos time.Duration
	// lastAt 是上一拍的墙钟时刻。
	lastAt time.Time
	// haveLast = 上一拍是不是有位置可读。
	//
	// ⚠️ **第一拍没有**：播放器刚装上时位置是 0，而墙钟不知道从哪儿起算。
	haveLast bool
	// accMs 是这一首**累计**的净时长（跨暂停累加）。
	accMs int64
	// accDay / accDayMs 是「这一遍**当前这一天**记了多少时长」。
	//
	// 🔴 **它们不是 `dayKey`**：`dayKey` 是**这一拍**是哪一天，而这一遍的
	// 时长可能跨了零点（前半段在昨天、后半段在今天）。
	//
	// accBestDay / accBestMs 是「这一遍时长**最多的那一天**」——而「听完」
	// 记在那一天（理由见 `settleLocked`）。
	accDay     string
	accDayMs   int64
	accBestDay string
	accBestMs  int64
	// dur 是这一首的总长。
	dur time.Duration
	// lastDayKey 是**上一拍**是哪一天。
	//
	// 🔴 **它和 `dayKey` 分开**：这一拍的时间增量是**从上一拍到这一拍**
	// 之间的，而那一段**大部分属于「上一拍那一天」**——尤其在跨零点那一拍：
	// 23:50 → 00:00 这一拍，时刻是 00:00（11-02），而它记的是 **11-01**
	// 的那十分钟。
	//
	// ⚠️ **不分开的后果**：跨零点的那首歌，它的时长**整段记在第二天**——
	// 而用户昨天明明听了十分钟。
	lastDayKey string
	// dayKey 是「今天」的键（本地日期）。
	//
	// ⚠️ **它跟着日期走，不是跟着某一首走**：一首歌跨零点时，后半段计入
	// 后一天——那是业界通行的口径。
	dayKey string
	// dayY / dayM / dayD 是 dayKey 对应的日期，用来判「跨天了没有」。
	//
	// ⚠️ **判跨天用它们而不用字符串比**：`time.Time.Date()` 返回三个 int，
	// 不分配；而重新 `Format` 一次要分配。
	dayY, dayM, dayD int
	// player 是上一拍的那台播放器。
	player *audio.Player
	// ended = 上一首**真的放完了**。
	//
	// 🔴 **它不是「清掉身份」**：清掉身份的话，下一拍会看到「身份变了」，
	// 而那一支会**再记一次起播**——于是「放完一遍」算成「起了两次」。
	//
	// ⚠️ **而下一拍往往还是同一首**（队列停在队尾、或者下一首还在取流而
	// `a.online` 还指着旧的这一首）——那正是那个多出来的起播。
	ended bool
}

// listeningTracker 是统计的累加器。
//
// ⚠️ **它有自己的锁，不在 `App.mu` 里**：它每 20Hz 跑一次，而 `App.mu` 是
// 界面**每帧**都要的——统计绝不能把界面卡住。
type listeningTracker struct {
	mu sync.Mutex

	// st 是这一拍到上一拍之间的标量状态。
	st statsTickState

	// days / tracks / artists 是三份数据。
	days    map[string]*StatsDayStat
	tracks  map[string]*StatsTrackStat
	artists map[int64]*StatsArtistStat

	// dirty = 有改动，该落盘了。
	dirty bool
	// readOnly = 这份文件是别的版本写的，一个字都不许写回去。
	readOnly bool
}

// newListeningTracker 开一个空的累加器。
func newListeningTracker() *listeningTracker {
	return &listeningTracker{
		days:    map[string]*StatsDayStat{},
		tracks:  map[string]*StatsTrackStat{},
		artists: map[int64]*StatsArtistStat{},
	}
}

// statsTickIn 是「这一拍看到了什么」。
//
// ⚠️ **它照着 tick 上真正能拿到的那些东西列**：位置、时长、在不在放、这一首
// 是谁、还有歌名和 UP 名（界面层要显示的那两样）。
type statsTickIn struct {
	// Kind 是这一首的类型："mv" / "audio" / "local"。空串 = 什么都没有在放。
	Kind string
	// Bvid / Cid / Sid / Path 是身份原件（见 statsIdentity）。
	Bvid string
	Cid  string
	Sid  string
	Path string
	// Title / Artist 是这一首的名字和 UP 名。
	Title     string
	Artist    string
	ArtistMid int64
	// Pos 是播放器的位置。
	Pos time.Duration
	// Dur 是这一首的总长。0 = 拿不到。
	Dur time.Duration
	// FreshPos = 这个位置是**这一拍新读到的**。
	//
	// ⚠️ **它不是可有可无的**：暂停的时候 tick 压根不读位置（读出来的是
	// 零值），而「读出来的零值」和「真的回到开头了」是两件事——分不清的话，
	// 暂停一次就会把位置当成跳回开头，下一次恢复时那一下子会被算成
	// 「听了很久」。
	FreshPos bool
	// Player 是这一拍的那台播放器（`*audio.Player`）。
	//
	// ⚠️ **它不是接口，是具体类型**——因为要在**热路径上比较它**，而比较
	// 指针是零分配的。存成一个 `any` 会多一次装箱（一次分配）。
	//
	// ⚠️ **它是「又听了一遍」的唯一可靠判据**：换音质会装上新的一台播放器
	// 但**还是同一首**（那不是重听），而单曲循环会装上新的一台播放器
	// **且还是同一首**（那是重听）。两者的区别在于**位置**——换音质时
	// 位置从原来那��继续，重听时位置回到 0。
	//
	// ⇒ 所以判据是「播放器换了 **且** 位置回到 0」。
	Player *audio.Player
	// Finished = 这一首**真的**放完了（字节到头、没有报错）。
	Finished bool
	// At 是这一拍的墙钟时刻。
	At time.Time
}

// Tick 是「这一拍看到了什么」。
//
// ⚠️ **它不许分配**：见 `TestTheTrackerDoesNotAllocateOnTick`。
//
// ⚠️ **它锁的是自己的锁，不是 `App.mu`**：见这个类型的注释。
func (tr *listeningTracker) Tick(in statsTickIn) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	id := statsIdentity{kind: in.Kind, bvid: in.Bvid, cid: in.Cid, sid: in.Sid, path: in.Path}

	// 🔴 **上一首放完了，而这一拍还是同一首**（队列停在队尾、或者下一首还在
	// 取流）：那**不是**一次新的起播——那是同一首的尾巴。
	//
	// ⚠️ **而「下一首还在取流」那一种更糟**：`a.online` 在新流装上之前
	// 一直指着**旧那一首**——于是那段时间会被算成「用户还在听旧那首」，
	// 连时长都会虚增。
	//
	// ⇒ 所以这一拍**什么都不记**：等身份真的变了（`changed`）再记。
	if tr.st.ended && id.equal(tr.st.id) {
		tr.st.ended = false
		tr.st.haveLast = false
		tr.st.lastPos = 0
		tr.st.lastAt = in.At
		return
	}

	// 「换了一首」= 身份原件变了。空身份 = 什么都没有在放。
	changed := !id.equal(tr.st.id)
	if changed {
		// 🔴 **先结算上一首**：它已经听掉的那些要在换歌这一刻记进去。
		tr.settleLocked()
		tr.st.id = id
		tr.st.key = id.key()
		tr.st.title = in.Title
		tr.st.artist = in.Artist
		tr.st.artistMid = in.ArtistMid
		tr.st.local = in.Kind == "local"
		tr.st.kind = in.Kind
		tr.st.accMs = 0
		tr.st.accDay = ""
		tr.st.accDayMs = 0
		tr.st.accBestDay = ""
		tr.st.accBestMs = 0
		tr.st.dur = in.Dur
		tr.st.haveLast = false
		// 🔴 **换歌 = 一次起播**。空身份（什么都没有）不算。
		if tr.st.key != "" {
			tr.countPlayLocked(in)
		}
	} else if tr.st.key != "" {
		if tr.st.dur == 0 && in.Dur > 0 {
			// 时长是后知的（取流完了才知道）。补上。
			tr.st.dur = in.Dur
		}
		// 🔴 **同一首重听**：身份没变，但**装上了新的一台播放器，而且位置
		// 回到 0**——那是一次新的起播，也是新的一遍。
		//
		// ⚠️ **为什么还要看位置**：换音质也会装上新的一台播放器（见
		// `restreamOnline`），但那**还是同一遍**（位置从原来那儿继续）。
		// 只看「播放器换了」会把换音质算成重听——于是单曲循环那一档的
		// 起播次数会虚高。
		//
		// ⚠️ **为什么还要看「之前真的听过」**：刚起播（位置还在 0）的时候
		// 立刻换一档音质，那一刻位置也是 0——它不是重听，是换了一档继续听。
		// 而「之前真的听过」的判据是「上一拍的位置不在 0 **或** 这一遍已经
		// 攒了时长」。
		if in.Player != nil && tr.st.player != nil && in.Player != tr.st.player &&
			in.FreshPos && in.Pos == 0 && (tr.st.lastPos > 0 || tr.st.accMs > 0) {
			tr.settleLocked()
			tr.st.accMs = 0
			tr.st.accDay = ""
			tr.st.accDayMs = 0
			tr.st.accBestDay = ""
			tr.st.accBestMs = 0
			tr.st.haveLast = false
			tr.countPlayLocked(in)
		}
	}

	if tr.st.key == "" {
		// 什么都没有在放：位置不动，那一拍不记时长。
		tr.st.lastPos = 0
		tr.st.lastAt = in.At
		tr.st.haveLast = false
		return
	}

	// 「今天是几号」——**只在跨天时重算**，于是热路径上零分配。
	day := tr.dayKeyLocked(in.At)

	// ⚠️ **这一拍没读到位置**（暂停时 tick 不读位置）：基线**不动**。
	//
	// 🔴 **动了就错**：暂停十分钟后恢复，第一拍会看到「位置从 0 跳到
	// 30 秒」——那一下子会被算成听了 30 秒。
	if !in.FreshPos {
		tr.st.lastAt = in.At
		return
	}

	if !tr.st.haveLast {
		// 第一拍：记住位置，不记时长。
		tr.st.lastPos = in.Pos
		tr.st.lastAt = in.At
		tr.st.player = in.Player
		tr.st.haveLast = true
		tr.st.lastDayKey = day
		// 🔴 **这一遍是从这一天开始的**：而「这一遍时长主要在哪一天」要靠
		// 这一栏——第一拍不记时长，但它**把这一天的起点**定了。
		//
		// ⚠️ **不记它会怎样**：一首歌 23:50 开始听，第一拍记下「基线」而
		// **不记时长**，于是 11-01 那一截是 0 分钟，而 11-02 的那一截是
		// 全部——于是「听完」记在 11-02，而**用户 11-01 明明听了十分钟**。
		if tr.st.accDay == "" {
			tr.st.accDay = day
		}
		if in.Finished {
			tr.settleLocked()
			tr.st.key = ""
			tr.st.haveLast = false
		}
		return
	}

	// 位置增量。
	delta := in.Pos - tr.st.lastPos
	// 墙钟增量。
	wall := in.At.Sub(tr.st.lastAt)

	// 🔴 **位置往回走**（拖回开头）→ 不记。
	if delta < 0 {
		delta = 0
	}
	// 🔴 **墙钟往回走**（系统时间改了）→ 不记，也别让钳制把它变成负的。
	if wall < 0 {
		wall = 0
	}
	// 🔴 **钳制**：拖进度条会让位置增量暴涨，而倍速播放会让它超过墙钟。
	// 两者都被同一个上限挡住。
	if max := wall * statsMaxRateNum / statsMaxRateDen; delta > max {
		delta = max
	}
	tr.st.accMs += delta.Milliseconds()
	// 🔴 **这一遍的时长主要记在哪一天**：按那一拍的时长加权，多的那天赢。
	//
	// ⚠️ **用的是「上一拍那一天」**（`lastDayKey`）：这一拍的时间增量是
	// **从上一拍到这一拍**之间的，而那一段**属于上一拍那一天**——跨零点
	// 那一拍尤其要紧（23:50 → 00:00 记的是 **11-01** 的那十分钟）。
	ms := delta.Milliseconds()
	// ⚠️ **这是「上一拍那一天」**：这一拍的时间增量是**从上一拍到这一拍**
	// 之间的，而那一段属于**上一拍那一天**——跨零点那一拍尤其要紧
	//（23:50 → 00:00 记的是 **11-01** 的那十分钟）。
	spanDay := tr.st.lastDayKey
	if spanDay == "" {
		spanDay = day
	}
	if tr.st.accDay != spanDay {
		// 换了一天：把前一天那一截**结算**成「最多的一天」的候选。
		//
		// ⚠️ **「大于等于」还是「大于」**：平手时**先来的那天**赢——而那是
		// **更早**的那一天（听歌是往前走的时间）。平手记在后面那天，会让
		// 「跨零点」的那首歌记到「用户还没听满一天」的那一边去。
		if tr.st.accDayMs >= tr.st.accBestMs {
			tr.st.accBestMs = tr.st.accDayMs
			tr.st.accBestDay = tr.st.accDay
		}
		tr.st.accDay = spanDay
		tr.st.accDayMs = 0
	}
	tr.st.accDayMs += ms
	if tr.st.accDayMs > tr.st.accBestMs {
		tr.st.accBestMs = tr.st.accDayMs
		tr.st.accBestDay = spanDay
	}

	// 🔴 **时长按「这一拍是哪一天」记进去**：一首歌跨零点时，后半段计入
	// 后一天——那是业界通行的口径。
	//
	// ⚠️ **三份都记，而且是同一拍记**：当天那一份、当天那首歌的明细、
	// 全局那一份。三份**不能只记其中一份**——
	//
	//   - 只记当天那一份 → 「Top 歌」要等换歌才准（正在放的那首是 0）；
	//   - 只记全局那一份 → 周和月拿不到区间内的量。
	//
	// ⚠️ **全局那一份按拍累加，所以「正在放的那首」也是准的**——界面层
	// 进页面时看到的就是实时的数，不用等它播完。
	if ms := delta.Milliseconds(); ms > 0 {
		d := tr.dayStatLocked(day)
		d.Ms += ms
		dt := tr.dayTrackLocked(d, tr.st.key)
		dt.Ms += ms

		t := tr.trackStatLocked(tr.st.key, statsTickIn{
			Title: tr.st.title, Artist: tr.st.artist, ArtistMid: tr.st.artistMid,
			Kind: tr.st.kind,
		}, day)
		t.Ms += ms

		// 🔴 **当天那一份也要带上那几个快照字段**（歌名 / UP 名 / UP 编号 /
		// 是不是本地文件）。
		//
		// ⚠️ **为什么**：界面层的「Top 艺人」那一档是**从按天存的那一份里
		// 加起来**的（见 `statsPeriodArtists`）——而它靠的是每一首的
		// `ArtistMid`。那一栏要是空的，那张表**永远是空的**，而界面上看不出
		// 任何异样（它只显示「这一段时间还没有听过 B 站的歌」）。
		dt.Title = t.Title
		dt.Artist = t.Artist
		dt.ArtistMid = t.ArtistMid
		dt.Local = t.Local
		dt.FirstHeard = t.FirstHeard

		// ⚠️ **本地文件没有 UP 主**，不进艺人榜。
		if !tr.st.local && tr.st.artistMid != 0 {
			a := tr.artistStatLocked(tr.st.artistMid, tr.st.artist)
			a.Ms += ms
		}
	}

	tr.st.lastPos = in.Pos
	tr.st.lastAt = in.At
	tr.st.player = in.Player
	tr.st.lastDayKey = day

	if in.Finished {
		// 🔴 **放完这一刻**：上层把位置归零了（见 app.go 的
		// `setPositionLocked(0)`），而**那一段时长不能丢**——它在上面那
		// 几行已经按位置增量记进去了。
		tr.settleLocked()
		// 🔴 **记下「这一首放完了」**，而**不是清掉身份**。
		//
		// ⚠️ **清掉身份会多记一次起播**：下一拍看到「身份变了」就记一次，
		// 而下一拍往往还是同一首（队列停在队尾 / 下一首还在取流而
		// `a.online` 还指着旧的这一首）——于是「听了一遍」算成「起了两次」，
		// 而且旧那首在取流那一段时间里**连时长都会虚增**。
		tr.st.ended = true
		tr.st.key = ""
		tr.st.haveLast = false
	}
}

// countPlayLocked 记一次起播。
func (tr *listeningTracker) countPlayLocked(in statsTickIn) {
	day := tr.dayKeyLocked(in.At)
	d := tr.dayStatLocked(day)
	d.Plays++

	t := tr.trackStatLocked(tr.st.key, in, day)
	t.Plays++

	// ⚠️ **当天那一份也要记次数**：周和月那两档的 Top 表是**从按天存的那份
	// 里加起来**的，而那份要能算出「这一段里这一首被起了几次」。
	dt := tr.dayTrackLocked(tr.dayStatLocked(day), tr.st.key)
	dt.Plays++

	// ⚠️ **本地文件没有 UP 主**，不进艺人榜。
	if !tr.st.local && tr.st.artistMid != 0 {
		a := tr.artistStatLocked(tr.st.artistMid, tr.st.artist)
		a.Plays++
	}
	tr.dirty = true
}

// settleLocked 结算「这一遍」——判「听完」，并把这一遍的累计归零。
//
// ⚠️ **它不再往三份数据里加时长**：那些是**按拍**加的（见 Tick 里那一段）。
// 它只做两件事：
//
//  1. 判「听完」——累计净时长到了一半、或到 4 分钟，且这一首长于 30 秒；
//  2. 把 `accMs` 归零（换歌 / 重听时开始新的一遍）。
func (tr *listeningTracker) settleLocked() {
	key := tr.st.key
	if key == "" || tr.st.accMs <= 0 {
		tr.st.accMs = 0
		return
	}
	// 🔴 **「听完」记在这一遍时长主要在的那一天**（理由见 `accBestDay`）。
	//
	// ⚠️ **它不是 `dayKey`**：`dayKey` 是**最后那一拍**是哪一天——而一首歌
	// 跨零点时，最后那一拍在第二天，于是「听完」会记到第二天，而那天用户
	// 一分钟都没听（时长已经按拍记在昨天了）。
	day := tr.st.accBestDay
	if day == "" {
		day = tr.st.accDay
	}
	if day == "" {
		day = tr.st.dayKey
	}
	if day == "" {
		tr.st.accMs = 0
		return
	}
	d := tr.dayStatLocked(day)

	// 🔴 **「听完」的判据**（Last.fm 口径）：长于 30 秒，且（听了一半 或 听满 4 分钟）。
	//
	// ⚠️ **判的是「这一遍」的累计**（`accMs`），不是「这一首一共听了多少」——
	// 后者会把「同一首听了十遍、每遍 10 秒」算成听完一遍。
	if tr.st.dur > statsMinTrackSecs*time.Second {
		half := (tr.st.dur / 2).Milliseconds()
		fourMin := int64(statsFourMinSecs) * 1000
		if tr.st.accMs >= half || tr.st.accMs >= fourMin {
			d.Completed++
		}
	}
	tr.st.accMs = 0
	tr.st.accDay = ""
	tr.st.accDayMs = 0
	tr.st.accBestDay = ""
	tr.st.accBestMs = 0
	tr.dirty = true
}

// dayKeyLocked 是「这一刻是哪一天」，**只在跨天时重算**。
//
// ⚠️ **热路径上它只做三次整数比较**（`time.Time.Date()` 不分配）——
// 而每 20Hz 重新 `Format` 一次字符串就是每秒 20 次分配。
func (tr *listeningTracker) dayKeyLocked(at time.Time) string {
	y, m, d := at.Date()
	if y == tr.st.dayY && int(m) == tr.st.dayM && d == tr.st.dayD && tr.st.dayKey != "" {
		return tr.st.dayKey
	}
	tr.st.dayY, tr.st.dayM, tr.st.dayD = y, int(m), d
	tr.st.dayKey = at.Format("2006-01-02")
	return tr.st.dayKey
}

// dayStatLocked 拿（没有就建）某一天的那一份。
func (tr *listeningTracker) dayStatLocked(day string) *StatsDayStat {
	d, ok := tr.days[day]
	if !ok {
		d = &StatsDayStat{Tracks: map[string]*StatsTrackStat{}}
		tr.days[day] = d
	}
	return d
}

// dayTrackLocked 拿（没有就建）某一天里某一首的那一份。
func (tr *listeningTracker) dayTrackLocked(d *StatsDayStat, key string) *StatsTrackStat {
	if d.Tracks == nil {
		d.Tracks = map[string]*StatsTrackStat{}
	}
	t, ok := d.Tracks[key]
	if !ok {
		t = &StatsTrackStat{}
		d.Tracks[key] = t
	}
	return t
}

// trackStatLocked 拿（没有就建）某一首的全局那一份。
func (tr *listeningTracker) trackStatLocked(key string, in statsTickIn, day string) *StatsTrackStat {
	t, ok := tr.tracks[key]
	if !ok {
		t = &StatsTrackStat{}
		tr.tracks[key] = t
		if day != "" {
			t.FirstHeard = day
		}
	}
	// 快照：只在有值的时候补上（后来的接口可能给得更全）。
	if in.Title != "" {
		t.Title = in.Title
	}
	if in.Artist != "" {
		t.Artist = in.Artist
	}
	if in.ArtistMid != 0 {
		t.ArtistMid = in.ArtistMid
	}
	if in.Kind == "local" {
		t.Local = true
	}
	return t
}

// artistStatLocked 拿（没有就建）某一个 UP 主的那一份。
func (tr *listeningTracker) artistStatLocked(mid int64, name string) *StatsArtistStat {
	a, ok := tr.artists[mid]
	if !ok {
		a = &StatsArtistStat{}
		tr.artists[mid] = a
	}
	if name != "" {
		a.Name = name
	}
	return a
}

// Snapshot 是一份只读的拷贝。
func (tr *listeningTracker) Snapshot() ListeningStats {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.snapshotLocked()
}

// snapshotLocked 是快照的实现（调用方已经持锁）。
func (tr *listeningTracker) snapshotLocked() ListeningStats {
	out := ListeningStats{
		Days:    make(map[string]*StatsDayStat, len(tr.days)),
		Tracks:  make(map[string]*StatsTrackStat, len(tr.tracks)),
		Artists: make(map[int64]*StatsArtistStat, len(tr.artists)),
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
		out.Days[day] = &cp
	}
	for k, v := range tr.tracks {
		t := *v
		out.Tracks[k] = &t
	}
	for k, v := range tr.artists {
		a := *v
		out.Artists[k] = &a
	}
	return out
}
