// 多文件一致性事务：升级要么全成，要么全不成。
//
// # 为什么原子写不够
//
// 设置、快捷键、搜索历史、下载队列是**四份**独立的文件，各自有 schema 号、各自
// 有迁移链（见 `settings.go` 与 `internal/ui/shortcuts.go`）。单份文件的
// 「临时文件 + rename」只保证「写到一半断电不会留下半个文件」，它管不了：
//
//   - 升到一半断电，四份文件停在**不同版本**上。设置已经是 v2、队列还是 v1 的
//     时候启动，读出来的东西是拼起来的，没有哪一份能说清「这是哪个状态」。
//   - 中间那一步失败。写完三份、第四份崩了，用户的设置改了一半。
//
// 做法是一个**总控标记文件**（`data-version.json`）：升级开始时先把所有涉及的文件
// 各自备份一份，写下「正在从 vN 升到 vN+1」，四份都迁完才把标记改成新版本。
// 任何一步失败就把四份备份全部恢复回去，**原来的数据一个字节都不少**——
// 「迁移失败保留旧文件」不是「迁移失败不写」，是「写回去」。
//
// 启动时看到未完成的标记就重跑一遍：进程是在升级中间死的，那四份文件还停在
// 上一版，重跑是最自然的事，也比「怎么判断上一次升到哪儿了」可靠。
//
// # 这一层不认识任何一份文件的内容
//
// 它只管「备份谁、什么时候删备份、失败了怎么恢复」。哪一份文件第几版、怎么从
// 第 N 版升到第 N+1 版，是那份文件自己的事（`settings.go`、`internal/ui/
// shortcuts.go`、`internal/service/download/store.go` 各自有）。所以这个包里
// 没有任何一份文件的结构，也因此不 import `internal/ui`——依赖是单向的
// （见 `settings_mount.go` 文件头那条约定）。
//
// # 降级快照
//
// 用户把应用换回旧版时，旧版读到新版的文件要么只读（各 store 自己判，见
// `ErrSettingsFromNewer`），要么干脆读不懂。有一份**上一版的完整副本**就能换回去。
// 升级成功时把迁移前的备份挪到 `data-snapshots/<原文件名>.v<那一版>.bak`
// （覆盖同名，不累积），用户降级时把它拷回原位即可。名字里的版本号是**这一份
// 文件自己**的版本，不是总版本号（`toSnapshot` 的注释说了为什么这两个会不一样）。
//
// 为什么留快照而不是「新版本写出旧版也读得懂的子集」：新版本加的键没法凭空藏起来
// （Biu 那边还要靠这些键双向兼容），而「子集」这个承诺只要哪一次迁移顺手多写了一个
// 键就破了，而且破了没人发现。
package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// UpgradeFileName 是总控标记文件的名字。
	//
	// 名字里没有「设置」两个字，是有意的：它管的是**全部**用户数据（设置、快捷键、
	// 搜索历史、下载队列）的版本，不只是设置那一份。叫 `settings-version.json`
	// 会让人以为它只管设置，而多文件一致性恰恰是「别把它当成只管一份」。
	UpgradeFileName = "data-version.json"

	// UpgradeSnapshotDir 是降级快照摆哪儿的（相对用户数据目录）。
	UpgradeSnapshotDir = "data-snapshots"

	// CurrentDataVersion 是**这一版**能读能写的总版本。
	//
	// 数据目录里的任何一份文件改了结构、搬了家，都要涨它，并往 `Upgrade` 里加
	// 相应那一份的迁移。它是整票的版本号，不是设置的（设置自己有
	// `SchemaVersion`，那个数管的是文件形状，这里管的是**哪几份文件已经升过了**）。
	//
	// ⚠️ 它必须 ≥ 每一份文件自己的 schema 号，否则会出现「标记说升过了，但某一份
	// 自己的迁移链还没走完」这种说法不通的状态。
	CurrentDataVersion = 1
)

// upgradeMarker 是总控标记文件的内容。
//
// Version 是**已经完成**到的版本。Pending 是正在做的那一次升级，只有在升级
// 过程中才是非空；正常状态下它是 null。
type upgradeMarker struct {
	Version int          `json:"version"`
	Pending *pendingStep `json:"pending"`
}

// pendingStep 是一次没做完的升级。
type pendingStep struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// ErrUpgradeFromNewer 是总控标记来自更新的版本。
//
// 和 `ErrSettingsFromNewer` 同一个道理、同一副后果：**一个字都不写回去**。
// 用户把应用换回旧版时这一版就是这个错，而它下面还挂着四份各自的只读判断
// （各 store 自己判）。这里单独报一次，是为了让用户看到的是「整个数据目录来自更新的
// 版本」而不是四条各自为政的提示。
var ErrUpgradeFromNewer = errors.New("app: 数据目录来自更新的版本，本次一个字都不写回去")

// Tx 是一次升级。
//
// **拿到它的人负责收尾**：要么 `Commit`（全成），要么 `Rollback`（全不成）。
// 两个都不调就把 Tx 扔了的话，备份文件会留在盘上——不致命（下次 `Stage` 会重写
// 它），但用户目录里会多出几个说不清是什么的文件。
//
// 零值不可用，用 `BeginUpgrade`。
type Tx struct {
	dir    string
	marker upgradeMarker

	// backups 是这一轮已经备份过的文件，按 Stage 的顺序。回滚时**倒着**恢复：
	// 前一份恢复的动作不该看到后一份已经改过的内容。
	backups []*backup

	// touched 报这份标记文件这次有没有被写过。回滚一个「什么都没升级」的 Tx
	// 不该动盘。
	touched bool
}

// backup 是一份文件的迁移前副本。
//
// 进这个表的一定是**盘上真有备份文件**的那一份：Stage 时文件不存在的那些直接
// 不入表（见 `Stage` 里那一段）。
type backup struct {
	// file 是原文件。
	file string
	// snap 是备份落在哪。Stage 时写在原文件旁边，Commit 时挪进快照目录。
	snap string
}

// BeginUpgrade 开一次升级，读总控标记。
//
// 标记文件不存在不是错误：那是第一次运行，`Version` 按 0 算（每一份文件都当
// v0 读，Biu 那边导出的裸文件走的正是这一档，见 `settings.go` 的 `splitEnvelope`）。
//
// 标记来自**更新的**版本时返回 `ErrUpgradeFromNewer`，且 Tx 不可用：这一版不碰
// 这个目录里的任何一份文件。
func BeginUpgrade(dir string) (*Tx, error) {
	t := &Tx{dir: dir}
	m := upgradeMarker{}
	b, err := os.ReadFile(filepath.Join(dir, UpgradeFileName))
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &m); err != nil {
			// 标记读不出来 = 我们不知道哪几份文件升过了。这种情况下**不能**当成
			// 「还没升过」去重跑一遍：有一份可能已经是 v2 了，重跑等于把 v2 的内容
			// 按 v1 的迁移再走一遍。所以只备份留存并报错，让用户看到。
			backupFile(filepath.Join(dir, UpgradeFileName))
			return nil, fmt.Errorf("app: 总控标记 %s 读不懂，已留存一份 .bak：%w", UpgradeFileName, err)
		}
	case errors.Is(err, os.ErrNotExist):
		m.Version = 0
	default:
		return nil, fmt.Errorf("app: 读总控标记：%w", err)
	}

	if m.Version > CurrentDataVersion {
		return nil, fmt.Errorf("%w：目录是 v%d，我们只到 v%d",
			ErrUpgradeFromNewer, m.Version, CurrentDataVersion)
	}
	t.marker = m
	return t, nil
}

// Version 是这份目录已经被声明成第几版。
func (t *Tx) Version() int { return t.marker.Version }

// Pending 报上一次升级是不是没做完。
//
// 是的话，调用方应该在**开任何窗口之前**重跑一遍整条迁移链——数据还停在上一版，
// 而上一版的文件全都还在备份里（见 `Rollback` 的注释）。
func (t *Tx) Pending() bool { return t.marker.Pending != nil }

// Stage 把一份文件纳入这一轮升级：先备份，再让调用方去读它、迁它。
//
// 返回的 *Backup 交给调用方在**改完那个文件之后**处置：改成功 `Commit`（这份的
// 备份挪进快照目录），失败 `Rollback`（或直接让整个 Tx 回滚，这份也在内）。
//
// 这份文件不需要迁（总标记已经是最新版）时返回 nil 和 nil——调用方照常读它，
// 只是不用备份、不用改标记。
//
// 一份文件被 Stage 两次会拿到**同一个**备份句柄，不会重新备份一次：第二次备份
// 拷的是「已经被迁移过的那一份」，而回滚要的是迁移**前**的内容。
func (t *Tx) Stage(file string) (*Backup, error) {
	if t.marker.Version >= CurrentDataVersion && !t.Pending() {
		// 已经升到最新：这一份不用管。
		return nil, nil
	}
	if file == "" {
		return nil, nil
	}
	if b := t.find(file); b != nil {
		return &Backup{tx: t, b: b}, nil
	}

	// 备份写在原文件旁边：同一个目录才能保证 rename 在同一卷上（跨卷的 rename
	// 会退化成拷贝加删除，中途失败就是两份都没有）。
	snap := filepath.Join(filepath.Dir(file), "."+filepath.Base(file)+".migrating")

	b, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		// 这个文件本来就不在（第一次运行）。**不进 backups、不给句柄**：
		// 没有东西可备份，回滚时也就没什么可恢复的；而把它记进 backups 会让
		// 回滚去**删掉**这个文件——那份文件可能是升级期间被别的进程建出来的，
		// 我们没备份过它，就没有资格处置它。
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("app: 读 %s 以便备份：%w", file, err)
	}
	ent := &backup{file: file, snap: snap}
	if err := os.WriteFile(snap, b, 0o644); err != nil {
		return nil, fmt.Errorf("app: 备份 %s 失败：%w", file, err)
	}

	t.backups = append(t.backups, ent)
	if err := t.markPending(); err != nil {
		return nil, err
	}
	return &Backup{tx: t, b: ent}, nil
}

// find 是这一份文件有没有已经 Stage 过。
func (t *Tx) find(file string) *backup {
	for _, b := range t.backups {
		if b.file == file {
			return b
		}
	}
	return nil
}

// markPending 写下「正在从 vFrom 升到 vTo」。
func (t *Tx) markPending() error {
	from := t.marker.Version
	if t.marker.Pending == nil {
		t.marker.Pending = &pendingStep{From: from, To: CurrentDataVersion}
	}
	return t.writeMarker()
}

// writeMarker 原子写标记文件。
func (t *Tx) writeMarker() error {
	b, err := json.MarshalIndent(t.marker, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(t.dir, UpgradeFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(path, b); err != nil {
		return fmt.Errorf("app: 写总控标记：%w", err)
	}
	t.touched = true
	return nil
}

// Commit 全部升完了：把总标记推到最新版，清掉 pending，把迁移前的备份挪进
// 快照目录（供降级时换回去）。
//
// 挪**而不是删**是给「旧版本回退」留的那条路（见文件头）。挪完之后的快照是覆盖
// 式的：升 v1→v2 存一份 `.v1.json`，将来升 v2→v3 覆盖成 `.v2.json`——用户目录里
// 不会越攒越多，而「上一版」这个保证一直在。
func (t *Tx) Commit() error {
	for _, b := range t.backups {
		if err := b.toSnapshot(t.dir, b.ownVersion()); err != nil {
			return err
		}
	}
	t.marker.Version = CurrentDataVersion
	t.marker.Pending = nil
	if err := t.writeMarker(); err != nil {
		return err
	}
	t.backups = nil
	return nil
}

// Rollback 升级失败：把每一份备份**倒着**恢复回去，总标记回到升级前的样子。
//
// 「倒着」是因为前面一份的恢复不该看到后面一份已经改过的内容——哪怕改的那些
// 都是彼此无关的文件，顺序一致这件事在有人加了跨文件校验之后不需要再改一次。
//
// 恢复的是**内容**（备份拷回原位），不是「删掉新文件」：一份 v2 的设置文件被换成
// 一份 v1 的，等于这次升级对用户等于没发生过，而且他们下次升级还会重来一遍。
func (t *Tx) Rollback() error {
	var firstErr error
	for i := len(t.backups) - 1; i >= 0; i-- {
		if err := t.backups[i].restore(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if t.touched {
		if t.marker.Version == 0 {
			// 这次升级之前根本没有标记文件：把它删掉，回到「从没升过」的样子。
			// 留着的话下一次会看到一个 Version=0 的标记和一堆 .v1 快照，
			// 而真相是「这里什么都没发生过」。
			_ = os.Remove(filepath.Join(t.dir, UpgradeFileName))
		} else if err := t.writeMarker(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	t.backups = nil
	return firstErr
}

// Backup 是一份已经被 Stage 进来的文件的备份句柄。
type Backup struct {
	tx *Tx
	b  *backup
}

// Commit 这一份改好了。备份挪进快照目录。
func (bk *Backup) Commit() error {
	if bk == nil || bk.b == nil {
		return nil
	}
	return bk.b.toSnapshot(bk.tx.dir, bk.b.ownVersion())
}

// Rollback 这一份改砸了。把它自己恢复回去（不动别的）。
func (bk *Backup) Rollback() error {
	if bk == nil || bk.b == nil {
		return nil
	}
	return bk.b.restore()
}

// toSnapshot 把备份挪到 `data-snapshots/<原文件名>.v<那一版>.bak`，
// 并把**同一份文件的更旧那份**删掉。
//
// ver 是**这一份文件自己**迁移前自称的版本，**不是**整轮升级之前那个总版本号。
//
// 这两者在「一个老用户第一次装上带总标记的这一版」时不一样：那时还没有
// `data-version.json`，总版本号记成 0，于是四份文件明明已经是当前形状的 v1，快照
// 却会被命名成 `.v0`——**名字在说谎**，而降级时用户正是照着这个名字去挑的。
//
// ## 为什么每份文件只留最近一份
//
// 名字里的版本号既然是这一份文件自己的，它就**单调递增**：`v0` → `v1` → `v2`
// 各是一个不同的文件名，「覆盖同名」这个做法已经不成立了（那是在名字还是总版本号
// 时的事）。
//
// 那就让每一份文件**只有一份**快照：新的一份来了就把它之前那份删掉。用户要的是
// 「**上一版**的数据」——从 v4 降回 v3 只需要 v3 时期的那一份，`.v1.bak` 对他是
// 死重量；而留着它们，用户目录里每升一版就多几份文件，几年后没人说得清哪份是哪
// 份。
func (b *backup) toSnapshot(dir string, ver int) error {
	dirSnap := filepath.Join(dir, UpgradeSnapshotDir)
	if err := os.MkdirAll(dirSnap, 0o755); err != nil {
		return fmt.Errorf("app: 建快照目录：%w", err)
	}
	// 扩展名是 `.bak` 不是 `.json`：备份是「上一版留下来的东西」，不是
	// 「本应用会读的配置」。用户一眼能看出它不属于哪一版。
	base := filepath.Base(b.file)
	name := fmt.Sprintf("%s.v%d.bak", base, ver)
	dst := filepath.Join(dirSnap, name)
	if err := os.Rename(b.snap, dst); err != nil {
		// 跨卷时 rename 会失败（备份和快照目录理论上在同一卷，但保险起见退化成
		// 拷贝）。留快照失败不是升级失败：数据已经是新版的了，为一个只服务于降级
		// 的副产物把整轮升级回滚，用户拿到的是一份不必要地退回去的旧数据。
		raw, rerr := os.ReadFile(b.snap)
		if rerr != nil {
			return nil
		}
		if werr := writeFileAtomic(dst, raw); werr != nil {
			return nil
		}
		_ = os.Remove(b.snap)
	}
	dropOlderSnapshotsOf(dirSnap, base, name)
	writeSnapshotReadme(dirSnap)
	return nil
}

// dropOlderSnapshotsOf 删掉同一个文件名的其它快照，只留 keep 这一份。
//
// 删不掉就算了：那是一个纯副产物，留着只是多几个文件，而为它把整轮升级报成失败
// 是更糟的。
func dropOlderSnapshotsOf(dir, base, keep string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if name == keep || !strings.HasPrefix(name, base+".") || !strings.HasSuffix(name, ".bak") {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}

// ownVersion 报**这一份文件**自己自称是第几版。读不出来或没有版本键时是 0。
//
// 函数是 `Stage` 的接收者，因为备份文件就在原文件旁边，读备份和读原文件一样。
func (b *backup) ownVersion() int {
	raw, err := os.ReadFile(b.snap)
	if err != nil {
		return 0
	}
	var probe struct {
		SchemaVersion *int `json:"schemaVersion"`
		Version       *int `json:"version"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return 0
	}
	// 设置用 `schemaVersion`，队列用 `version`（`download/store.go` 的
	// `queueFile`）。两个都认，因为这一层不认识任何一份文件的内容。
	if probe.SchemaVersion != nil {
		return *probe.SchemaVersion
	}
	if probe.Version != nil {
		return *probe.Version
	}
	return 0
}

// restore 把备份内容拷回原位。
//
// 备份不在这一表里（Stage 时文件就不存在）的那些**不会走到这里**——我们没有备份
// 过那个文件，也就没有资格处置它。它是升级期间被别的进程建出来的话，删掉它是在
// 毁别人的东西。
func (b *backup) restore() error {
	raw, err := os.ReadFile(b.snap)
	if err != nil {
		return fmt.Errorf("app: 回滚 %s：%w", b.file, err)
	}
	if err := writeFileAtomic(b.file, raw); err != nil {
		return fmt.Errorf("app: 回滚 %s：%w", b.file, err)
	}
	// 恢复完了就把备份删掉。它现在的职责（让回滚有东西可恢复）已经尽到；而留着
	// 它就是让用户的目录里躺着一堆 `.<文件名>.migrating`——下一次升级会被当成
	// 「上一份备份」重新用起来，而它的内容是**上一次**的。
	_ = os.Remove(b.snap)
	return nil
}

// backupFile 把一份读不懂的文件挪成 `.bak` 留着。
//
// 挪不动也不报错——已经出过一次问题了，不能因为留个证据再报一次，那会把真正的原因
// 淹掉（和 `settings.go` 的 `backupSettings` 同一个道理）。
func backupFile(path string) { _ = os.Rename(path, path+".bak") }

// SnapshotReadmeName 是快照目录里那份说明文件的名字。
const SnapshotReadmeName = "怎么用.txt"

// SnapshotReadme 是写在快照目录里的那份说明。
//
// 为什么要有它：`data-snapshots/` 里的东西**没有任何代码读**——它们是给「用户把
// 应用换回旧版」时**手动**换回去用的。而一个用户在自己硬盘上看到一个自己没建过的
// 目录，里面躺着四份自己的配置，他唯一能做的就是把它删掉或者永远不管它。
//
// 一句话说明，比「以后再说文档」有用得多：真正会用它的场景是用户降级之后发现
// 「我刚在 Biu 里设的那些东西怎么没了」，那时候他在翻这个目录。
const SnapshotReadme = `这是「上一版」的用户数据备份。

应用每次升级之前会把设置、快捷键、搜索历史和下载队列各存一份在这里，
文件名里的版本号是那份文件自己在被存下来之前自称的版本。
每一份文件只留最近一份——你要的是「上一版」，更早的那些对降级没有用。

════════════════════════════════════════════════════════════
什么时候用：你把应用换回了旧版，而旧版读不懂新版的文件。
════════════════════════════════════════════════════════════

症状是这个：旧版每次启动都说「数据目录来自更新的版本，这一版一个字都不会
写回去」——设置改了不生效，导入也导不进去。

要动**两个**东西，缺一个都还是那句话：

  ① 把对应的数据文件从下面拷回去，覆盖应用数据目录里同名的那一份
  ② 把应用数据目录里的 data-version.json 删掉

⚠️ ② 经常被漏掉。data-version.json 是「整票数据迁到哪一版」的总标记，
它记的是**新版**的号（比方说 2）。你只把数据文件拷回去而留着它的话，
旧版读到的仍然是「数据目录来自更新的版本」，那句话会**每次启动都弹**，
而设置、快捷键、导入全都还是不动。

例（把 macOS 上的路径换成你自己的）：

  D="$HOME/Library/Application Support/Biu MyGo"

  cp "data-snapshots/settings.json.v1.bak" "$D/settings.json"
  rm "$D/data-version.json"

拷之前先把原来那份改名留一手（.bak.bak）——拷错了还能换回去。
data-version.json 也可以不删，而是把里面的 version 改回你这一版的号
（"version": 1）；删掉的话旧版会按「从来没升过」处理，把那几份按 v0 读一遍
再走一遍迁移链——内容一样，只是多写一遍。

拷完重开应用。那句话不弹了，你的设置和快捷键就回来了。

这个目录（data-snapshots）可以整个删掉，下次升级会重新建。它不参与应用的
任何判断——但 data-version.json **参与**，所以那一句删的是它，不是这个目录。
`

// writeSnapshotReadme 在快照目录里放一份说明。写不了就算了。
//
// ⚠️ **不要**用 `toSnapshot` 那条路的失败来卡升级：一个纯副产物写不进去（目录只读、
// 配额满），用户的数据已经是新版的了，为它把整轮升级报成失败是更糟的。
func writeSnapshotReadme(dirSnap string) {
	_ = writeFileAtomic(filepath.Join(dirSnap, SnapshotReadmeName), []byte(SnapshotReadme))
}
