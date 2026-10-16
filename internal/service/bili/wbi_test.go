package bili

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// # WBI 为什么单独立一个测试文件
//
// 因为它是这个包里**唯一一处错了不报错**的逻辑。签名对不上时
// B 站返回的是 -403「访问权限不足」，跟签名一个字都沾不上边——
// 一个查三天才发现「哦，是 w_rid 算错了」的 bug。
//
// 所以这里钉的是**具体数值**，不是「调用没 panic」。
// 向量用的是 B 站 WBI 说明里公开流传的那一组 key 和参数，
// 它是所有实现互相对齐用的公共基准。

// B 站说明文档里那对公开的示例 key。
const (
	sampleImgKey = "7cd084941338484aae1ad9425b84077c"
	sampleSubKey = "4932caff0ff746eab6f01bf08b70ac45"
	// sampleMixinKey 是这一对 key 算出来的混合密钥。
	// 所有实现都该算出这一个值，算不出来就是表抄错了。
	sampleMixinKey = "ea1db124af3c7062474693fa704f4ff8"
)

// TestMixinKeyMatchesThePublishedValue 钉混合密钥。
func TestMixinKeyMatchesThePublishedValue(t *testing.T) {
	if got := mixinKey(sampleImgKey, sampleSubKey); got != sampleMixinKey {
		t.Errorf("混合密钥是 %q，want %q", got, sampleMixinKey)
	}
}

// TestMixinKeyRejectsShortKeys 验 key 不够长时返回空串而不是 panic。
//
// key 来自**外部**（B 站的 nav 接口）。B 站改格式、返回空、返回 31 位
// 都是可能发生的，而下标越界是一个从编码器冒出去的 panic——
// 在界面层那就是整个进程没了。
func TestMixinKeyRejectsShortKeys(t *testing.T) {
	for _, c := range []struct{ img, sub string }{
		{"", ""},
		{sampleImgKey, ""},
		{sampleImgKey, sampleSubKey[:31]},
		{"short", "alsoshort"},
	} {
		if got := mixinKey(c.img, c.sub); got != "" {
			t.Errorf("img=%q sub=%q 算出了 %q，应该返回空串", c.img, c.sub, got)
		}
	}
}

// TestWBISignatureMatchesThePublishedVector 钉签名。
//
// 期望值是拿「另外一份独立实现」算出来的（参数排序 → 剔五个字符 →
// JS 风格编码 → 拼混合密钥 → MD5）。跟 wbiSign 是两条不同的代码路径，
// 所以这条真的在验签名，而不只是验「没 panic」。
func TestWBISignatureMatchesThePublishedVector(t *testing.T) {
	params := url.Values{
		"foo": {"114"},
		"bar": {"514"},
		"baz": {"1919810"},
	}
	now := time.Unix(1702204169, 0)

	got := wbiSign(params, sampleMixinKey, now)

	if v := got.Get("wts"); v != "1702204169" {
		t.Errorf("wts 是 %q", v)
	}
	if v := got.Get("w_rid"); v != "6149fdadf571698ca7e6a567265cd0ee" {
		t.Errorf("w_rid 是 %q，want 6149fdadf571698ca7e6a567265cd0ee", v)
	}
	// 排序后的查询串也钉一下：w_rid 是它加上密钥的哈希，
	// 顺序错了哈希一定不同，但钉住原文能让失败时一眼看出是哪一步歪了。
	// 注意是拿**签名之前**的参数去拼：签名之后多了 w_rid，
	// 拼出来的串当然不一样（w_rid 是拿不含自己的串算的）。
	if q := sortedQuery(params, now); q != "bar=514&baz=1919810&foo=114&wts=1702204169" {
		t.Errorf("查询串是 %q", q)
	}
}

// TestWBISignatureDoesNotMutateItsInput 验输入不被改。
//
// 调用方常常要把「带签名的」和「不带签名的」都留着（诊断信息用后者）。
// 被改了的话那份「不带签名的」其实已经带了 wts 和 w_rid，诊断文件里
// 就多了一段一次性凭据。
func TestWBISignatureDoesNotMutateItsInput(t *testing.T) {
	params := url.Values{"foo": {"114"}}
	before := params.Encode()
	wbiSign(params, sampleMixinKey, time.Unix(1702204169, 0))
	if after := params.Encode(); after != before {
		t.Errorf("签名改了传进来的参数：%q → %q", before, after)
	}
}

// TestWBIStripsTheFiveCharacters 验 `!'()*` 被剔掉。
//
// 漏了它签名一定对不上，而报出来的是 -403。
func TestWBIStripsTheFiveCharacters(t *testing.T) {
	params := url.Values{"q": {"a!b'c(d)e*f"}}
	got := wbiSign(params, sampleMixinKey, time.Unix(1702204169, 0))

	want := sortedQuery(url.Values{"q": {"abcdef"}, "wts": {"1702204169"}}, time.Unix(1702204169, 0))
	sum := md5.Sum([]byte(want + sampleMixinKey))
	if got.Get("w_rid") != hex.EncodeToString(sum[:]) {
		t.Errorf("剔字符之后签名是 %q，want %q", got.Get("w_rid"), hex.EncodeToString(sum[:]))
	}
}

// TestWBIEncodesSpacesAsPercentTwenty 不是加号。
//
// `url.QueryEscape` 把空格编成 `+`，而 B 站的算法用的是
// encodeURIComponent，编成 `%20`。**一个空格就够让整条签名对不上**，
// 而表现是「带空格的参数一律签名失败」。
func TestWBIEncodesSpacesAsPercentTwenty(t *testing.T) {
	if got := jsEscape("a b"); got != "a%20b" {
		t.Errorf("空格编成了 %q，want a%%20b", got)
	}
	if got := jsEscape("a+b"); got != "a%2Bb" {
		t.Errorf("加号编成了 %q，want a%%2Bb", got)
	}
	if got := jsEscape("中文"); got != "%E4%B8%AD%E6%96%87" {
		t.Errorf("中文编成了 %q", got)
	}
}

// TestWBIKeyComesFromTheImageURLs 验从 nav 的地址里取 key。
func TestWBIKeyComesFromTheImageURLs(t *testing.T) {
	cases := map[string]string{
		"https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png": sampleImgKey,
		"https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png": sampleSubKey,
		// 剥的是最后一个点之后的后缀，不是写死 ".png"：
		// B 站换过格式，写死的话哪天变成 .webp 就静默取到空 key，
		// 表现是「签名全失效」而没有任何提示。
		"https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.webp": sampleImgKey,
		"": "",
	}
	for in, want := range cases {
		if got := wbiKeyFromURL(in); got != want {
			t.Errorf("从 %q 取出的 key 是 %q，want %q", in, got, want)
		}
	}
}

// TestWBIKeysAreFetchedOnceAndCached 验 key 有缓存。
//
// 每秒一次取流请求各自去 nav 拿一遍 key，几十秒就能把接口打到风控
// （-412）。这一条钉住「只取一次」。
func TestWBIKeysAreFetchedOnceAndCached(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"wbi_img": map[string]string{
				"img_url": "https://i0.hdslb.com/bfs/wbi/" + sampleImgKey + ".png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/" + sampleSubKey + ".png",
			},
		}))
	})
	c := f.client(t, Options{
		Now: func() time.Time { return time.Unix(1702204169, 0) },
	})

	var first string
	for range 3 {
		q, err := c.signWBI(context.Background(), url.Values{"foo": {"114"}})
		if err != nil {
			t.Fatal(err)
		}
		if q.Get("w_rid") == "" {
			t.Error("没有签上名")
		}
		if first == "" {
			first = q.Get("w_rid")
		} else if q.Get("w_rid") != first {
			t.Errorf("同样的参数两次签名不同：%q / %q", first, q.Get("w_rid"))
		}
	}
	if n := len(f.requests()); n != 1 {
		t.Errorf("取了三轮 key 却发了 %d 个 nav 请求", n)
	}
}

// TestWBIKeyIsRefetchedAfterItExpires 验缓存会过期。
//
// B 站的 key 每天换。缓存不过期的话，某天早上开始全部签名失败，
// 而且没有任何日志能解释为什么「昨天还好好的」。
func TestWBIKeyIsRefetchedAfterItExpires(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"wbi_img": map[string]string{
				"img_url": "https://i0.hdslb.com/bfs/wbi/" + sampleImgKey + ".png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/" + sampleSubKey + ".png",
			},
		}))
	})
	now := time.Unix(1702204169, 0)
	c := f.client(t, Options{Now: func() time.Time { return now }})

	if _, err := c.signWBI(context.Background(), url.Values{}); err != nil {
		t.Fatal(err)
	}
	// 还在有效期内：不重取。
	if _, err := c.signWBI(context.Background(), url.Values{}); err != nil {
		t.Fatal(err)
	}
	if n := len(f.requests()); n != 1 {
		t.Fatalf("有效期内重取了：%d 个请求", n)
	}
	// 过了有效期：重取。
	now = now.Add(wbiKeyTTL + time.Second)
	if _, err := c.signWBI(context.Background(), url.Values{}); err != nil {
		t.Fatal(err)
	}
	if n := len(f.requests()); n != 2 {
		t.Errorf("过期之后没有重取 key（%d 个请求）", n)
	}
}

// TestConcurrentSigningsOnlyFetchTheKeyOnce 验并发时只取一次。
//
// 冷启动那一瞬间十几个取流请求一起来，全都去 nav 的话直接撞风控。
// 这一条是那件事唯一的钉子，所以用 -race 跑。
func TestConcurrentSigningsOnlyFetchTheKeyOnce(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		// 慢一点，让并发真的撞上。
		time.Sleep(20 * time.Millisecond)
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"wbi_img": map[string]string{
				"img_url": "https://i0.hdslb.com/bfs/wbi/" + sampleImgKey + ".png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/" + sampleSubKey + ".png",
			},
		}))
	})
	c := f.client(t, Options{})

	const n = 12
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = c.signWBI(context.Background(), url.Values{"foo": {"114"}})
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("第 %d 个并发签名失败：%v", i, err)
		}
	}
	if got := len(f.requests()); got != 1 {
		t.Errorf("%d 个并发签名发了 %d 个 nav 请求，应该只有 1 个", n, got)
	}
}

// TestSignWithoutAKeyPassesThrough 验拿不到 key 时原样放行。
//
// 不是每个接口都要签名。为了一个没登录的用户去签名，
// 反而会把本来能调的接口搞挂。
func TestSignWithoutAKeyPassesThrough(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		// B 站在某些情况下给空 key（实测有过）。
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"wbi_img": map[string]string{}}))
	})
	c := f.client(t, Options{})
	got, err := c.signWBI(context.Background(), url.Values{"foo": {"114"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("w_rid") != "" || got.Get("wts") != "" {
		t.Errorf("没有 key 却还是签了名：%v", got)
	}
	if got.Get("foo") != "114" {
		t.Errorf("原参数被弄丢了：%v", got)
	}
}

// TestSortStringsIsSorted 不是排序的地方不能出错。
func TestSortStringsIsSorted(t *testing.T) {
	in := []string{"foo", "bar", "baz", "wts", "A", "a", "0"}
	sortStrings(in)
	want := append([]string(nil), in...)
	sort.Strings(want)
	for i := range in {
		if in[i] != want[i] {
			t.Fatalf("排序结果是 %v，want %v", in, want)
		}
	}
}

// sortedQuery 独立实现一遍「排序 + 剔字符 + 编码」，用来自查签名。
//
// 它和 wbiSign 是两份代码：一份在生产路径上，一份只在测试里。
// 这正是它能当基准的原因。
func sortedQuery(v url.Values, now time.Time) string {
	cp := url.Values{}
	for k, vs := range v {
		cp[k] = vs
	}
	cp.Set("wts", itoa64(now.Unix()))
	keys := make([]string, 0, len(cp))
	for k := range cp {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		v := strings.Map(func(r rune) rune {
			if strings.ContainsRune("!'()*", r) {
				return -1
			}
			return r
		}, cp.Get(k))
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v)
	}
	return b.String()
}

func itoa64(v int64) string {
	if v == 0 {
		return "0"
	}
	var b [24]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
