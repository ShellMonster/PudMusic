//go:build darwin

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// selftestOn 报告要不要真的动钥匙串。
//
// ⚠️ 它跟**旧**的钥匙串后端那套自测用的是同一个开关名
// （`BIU_KEYCHAIN_SELFTEST=1`）：那一套已经删了，但开关留着——
// 「这台机器上到底能不能动真钥匙串」是**同一个问题**，换个名字只会让人
// 以为有两套规矩。
var selftestOn = os.Getenv("BIU_KEYCHAIN_SELFTEST") == "1"

// 这一套测的是**文件后端**（macOS）。
//
// 大部分用例跑在 `t.TempDir()` 里，不碰用户真数据。
// ⚠️ 只有迁移那一条要动**真钥匙串**——它用一次性服务名，且**默认跳过**，
// 要跑得显式说一声：
//
//	BIU_KEYCHAIN_SELFTEST=1 go test ./internal/app/ -run File -v
//
// 为什么迁移那条非得动真钥匙串：它要验的是「钥匙串里那一项读得出来、
// 搬得走、而且**搬完真的没了**」——这三件事只有在真的钥匙串上才成立。

// TestTheCredentialsFileSurvivesAProcessRestart 钉住「重启之后还是登录态」。
//
// 这条对应的是判据「重启 app 仍是登录状态，不用重新登录」。
// 这里用「换一个后端实例去读同一个文件」来代表重启——进程里���
// 内存状态全没了，只剩磁盘上那一份。
func TestTheCredentialsFileSurvivesAProcessRestart(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	want := testSession()
	if err := f.Save(ctx, want); err != nil {
		t.Fatalf("存：%v", err)
	}

	// 一个**全新的**后端实例——相当于重启之后的第一次读。
	restarted := &fileStore{
		dir:           f.dir,
		path:          f.path,
		legacyService: f.legacyService,
	}
	got, err := restarted.Load(ctx)
	if err != nil {
		t.Fatalf("重启之后取：%v", err)
	}
	if got.SESSDATA != want.SESSDATA {
		t.Errorf("重启之后取回来的不是存进去的：SESSDATA=%q", got.SESSDATA)
	}
}

// TestReadingTheCredentialsFileDoesNotTouchTheKeychain 钉住「有文件就不碰钥匙串」。
//
// 这一条是判据「不再有任何钥匙串授权弹框」的**结构性**保证：
// 凭据在文件里的时候，读取路径**完全不经过钥匙串**——
// 所以它既不会弹授权框，也不会往分区列表里追加 cdhash。
//
// 验法：钥匙串里放一项（一次性名字），文件也放好；读完之后
// **钥匙串那一项必须还在**——它在，说明压根没去碰过它。
//
// ⚠️ 要动真钥匙串，默认跳过。
func TestReadingTheCredentialsFileDoesNotTouchTheKeychain(t *testing.T) {
	if !selftestOn {
		t.Skip("要动真钥匙串，设 BIU_KEYCHAIN_SELFTEST=1 再跑")
	}
	ctx := context.Background()
	f := newTestStore(t)
	service := f.legacyService

	cleanup := func() {
		_ = exec.Command("security", "delete-generic-password",
			"-s", service, "-a", "primary").Run()
	}
	cleanup()
	t.Cleanup(cleanup)
	// 钥匙串里放一项（内容随便——它不该被读）。
	if out, err := exec.Command("security", "add-generic-password",
		"-s", service, "-a", "primary", "-w", "untouched").CombinedOutput(); err != nil {
		t.Fatalf("往钥匙串里造一项：%v: %s", err, out)
	}

	// 文件里放好登录态。
	if err := f.Save(ctx, testSession()); err != nil {
		t.Fatalf("存：%v", err)
	}
	if _, err := f.Load(ctx); err != nil {
		t.Fatalf("取：%v", err)
	}

	// 钥匙串那一项应该**还在**。
	find := exec.Command("security", "find-generic-password",
		"-s", service, "-a", "primary")
	if out, err := find.CombinedOutput(); err != nil {
		t.Errorf("钥匙串那一项不见了——读取路径碰了钥匙串：%v: %s", err, out)
	}
}

// TestTheTestBackendNeverPointsAtTheRealKeychainItem 是一条**保险**。
//
// 迁移的后端会去读钥匙串里旧的那一项，**读完紧接着就把它删掉**。
// 所以「测试用的后端指向哪个服务名」这件事一旦写错，后果不是测试红，
// 而是**用户的登录态被删掉**——而内容进了 `t.TempDir()`，测试一结束就没了。
//
// （这个坑本票真踩过一次：迁移函数一开始用的是包级常量而不是实例字段，
// 于是测试去删了用户真数据那一项。见收尾说明。）
func TestTheTestBackendNeverPointsAtTheRealKeychainItem(t *testing.T) {
	f := newTestStore(t)
	if f.legacyService == legacyKeychainService {
		t.Fatalf("测试后端指向了用户真数据那一项（%s）", legacyKeychainService)
	}
	if f.legacyService == "" {
		t.Fatal("测试后端没有设迁移用的服务名")
	}
}

// newTestStore 造一个指向临时目录的文件后端。
//
// ⚠️ `legacyService` 用的是一个**保证不存在**的服务名：迁移是从钥匙串里
// 「读出一项旧数据」，那个名字要是真的存在，别处的用例就会把它的内容
// 当成登录态搬进自己的临时文件里（症状：某个用例莫名其妙地登录上了）。
func newTestStore(t *testing.T) *fileStore {
	t.Helper()
	dir := t.TempDir()
	return &fileStore{
		dir:  dir,
		path: filepath.Join(dir, credentialsDirName, credentialsFileName),
		// 这个名字在钥匙串里不存在；迁移那条用例会自己造一项出来。
		legacyService: "com.biu-go.biu-mygo.login.selftest.absent",
	}
}

// testSession 造一份能用的登录态。
func testSession() Session {
	return Session{
		SESSDATA:     "sessdata-value",
		BiliJCT:      "bili-jct-value",
		DedeUserID:   "12345",
		RefreshToken: "refresh-value",
	}
}

func TestTheCredentialsFileRoundTripsTheSession(t *testing.T) {
	f := newTestStore(t)
	want := testSession()
	if err := f.Save(context.Background(), want); err != nil {
		t.Fatalf("存：%v", err)
	}
	got, err := f.Load(context.Background())
	if err != nil {
		t.Fatalf("取：%v", err)
	}
	if got.SESSDATA != want.SESSDATA || got.BiliJCT != want.BiliJCT ||
		got.RefreshToken != want.RefreshToken {
		t.Errorf("取回来的不是存进去的：%+v", got)
	}
}

func TestTheCredentialsFileIsOverwrittenRatherThanDuplicated(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	if err := f.Save(ctx, testSession()); err != nil {
		t.Fatalf("第一次存：%v", err)
	}
	second := testSession()
	second.SESSDATA = "second-value"
	if err := f.Save(ctx, second); err != nil {
		t.Fatalf("第二次存：%v", err)
	}
	got, err := f.Load(ctx)
	if err != nil {
		t.Fatalf("取：%v", err)
	}
	if got.SESSDATA != "second-value" {
		t.Errorf("第二次存没有覆盖：SESSDATA=%q", got.SESSDATA)
	}
	// 目录里只该有一个凭据文件（临时文件不该留下）。
	entries, err := os.ReadDir(filepath.Dir(f.path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != credentialsFileName {
			t.Errorf("凭据目录里多了一个文件 %q", e.Name())
		}
	}
}

func TestClearingTheCredentialsFileIsIdempotent(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	if err := f.Save(ctx, testSession()); err != nil {
		t.Fatalf("存：%v", err)
	}
	if err := f.Clear(ctx); err != nil {
		t.Fatalf("第一次清：%v", err)
	}
	if err := f.Clear(ctx); err != nil {
		t.Fatalf("第二次清（该是空操作）：%v", err)
	}
	if _, err := f.Load(ctx); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("清完之后取，应是 ErrNotLoggedIn，实际 %v", err)
	}
}

func TestNoCredentialsFileMeansNotLoggedIn(t *testing.T) {
	f := newTestStore(t)
	if _, err := f.Load(context.Background()); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("没有文件时应是 ErrNotLoggedIn，实际 %v", err)
	}
}

func TestUnreadableCredentialsFileMeansCorrupt(t *testing.T) {
	f := newTestStore(t)
	if err := os.MkdirAll(filepath.Dir(f.path), credentialsDirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.path, []byte("{not json"), credentialsFileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Load(context.Background()); !errors.Is(err, ErrCorrupt) {
		t.Errorf("坏内容应是 ErrCorrupt，实际 %v", err)
	}
}

// TestTheCredentialsFileIsOnlyReadableByItsOwner 钉住那两个权限位。
//
// ⚠️ 它们挡的是**别的用户**（共享机器上的另一个人、别人拷走你的磁盘镜像），
// **不挡同一个用户下的别的进程**——同一个用户的进程本来就能读你的文件。
// 这一点在 README「已知边界」那一节里写明了，别在这里假装它更严。
func TestTheCredentialsFileIsOnlyReadableByItsOwner(t *testing.T) {
	f := newTestStore(t)
	if err := f.Save(context.Background(), testSession()); err != nil {
		t.Fatalf("存：%v", err)
	}
	st, err := os.Stat(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != credentialsFileMode {
		t.Errorf("凭据文件权限是 %04o，应是 %04o", perm, credentialsFileMode)
	}
	dirSt, err := os.Stat(filepath.Dir(f.path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirSt.Mode().Perm(); perm != credentialsDirMode {
		t.Errorf("凭据目录权限是 %04o，应是 %04o", perm, credentialsDirMode)
	}
}

// TestAWidenedCredentialsFileIsCaught 钉住「权限被放宽要能被发现」。
//
// ⚠️ **而且不能因此把登录态弄丢**：早先的版本在这里返回 ErrBackend，
// 于是上层判定「后端坏了」→ Logout → **凭据文件被删掉** → 用户的登录态没了，
// 而界面上什么也不说（评审逮到过，见 issue #493 的收尾说明）。
//
// 现在的口径：**读照常读，权限顺手收回**——
// 「被发现」的方式是权限自己收回去了（`ls -l` 一眼看得见），不需要丢数据。
func TestAWidenedCredentialsFileIsCaught(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	if err := f.Save(ctx, testSession()); err != nil {
		t.Fatalf("存：%v", err)
	}
	if err := os.Chmod(f.path, 0o644); err != nil {
		t.Fatal(err)
	}

	// ① 读得出来——登录态没丢。
	got, err := f.Load(ctx)
	if err != nil {
		t.Fatalf("权限被放宽不该让读取失败：%v", err)
	}
	if got.SESSDATA != "sessdata-value" {
		t.Errorf("读回来的登录态不对：SESSDATA=%q", got.SESSDATA)
	}
	// ② 权限被收回了——这就是「被发现」。
	st, err := os.Stat(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != credentialsFileMode {
		t.Errorf("权限没有被收回：%04o，应是 %04o", perm, credentialsFileMode)
	}
}

// TestAnExecutableCredentialsFileIsNotTooWide 钉住「判据只看 group / other」。
//
// 0700（owner 可执行）对凭据文件**不算「宽」**：组和其他用户一个字节都读不到。
// 早先的判据 `perm &^ 0600 != 0` 会把它算成「宽」，而报错文案写着
// 「别人能读了」——那是假的，而且照样会把用户引上「拒绝读 → 删凭据」那条路。
func TestAnExecutableCredentialsFileIsNotTooWide(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	if err := f.Save(ctx, testSession()); err != nil {
		t.Fatalf("存：%v", err)
	}
	if err := os.Chmod(f.path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Load(ctx); err != nil {
		t.Fatalf("0700 不该算「宽」：%v", err)
	}
	st, err := os.Stat(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o700 {
		t.Errorf("0700 不该被改动：%04o", perm)
	}
}

// TestTheMigrationSurvivesAFailedKeychainDeletion 钉住「搬成功但钥匙串删不掉」。
//
// 那一项删不掉时（钥匙串锁着、security 失败……），**文件里已经有可用的凭据了**，
// 用户是登录着的。早先的代码把任何迁移错误一律映射成「没登录」，
// 于是用户明明登录着却被当成没登录（评审逮到过）。
//
// 这条用一个**假的 security** 模拟「find 成功、delete 失败」。
func TestTheMigrationSurvivesAFailedKeychainDeletion(t *testing.T) {
	fake := t.TempDir()
	content := `{"cookies":[],"SESSDATA":"sess","bili_jct":"jct","DedeUserID":"1","refresh_token":"rt"}`
	script := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"  find-generic-password) printf '%s' '" + content + "'; exit 0;;\n" +
		"  delete-generic-password) exit 45;;\n" +
		"esac\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(fake, "security"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fake+string(os.PathListSeparator)+os.Getenv("PATH"))

	f := newTestStore(t)
	got, err := f.Load(context.Background())
	if err != nil {
		t.Fatalf("钥匙串删不掉不该让读取失败：%v", err)
	}
	if got.SESSDATA != "sess" {
		t.Errorf("迁过来的登录态不对：SESSDATA=%q", got.SESSDATA)
	}
	// 旧的那份还在钥匙串里 → 必须留下一个「待清理」的标记。
	if _, err := os.Stat(filepath.Join(f.dir, cleanupMarkerName)); err != nil {
		t.Errorf("钥匙串里旧的那一份还没删掉，应该留下清理标记：%v", err)
	}
}

// TestAWidenedCredentialsFileDoesNotThrowAwayTheLoginState 是评审给的那条
// 复现路径的**正面版本**：
//
//	Save 一份登录态 → chmod 0644 → 走真路径 restore(nil)
//	→ 凭据文件**必须还在**，登录态**必须还在**。
//
// 早先的版本：权限自检让 Load 失败 → restore 判定「后端坏了」→ Logout
// → **凭据文件被删掉** → 用户的登录态没了，而界面上什么也不说。
func TestAWidenedCredentialsFileDoesNotThrowAwayTheLoginState(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	if err := f.Save(ctx, testSession()); err != nil {
		t.Fatalf("存：%v", err)
	}
	if err := os.Chmod(f.path, 0o644); err != nil {
		t.Fatal(err)
	}

	a := newAccount(f, nil)
	// restore(nil) 最后会因为「没有客户端」返回错，但那是在**读完凭据之后**。
	_ = a.restore(nil)

	if _, err := os.Stat(f.path); err != nil {
		t.Fatalf("凭据文件被删了：%v", err)
	}
	if !a.mgr.Current().LoggedIn() {
		t.Error("登录态没了")
	}
}

// clearingStore 是「读不出来」的后端，并且**记下有没有被 Clear 过**。
type clearingStore struct{ cleared bool }

func (*clearingStore) Name() string { return "clearing" }
func (*clearingStore) Load(context.Context) (Session, error) {
	return Session{}, fmt.Errorf("%w: 读不出来", ErrBackend)
}
func (*clearingStore) Save(context.Context, Session) error { return nil }
func (c *clearingStore) Clear(context.Context) error {
	c.cleared = true
	return nil
}

// TestABackendThatCannotReadDoesNotThrowAwayTheCredentials 钉住
// 「读不出来 ≠ 凭据该被删掉」。
//
// 清掉是**不可逆**的（凭据一删，登录态就没了），而「读不出来」只是
// 暂时的问题——凭据还在那儿，下次读得到。所以只有「解不开」
// （ErrCorrupt）才清，「读不出来」（ErrBackend）不清。
func TestABackendThatCannotReadDoesNotThrowAwayTheCredentials(t *testing.T) {
	store := &clearingStore{}
	a := newAccount(store, nil)
	if err := a.restore(nil); !errors.Is(err, ErrBackend) {
		t.Fatalf("restore 应把后端的错原样带出来，实际 %v", err)
	}
	if store.cleared {
		t.Error("后端只是读不出来，凭据却被清掉了")
	}
}

// TestTheMigrationLeavesAMarkerWhenTheKeychainCannotBeRead 钉住
// 「钥匙串读不到 → 留下待清理的标记」。
//
// 那一项可能还在钥匙串里（钥匙串锁着而已），而判据 ② 要求它最后必须没有。
// 所以搬不动的时候要记一笔，下次启动再清。
func TestTheMigrationLeavesAMarkerWhenTheKeychainCannotBeRead(t *testing.T) {
	fake := t.TempDir()
	script := "#!/bin/sh\nexit 1\n" // find 和 delete 都失败
	if err := os.WriteFile(filepath.Join(fake, "security"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fake+string(os.PathListSeparator)+os.Getenv("PATH"))

	f := newTestStore(t)
	if _, err := f.Load(context.Background()); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("钥匙串读不到时应是 ErrNotLoggedIn，实际 %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.dir, cleanupMarkerName)); err != nil {
		t.Errorf("应该留下「待清理」的标记：%v", err)
	}
}

// TestWritingAgainTightensThePermissions 钉住「写回去会把权限收回」。
func TestWritingAgainTightensThePermissions(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	if err := f.Save(ctx, testSession()); err != nil {
		t.Fatalf("存：%v", err)
	}
	if err := os.Chmod(f.path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(ctx, testSession()); err != nil {
		t.Fatalf("再存一次：%v", err)
	}
	st, err := os.Stat(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != credentialsFileMode {
		t.Errorf("再存之后权限是 %04o，应是 %04o", perm, credentialsFileMode)
	}
}

func TestTheCredentialsBackendIsNamedForDiagnostics(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSystemStore(dir)
	if err != nil {
		t.Fatalf("建后端：%v", err)
	}
	if got := s.Name(); got != "macos-file" {
		t.Errorf("后端名字是 %q，want macos-file（写进诊断信息用）", got)
	}
	if _, ok := s.(*fileStore); !ok {
		t.Errorf("后端类型是 %T，want *fileStore", s)
	}
}

func TestNoUserDataDirMeansNoBackend(t *testing.T) {
	if _, err := NewSystemStore(""); !errors.Is(err, ErrUnavailable) {
		t.Errorf("没有数据目录应是 ErrUnavailable，实际 %v", err)
	}
}

// TestTheLoginStateMovesOutOfTheKeychainAndTheKeychainItemIsDeleted
// 验的是**一次性迁移**这条路的三个关键点：
//
//  1. 钥匙串里旧的那份**读得出来**（不要求用户重新登录）；
//  2. 搬完之后凭据在文件里；
//  3. **钥匙串里那一项真的没了**——留着就等于同一份明文还在那儿。
//
// ⚠️ 这条要动**真的钥匙串**，所以默认跳过（见文件头）。
func TestTheLoginStateMovesOutOfTheKeychainAndTheKeychainItemIsDeleted(t *testing.T) {
	if !selftestOn {
		t.Skip("要动真钥匙串，设 BIU_KEYCHAIN_SELFTEST=1 再跑")
	}
	ctx := context.Background()
	f := newTestStore(t)
	service := f.legacyService

	// 造一份旧格式的登录态放进钥匙串（模拟升级前的状态）。
	old := testSession()
	blob, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		_ = exec.Command("security", "delete-generic-password",
			"-s", service, "-a", "primary").Run()
	}
	cleanup()
	t.Cleanup(cleanup)
	add := exec.Command("security", "add-generic-password",
		"-s", service, "-a", "primary", "-w", string(blob))
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("往钥匙串里造一项：%v: %s", err, out)
	}

	// 文件还没有 → 触发迁移。
	got, err := f.Load(ctx)
	if err != nil {
		t.Fatalf("迁移后取：%v", err)
	}
	if got.SESSDATA != old.SESSDATA {
		t.Errorf("迁过来的不是原来那份：SESSDATA=%q", got.SESSDATA)
	}
	if _, err := os.Stat(f.path); err != nil {
		t.Errorf("凭据文件没落盘：%v", err)
	}

	// 钥匙串里那一项应该**没了**。
	find := exec.Command("security", "find-generic-password",
		"-s", service, "-a", "primary")
	if out, err := find.CombinedOutput(); err == nil {
		t.Errorf("钥匙串里那一项还在（迁移没删掉它）：%s", out)
	}
}
