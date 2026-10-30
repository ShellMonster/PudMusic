// Package notice 定义「说一句话」的那句话的语义：成功、警告、失败、普通。
//
// # 为什么它不在界面层里
//
// 语义要**跟着那句话一路走到画它的地方**，而说那句话的地方有好几层：
//
//   - 界面层（`internal/ui`）把那句话画成右下角那张卡片；
//   - 应用层（`internal/app`）在后台做完一件事之后递回来一句话（「已播放 200 首，
//     这个收藏夹里还有更多没放进来」/「播放不了这个收藏夹：被 B 站风控拦下了」）；
//   - 「列表会做的那些操作」（`internal/ui/widgets/list`）也要说一句话——它不知道
//     提示长什么样，但知道这句话是「成了」还是「没成」。
//
// 应用层**不认识界面**（`internal/app` 的包注释第一条），而界面层是 import
// 应用层的那一边——所以语义**不能住在界面层里**（那样应用层就 import 不到它），
// 只能住在**谁都能 import、而自己不依赖任何东西**的地方：颜色是界面层的事
// （`internal/ui` 里的 `toastColors`），这里只说「这句话是哪种」。
//
// # 四档是哪四档
//
// 上游 Biu 的 `addToast` 按调用给 `color`：`danger` / `success` / `warning` /
// `primary`（`src/app.tsx:167-177`，164 处调用：danger 84、success 58、
// warning 22、primary 0）。这里是同一套四档，名字换成了这一侧的说法：
//
//	Plain   普通（上游的 primary）：主色
//	Success 成功：绿 `#17c964`
//	Warning 警告：橙 `#f5a524`
//	Danger  失败：红 `#f31260`
//
// 那三个色值是上游 HeroUI 的语义色（DESIGN.md §四），深浅两套同一个值。
package notice

// Tone 是那句话的语义。
type Tone uint8

const (
	// Plain 是那句普通的话：主色（上游的 `color: "primary"`）。
	//
	// 它是**零值**：一条没给语义的话默认按普通画，而普通就是主色——
	// 上游那 164 处调用里 `primary` 那一档是 0 处，可我们这边「刚发生了
	// 一件事，说一声」的中性提示不少。
	Plain Tone = iota
	// Success 是那句成功的话：绿。
	Success
	// Warning 是那句警告的话：橙。
	Warning
	// Danger 是那句失败的话：红。
	Danger
)

// SearchErrorCategory classifies a search failure for application and UI code.
//
// The category carries stable meaning across layers; the user-facing sentence
// for it is kept in this package rather than in internal/app.
type SearchErrorCategory uint8

const (
	SearchErrorCategoryUnknown SearchErrorCategory = iota
	SearchErrorCategoryMissingClient
	SearchErrorCategoryInvalidTab
	SearchErrorCategoryCanceled
	SearchErrorCategoryNetwork
	SearchErrorCategoryNotLoggedIn
	SearchErrorCategoryRiskControl
	SearchErrorCategoryTooFrequent
	SearchErrorCategoryCSRF
	SearchErrorCategoryBadResponse
	SearchErrorCategoryTruncatedResponse
	SearchErrorCategoryAPI
)

// SearchErrorText returns the stable Chinese presentation for a search error
// category. Application code may retain this text for compatibility, while UI
// code can use the category directly when choosing what to render.
func SearchErrorText(category SearchErrorCategory) string {
	switch category {
	case SearchErrorCategoryMissingClient:
		return "搜索暂时用不了"
	case SearchErrorCategoryInvalidTab:
		return "搜索类型暂时用不了"
	case SearchErrorCategoryCanceled:
		return ""
	case SearchErrorCategoryNetwork:
		return "连不上 B 站，检查一下网络"
	case SearchErrorCategoryNotLoggedIn:
		return "登录已经过期了，重新扫码登录一下"
	case SearchErrorCategoryRiskControl:
		return "被 B 站的风控拦下了，等一会儿再试"
	case SearchErrorCategoryTooFrequent:
		return "操作太快了，等一会儿再试"
	case SearchErrorCategoryCSRF:
		return "登录状态有问题，重新登录一次就好"
	case SearchErrorCategoryBadResponse:
		return "B 站这次回的不是预期的内容（多半是被挡下了），等一会儿再试"
	case SearchErrorCategoryTruncatedResponse:
		return "B 站这次只回了一半（多半是网络抖了一下），再试一次就好"
	case SearchErrorCategoryAPI:
		return "B 站拒绝了这次请求"
	default:
		return "拉不到这一场搜索，等一会儿再试"
	}
}
