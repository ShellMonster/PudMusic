// 评论接口的测试。
//
// # fixture 是**真接口的形状**
//
// `testdata/comment/*.json` 是从真的 `x/v2/reply/main` 与 `x/v2/reply/reply`
// 返回里取下来的（2026-10-26 只读探针），**不是照着 `Comment` 这个结构体
// 造的**——所以字段名对不上、某个字段忽然多了一个类型，B 站那边一改，
// 这边就会红。
//
// # 这一组盯的是那三个「静默出错」的坑
//
//	请求头里不能有 buvid3	（带了 B 站把列表砍到 3 条并宣称「到底了」）
//	请求串里不能有 w_rid	（不签 WBI 也发得出去）
//	ps 不能超过 30		（超了 B 站默默截断，翻页会漏掉一批）
package bili

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// commentFixture 读一份真接口形状的 fixture。
func commentFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "comment", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// commentFake 是一台只答评论那两个接口的假 B 站。
//
// 🔴 **它也答 `/x/frontend/finger/spi`**（取 buvid 那个），而且答的是**真的**
// buvid 值——不然「请求头里没有 buvid3」那条断言是**空的**：
// `Call.Buvid` 那一路会先去 spi 取 buvid，取不到就什么都不加，于是即使
// 代码里写了 `Buvid: true`，请求头里也照样没有 buvid3——**断言照样绿**。
//
// 让 spi 能答上，「代码里开了 Buvid」就一定会被这一条抓住。
func commentFake(t *testing.T, mainBody string) (*fakeBili, *Client) {
	t.Helper()
	f := newFakeBili(t)
	f.on(pathReplyMain, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, mainBody)
	})
	f.on("/x/frontend/finger/spi", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "ok", map[string]any{
			"b_3": "BUVID3-FROM-SPI",
			"b_4": "BUVID4-FROM-SPI",
		}))
	})
	// 🔴 **nav 也要答**（理由同上）：WBI 那条路会先去 nav 取 key，而
	// `signWBI` **拿不到 key 时会「原样放行」**（不签名直接发）——那正好也是
	// 「请求串里没有 w_rid」，于是「没签名」这条断言就成了空的。
	//
	// 让 nav 能答上，代码里真开了 `WBI: true` 就一定会被抓住（签名会出现）。
	f.on(pathNav, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"wbi_img": map[string]string{
				"img_url": "https://i0.hdslb.com/bfs/wbi/" + sampleImgKey + ".png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/" + sampleSubKey + ".png",
			},
		}))
	})
	return f, f.client(t, Options{})
}

// lastCommentRequest 是打给主列表的最后一次请求。
func lastCommentRequest(t *testing.T, f *fakeBili) recordedRequest {
	t.Helper()
	var last recordedRequest
	found := false
	for _, rec := range f.requests() {
		if rec.Path == pathReplyMain {
			last, found = rec, true
		}
	}
	if !found {
		t.Fatalf("没有打给 %s 的请求", pathReplyMain)
	}
	return last
}

// ─────────────────────────────────────────────────────────────────────────────
// 坑一：buvid3
// ─────────────────────────────────────────────────────────────────────────────

// TestCommentListNeverSendsBuvid 钉住「评论请求里没有 buvid3」。
//
// 🔴 **这是这一票最要紧的一条**：带 `buvid3` 会让 B 站把评论列表**砍到 3 条
// 并宣称「到底了」**（实测 2026-10-26，见文件头）。而本仓库「取流要带 buvid」
// 是既有习惯，照抄就会踩。
//
// 钉的方式是**看发出去的头**——不是看代码里有没有写那个字段。
func TestCommentListNeverSendsBuvid(t *testing.T) {
	f, c := commentFake(t, commentFixture(t, "reply-main.json"))
	if _, err := c.CommentList(context.Background(), CommentTypeVideo, 117506312373775, int(CommentSortHot), "", 20); err != nil {
		t.Fatal(err)
	}
	rec := lastCommentRequest(t, f)
	if ck := rec.Header.Get("Cookie"); strings.Contains(ck, "buvid3") {
		t.Fatalf("评论请求带了 buvid3：%q（B 站会因此只给 3 条）", ck)
	}
	// 也不能有 buvid4 / b_nut 那一族——同一个开关带的是一整串。
	if ck := rec.Header.Get("Cookie"); strings.Contains(ck, "buvid") {
		t.Fatalf("评论请求带了 buvid 那一族：%q", ck)
	}
}

// TestCommentListNeverSendsTheBuvidThatLivesInTheJar 钉住「jar 里的 buvid3
// 也不会被带出去」。
//
// 🔴 **这一条是上一轮评审指出来的那个洞**：上面那条
// （`TestCommentListNeverSendsBuvid`）只在**空 jar**下跑，而 jar 是空的时
// 「代码里没写 `Buvid: true`」就足够让断言绿。
//
// 但 jar **不是**空的：
//
//   - jar 里的 cookie 是在 `http.Client.send` 里加的，而那**在运输层之前**
//     （`net/http/client.go`：`c.Jar.Cookies(url)` → `req.AddCookie` →
//     `RoundTrip`）——所以「不设 `Call.Buvid`」**挡不住** jar 里已有的一枚
//     buvid3；
//   - jar 里**确实**会有 buvid3：登录成功时 B 站下发的**全部** cookie 都被
//     存下来（`app.Account.finish` 收下 `IssuedCookies()` 的每一条），而
//     B 站的 cookie 域是 `.bilibili.com`，`api.bilibili.com` 收得到。
//
// ⇒ 症状是「登录之后评论只有 3 条」，而 B 站那句「到底了」说得天衣无缝。
//
// 所以这条把 jar 装上（`SeedCookies`）再拉评论，断言**发出去的头里没有
// buvid3**，而**登录态照带**（SESSDATA 还在——摘设备标识不是摘凭据）。
func TestCommentListNeverSendsTheBuvidThatLivesInTheJar(t *testing.T) {
	f, c := commentFake(t, commentFixture(t, "reply-main.json"))
	if err := c.SeedCookies([]*http.Cookie{
		{Name: "SESSDATA", Value: "review-only"},
		{Name: "buvid3", Value: "REVIEW-BUVID"},
		{Name: "buvid4", Value: "REVIEW-BUVID4"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CommentList(context.Background(), CommentTypeVideo, 117506312373775, int(CommentSortHot), "", 20); err != nil {
		t.Fatal(err)
	}
	rec := lastCommentRequest(t, f)
	ck := rec.Header.Get("Cookie")
	if strings.Contains(ck, "buvid") {
		t.Fatalf("评论请求带出了 jar 里的 buvid 那一族：%q（B 站会因此只给 3 条）", ck)
	}
	// ⚠️ **登录态必须还在**：摘的是设备标识，不是凭据。把 SESSDATA 一起摘掉
	// 的话，登录用户看到的评论会比匿名用户还少。
	if !strings.Contains(ck, "SESSDATA=review-only") {
		t.Fatalf("登录态被一起摘掉了：%q", ck)
	}
}

// TestCommentRepliesNeverSendsTheBuvidThatLivesInTheJar 楼中楼那一条路
// 同样要挡住 jar 里的 buvid3。
func TestCommentRepliesNeverSendsTheBuvidThatLivesInTheJar(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathReplyReply, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, commentFixture(t, "reply-reply.json"))
	})
	f.on("/x/frontend/finger/spi", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "ok", map[string]any{
			"b_3": "BUVID3-FROM-SPI",
			"b_4": "BUVID4-FROM-SPI",
		}))
	})
	c := f.client(t, Options{})
	if err := c.SeedCookies([]*http.Cookie{
		{Name: "SESSDATA", Value: "review-only"},
		{Name: "buvid3", Value: "REVIEW-BUVID"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.CommentReplies(context.Background(), CommentTypeVideo, 117506312373775, 321942994928, 1, 20); err != nil {
		t.Fatal(err)
	}
	for _, rec := range f.requests() {
		if rec.Path != pathReplyReply {
			continue
		}
		ck := rec.Header.Get("Cookie")
		if strings.Contains(ck, "buvid") {
			t.Fatalf("楼中楼请求带出了 jar 里的 buvid 那一族：%q", ck)
		}
		if !strings.Contains(ck, "SESSDATA=review-only") {
			t.Fatalf("登录态被一起摘掉了：%q", ck)
		}
	}
}

// TestCommentRepliesNeverSendsBuvid 楼中楼那一条路也一样。
func TestCommentRepliesNeverSendsBuvid(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathReplyReply, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, commentFixture(t, "reply-reply.json"))
	})
	// 🔴 **spi 也要答**（理由见 `commentFake` 的注释）：不然这一条是空的。
	f.on("/x/frontend/finger/spi", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "ok", map[string]any{
			"b_3": "BUVID3-FROM-SPI",
			"b_4": "BUVID4-FROM-SPI",
		}))
	})
	c := f.client(t, Options{})
	if _, _, err := c.CommentReplies(context.Background(), CommentTypeVideo, 117506312373775, 321942994928, 1, 20); err != nil {
		t.Fatal(err)
	}
	for _, rec := range f.requests() {
		if rec.Path != pathReplyReply {
			continue
		}
		if ck := rec.Header.Get("Cookie"); strings.Contains(ck, "buvid") {
			t.Fatalf("楼中楼请求带了 buvid 那一族：%q", ck)
		}
	}
	for _, rec := range f.requests() {
		if strings.Contains(rec.Path, "finger/spi") {
			t.Fatalf("楼中楼这一趟去取了 buvid：%s", rec.Path)
		}
	}
}

// TestCommentListDoesNotAskForBuvid 盯「连取 buvid 那一次请求都没有」。
//
// `Call.Buvid` 不只是「加一个头」——它还会**先去 `/x/frontend/finger/spi`
// 取一次 buvid**。所以「没带 buvid3」有两种做法：真的没去取，和取了但没发。
// 后者虽然不出错，但白花一次往返，而且下次别的接口调 `buvidCookie` 时会拿到
// 一份**已经被这个客户端缓存住**的 buvid。
func TestCommentListDoesNotAskForBuvid(t *testing.T) {
	f, c := commentFake(t, commentFixture(t, "reply-main.json"))
	if _, err := c.CommentList(context.Background(), CommentTypeVideo, 117506312373775, int(CommentSortHot), "", 20); err != nil {
		t.Fatal(err)
	}
	for _, rec := range f.requests() {
		if strings.Contains(rec.Path, "finger/spi") {
			t.Fatalf("评论这一趟去取了 buvid：%s", rec.Path)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 坑二：WBI
// ─────────────────────────────────────────────────────────────────────────────

// TestCommentListIsNotSigned 钉住「不签 WBI 也发得出去」。
//
// `/x/v2/reply/wbi/main` 少了签名就是 -403；本仓库的 `signWBI` 在拿不到 key
// 时会「原样放行」——那等于发一个必然被拒的请求。所以评论这一处走的是
// **不带 WBI 的** `/x/v2/reply/main`，请求串里**不该有** `w_rid`。
func TestCommentListIsNotSigned(t *testing.T) {
	f, c := commentFake(t, commentFixture(t, "reply-main.json"))
	if _, err := c.CommentList(context.Background(), CommentTypeVideo, 117506312373775, int(CommentSortHot), "", 20); err != nil {
		t.Fatal(err)
	}
	rec := lastCommentRequest(t, f)
	if rec.Query.Has("w_rid") {
		t.Fatalf("评论请求带了 WBI 签名：%v", rec.Query)
	}
	if rec.Query.Has("wts") {
		t.Fatalf("评论请求带了 wts：%v", rec.Query)
	}
	// 也别偷偷去打 nav 拿 key。
	for _, r := range f.requests() {
		if strings.Contains(r.Path, "/nav") {
			t.Fatalf("评论这一趟去取 WBI key 了：%s", r.Path)
		}
	}
}

// TestCommentListGoesToTheUnsignedEndpoint 钉住「走的是不带 WBI 的那个路径」。
//
// 光断言「没带 w_rid」不够：**路径**也得钉住——`/x/v2/reply/wbi/main` 那个
// 路径就是「必须签名」的那一个。
func TestCommentListGoesToTheUnsignedEndpoint(t *testing.T) {
	f, c := commentFake(t, commentFixture(t, "reply-main.json"))
	if _, err := c.CommentList(context.Background(), CommentTypeVideo, 117506312373775, int(CommentSortHot), "", 20); err != nil {
		t.Fatal(err)
	}
	rec := lastCommentRequest(t, f)
	if rec.Path != pathReplyMain {
		t.Fatalf("打的是 %q，want %q", rec.Path, pathReplyMain)
	}
	if strings.Contains(rec.Path, "wbi") {
		t.Fatalf("打的是带 WBI 的那条路：%q", rec.Path)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 坑三：ps 不超过 30
// ─────────────────────────────────────────────────────────────────────────────

// TestCommentListNeverAsksForMoreThanThirty 钉住「ps 不超过 30」。
//
// ⚠️ **超了 B 站不报错**，只是默默按 30 截断——于是翻页从第二页开始会漏掉
// 一批，而界面上看不出任何异样（总数在那儿，列表在增长，唯独中间少了一段）。
func TestCommentListNeverAsksForMoreThanThirty(t *testing.T) {
	f, c := commentFake(t, commentFixture(t, "reply-main.json"))
	if _, err := c.CommentList(context.Background(), CommentTypeVideo, 117506312373775, int(CommentSortHot), "", 100); err != nil {
		t.Fatal(err)
	}
	rec := lastCommentRequest(t, f)
	if got := rec.Query.Get("ps"); got != "30" {
		t.Fatalf("ps = %q，want 30", got)
	}
}

// TestCommentRepliesNeverAsksForMoreThanTwenty 楼中楼的上限是 20。
//
// 实测 `ps=30` / `ps=49` 都只给 20 条（`page.size` 回的也是 20）。
func TestCommentRepliesNeverAsksForMoreThanTwenty(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathReplyReply, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, commentFixture(t, "reply-reply.json"))
	})
	c := f.client(t, Options{})
	if _, _, err := c.CommentReplies(context.Background(), CommentTypeVideo, 117506312373775, 321942994928, 1, 49); err != nil {
		t.Fatal(err)
	}
	for _, rec := range f.requests() {
		if rec.Path != pathReplyReply {
			continue
		}
		if got := rec.Query.Get("ps"); got != "20" {
			t.Fatalf("ps = %q，want 20", got)
		}
	}
}

// TestCommentPageSizeWithinTheLimitIsNotChanged 钉住「没超上限时照原样发」。
//
// 上一条钉的是「超了要封顶」；这一条钉「没超别乱改」——把 5 改成 30 也是错的
// （那会让每一页都比用户要的��，而翻页的节奏是界面说了算）。
func TestCommentPageSizeWithinTheLimitIsNotChanged(t *testing.T) {
	f, c := commentFake(t, commentFixture(t, "reply-main.json"))
	if _, err := c.CommentList(context.Background(), CommentTypeVideo, 117506312373775, int(CommentSortHot), "", 5); err != nil {
		t.Fatal(err)
	}
	rec := lastCommentRequest(t, f)
	if got := rec.Query.Get("ps"); got != "5" {
		t.Fatalf("ps = %q，want 5", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 字段
// ─────────────────────────────────────────────────────────────────────────────

// TestCommentListReadsTheFieldsOffTheRealShape 验字段是照着**真形状**读的。
//
// 这一条是票 #571 的第一条验收。fixture 是真的 B 站返回（2026-10-26 探针），
// 所以它顺带钉住了几件「B 站改了就会红」的事：
//
//	头像在 `member.avatar`（**不在 `face`**——`face` 这个键压根不存在）
//	认证在 `member.official_verify.type`（**没有认证时是 -1**，不是 0）
//	等级在 `member.level_info.current_level`
//	相对时间在 `reply_control.time_desc`（「7小时前发布」这种现成中文）
//	回复数文案在 `reply_control.sub_reply_entry_text`（「共52条回复」）
func TestCommentListReadsTheFieldsOffTheRealShape(t *testing.T) {
	_, c := commentFake(t, commentFixture(t, "reply-main.json"))
	page, err := c.CommentList(context.Background(), CommentTypeVideo, 117506312373775, int(CommentSortHot), "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Replies) == 0 {
		t.Fatal("一条评论都没读出来")
	}

	upper := page.Replies[0]
	if upper.Uname != "大圆镜科普" {
		t.Errorf("昵称 = %q，want 大圆镜科普", upper.Uname)
	}
	if !strings.HasPrefix(upper.Avatar, "https://i0.hdslb.com/bfs/face/") {
		t.Errorf("头像 = %q，want member.avatar 那个地址", upper.Avatar)
	}
	if upper.Level != 6 {
		t.Errorf("等级 = %d，want 6（member.level_info.current_level）", upper.Level)
	}
	// ⚠️ **没有认证时 B 站给的是 -1**（不是 0）。
	if upper.OfficialType != 0 {
		t.Errorf("认证类型 = %d，want 0（这一条是有个人认证的）", upper.OfficialType)
	}
	if upper.OfficialDesc != "bilibili 知名科普UP主" {
		t.Errorf("认证文案 = %q", upper.OfficialDesc)
	}
	if upper.TimeDesc != "7小时前发布" {
		t.Errorf("相对时间 = %q，want 「7小时前发布」", upper.TimeDesc)
	}
	if upper.SubReplyEntryText != "共52条回复" {
		t.Errorf("回复数文案 = %q，want 「共52条回复」", upper.SubReplyEntryText)
	}
	if upper.Like != 401 {
		t.Errorf("点赞数 = %d，want 401", upper.Like)
	}
	if upper.Count != 61 || upper.RCount != 52 {
		t.Errorf("回复数 = %d / %d，want 61 / 52", upper.Count, upper.RCount)
	}
	if upper.Ctime != 1793005248 {
		t.Errorf("发表时间 = %d，want 1793005248", upper.Ctime)
	}
	if upper.Rpid != 321942994928 {
		t.Errorf("评论 id = %d，want 321942994928", upper.Rpid)
	}

	// 表情与图片：正文里的占位符要能换成图。
	var sawEmote, sawPicture bool
	for _, r := range page.Replies {
		if len(r.Emotes) > 0 {
			sawEmote = true
			for placeholder, e := range r.Emotes {
				if placeholder == "" {
					t.Error("表情的占位符是空串")
				}
				if e.URL == "" {
					t.Errorf("表情 %q 没有图片地址", placeholder)
				}
			}
		}
		if len(r.Pictures) > 0 {
			sawPicture = true
			for _, p := range r.Pictures {
				if p.URL == "" {
					t.Error("图片没有地址")
				}
				if p.Width == 0 || p.Height == 0 {
					t.Errorf("图片尺寸是 %d×%d", p.Width, p.Height)
				}
			}
		}
	}
	if !sawEmote {
		t.Error("一条带表情的评论都没读到")
	}
	if !sawPicture {
		t.Error("一条带图片的评论都没读到")
	}
}

// TestCommentListReadsThePageMeta 验游标那一栏。
//
// ⚠️ **「B 站实际采用的排序」不是「我们请求的那个」**：实测请求 `mode=0` 时
// B 站回的是 `cursor.mode=3`（热门评论）。所以界面上写「最新评论」要认
// `page.Mode`，别认自己传的那个。
func TestCommentListReadsThePageMeta(t *testing.T) {
	_, c := commentFake(t, commentFixture(t, "reply-main.json"))
	page, err := c.CommentList(context.Background(), CommentTypeVideo, 117506312373775, int(CommentSortHot), "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if page.AllCount != 388 {
		t.Errorf("评论总数 = %d，want 388", page.AllCount)
	}
	if page.Next != 2 {
		t.Errorf("下一页游标 = %d，want 2", page.Next)
	}
	if page.IsEnd {
		t.Error("还没到底呢（cursor.is_end 明明是 false）")
	}
	if page.UpperMid != 1208823126 {
		t.Errorf("UP 主 mid = %d，want 1208823126", page.UpperMid)
	}
	if page.Mode != 3 {
		t.Errorf("排序 = %d，want 3", page.Mode)
	}
	if page.Name != "热门评论" {
		t.Errorf("排序名 = %q，want 热门评论", page.Name)
	}
}

// TestIsUpperIsTheAuthorNotTheOneTheUpperRepliedTo 钉住「这条是不是 UP 主发的」。
//
// 🔴 **判据是 `mid == data.upper.mid`，不是 `reply_control.up_reply`。**
//
// `up_reply` 的意思是「**UP 主回复过这一条**」，跟「这条是 UP 主发的」是两
// 回事。实测（2026-10-26，oid=114256632618805）：`upper.mid` = 654552，而带
// `up_reply=true` 的那几条评论 mid 是 294067289 / 594810944 / …——**没有一条
// 等于 UP 主**。
//
// fixture 里那几条带 `up_reply` 的评论正好是这一档：拿 `up_reply` 当判据的话，
// 它们会被错标成「UP 主发的」。
func TestIsUpperMarksOnlyTheUpAuthorsOwnComment(t *testing.T) {
	_, c := commentFake(t, commentFixture(t, "reply-main.json"))
	page, err := c.CommentList(context.Background(), CommentTypeVideo, 117506312373775, int(CommentSortHot), "", 20)
	if err != nil {
		t.Fatal(err)
	}

	var marked int
	for _, r := range page.Replies {
		want := r.Mid == page.UpperMid
		if r.IsUpper != want {
			t.Errorf("评论 %d（mid %d）的 IsUpper = %v，want %v（upper.mid=%d）",
				r.Rpid, r.Mid, r.IsUpper, want, page.UpperMid)
		}
		if r.IsUpper {
			marked++
		}
	}
	// fixture 里确实有一条 UP 主自己发的评论（大圆镜科普）——**没有它这一条
	// 就验不到东西**（一个空集上的断言永远是绿的）。
	if marked != 1 {
		t.Errorf("标成 UP 主发的有 %d 条，want 1（只有 mid == upper.mid 那一条）", marked)
	}
}

// TestUpReplyCommentsAreNotMarkedAsTheUpper 单独钉「up_reply 那几条」。
//
// 这一条是上一条的**反面**：没有 UP 主自己发的评论时（那个 fixture 里的
// `upper.mid` 654552 自己没发言），所有 `up_reply=true` 的评论都必须**不是**
// UP 主发的。
func TestUpReplyCommentsAreNotMarkedAsTheUpper(t *testing.T) {
	_, c := commentFake(t, commentFixture(t, "reply-main-upreply.json"))
	page, err := c.CommentList(context.Background(), CommentTypeVideo, 114256632618805, int(CommentSortHot), "", 20)
	if err != nil {
		t.Fatal(err)
	}

	// fixture 里确实有 `up_reply=true` 的那几条（原始返回里带着）。
	var upReplied int
	for _, r := range page.Replies {
		if r.Mid == page.UpperMid {
			continue
		}
		if r.IsUpper {
			t.Errorf("评论 %d（mid %d）被标成了 UP 主发的（upper.mid=%d）",
				r.Rpid, r.Mid, page.UpperMid)
		}
		upReplied++
	}
	if upReplied == 0 {
		t.Fatal("fixture 里一条评论都没有")
	}
}

// TestCommentListReadsTheRawUpReplyFlag 确认 fixture 里**真的**有 up_reply。
//
// 上一条验的是「我们没把它当成判据」，这一条验的是「fixture 里真的有那个
// 字段」——不然上一条就只是「一个空集上的断言」。
func TestCommentListReadsTheRawUpReplyFlag(t *testing.T) {
	body := commentFixture(t, "reply-main-upreply.json")
	var env struct {
		Data struct {
			Replies []struct {
				ReplyControl struct {
					UpReply bool `json:"up_reply"`
				} `json:"reply_control"`
			} `json:"replies"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatal(err)
	}
	var n int
	for _, r := range env.Data.Replies {
		if r.ReplyControl.UpReply {
			n++
		}
	}
	if n == 0 {
		t.Fatal("fixture 里一条 up_reply 的评论都没有——这个 fixture 验不到「up_reply 不是判据」")
	}
}

// TestCommentListReadsTheEmptyPage 验「0 条评论」那一条路。
//
// 这一条是票 #571 的验收里「0 条评论」那一半：它**不是** -404（返回的是
// code 0 + 空列表 + `cursor.is_end=true`），但**也有一个自己的哨兵**。
//
// ⚠️ **它不是失败**：这一页**照常返回**（`AllCount` / `UpperMid` 那些都还有
// 值），只是**同时**带出 `ErrCommentNoComments`——所以调用方能一眼分清
// 「没人评论」和「拉失败了」。
func TestCommentListReadsTheEmptyPage(t *testing.T) {
	_, c := commentFake(t, commentFixture(t, "reply-main-empty.json"))
	page, err := c.CommentList(context.Background(), CommentTypeAudio, 563526, int(CommentSortHot), "", 20)
	if !errors.Is(err, ErrCommentNoComments) {
		t.Fatalf("err = %v，want ErrCommentNoComments", err)
	}
	// ⚠️ **但这一页照样返回**——它不是失败，是「一个空列表」。
	if len(page.Replies) != 0 {
		t.Errorf("0 条评论那一页读出了 %d 条", len(page.Replies))
	}
	if !page.IsEnd {
		t.Error("空页面的 is_end 应该是 true")
	}
	if page.UpperMid != 92962414 {
		t.Errorf("UP 主 mid = %d，want 92962414（空列表也有）", page.UpperMid)
	}
	// 它**不是**「对象不在了」。
	if errors.Is(err, ErrCommentTargetMissing) {
		t.Error("「还没有人评论」被认成了「对象不在了」——这两件事要分开说")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 错误：-404 与「0 条评论」是两回事
// ─────────────────────────────────────────────────────────────────────────────

// TestCommentListSaysTheObjectIsGoneWhenBiliSays404 钉住「-404 → 哨兵」。
//
// 🔴 **-404（对象不存在）和「0 条评论」是两回事**：前者是「这个稿件/这首歌
// 不在了」，后者是「它在，只是没人评论」。混成一句话的话，用户看到「这个对象
// 不在了」会以为歌没了，而歌好好地摆在那儿——**他会去重新下载一遍**。
func TestCommentListSaysTheObjectIsGoneWhenBiliSays404(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathReplyMain, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, -404, "啥都木有", nil))
	})
	c := f.client(t, Options{})
	_, err := c.CommentList(context.Background(), CommentTypeVideo, 999999999999, int(CommentSortHot), "", 20)
	if err == nil {
		t.Fatal("-404 被当成了成功")
	}
	if !errors.Is(err, ErrCommentTargetMissing) {
		t.Fatalf("err = %v，want ErrCommentTargetMissing", err)
	}
	// 那一句话必须和「0 条评论」的那句**不一样**。
	msg := CommentFriendly(err)
	if msg == "" {
		t.Fatal("这一句是空串")
	}
	if strings.Contains(msg, "还没有人评论") || strings.Contains(msg, "没有评论") {
		t.Errorf("把「对象不在了」说成了「没人评论」：%q", msg)
	}
	if !strings.Contains(msg, "不在了") {
		t.Errorf("话里没说对象不在了：%q", msg)
	}
}

// TestTheEmptyListAndTheMissingObjectSayDifferentThings 钉住「两句不一样」。
//
// 这一条是上一条的**对照**：同样是一个空结果，「对象不在了」和「还没有人
// 评论」必须说得不一样——**两个哨兵，两个句子**（票 #571 的验收）。
func TestTheEmptyListAndTheMissingObjectSayDifferentThings(t *testing.T) {
	// 四种情况**各有一句**，而且**互不相同**。
	missing := CommentFriendly(ErrCommentTargetMissing)
	closed := CommentFriendly(ErrCommentSectionClosed)
	notFound := CommentFriendly(ErrCommentNotFound)
	none := CommentFriendly(ErrCommentNoComments)

	if missing == "" || closed == "" || notFound == "" || none == "" {
		t.Fatal("有一句是空串")
	}
	if missing == closed {
		t.Errorf("「对象不在了」和「评论区关了」说的是同一句：%q", missing)
	}
	if missing == notFound {
		t.Errorf("「对象不在了」和「没有这一条」说的是同一句：%q", missing)
	}
	if closed == notFound {
		t.Errorf("「评论区关了」和「没有这一条」说的是同一句：%q", closed)
	}
	// 🔴 **这一对是最要紧的**：混了的话，用户看到「这个对象不在了」会以为歌
	// 没了，而歌好好地摆在那儿——**他会去重新下载一遍**。
	if missing == none {
		t.Errorf("「对象不在了」和「还没有人评论」说的是同一句：%q", missing)
	}
	if strings.Contains(none, "不在了") {
		t.Errorf("「还没有人评论」那句话里说了「不在了」：%q", none)
	}
}

// TestCommentSectionClosedHasItsOwnSentinel 钉住「评论区被关了」这一档。
//
// 实测（2026-10-26，oid=117502252158200）：UP 主关了评论区时 B 站回的是
// **12061**，不是 -404 也不是空列表。
func TestCommentSectionClosedHasItsOwnSentinel(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathReplyMain, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 12061, "UP主已关闭评论区", nil))
	})
	c := f.client(t, Options{})
	_, err := c.CommentList(context.Background(), CommentTypeVideo, 117502252158200, int(CommentSortHot), "", 20)
	if !errors.Is(err, ErrCommentSectionClosed) {
		t.Fatalf("err = %v，want ErrCommentSectionClosed", err)
	}
	if errors.Is(err, ErrCommentTargetMissing) {
		t.Error("「评论区关了」被认成了「对象不在了」——这两句话要分开说")
	}
	if msg := CommentFriendly(err); !strings.Contains(msg, "关掉") {
		t.Errorf("话里没说评论区被关了：%q", msg)
	}
}

// TestCommentRepliesSaysTheRootIsGone 楼中楼那条路的 12006。
func TestCommentRepliesSaysTheRootIsGone(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathReplyReply, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 12006, "没有该评论", nil))
	})
	c := f.client(t, Options{})
	_, _, err := c.CommentReplies(context.Background(), CommentTypeVideo, 170001, 999999999, 1, 20)
	if !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("err = %v，want ErrCommentNotFound", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 错误：-403 在评论这一处判成可重试
// ─────────────────────────────────────────────────────────────────────────────

// TestCommentTreatsForbiddenAsWorthRetrying 钉住「-403 在评论这一处可重试」。
//
// 🔴 **全局那张表把 -403 判成不可重试**（`Recoverable` 里没有它 ⇒ false），
// 那在别的接口上是对的，在评论接口上是**错的**：评论接口的 -403 多半是
// 「签名没签上 / 被挡了一下」，**再问一次有可能好**。
//
// ⇒ 这一处**单独判**，全局那张表一个字都不改。
func TestCommentTreatsForbiddenAsWorthRetrying(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathReplyMain, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, -403, "访问权限不足", nil))
	})
	c := f.client(t, Options{})
	_, err := c.CommentList(context.Background(), CommentTypeVideo, 170001, int(CommentSortHot), "", 20)
	if err == nil {
		t.Fatal("-403 被当成了成功")
	}
	if !CommentRecoverable(err) {
		t.Errorf("评论这一处的 -403 被判成不可重试了（多半只是被挡了一下）")
	}
	// ⚠️ **全局那张表一个字都不许改**——它管的是别的接口。
	if Recoverable(err) {
		t.Error("全局的 Recoverable 也变了——那会波及别的接口（票 #571 的纪律）")
	}
	// 人话也要跟全局那句不一样。
	msg := CommentFriendly(err)
	if msg == codeMessages[-403] {
		t.Errorf("评论这一处还是用了全局那句「%q」——那一句在评论这儿是错的", msg)
	}
	if msg == "" {
		t.Error("这一句是空串")
	}
}

// TestCommentDoesNotRetryTheErrorsThatWillNotChange 钉住「另外那几种不重试」。
//
// 「对象不在了」「评论区关了」再问多少次都一样——给一个按了也没用的「重试」
// 比不给更糟（用户按两次发现还是那样，就不再信这个应用了）。
func TestCommentDoesNotRetryTheErrorsThatWillNotChange(t *testing.T) {
	for _, c := range []struct {
		what string
		err  error
	}{
		{"对象不在了", ErrCommentTargetMissing},
		{"评论区关了", ErrCommentSectionClosed},
		{"没有这一条", ErrCommentNotFound},
		// ⚠️ **「还没有人评论」压根没有错**——用户要的是一个空列表，
		// 不是「再问一次」。
		{"还没有人评论", ErrCommentNoComments},
	} {
		if CommentRecoverable(c.err) {
			t.Errorf("%s：判成了可重试——再问多少次都一样", c.what)
		}
	}
	if CommentRecoverable(nil) {
		t.Error("nil 也说成可以重试")
	}
}

// TestCommentFriendlyAlwaysSaysSomething 盯「认不出来也要给一句人话」。
func TestCommentFriendlyAlwaysSaysSomething(t *testing.T) {
	for _, err := range []error{
		errors.New("something went sideways"),
		&APIError{Code: -99999, Message: "内部错误", Endpoint: "/x"},
	} {
		if CommentFriendly(err) == "" {
			t.Errorf("%v 变成了空串", err)
		}
	}
	if CommentFriendly(nil) != "" {
		t.Error("nil 也给了话")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 楼中楼
// ─────────────────────────────────────────────────────────────────────────────

// TestCommentRepliesReadsTheRealShape 验楼中楼那一路读的是真形状。
func TestCommentRepliesReadsTheRealShape(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathReplyReply, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, commentFixture(t, "reply-reply.json"))
	})
	c := f.client(t, Options{})
	replies, count, err := c.CommentReplies(context.Background(), CommentTypeVideo, 117506312373775, 321942994928, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if count != 52 {
		t.Errorf("总回复数 = %d，want 52（page.count）", count)
	}
	if len(replies) == 0 {
		t.Fatal("一条楼中楼都没读出来")
	}
	first := replies[0]
	if first.Rpid == 0 {
		t.Error("rpid 是 0")
	}
	if first.Uname == "" {
		t.Error("昵称是空串")
	}
	if first.Avatar == "" {
		t.Error("头像是空串")
	}
	if first.Message == "" {
		t.Error("正文是空串")
	}
	// 楼中楼里的 root 是**那条被回复的主评论**。
	if first.Root != 321942994928 {
		t.Errorf("root = %d，want 321942994928", first.Root)
	}
	if first.Parent == 0 {
		t.Error("parent 是 0")
	}
	// 参数要照发。
	for _, rec := range f.requests() {
		if rec.Path != pathReplyReply {
			continue
		}
		if got := rec.Query.Get("root"); got != "321942994928" {
			t.Errorf("root = %q", got)
		}
		if got := rec.Query.Get("pn"); got != "1" {
			t.Errorf("pn = %q，want 1", got)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 请求参数
// ─────────────────────────────────────────────────────────────────────────────

// TestCommentListSendsTheTypeAndOid 钉住「type / oid 照发」。
//
// ⚠️ **视频的 oid 是 avid（不是 bvid）**，音乐的 oid 是 auid。发错的话 B 站
// 回 -404，而那一句是「啥都木有」——看不出是 id 发错了。
//
// 🔴 **断言必须钉到「这一对确切的值」**：写成「type 是 1 或 14 都算对、
// oid 是 170001 或 563526 都算对」的话，实现**永远发同一对参数**这两条
// 子用例照样全绿——那等于没钉。所以下面按子用例各自写死 want。
func TestCommentListSendsTheTypeAndOid(t *testing.T) {
	for _, tc := range []struct {
		what     string
		typ      int
		oid      int64
		wantType string
		wantOID  string
	}{
		{"视频", CommentTypeVideo, 170001, "1", "170001"},
		{"音乐库音频", CommentTypeAudio, 563526, "14", "563526"},
	} {
		t.Run(tc.what, func(t *testing.T) {
			f, cl := commentFake(t, commentFixture(t, "reply-main.json"))
			if _, err := cl.CommentList(context.Background(), int64(tc.typ), tc.oid, int(CommentSortHot), "", 20); err != nil {
				t.Fatal(err)
			}
			rec := lastCommentRequest(t, f)
			if got := rec.Query.Get("type"); got != tc.wantType {
				t.Errorf("type = %q，want %q", got, tc.wantType)
			}
			if got := rec.Query.Get("oid"); got != tc.wantOID {
				t.Errorf("oid = %q，want %q", got, tc.wantOID)
			}
		})
	}
}

// TestCommentRepliesSendsTheTypeAndOid 楼中楼那一条路也得照发 type / oid。
//
// ⚠️ **同样钉到确切的值**：理由同 `TestCommentListSendsTheTypeAndOid`——
// 「两个都算对」的断言钉不住任何东西。
func TestCommentRepliesSendsTheTypeAndOid(t *testing.T) {
	for _, tc := range []struct {
		what     string
		typ      int
		oid      int64
		wantType string
		wantOID  string
	}{
		{"视频", CommentTypeVideo, 170001, "1", "170001"},
		{"音乐库音频", CommentTypeAudio, 563526, "14", "563526"},
	} {
		t.Run(tc.what, func(t *testing.T) {
			f := newFakeBili(t)
			f.on(pathReplyReply, func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, commentFixture(t, "reply-reply.json"))
			})
			cl := f.client(t, Options{})
			if _, _, err := cl.CommentReplies(context.Background(), int64(tc.typ), tc.oid, 321942994928, 1, 20); err != nil {
				t.Fatal(err)
			}
			var rec recordedRequest
			for _, r := range f.requests() {
				if r.Path == pathReplyReply {
					rec = r
				}
			}
			if rec.Path == "" {
				t.Fatalf("没有打给 %s 的请求", pathReplyReply)
			}
			if got := rec.Query.Get("type"); got != tc.wantType {
				t.Errorf("type = %q，want %q", got, tc.wantType)
			}
			if got := rec.Query.Get("oid"); got != tc.wantOID {
				t.Errorf("oid = %q，want %q", got, tc.wantOID)
			}
		})
	}
}

// TestCommentListSendsTheCursorAsGiven 钉住「偏移量原样带回去」。
//
// 🔴 **偏移量是 B 站给的字符串，一个字都不能改**（实测 2026-10-27）：
//
//	`cursor.pagination_reply.next_offset` = "CAESEDE4MzYx…/2za8JMgQIu+YJ"
//	（带 base64 里的 `+` `/` `=`）
//
// ⚠️ **改一个字符 B 站不报错**——它回的是**第一页**（实测：拿一个乱写的
// offset 去问，它回 code 0 + 第一页）。所以「原样带回去」不是讲究，是**必须**。
func TestCommentListSendsTheCursorAsGiven(t *testing.T) {
	f, c := commentFake(t, commentFixture(t, "reply-main.json"))
	const offset = "CAESEDE4MzYxMTQ3MTIyOTA0NjAaCAoGgN/2za8JMgQIu+YJ"
	if _, err := c.CommentList(context.Background(), CommentTypeVideo, 170001, int(CommentSortLatest), offset, 20); err != nil {
		t.Fatal(err)
	}
	rec := lastCommentRequest(t, f)
	// ⚠️ **走的是 `pagination_str`，不是 `next`**：`cursor.next` 在「评论」那一档
	// （mode=1）上带上去 B 站回 `-400 min_score … must be <= max_score …`。
	if rec.Query.Has("next") {
		t.Fatalf("带了 next=%q，want 不带（翻页走 pagination_str）", rec.Query.Get("next"))
	}
	var got struct {
		Offset string `json:"offset"`
	}
	if err := json.Unmarshal([]byte(rec.Query.Get("pagination_str")), &got); err != nil {
		t.Fatalf("pagination_str 不是合法 JSON：%v（值 %q）", err, rec.Query.Get("pagination_str"))
	}
	if got.Offset != offset {
		t.Fatalf("offset = %q，want %q（原样带回去）", got.Offset, offset)
	}
}

// TestCommentListOmitsTheCursorOnTheFirstPage 钉住「第一页不带游标」。
//
// ⚠️ **带了空的 offset 是不对的**：实测 `pagination_str={"offset":""}` 回的是
// **第一页**（code 0 + 第一页那 19 条）——而空串的意思本来就是「从头」。
func TestCommentListOmitsTheCursorOnTheFirstPage(t *testing.T) {
	f, c := commentFake(t, commentFixture(t, "reply-main.json"))
	if _, err := c.CommentList(context.Background(), CommentTypeVideo, 170001, int(CommentSortLatest), "", 20); err != nil {
		t.Fatal(err)
	}
	rec := lastCommentRequest(t, f)
	if rec.Query.Has("pagination_str") {
		t.Fatalf("第一页带了 pagination_str=%q，want 不带", rec.Query.Get("pagination_str"))
	}
}

// TestCommentListFallsBackToAKnownSort 钉住「认不出的排序不照发」。
//
// 实测 `mode=99` 直接回 `-400 invalid mode`。所以认不出的值要落到一个
// **认得出来的**档上，而不是把 0 原样发出去。
func TestCommentListFallsBackToAKnownSort(t *testing.T) {
	f, c := commentFake(t, commentFixture(t, "reply-main.json"))
	if _, err := c.CommentList(context.Background(), CommentTypeVideo, 170001, 99, "", 20); err != nil {
		t.Fatal(err)
	}
	rec := lastCommentRequest(t, f)
	got := rec.Query.Get("mode")
	switch got {
	case "1", "2", "3":
	default:
		t.Fatalf("mode = %q，want 1/2/3 里的一个", got)
	}
}

// TestCommentListRejectsAnEmptyObject 钉住「oid 不对」不发给 B 站。
func TestCommentListRejectsAnEmptyObject(t *testing.T) {
	f, c := commentFake(t, commentFixture(t, "reply-main.json"))
	if _, err := c.CommentList(context.Background(), CommentTypeVideo, 0, int(CommentSortHot), "", 20); !errors.Is(err, ErrCommentTargetMissing) {
		t.Fatalf("err = %v，want ErrCommentTargetMissing", err)
	}
	if len(f.requests()) != 0 {
		t.Error("oid 不对也发了一次请求")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 表情
// ─────────────────────────────────────────────────────────────────────────────

// TestCommentTextSwapsTheEmotePlaceholders 验占位符换成了能读的标记。
//
// ⚠️ 正文里的 `[梦限大_律敬礼]` 是**占位符**，直接画出来用户看到的是一串
// 方括号里的怪东西。
func TestCommentTextSwapsTheEmotePlaceholders(t *testing.T) {
	c := Comment{
		Message: "谢谢 [梦限大_律敬礼] 和 [doge]",
		Emotes: map[string]CommentEmote{
			"[梦限大_律敬礼]": {ID: 130694, URL: "https://i0.hdslb.com/x.png", JumpTitle: "律敬礼"},
			"[doge]":    {ID: 1, URL: "https://i0.hdslb.com/y.png"},
		},
	}
	got := c.CommentText()
	if strings.Contains(got, "[梦限大_律敬礼]") {
		t.Errorf("占位符没换掉：%q", got)
	}
	if !strings.Contains(got, "[律敬礼]") {
		t.Errorf("换成了别的：%q", got)
	}
	// 没有 JumpTitle 的退回占位符去掉方括号。
	if !strings.Contains(got, "[doge]") {
		t.Errorf("没有名字的表情没退回占位符：%q", got)
	}
}

// TestCommentTextLeavesAPlainMessageAlone 没有表情时原样回。
func TestCommentTextLeavesAPlainMessageAlone(t *testing.T) {
	c := Comment{Message: "普通的一句评论"}
	if got := c.CommentText(); got != "普通的一句评论" {
		t.Errorf("CommentText = %q，want 原样", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// B 站改字段类型的那一类事故
// ─────────────────────────────────────────────────────────────────────────────

// TestAPictureWithAFractionalSizeDoesNotTakeTheWholePageDown 钉住「小数大小」。
//
// 🔴 **这一条是 fixture 抓出来的**（2026-10-26 的真返回里 `img_size` 是
// `283.87`，而别的页子里它是整数）。按 `int` 解的话，`encoding/json` 撞上
// 类型对不上会让**整个反序列化失败**——于是「一条评论带一张图」变成
// 「**整页 30 条评论一条都读不出来**」，而界面上只是一个空列表（没有报错、
// 没有提示、什么都没有）。
//
// 这跟 `flexjson.go` 文件头记的是**同一类事故**（票 #300：`pub_ts` 从数字
// 变成字符串，于是 UP 空间那一页永远转圈）。
func TestAPictureWithAFractionalSizeDoesNotTakeTheWholePageDown(t *testing.T) {
	// 一条**只**带一张图、图的大小是小数的评论。
	body := `{"code":0,"message":"OK","ttl":1,"data":{
		"cursor":{"next":2,"is_end":false,"all_count":1,"mode":3,"name":"热门评论"},
		"upper":{"mid":122541},
		"replies":[{
			"rpid":1,"mid":266952687,"root":0,"parent":0,"count":0,"rcount":0,
			"like":1,"ctime":1549166850,
			"member":{"mid":"266952687","uname":"君之自由",
				"avatar":"https://i0.hdslb.com/bfs/face/x.jpg",
				"level_info":{"current_level":6},"official_verify":{"type":-1,"desc":""}},
			"content":{"message":"带图的一条","members":[],"jump_url":{},"max_line":6,
				"pictures":[{"img_src":"http://i0.hdslb.com/bfs/new_dyn/x.png",
					"img_width":533,"img_height":383,"img_size":283.87}]},
			"reply_control":{"max_line":6,"time_desc":"2822天前发布"}
		}]}}`

	_, c := commentFake(t, body)
	page, err := c.CommentList(context.Background(), CommentTypeVideo, 170001, int(CommentSortHot), "", 20)
	if err != nil {
		t.Fatalf("一条带小数大小图片的评论把整页带崩了：%v", err)
	}
	if len(page.Replies) != 1 {
		t.Fatalf("读出了 %d 条，want 1", len(page.Replies))
	}
	pics := page.Replies[0].Pictures
	if len(pics) != 1 {
		t.Fatalf("读出了 %d 张图，want 1", len(pics))
	}
	if pics[0].Width != 533 || pics[0].Height != 383 {
		t.Errorf("尺寸 = %d×%d，want 533×383", pics[0].Width, pics[0].Height)
	}
	// 283.87 KB → 283（截掉小数那一截）。
	if pics[0].Size != 283 {
		t.Errorf("大小 = %d，want 283", pics[0].Size)
	}
}

// TestAnIntegerPictureSizeStillWorks 钉住「整数那一边没被改坏」。
//
// 上一条钉的是小数那一半；这一条钉整数那一半——**两个方向都要过**。
func TestAnIntegerPictureSizeStillWorks(t *testing.T) {
	body := `{"code":0,"message":"OK","ttl":1,"data":{
		"cursor":{"next":2,"is_end":false,"all_count":1,"mode":3,"name":"热门评论"},
		"upper":{"mid":122541},
		"replies":[{
			"rpid":1,"mid":266952687,"like":1,"ctime":1,
			"member":{"uname":"某人","avatar":"https://x/a.jpg"},
			"content":{"message":"带图的一条","pictures":[
				{"img_src":"http://x/a.png","img_width":10,"img_height":20,"img_size":57}]},
			"reply_control":{}
		}]}}`

	_, c := commentFake(t, body)
	page, err := c.CommentList(context.Background(), CommentTypeVideo, 170001, int(CommentSortHot), "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Replies) != 1 || len(page.Replies[0].Pictures) != 1 {
		t.Fatalf("没读出来：%+v", page.Replies)
	}
	if got := page.Replies[0].Pictures[0].Size; got != 57 {
		t.Errorf("大小 = %d，want 57", got)
	}
}

// TestAnEmptyPageMidwayIsNotNoComments 钉住「翻到一半的空页不是「没人评论」。
//
// ⚠️ **空页有两种**：B 站说「到底了」（`cursor.is_end=true`）的空页是
// 「这个对象一条评论都没有」；翻页途中遇到、而 `is_end` 还是 false 的空页是
// **翻页没翻完**——还要继续拉。
//
// 混起来的话，界面会在翻到一半时忽然说「还没有人评论」。
func TestAnEmptyPageMidwayIsNotNoComments(t *testing.T) {
	body := `{"code":0,"message":"OK","ttl":1,"data":{
		"cursor":{"next":3,"is_end":false,"all_count":100,"mode":3,"name":"热门评论"},
		"upper":{"mid":122541},
		"replies":[]}}`

	_, c := commentFake(t, body)
	page, err := c.CommentList(context.Background(), CommentTypeVideo, 170001, int(CommentSortHot), "OFFSET-2", 20)
	if err != nil {
		t.Fatalf("翻页途中的空页被当成了「还没有人评论」：%v", err)
	}
	if len(page.Replies) != 0 {
		t.Errorf("读出了 %d 条", len(page.Replies))
	}
	if page.IsEnd {
		t.Error("is_end 应该是 false")
	}
	// 但**这一页照样返回**（翻页还要靠游标继续）。
	//
	// ⚠️ **判「能不能继续翻」看的是 `NextOffset`**（`cursor.pagination_reply
	// .next_offset`），不是 `cursor.next`——后者在「评论」档上带上去必挂。
	if page.NextOffset != "" {
		t.Errorf("下一页偏移量 = %q，want 空串（这一档没给）", page.NextOffset)
	}
	// `cursor.next` 那一栏仍然读得出来（B 站确实给），但**翻页不用它**。
	if page.Next != 3 {
		t.Errorf("cursor.next = %d，want 3（读得出来，但翻页不走它）", page.Next)
	}
}

// TestASubLevelWithNobodyReplying 钉住「楼中楼里没人回」。
func TestASubLevelWithNobodyReplying(t *testing.T) {
	body := `{"code":0,"message":"OK","ttl":1,"data":{
		"page":{"num":1,"size":20,"count":0},
		"upper":{"mid":122541},
		"replies":[]}}`

	f := newFakeBili(t)
	f.on(pathReplyReply, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, body)
	})
	c := f.client(t, Options{})
	replies, count, err := c.CommentReplies(context.Background(), CommentTypeVideo, 170001, 1353715029, 1, 20)
	if !errors.Is(err, ErrCommentNoComments) {
		t.Fatalf("err = %v，want ErrCommentNoComments", err)
	}
	if len(replies) != 0 {
		t.Errorf("读出了 %d 条", len(replies))
	}
	if count != 0 {
		t.Errorf("总回复数 = %d，want 0", count)
	}
}

// TestAnEmptySubPageMidwayIsNotNoComments 楼中楼翻到一半的空页同理。
func TestAnEmptySubPageMidwayIsNotNoComments(t *testing.T) {
	body := `{"code":0,"message":"OK","ttl":1,"data":{
		"page":{"num":4,"size":20,"count":52},
		"upper":{"mid":122541},
		"replies":[]}}`

	f := newFakeBili(t)
	f.on(pathReplyReply, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, body)
	})
	c := f.client(t, Options{})
	_, count, err := c.CommentReplies(context.Background(), CommentTypeVideo, 170001, 1353715029, 4, 20)
	if err != nil {
		t.Fatalf("翻页途中的空页被当成了「还没有人评论」：%v", err)
	}
	if count != 52 {
		t.Errorf("总回复数 = %d，want 52", count)
	}
}
