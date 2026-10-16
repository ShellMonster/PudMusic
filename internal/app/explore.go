// 探索：UP 空间与动态流要用的那一摊 B 站接口。
//
// # 为什么是「探索」而不是给空间页单开一块
//
// UP 空间的头部、四个 Tab、动态流的作者栏和动态列表，用的是**同一个** B 站
// 客户端——就是 `client.go` 文件头那条「全项目一个」的约定。所以这里挂的
// 不是「空间页用的客户端」，是「全项目那一个」的另一个把手。
//
// 挂载关系记在包级的表里，而不是 App 的一个字段：App 的结构体写在 app.go
// 里，那个文件不归本文件改，而 Go 的方法能写在任意文件里、字段不能
// （`favorites.go` 里是同一个道理）。
//
// # 头部为什么值得有一份状态，而四个 Tab 没有
//
// 四个 Tab 的数据是「一拉一扔」的：给 `list.PagedList` 当 Fetcher，
// 结果由它自己收、自己叫醒界面（`paged.go` 的 deliver），所以这一层
// 不经手。
//
// 头部不一样：它**不在任何一个 PagedList 里**，而「关注按钮按下去之后
// 粉丝数要 +1」要求它是一份能被就地改的状态。所以它有 `UpSpace` 这一份，
// 自己管代次、自己管唤醒。
//
// # 代次挡的是「连点关注」
//
// `SetFollow` 是写接口，要 CSRF、要网络。用户在按钮上连点三下就是三个在途
// 的请求，回来时顺序不保证——不给代次的话，「已关注」可能被最后回来的那个
// 「取关」覆盖掉，而用户明明看到按钮变成了「已关注」。
package app

import (
	"context"
	"errors"
	"strconv"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// errExploreMissing 是「探索这一摊还没挂上」。
//
// 没挂上的表现是**每一页**都空，而原因在界面上一个字都看不出来。把它做成
// 一个能判的哨兵错误，是为了让「忘了调 MountExplore」这件事在测试里能
// 立刻红，而不是变成「列表一直是空的」那种要人肉找的 bug。
var errExploreMissing = errors.New("还没接上 B 站接口")

// MountExplore 装上探索页面用的那一个 B 站客户端。**在开窗口之前调**。
//
// 传进来的必须就是 `MountLogin` 返回的那一个（`ui.MountLogin` 的返回值）。
// 传第二个的话，登录态、请求头、WBI 密钥全都不是同一份了，而症状是
// 「登录成功了，一打开空间就 -101」。
func (a *App) MountExplore(cli *bili.Client) {
	mountedExplore.Store(a, cli)
}

// explore 返回那个客户端。没挂过是 nil（调用方返回 errExploreMissing）。
func (a *App) explore() *bili.Client {
	if v, ok := mountedExplore.Load(a); ok {
		return v.(*bili.Client)
	}
	return nil
}

// mountedExplore 是「这个 App 用的是哪个 B 站客户端」。
var mountedExplore sync.Map // *App → *bili.Client

// TestOnlyAdoptLogin 装一份登录态，**只给界面层的测试用**。
//
// 真机上的登录只有一条路（扫码，account.go 的 adopt）。测试要「登录了」
// 那个状态（关注按钮只在登录后才画），而走一遍扫码会让每个界面用例都
// 先摆一张二维码再轮询一次——测的不是被测的那件事。
//
// 之所以不是 `_test.go` 里的一个辅助：界面层的包外测试要用它，而那个包
// 看不到这里的未导出字段。名字里带 TestOnly 是为了 grep 一下就找得到
// （和 `TestOnlyPointHostsAtFixture` 一个理由）。
func (a *App) TestOnlyAdoptLogin(mid string) {
	if a.account == nil {
		return
	}
	a.account.mgr.Adopt(Session{
		SESSDATA: "test-sessdata", BiliJCT: "test-jct", DedeUserID: mid,
	})
}

// SelfMid 是当前登录账号的 mid，0 是没登录。
//
// 动态流左侧的作者栏列的是「**我**关注的人」，所以要的是自己的 mid，
// 不是页面上那个 UP 的（那个在路由参数里）。
func (a *App) SelfMid() int64 {
	id := a.Session().DedeUserID
	if id == "" {
		return 0
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// UP 空间的头部
// ─────────────────────────────────────────────────────────────────────────────

// SpaceHeader 是 UP 空间头部此刻的样子。界面一帧只读这一份拷贝。
type SpaceHeader struct {
	// Loaded 是「已经拿到过一次了」。false 时界面上画的是加载中/失败那两支，
	// 别拿 User.Name == "" 当「没加载」——他可能真的就叫空。
	Loaded  bool
	Loading bool
	// User 是昵称、头像、头图、等级。
	User User
	// Relation 是我和他的关系（有没有关注、是不是拉黑了）。
	Relation bili.Relation
	// Stat 是关注数和粉丝数。
	Stat bili.RelationStat
	// Err 是上一次加载的错。成功时是空串。
	Err string
}

// User 是 UP 主本人。B 站那份叫 `bili.User`，这里换个名是为了界面上读起来
// 是 `header.User.Name` 而不是 `header.User.User.Name`。
type User = bili.User

// UpSpace 是一个 UP 主空间的**头部状态**。按 mid 存着，切走再切回来是热的。
type UpSpace struct {
	app *App
	mid int64

	mu      sync.Mutex
	head    SpaceHeader
	gen     uint64
	running bool
}

// UpSpaceOf 是 mid 这个 UP 的头部状态。**第一次问的时候现建一个空的**：
// 「还没加载」是正常状态，不该让调用方判 nil。
func (a *App) UpSpaceOf(mid int64) *UpSpace {
	key := upSpaceKey{a, mid}
	if v, ok := mountedSpaces.Load(key); ok {
		return v.(*UpSpace)
	}
	s := &UpSpace{app: a, mid: mid}
	actual, _ := mountedSpaces.LoadOrStore(key, s)
	return actual.(*UpSpace)
}

// upSpaceKey 是头部状态的键。带上 App 是为了两个窗口/两次测试之间不串。
type upSpaceKey struct {
	app *App
	mid int64
}

// mountedSpaces 存着每个 UP 的头部状态。
var mountedSpaces sync.Map // upSpaceKey → *UpSpace

// Mid 是这个 UP 的 id。
func (s *UpSpace) Mid() int64 { return s.mid }

// Snapshot 是头部此刻的样子。界面每一帧调一次。
func (s *UpSpace) Snapshot() SpaceHeader {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.head
}

// Load 拉一次头部（昵称/头像/头图/等级 + 关注关系 + 粉丝数）。**立刻返回**。
//
// 三个请求是**并发**发的：串行的话最坏要等三个往返，而界面上是「正在加载」
// 转三个圈才画出来。并发的代价只是多两个在途请求——这三个都是只读的。
//
// ⚠️ 没登录时**不查关注关系**：那个接口要 Cookie，没登录会回 -101，而
// `-101` 在这一层和「网络断了」长得一模一样。界面上没登录本来就不画关注
// 按钮，为它多打一个注定失败的请求没有意义。
//
// 🔴 铁律三：后台投递完状态**必须叫醒界面**，只投递不唤醒 = 界面停在旧帧
// （实测 0 帧刷新）。
func (s *UpSpace) Load() {
	cli := s.app.explore()
	if cli == nil {
		s.fail(errExploreMissing)
		return
	}

	s.mu.Lock()
	if s.running {
		// 已经在拉了。**不排队**：连点三下就是三个在途请求，回来时顺序不保证，
		// 而三个结果说的是同一件事。
		s.mu.Unlock()
		return
	}
	s.running = true
	s.head.Loading, s.head.Err = true, ""
	gen := s.gen
	s.mu.Unlock()
	wake := s.app.wakeLocked()

	mid := s.mid
	go func() {
		ctx := context.Background()
		var (
			user     *bili.User
			relation bili.Relation
			stat     *bili.RelationStat
		)
		done := make(chan struct{}, 3)
		go func() { user, _ = cli.SpaceAcc(ctx, mid); done <- struct{}{} }()
		go func() { stat, _ = cli.RelationStatOf(ctx, mid); done <- struct{}{} }()
		if s.app.LoggedIn() {
			go func() { relation, _ = cli.SpaceRelation(ctx, mid); done <- struct{}{} }()
		} else {
			done <- struct{}{}
		}
		<-done
		<-done
		<-done

		s.mu.Lock()
		stale := gen != s.gen
		s.running = false
		if stale {
			// 换 UP 主之前的那个请求。它说什么都不作数。
			s.mu.Unlock()
			return
		}
		s.head.Loading = false
		switch {
		case user == nil && stat == nil:
			// 两个都要的都没拿到。这才是「加载失败」。
			s.head.Err = "这个 UP 主的空间读不出来，换一个或者检查网络"
		default:
			s.head.Loaded = true
			if user != nil {
				s.head.User = *user
			}
			if stat != nil {
				s.head.Stat = *stat
			}
			s.head.Relation = relation
		}
		s.mu.Unlock()

		if wake != nil {
			wake()
		}
	}()
}

// fail 记一次加载失败并叫醒界面。
func (s *UpSpace) fail(err error) {
	s.mu.Lock()
	s.head.Loading = false
	s.head.Err = err.Error()
	s.mu.Unlock()
	if wake := s.app.wakeLocked(); wake != nil {
		wake()
	}
}

// Follow 关注或取关，然后**就地**把关系改掉。
//
// ⚠️ **先改状态再发请求**：B 站的写接口要一个往返，等它回来才改的话按钮
// 会「按下去一点反应都没有」——而这一点反馈正好是用户在等的。
// 失败时回滚（回滚也只改这一份状态，不重新拉一遍）。
//
// 粉丝数**不在这里改**：粉丝数是 B 站那边算的，改它等于自己编一个数。
// 取关之后它会少 1，那是下一次进这个空间才看得见的。
func (s *UpSpace) Follow(ctx context.Context, follow bool) error {
	cli := s.app.explore()
	if cli == nil {
		return errExploreMissing
	}
	act := bili.ActionFollow
	if !follow {
		act = bili.ActionUnfollow
	}

	s.mu.Lock()
	was := s.head.Relation
	s.head.Relation = relationAfter(was, follow)
	s.mu.Unlock()

	err := cli.ModifyRelation(ctx, s.mid, act)
	if err != nil {
		s.mu.Lock()
		s.head.Relation = was // 回滚：按钮要变回原来那个样子
		s.mu.Unlock()
		if wake := s.app.wakeLocked(); wake != nil {
			wake()
		}
		return err
	}
	// 成功路径也要叫醒：上面的 `Follow` 已经把按钮的字改了，但界面是即时模式，
	// 不叫醒它就一直停在按下去之前那一帧（铁律三）。
	if wake := s.app.wakeLocked(); wake != nil {
		wake()
	}
	return nil
}

// relationAfter 是「按完这个按钮之后关系该是什么」。
//
// 关注之后是 6（互相关注）而不是 2 吗？——**不是**。我们不知道他有没有关注
// 我们（B 站的 be_relation 我们没查），所以老老实实写成 2（已关注）。
// 写成 6 的话按钮照样显示「已关注」（`Relation.Followed` 两个值都算），
// 而下次刷新时 B 站给回 6 也对得上——两种写法在界面上分不出差别，
// 写 2 是不把不知道的事编出来。
func relationAfter(was bili.Relation, follow bool) bili.Relation {
	if follow {
		if was == bili.RelationMutual {
			// 已经互关了，关注之后还是互关（而且我们确知）。
			return was
		}
		return bili.RelationFollowed
	}
	return bili.RelationNone
}

// ─────────────────────────────────────────────────────────────────────────────
// 四个 Tab 和动态流的取数
// ─────────────────────────────────────────────────────────────────────────────
//
// 这一组全是**同步**的：它们由 `list.PagedList` 的 Fetcher 在后台 goroutine
// 里调（`paged.go` 的 load/fetchOne），结果投递和叫醒界面那一套 PagedList
// 自己管。所以这里只做三件事：拿到客户端、转发、把「没挂上」变成能判的错。

// UpVideos 拉「投稿」Tab 的一页。
func (a *App) UpVideos(ctx context.Context, q bili.UpVideoQuery) (bili.UpVideoPage, error) {
	cli := a.explore()
	if cli == nil {
		return bili.UpVideoPage{}, errExploreMissing
	}
	return cli.SpaceVideos(ctx, q)
}

// UpCollections 拉「收藏夹」Tab。B  站那个接口一次给全，没有翻页。
func (a *App) UpCollections(ctx context.Context, mid int64) ([]bili.UpCollection, error) {
	cli := a.explore()
	if cli == nil {
		return nil, errExploreMissing
	}
	return cli.SpaceFavFolders(ctx, mid)
}

// UpSeries 拉「合集」Tab 的一页。
func (a *App) UpSeries(ctx context.Context, mid int64, page, size int) (bili.SpaceSeriesPage, error) {
	cli := a.explore()
	if cli == nil {
		return bili.SpaceSeriesPage{}, errExploreMissing
	}
	return cli.SpaceSeries(ctx, mid, page, size)
}

// UpDynamics 拉 UP 空间「动态」Tab 的一页。offset 空串是第一页。
func (a *App) UpDynamics(ctx context.Context, mid int64, offset string) (bili.DynamicPage, error) {
	cli := a.explore()
	if cli == nil {
		return bili.DynamicPage{}, errExploreMissing
	}
	return cli.SpaceDynamics(ctx, mid, offset)
}

// DynamicFeed 拉动态流（关注的人在发什么）的一页。
func (a *App) DynamicFeed(ctx context.Context, offset string) (bili.DynamicPage, error) {
	cli := a.explore()
	if cli == nil {
		return bili.DynamicPage{}, errExploreMissing
	}
	return cli.DynamicFeed(ctx, offset)
}

// FollowedAuthors 拉动态流左侧作者栏的一页。
func (a *App) FollowedAuthors(ctx context.Context, vmid int64, page int) (bili.FollowPage, error) {
	cli := a.explore()
	if cli == nil {
		return bili.FollowPage{}, errExploreMissing
	}
	return cli.Followings(ctx, vmid, page)
}
