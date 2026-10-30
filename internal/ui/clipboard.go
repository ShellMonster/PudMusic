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
//   - 播放条左边那一列的宽度是被**钉死的**（`playbarLeftMinW` = 272，票 #573
//     之后），加一枚键就要动那个数，而那个数一动，窄窗口下歌名的宽度跟着变
//     （那一处有 `TestTheLeftBlockIsThreeSegments` 和 `playbar_layout_test.go`
//     钉着）。
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

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/title"
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
// # 怎么挡：**判据和写放进同一个主线程段**
//
// 主线程是**单线程事件循环** ⇒ 同一时刻只有一段在跑 ⇒ **两次写天然串行**。
// 所以只要「比号」和「写」在**同一个主线程回调**里，过时的那一份就**根本
// 不会落地**——而不是落地了再改回来。
//
// **写之前**比一比号（`isLatest`）：**不是最新的一趟什么都不写**——剪贴板里
// **压根不出现**过时的东西（这是大多数情形：旧封面还在下网络，文本早就写完了）。
//
// # 为什么前一版的「写之前比 + 写之后收敛」不够
//
// 前一版是「写完了再比一次，发现盖掉了更新的就**写回去**」——而**那个修法
// 自己造出了一个新的窗口**：盖掉的那一刻已经过去了，而剪贴板是**一个全局的
// 格子**，别的应用可能正好在读它。于是用户粘出来的东西**在那一瞬间是错的**，
// 而那一瞬间正是「收敛写回去」之前。
//
// ⚠️ **所以那一整套删掉了**（`committed` 记账 + 收敛循环 + 收敛失败那句
// 提示）：它造出的那个窗口（写进去 → 盖掉 → 再写回去，中间那一瞬间剪贴板里
// 是过时的那一份）正是「判据和写放进同一段」要消除的东西。有了同一段，
// **过时的那一份压根不会落地**——于是没有「要收敛回去」的东西，记账和循环
// 一起没了。
//
// # 为什么「握着锁写」也走不通
//
// 框架那一档要走主线程并**等**它（`mygo.Clipboard.Write` → `clipboardOnMain`
// → `onMain`），而主线程可能正在按「复制歌曲信息」并**等同一把锁**——那就是
// **死锁**。所以这里**不握着锁写**，而是**进主线程段**。
//
// ⚠️ **在主线程上调 `WriteImage` 不会死锁**：框架那一档走
// `mygo.Clipboard.Write` → `clipboardOnMain` → `onMain`，而 `onMain` 在
// **主线程上直接执行** `fn`（`mygo/loop.go:94` 的 `isMainThread()` 短路，
// 不排队也不阻塞）。
//
// ⚠️ **文本那一档也要占号**：文本是同步写的（不等网络），但它**照样要作废
// 前面那一趟封面**——否则「先按封面（慢）、再按文本」最后留着的还是那张图。
// 而「先按文本（快）、再按封面（慢）」是对的：封面是**最后**按的那一个，
// 它跑完之后写进去，正是用户要的。
type copyClipboard struct {
	mu sync.Mutex
	// gen 是用户到这一刻为止一共按过几次复制。
	gen uint64
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

// onMain 是「**在主线程上调**主线程」的那一档：**直接执行**。
//
// ⚠️ **默认是生产那一档**（`mygo.RunOnMain`）：它在主线程上**直接执行**
// （`mygo/loop.go:94` 的 `isMainThread()` 短路，不排队也不阻塞）。
// 测试把它换成**一个假主线程**（见 `shelltest_test.go` 的 `mainThreadStub`）。
func (s *Shell) onMain(fn func()) {
	run := s.runOnMain
	if run == nil {
		run = systemRunOnMain
	}
	run(fn)
}

// onMainBackground 是「**从后台**调主线程」的那一档：**排队**，并等它跑完。
//
// ⚠️ **它和 `onMain` 的差别就是 F1 那个竞态的机制**：框架的 `onMain` 在
// **主线程上直接执行** `fn`（`mygo/loop.go:94` 的 `isMainThread()` 短路），
// 而从后台调它会**投递到主线程的队列里**——那一档**还在排队**，没执行。
//
// 于是这一种交错是真的：
//
//	10:00:00  封面那一趟（后台）把段**投进队列**——还在排队
//	10:00:01  用户按「复制歌曲信息」——**主线程档**，绕过队列直接执行
//	          ⇒ 文本**先**落地
//	10:00:02  主线程排空队列 ⇒ 旧封面的段执行
//
// ⚠️ **所以判据必须在那段里**：如果它在**投递之前**就判了（旧封面的号那时
// 还是最新的），它执行的时候会把文本**覆盖掉**。而在段里判，那一段执行时
// 文本已经占过号了——旧封面**压根不写**（见 `commitLater`）。
func (s *Shell) onMainBackground(fn func()) {
	run := s.runOnMainBackground
	if run == nil {
		run = systemRunOnMain
	}
	run(fn)
}

// systemRunOnMain 是生产那一档：把 fn 放到主线程上执行。
//
// ⚠️ **主线程上直接执行**（`mygo/loop.go:94` 的 `isMainThread()` 短路）——
// 所以在主线程回调里调它**不会死锁**，而那正是「判据 + 写」能在同一个
// 主线程段里做完的前提。
func systemRunOnMain(fn func()) { mygo.RunOnMain(fn) }

// commitNow 是「这一趟现在就要写」：**取号 + 写**都在**同一个主线程段**里。
//
// ⚠️ **为什么取号也在这一段里**：号记的是「用户按复制的次序」，而**按复制本身
// 就是主线程上的事**（菜单回调跑在主线程上）。取号和写既然都在同一段里，那
// 一段跑着的期间**不可能**插进另一次复制——于是「用户最后按的那一份」一定
// 是剪贴板里最后那一份。
//
// ⚠️ **这一段里没有「判据」**：判据是「我是不是最新的一趟」，而**这一段跑着的
// 期间没人能取新号**（取号也在这类段里）——所以这一段跑完时它**必然**还是
// 最新的。判据只对**号早就取好了**的那一趟有意义（封面那一趟，见
// `commitLater`）。
//
// ⚠️ **在主线程上调 `WriteText` 不会死锁**：框架那一档走
// `mygo.Clipboard.Write` → `clipboardOnMain` → `onMain`，而 `onMain` 在
// **主线程上直接执行** `fn`（`mygo/loop.go:94` 的 `isMainThread()` 短路，
// 不排队也不阻塞）。
func (s *Shell) commitNow(write func() error) error {
	var err error
	s.onMain(func() {
		// ⚠️ **号在这一段里取**：这样「取号」和「写」之间**没有缝隙**。
		copyClip.next()
		err = write()
	})
	return err
}

// commitLater 是「这一趟的号早就取好了，现在才轮到它写」（封面那一趟：号在
// 用户按下去那一刻就取了，而写要等后台把图编完）。
//
// # 判据和写在**同一个主线程段**里
//
// 主线程是**单线程事件循环** ⇒ 同一时刻只有一段在跑 ⇒ 两次写天然串行。
// 于是「这一趟还是不是最新的」这个判据和「写」之间**没有缝隙**——过时的那
// 一份**根本不会落地**（而不是落地了再改回来）。
//
// ⚠️ **这一段跑在主线程上**：而主线程是单线程的，所以这一段跑着的期间，
// **用户按不了任何菜单**（菜单回调也要主线程）——于是「比号」到「写」
// 之间**不可能**插进一次新的复制动作。
func (s *Shell) commitLater(gen uint64, write func() error) {
	// ⚠️ **判据和写必须在一起**：在后台先比一次只是**省事的预检**（省掉一次
	// 主线程往返），**权威判据在主线程段里**。
	//
	// ⚠️ **走后台那一档**（排队 + 等）：这一段是**从后台**发起的（封面那一趟
	// 编完 PNG 才走到这里），而「从后台调主线程」和「在主线程上调主线程」
	// **不是一回事**（见 `onMainBackground` 的注释）——那差别正是那个竞态的
	// 机制。
	s.onMainBackground(func() {
		// ⚠️ **权威判据在这里**：不是最新的一趟**什么都不写**——剪贴板里压根
		// 不出现过时的内容（这是大多数情形：旧封面还在下网络，而文本早就写
		// 完了）。
		if !copyClip.isLatest(gen) {
			// ⚠️ **作废的那一趟什么也不说**：用户后来按了别的，那一档会自己
			// 说。而**这一趟也结束了**（测试要靠这个信号知道「它没写成」）。
			s.copyDoneLater(gen)
			return
		}
		// ⚠️ **写和「说」在同一个段里**：剪贴板里是什么，屏幕上就该说
		// 什么——而这两句必须**同时**落地，否则用户会看到一句和剪贴板对不
		// 上的话（比如剪贴板里已经是文字了，屏幕上还在说「封面已复制」）。
		if err := write(); err != nil {
			s.addToast("复制不了封面："+err.Error(), notice.Danger)
			s.wake.invalidate()
			s.copyDoneLater(gen)
			return
		}
		s.addToast("封面已复制", notice.Success)
		s.wake.invalidate()
		s.copyDoneLater(gen)
	})
}

// copyDoneLater 是那个「某一趟后台复制结束了」测试钩子。
//
// ⚠️ **它在主线程段里调**：测试要靠它知道「先发起的那一趟跑完了」——而那一
// 趟**可能什么都没写**（被后来的动作作废了），所以**光看剪贴板里的内容是
// 等不到它结束的**。
func (s *Shell) copyDoneLater(gen uint64) {
	if s.copyDone != nil {
		s.copyDone(gen)
	}
}

// claim 在**主线程段里**取一个新号。
//
// ⚠️ **封面那一趟的号是在「用户按下去那一刻」取的**：那一趟的写要等后台把
// 图编完（几百毫秒），而号必须**那时候**就发出去——否则「先按封面（慢）、
// 再按文本（快）」这一趟的号会被后来的文本占掉，于是它跑完发现自己不是最新
// 的（那是对的），但反过来「先按文本、再按封面」要是占晚了，就会把**用户最后
// 按的那一次**作废掉。
//
// ⚠️ **取号也在主线程段里**：按复制本身就是主线程上的事，所以号和「用户真的
// 按了」的先后得对得上。
func (s *Shell) claim() uint64 {
	var gen uint64
	s.onMain(func() { gen = copyClip.next() })
	return gen
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

// copyText 把一段文字放进剪贴板，**并算一次「复制」**（取号作废在途的封面那一趟）。
//
// 🔴 **票 #685 新加的那几条复制入口必须走这里，不能直接调
// `s.wake.writeClipboard`**：`copyClipboard` 的号是为了「先按封面（慢，后台编
// PNG）、再按别的复制（快）」不会反过来被旧封面盖掉（见那个类型的注释）。
// 跳过 `commitNow` 的那条路**不取号**——于是它写完以后，在途的封面那一趟问
// `isLatest(gen)` 仍然拿到 true，它会照写，剪贴板最后是**旧封面**。对抗性审查
// 实测过这个洞（列表 ⌘C 和抽屉「复制歌名」两条都在）。
//
// ⚠️ **取号和写在同一段里**（`commitNow`）：理由同 `copySongInfo`——「比过了」
// 不等于「写进去的时候仍然是这一档」。
//
// ⚠️ **说话由调用方管**（它把错误带回去）：每个入口成功那句话说得不一样
// （列表 / 抽屉说「已复制歌名」，菜单说「已复制歌曲信息」），而失败那句话也
// 该点名是哪一项——这一层只说「没写成，原因是这个」。
//
// ⚠️ **两个出口按有没有接缝分流**：`Shell.writeClipboardText` 是测试的桩
// （`copySongInfo` 那一档也用它）；没装的时候走 `s.wake.writeClipboard`——那
// 是界面层拿到的**窗口**剪贴板接缝，生产上（`Shell.Attach` 之后）和 Tester 里
// 都有。**不能在这里退回 `systemWriteClipboardText`**：它要真的 App 事件循环，
// 无头 Tester 里那一句会挂住（不是 panic，是等一个永远不来的主线程回调）。
func (s *Shell) copyText(text string) error {
	write := s.writeClipboardText
	err := s.commitNow(func() error {
		if write != nil {
			return write(text)
		}
		s.wake.writeClipboard(text)
		return nil
	})
	s.wake.invalidate()
	return err
}

// copySongInfo 把这一首的信息放进剪贴板。
//
// ⚠️ **失败了要说得出来**（票面要求「不静默失败」）：剪贴板写失败就弹一句
// 提示，而不是什么都不做。
func (s *Shell) copySongInfo(st app.State) {
	text := songInfoText(st)
	if text == "" {
		s.addToast("这一首没有可复制的信息", notice.Warning)
		return
	}
	write := s.writeClipboardText
	if write == nil {
		write = systemWriteClipboardText
	}
	// ⚠️ **取号 + 写 + 说在同一个主线程段里**（`commitNow`）：这一档是同步
	// 的，但**写剪贴板本身要走到主线程**（框架那一档），而用户在那段时间里
	// 按了「复制封面」是可能的——「比过了」不等于「写进去的时候仍然是这一档」
	// （见 `copyClipboard` 的注释）。
	//
	// ⚠️ **这一档照样要作废前面那一趟封面**（「先按封面（慢）、再按文本」最后
	// 留着的必须是那段文字）——而那正是**取号**干的事：取号就是作废前面所有
	// 的号。
	//
	// ⚠️ **不走 `copyText`**：这一档把「说」也放进同一段里（剪贴板里是什么，
	// 屏幕上就该同时说什么），而 `copyText` 把说话留给调用方、在外面说。
	s.commitNow(func() error {
		if err := write(text); err != nil {
			s.addToast("复制不了歌曲信息："+err.Error(), notice.Danger)
			s.wake.invalidate()
			return err
		}
		// ⚠️ **「说」和「写」在同一段里**：剪贴板里是什么，屏幕上就该说什么。
		s.addToast("歌曲信息已复制", notice.Success)
		s.wake.invalidate()
		return nil
	})
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
	//
	// ⚠️ **取号在主线程段里**（`claim`）：按复制本身就是主线程上的事，而
	// 「取号」和「写」要在同一段里才没有缝隙（见 `copyClipboard`）。
	gen := s.claim()
	url := st.CoverURL
	go s.copyCoverInBackground(url, gen)
}

// copyCoverInBackground 是「复制封面」的后台那一半。
//
// ⚠️ **gen 是这一趟自己的号**：跑完时要比一比它还是不是最新的（见
// `copyClipboard`）。不是最新就**什么都不写**——用户后来按的那一次才是
// 他要的。
func (s *Shell) copyCoverInBackground(url string, gen uint64) {
	data, err := s.coverPNG(url)
	if err != nil {
		// ⚠️ **判和说也在同一个主线程段里**：过时的失败不用吵用户——用户
		// 后来已经按了别的，他要的是那一个的结果。而「这一趟结束了」那个
		// 信号也在同一段里（见 `copyDoneLater` 的注释）。
		s.failLater(gen, "复制不了封面："+err.Error())
		return
	}
	write := s.writeClipboardImage
	if write == nil {
		write = systemWriteClipboardImage
	}
	// ⚠️ **走 `commitLater`**：这一段是整趟里最贵的一步（编一张 PNG），而它在
	// 后台跑着——用户完全可能在它跑的时候又按了一次。
	// 而「比过了号」**不等于**「写进去的时候仍然是这一档」：中间还夹着一次
	// 真正的写（要走主线程，可能慢），而那一次里用户可能又按了文本——
	// 那样的话旧封面会盖掉那段文字（见 `copyClipboard` 的注释）。
	//
	// ⚠️ **`commitLater` 自己进主线程**：判据和写在**同一个主线程段**里。旧
	// 的封面那一趟进到那一段的时候，如果用户已经按过文本了，它**压根不写**
	// ——剪贴板里**不会**先出现那张过时的图再被改回来。
	s.commitLater(gen, func() error { return write(data) })
}

// failLater 说「这一趟失败了」——**判据和说在同一个主线程段里**。
//
// ⚠️ **为什么失败也要进主线程段**：号是在主线程段里取的，而「这一趟是不是
// 最新的」这个判据**必须和取号那一段互斥**——否则判完到开口说话之间可能
// 插进一次新的复制，于是用户已经按了文本，却还收到一句「复制不了封面」。
// 那是**过时的话**，而它是给用户看的最后一句。
//
// ⚠️ **而这一段里没有写剪贴板**：失败的时候**什么都不写**（写进去的是半个
// 东西，或者干脆没有）——所以「失败」这一档不会留下一个坏掉的剪贴板。
func (s *Shell) failLater(gen uint64, msg string) {
	s.onMainBackground(func() {
		if copyClip.isLatest(gen) {
			s.addToast(msg, notice.Danger)
			s.wake.invalidate()
		}
		// ⚠️ **这一趟也结束了**：测试要靠这个信号知道「它没写成」（见
		// `copyDoneLater` 的注释）。
		s.copyDoneLater(gen)
	})
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
