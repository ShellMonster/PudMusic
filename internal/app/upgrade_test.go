package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/platform"
)

// ─────────────────────────────────────────────────────────────────────────────
// 脚手架
// ─────────────────────────────────────────────────────────────────────────────

// upgradeDir 造一个只有「升级之前」那种形状的数据目录：三份文件在，标记没有。
//
// 故意**不**造 `data-version.json`：第一次运行就是这样，而大多数升级都是从
// 「标记还没有」的状态出发的（老版本根本不知道有这个标记）。
func upgradeDir(t *testing.T, settings string) (dir, settingsPath string) {
	t.Helper()
	dir = t.TempDir()
	settingsPath = SettingsPath(dir)
	mustWrite(t, settingsPath, settings)
	return dir, settingsPath
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s：%v", path, err)
	}
	return string(b)
}

// markerOf 读总控标记。
func markerOf(t *testing.T, dir string) upgradeMarker {
	t.Helper()
	var m upgradeMarker
	b, err := os.ReadFile(filepath.Join(dir, UpgradeFileName))
	if err != nil {
		t.Fatalf("读总控标记：%v", err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("总控标记读不懂：%v", err)
	}
	return m
}

// ─────────────────────────────────────────────────────────────────────────────
// 备份
// ─────────────────────────────────────────────────────────────────────────────

// 一条断言型检查：Stage 一完成、原文件一个字都还没动的时候，备份就该在原位、
// 而且内容是**迁移前**的那一份。
func TestAnUpgradeBacksEveryFileUpBeforeTouchingIt(t *testing.T) {
	dir, settingsPath := upgradeDir(t, `{"appSettings":{"primaryColor":"#abcdef"}}`)

	tx, err := BeginUpgrade(dir)
	if err != nil {
		t.Fatal(err)
	}
	bk, err := tx.Stage(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if bk == nil {
		t.Fatal("Stage 了一份却没给备份句柄")
	}

	// 备份和原文件在同一个目录里（跨卷的 rename 会退化成拷贝加删除，中途失败
	// 就是两份都没有）。
	snap := filepath.Join(filepath.Dir(settingsPath), "."+filepath.Base(settingsPath)+".migrating")
	if got := mustRead(t, snap); !strings.Contains(got, "#abcdef") {
		t.Fatalf("备份里没有迁移前的内容：%s", got)
	}
	// 原文件此刻一个字都没动。
	if got := mustRead(t, settingsPath); !strings.Contains(got, "#abcdef") {
		t.Fatalf("Stage 之后原文件被改了：%s", got)
	}
	// 而「正在升」这件事已经记下来了——升级中间断电，靠的就是这一行。
	if m := markerOf(t, dir); m.Pending == nil {
		t.Fatal("Stage 之后标记里没有 pending：下次启动认不出这一次没做完")
	} else if m.Pending.From != 0 || m.Pending.To != CurrentDataVersion {
		t.Fatalf("pending 记的不是 v0→v%d，是 %+v", CurrentDataVersion, *m.Pending)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 失败保留旧文件
// ─────────────────────────────────────────────────────────────────────────────

// 三份文件一起升，第三份的迁移抛错：前两份已经改过的内容要**原样**回来。
//
// 这是这一票最核心的一条。升级失败最坏的表现不是「没升成」，是「升了一半」——
// 用户的设置是新版的、队列还是旧版的，而没有任何地方记着这件事。
func TestAFailedUpgradePutsEveryFileBackExactly(t *testing.T) {
	dir, settingsPath := upgradeDir(t, `{"appSettings":{"primaryColor":"#abcdef"}}`)

	// 两份无关的文件当"另外两份"，各自带一句能被认出来的内容。
	queuePath := filepath.Join(dir, "download-queue.json")
	histPath := filepath.Join(dir, "search-history.json")
	mustWrite(t, queuePath, `{"version":1,"tasks":[]}`)
	mustWrite(t, histPath, `{"schemaVersion":1,"items":[{"value":"甲","time":1}]}`)

	tx, err := BeginUpgrade(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Stage(settingsPath); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Stage(queuePath); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Stage(histPath); err != nil {
		t.Fatal(err)
	}

	// 前两份真的改了内容（模拟迁完）。
	mustWrite(t, settingsPath, `{"schemaVersion":1,"appSettings":{"primaryColor":"#fedcba"}}`)
	mustWrite(t, queuePath, `{"version":1,"tasks":[{"id":"x"}]}`)

	// 第三份炸了 → 整轮回滚。回滚本身不该报错（数据都在备份里）。
	if err := tx.Rollback(); err != nil {
		t.Fatalf("回滚报了错：%v", err)
	}

	// 前两份回到了升级前的内容，一个字节都不差。
	if got := mustRead(t, settingsPath); !strings.Contains(got, "#abcdef") || strings.Contains(got, "#fedcba") {
		t.Fatalf("设置没有被恢复成迁移前的那一份：%s", got)
	}
	if got := mustRead(t, queuePath); !strings.Contains(got, `"tasks":[]`) {
		t.Fatalf("队列没有被恢复：%s", got)
	}
	// 备份文件也不能留在用户目录里当垃圾。
	leftovers := migratingFiles(t, dir)
	if len(leftovers) != 0 {
		t.Fatalf("回滚之后还留着备份文件：%v", leftovers)
	}
}

// 回滚之后总标记回到「从没升过」的样子：一个 Version=0 的标记 + 一堆快照，
// 而真相是「这里什么都没发生过」。
func TestAFailedUpgradeLeavesNoTraceOfAnUpgradeThatNeverHappened(t *testing.T) {
	dir, settingsPath := upgradeDir(t, `{"appSettings":{}}`)
	tx, err := BeginUpgrade(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Stage(settingsPath); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, settingsPath, `{"schemaVersion":1,"appSettings":{"primaryColor":"#fedcba"}}`)
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, UpgradeFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("回滚之后总标记还在：%v", err)
	}
	if snaps := snapshotFiles(t, dir); len(snaps) != 0 {
		t.Fatalf("回滚之后还留了快照：%v", snaps)
	}
}

// migratingFiles 列出还没处置的迁移备份。
func migratingFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(dir, func(p string, _ os.FileInfo, err error) error {
		if err == nil && strings.Contains(filepath.Base(p), ".migrating") {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// snapshotFiles 列出降级快照（**不含**那份说明）。
func snapshotFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, UpgradeSnapshotDir))
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Name() == SnapshotReadmeName {
			continue
		}
		out = append(out, e.Name())
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 多文件一致性
// ─────────────────────────────────────────────────────────────────────────────

// 升级成功：标记推到最新版、pending 清空、四份文件都迁完了。
func TestAWholeUpgradeLandsAndClearsItsMarker(t *testing.T) {
	dir, settingsPath := upgradeDir(t, `{"appSettings":{"primaryColor":"#abcdef"}}`)
	queuePath := filepath.Join(dir, "download-queue.json")
	mustWrite(t, queuePath, `{"version":1,"tasks":[]}`)

	tx, err := BeginUpgrade(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Stage(settingsPath); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Stage(queuePath); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSettingsFile(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSettingsFile(queuePath); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	m := markerOf(t, dir)
	if m.Version != CurrentDataVersion {
		t.Fatalf("标记停在 v%d，不是 v%d", m.Version, CurrentDataVersion)
	}
	if m.Pending != nil {
		t.Fatalf("升完了 pending 还在：%+v", *m.Pending)
	}
	if leftovers := migratingFiles(t, dir); len(leftovers) != 0 {
		t.Fatalf("升完了还留着迁移备份：%v", leftovers)
	}
}

// 升级升到一半进程死了：下次启动看到未完成的标记，知道该重跑。
func TestTheMarkerRemembersAnUpgradeThatWasInterrupted(t *testing.T) {
	dir, settingsPath := upgradeDir(t, `{"appSettings":{}}`)
	tx, err := BeginUpgrade(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Stage(settingsPath); err != nil {
		t.Fatal(err)
	}
	// 进程在这里没了：没有 Commit，没有 Rollback。

	next, err := BeginUpgrade(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !next.Pending() {
		t.Fatal("上一次升级没做完，标记却没说：下次启动会以为数据是齐的")
	}
	if next.Version() != 0 {
		t.Fatalf("没做完的升级把版本号推到 v%d 了", next.Version())
	}
}

// Stage 同一份文件两次不会备份两次。
//
// 第二次备份拷的是「已经被迁移过的那一份」，而回滚要的是迁移**前**的内容。
func TestStagingTheSameFileTwiceKeepsTheOriginal(t *testing.T) {
	dir, settingsPath := upgradeDir(t, `{"appSettings":{"primaryColor":"#abcdef"}}`)
	tx, err := BeginUpgrade(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Stage(settingsPath); err != nil {
		t.Fatal(err)
	}
	// 迁移改写了它。
	mustWrite(t, settingsPath, `{"schemaVersion":1,"appSettings":{"primaryColor":"#fedcba"}}`)
	if _, err := tx.Stage(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, settingsPath); !strings.Contains(got, "#abcdef") {
		t.Fatalf("回滚回来的是迁移后的内容：%s", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 降级快照
// ─────────────────────────────────────────────────────────────────────────────

// 升级成功之后，上一版的完整副本留在 data-snapshots/ 里——用户把应用换回旧版时
// 拿它换回去。
//
// 断言的是**内容**：那份快照必须还是升级前的那一份（`#abcdef`），而不是升级后的。
func TestASuccessfulUpgradeLeavesThePreviousVersionOnDisk(t *testing.T) {
	dir, settingsPath := upgradeDir(t, `{"appSettings":{"primaryColor":"#abcdef"}}`)

	tx, err := BeginUpgrade(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Stage(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSettingsFile(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// 当前文件已经是新版。
	if !strings.Contains(mustRead(t, settingsPath), fmtSchemaVersion()) {
		t.Fatalf("当前文件没被迁到当前 schema：%s", mustRead(t, settingsPath))
	}
	// Biu 导出的裸设置没有 schemaVersion 键，也就是它自称 v0——快照名照它说的写。
	snap := filepath.Join(dir, UpgradeSnapshotDir, "settings.json.v0.bak")
	if got := mustRead(t, snap); !strings.Contains(got, "#abcdef") {
		t.Fatalf("快照里不是升级前的内容：%s", got)
	}
}

// 快照名里那个版本号是**这一份文件自己**的，不是整轮升级之前的总版本号。
//
// 两者在一个场景里不一样：一个老用户第一次装上带总标记的这一版——那时还没有
// `data-version.json`，总版本号记成 0，于是四份文件明明已经是当前形状的 v1，快照
// 却会被命名成 `.v0`。**降级时用户正是照着这个名字去挑的**，说谎的名字会让他
// 拿错一份。
func TestTheSnapshotNameIsTheFilesOwnVersionNotTheTotals(t *testing.T) {
	dir, settingsPath := upgradeDir(t, `{"schemaVersion":1,"appSettings":{"primaryColor":"#abcdef"}}`)

	tx, err := BeginUpgrade(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Stage(settingsPath); err != nil {
		t.Fatal(err)
	}
	// 这一轮升完之后标记是 v1；快照里那一份自称的却是 v1（已经是当前形状了）。
	if err := MigrateSettingsFile(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// 没有 `.v0` 那一份——总版本号虽然是 0，但那不是这份文件的版本。
	if _, err := os.Stat(filepath.Join(dir, UpgradeSnapshotDir, "settings.json.v0.bak")); err == nil {
		t.Fatalf("快照名里用的是总版本号（v0），而这一份文件自称 v1")
	}
	snap := filepath.Join(dir, UpgradeSnapshotDir, "settings.json.v1.bak")
	if got := mustRead(t, snap); !strings.Contains(got, "#abcdef") {
		t.Fatalf("快照不在 %s：%s", snap, got)
	}
}

// 快照是覆盖式的：升 v0→v1 存一份，v1→v2 覆盖成 `.v1.json`，用户目录里不会
// 越攒越多，而「上一版」这个保证一直在。
func TestTheSnapshotOnlyKeepsTheVersionRightBelow(t *testing.T) {
	dir, settingsPath := upgradeDir(t, `{"appSettings":{"primaryColor":"#abcdef"}}`)

	tx, _ := BeginUpgrade(dir)
	if _, err := tx.Stage(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSettingsFile(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// 第二轮：把标记拨回 v0，模拟「将来某一版又升了」。
	mustWrite(t, filepath.Join(dir, UpgradeFileName), `{"version":0}`)

	tx2, err := BeginUpgrade(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx2.Stage(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatal(err)
	}
	// 第二轮之后目录里**只有一份** settings.json 的快照：第一轮留的那份
	// （当时文件自称 v0）被这一份（文件自称 v1）替掉了。
	if snaps := snapshotFiles(t, dir); len(snaps) != 1 || snaps[0] != "settings.json.v1.bak" {
		t.Fatalf("快照攒了一串：%v", snaps)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 来自更新的版本
// ─────────────────────────────────────────────────────────────────────────────

// 整个数据目录来自更新的版本：事务压根不开，一个文件都不碰。
//
// 用户把应用换回旧版时就是这个状态。这一版如果照常跑迁移，会把新版的文件按
// 旧语义改写一遍——用户升级回来时数据已经面目全非。
func TestADataDirectoryFromANewerVersionIsNotTouchedAtAll(t *testing.T) {
	dir, settingsPath := upgradeDir(t, `{"appSettings":{"primaryColor":"#abcdef"}}`)
	mustWrite(t, filepath.Join(dir, UpgradeFileName),
		`{"version":`+itoa(CurrentDataVersion+1)+`}`)
	before := mustRead(t, settingsPath)

	_, err := BeginUpgrade(dir)
	if !errors.Is(err, ErrUpgradeFromNewer) {
		t.Fatalf("来自更新的版本没有报 ErrUpgradeFromNewer，报的是 %v", err)
	}
	if got := mustRead(t, settingsPath); got != before {
		t.Fatalf("探测本身动了文件：\n改前 %s\n改后 %s", before, got)
	}
}

// 一份文件自称比当前版本新时，`MigrateSettingsFile` 报 `ErrSettingsFromNewer`
// 且一个字节都不动。
//
// ⚠️ 这一条**只管迁移这一层**。「总标记说是最新版、但有一份文件说更新」那个情形
// 由**生产入口** `runUpgrade` 判（它得先知道有这一份，才能不 Stage 它），测试在
// `internal/ui` 那边的 `TestAMarkerSaysUpToDateButOneFileSaysNewer`——它造的目录
// 标记是 `version:1`（**最新版**），所以会走进 `runUpgrade` 那条早退以前的那段。
// 原来这里那条同名测试用的标记是 `version:0`、而且直接调 `BeginUpgrade`+`Stage`
// 绕开了生产入口，看着覆盖了这个情形，其实没有。
func TestMigrateSettingsRefusesAFileThatClaimsANewerSchema(t *testing.T) {
	dir, settingsPath := upgradeDir(t, `{"appSettings":{}}`)
	mustWrite(t, filepath.Join(dir, UpgradeFileName), `{"version":0}`)
	// 一份未来的设置文件。
	mustWrite(t, settingsPath, `{"schemaVersion":`+itoa(SchemaVersion+5)+`,"appSettings":{"primaryColor":"#abcdef"}}`)

	tx, err := BeginUpgrade(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Stage(settingsPath); err != nil {
		t.Fatal(err)
	}
	migErr := MigrateSettingsFile(settingsPath)
	if !errors.Is(migErr, ErrSettingsFromNewer) {
		t.Fatalf("一份未来的设置文件没有报 ErrSettingsFromNewer，报的是 %v", migErr)
	}
	if got := mustRead(t, settingsPath); !strings.Contains(got, "#abcdef") {
		t.Fatalf("这份文件被动了：%s", got)
	}
}

// 标记文件本身读不出来：只备份留证，然后报错。
//
// **不能当成「还没升过」**去重跑一遍迁移。有一份可能已经是新版的，重跑等于把
// 新版内容按旧迁移再走一次。
func TestACorruptMarkerDoesNotMakeUsRerunEveryMigration(t *testing.T) {
	dir, settingsPath := upgradeDir(t, `{"appSettings":{}}`)
	mustWrite(t, filepath.Join(dir, UpgradeFileName), `这不是 JSON`)
	before := mustRead(t, settingsPath)

	_, err := BeginUpgrade(dir)
	if err == nil {
		t.Fatal("总标记读不懂却没有报错：下一次会当成 v0 把每一份都重跑一遍迁移")
	}
	if got := mustRead(t, settingsPath); got != before {
		t.Fatalf("报错了却动了文件：\n改前 %s\n改后 %s", before, got)
	}
	if _, statErr := os.Stat(filepath.Join(dir, UpgradeFileName+".bak")); statErr != nil {
		t.Fatalf("坏掉的标记没有留一份 .bak：%v", statErr)
	}
}

// 标记缺失不是错误：那是第一次运行。
func TestAMissingMarkerIsJustAFreshInstall(t *testing.T) {
	dir := t.TempDir()
	tx, err := BeginUpgrade(dir)
	if err != nil {
		t.Fatalf("第一次运行报错了：%v", err)
	}
	if tx.Version() != 0 {
		t.Fatalf("第一次运行的版本不是 0，是 %d", tx.Version())
	}
	if tx.Pending() {
		t.Fatal("第一次运行就有没做完的升级")
	}
	// 没升过的时候 Stage 不备份任何东西——目录是空的。
	bk, err := tx.Stage(filepath.Join(dir, "never-existed.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bk != nil {
		t.Fatal("目录里根本没有这个文件，Stage 却备份了一个不存在的句柄")
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if snaps := snapshotFiles(t, dir); len(snaps) != 0 {
		t.Fatalf("第一次运行留下了快照：%v", snaps)
	}
}

// 快照目录里那份说明要说清楚**怎么用**。
//
// 快照没有任何代码读——它是给用户手动换回去用的。而一个用户在自己硬盘上看到一个
// 自己没建过的目录、里面躺着四份自己的配置，他唯一能做的就是把它删掉或者永远不管
// 它。真正会用它的场景是「降级之后发现刚在 Biu 里设的东西怎么没了」，那时候他
// 正在翻这个目录。
func TestTheSnapshotDirectoryExplainsItself(t *testing.T) {
	dir, settingsPath := upgradeDir(t, `{"schemaVersion":1,"appSettings":{"primaryColor":"#abcdef"}}`)
	tx, _ := BeginUpgrade(dir)
	if _, err := tx.Stage(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSettingsFile(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	body := mustRead(t, filepath.Join(dir, UpgradeSnapshotDir, SnapshotReadmeName))
	for _, want := range []string{
		"旧版",            // 什么时候用
		"换回了旧版",         // 同上
		"拷回",            // 怎么做
		"settings.json", // 拿哪一份
		// 那条给用户敲的路径。它得是**真实存在的那个数据目录**，也就是
		// `platform.LegacyUserDataDirName` ——见下面那条专门的测试。
		platform.LegacyUserDataDirName,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("说明里没有 %q：\n%s", want, body)
		}
	}
}

// TestTheSnapshotReadmePointsAtTheDirectoryThatExists 钉住「回滚说明里那条路径是
// 真的那个数据目录」。
//
// 这条最容易在**改名**那一票里悄悄坏掉：说明里那句 `D="$HOME/Library/
// Application Support/Biu MyGo"` 是照着当时的目录名写的，而应用显示名改成了
// BiuGo。谁在改名时「顺手」把它一起改掉，用户照着敲就得到一句
// `cp: …: No such file or directory`——**而他真正要回滚的东西一个字节都没少**。
// 那句话读起来像数据没了，实际是路径写错了，是最难查的一类问题。
//
// 所以这里断的是「说明里的目录名 == 应用实际在用的目录名」，而不是某个字面量：
// 哪天真的搬了目录（理由见 `internal/platform/datadir.go` 顶上那一节），这条会红，
// 而它红得正是时候。
func TestTheSnapshotReadmePointsAtTheDirectoryThatExists(t *testing.T) {
	if !strings.Contains(SnapshotReadme, platform.LegacyUserDataDirName) {
		t.Errorf("回滚说明里的路径没有用应用真正在用的目录名 %q：\n%s",
			platform.LegacyUserDataDirName, SnapshotReadme)
	}
}

// fmtSchemaVersion 是当前设置文件该带的那句 `"schemaVersion": N`。
func fmtSchemaVersion() string { return `"schemaVersion": ` + strconv.Itoa(SchemaVersion) }
