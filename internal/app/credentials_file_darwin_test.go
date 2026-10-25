//go:build darwin

package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
// 这一条对应的是**静默出错**：文件变成 0644 之后，没有任何系统会报错——
// 它照常读、照常写，登录态照常能用，只是别的用户也能看了。
// 所以这一层自己得会喊。
func TestAWidenedCredentialsFileIsCaught(t *testing.T) {
	f := newTestStore(t)
	ctx := context.Background()
	if err := f.Save(ctx, testSession()); err != nil {
		t.Fatalf("存：%v", err)
	}
	if err := os.Chmod(f.path, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := f.Load(ctx)
	if !errors.Is(err, ErrBackend) {
		t.Fatalf("权限被放宽后取，应是 ErrBackend，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("报错要告诉用户怎么修，实际：%v", err)
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
