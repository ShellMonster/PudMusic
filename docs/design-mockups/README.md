# 设计稿与迭代路线

稿子由 `gpt-image-2.5-flare` 生成，是**方向参考，不逐像素比**（AGENTS.md §1）。落地时数值以 `DESIGN.md` 为准，框架没有的效果（混合模式等）用线性渐变等价替代，毛玻璃用框架自带的 `glass.Blur`。

| 稿 | 对应页面 / 代码 | 状态 |
|---|---|---|
| `skeleton.png` | `widgets/skeleton`；首页新歌速递首屏 | 已做（Block / Row / Card + 扫光）；列表、收藏夹、UP 空间尚未接 |
| `fullplayer.png` | `internal/ui/fullplayer*.go` | 待做：封面投影、取色背景光斑、歌词当前句放大、频谱位置 |
| `miniplayer.png` | `internal/ui/miniplayer.go`，票 #536 | 待做：点封面展开歌词卷帘 |
| `collection_header.png` | `internal/ui/collectionpage.go` | 待做：封面取色渐变头部，全部播放 / 随机播放 / 收藏 / 下载 / 更多一排 |
| `homepage.png` | `internal/ui/homepage*.go` | 待做：分区头（最近播放 / 每日推荐 / 新歌）与层次 |
| `playbar.png` | `internal/ui/playbar.go` | 待做：按封面主色微调整条色调，深浅各一版 |
| `settings.png` | `internal/ui/settings_*.go` | 待做：分组卡片 + 主题预览卡 |
| `empty_states.png` | `internal/ui/empty.go`，`art/` | 待做：一套统一风格的六张空状态插画 |

接新稿前先在 GitHub 开票，UI 票必须带「视觉审查」一条（`docs/agents/visual-review.md`），写明跟哪张稿比。
