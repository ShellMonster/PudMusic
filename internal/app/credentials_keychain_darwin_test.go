//go:build darwin

package app

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// writeRaw 直接往这一项里塞任意字节，用来造「存进去的东西坏了」的场景。
//
// 走的是和 Save 一样的两条路径（有就改、没有就加），所以它模拟的是
// 「另一个版本的程序存了点别的东西进来」，不是「测试特批了一条后门」。
func (k *keychainStore) writeRaw(blob []byte) error {
	item, found, err := k.findItem()
	if err != nil {
		return err
	}
	if found {
		defer cfRelease(item)
		if s := secKeychainItemModifyContent(item, 0, uint32(len(blob)), &blob[0]); s != errSecSuccess {
			return keychainError("改写", s)
		}
		return nil
	}
	st := secKeychainAddGenericPassword(0,
		uint32(len(k.service)), cstr(k.service),
		uint32(len(k.account)), cstr(k.account),
		uint32(len(blob)), &blob[0], nil)
	if st == nil {
		return errors.New("没有返回状态码")
	}
	if *st != errSecSuccess {
		return keychainError("写入", *st)
	}
	return nil
}

// 这个文件测的是**真的钥匙串**。
//
// 不是 mock：纯逻辑（序列化、代次、脱敏）另有纯逻辑测试兜着，
// 而这里要验的恰恰是「那几条 unsafe 的参数声明对不对」——
// 声明错了 mock 测不出来，表现为「函数返回成功、出参却是空的」。
//
// # 默认跳过，要显式开
//
// 这些测试会**往用户的登录钥匙串里真的写一项**。它用的是测试专用
// 的服务名（`testService`），跟正式那条不重名，正式那条（用户真正的
// 账号）不会被碰到——但即使这样，「跑一次测试就在钥匙串里留下东西」
// 也不该是默认行为：CI 里没有钥匙串、容器里会失败、别人的机器上
// 可能要授权弹窗。
//
// 所以要跑它得显式说一声：
//
//	BIU_KEYCHAIN_SELFTEST=1 go test ./internal/app/ -run Keychain -v
//
// `TestKeychainSymbolsResolve` 不写任何东西，默认就跑，它守的是
// 「符号还在不在」——macOS 把 `SecKeychainFreeContent` 移出导出表
// 那次就是这么发现的。
const testService = "com.biu-go.biu-mygo.login.selftest"

// selftestOn 报告要不要真的动钥匙串。
//
// 用 var 而不是 const：os.Getenv 不是常量表达式，const 声明直接编译不过。
var selftestOn = os.Getenv("BIU_KEYCHAIN_SELFTEST") == "1"

// TestKeychainSymbolsResolve 只做符号解析，一个字节都不写。
//
// 这一条必须默认就跑：`purego.Dlopen` / `Dlsym` 全失败时 loadErr 非空，
// 后面所有钥匙串操作直接返回 ErrUnavailable——那个失败会被跳过逻辑
// 一起吞掉，症状是「macOS 上钥匙串莫名其妙不可用」而日志里什么都没有。
// 这条把那种失败变成一条明确的红。
func TestKeychainSymbolsResolve(t *testing.T) {
	// 五个符号是在同一个循环里绑的，所以 loadErr 非空就意味着其中
	// 至少一个没绑上。错误文本里带着具体是哪个符号。
	if loadErr != nil {
		t.Fatalf("钥匙串符号取不到，后端整个不可用：%v", loadErr)
	}
}

// testStore 建一个测试用的后端，测完自己清干净。
func testStore(t *testing.T) *keychainStore {
	t.Helper()
	k := &keychainStore{service: testService, account: keychainAccount}
	t.Cleanup(func() {
		// 用独立的 context：清理不能因为测试提前 Fatal 就被跳过。
		_ = k.Clear(context.Background())
	})
	return k
}

// keychainAvailable 这个环境有没有可用的钥匙串。
//
// CI、容器、ssh 会话里都可能没有（没有登录钥匙串、或者访问要弹窗）。
// 那种环境下 skip 而不是 fail——这里要证明的是「代码对不对」，
// 不是「这台机器上有没有钥匙串」。
func keychainAvailable(t *testing.T) {
	t.Helper()
	if !selftestOn {
		t.Skip("要动真钥匙串，设 BIU_KEYCHAIN_SELFTEST=1 再跑")
	}
	if loadErr != nil {
		t.Skipf("钥匙串符号取不到，跳过：%v", loadErr)
	}
	k := testStore(t)
	// 用一个空值先探一下：能加进去说明这一项可写。
	if err := k.Save(context.Background(), Session{SESSDATA: "probe-not-a-real-token"}); err != nil {
		t.Skipf("这台机器的钥匙串不可用，跳过：%v", err)
	}
}

// TestKeychainRoundTripsTheSession 是这个文件的主线：
// 存进去 → 读回来 → 一模一样。
func TestKeychainRoundTripsTheSession(t *testing.T) {
	keychainAvailable(t)
	ctx := context.Background()
	k := testStore(t)

	want := Session{
		Cookies: []Cookie{
			{Name: CookieSESSDATA, Value: "sessdata-selftest-0123456789", Domain: ".bilibili.com", Path: "/", Secure: true, HTTPOnly: true},
			{Name: CookieBiliJCT, Value: "jct-selftest-0123456789", Domain: ".bilibili.com", Path: "/"},
			{Name: CookieDedeUserID, Value: "424242", Domain: ".bilibili.com", Path: "/"},
		},
		RefreshToken:         "refresh-selftest-0123456789",
		NextCheckRefreshTime: time.Now().Add(48 * time.Hour),
	}
	want.Normalize(want.SavedAt)

	if err := k.Save(ctx, want); err != nil {
		t.Fatal(err)
	}

	got, err := k.Load(ctx)
	if err != nil {
		t.Fatalf("读不回来：%v", err)
	}
	if got.SESSDATA != want.SESSDATA || got.BiliJCT != want.BiliJCT || got.DedeUserID != want.DedeUserID {
		t.Errorf("登录态对不上：拿到 %+v", got)
	}
	if got.RefreshToken != want.RefreshToken {
		t.Errorf("refresh_token 对不上：%q", got.RefreshToken)
	}
	if len(got.Cookies) != len(want.Cookies) {
		t.Errorf("cookie 数量是 %d，want %d", len(got.Cookies), len(want.Cookies))
	}
	// HttpOnly / Secure 这两个标志最容易在往返里丢——它们决定
	// 「这段 cookie 能不能被网页脚本读到」。丢了不报错，只是安全边界松了。
	var foundSess bool
	for _, c := range got.Cookies {
		if c.Name == CookieSESSDATA {
			foundSess = true
			if !c.HTTPOnly || !c.Secure || c.Path != "/" {
				t.Errorf("SESSDATA 的属性丢了：%+v", c)
			}
		}
	}
	if !foundSess {
		t.Errorf("读回来的 cookie 里没有 SESSDATA：%+v", got.Cookies)
	}
}

// TestKeychainSaveOverwritesRatherThanDuplicating 验重复保存走的是「改内容」。
//
// 真实后果很具体：第二次保存如果报 errSecDuplicateItem，用户看到的
// 就是「已经登录了但保存失败」，而实际登录已经成功了——一个假失败。
func TestKeychainSaveOverwritesRatherThanDuplicating(t *testing.T) {
	keychainAvailable(t)
	ctx := context.Background()
	k := testStore(t)

	for i, token := range []string{"first-token-0123456789", "second-token-9876543210"} {
		if err := k.Save(ctx, Session{SESSDATA: token}); err != nil {
			t.Fatalf("第 %d 次保存返回 %v", i+1, err)
		}
		got, err := k.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got.SESSDATA != token {
			t.Errorf("第 %d 次保存之后读到的是 %q", i+1, got.SESSDATA)
		}
	}
}

// TestKeychainClearIsIdempotent 验退登连点两次不报错。
func TestKeychainClearIsIdempotent(t *testing.T) {
	keychainAvailable(t)
	ctx := context.Background()
	k := testStore(t)

	if err := k.Save(ctx, Session{SESSDATA: "to-be-cleared-0123456789"}); err != nil {
		t.Fatal(err)
	}
	if err := k.Clear(ctx); err != nil {
		t.Fatalf("第一次清：%v", err)
	}
	if err := k.Clear(ctx); err != nil {
		t.Fatalf("第二次清：%v（退登按钮连点两次不能报错）", err)
	}
	if _, err := k.Load(ctx); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("清完之后读出来是 %v，want ErrNotLoggedIn", err)
	}
}

// TestKeychainLoadOnEmptyIsNotLoggedIn 验「没存过」是正常状态。
func TestKeychainLoadOnEmptyIsNotLoggedIn(t *testing.T) {
	keychainAvailable(t)
	if _, err := testStore(t).Load(context.Background()); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("空钥匙串 Load 返回 %v，want ErrNotLoggedIn", err)
	}
}

// TestKeychainCorruptDataIsTreatedAsLoggedOut 验解不开的按退登处理。
//
// 这一条要手工往钥匙串里塞一段垃圾：解不开的凭据不能当没登录继续用，
// 但也不能让应用起不来。
func TestKeychainCorruptDataIsTreatedAsLoggedOut(t *testing.T) {
	keychainAvailable(t)
	k := testStore(t)
	if err := k.Save(context.Background(), Session{SESSDATA: "will-be-corrupted-0123456789"}); err != nil {
		t.Fatal(err)
	}
	if err := k.writeRaw([]byte("这不是 JSON，这是一段没人认得的字节")); err != nil {
		t.Fatal(err)
	}

	_, err := k.Load(context.Background())
	if !errors.Is(err, ErrCorrupt) {
		t.Errorf("坏数据 Load 返回 %v，want ErrCorrupt", err)
	}
}

// TestKeychainSurvivesAProcessRestart 是「重启应用还是登录状态」这条验收。
//
// 这里能验的跟真重启不完全等价（同一个进程、同一份动态库），
// 但它至少证明了**数据真的进了系统存储**，而不是躺在某个
// 进程内的 map 里。真杀进程重启由 settings 那边的同类测试覆盖模式，
// 钥匙串这条验不了的是「钥匙串服务在进程重启后还在」——那是系统的活。
func TestKeychainSurvivesAProcessRestart(t *testing.T) {
	keychainAvailable(t)
	ctx := context.Background()

	k := testStore(t)
	if err := k.Save(ctx, Session{SESSDATA: "restart-selftest-0123456789"}); err != nil {
		t.Fatal(err)
	}

	// 「重启」= 一个全新的后端对象，且没有任何进程内的缓存。
	fresh := &keychainStore{service: testService, account: keychainAccount}
	got, err := fresh.Load(ctx)
	if err != nil {
		t.Fatalf("重启之后读不回来：%v", err)
	}
	if got.SESSDATA != "restart-selftest-0123456789" {
		t.Errorf("重启之后读到的是 %q", got.SESSDATA)
	}
}

// TestNewSystemStoreOnMacOS 验正式入口给的是钥匙串。
//
// 只读不写：这一条验的是「入口挑对了后端」，
// 往用户的真实钥匙串里写测试数据不在验收范围内。
func TestNewSystemStoreOnMacOS(t *testing.T) {
	if loadErr != nil {
		t.Fatalf("钥匙串符号取不到：%v", loadErr)
	}
	s, err := NewSystemStore(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Name(); got != "macos-keychain" {
		t.Errorf("后端名字是 %q，want macos-keychain（写进诊断信息用）", got)
	}
	if _, ok := s.(*keychainStore); !ok {
		t.Errorf("macOS 上给出的不是钥匙串后端，是 %T", s)
	}
}
