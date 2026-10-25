//go:build darwin

package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// 这一套测的是**文件后端**（macOS），全部跑在 `t.TempDir()` 里，不碰用户真数据。

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
		dir:  f.dir,
		path: f.path,
	}
	got, err := restarted.Load(ctx)
	if err != nil {
		t.Fatalf("重启之后取：%v", err)
	}
	if got.SESSDATA != want.SESSDATA {
		t.Errorf("重启之后取回来的不是存进去的：SESSDATA=%q", got.SESSDATA)
	}
}

// newTestStore 造一个指向临时目录的文件后端。
func newTestStore(t *testing.T) *fileStore {
	t.Helper()
	dir := t.TempDir()
	return &fileStore{
		dir:  dir,
		path: filepath.Join(dir, credentialsDirName, credentialsFileName),
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
