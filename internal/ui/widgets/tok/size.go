package tok

// 圆角令牌（原型 `--r-4` / `--r-8` / `--r-12` / `--r-pill`）。
const (
	// R2 / R3 / R6 / R10 是原型里零星出现的小圆角：R2 进度条 / 下划线 / 选中条，R3 预览缩略图里的细条，
	// R6 计数角标 / 视频卡角标 / 画中画菜单项，R10 画中画清晰度菜单。
	R2  float32 = 2
	R3  float32 = 3
	R6  float32 = 6
	R10 float32 = 10
	// R4 是小封面（40 / 56）、kbd、角标、侧栏收藏夹封面。
	R4 float32 = 4
	// R8 是控件、列表行、卡片封面、输入框、select（也是 `ui.Theme.Radius`）。
	R8 float32 = 8
	// R12 是卡片、设置卡、抽屉、弹层、对话框、主视觉区、UP 卡。
	R12 float32 = 12
	// Pill 是按钮、Chip、搜索框、Toast：框架按高度的一半画成胶囊。
	Pill float32 = 999
)

// 外壳尺寸（原型 `--side-w` / `--top-h` / `--bar-h` 及 `.win[data-collapsed]`）。
// T1 用它们重排外壳；页面用它们算内容区。
const (
	// SideW 是侧栏展开宽。
	SideW float32 = 232
	// SideCollapsedW 是侧栏收起宽（只剩图标）；内容区 < 1024 时也自动收起。
	SideCollapsedW float32 = 72
	// TopH 是顶栏高。
	TopH float32 = 60
	// BarH 是播放条高。
	BarH float32 = 84
	// AutoCollapseBelow 是窗口内容宽小于它时侧栏自动收起（原型 1024 那条媒体查询）。
	AutoCollapseBelow float32 = 1024
)

// 组件尺寸（原型组件 CSS，DESIGN.md §0′ 的组件表）。公共组件包 `widgets/pud`
// 用的就是这几个数。
const (
	// IconHit 是图标键命中区（`.icon-btn` 36）；IconHitRow 是列表行内那一档（32）。
	IconHit    float32 = 36
	IconHitRow float32 = 32
	// Icon 是图标本身（18）；IconSm 是按钮 / 搜索框里的 16；IconXs 是 Chip 里的 12。
	Icon   float32 = 18
	IconSm float32 = 16
	IconXs float32 = 12
	// BtnH / BtnSmH 是按钮标准高 40 和小号高 32。
	BtnH   float32 = 40
	BtnSmH float32 = 32
	// PlayBtn 是播放条播放键（44）；PlayBtnFull 是全屏那一枚（64）。
	PlayBtn     float32 = 44
	PlayBtnFull float32 = 64
	// ChipH 是 Chip 高（30）。
	ChipH float32 = 30
	// SearchW / SearchH 是顶栏搜索框（440 × 38，胶囊）。
	SearchW float32 = 440
	SearchH float32 = 38
	// MiniSearchH 是列表工具栏里的小搜索框（32）。
	MiniSearchH float32 = 32
	// RowH / RowCompactH 是曲目行高 60 / 紧凑 44；NavRowH 是侧栏导航行高 40。
	RowH        float32 = 60
	RowCompactH float32 = 44
	NavRowH     float32 = 40
	// CoverRow / CoverBar / CoverMini 是行内封面 40、播放条封面 56、迷你 / 动态 72。
	CoverRow  float32 = 40
	CoverBar  float32 = 56
	CoverMini float32 = 72
	// CoverPlay 是卡片封面上悬停出现的播放键（44），离封面右下各 12。
	CoverPlay      float32 = 44
	CoverPlayInset float32 = 12
	// DrawerW 是抽屉宽（400）；DialogW 是对话框宽（440）。
	DrawerW float32 = 400
	DialogW float32 = 440
	// PopMinW 是弹层最小宽（200）。
	PopMinW float32 = 200
	// SwitchW / SwitchH 是开关（44 × 26），旋钮 20。
	SwitchW    float32 = 44
	SwitchH    float32 = 26
	SwitchKnob float32 = 20
	// SelectH 是下拉高（36）。
	SelectH float32 = 36
	// BadgeH 是音质角标高（18）。
	BadgeH float32 = 18
	// RangeTrack 是进度条轨道高（4）；RangeThumb 是悬停时出现的旋钮（12）。
	RangeTrack float32 = 4
	RangeThumb float32 = 12
)
