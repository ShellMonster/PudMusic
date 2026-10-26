package bili

import (
	"encoding/json"
	"testing"
)

// decodeEntities 钉的是「标题里不再有 &amp; 这种串」：
//
// B 站的 web 接口把文案转义过再回，网页端渲染时顺手解掉，原生端不解的话
// 用户看到的就是「林俊杰&amp;李健合唱《传奇》LIVE…」。这一组钉三件事：
// 标准实体解得掉、全角分号的那一支（用户手里真实出现过的「&amp；」）也解得
// 掉、没有转义的标题（绝大多数）一个字节都不动。
func TestDecodeEntitiesUnescapesTitleText(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"标准实体", "林俊杰&amp;李健合唱《传奇》LIVE JJ20演唱会武汉站-", "林俊杰&李健合唱《传奇》LIVE JJ20演唱会武汉站-"},
		{"引号和单引号", "《歌名》&quot;现场&quot;&#39;版", `《歌名》"现场"'版`},
		{"数字实体", "A &#38; B", "A & B"},
		{"尖括号实体", "a &lt;b&gt; c", "a <b> c"},
		{"全角分号那一支", "林俊杰&amp；李健", "林俊杰&李健"},
		{"双重转义只解一层", "A &amp;amp; B", "A &amp; B"},
		{"没有转义原样回", "普通的标题，带全角；分号", "普通的标题，带全角；分号"},
		{"实体外的全角分号不动", "第一卷；第二卷", "第一卷；第二卷"},
		{"剥过标签的串也照解", "只有 bvid 的结果", "只有 bvid 的结果"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := decodeEntities(c.in); got != c.want {
				t.Errorf("decodeEntities(%q) = %q，want %q", c.in, got, c.want)
			}
		})
	}
}

// TestTheRecommendFeedDecodesTitleEntities 钉住「映射点真的包了解码」：
// 映射点散在十一个文件里，将来新增一个接口映射忘了包，界面上就又是一条
// 「林俊杰&amp;李健」。推荐流是最容易漏的那一支——直接喂它的纯映射函数
// （`regionFeedPage`，和它的测试同一个理由），判据是映射出来的标题和 UP 名
// 里没有原样的转义串。
func TestTheRecommendFeedDecodesTitleEntities(t *testing.T) {
	raw := `{"archives":[{"aid":1,"bvid":"BV1amp","cid":2,
		"title":"林俊杰&amp;李健合唱《传奇》",
		"author":{"mid":3,"name":"UP&amp;UP"},
		"stat":{"view":9},"duration":200,"trackid":"tk"}]}`
	var d regionFeedData
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	items := regionFeedPage(d)
	if len(items) != 1 {
		t.Fatalf("映射出来 %d 条，want 1", len(items))
	}
	if items[0].Title != "林俊杰&李健合唱《传奇》" {
		t.Errorf("标题是 %q，want 解过实体的那一句", items[0].Title)
	}
	if items[0].Owner != "UP&UP" {
		t.Errorf("UP 名是 %q，want 解过实体的那一个", items[0].Owner)
	}
}
