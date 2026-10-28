// 本地音乐目录的文件监听：把 `app.DirWatch` 那个接口接到 MyGo 的
// `WatchFiles` 上。
//
// 单独一个文件，因为它 import MyGo 而 `internal/app` 不 import（那一层
// 不认识界面）。而且它**必须**住在界面层：`mygo.WatchFiles` 在应用还没
// ready 的时候调会 panic（框架的 `needsApp`，`mygo/app.go:128`）。
//
// # 为什么监听的是 v0.5.2 之后那套 API
//
// 框架在 v0.5.2 把文件监听大改过（重命名靠**文件身份**配对、新增
// `FileWatchOptions.HintsOnly`）。本项目现在是 v0.6.3，所以下面两件事都不是
// 「顺便加上」，而是配对的：
//
//   - `HintsOnly: true`——**这里正是它该用的地方**。框架文档原话是
//     「never use it where what changed matters, such as following a
//     document's file」。本地音乐不是那种场景：收到通知之后我们**整个目录
//     重扫一遍**，不靠事件说出「哪个文件、怎么变了」。所以「只知道有东西
//     变了」是够的，而省下的正是最贵的那部分——
//
//     ⚠️ 一个几千文件的音乐目录，用默认监听要给**每个文件**建一份快照
//     （它靠文件身份配对重命名），那是一次几千次 stat 加几 MB 常驻内存。
//     `HintsOnly` 不留快照、不 stat 任何文件，只报 `FileHint`；而
//     `MaxEntries` 在这个模式下数的是**目录**，一层的目录通常个位数。
//
//   - `Recursive` 保持 false——和扫描「只扫一层」是同一件事。
//     递归监听的收益是能收到子目录的变化，而扫描根本不进子目录，
//     那些事件到了也只会触发一次没必要的全表扫描。
//
// # 监听停了就是停了
//
// 框架在**被盯的目录本身**没了（外接盘拔了）时报一个 `FileRemove{"."}`，
// 然后 `Next` 返回 `io.EOF`，这个 watch 就结束了。我们不在那儿自动重开：
// 用户把盘插回来之后，在这一页点一下「重新扫描」就行，而一个
// 「每 200ms 试着重开一次」的后台 goroutine 比这个更糟。
//
// # stop 为什么要掐 context
//
// `app.DirWatch` 只给 `stop func()`，没有 context 可言。但那条读事件的
// goroutine 正**阻塞**在 `Next` 上，光关 watch 不会把它叫醒（框架的
// `Next` 只在有事件或 watch 自己结束时才返回）。所以 stop 里除了
// `w.Close()` 还要 `cancel()`——那个 context 是这个 watch 私有的，
// 掐它等于说「这条 goroutine 可以走了」。

package ui

import (
	"context"
	"io"
	"log"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo"
)

// watchDebounce 是同一批改动合并成一次重扫的等待时间。
//
// 框架自己也有 Debounce（它等系统那边的变化停下来），这一层是**再叠一层**：
// 用户把一整个专辑拖进目录时，系统那边报的是几十个文件的事件，
// 框架会分几批报出来。不叠这一层就是每批一次全表扫描。
//
// 500ms 是权衡出来的：用户拷完一个专辑停下来，看到这一页更新的时间；
// 再长就觉得「没反应」，再短就拦不住连着几批的事件。
const watchDebounce = 500 * time.Millisecond

// mygoDirWatch 是 `app.DirWatch` 的框架实现。
type mygoDirWatch struct{}

// Watch 盯一个目录。实现了 `app.DirWatch`。
//
// 返回的 stop 幂等，而且**一定会**让那条读事件的 goroutine 结束。
func (mygoDirWatch) Watch(ctx context.Context, dir string, onChange func()) (func(), error) {
	// 这个 watch 私有的 context。stop 掐它，父 context（App.Close 走那条）
	// 取消时它也跟着取消。
	life, cancel := context.WithCancel(ctx)

	w, err := mygo.WatchFiles(life, dir, mygo.FileWatchOptions{
		// 见文件头：这一页要的只是「有东西变了」。
		HintsOnly: true,
		// 保持 false：扫描只扫一层，递归监听到的事件用不上。
		Recursive: false,
		Debounce:  watchDebounce,
		// HintsOnly 下这个上限数的是**目录**，一层目录个位数就够。
		MaxEntries: 64,
	})
	if err != nil {
		cancel()
		return nil, err
	}

	go func() {
		defer w.Close()
		for {
			events, err := w.Next(life)
			if err != nil {
				// io.EOF 是「目录没了、这个 watch 正常结束」，不是故障。
				// 别的错才值得记——而且只在不是我们主动停掉的时候记。
				if err != io.EOF && life.Err() == nil {
					log.Printf("biu: 盯本地音乐目录 %s 时停了：%v", dir, err)
				}
				return
			}
			if len(events) == 0 {
				continue
			}
			// ⚠️ onChange 在**这一条 goroutine** 上调，而它会起一遍后台全表
			// 扫描。自己那层的防重入（app 里的 flying/again）挡得住重复扫描，
			// 但这条循环不能被它阻塞——而 onChange 恰好很快：它只置两个标志
			// 然后起一个 goroutine。
			onChange()
		}
	}()

	return func() {
		// 顺序：先掐 context 让 goroutine 醒过来退出，再关 watch。
		// 反过来的话 goroutine 会在一个已经关掉的 watch 上多等一轮事件。
		cancel()
		w.Close()
	}, nil
}

// 编译期确认这一摊确实实现了 app 那个接口。
var _ app.DirWatch = mygoDirWatch{}
