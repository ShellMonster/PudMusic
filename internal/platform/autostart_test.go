package platform

import "testing"

// TestPlanAutoStart 验启动时该不该动系统的开机自启。
//
// 这条规则的方向性比它本身重要：反过来的那种情况（设置说关、系统说开）
// **故意**不处理——开机自启也可能是用户自己在「系统设置 → 通用 → 登录项」
// 里加的，启动时拿我们设置文件里的一个字节把它按掉，是替用户做决定。
// 写成 `if want != applied { ... }` 看起来更对称，实际是错的。
func TestPlanAutoStart(t *testing.T) {
	cases := []struct {
		want, applied bool
		action        AutoStartAction
		why           string
	}{
		{false, false, AutoStartKeep, "设置说关、系统也没开：什么都不用做"},
		{false, true, AutoStartKeep, "设置说关、系统开着：不动（多半是用户自己在系统设置里加的）"},
		{true, true, AutoStartKeep, "已经对上了"},
		{true, false, AutoStartTurnOn, "设置说开、系统里没有：补上，否则那个开关就是一句空话"},
	}
	for _, c := range cases {
		if got := PlanAutoStart(c.want, c.applied); got != c.action {
			t.Errorf("PlanAutoStart(%v, %v) = %v，want %v（%s）", c.want, c.applied, got, c.action, c.why)
		}
	}
}
