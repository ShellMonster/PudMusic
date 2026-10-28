package notice

import "testing"

func TestSearchErrorTextMapsCategories(t *testing.T) {
	tests := []struct {
		category SearchErrorCategory
		want     string
	}{
		{SearchErrorCategoryMissingClient, "搜索暂时用不了"},
		{SearchErrorCategoryInvalidTab, "搜索类型暂时用不了"},
		{SearchErrorCategoryCanceled, ""},
		{SearchErrorCategoryNetwork, "连不上 B 站，检查一下网络"},
		{SearchErrorCategoryNotLoggedIn, "登录已经过期了，重新扫码登录一下"},
		{SearchErrorCategoryRiskControl, "被 B 站的风控拦下了，等一会儿再试"},
		{SearchErrorCategoryTooFrequent, "操作太快了，等一会儿再试"},
		{SearchErrorCategoryCSRF, "登录状态有问题，重新登录一次就好"},
		{SearchErrorCategoryBadResponse, "B 站这次回的不是预期的内容（多半是被挡下了），等一会儿再试"},
		{SearchErrorCategoryTruncatedResponse, "B 站这次只回了一半（多半是网络抖了一下），再试一次就好"},
		{SearchErrorCategoryAPI, "B 站拒绝了这次请求"},
		{SearchErrorCategoryUnknown, "拉不到这一场搜索，等一会儿再试"},
	}
	for _, test := range tests {
		if got := SearchErrorText(test.category); got != test.want {
			t.Errorf("SearchErrorText(%d) = %q, want %q", test.category, got, test.want)
		}
	}
}
