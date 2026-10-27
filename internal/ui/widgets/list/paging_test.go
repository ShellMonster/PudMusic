package list

// 这一组断的是**三重保障的判据**（`DecideFirstPage` / `DecideNextPage`）。
//
// # 为什么要单独断它
//
// 这一份判据是**共享的**：`PagedList` 自己用它，评论页（`internal/ui/
// comments.go`）也用它——两边的底座不一样（音乐卡片 vs 变高正文），但
// 「这一帧该不该拉下一页」判的是同一件事。
//
// ⇒ 判据一旦分叉，症状是**一边会补屏、另一边不会**，而两边看着都「对」——
// 那种差异极难查。所以判据本身要有测试钉住。
//
// # 断的是「边界」，不是「实现」
//
// 这里断的是纯函数的输入输出：什么状态该拉、什么状态不该拉、②（用户滚过去）
// 和 ③（容器没填满）分不分得清。真的拉页那一趟在 `paged_test.go` 里断。

import "testing"

// TestTheFirstPageIsOnlyAskedForOnce 断保障①的判据。
//
// ⚠️ **「已经拿到过」就不要再问**：而「拿到过」包含**一页是空的**——那一档
// 是「没有更多了」，不是「还没问过」。
func TestTheFirstPageIsOnlyAskedForOnce(t *testing.T) {
	for _, tc := range []struct {
		name string
		st   PagingState
		want PagingAction
	}{
		{"还没问过", PagingState{}, PagingLoadFirst},
		{"已经拿到过", PagingState{Fetched: true}, PagingNone},
		{"有一趟在路上", PagingState{Loading: true}, PagingNone},
		{"上一趟失败了", PagingState{Blocked: true}, PagingNone},
		{"拿到过而且有一趟在路上", PagingState{Fetched: true, Loading: true}, PagingNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideFirstPage(tc.st)
			if got.Action != tc.want {
				t.Errorf("判据说该做 %v，期望 %v", got.Action, tc.want)
			}
		})
	}
}

// TestTheNextPageIsOnlyAskedForWhenTheSentinelIsOnScreen 断保障②③的判据。
//
// ⚠️ **哨兵那一行（末尾那一行）进没进画面就是全部**：它在画面外 ⇒ 等用户
// 滚过去；它在画面里 ⇒ 该拉了。
func TestTheNextPageIsOnlyAskedForWhenTheSentinelIsOnScreen(t *testing.T) {
	// 有一屏拉得下：行号 0..4，哨兵是第 5 行（rows=6）。
	const rows = 6

	for _, tc := range []struct {
		name  string
		st    PagingState
		first int
		last  int
		want  PagingAction
	}{
		{"哨兵还在画面外", PagingState{HasMore: true}, 0, 4, PagingNone},
		{"哨兵刚进画面", PagingState{HasMore: true}, 0, 5, PagingLoadNext},
		{"滚到底了", PagingState{HasMore: true}, 3, 5, PagingLoadNext},
		{"已经到底了", PagingState{HasMore: false}, 0, 5, PagingNone},
		{"有一趟在路上", PagingState{HasMore: true, Loading: true}, 0, 5, PagingNone},
		{"上一趟失败了", PagingState{HasMore: true, Blocked: true}, 0, 5, PagingNone},
		{"列表里一行都没有", PagingState{HasMore: true}, 0, -1, PagingNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideNextPage(tc.st, tc.first, tc.last, rows)
			if got.Action != tc.want {
				t.Errorf("判据说该做 %v，期望 %v", got.Action, tc.want)
			}
		})
	}
}

// TestTheRefillBranchAndTheScrollBranchAreToldApart 断②和③分得清。
//
// ⚠️ **两支都是「该拉就拉」**，但「为什么拉」那句话不一样：②是用户滚过去
// 的，③是容器自己没填满（**这一支就是「筛选后只剩两页」的救命的那一支**）。
//
// ⇒ 判据得说得出是哪一支（`Scrolled`），而它**不影响拉不拉**。
func TestTheRefillBranchAndTheScrollBranchAreToldApart(t *testing.T) {
	const rows = 6

	// ③：还在顶上（first == 0）而末尾已经画在屏幕上 = 内容一屏摆得下。
	refill := DecideNextPage(PagingState{HasMore: true}, 0, rows-1, rows)
	if refill.Action != PagingLoadNext {
		t.Fatalf("容器没填满那一支该拉下一页，判据说 %v", refill.Action)
	}
	if refill.Scrolled {
		t.Errorf("容器没填满那一支被说成「用户滚过去的」了")
	}

	// ②：列表不在顶上 = 用户滚过去的。
	scroll := DecideNextPage(PagingState{HasMore: true}, 2, rows-1, rows)
	if scroll.Action != PagingLoadNext {
		t.Fatalf("滚到底那一支该拉下一页，判据说 %v", scroll.Action)
	}
	if !scroll.Scrolled {
		t.Errorf("滚到底那一支没有被说成「用户滚过去的」")
	}
}

// TestMayLoadNextIsTheGateForTheNextPage 断那道闸门本身。
//
// ⚠️ **闸门判的是状态，不判画面**：正在拉 / 上一趟失败了 / 已经到底了——
// 这三种情形下不该再拉（而画面那一半在 `DecideNextPage` 里）。
func TestMayLoadNextIsTheGateForTheNextPage(t *testing.T) {
	for _, tc := range []struct {
		name string
		st   PagingState
		want bool
	}{
		{"一切正常", PagingState{HasMore: true}, true},
		{"正在拉", PagingState{HasMore: true, Loading: true}, false},
		{"上一趟失败了", PagingState{HasMore: true, Blocked: true}, false},
		{"已经到底了", PagingState{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := MayLoadNext(tc.st); got != tc.want {
				t.Errorf("闸门说 %v，期望 %v", got, tc.want)
			}
		})
	}
}
