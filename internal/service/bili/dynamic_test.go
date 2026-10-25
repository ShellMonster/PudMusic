// 动态流那几个接口的测试。
//
// 重点是那一条包住整个动态流的约定：**不认识的动态丢掉，不报错**。
// 断言的是「一页 8 条里有几条留下了、丢了几条、有没有把整页带崩」，
// 不是「调了哪个函数」。
package bili

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// dynItem 造一条原始动态（B 站返回的那个形状）。
//
// 默认是一条纯文字动态；调用方改字段来造别的那种。
func dynItem(id string, mutate ...func(m map[string]any)) map[string]any {
	m := map[string]any{
		"id_str": id,
		"type":   "DYNAMIC_TYPE_WORD",
		"modules": map[string]any{
			"module_author": map[string]any{
				"mid": 100, "name": "作者" + id, "face": "https://f/" + id + ".jpg",
				"pub_time": "3 分钟前",
			},
			"module_dynamic": map[string]any{
				"desc": map[string]any{"text": "正文 " + id},
			},
		},
	}
	for _, f := range mutate {
		f(m)
	}
	return m
}

// majorOf 拿到（并造出）major 那一段。
func majorOf(m map[string]any) map[string]any {
	mods := m["modules"].(map[string]any)
	dyn := mods["module_dynamic"].(map[string]any)
	major, ok := dyn["major"].(map[string]any)
	if !ok {
		major = map[string]any{}
		dyn["major"] = major
	}
	return major
}

// setArchive 把一条动态变成投稿视频。
func setArchive(bvid string) func(map[string]any) {
	return func(m map[string]any) {
		major := majorOf(m)
		major["type"] = "MAJOR_TYPE_ARCHIVE"
		major["archive"] = map[string]any{
			"bvid": bvid, "title": "投稿 " + bvid, "cover": "https://f/c.jpg",
			"desc": "投稿简介", "duration_text": "3:20",
			"stat": map[string]any{"play": "1.2万"},
		}
		m["type"] = "DYNAMIC_TYPE_AV"
	}
}

// setOpus 把一条动态变成图文（新字段名）。
func setOpus(typ, text string, pics ...string) func(map[string]any) {
	return func(m map[string]any) {
		major := majorOf(m)
		major["type"] = typ
		items := make([]any, 0, len(pics))
		for _, p := range pics {
			items = append(items, map[string]any{"url_src": p, "width": 100, "height": 100})
		}
		major["opus"] = map[string]any{
			"summary": map[string]any{"text": text},
			"pics":    items,
		}
		m["type"] = "DYNAMIC_TYPE_DRAW"
	}
}

// setEmptyBody 把一条动态的正文清空（造「什么都没有」那种）。
func setEmptyBody() func(map[string]any) {
	return func(m map[string]any) {
		mods := m["modules"].(map[string]any)
		mods["module_dynamic"].(map[string]any)["desc"] = map[string]any{"text": ""}
	}
}

// setType 改一条动态的顶层 `type`。
func setType(typ string) func(map[string]any) {
	return func(m map[string]any) { m["type"] = typ }
}

// setText 改一条动态的 `desc.text`。
func setText(txt string) func(map[string]any) {
	return func(m map[string]any) {
		m["modules"].(map[string]any)["module_dynamic"].(map[string]any)["desc"] =
			map[string]any{"text": txt}
	}
}

// setMajor 往 major 里塞一样东西（用来让「未知类型」也有 major）。
func setMajor(f func(m map[string]any)) func(map[string]any) {
	return func(m map[string]any) { f(m) }
}

// dynamicPageOf 让一条动态走一遍**真的解析路径**（`Client.DynamicFeed` →
// `dynamicPageFrom` → `parseDynamic`），而不直接调 `parseDynamic`。
//
// 走整条路而不是直接调解析器，是因为评审抓到的那两个 bug（`major.draw`
// 没解析、类型闸门）都是在**模型层**发现的，测试也应该钉在这一层。
func dynamicPageOf(t *testing.T, item map[string]any) (DynamicPage, error) {
	t.Helper()
	_, cli := dynamicFixture(t, []any{item}, "", false)
	return cli.DynamicFeed(context.Background(), "")
}

// dynamicFixture 造一台会回给定 items 的假 B 站。
func dynamicFixture(t *testing.T, items []any, offset string, hasMore bool) (*fakeBili, *Client) {
	t.Helper()
	f := spaceClient(t) // 顺手把 WBI 的 key 备上（feed/space 要签名）
	f.on(dynamicFeedAllPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"items": items, "offset": offset, "has_more": hasMore,
		}))
	})
	f.on(dynamicFeedSpacePath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"items": items, "offset": offset, "has_more": hasMore,
		}))
	})
	return f, f.client(t, Options{})
}

// ─────────────────────────────────────────────────────────────────────────────
// 三种类型
// ─────────────────────────────────────────────────────────────────────────────

// TestTheThreeTypesAreKeptAndTheRestAreDropped 是这一层存在的理由。
//
// B 站的动态**远不止三种**（上游的 DynamicType 枚举列了二十多种）。这一版只画
// 纯文字、图文、投稿三种；其余的必须**静默丢掉**。
//
// 判据是「这一页还剩几条、丢了几条、**有没有报错**」。那一页里塞了转发、
// 剧集、直播、广告、课程五种画不出来的——它们一个都不能把整页带崩，
// 而三种画得出来的必须一条不少地留下。
func TestTheThreeTypesAreKeptAndTheRestAreDropped(t *testing.T) {
	items := []any{
		dynItem("d1"), // 纯文字
		dynItem("d2", setArchive("BV1aa")),
		dynItem("d3", setOpus("MAJOR_TYPE_OPUS", "图文正文", "https://f/1.jpg", "https://f/2.jpg")),
		// ↓↓↓ 下面五种是「B 站有、这一版画不出来」的
		dynItem("x1", func(m map[string]any) { // 转发：orig 里才是正文
			m["type"] = "DYNAMIC_TYPE_FORWARD"
			majorOf(m)["none"] = map[string]any{}
			setEmptyBody()(m)
		}),
		dynItem("x2", func(m map[string]any) { // 剧集
			m["type"] = "DYNAMIC_TYPE_PGC"
			majorOf(m)["pgc"] = map[string]any{"title": "番剧"}
			setEmptyBody()(m)
		}),
		dynItem("x3", func(m map[string]any) { // 直播
			m["type"] = "DYNAMIC_TYPE_LIVE"
			majorOf(m)["live"] = map[string]any{"title": "开播了"}
			setEmptyBody()(m)
		}),
		dynItem("x4", func(m map[string]any) { // 广告
			m["type"] = "DYNAMIC_TYPE_AD"
			setEmptyBody()(m)
		}),
		dynItem("x5", func(m map[string]any) { // 课程
			m["type"] = "DYNAMIC_TYPE_COURSES"
			majorOf(m)["courses"] = map[string]any{"title": "课"}
			setEmptyBody()(m)
		}),
	}
	_, cli := dynamicFixture(t, items, "OFFSET-2", true)

	pg, err := cli.DynamicFeed(context.Background(), "")
	if err != nil {
		t.Fatalf("五种画不出来的动态把整页带崩了：%v", err)
	}
	if len(pg.Items) != 3 {
		t.Fatalf("留下了 %d 条，want 3（纯文字 / 图文 / 投稿）", len(pg.Items))
	}
	if len(pg.Drops) != 5 {
		t.Errorf("丢掉 %d 条，want 5", len(pg.Drops))
	}
	kinds := map[DynamicKind]string{}
	for _, d := range pg.Items {
		kinds[d.Kind] = d.ID
	}
	for kind, id := range map[DynamicKind]string{
		DynamicText: "d1", DynamicVideo: "d2", DynamicOpus: "d3",
	} {
		if kinds[kind] != id {
			t.Errorf("%s 认成了别的：%s → kind=%v", kind, id, kinds[kind])
		}
	}
	// 三种都留下来了，而 20 种形态里另外 17 种一条都没把页面带崩——这一条
	// 就是「静默丢弃而不是报错」的全部内容。
	if pg.Offset != "OFFSET-2" || !pg.HasMore {
		t.Errorf("翻页入口丢了：offset=%q hasMore=%v", pg.Offset, pg.HasMore)
	}
}

// 🔴 TestUnknownTypesWithTextAreStillDropped 是「静默丢弃」那条验收真正的
// 考题。
//
// 早一版的排除用例**把所有未知类型的正文都清空了**，于是「带正文的转发」和
// 「B 站新加的形态」这两种输入压根没被喂进去——而它们正是最容易漏的：
// `parseDynamic` 原来的第三支是「认不出 major 就当纯文字」，只要有
// `desc.text` 就画出来了。实测过：一条带 `desc.text` 的
// `DYNAMIC_TYPE_FORWARD` 和一条带 `desc.text` 的 `FUTURE` 都被当成了纯文字，
// 而一条转发被当成他自己发的话，比不画更糟。
//
// 判据是「剩下的**正好是那三种**」和「**一条文字都没漏出来**」。
func TestUnknownTypesWithTextAreStillDropped(t *testing.T) {
	const spoken = "转发正文：「他」说了这句话"
	items := []any{
		dynItem("word"),
		dynItem("av", setArchive("BV1aa")),
		dynItem("draw", setOpus("MAJOR_TYPE_OPUS", "图文正文", "https://f/1.jpg")),
		// ↓↓↓ 下面四条**都带着正文**，而且都不是那三种
		dynItem("fwd", setType("DYNAMIC_TYPE_FORWARD"), setText(spoken)),
		dynItem("future", setType("DYNAMIC_TYPE_FUTURE"), setText(spoken)),
		dynItem("pgc", setType("DYNAMIC_TYPE_PGC"), setText(spoken),
			setMajor(func(m map[string]any) { majorOf(m)["pgc"] = map[string]any{"title": "番剧"} })),
		dynItem("live", setType("DYNAMIC_TYPE_LIVE"), setText(spoken),
			setMajor(func(m map[string]any) { majorOf(m)["live"] = map[string]any{"title": "开播"} })),
	}
	_, cli := dynamicFixture(t, items, "", false)

	pg, err := cli.DynamicFeed(context.Background(), "")
	if err != nil {
		t.Fatalf("整页崩了：%v", err)
	}
	if len(pg.Items) != 3 {
		t.Fatalf("留下了 %d 条，want 3（另外四条带正文的不该被当成纯文字）", len(pg.Items))
	}
	for _, d := range pg.Items {
		if d.Text == spoken {
			t.Errorf("%q 是一条未知类型，却把转发正文当成了它自己的话", d.ID)
		}
		switch d.Kind {
		case DynamicText, DynamicVideo, DynamicOpus:
		default:
			t.Errorf("%q 的类型是 %v，不在那三种里", d.ID, d.Kind)
		}
	}
	if len(pg.Drops) != 4 {
		t.Errorf("丢了 %d 条，want 4", len(pg.Drops))
	}
}

// TestAnItemWeCannotReadAtAllIsDroppedToo 钉住「连 JSON 都读不懂的那一条」
// 也是丢掉，而不是整页报错。
//
// 真实触发条件是 B 站给某一条塞了一个我们没声明的形状；`items` 是
// `[]json.RawMessage`，所以「读不懂」只在**类型不对**（比如那个元素是一个
// 字符串而不是一个对象）时发生。这一条模拟的正是那个。
func TestAnItemWeCannotReadAtAllIsDroppedToo(t *testing.T) {
	items := []any{
		dynItem("good1"),
		"这一条根本不是一个对象",
		dynItem("good2"),
	}
	_, cli := dynamicFixture(t, items, "", false)

	pg, err := cli.DynamicFeed(context.Background(), "")
	if err != nil {
		t.Fatalf("一条坏掉就把整页带崩了：%v", err)
	}
	if len(pg.Items) != 2 || len(pg.Drops) != 1 {
		t.Errorf("拿到 %d 条丢了 %d 条，want 2 / 1", len(pg.Items), len(pg.Drops))
	}
}

// TestAnItemWithoutAnIDIsDropped 钉住「没有 id 的行没有身份」。
//
// 铁律四要的是一个**稳定且唯一**的行身份；没有 id 的动态拿不到，
// 而一个拿不到身份的行使整个列表的状态对不上（Tester 里当场 panic）。
func TestAnItemWithoutAnIDIsDropped(t *testing.T) {
	items := []any{
		map[string]any{"type": "DYNAMIC_TYPE_WORD", "modules": map[string]any{}},
		dynItem("good"),
	}
	_, cli := dynamicFixture(t, items, "", false)

	pg, err := cli.DynamicFeed(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Items) != 1 || pg.Items[0].ID != "good" {
		t.Errorf("拿到 %+v，want 只有 good 那一条", pg.Items)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 丢掉的理由分了类（票 #344）
// ─────────────────────────────────────────────────────────────────────────────

// TestTheDroppedOnesAreToldApartByReason 钉住「丢掉的理由分了类」。
//
// ⚠️ 分类不是为了好看：界面上只有一句话概括「丢了几条」的话，「B 站返回了
// 一种我们不认识的形态」和「这一条我们读不懂（B 站改了返回的形状）」会被
// 说成**同一件事**——而后者那句话对用户是**错的**（票 #344）：B 站没有换
// 形态，是**我们读不出来**。
//
// 判据是**按类别的计数**，不是一个总数。
func TestTheDroppedOnesAreToldApartByReason(t *testing.T) {
	items := []any{
		// 形态不认识：转发。
		dynItem("x1", setType("DYNAMIC_TYPE_FORWARD")),
		// 读不懂：`major.opus` 给的不是对象（`encoding/json` 整个失败）。
		map[string]any{
			"id_str": "x2", "type": "DYNAMIC_TYPE_DRAW",
			"modules": map[string]any{
				"module_dynamic": map[string]any{
					"major": map[string]any{"opus": "不是一个对象"},
				},
			},
		},
		// 没有身份：`id_str` 是空的。
		map[string]any{"type": "DYNAMIC_TYPE_WORD", "modules": map[string]any{}},
		// 什么也没有：声明是纯文字，正文却是空的。
		dynItem("x4", setEmptyBody()),
	}
	_, cli := dynamicFixture(t, items, "", false)

	pg, err := cli.DynamicFeed(context.Background(), "")
	if err != nil {
		t.Fatalf("整页崩了：%v", err)
	}
	if len(pg.Drops) != 4 {
		t.Fatalf("丢掉 %d 条，want 4", len(pg.Drops))
	}

	var c DropCounts
	for _, d := range pg.Drops {
		c.Add(d.Reason)
	}
	for _, c2 := range []struct {
		what string
		got  int
	}{
		{"形态不认识", c.ShapeUnknown},
		{"读不懂", c.Unreadable},
		{"没有身份", c.NoIdentity},
		{"什么也没有", c.Empty},
	} {
		if c2.got != 1 {
			t.Errorf("「%s」数了 %d 条，want 1", c2.what, c2.got)
		}
	}
	if c.Total() != 4 {
		t.Errorf("加起来是 %d 条，want 4", c.Total())
	}
}

// 🔴 TestAnArchiveWithoutABvidIsItsOwnReason 钉住「投稿动态没有稿件号」是
// **另一类**，不是「什么也没有」。
//
// ⚠️ **这一条带着正文**：UP 写的那段话就在 `desc.text` 里（B 站的投稿动态
// 本来就带一段话），只是 `major.archive.bvid` 是空的——放不出来。而「什么也
// 没有」那一档说的是「既没有正文也没有图」。
//
// ⚠️ **混进去的话，界面上那句话对用户是错的**：「有 1 条什么也没有，画不出来」
// 说的是「他什么都没发」，而真相是「他发了一条投稿，我们放不出来」。
func TestAnArchiveWithoutABvidIsItsOwnReason(t *testing.T) {
	const spoken = "UP 写的那段话"
	// 一条投稿动态：**有正文**，而稿件号是空的。
	noBvid := dynItem("av1", setType("DYNAMIC_TYPE_AV"), setText(spoken))
	majorOf(noBvid)["type"] = "MAJOR_TYPE_ARCHIVE"
	majorOf(noBvid)["archive"] = map[string]any{"title": "一个标题"}
	// 另一条**真的什么也没有**：纯文字，正文空的。
	empty := dynItem("w1", setEmptyBody())
	_, cli := dynamicFixture(t, []any{noBvid, empty}, "", false)

	pg, err := cli.DynamicFeed(context.Background(), "")
	if err != nil {
		t.Fatalf("整页崩了：%v", err)
	}
	if len(pg.Drops) != 2 {
		t.Fatalf("丢掉 %d 条，want 2", len(pg.Drops))
	}

	var c DropCounts
	for _, d := range pg.Drops {
		c.Add(d.Reason)
	}
	if c.NoArchive != 1 {
		t.Errorf("「投稿没有稿件号」数了 %d 条，want 1", c.NoArchive)
	}
	if c.Empty != 1 {
		t.Errorf("「什么也没有」数了 %d 条，want 1（投稿没有稿件号不该混进这一档）", c.Empty)
	}
	if c.Total() != 2 {
		t.Errorf("加起来是 %d 条，want 2", c.Total())
	}
	// ⚠️ **正文是有还是没有，要看得见**：那一档说的是「什么也没有」，而这条
	// 明明带着正文——所以它的理由里得留着那句「投稿动态没有稿件号」。
	found := false
	for _, d := range pg.Drops {
		if d.Reason == DropNoArchive && strings.Contains(d.Why, "稿件号") {
			found = true
		}
	}
	if !found {
		t.Errorf("没有一条的理由写着「投稿动态没有稿件号」：%+v", pg.Drops)
	}
}

// 🔴 TestAnArchiveWithoutABvidAndWithoutTextIsEmpty 钉住「投稿动态**既**没有
// 稿件号**又**没有正文」算「什么也没有」，**不算**「投稿没有稿件号」。
//
// ⚠️ **这一条是评审查出来的那处**（票 #344 的评审）：新加的那一类说「正文有，
// 稿件号没有」，而代码**从头到尾没看正文**——一条正文也是空的投稿型动态照样
// 被记成「投稿没有稿件号」。而界面上那句话是「有 N 条**是投稿却放不出来**」，
// 对一条**什么也没有**的动态来说，那句话是错的。
func TestAnArchiveWithoutABvidAndWithoutTextIsEmpty(t *testing.T) {
	// 稿件号是空的，**正文也是空的**。
	noBvidNoText := dynItem("av2", setType("DYNAMIC_TYPE_AV"), setEmptyBody())
	majorOf(noBvidNoText)["type"] = "MAJOR_TYPE_ARCHIVE"
	majorOf(noBvidNoText)["archive"] = map[string]any{"title": "一个标题"}
	_, cli := dynamicFixture(t, []any{noBvidNoText}, "", false)

	pg, err := cli.DynamicFeed(context.Background(), "")
	if err != nil {
		t.Fatalf("整页崩了：%v", err)
	}
	if len(pg.Drops) != 1 {
		t.Fatalf("丢掉 %d 条，want 1", len(pg.Drops))
	}
	if got := pg.Drops[0].Reason; got != DropEmpty {
		t.Errorf("理由是 %v，want 「什么也没有」——它既没有稿件号也没有正文", got)
	}
	var c DropCounts
	for _, d := range pg.Drops {
		c.Add(d.Reason)
	}
	if c.Empty != 1 {
		t.Errorf("「什么也没有」数了 %d 条，want 1", c.Empty)
	}
	if c.NoArchive != 0 {
		t.Errorf("「投稿没有稿件号」数了 %d 条，want 0（这一条什么也没有）", c.NoArchive)
	}
}

// TestAnUnknownDropReasonIsStillCounted 钉住「加了一类而忘了加一档」不会
// **静默少一条**。
//
// ⚠️ **这是 `DropCounts.Add` 那个 `default` 那一档的用处**：将来加了第五类
// 丢弃理由而 `Add` 忘了跟上，原来那个没有 `default` 的 switch 会把它**静默
// 吞掉**——总数少了、界面上那句话少了，而**没有任何报错**。
//
// 判据是「总数不变」：还不认识的那一类落进 `Other`，而 `Total()` 把它算进去。
func TestAnUnknownDropReasonIsStillCounted(t *testing.T) {
	var c DropCounts
	c.Add(DropReason(200))
	if c.Other != 1 {
		t.Errorf("还不认识的那一类数了 %d 条，want 1（它不许被静默吞掉）", c.Other)
	}
	if c.Total() != 1 {
		t.Errorf("加起来是 %d 条，want 1", c.Total())
	}
	// 已知的那几档不受影响。
	c.Add(DropShapeUnknown)
	if c.Other != 1 || c.ShapeUnknown != 1 || c.Total() != 2 {
		t.Errorf("加了一条已知理由之后数错了：%+v，总数 %d", c, c.Total())
	}
}

// TestTheSameDroppedDynamicInTwoPagesIsTheSameOne 钉住「跨页去重靠的是身份」。
//
// ⚠️ B 站翻页**会重叠**：同一批画不出来的动态在两页里各出现一次。界面上那个
// 数要按**不同的动态**算（不是按出现次数算），所以同一条在两页里必须是
// **同一个身份**——这是上层去重的唯一依据（票 #344）。
//
// 判据是**两页给出的 `Key` 一样**。
func TestTheSameDroppedDynamicInTwoPagesIsTheSameOne(t *testing.T) {
	f := spaceClient(t) // 顺手把 WBI 的 key 备上（feed/space 要签名）
	f.on(dynamicFeedSpacePath, func(w http.ResponseWriter, r *http.Request) {
		// **两页给的是同一条**（B 站翻页重叠的形状）。
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"items":    []any{dynItem("f1", setType("DYNAMIC_TYPE_FORWARD"))},
			"offset":   "O2",
			"has_more": true,
		}))
	})
	cli := f.client(t, Options{})

	pg1, err := cli.SpaceDynamics(context.Background(), 42, "")
	if err != nil {
		t.Fatal(err)
	}
	pg2, err := cli.SpaceDynamics(context.Background(), 42, "O1")
	if err != nil {
		t.Fatal(err)
	}
	if len(pg1.Drops) != 1 || len(pg2.Drops) != 1 {
		t.Fatalf("两页各丢了 %d / %d 条，want 1 / 1", len(pg1.Drops), len(pg2.Drops))
	}
	if pg1.Drops[0].Key != pg2.Drops[0].Key {
		t.Errorf("同一条在两页里身份不同：%q vs %q（跨页去重靠它，"+
			"不一样的话界面上那个数会翻倍）", pg1.Drops[0].Key, pg2.Drops[0].Key)
	}
}

// TestADroppedOneWeCannotReadAtAllStillHasAStableIdentity 钉住「连 JSON 都
// 读不懂的那一条，身份也是稳的」。
//
// 那一类拿不到 `id_str`（整条读不开），所以身份退到**原文的指纹**——
// 翻页重叠时那一条原文是一样的，指纹也就一样，于是它被数一次而不是两次
// （票 #344）。
func TestADroppedOneWeCannotReadAtAllStillHasAStableIdentity(t *testing.T) {
	// 一个根本不是对象的那一条（`items` 是 `[]json.RawMessage`，
	// 「读不懂」只在类型不对时发生）。
	broken := json.RawMessage(`"这一条根本不是一个对象"`)
	f := spaceClient(t)
	f.on(dynamicFeedSpacePath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"items":    []any{json.RawMessage(broken)},
			"offset":   "O2",
			"has_more": true,
		}))
	})
	cli := f.client(t, Options{})

	pg1, err := cli.SpaceDynamics(context.Background(), 42, "")
	if err != nil {
		t.Fatal(err)
	}
	pg2, err := cli.SpaceDynamics(context.Background(), 42, "O1")
	if err != nil {
		t.Fatal(err)
	}
	if len(pg1.Drops) != 1 || len(pg2.Drops) != 1 {
		t.Fatalf("两页各丢了 %d / %d 条，want 1 / 1", len(pg1.Drops), len(pg2.Drops))
	}
	if pg1.Drops[0].Reason != DropUnreadable {
		t.Errorf("理由是 %v，want 「读不懂」", pg1.Drops[0].Reason)
	}
	if pg1.Drops[0].Key != pg2.Drops[0].Key {
		t.Errorf("同一条读不懂的在两页里身份不同：%q vs %q",
			pg1.Drops[0].Key, pg2.Drops[0].Key)
	}
}

// TestGraphicalPostIsRecognisedByItsFieldsNotByItsTypeName 钉住「判类型按
// major 装了什么，不按 `major.type` 的字符串」。
//
// B 站给同一个图文类型写过两个名字（`MAJOR_TYPE_DRAW` → `MAJOR_TYPE_OPUS`）。
// 按字符串判的话，同一个东西在新旧两种返回里会分叉成两种类型——而新名字
// 是现在 B 站给的，老名字是历史抓包里才有的，按字符串判的代码只对一半
// 的返回说得对。
func TestGraphicalPostIsRecognisedByItsFieldsNotByItsTypeName(t *testing.T) {
	items := []any{
		dynItem("new", setOpus("MAJOR_TYPE_OPUS", "新的", "https://f/n.jpg")),
		dynItem("old", setOpus("MAJOR_TYPE_DRAW", "旧的", "https://f/o.jpg")),
	}
	_, cli := dynamicFixture(t, items, "", false)

	pg, err := cli.DynamicFeed(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Items) != 2 {
		t.Fatalf("拿到 %d 条，want 2", len(pg.Items))
	}
	for _, d := range pg.Items {
		if d.Kind != DynamicOpus {
			t.Errorf("%s 是 %v，want 图文（两种 type 名字该认成同一种）", d.ID, d.Kind)
		}
	}
}

// TestTheOldPictureFieldNameStillReads 钉住图的两个字段名都要认。
//
// B 站对同一批图给过 `src`（老）和 `url_src`（新）。只认一个的话，另一批
// 图文在界面上是「一张图都没有」的空卡片。
func TestTheOldPictureFieldNameStillReads(t *testing.T) {
	m := dynItem("legacy", func(m map[string]any) {
		major := majorOf(m)
		major["type"] = "MAJOR_TYPE_DRAW"
		major["opus"] = map[string]any{
			"summary": map[string]any{"text": "老字段名"},
			"pics": []any{
				map[string]any{"src": "https://f/old1.jpg"},
				map[string]any{"url_src": "https://f/new1.jpg"},
			},
		}
	})
	_, cli := dynamicFixture(t, []any{m}, "", false)

	pg, err := cli.DynamicFeed(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Items) != 1 {
		t.Fatalf("拿到 %d 条，want 1", len(pg.Items))
	}
	pics := pg.Items[0].Pics
	if len(pics) != 2 || pics[0] != "https://f/old1.jpg" || pics[1] != "https://f/new1.jpg" {
		t.Errorf("图认成了 %v（老字段名 src 没认出来？）", pics)
	}
}

// TestTheLegacyDrawFormatStillShowsItsImages 钉住**旧格式**的图文。
//
// ⚠️ B 站把这个类型从 `major.draw` 改名成了 `major.opus`，而**老动态的返回里
// 仍然是 `draw`**（`web-dynamic.ts:207-216` 的 `major.draw.items[].src`）。
//
// 认不出来的症状不是「少一张图」，而是**整条消失**：旧格式的图文常常没有
// `desc.text`（正文就在图里），于是 `pics` 空 + `Text` 空 → `dropped` ——
// 明明有九张图，界面上一个格子都没有。
func TestTheLegacyDrawFormatStillShowsItsImages(t *testing.T) {
	t.Run("有正文", func(t *testing.T) {
		m := dynItem("draw-with-text", func(m map[string]any) {
			majorOf(m)["type"] = "MAJOR_TYPE_DRAW"
			majorOf(m)["draw"] = map[string]any{"id": 7, "items": []any{
				map[string]any{"src": "https://f/old1.jpg", "width": 100, "height": 100},
				map[string]any{"src": "https://f/old2.jpg", "width": 100, "height": 100},
			}}
		})
		pg, err := dynamicPageOf(t, m)
		if err != nil {
			t.Fatal(err)
		}
		if len(pg.Items) != 1 || pg.Items[0].Kind != DynamicOpus {
			t.Fatalf("旧格式的图文没被当成图文：%+v（丢了 %d 条）", pg.Items, len(pg.Drops))
		}
		if len(pg.Items[0].Pics) != 2 {
			t.Errorf("拿到 %d 张图，want 2（`major.draw.items[].src` 没解析）", len(pg.Items[0].Pics))
		}
	})

	t.Run("没正文", func(t *testing.T) {
		// 这一支才是真会「整条消失」的：没有 desc.text，只剩图。
		m := dynItem("draw-bare", setEmptyBody(), func(m map[string]any) {
			majorOf(m)["type"] = "MAJOR_TYPE_DRAW"
			majorOf(m)["draw"] = map[string]any{"id": 8, "items": []any{
				map[string]any{"src": "https://f/only.jpg", "width": 100, "height": 100},
			}}
		})
		pg, err := dynamicPageOf(t, m)
		if err != nil {
			t.Fatal(err)
		}
		if len(pg.Items) != 1 {
			t.Fatalf("旧格式的图文（只有图、没有正文）被整条丢掉了：丢了 %d 条", len(pg.Drops))
		}
		if len(pg.Items[0].Pics) != 1 || pg.Items[0].Pics[0] != "https://f/only.jpg" {
			t.Errorf("拿到 %v，want 那一张图", pg.Items[0].Pics)
		}
	})
}

// TestAGraphicalPostWithNeitherTextNorPicsIsDropped 钉住「声明是图文但既没
// 图也没字」那一种：画出来是一个空卡片，比不画更糟。
func TestAGraphicalPostWithNeitherTextNorPicsIsDropped(t *testing.T) {
	m := dynItem("blank", setEmptyBody(), setOpus("MAJOR_TYPE_OPUS", "", ""))
	_, cli := dynamicFixture(t, []any{m}, "", false)

	pg, err := cli.DynamicFeed(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Items) != 0 || len(pg.Drops) != 1 {
		t.Errorf("拿到 %d 条丢了 %d 条，want 0 / 1（空卡片比不画更糟）", len(pg.Items), len(pg.Drops))
	}
}

// TestTheTextOfAnOpusIsTakenOnceNotTwice 钉住「正文二选一，不是拼起来」。
//
// ⚠️ B 站的正常返回里 `desc.text` 和 `opus.summary.text` 是**同一段话**
// （上游两处都是 `desc?.text || opus?.summary?.text`）。拼起来的结果是
// 界面上同一句话出现两遍——而这个 bug 很难被 `HasText` 抓到：那条断言是
// **子串**匹配，两遍也照样为真。
func TestTheTextOfAnOpusIsTakenOnceNotTwice(t *testing.T) {
	const same = "今天更新了一期视频，记得来看。"
	m := dynItem("dup", func(m map[string]any) {
		major := majorOf(m)
		major["type"] = "MAJOR_TYPE_OPUS"
		major["opus"] = map[string]any{
			"summary": map[string]any{"text": same},
			"pics":    []any{map[string]any{"url_src": "https://f/1.jpg"}},
		}
	})
	// dynItem 已经把 desc 设成了「正文 dup」——改成和 summary 同一段，
	// 复现 B 站正常返回的形状。
	mods := m["modules"].(map[string]any)
	mods["module_dynamic"].(map[string]any)["desc"] = map[string]any{"text": same}
	_, cli := dynamicFixture(t, []any{m}, "", false)

	pg, err := cli.DynamicFeed(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Items) != 1 {
		t.Fatalf("拿到 %d 条，want 1", len(pg.Items))
	}
	if got := pg.Items[0].Text; got != same {
		t.Errorf("正文是 %q，want %q（两处是同一段话，只该取一次；拼起来会显示两遍）", got, same)
	}
}

// TestTheTextFallsBackToTheOpusSummaryWhenDescIsEmpty 是上一条的另一半：
// desc 空的时候要拿 summary，否则图文的正文就没了。
func TestTheTextFallsBackToTheOpusSummaryWhenDescIsEmpty(t *testing.T) {
	m := dynItem("fallback", setEmptyBody(), func(m map[string]any) {
		major := majorOf(m)
		major["type"] = "MAJOR_TYPE_OPUS"
		major["opus"] = map[string]any{
			"summary": map[string]any{"text": "只有 summary 里的话"},
			"pics":    []any{map[string]any{"url_src": "https://f/1.jpg"}},
		}
	})
	_, cli := dynamicFixture(t, []any{m}, "", false)

	pg, err := cli.DynamicFeed(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Items) != 1 || pg.Items[0].Text != "只有 summary 里的话" {
		t.Errorf("desc 空时该退回 summary，拿到 %+v", pg.Items)
	}
}

// TestAnArchiveDynamicIsPlayable 钉住投稿型动态带出来的**队列项**
// （bvid 那一格必须有——少了它点一下放不出来）。
func TestAnArchiveDynamicIsPlayable(t *testing.T) {
	// 正文留空（`setEmptyBody`），好把「正文空时用简介顶上」那一支也走一遍。
	item := dynItem("v", setEmptyBody(), setArchive("BV1play"))
	_, cli := dynamicFixture(t, []any{item}, "", false)
	pg, err := cli.DynamicFeed(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	v := pg.Items[0].Video
	if v == nil {
		t.Fatal("投稿型动态没有带出稿件")
	}
	if v.Bvid != "BV1play" || v.Title != "投稿 BV1play" || v.Cover == "" {
		t.Errorf("稿件读成 %+v", *v)
	}
	if v.Duration != "3:20" || v.Play != "1.2万" {
		t.Errorf("时长/播放数读成 %q / %q", v.Duration, v.Play)
	}
	// 正文空的时候用简介顶上——界面上那一句不能是空的。
	if pg.Items[0].Text != "投稿简介" {
		t.Errorf("正文是 %q，want 用简介顶上（正文空的投稿卡上是空的）", pg.Items[0].Text)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 两个入口的差别
// ─────────────────────────────────────────────────────────────────────────────

// TestSpaceDynamicsIsSignedAndAllIsNot 钉住那对最容易抄错的差别：
// **feed/space 要签名，feed/all 不要**。
//
// 两个接口一路之差，参数也几乎一样。照抄错了的症状是 -403
// 「访问权限不足」——那句话和「签名算错了」没有任何关系。
func TestSpaceDynamicsIsSignedAndAllIsNot(t *testing.T) {
	f, cli := dynamicFixture(t, []any{dynItem("d")}, "", false)
	ctx := context.Background()

	if _, err := cli.SpaceDynamics(ctx, 1, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := cli.DynamicFeed(ctx, ""); err != nil {
		t.Fatal(err)
	}

	signed := map[string]bool{}
	for _, r := range f.requests() {
		if r.Path == dynamicFeedSpacePath || r.Path == dynamicFeedAllPath {
			signed[r.Path] = r.Query.Get("w_rid") != ""
		}
	}
	if !signed[dynamicFeedSpacePath] {
		t.Errorf("%s 应该有 WBI 签名", dynamicFeedSpacePath)
	}
	if signed[dynamicFeedAllPath] {
		t.Errorf("%s 不该签名（上游没传 useWbi）", dynamicFeedAllPath)
	}
}

// TestRawCountsWhatTheServerSentNotWhatWeKept 钉住 `DynamicPage.Raw`。
//
// 上层要靠它分「这一页真的没有」和「这一页有、但全是画不出来的」：只看
// `len(Items)` 的话，一页全是转发的动态会被当成「到底了」，后面那一页
// 有效动态**永远不再加载**（实测过的卡死）。
func TestRawCountsWhatTheServerSentNotWhatWeKept(t *testing.T) {
	items := []any{
		dynItem("f1", setType("DYNAMIC_TYPE_FORWARD"), setText("转发")),
		dynItem("f2", setType("DYNAMIC_TYPE_LIVE"), setText("直播")),
	}
	_, cli := dynamicFixture(t, items, "NEXT", true)

	pg, err := cli.DynamicFeed(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if pg.Raw != 2 {
		t.Errorf("Raw 是 %d，want 2（B 站发了 2 条）", pg.Raw)
	}
	if len(pg.Items) != 0 {
		t.Errorf("留下了 %d 条，want 0", len(pg.Items))
	}
	if !pg.HasMore {
		t.Error("B 站说有下一页，那就有——上层靠 Raw 区分「没有」和「都画不出来」")
	}
}

// TestTheDynamicOffsetIsPassedThroughVerbatim 钉住「offset 原样带回」。
//
// 它是 B 站给的一个不透明串，**不能解析也不能数**——按页码数会拿到重复
// 的那一页，而界面上看起来是「一直加载同一页」。
func TestTheDynamicOffsetIsPassedThroughVerbatim(t *testing.T) {
	const weird = "778899112233445566778899"
	f, cli := dynamicFixture(t, []any{dynItem("d")}, weird, false)
	if _, err := cli.DynamicFeed(context.Background(), weird); err != nil {
		t.Fatal(err)
	}

	asked := false
	for _, r := range f.requests() {
		if r.Path != dynamicFeedAllPath {
			continue
		}
		asked = true
		if got := r.Query.Get("offset"); got != weird {
			t.Errorf("offset 传成了 %q，want 原样带回 %q", got, weird)
		}
	}
	if !asked {
		t.Fatalf("没请求 %s", dynamicFeedAllPath)
	}
}

// TestAnEmptyFirstPageStopsTheList 是 `PagedList` / `upPager` 那一层的
// 「空页一律收口」的上游一半：空页自己不该说「还有」。
func TestAnEmptyFirstPageStopsTheList(t *testing.T) {
	_, cli := dynamicFixture(t, []any{}, "", true)
	pg, err := cli.DynamicFeed(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Items) != 0 {
		t.Fatalf("空页里有 %d 条", len(pg.Items))
	}
	// ⚠️ **B 站说「还有」也不作数**：这一页既没有东西，也没有**下一次要问的
	// 入口**（offset 是空串），于是「再问一页」问回来的是同一页——那个圈
	// 会一直转下去（票 #300 的实测：60 帧发了 40 次请求）。
	if pg.HasMore {
		t.Error("空页又没给下一次的口令，却说「还有下一页」——会一直问同一页")
	}
}

// TestTheOffsetThatDoesNotMoveEndsTheList 钉住「入口没前进就是到底了」。
//
// ⚠️ **B 站的 `has_more` 和 `offset` 不是一起可信的**：它有时候会回一个和
// 上一次**一模一样**的 offset（或者干脆空串）而 `has_more` 仍然是 true。
// 拿着那个 offset 再问一次，问回来的是**同一页**——于是「再问一页」永远
// 问不完（实测：一页全是转发动态时 60 帧发了 40 次请求，界面上一直转圈，
// 一条内容都出不来）。
//
// 判据是「下一次要问的入口和这一次的**是不是同一个**」。
func TestTheOffsetThatDoesNotMoveEndsTheList(t *testing.T) {
	for _, tc := range []struct {
		name   string
		used   string
		offset string
	}{
		{"同一个 offset", "O1", "O1"},
		{"空的 offset", "O1", ""},
		{"第一页就给空串", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, cli := dynamicFixture(t, []any{dynItem("d")}, tc.offset, true)
			pg, err := cli.DynamicFeed(context.Background(), tc.used)
			if err != nil {
				t.Fatal(err)
			}
			if pg.HasMore {
				t.Errorf("入口没有前进（问的是 %q，回来的是 %q），却还说「还有下一页」",
					tc.used, tc.offset)
			}
		})
	}
}

// TestTheOffsetThatMovesKeepsTheNextPage 钉住上一条的反面：入口**动了**就
// 还有下一页。
//
// 少了这一条，上一条那个「一律收口」就能把正常的翻页也掐死——而症状是
// 「翻不到第二页」，比「一直翻同一页」更难一眼看出来。
func TestTheOffsetThatMovesKeepsTheNextPage(t *testing.T) {
	_, cli := dynamicFixture(t, []any{dynItem("d")}, "O2", true)
	pg, err := cli.DynamicFeed(context.Background(), "O1")
	if err != nil {
		t.Fatal(err)
	}
	if !pg.HasMore {
		t.Error("入口前进了（O1 → O2），却把「还有下一页」收掉了——翻页被掐死了")
	}
}

// TestAFieldThatChangedItsTypeDoesNotTakeThePageDown 钉住「一个字段改了
// 类型不能把整页带走」。
//
// 🔴 **实测踩过的坑**（票 #300，2026-10-24）：B 站把 `module_author.pub_ts`
// 从 Unix 秒（数字）改成了**字符串**（`"1792720812"`），而 `encoding/json`
// 遇到类型对不上会让**整个反序列化失败**——于是那一页 12 条动态**全部**被
// 当成「读不懂」丢掉，UP 空间的「动态」那一格永远转圈、永远拿不到内容，
// 而界面上**一个字都不提**。
//
// 判据是「这些字段换成另一种写法，那一条**照样留下**」。
func TestAFieldThatChangedItsTypeDoesNotTakeThePageDown(t *testing.T) {
	for _, tc := range []struct {
		name   string
		wantID string
		mutate func(m map[string]any)
	}{
		{"pub_ts 是字符串", "d1", func(m map[string]any) {
			authorOf(m)["pub_ts"] = "1792720812"
		}},
		{"pub_ts 是数字", "d1", func(m map[string]any) {
			authorOf(m)["pub_ts"] = 1792720812
		}},
		// ⚠️ **id 变成数字时身份就是那个数字的字面量**——不能走一遍 float
		// （19 位的大整数过一趟 float 就掉精度了）。
		{"id_str 是数字", "1261627848475541545", func(m map[string]any) {
			m["id_str"] = 1261627848475541545
		}},
		{"mid 是字符串", "d1", func(m map[string]any) {
			authorOf(m)["mid"] = "42"
		}},
		{"赞的数是字符串", "d1", func(m map[string]any) {
			mods := m["modules"].(map[string]any)
			mods["module_stat"] = map[string]any{
				"like": map[string]any{"count": "367309", "status": 1},
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pg, err := dynamicPageOf(t, dynItem("d1", tc.mutate))
			if err != nil {
				t.Fatalf("这一条把整页带崩了：%v", err)
			}
			if len(pg.Items) != 1 {
				t.Fatalf("留下了 %d 条，want 1（字段换了写法不该让这一条消失）", len(pg.Items))
			}
			if pg.Items[0].ID != tc.wantID {
				t.Errorf("留下的是 %q，want %q", pg.Items[0].ID, tc.wantID)
			}
		})
	}
}

// authorOf 拿到（并造出）module_author 那一段。
func authorOf(m map[string]any) map[string]any {
	mods := m["modules"].(map[string]any)
	a, ok := mods["module_author"].(map[string]any)
	if !ok {
		a = map[string]any{}
		mods["module_author"] = a
	}
	return a
}

// ─────────────────────────────────────────────────────────────────────────────
// 关注列表（作者栏）
// ─────────────────────────────────────────────────────────────────────────────

// TestFollowingsReadsMidNameAndFace 钉住作者栏那一行要的三样东西。
//
// 字段名是 `uname`（不是 `name`）——认错了的话作者栏是一列空名字。
func TestFollowingsReadsMidNameAndFace(t *testing.T) {
	f := spaceClient(t)
	f.on(followingsPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"total": 3,
			"list": []any{
				map[string]any{"mid": 1, "uname": "第一个", "face": "https://f/1.jpg"},
				map[string]any{"mid": 0, "uname": "没有 mid 的"},
				map[string]any{"mid": 2, "uname": "第二个", "face": "https://f/2.jpg"},
			},
		}))
	})

	pg, err := f.client(t, Options{}).Followings(context.Background(), 999, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Authors) != 2 {
		t.Fatalf("拿到 %d 个，want 2（mid 为 0 的那条被丢掉）", len(pg.Authors))
	}
	if pg.Authors[0].Name != "第一个" || pg.Authors[0].Mid != 1 {
		t.Errorf("第一条是 %+v", pg.Authors[0])
	}
	if pg.Total != 3 {
		t.Errorf("总数是 %d，want 3", pg.Total)
	}
	// 「最常访问」排序是上游的默认（`author-list.tsx:47`）：不给的话按关注
	// 时间排，作者栏一打开是一串很久以前的 UP。
	for _, r := range f.requests() {
		if r.Path == followingsPath && r.Query.Get("order_type") != "attention" {
			t.Errorf("order_type 是 %q，want attention（按最常访问排）", r.Query.Get("order_type"))
		}
	}
}

// TestFollowingsStopsAtAnEmptyPage 钉住「空页收口」。
// B 站在页码超了的时候给的是空 list 而不是报错——不收口的话作者栏永远转圈。
func TestFollowingsStopsAtAnEmptyPage(t *testing.T) {
	f := spaceClient(t)
	f.on(followingsPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"total": 500,
			"list":  []any{},
		}))
	})
	pg, err := f.client(t, Options{}).Followings(context.Background(), 999, 99)
	if err != nil {
		t.Fatal(err)
	}
	if pg.HasMore {
		t.Errorf("空页却说还有下一页（total=%d），作者栏会永远转圈", pg.Total)
	}
}

// TestFollowingsNeedsYourOwnMid 钉住「作者栏列的是**我**关注的人」。
//
// 拿页面上那个 UP 的 mid 去问会得到**他**关注的人——作者栏会安安静静地
// 显示一串完全不相干的人，而界面上看不出任何异样。
func TestFollowingsNeedsYourOwnMid(t *testing.T) {
	c := NewClient(Options{})
	if _, err := c.Followings(context.Background(), 0, 1); err == nil {
		t.Error("vmid=0（没登录）不该发出去：那个接口会回 -101")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 有没有新动态
// ─────────────────────────────────────────────────────────────────────────────

// TestDynamicFeedUpdateAsksWithTheBaseline 验「有没有新动态」那一趟。
//
// 判据是**请求里那三个参数**和**回来的那个数**：B 站的这个接口按一个基线算
// 「有多少条比它新」，而基线传错的话那个点要么永远不亮、要么永远亮着。
//
// ⚠️ **它不要签名**：上游那一支没传 useWbi（而 feed/space 传了）。抄错的话
// B 站回 -403，而症状是「那个点永远不亮」。
func TestDynamicFeedUpdateAsksWithTheBaseline(t *testing.T) {
	f := newFakeBili(t)
	f.on(dynamicFeedUpdatePath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"update_num": 7}))
	})
	cli := f.client(t, Options{})

	// ⚠️ **基线是动态 id**（B 站的 update_baseline 要的就是那个，不是什么时间戳：
	// 社区文档写着它是 str、必填，而 /feed/all 的返回里 data.update_baseline 是
	// 「等于 items 中第一条记录的 id」）。
	const baseline = "954659517801431040"
	n, err := cli.DynamicFeedUpdate(context.Background(), baseline)
	if err != nil {
		t.Fatalf("问新动态失败了：%v", err)
	}
	if n != 7 {
		t.Errorf("问回来 %d 条，want 7", n)
	}

	reqs := f.requests()
	if len(reqs) != 1 {
		t.Fatalf("发了 %d 个请求，want 1", len(reqs))
	}
	if reqs[0].Method != http.MethodGet {
		t.Errorf("方法是 %s，want GET（这是个查询）", reqs[0].Method)
	}
	q := reqs[0].Query
	if got := q.Get("update_baseline"); got != baseline {
		t.Errorf("基线是 %q，want %q（那是一个动态 id，不是什么时间戳）", got, baseline)
	}
	if got := q.Get("type"); got != "video" {
		t.Errorf("type 是 %q，want video（上游写死的那个）", got)
	}
	if got := q.Get("web_location"); got != "333.1365" {
		t.Errorf("web_location 是 %q，want 333.1365（上游写死的那个）", got)
	}
}

// TestDynamicFeedUpdateReadsANumberThatComesAsAString 验那个数是字符串也读得出来。
//
// B 站同一个字段有可能给数字也有可能给字符串（见 flexjson.go 的文件头），
// 而这一条读不出来就当 0——那个点就永远不亮，而界面上看不出任何异样。
func TestDynamicFeedUpdateReadsANumberThatComesAsAString(t *testing.T) {
	f := newFakeBili(t)
	f.on(dynamicFeedUpdatePath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"update_num": "12"}))
	})
	cli := f.client(t, Options{})

	n, err := cli.DynamicFeedUpdate(context.Background(), "")
	if err != nil {
		t.Fatalf("问新动态失败了：%v", err)
	}
	if n != 12 {
		t.Errorf("问回来 %d 条，want 12（字符串也要读得出来）", n)
	}
}

// TestDynamicFeedUpdateAsksZeroWhenNothingWasSeen 验「一次都没看过」传的是 0。
//
// ⚠️ **0 是上游 Biu 一直传的那个值**（`user-feed/index.tsx:18`）——那一档是
// 「全部」，于是用户第一次打开就能看见那个点（和上游一样）。
func TestDynamicFeedUpdateAsksZeroWhenNothingWasSeen(t *testing.T) {
	f := newFakeBili(t)
	f.on(dynamicFeedUpdatePath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"update_num": 3}))
	})
	cli := f.client(t, Options{})

	if _, err := cli.DynamicFeedUpdate(context.Background(), ""); err != nil {
		t.Fatalf("问新动态失败了：%v", err)
	}
	reqs := f.requests()
	if len(reqs) != 1 {
		t.Fatalf("发了 %d 个请求，want 1", len(reqs))
	}
	if got := reqs[0].Query.Get("update_baseline"); got != "0" {
		t.Errorf("基线是 %q，want 0（一次都没看过 → 从头算）", got)
	}
}

// TestDynamicFeedCarriesTheUpdateBaseline 验动态流那一页把**更新基线**带出来。
//
// ⚠️ **它是动态 id，不是时间戳**：社区文档写着 /feed/all 的返回里
// `data.update_baseline`「等于 items 中第一条记录的 id」，而 /feed/all/update
// 要的就是它（真实返回长这样："954659517801431040"）。
//
// 拿不到它，「用户看到哪儿了」就没地方记——顶栏那个点于是只能永远传 0（上游
// 那一档），而点掉了之后五分钟它又回来。
func TestDynamicFeedCarriesTheUpdateBaseline(t *testing.T) {
	f := newFakeBili(t)
	f.on(dynamicFeedAllPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"items": []any{}, "offset": "OFF", "has_more": false,
			"update_baseline": "954659517801431040",
		}))
	})
	pg, err := f.client(t, Options{}).DynamicFeed(context.Background(), "")
	if err != nil {
		t.Fatalf("拉动态流失败了：%v", err)
	}
	if pg.Baseline != "954659517801431040" {
		t.Errorf("更新基线是 %q，want 954659517801431040（那是最新那条动态的 id）", pg.Baseline)
	}
}
