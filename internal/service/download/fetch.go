// 下载的那一半：探长度、对身份、发 Range 请求、往盘上写。
//
// # 🔴 整个引擎最要紧的一道关在这里
//
// `pullFrom` 里那句「不是 206 就中止」。B 站上游**完全没有这道检查**
// （`download-core.ts:395` 是全仓库唯一一处设 `Range` 的地方，grep
// `206` / `content-range` 零命中），而 `got` 默认只对非 2xx 抛错——
// 服务器回 200 在它是**成功**。于是服务器（或中间那个代理）不认 Range 时：
//
//	我们要第 20MB~25MB，它回 200 + 整个 25MB 的正文
//	→ 被当成这一块写进文件的 20MB 处
//	→ 文件长度还是「对得上」的（预分配截到了 25MB）
//	→ 歌能打开，播到 20MB 那个位置就断
//
// **长度对得上，所以没有人会去怀疑拼接。** 这就是任务里说的
// 「极难排查」。中止而不是拼接，是因为除了中止没有一个动作是安全的：
// 重下要丢掉已经下好的十几兆，退回单流下载又等于放弃并发 3。
//
// # 为什么并发 3 能安全地同时写一个文件
//
// 每个 worker 拿一个独立的 `*os.File` 句柄、用 `WriteAt`（pwrite）写自己
// 那一段。`os.File` 明确保证多个 goroutine 可以同时调它的方法，而
// `Write`（共享文件偏移）**不行**——那正是并发写同一个文件最经典的竞态。
// 这里的每一条写入都带绝对偏移，不依赖任何人的文件位置。

package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/service/mux"
)

// 取流和下载这一摊的哨兵错误。都拿 errors.Is 判。
var (
	// ErrNoSource 是「没有能下的地址」。
	ErrNoSource = errors.New("download: 没有能下的地址")

	// ErrRangeUnsupported 是「服务器不认续传」。
	//
	// 它表示服务器对带 `Range` 的请求回了 200 而不是 206（body 是整个
	// 文件）。**这时候只能中止**——理由见文件头。
	ErrRangeUnsupported = errors.New("download: 服务器不认续传（回的是 200 而不是 206）")

	// ErrRangeMismatch 是「服务器给的字节范围和要的不一样」。
	//
	// 比 `ErrRangeUnsupported` 更少见但更阴：状态码确实是 206，
	// 而 `Content-Range` 指的不是我们要的那一段（有些 CDN 会把请求
	// 重定向到另一个分片上）。照样不能写。
	ErrRangeMismatch = errors.New("download: 服务器给的字节范围和要的不一样")

	// ErrShortChunk 是「这一块没下全就断了」。
	//
	// **不算失败**：已经下完的其它分块留在账上，重试只会重来这一块。
	// 它只是让当前这一次停下来。
	ErrShortChunk = errors.New("download: 这一块没下全就断了")
)

// Source 是「这一段字节现在从哪儿下」。
//
// 每次开始或续传都要**重新问一遍**（`Manager.resolve`）：B 站的地址带
// `deadline`，实测两小时就失效，而一个下了一半的歌很可能下超过两小时。
// 拿着上次的地址续，续到一半必然 403。
type Source struct {
	Bvid string
	Cid  string
	// Quality 是 B 站自己的音质 id（`bili.Quality` 的数字形式）。
	Quality int
	// Label 是给人看的徽章文字，纯记录。
	Label string
	// Ext 是**地址上没带**扩展名时的后备（`.flac` / `.m4a`）。空串表示
	// 也没有，那时落盘就不带扩展名。
	//
	// 放在 Source 里而不是让 download 去猜容器：容器名是 app 层从取流
	// 结果里读的（`bili.StreamRef.Container`），download 不认识 B 站。
	Ext string
	// URLs 是同一段字节的多个地址：主地址在前，备用地址在后。
	URLs []string
	// CompanionURLs 是**第二那条流**的地址（视频下载里的音频）。
	//
	// **空 = 这一项只有一条流**，那是最常见的情况（音频下载），
	// `prepareCompanion` 也就不会被调。视频那一路是两条：dash 返回的
	// 视频流 + 音频流，各自下到自己的 `.part`，下完交织成一个 `.mp4`
	// （见 `runMux`）。
	CompanionURLs []string
}

// IdentityOf 把一次探测的结果凑成资源身份。
//
// ⚠️ 它**只包含源告诉我们的那几样**（是谁、哪一档、多长）。这一项
// 自己带着的那两样——`Video` 和 `VideoQ`——**不在这里**，而是由 `Task`
// 保住（见 `Task.adoptSource`）。
//
// ⚠️ 别把这两个字段加回来。它们**曾经**在这里，而那是个陷阱：
// `prepare` 每次都会 `t.Identity = src.IdentityOf(length)`（那是续传
// 身份校验的一部分），于是只要有一个 `Source` 忘了填，「这一项是不是
// 视频」「用户点的是哪一档」就**被抹成零值**了——
//
//	暂停一下再继续 → 取流看到 want.Video=false → 只问音频
//	                → 「音频流：没有能下的地址」
//
// 而这两种情况下界面上的那一行都不给任何提示：它只是再也下不完了。
func (s *Source) IdentityOf(length int64) Identity {
	return Identity{Bvid: s.Bvid, Cid: s.Cid, Quality: s.Quality, Length: length}
}

// run 把一个任务下完：探长度 → 对身份 → 铺分块 → 并发拉 → 收尾改名。
//
// 调用前：任务已经出队、状态是 running、锁已经放开。**返回 nil 就是下完了**
// （文件已经落在 `Task.Dest` 上）；返回错误时任务已经在队列里被标成
// `StatusError`，但已完成的分块还留着，重试只重来没下完的那些。
//
// 视频任务（`Task.Video`）多一段：两条流各自下完之后交织成一个 `.mp4`，
// 交织这一步的错**同样回到这一行**（见 `runMux`）——记录页上那一行的
// 红字因此是「交织」的真实原因，而不是「下完了」或者一句没头没尾的错。
func (m *Manager) run(ctx context.Context, id string) error {
	// 取流要的是这一项的**身份**（bvid / cid / 上一轮拿到的音质）。
	// 在锁下拷一份出来，后面全程不碰那张 map。
	want, ok := m.identityOf(id)
	if !ok {
		return ErrNotFound
	}
	src, err := m.resolve(ctx, want)
	if err != nil {
		return err
	}
	length, err := m.probeLength(ctx, src)
	if err != nil {
		return err
	}

	part, err := m.prepare(id, src, length)
	if err != nil {
		return err
	}
	if err := m.pullStream(ctx, id, false, part, src.URLs); err != nil {
		return err
	}

	if !m.hasCompanion(id) {
		// 只有一条流：下完就是它。半成品留在 `.part` 上，用户在下载
		// 目录里看到的就永远是完整的一首，不会有一首「打不开的歌」
		// 混在里面。
		if err := m.land(id, part); err != nil {
			return err
		}
	} else {
		// 第二那条流（音频）。**它和主那条流各自算进度、各自续传**——
		// 混在一起记的话「下了一半」这句话就没意义了（见 `Companion`）。
		clen, err := m.probeURLs(ctx, src.CompanionURLs)
		if err != nil {
			return fmt.Errorf("download: 音频流：%w", err)
		}
		cpart, err := m.prepareCompanion(id, src, clen)
		if err != nil {
			return err
		}
		if err := m.pullStream(ctx, id, true, cpart, src.CompanionURLs); err != nil {
			return err
		}
		if err := m.runMux(ctx, id, part, cpart); err != nil {
			return err
		}
	}

	// 封面跟着这一首存一份（`saveCover`）。**错只记日志、不往上交**：这一段
	// 跑的时候歌已经在 `Dest` 上了，把它算成「这一首下不了」是拿一整首歌去换一个
	// 几百 KB 的图，而界面上那一行的红字只有这一件事可说（见 cover.go）。
	//
	// ⚠️ **两条流都走到这里，而不是塞进上面那一个分支里**：视频那一路现在带
	// 第二条流（`Enqueue` 一定会给它建 `Companion`），所以「只在音频那一支
	// 存」的实现里那句「视频不存」就是一句没人会走到的空话——而哪天有人把
	// `Companion` 改成有伴流才建，MV 就开始存封面了，**没有任何测试会红**。
	// 放在这里，那道判断就真的在挡东西（`coverOf` 里），也有测试盯着它。
	//
	// ⚠️ 它在**落盘之后**、状态落成「完成」之前，所以用户看到「下载完成」的
	// 时候封面已经在那儿了——那正是存它要的（本地音乐库扫目录的那一刻）。
	// 代价是落盘到完成之间多了一次网络往返，所以 `runOne` 那个 `select` 必须
	// 偏向「后台已经回来了」那一支（见那里的注释）。
	if err := m.saveCover(ctx, id); err != nil {
		logCoverErr(id, err)
	}
	return nil
}

// hasCompanion 报这一项有没有第二那条流。**必须在锁下调**。
func (m *Manager) hasCompanion(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tasks[id]
	return t != nil && t.Companion != nil
}

// pullStream 把指定那条流下完。
//
// 和旧的那条路一模一样（探过的长度、铺好的 `.part`、三个并发），
// 唯一多出来的是「哪一条流」这个参数——它决定记账记在 `Task.Chunks`
// 还是 `Companion.Chunks` 上（见 `finishChunk`）。
func (m *Manager) pullStream(ctx context.Context, id string, companion bool, part string, urls []string) error {
	f, err := os.OpenFile(part, os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("download: 打开 %s：%w", part, err)
	}
	pullErr := m.pullAll(ctx, id, companion, f, urls)
	// ⚠️ 关句柄之前不能改名，也不能交织：Windows 上重命名一个还开着的
	// 文件会直接失败（POSIX 允许，所以这段在 macOS 上看不出来）。
	if cerr := f.Close(); pullErr == nil && cerr != nil {
		pullErr = fmt.Errorf("download: 关 %s：%w", part, cerr)
	}
	return pullErr
}

// destOf 拿这一项当前的 `Dest`。**必须在锁下调**。
func (m *Manager) destOf(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := m.tasks[id]; t != nil {
		return t.Dest
	}
	return ""
}

// restoreDest 把 `Dest` 放回它交织失败之前的样子。
//
// 用在 `runMux`：交织失败时那两条半成品留在**旧**路径上（见那里的四步），
// 而 `Dest` 被 `destToLand` 改到了新路径。不放回去的话，下一轮重试会
// 判成「半成品不见了」而从零重下几百兆。
//
// 只在**失败**那条路上调：成功时新名字才是对的（`Remove` / `Clear` 要删的
// 是新名字那两个）。
func (m *Manager) restoreDest(id, dest string) {
	if dest == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := m.tasks[id]; t != nil {
		t.Dest = dest
	}
}

// markMuxing 把状态落成「交织中」，并叫界面要一帧。
func (m *Manager) markMuxing(id string) {
	m.mu.Lock()
	t := m.tasks[id]
	if t != nil {
		t.Status = StatusMuxing
	}
	wake := m.wake
	m.mu.Unlock()
	if wake != nil {
		wake()
	}
}

// land 把半成品改名成最终落盘的那个文件。
func (m *Manager) land(id, part string) error {
	dest := m.destToLand(id)
	if err := os.Rename(part, dest); err != nil {
		return fmt.Errorf("download: 落盘 %s：%w", dest, err)
	}
	return nil
}

// runMux 把两条流交织成一个 MP4。
//
// # 交织出来的名字叫什么
//
// 产物写到 `<最终名>.part.mix`（`mixPartPath`）——**和两条源流的半成品
// 各占各的名字**，理由见 `mixPartPath` 那段（写成同一个路径会把正在
// 读的源流截断，出来的是「结构完全正常、画面全是花的」文件）。
//
// 交织完再改名：落盘前那一眼（那个位置上是不是已经有别人的东西了）
// 对视频同样要办（`destToLand`）。
//
// # 那两条源流怎么办
//
// 交织成功之后**删掉**。它们是 fMP4 分片流，用户点开是放不出来的，
// 而留着就是盘上凭空多出**两个和成片一样大**的文件——用户会以为下重了。
//
// 交织失败**不删**：它们是下载的成果，而续传要靠它们（`prepare` 认的是
// 这些路径和分块记账）。下一轮「重试」只要交织那一步的原因消失了，
// 几百 MB 就不用重下。跟音频那一票一个规矩：出错时半成品留在 `.part`
// 上，由 `Remove` / `Clear` 来删。
//
// # 错怎么报
//
// 交织的错**原样交上去**。它会落到记录页那一行的红字上，而那句话就是
// 「为什么这个 MV 没下下来」的真话（「两条流对不上，差了 7 秒」、
// 「视频流里一个关键帧都没有」），而不是一句笼统的「下载失败」。
func (m *Manager) runMux(ctx context.Context, id, videoPart, audioPart string) error {
	if err := ctx.Err(); err != nil {
		return err // 用户按了暂停：别在暂停之后还交织一遍
	}
	// ⚠️ `destToLand` **可能改 `Dest`**（那个位置上已经有别人的东西时
	// 让开）。而那两条半成品是按**改之前**那个 `Dest` 建出来的——交织失败
	// 时它们留在旧路径上，`Dest` 却指到了新路径。
	//
	// 后果是「几百 MB 白下」：下一轮「重试」走 `prepare` 的 else 分支，
	// `os.Stat` 新路径上的半成品当然不存在 → `resetChunks` → **两条流
	// 从零重下**，而旧路径上那两个几百 MB 成了孤儿（`Remove` / `Clear`
	// 只认当前 `Dest`，永远不会去删它们）。
	//
	// 所以失败时把 `Dest` **放回去**：让它和半成品在的地方始终一致。
	old := m.destOf(id)
	dest := m.destToLand(id)
	if dest == "" {
		return ErrNotFound
	}
	// 那一刻网络空出来了，但盘在动：状态落成「交织中」，界面上才不是
	// 「下完了卡住了」（见 `StatusMuxing`）。
	m.markMuxing(id)

	mix := mixPartPath(dest)
	if _, err := mux.Mux(ctx, videoPart, audioPart, mix); err != nil {
		m.restoreDest(id, old)
		return err
	}
	if err := os.Rename(mix, dest); err != nil {
		_ = os.Remove(mix)
		m.restoreDest(id, old)
		return fmt.Errorf("download: 落盘 %s：%w", dest, err)
	}
	// 成片已经在 `dest` 上了，那两条源流就是没人要的东西了。
	_ = os.Remove(videoPart)
	_ = os.Remove(audioPart)
	return nil
}

// destToLand 拿下这一项要落到的最终路径：那个位置上已经有东西了就避让。
//
// # 为什么落盘前还要再看一眼
//
// `Dest` 是**入队那一刻**定的，而那一刻到下载完之间世界会变。`destFor`
// 在挑名字时已经避开了「队列里别的任务认领的名字」和「盘上的 `.part`」，
// 但它避不开**这之后**才发生的事：另一首同名歌下完了、或者用户自己往那儿
// 丢了个文件。
//
// 而 `os.Rename` 在目标存在时是**直接盖掉**的（POSIX）。盖掉的后果不是
// 这一首出错，是**另一首从界面上消失**：队列里它那条还写着
// 「已完成 41943040/41943040」，而那个文件现在装的是别的歌——目录里只剩
// 一个文件，界面上一切正常。宁可换一个名字落，也不能静默吃掉别人的歌。
//
// 换个名字是安全的：分块已经全在 `.part` 里了，改名不碰一个字节。
func (m *Manager) destToLand(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	t := m.tasks[id]
	if t == nil || t.Dest == "" {
		return ""
	}
	// 我们自己下出来的东西只会是 `.part`——`.part` 在 `run` 里单独处理，
	// 而我们不会去建 `Dest`。所以这里存在的东西**不是我们的**。
	if !exists(t.Dest) {
		return t.Dest
	}
	dest := m.destFor(filepath.Dir(t.Dest), t)
	t.Dest = dest
	return dest
}

// prepare 探完长度之后要做的事：对身份、校验半成品、把文件铺到该有的长度。
//
// 返回 .part 的路径。任务在锁下现查现改——它同时是别的 goroutine
// （界面读快照、用户点删除）在碰的东西。
func (m *Manager) prepare(id string, src *Source, length int64) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	t := m.tasks[id]
	if t == nil {
		return "", ErrNotFound
	}
	part := partPath(t.Dest)

	// 🔴 续传前必须校验资源身份。对不上就**清空分块记账**、从 0 重下：
	// 盘上那些字节是**旧内容**的，续上去拼出来的是一个长度对得上、
	// 内容对不上的坏文件（见 `Identity` 的注释）。
	if !t.Identity.SameResource(src.IdentityOf(length)) {
		t.resetChunks()
		// ⚠️ 伴流也要清。资源身份变了 = 这一稿换了内容，而**两条流都是
		// 旧内容的**。只清主流的话伴流那几百 KB 会**接着往下拼**——
		// 出来的是一个音画来自两个不同版本的文件，能播、播一会儿就没声
		// 了，而两条流各自的长度都「对得上」。
		if t.Companion != nil {
			t.Companion.Chunks = nil
		}
	}
	t.adoptSource(src, length)
	if t.Label == "" {
		t.Label = src.Label
	}
	if len(t.Chunks) == 0 {
		// 还没下过一个字节：扩展名按**地址上真的后缀**重算（入队时那个
		// `.m4a` 只是临时值，那一刻还没有地址），落盘名跟着重算，顺带
		// 再避让一次同名文件——入队到开始之间用户可能已经下完了一首同名
		// 的。**下过之后这两样都不能改。**
		if e := extFor(t, src); e != "" {
			t.Ext = e
		}
		dest := m.destFor(filepath.Dir(t.Dest), t)
		if dest != t.Dest {
			// 落盘名变了（这一轮取流的扩展名和上一轮不同，或者同名被别的
			// 歌占了）：把旧名字下的半成品删掉。
			//
			// 不删的话它就是**孤儿**——`Remove` / `Clear` 只删当前 `Dest`
			// 那一个，于是那几百 MB 永远留在用户的下载目录里，界面上再也
			// 没有任何东西指向它。（能走到这里说明分块记账已经空了：资源
			// 身份对不上，或者上一轮一块都没下完而 `.part` 已经预分配了。）
			//
			// ⚠️ **两个半成品都要删**。视频有两个，只删主那个的话伴流那几百 KB
			// 变成孤儿——`Remove` / `Clear` 只认新名字下那两个。
			_ = os.Remove(partPath(t.Dest))
			_ = os.Remove(companionPartPath(t.Dest))
		}
		t.Dest = dest
		part = partPath(t.Dest)
	} else {
		// 账上有进度，盘上却对不上：文件被删了、被用户截短了、或者目录换了。
		// 那那些字节已经没了，继续下会得到一个中间有洞的文件——洞在
		// m4a 里就是「播一半就断」。所以重来。
		st, err := os.Stat(part)
		if err != nil || st.Size() < length {
			t.resetChunks()
			part = partPath(t.Dest)
		}
	}

	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return "", fmt.Errorf("download: 建 %s：%w", part, err)
	}
	// 铺到该有的长度：磁盘上是稀疏的，没下到的那些位置读出来是零。
	// 有了这个长度，每个分块的位置就是确定的，续传时的下标才对得上。
	if err := f.Truncate(length); err != nil {
		f.Close()
		return "", fmt.Errorf("download: 铺 %s 到 %d 字节：%w", part, length, err)
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return part, nil
}

// prepareCompanion 是 `prepare` 的第二那条流那一版。
//
// 和主那条流同款的三件事，**一件都不能少**：资源身份对不上就清空记账
// 从头下、盘上的东西比总长短就重来、把文件铺到该有的长度（并发 3 的
// 每一条写入都带绝对偏移，而偏移是按总长算出来的——不铺的话各分块
// 的位置全指错，出来的是一个长度对得上、内容全是错的文件）。
func (m *Manager) prepareCompanion(id string, src *Source, length int64) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	t := m.tasks[id]
	if t == nil || t.Companion == nil {
		return "", ErrNotFound
	}
	part := companionPartPath(t.Dest)

	// 身份对不上：本地那些字节是**旧内容**的（见 `Identity`）。
	if t.Companion.Length > 0 && t.Companion.Length != length {
		t.Companion.Chunks = nil
	}
	t.Companion.Length = length

	// 账上有进度，盘上却对不上：文件被删了、被用户截短了、或者目录换了。
	// 那那些字节已经没了，继续下会得到一个中间有洞的文件——**洞在
	// fMP4 里就是「交织出来少几帧」**，而那种文件照样能播，只是结尾
	// 突然断掉。所以重来。
	if len(t.Companion.Chunks) > 0 {
		if st, err := os.Stat(part); err != nil || st.Size() < length {
			t.Companion.Chunks = nil
		}
	}

	// 落盘名变了就删掉旧名字下的半成品——`prepare` 里那四步孤儿场景
	// 在这儿一模一样地成立。
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return "", fmt.Errorf("download: 建 %s：%w", part, err)
	}
	if err := f.Truncate(length); err != nil {
		f.Close()
		return "", fmt.Errorf("download: 铺 %s 到 %d 字节：%w", part, length, err)
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return part, nil
}

// extFor 决定这一首落盘用什么扩展名。
//
// 顺序是**地址 > 来源说的 > 入队时的临时值**。地址排第一是因为任务里
// 点名了这一条：「B 站返回的 URL 后缀就是 `.m4a` / `.flac`，按扩展名直接
// 落盘」——地址是 B 站自己给的、最不会错的那一份。
// `Source.Ext`（app 层从取流结果里的容器名换算来的）只在地址没带后缀时
// 顶上；而入队时的 `.m4a` 排在最后，因为它只是个猜测。
func extFor(t *Task, src *Source) string {
	// ⚠️ **视频这一票不走「地址后缀」这条路**。两条 dash 流的地址后缀
	// 都是 `.m4s`，而落盘的不是任何一条流——是**交织出来的那个 MP4**。
	// 按地址算的话会落一个 `歌.m4s`：文件名在、内容对（确实是 fMP4），
	// 而双击打不开。
	if t.Video {
		return videoExt
	}
	if len(src.URLs) > 0 {
		if e := ExtOf(src.URLs[0]); e != "" {
			return e
		}
	}
	if src.Ext != "" {
		return src.Ext
	}
	return t.Ext
}

// pullAll 并发把没下完的分块拉下来。
//
// **一个分块失败就整体停下来**，不去等别的分块跑完：留着那些已完成的
// 记账，用户点「重试」时只重来没下完的几块，不亏。
func (m *Manager) pullAll(ctx context.Context, id string, companion bool, f *os.File, urls []string) error {
	pending := m.pendingChunks(id, companion)
	if len(pending) == 0 {
		return nil
	}

	// 每个 worker 一个 ctx 派生的可取消 ctx：任何一个分块失败，
	// 其余的在飞的请求都要立刻断，否则用户点了取消之后网络还在跑。
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int)
	var wg sync.WaitGroup
	var once sync.Once
	var failure error

	noteFailure := func(err error) {
		once.Do(func() {
			failure = err
			cancel()
		})
	}

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				// 投喂那一侧在取消之后仍可能塞进来一个（`select` 两个
				// case 都ready 时是随机的），所以这里自己再判一次：
				// 不判的话这一块会真的建出一个请求、连上、发出去，
				// 然后被 ctx 掐掉——用户点暂停之后网络还动了一下。
				if ctx.Err() != nil {
					return
				}
				start, end, ok := m.chunkBounds(id, companion, i)
				if !ok {
					continue
				}
				if err := m.pullChunk(ctx, id, companion, f, urls, start, end); err != nil {
					noteFailure(err)
					return
				}
				m.finishChunk(id, companion, i)
			}
		}()
	}

	// 投喂的那一侧要认取消：第一个分块失败之后不该再把剩下的塞进去。
	go func() {
		defer close(jobs)
		for _, i := range pending {
			select {
			case jobs <- i:
			case <-ctx.Done():
				return
			}
		}
	}()

	wg.Wait()
	if failure != nil {
		return failure
	}
	// 没有任何一块失败，但 ctx 已经被取消了：那是用户按了暂停或者应用
	// 在退出。**照样把错误交上去**——上层（`runOne`）认得出
	// `context.Canceled`，会把它算成「暂停」而不是「出错」，而这里自己
	// 判断不了是不是「本来就没东西可下」。
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// pullChunk 拉一个分块，主地址不行就换备用的。
//
// 换备用地址只对**地址类**的错误有意义：403、连接断了。服务器不认
// Range（`ErrRangeUnsupported`）或者给的段不对（`ErrRangeMismatch`）是
// **后端**的脾气，几个 CDN 节点背后是同一套，换节点只是白费一个请求。
func (m *Manager) pullChunk(ctx context.Context, id string, companion bool, f *os.File, urls []string, start, end int64) error {
	if len(urls) == 0 {
		return ErrNoSource
	}
	var last error
	for _, u := range urls {
		err := m.pullFrom(ctx, id, companion, f, u, start, end)
		if err == nil {
			return nil
		}
		last = err
		if ctx.Err() != nil {
			return err // 取消了就别再试下一个地址
		}
		if errors.Is(err, ErrRangeUnsupported) || errors.Is(err, ErrRangeMismatch) {
			return err
		}
	}
	return last
}

// pullFrom 从一个地址拉一个分块，写进 f 的 [start, end]。
//
// # 🔴 206 检查
//
// 带了 `Range` 的请求只有 206 才是我们要的东西。回 200 意味着服务器
// 把**整个文件**当正文发过来了，而这一块的位置在文件的中间——照写就是
// 把整首歌的字节盖在文件中间，**长度还是对得上**。中止，别拼。
func (m *Manager) pullFrom(ctx context.Context, id string, companion bool, f *os.File, rawURL string, start, end int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("download: 发请求：%w", err)
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("download: 拉 %d-%d 字节：%w", start, end, err)
	}
	defer resp.Body.Close()

	// 先看状态码，而且要在 206 那道关**之前**看：403 落到那道关里
	// 会被说成「服务器不认续传」，那句话对用户没有用——403 的真正
	// 意思是「这条地址过期了」，而换个备用地址就成了。
	if resp.StatusCode >= 400 {
		return statusErr(resp)
	}
	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("%w：要的是 %d-%d 字节，服务器回了 HTTP %d，正文是整个文件", ErrRangeUnsupported, start, end, resp.StatusCode)
	}
	// 回了 206 也要看 Content-Range：有的服务器 206 给的却是另一段
	// （CDN 把请求重定向到别的分片上时见过）。状态码对了不等于段对了。
	if got, ok := parseContentRange(resp.Header.Get("Content-Range")); !ok || got != start {
		return fmt.Errorf("%w：要的是从第 %d 字节开始，服务器给的 Content-Range 是 %q", ErrRangeMismatch, start, resp.Header.Get("Content-Range"))
	}

	// 恰好 start..end 这么多个字节。少了是这一块没下全（下面单独报），
	// 多了说明这个服务器没听 Range——但那已经被上面那道关挡掉了。
	//
	// 包一层 countingReader：进度条要**边下边走**。只在一整块下完之后才
	// 报的话，一个 30 MB 的歌在 10 MB 一块、网速一般的情况下，进度条
	// 会先在 0% 站十几秒，然后一下跳到 33%——用户看到的是「卡住了」。
	n, err := io.CopyN(&writerAt{f: f, off: start}, m.counting(id, resp.Body), end-start+1)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%w：%d-%d 只到了 %d 字节", ErrShortChunk, start, end, n)
		}
		return fmt.Errorf("download: 读 %d-%d 字节：%w", start, end, err)
	}
	return nil
}

// counting 包一层，每读到一批字节就报一次进度。
//
// 报上去的是**原始**字节数，不含已完成分块的记账——`noteProgress` 只
// 关心「该叫界面要一帧了没有」，而进度本身是从分块记账算出来的
// （`SnapshotOf`）。这里如果顺手把进行中那一块的字节也加进 `Done`，
// 进度条就会在三个分块之间来回跳（见 `Task.Done` 的注释）。
func (m *Manager) counting(id string, r io.Reader) io.Reader {
	return &countingReader{r: r, onRead: func(int64) { m.noteProgress(id) }}
}

// countingReader 是那个包一层。
type countingReader struct {
	r      io.Reader
	onRead func(int64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 && c.onRead != nil {
		c.onRead(int64(n))
	}
	return n, err
}

// writerAt 把 io.WriterAt 包成 io.Writer，给 io.CopyN 用。
//
// `io.CopyN` 只认 io.Writer，而并发分块要写同一个文件的**不同位置**。
// 这里必须走 WriteAt（pwrite，位置由参数决定）而不是 Write（共用文件
// 偏移）——后者是并发写同一个文件最经典的竞态，而且它错起来是**内容错**，
// 不是报错。
type writerAt struct {
	f   *os.File
	off int64
}

func (w *writerAt) Write(p []byte) (int, error) {
	n, err := w.f.WriteAt(p, w.off)
	w.off += int64(n)
	return n, err
}

// probeLength 问这条流一共多少字节。
//
// 先只问 1 个字节（`Range: bytes=0-0`）：206 的 `Content-Range` 里带着
// 总长，而正文只传一个字节。**不发 Range 问整个文件的头**是退路——
// 有些服务器对 Range 一律回 200，那一发只有响应头是可用的。
//
// 两个地址都问一遍：第一个 403（过期了）就换第二个。
func (m *Manager) probeLength(ctx context.Context, src *Source) (int64, error) {
	return m.probeURLs(ctx, src.URLs)
}

// probeURLs 从一组地址问总长（主地址不行就换备用）。
func (m *Manager) probeURLs(ctx context.Context, urls []string) (int64, error) {
	if len(urls) == 0 {
		return 0, ErrNoSource
	}
	var last error
	for _, u := range urls {
		n, err := m.probeOne(ctx, u)
		if err == nil {
			return n, nil
		}
		last = err
		if ctx.Err() != nil {
			return 0, err
		}
	}
	return 0, last
}

// probeOne 从一个地址问总长。
func (m *Manager) probeOne(ctx context.Context, rawURL string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", "bytes=0-0")

	resp, err := m.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("download: 探长度：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, statusErr(resp)
	}

	// ⚠️ **必须按状态码分岔，不能「Content-Range 认不出来就退回
	// Content-Length」**。
	//
	// 206 意味着服务器认了 Range，而分块规划只认 `Content-Range` 里那个
	// 总长。RFC 7233 允许它写成 `*`（「实例长度未知」，第 4.4 节），这时
	// **不能退回 `Content-Length`**：这一发只问了 `bytes=0-0`，所以
	// Content-Length 说的是「这一个字节的长度」。
	//
	// 退回会发生什么：总长=1 → 只规划出一个分块 `bytes=0-0` → 下载
	// 「成功」→ 落盘一个 1 字节的 `.m4s` → 状态报「已完成」。一首 30 MB
	// 的歌变成 1 字节，而歌名、大小、状态全都看着像那么回事——这是这一票
	// 里第二类「错了不报错」的东西（第一类是 200 拼出来的坏文件）。
	//
	// 宁可报错：这一首下不了，用户看到一句能看懂的话；下出一首 1 字节的
	// 歌，用户要过很久才发现「怎么只放了两秒」。
	if resp.StatusCode == http.StatusPartialContent {
		if _, total, ok := parseContentRangeFull(resp.Header.Get("Content-Range")); ok && total > 0 {
			return total, nil
		}
		return 0, fmt.Errorf("download: 服务器不肯说这条流有多长（Content-Range %q）——分块要靠总长规划，不猜", resp.Header.Get("Content-Range"))
	}

	// 200：服务器没认 Range，回的是整个文件，`Content-Length` 就是总长。
	// 正文一个字都不读——`defer Close` 只关连接，不会把几十兆读下来。
	if resp.ContentLength > 0 {
		return resp.ContentLength, nil
	}
	return 0, fmt.Errorf("download: 问不出这条流有多长（Content-Range %q，Content-Length %d）",
		resp.Header.Get("Content-Range"), resp.ContentLength)
}

// parseContentRange 从 `bytes 12-99/1234` 里读出起点。
func parseContentRange(v string) (start int64, ok bool) {
	start, _, ok = parseContentRangeFull(v)
	return start, ok
}

// parseContentRangeFull 从 `bytes 12-99/1234` 里读出起点和**总长**。
//
// 只认 `bytes .../...` 这一种写法（RFC 7233 允许多区间 `bytes 0-9,20-29/…`，
// 那种这里当成认不出来——B 站不给，而真给了的话照拼只会拼出坏文件）。
// 总长是 `*` 时当成没有（服务器不肯说长度）。
func parseContentRangeFull(v string) (start, total int64, ok bool) {
	v = strings.TrimSpace(v)
	rest, ok := strings.CutPrefix(v, "bytes ")
	if !ok {
		return 0, 0, false
	}
	rangePart, sizePart, ok := strings.Cut(rest, "/")
	if !ok {
		return 0, 0, false
	}
	first, _, ok := strings.Cut(rangePart, "-")
	if !ok {
		return 0, 0, false
	}
	start, err := strconv.ParseInt(strings.TrimSpace(first), 10, 64)
	if err != nil || start < 0 {
		return 0, 0, false
	}
	total, err = strconv.ParseInt(strings.TrimSpace(sizePart), 10, 64)
	if err != nil || total < 0 {
		// `bytes 0-0/*`：服务器不说总长。只还起点，总长交给调用方另外问。
		return start, 0, true
	}
	return start, total, true
}

// statusErr 把状态码变成一句能对人说的话。
func statusErr(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusForbidden:
		return fmt.Errorf("download: 服务器不给下这一段（403，多半是地址过期了）")
	case http.StatusNotFound:
		return fmt.Errorf("download: 服务器上没有这一段（404）")
	case http.StatusGone:
		return fmt.Errorf("download: 这一段已经不在了（410）")
	}
	return fmt.Errorf("download: 服务器回了 HTTP %d", resp.StatusCode)
}
