// 复制当前这一首的信息和封面。
//
// # 两个动作
//
//	复制歌曲信息	把这一首的可读信息放进剪贴板（纯文本）
//	复制封面		把当前封面作为 PNG 放进剪贴板
//
// 入口是**当前封面上的右键菜单**（播放条上那一张、满屏里那一张共用同一份
// 菜单，见 `viewCopyMenu`）——而**不是**在播放条上再摆一枚键：
//
//   - 播放条左边那一列的宽度是被**钉死的**（`playbarLeftMinW` = 244），加一枚
//     键就要动那个数，而那个数一动，窄窗口下歌名的宽度跟着变（那一处有
//     `TestTheLeftBlockIsThreeSegments` 和 `playbar_layout_test.go` 钉着）。
//   - 满屏顶栏那一组是**贴着歌名**摆的（上游 `space-x-2`，齿轮紧跟歌名，
//     8 像素——`TestTheFullPlayerGearSitsRightAfterTheTitle` 钉着），加一枚键要
//     重算那一组的构图。
//
// 所以走右键菜单：**不碰任何一处布局**，也就没有视觉审查那一档的负担。
//
// # 为什么走系统原生菜单
//
// 和列表行菜单、队列抽屉行菜单一样（见 `widgets/list/menu.go` 的文件头）：
// 验收要的是「系统原生样式、键盘方向键能导航」，只有系统菜单免费给这两样。
//
// # 键盘 / 读屏怎么够得着
//
// 封面那一层是**可聚焦**的（`.Focusable()`），于是：
//
//   - **菜单键 / Shift+F10** 在焦点落在封面上时开出这一份菜单（框架的
//     `menuKey` 找的是「焦点链上最里层那个带菜单的元素」，见
//     `mygo/ui/menu.go` 的 `menuTarget`）；
//   - 读屏软件念得到那一层（`.Label(...)` 给了它一个名字）。
//
// ⚠️ **别给封面那一层再补一次 `Label("当前封面")`**：里面那张图已经有一个
// 了（`imgcache.View...(...).Label("当前封面")`），补第二个会让读屏念两遍。
// 这一层用的是**另一个名字**（「当前封面的操作」），说的是「这一层能干什么」。
package ui

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"strings"
	"sync"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/title"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// 菜单里那两项的名字。
//
// ⚠️ **它们是菜单项的字**，而菜单项是系统画的——读屏软件念的就是这两个字，
// 所以它们自己得说得清（不能是「复制」「复制图」这种）。
const (
	copyInfoLabel  = "复制歌曲信息"
	copyCoverLabel = "复制封面"
)

// copyNoCoverNote 是**没有封面时**在菜单末尾说的那句话。
//
// ⚠️ **按不动而不说为什么等于让用户猜**（同一句话在 `widgets/list/menu.go`
// 的 `note` 那一栏写着）。所以「复制封面」那一项按不动，而末尾补一句说明。
const copyNoCoverNote = "这一首没有封面，复制不了"

// copyCoverActionsLabel 是封面那一层给读屏软件的名字。
//
// ⚠️ **不能叫「当前封面」**：里面那张图已经有一个了（`imgcache.View…`），而
// 读屏会把两个名字都念一遍——用户听到的是「当前封面，当前封面的操作」。
const copyCoverActionsLabel = "当前封面的操作"

// copyAction 是**一次复制动作**：它拿到的号，和把它的内容写进剪贴板的那一下。
type copyAction struct {
	// gen 是这次动作的号（见 `copyClipboard.next`）。
	gen uint64
	// write 把这一档的内容写进剪贴板（接缝那一下）。
	write func() error
}

// copyClipboard 是「复制」这一族**全进程一份**的状态。
//
// # 它挡的是什么
//
// 「复制封面」那一趟要下网络 + 解一张图 + 编一张 PNG，全在**后台 goroutine**
// 上。而那个后台跑完的时候，**用户可能已经又按了别的**：
//
//	10:00:00  按「复制封面」（封面 A 在慢速网络上，要 3 秒）
//	10:00:01  按「复制歌曲信息」（一瞬间就写完了）
//	10:00:03  封面 A 的后台跑完了，把封面 A 写进剪贴板
//
// 于是**剪贴板里最后是封面 A**，而用户**最后按的是「复制歌曲信息」**——
// 他粘出来的是一张图，不是他最后要的那段文字。
//
// 症状是**静默**的：提示弹出来的是「封面已复制」（那一趟确实成功了），
// 而剪贴板里的东西和用户最后一次的选择对不上，没有任何报错。
//
// # 为什么「写之前比一比号」**不够**
//
// 起初的做法是「写之前比一比自己那个号是不是最新的」——而**那漏了一种交错**：
// 「比」和「写」是**两件事**，中间夹着一次真正写剪贴板（要走主线程，可能
// 慢）。于是这一种漏了过去：
//
//	10:00:00  按「复制封面」
//	10:00:01  封面那一趟**比过了**（它当时确实是最新的一趟），开始写
//	10:00:02  按「复制歌曲信息」——写完了，剪贴板里是那段文字
//	10:00:03  封面那一趟的写**落地**——剪贴板里又变成那张旧封面
//
// 用户最后按的是文字，而剪贴板里是图。**「比过了」不等于「写进去的时候
// 仍然是那一档」**——中间还有一次真正的写。
//
// # 怎么挡：**写之前比一比 + 写之后收敛**
//
// **写之前**比一比号（`isLatest`）：**不是最新的一趟什么都不写**——剪贴板里
// **压根不出现**过时的东西（这是大多数情形：旧封面还在下网络，文本早就写完了）。
//
// 而**写之后**还要再比一次——因为「写」和「比」之间夹着一次真正的写（要走
// 主线程，可能慢），而那一次里用户可能又按了别的。所以这里多记一样东西：
// **剪贴板里现在是什么**（`committed`，连着写下它那一档的号）。每一趟写完
// 之后**比一比**：
//
//   - 自己那一趟的号 **≥** 剪贴板里那一档的号 → 我写的这一份就是最新的 →
//     记下来，收工；
//   - **小于** → 有一档**更新的**内容在我写之前就已经写进剪贴板了，而我这一趟
//     **把它盖掉了** → **把那一档再写回去**（然后再比一次）。
//
// ⚠️ **「盖掉了就写回去」**：盖掉的那一刻已经过去了，而剪贴板是**一个全局的
// 格子**——别的应用可能正好在读它。要让**最后**留在剪贴板里的一定是用户最后
// 按的那一份，只能这样收敛。
//
// ⚠️ **收敛留着一小段「过时内容在剪贴板里」的窗口**（盖掉的那一下到写回去
// 的那一下之间）：**握着锁写是走不通的**——框架那一档要走主线程并**等**它
// （`mygo.Clipboard.Write` → `onMain`），而主线程可能正在按「复制歌曲信息」
// 并**等同一把锁**，那就是死锁。所以这里**不握着锁写**，改用收敛。
//
// ⚠️ **文本那一档也要占号**：文本是同步写的（不等网络），但它**照样要作废
// 前面那一趟封面**——否则「先按封面（慢）、再按文本」最后留着的还是那张图。
// 而「先按文本（快）、再按封面（慢）」是对的：封面是**最后**按的那一个，
// 它跑完之后写进去，正是用户要的。
type copyClipboard struct {
	mu sync.Mutex
	// gen 是用户到这一刻为止一共按过几次复制。
	gen uint64
	// committed 是「剪贴板里现在是什么」：那一档，和写下它的那次动作的号。
	//
	// ⚠️ **零值是「还没有复制过」**（号 0）：于是第一次写的那一趟（号 ≥ 1）
	// 一定赢——而它也确实该赢（剪贴板里本来就是空的）。
	committed copyAction
}

// copyClip 是**全进程一份**的那一份状态。
//
// ⚠️ **它必须是包级的，不能每个外壳一份**：剪贴板是**整个系统只有一份**，
// 而主窗口和迷你播放器是**两个外壳**——两边都发起复制的话，两个各自计数
// 的号互不相识，于是「迷你窗发起的封面（慢）、主窗发起的文本（快）」这一趟
// 不会把迷你窗那一趟作废掉。
//
// ⚠️ **不是每个测试一份**：这一份是**全局状态**，而测试要能断言「这一趟被
// 作废了」——所以测试靠**号本身**（而不是全局的当前值）来断，见
// `copyClipboard.isLatest`。
var copyClip copyClipboard

// next 发一个新的号，并作废前面所有的号。
func (c *copyClipboard) next() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	return c.gen
}

// isLatest 报 gen 还是不是**最新**的那一个。
//
// ⚠️ **比的是「有没有人后来又按了一次」**，不是「是不是我发的那个」——
// 两者在只有一个动作时一样，在两个动作时不同，而这正是要挡的那一种。
func (c *copyClipboard) isLatest(gen uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return gen == c.gen
}

// commit 把这一档写进剪贴板，并且**保证最后留在剪贴板里的是用户最后按的那一份**。
//
// 它报「**该不该说「已复制」**」——不是的话这一趟**什么也不说**（用户后来
// 按了别的，那一档会自己说；这一趟说了就是骗人）。
//
// ⚠️ **写的时候不握着那把锁**（理由见类型注释里那一条「不会死锁」）。
func (c *copyClipboard) commit(a copyAction) (bool, error) {
	// ⚠️ **mine 是「我这一趟」的号**：收敛会把 `a` 换成**别的**那一档（把它
	// 写回去），所以最后回答「剪贴板里是不是我这一份」时要比的是 `mine`，
	// 不是 `a.gen`——否则**过时的封面那一趟会以为自己赢了**（它收敛之后
	// `a.gen` 是**新**那一档的号），于是弹一句「封面已复制」——而剪贴板里
	// 其实是那段文字。
	mine := a.gen
	// ⚠️ **写之前先比一次**：不是最新的一趟**什么都不写**——剪贴板里压根不
	// 出现过时的内容（这是大多数情形：旧封面还在下网络，而文本早就写完了）。
	if !c.isLatest(mine) {
		return false, nil
	}
	for {
		err := a.write()
		c.mu.Lock()
		if err != nil {
			// 没写进去 → 剪贴板里还是原来那份 → 不用收敛。
			c.mu.Unlock()
			return false, err
		}
		if a.gen >= c.committed.gen {
			c.committed = a
			c.mu.Unlock()
			// ⚠️ **「我这一份现在在剪贴板里」还不够**：用户可能又按了别的
			// （那一档还没写进去）。那时候说「已复制」是在骗人——它马上就要
			// 被换掉。而**那一档会自己说**。
			return c.isLatest(mine), nil
		}
		// 有一档比我新的，它在我写之前就写进去了，而我把它盖掉了 →
		// **把它写回去**（然后再比一次）。
		newer := c.committed
		c.mu.Unlock()
		a = newer
	}
}

// viewCopyMenu 建当前这一首的「复制」菜单。播放条上那一张封面和满屏里那一张
// 共用它（两处的入口是同一个动作）。
//
// ⚠️ **每一项都要 `m.Item` 一遍**：菜单是这趟全部建出来的，只有被 `Chosen`
// 的那一项为 true——少建一项，那一项就永远点不到（`mygo/ui/menu.go` 的
// `MenuItem.Chosen`）。
//
// ⚠️ **st 是这一帧的那一份**（外面画这一块时读出来的），不是现读快照：菜单是
// **弹出那一刻**才建的，而现读快照拿到的是**弹出之后**的样子——那一帧可能已经
// 换歌了（菜单里的歌名和用户右键时看到的那一首对不上）。
func (s *Shell) viewCopyMenu(st app.State, m *ui.Menu) {
	hasCover := st.CoverURL != ""

	// 「复制歌曲信息」什么时候都摆得出来：本地文件也有歌名和路径。
	if m.Item(copyInfoLabel).Chosen() {
		s.copySongInfo(st)
		return
	}
	// 「复制封面」没有封面时按不动（灰的），末尾再补一句为什么。
	if m.Item(copyCoverLabel).Disabled(!hasCover).Chosen() {
		s.copyCover(st)
		return
	}
	if !hasCover {
		m.Item(copyNoCoverNote).Disabled(true)
	}
}

// songInfoText 是「复制歌曲信息」放进剪贴板的那段文本。
//
// # 格式是**稳定的**（票面要求）
//
//	歌名：<歌名>
//	UP主：<UP 主 / 歌手>		（这一项没有就不出现）
//	时长：<m:ss>			（这一项没有就不出现）
//	BVID：<BV...>			（在线才有）
//	链接：<https://…>		（在线才有）
//	路径：<本地路径>			（本地文件才有）
//
// ⚠️ **每一行是「标签：值」**：用户粘到聊天框里之后一眼看得出哪一行是什么，
// 而**不粘进任何应用的结构化字段**（那是另一个需求，那时要升级到
// `Clipboard.Write` + `Flush`，见文件末尾那一节）。
//
// ⚠️ **没有的字段整行不出现**，不是留空：留一行「UP主：」比不出现更难读。
//
// ⚠️ **歌名用清洗过的那一份**（`title.Clean`）：界面上摆的是清洗后的歌名
// （去掉了「【4K】」「官方MV」那些视频痕迹），而**复制出去的要是同一个**——
// 复制出来是「【4K修复】晴天」而界面上写着「晴天」，那是两首歌。
func songInfoText(st app.State) string {
	var b strings.Builder
	write := func(label, value string) {
		if value == "" {
			return
		}
		b.WriteString(label)
		b.WriteString("：")
		b.WriteString(value)
		b.WriteString("\n")
	}
	write("歌名", title.Clean(st.Title).Title)
	write("UP主", st.Artist)
	if st.Duration > 0 {
		write("时长", formatSongDuration(st.Duration))
	}
	// BVID 和链接：从 B 站地址里拆（`app.BiliLink` 的那两种形状：投稿 / 音频）。
	bvid, link := splitBiliLink(st.BiliURL)
	write("BVID", bvid)
	write("链接", link)
	write("路径", st.Path)
	return strings.TrimRight(b.String(), "\n")
}

// splitBiliLink 从 B 站地址里拆出 BVID 和地址本身。
//
// ⚠️ **本地文件没有这一对**，而**没有稿件身份的在线条目也没有**（连同
// `BiliLink` 那个函数一样，见 `app/unplayable.go`）。拆不出来就是空串，
// 而空串那一行整行不出现。
//
// ⚠️ **音频条目（`audio/au<sid>`）拆不出 BVID**：那一串是**音频 id**（sid），
// 不是稿件号（BVID）。把它当 BVID 报出去是**说错了**——用户拿着「au123」
// 去 B 站搜，搜出来的是别的东西。所以那一档只报链接，不报 BVID。
func splitBiliLink(url string) (bvid, link string) {
	if url == "" {
		return "", ""
	}
	link = url
	const videoPrefix = "https://www.bilibili.com/video/"
	const audioPrefix = "https://www.bilibili.com/audio/au"
	switch {
	case strings.HasPrefix(url, videoPrefix):
		bvid = strings.TrimPrefix(url, videoPrefix)
	// ⚠️ **音频那一档不拆 BVID**（理由见上面那段注释）。
	case strings.HasPrefix(url, audioPrefix):
	}
	return bvid, link
}

// formatSongDuration 把时长写成 `m:ss`（超过一小时的是 `h:mm:ss`）。
//
// ⚠️ **分不补零、秒补零**（`3:05` 不是 `03:05`）：那一份是给人读的。
func formatSongDuration(d time.Duration) string {
	total := int(d.Round(time.Second).Seconds())
	if total < 0 {
		total = 0
	}
	h, m, sec := total/3600, (total%3600)/60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%d:%02d", m, sec)
}

// copySongInfo 把这一首的信息放进剪贴板。
//
// ⚠️ **失败了要说得出来**（票面要求「不静默失败」）：剪贴板写失败就弹一句
// 提示，而不是什么都不做。
func (s *Shell) copySongInfo(st app.State) {
	// ⚠️ **先占号**：这一档虽然是同步写完的，但它**照样要作废前面那一趟封面**
	// （「先按封面（慢）、再按文本」最后留着的必须是那段文字，见 `copyClipboard`）。
	gen := copyClip.next()
	text := songInfoText(st)
	if text == "" {
		s.addToast("这一首没有可复制的信息", notice.Warning)
		return
	}
	write := s.writeClipboardText
	if write == nil {
		write = systemWriteClipboardText
	}
	// ⚠️ **走 `commit` 而不是「比一比再写」**：这一档是同步的，但**写剪贴板
	// 本身要走到主线程**（框架那一档），而用户在那段时间里按了「复制封面」
	// 是可能的——「比过了」不等于「写进去的时候仍然是这一档」（见
	// `copyClipboard` 的注释）。
	mine, err := copyClip.commit(copyAction{gen: gen, write: func() error {
		return write(text)
	}})
	if err != nil {
		s.addToast("复制不了歌曲信息："+err.Error(), notice.Danger)
		return
	}
	// ⚠️ **不是我这一份就不说「已复制」**：用户后来按了别的，那一档会自己说。
	if !mine {
		return
	}
	s.addToast("歌曲信息已复制", notice.Success)
}

// copyCover 把当前封面编码成 PNG 放进剪贴板。
//
// # 为什么在**回调里**才编码
//
// 编码是 CPU 活（一张 1920×1080 的封面编成 PNG 要几百毫秒），而这一段是
// 用户**按下去之后**才跑的——不在每一帧里跑。菜单回调本身就在用户按下的
// 那一帧（框架会在选中的那一帧重跑一遍菜单的建树函数），所以天然只有一次。
//
// ⚠️ **取字节 + 解码 + 编码都在后台**：这一段要下网络（封面可能还没在盘上）
// 和解一张图，在主线程上做会把界面冻住。所以这一整个动作跑在后台 goroutine
// 上，完了调 `addToast`（它自己带锁）和 `wake.invalidate`（铁律三）。
func (s *Shell) copyCover(st app.State) {
	if st.CoverURL == "" {
		// 这一格按理按不到（菜单里那一项是灰的）。留着是为了「菜单项按得动
		// 但那时封面没了」这一种——直接说清楚，不静默。
		s.addToast(copyNoCoverNote, notice.Warning)
		return
	}
	// ⚠️ **占号在起后台之前**：占晚了的话，「先按封面（慢）、再按文本」这一趟
	// 的号会被后来的文本占掉，于是这一趟跑完发现自己不是最新的——那是对的；
	// 但反过来「先按文本、再按封面」要是占晚了，就会把**用户最后按的那一次**
	// 作废掉（文本那一趟已经写完了，而封面这一趟被自己作废）——剪贴板里留着
	// 的是文本，而用户最后按的是封面。所以号必须在**动作开始的那一刻**发。
	gen := copyClip.next()
	url := st.CoverURL
	go s.copyCoverInBackground(url, gen)
}

// copyCoverInBackground 是「复制封面」的后台那一半。
//
// ⚠️ **gen 是这一趟自己的号**：跑完时要比一比它还是不是最新的（见
// `copyClipboard`）。不是最新就**什么都不写**——用户后来按的那一次才是
// 他要的。
func (s *Shell) copyCoverInBackground(url string, gen uint64) {
	// ⚠️ **收尾那一句话在最后**：这一趟可能什么都没写（被后来的动作作废了），
	// 所以「它结束了」这件事**不能靠剪贴板里的内容判断**——测试要靠这一句。
	defer func() {
		if s.copyDone != nil {
			s.copyDone(gen)
		}
	}()
	data, err := s.coverPNG(url)
	if err != nil {
		// ⚠️ **只有还是最新的那一趟才说话**：过时的失败不用吵用户——
		// 用户后来已经按了别的，他要的是那一个的结果。
		if copyClip.isLatest(gen) {
			s.addToast("复制不了封面："+err.Error(), notice.Danger)
			s.wake.invalidate()
		}
		return
	}
	write := s.writeClipboardImage
	if write == nil {
		write = systemWriteClipboardImage
	}
	// ⚠️ **走 `commit`**：这一段是整趟里最贵的一步（编一张 PNG），而它在
	// 后台跑着——用户完全可能在它跑的时候又按了一次。
	// 而「比过了号」**不等于**「写进去的时候仍然是这一档」：中间还夹着一次
	// 真正的写（要走主线程，可能慢），而那一次里用户可能又按了文本——
	// 那样的话旧封面会盖掉那段文字（见 `copyClipboard` 的注释）。
	mine, err := copyClip.commit(copyAction{gen: gen, write: func() error {
		return write(data)
	}})
	if err != nil {
		if copyClip.isLatest(gen) {
			s.addToast("复制不了封面："+err.Error(), notice.Danger)
			s.wake.invalidate()
		}
		return
	}
	// ⚠️ **不是我这一份就不说「已复制」**：用户后来按了别的，那一档会自己说。
	// 而**过时的那一趟不叫醒界面**：它什么也没说、什么也没改成，画面上
	// 没有变化——叫醒只是白画一帧。
	if !mine {
		return
	}
	s.addToast("封面已复制", notice.Success)
	s.wake.invalidate()
}

// coverPNG 拿这张封面的 **PNG 字节**。
//
// # 为什么不能直接用缓存里那一份
//
// 缓存里存的是**解码之后**的 `*ui.Bitmap`（缩到最长边 768），而 `ui.Bitmap`
// 的像素**读不回来**（`mygo/ui/bitmap.go`：只有 Pixelated / Update / Size）。
// 所以要 PNG 就得从**原始字节**重新走一遍：解 → 编。
//
// ⚠️ **EXIF 旋转的那一张**：界面上摆正了（框架的 `ui.DecodeBitmap` 按 EXIF
// 摆正），而这里从原始字节重新解的时候也得**自己摆正**，否则复制出去的是一张
// 横躺着的图。摆正那一段在 `imgcache` 里（`SourceImage`：那边有 EXIF 的解析
// 和全部解码器）。
func (s *Shell) coverPNG(url string) ([]byte, error) {
	img, err := s.coverImageFor(url)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	// ⚠️ **编码失败必须报出来**（票面：「编码失败有可见反馈，不静默失败」）。
	// 而它**在生产上几乎不会发生**——所以正因如此，它**必须有测试**，否则
	// 这一句被删掉也没有任何东西会红（测试接缝见 `Shell.coverImage`）。
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// coverImageFor 拿这一张封面那张**能编码的图**。
//
// ⚠️ **默认那一档是 `s.images.SourceImage`**（原图 + EXIF 摆正，见
// `imgcache.SourceImage`）。
//
// ⚠️ **接缝只给测试用**（`Shell.coverImage`）：编码失败那一档在生产上几乎
// 不会发生，而正因如此它必须有测试——测试要造出一张**编码不了的图**，最直接
// 的办法就是在这里换掉「取图」这一下。
func (s *Shell) coverImageFor(url string) (image.Image, error) {
	if s.coverImage != nil {
		return s.coverImage(url)
	}
	if s.images == nil {
		return nil, errors.New("没有封面缓存")
	}
	return s.images.SourceImage(url)
}

// systemWriteClipboardText 是生产那一档：把文本放进系统剪贴板。
//
// ⚠️ **无头 Tester 里调它会 panic**（`mygo.Clipboard.WriteText` 里的
// `needsApp` 见到 `App.Run` 没跑过就直接 panic，见 `mygo/app.go:128`）——
// 所以界面上走接缝（`Shell.writeClipboardText` / `Shell.writeClipboardImage`），
// 测试把它换成记下内容的桩。
func systemWriteClipboardText(text string) error {
	mygo.Clipboard.WriteText(text)
	return nil
}

// systemWriteClipboardImage 是生产那一档：把 PNG 放进系统剪贴板。
//
// ⚠️ **`WriteImage` 自己会验一遍是不是 PNG**（`mygo/modules.go:184`），不是
// PNG 就返回错——所以这一层传上去的必须真的是 PNG（`coverPNG` 保证了）。
func systemWriteClipboardImage(data []byte) error {
	return mygo.Clipboard.WriteImage(data)
}

// ─────────────────────────────────────────────────────────────────────────────
// 升级到「一次复制同时给文本 + HTML + 图片」的时候要动的那一处
//
// 🔴 **首版只调 `WriteText` / `WriteImage`**（票面钉的）：一次复制给一样东西
// 的时候，两个 API 就够了。
//
// 而**将来**如果要用「一次复制同时带文本、HTML 和图片」（很多应用粘进富文本
// 编辑器时要三样都在），那就得换成 `mygo.Clipboard.Write` + `Flush`：
//
//	mygo.Clipboard.Write(transfer.New(
//		transfer.NewItem(
//			transfer.Bytes(transfer.PNG, pngBytes),
//			transfer.Bytes(transfer.Text, []byte(text)),
//		)))
//
// 那一档**不要现在做**：它要自己管一份带锁的「这趟复制的内容」（多 goroutine
// 同时复制时要排队），而这一版一个用户只会在一个地方按一下。
// ─────────────────────────────────────────────────────────────────────────────
