package list

// 这一组是「换查询」和「回顶」那几条：取消在途请求、旧结果作废、切路由回顶、
// 滚 400 像素的回顶按钮。假拉页器在 paged_test.go（同一包，共用）。

import "testing"

// ─────────────────────────────────────────────────────────────────────────────
// 取消与旧结果作废
// ─────────────────────────────────────────────────────────────────────────────

// TestReloadCancelsThePageInFlight 是验收第 3 条的前半句：中途改关键词能取消
// 在途请求。取消是真的取消（fetch 的 ctx 收到 Done，服务端收得到）。
func TestReloadCancelsThePageInFlight(t *testing.T) {
	st := newStub().with(1, 200, true).gate(2)
	p, tt := newPaged(t, st.fetch)

	pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 200 })
	tt.Scroll(testerW/2, testerH/2, 0, scrollToBottom)
	pump(t, tt, "第 2 页发出去", func() bool { return p.Paged().Loading })

	p.Reload() // 换关键词了
	pump(t, tt, "看见取消", func() bool { return st.sawCancel(2) })
}

// TestAStaleResultNeverLands 是 PLAN 风险栏点名的那一条：
// **用户连点筛选条件时，旧请求的结果不能覆盖新条件的结果**。
//
// 这一条是 cancel 挡不住的：请求已经回来了、正往信箱里投的时候，cancel 已经
// 来不及。所以靠的是代次（gen）。
func TestAStaleResultNeverLands(t *testing.T) {
	st := newStub().with(1, 200, true).stickyPage(2)
	p, tt := newPaged(t, st.fetch)

	pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 200 })
	tt.Scroll(testerW/2, testerH/2, 0, scrollToBottom)
	pump(t, tt, "第 2 页发出去", func() bool { return p.Paged().Loading })

	// 换关键词：这一页作废。**先**换好新条件的答案再 Reload —— Reload 当场就
	// 发第一页，答案换晚了它会拉到旧条件的那一份（那就是这个测试要抓的东西）。
	st.serve(1, Page{Songs: pageSongs(9, 3), HasMore: false}, nil)
	p.Reload()
	st.release(2) // 旧的那一页这时候才回来（它没理 ctx，照样给了 200 行）
	pump(t, tt, "新条件的第一页落地", func() bool { return p.Paged().Loaded == 3 && !p.Paged().Loading })
	frame(tt)
	frame(tt)

	if got := p.Paged().Loaded; got != 3 {
		t.Errorf("加载了 %d 行，want 3：旧条件那 200 行漏进来了", got)
	}
	if p.Paged().Page != 1 {
		t.Errorf("Page = %d，want 1", p.Paged().Page)
	}
	if onScreen(tt, "第 2 页 歌 1") {
		t.Errorf("旧条件那页的行还在画面上")
	}
}

// TestReloadClearsAndGoesBackToTheTop 是「切路由回顶」：换查询之后从第一页
// 重新来，滚动位置回到顶上。
func TestReloadClearsAndGoesBackToTheTop(t *testing.T) {
	st := newStub().with(1, 200, true)
	p, tt := newPaged(t, st.fetch)

	pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 200 })
	tt.Scroll(testerW/2, testerH/2, 0, scrollToBottom)
	frame(tt)
	if p.scroll.Y < 400 {
		t.Fatalf("没滚下去（Y=%v），后面那句就是假的", p.scroll.Y)
	}

	st.serve(1, Page{Songs: pageSongs(9, 3), HasMore: false}, nil)
	p.Reload()
	pump(t, tt, "新查询落地", func() bool { return p.Paged().Loaded == 3 && !p.Paged().Loading })
	frame(tt)

	if p.scroll.Y != 0 {
		t.Errorf("换查询之后滚到 Y=%v，want 0（切路由要回顶）", p.scroll.Y)
	}
	if !onScreen(tt, "第 9 页 歌 1") {
		t.Errorf("换查询之后第一行不在画面上，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 回顶按钮
// ─────────────────────────────────────────────────────────────────────────────

// TestBackToTopAppearsAfter400Pixels 是验收第 4 条的后半句。400 是死数。
func TestBackToTopAppearsAfter400Pixels(t *testing.T) {
	st := newStub().with(1, 200, false)
	p, tt := newPaged(t, st.fetch)

	pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 200 })
	if tt.HasText("返回顶部") {
		t.Fatalf("还没滚就出现回顶按钮了")
	}

	// 滚 300 像素：不够，不出现。
	tt.Scroll(testerW/2, testerH/2, 0, 300)
	frame(tt)
	if p.scroll.Y < 300 {
		t.Skipf("Tester 一次滚不动 300 像素（Y=%v），这一条验不了", p.scroll.Y)
	}
	if tt.HasText("返回顶部") {
		t.Errorf("滚了 %v 像素就出现回顶按钮了，want 400 以后", p.scroll.Y)
	}

	// 再滚到 400 以上：出现。
	tt.Scroll(testerW/2, testerH/2, 0, 200)
	frame(tt)
	if p.scroll.Y <= backTopPx {
		t.Fatalf("滚到 %v 像素还是没有回顶按钮", p.scroll.Y)
	}
	if !tt.HasText("返回顶部") {
		t.Fatalf("滚过 400 像素却没有回顶按钮，文字有 %q", tt.Texts())
	}

	// 点它：回顶。
	if err := tt.Click("返回顶部"); err != nil {
		t.Fatalf("点不到回顶按钮：%v", err)
	}
	frame(tt)
	frame(tt)
	if p.scroll.Y != 0 {
		t.Errorf("点了回顶之后 Y=%v，want 0", p.scroll.Y)
	}
	if tt.HasText("返回顶部") {
		t.Errorf("回到顶上之后按钮还在")
	}
	if !onScreen(tt, "第 1 页 歌 1") {
		t.Errorf("回到顶上之后第一行不在画面上，文字有 %q", tt.Texts())
	}
}
