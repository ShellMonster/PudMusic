// Package title 把 B 站视频标题清洗成「音乐 App 该显示的歌名」。
//
// 来源是 B 站，标题里全是视频的痕迹：`【4K修复】`、`官方MV`、`[Hi-Res]`、
// `【周杰伦】`……产品是纯音乐 App（设计规范 v0.1 §0），这些不该占着歌名。
//
// # ⚠️ **它是公共层的薄封装**
//
// 「从标题里认出歌名」那一份逻辑住在 `internal/ui/songmeta`——**显示**（这一
// 包）和**匹配**（`internal/ui/songname`）**共用**它。两份口径迟早分叉，而
// 分叉出来的那种差异极难查（一个界面上写着「晴天」、另一个界面上筛不出提到
// 「晴天」的评论，而两边看着都「对」）。
//
// 这一包只做**显示层特有的那两件事**：
//
//	① **认不出就退回原标题**（`songmeta` 给的是 `OK == false`，而这里要的是
//	   一个**永远非空**的显示串）
//	② **缓存**（列表每帧每行都要问一次，正则不该每帧跑）
//
// # ⚠️ **宣传词表只有一份**
//
// `IsPromoOnly` / `ContainsPromo` 现在是 `songmeta` 那一处的薄封装——**表仍然
// 只有一份**（理由见 `songmeta.IsPromoOnly` 的注释）。
//
// # 规则（那些判据都在 `songmeta` 里）
//
//	① 《》「」『』引号里  ⇒ **就是歌名**
//	② 【】[] 里           ⇒ 可能是歌名（「【晴天】」），也可能是歌手
//	                        （「【周杰伦】晴天」）——靠前后有没有别的段来定
//	③ ` - ` / `-` 破折号   ⇒ 左边署名、右边歌名（通行约定）
//	④ `:` `：` 冒号        ⇒ 剔掉「说明」那一族，剩下最后一段是歌名
//	⑤ `|` 竖线            ⇒ 歌名在**前**那一段（「起风了 | 吴青峰 | 官方MV」）
//	⑥ 括号**外面**的宣传词  ⇒ 也要清（「官方MV 1080P 高音质」挂在括号外面）
//
// 它只管**显示**：不改 `app.State.Title`，下载文件名、搜索、去重仍用原标题。
package title

import (
	"sync"

	"github.com/ShellMonster/PudMusic/internal/ui/songmeta"
)

// Cleaned 是清洗结果。
type Cleaned struct {
	// Title 是要显示的歌名。
	Title string
	// Extra 是从方括号里挪出来的非宣传内容（歌手 / 专辑名），多段用 " · " 连；
	// 没有就是空串。适合拼进副标题。
	Extra string
	// Live 是「这是现场版」。
	Live bool
}

// cache 记住清洗结果：列表每帧每行都要问一次，正则不该每帧跑。键是原标题，
// 满了（一万条）就整个清掉重来——够用，也不会无限长。
var (
	cacheMu sync.Mutex
	cache   = map[string]Cleaned{}
)

const cacheMax = 10000

// Clean 清洗一条标题（带缓存）。
func Clean(raw string) Cleaned {
	cacheMu.Lock()
	if c, ok := cache[raw]; ok {
		cacheMu.Unlock()
		return c
	}
	cacheMu.Unlock()
	c := clean(raw)
	cacheMu.Lock()
	if len(cache) >= cacheMax {
		clear(cache)
	}
	cache[raw] = c
	cacheMu.Unlock()
	return c
}

// clean 是真正的清洗（不带缓存）。
//
// 🔴 **认不出歌名就退回原标题**（用户口径）：宁可显示脏标题，也不显示空白。
// 而「退回原标题」时**方括号里那些内容照样给出去**（`Extra`）——它们是歌
// 手的名字，对用户有用，**内容不许丢**。
func clean(raw string) Cleaned {
	id := songmeta.Identify(raw)
	if !id.OK {
		return Cleaned{Title: raw, Extra: id.Extra, Live: id.Live}
	}
	return Cleaned{Title: id.Name, Extra: id.Extra, Live: id.Live}
}

// IsPromoOnly 报这一段是不是**全是宣传词**（「官方MV」「4K修复」「高音质」…
// ）。
//
// ⚠️ **它是宣传词那张表的唯一出口**：别处要判「这一段是不是只有宣传词」就走
// 它——自己再抄一份那张表就是两份口径，而分叉出来的那种差异极难查（一个说
// 「官方MV」是宣传词，另一个说不算）。
//
// ⚠️ **实现在 `songmeta`**（表搬到了公共层，见 `songmeta.IsPromoOnly`）。
//
// 🔴 **空串不算**：那是「没有这一段」，不是「这一段全是宣传词」。
func IsPromoOnly(s string) bool { return songmeta.IsPromoOnly(s) }

// ContainsPromo 报这一段里**有没有**宣传词（「官方MV」「4K修复」「高音质」…
// ）。
//
// ⚠️ **它跟 `IsPromoOnly` 的区别是「全是」和「有」**：
//
//	IsPromoOnly   这一段**全是**宣传词（"（官方MV）"那一整个括号丢掉）
//	ContainsPromo 这一段**里有**宣传词（"官方发布"是说明，不是歌名）
//
// ⚠️ **宣传词那张表仍然只有这一处**（理由同 `IsPromoOnly`）。
//
// 🔴 **空串不算**：那是「没有这一段」。
func ContainsPromo(s string) bool { return songmeta.ContainsPromo(s) }

// HasLive 报这一段里有没有「现场」那一族。
//
// ⚠️ **实现在 `songmeta`**（`Live` 那个标记只有一份口径）。
func HasLive(s string) bool { return songmeta.HasLive(s) }
