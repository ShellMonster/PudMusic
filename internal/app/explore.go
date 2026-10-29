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
	"log"
	"strconv"
	"sync"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// errExploreMissing 是「探索这一摊还没挂上」。
//
// 没挂上的表现是**每一页**都空，而原因在界面上一个字都看不出来。把它做成
// 一个能判的哨兵错误，是为了让「忘了调 MountExplore」这件事在测试里能
// 立刻红，而不是变成「列表一直是空的」那种要人肉找的 bug。
// ⚠️ **这句是给用户看的**（列表拉不出来时它就是那一页的失败原因），不许说
// 「接口」（票 #242，UX 报告 §3-F 的 F2）。
var errExploreMissing = errors.New("这一页暂时加载不出来")

// MountExplore 装上探索页面用的那一个 B 站客户端。**在开窗口之前调**。
//
// 传进来的必须就是 `MountLogin` 返回的那一个（`ui.MountLogin` 的返回值）。
// 传第二个的话，登录态、请求头、WBI 密钥全都不是同一份了，而症状是
// 「登录成功了，一打开空间就 -101」。
func (a *App) MountExplore(cli *bili.Client) {
	a.mu.Lock()
	oldSpaces := a.spaces
	a.exploreClient = cli
	a.exploreGen++
	a.spaces = make(map[int64]*UpSpace)
	a.mu.Unlock()

	// The keyed spaces belong to the client mount. Invalidate them after dropping
	// the app lock so an in-flight response cannot repopulate state for the new
	// client or account.
	for _, s := range oldSpaces {
		s.invalidate()
	}
}

// ExploreGeneration identifies the current Explore client mount.
//
// UI-owned cached Explore state captures this value when it is created. A new
// mount means results from the old client or account must not be adopted into
// that cache, even if cancellation arrives after the result was queued.
func (a *App) ExploreGeneration() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.exploreGen
}

// explore 返回那个客户端。没挂过是 nil（调用方返回 errExploreMissing）。
func (a *App) explore() *bili.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.exploreClient
}

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

// AccountSnapshot captures the current account identity and generation once.
func (a *App) AccountSnapshot() AccountSnapshot {
	if a.account == nil {
		return AccountSnapshot{}
	}
	session, generation := a.account.mgr.Snapshot()
	return AccountSnapshot{
		SelfMid:          parseSelfMid(session.DedeUserID),
		Generation:       generation,
		ClientGeneration: a.ExploreGeneration(),
	}
}

// accountSnapshotCurrent reports whether a request still belongs to the current account.
func (a *App) accountSnapshotCurrent(snapshot AccountSnapshot) bool {
	return a.AccountSnapshot() == snapshot
}

// parseSelfMid converts a DedeUserID into the public self mid.
func parseSelfMid(id string) int64 {
	if id == "" {
		return 0
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// SelfMid 是当前登录账号的 mid，0 是没登录。
//
// 动态流左侧的作者栏列的是「**我**关注的人」，所以要的是自己的 mid，
// 不是页面上那个 UP 的（那个在路由参数里）。
func (a *App) SelfMid() int64 {
	return a.AccountSnapshot().SelfMid
}

// ─────────────────────────────────────────────────────────────────────────────
// UP 空间的头部
// ─────────────────────────────────────────────────────────────────────────────

// SpaceHeader is the UP-space header snapshot consumed by the UI.
type SpaceHeader = SpaceHeaderSnapshot

// User is the UP-space identity exposed to the UI.
type User = SpaceUser

// UpSpace is a single UP-space header state keyed by mid.
type UpSpace struct {
	app *App
	mid int64

	mu        sync.Mutex
	head      SpaceHeader
	gen       uint64
	followGen uint64
	running   bool
	closed    bool
}

// UpSpaceOf 是 mid 这个 UP 的头部状态。**第一次问的时候现建一个空的**：
// 「还没加载」是正常状态，不该让调用方判 nil。
func (a *App) UpSpaceOf(mid int64) *UpSpace {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.spaces == nil {
		a.spaces = make(map[int64]*UpSpace)
	}
	if s := a.spaces[mid]; s != nil {
		return s
	}
	s := &UpSpace{app: a, mid: mid}
	a.spaces[mid] = s
	return s
}

// Mid 是这个 UP 的 id。
func (s *UpSpace) Mid() int64 { return s.mid }

// Snapshot 是头部此刻的样子。界面每一帧调一次。
func (s *UpSpace) Snapshot() SpaceHeader {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.head
}

// invalidate closes this keyed state for the client mount that created it.
// In-flight work may still return, but it can no longer publish or wake.
func (s *UpSpace) invalidate() {
	s.mu.Lock()
	s.closed = true
	s.gen++
	s.running = false
	s.head = SpaceHeader{}
	s.mu.Unlock()
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

	// 要不要顺带查关注关系，**在这一刻定下来**（而不是在后台那个 goroutine
	// 里问 `a.LoggedIn()`）：中间可能已经退登了，而查出来的关系是按账号的。
	selfMid := s.app.SelfMid()
	askRelation := s.app.LoggedIn()
	// 关注数/粉丝数**只在登录后才查**：界面那一块也只在登录后才画
	// （`viewSpaceActions` 里 `if s.app.LoggedIn()`），没登录时查它是白花一次
	// 请求，而且会让 `NeedStat` 永远为真（见那里的注释）。
	askStat := selfMid != 0

	s.mu.Lock()
	if s.closed || s.running {
		// 已经在拉了。**不排队**：连点三下就是三个在途请求，回来时顺序不保证，
		// 而三个结果说的是同一件事。
		s.mu.Unlock()
		return
	}
	s.running = true
	s.gen++
	gen := s.gen
	s.head.Loading, s.head.Err = true, ""
	s.mu.Unlock()
	wake := s.app.wakeLocked()

	mid := s.mid
	go func() {
		ctx := context.Background()
		var (
			user        *SpaceUser
			stat        *RelationStats
			statErr     error
			relation    SpaceRelation
			relationOK  bool
			relationErr error
		)
		done := make(chan struct{}, 3)
		go func() {
			defer func() { done <- struct{}{} }()
			v, err := cli.SpaceAcc(ctx, mid)
			if err == nil {
				mapped := mapSpaceUser(v)
				user = &mapped
			}
		}()

		if askStat {
			go func() {
				defer func() { done <- struct{}{} }()
				v, err := cli.RelationStatOf(ctx, mid)
				statErr = err
				if err == nil {
					mapped := mapStats(v)
					stat = &mapped
				}

			}()
		} else {
			done <- struct{}{}
		}
		if askRelation {
			go func() {
				defer func() { done <- struct{}{} }()
				v, err := cli.SpaceRelation(ctx, mid)
				relationErr = err
				relation = mapRelation(v)
				relationOK = err == nil

			}()
		} else {
			done <- struct{}{}
		}
		<-done
		<-done
		<-done

		s.mu.Lock()
		if s.app.isClosed() {
			s.running = false
			s.mu.Unlock()
			return
		}
		stale := s.gen != gen || s.closed

		s.running = false
		if stale {
			// 已经有一次在路上跑着了。这一份不管。
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
			// 🔴 **关注关系查失败要说出来，不能把零值当「没关注」**。
			//
			// 吞掉的话界面上会是一个**能按的**「关注」按钮，而它其实没查到
			// 任何东西——用户按下去是一次取关（B 站对没关注过的人「取关」
			// 是成功的，于是他更确信自己关注过）。
			// 🔴 **关注数/粉丝数查失败也要说出来，不能把零值当「0 个粉丝」**。
			//
			// 吞掉的话界面上是一个干干净净的「粉丝数 0」——那是个**假数**，
			// 用户会相信它（而真实值可能是十几万）。这是这一条最难受的地方：
			// 它看起来完全正常。
			s.head.statKnown = false
			if statErr == nil && stat != nil {
				s.head.statKnown = true
				s.head.Stat = *stat
			}
			if statErr != nil {
				s.head.statErr = statErr.Error()
			} else {
				s.head.statErr = ""
			}

			s.head.relationFor, s.head.relationKnown = 0, false
			if askRelation {
				s.head.relationFor = selfMid
				s.head.relationKnown = relationOK
			}
			if relationErr != nil {
				s.head.relationErr = relationErr.Error()
			} else {
				s.head.relationErr = ""
			}
			s.head.Relation = relation
		}
		s.mu.Unlock()

		if wake != nil {
			wake()
		}
	}()
}

// RetryRelation 清掉「关注关系 / 关注数没查到」，好让下一次 Load 又去问一次。
//
// 两样一起清：它们来自**不同的接口**，而界面上是同一个「重试」按钮
// （`SpaceHeader.AnyPartFailed`）——只清一样的话，另一个还得再点一次。
//
// 只有**人点「重试」**才调它：查失败之后不自动重试（见 NeedRelation 的
// 防抖那条），而这一页也没有别的入口能让用户重来一次。
func (s *UpSpace) RetryRelation() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.head.relationErr = ""
	s.head.statErr = ""
	s.mu.Unlock()
	if wake := s.app.wakeLocked(); wake != nil {
		wake()
	}
}

// fail 记一次加载失败并叫醒界面。
func (s *UpSpace) fail(err error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
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
	return s.FollowForAccount(ctx, s.app.AccountSnapshot(), follow)
}

// FollowForAccount applies the optimistic follow change for one immutable account snapshot.
func (s *UpSpace) FollowForAccount(ctx context.Context, snapshot AccountSnapshot, follow bool) error {
	cli, err := s.app.exploreAt(snapshot)
	if err != nil {
		return err
	}
	act := bili.ActionFollow
	if !follow {
		act = bili.ActionUnfollow
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errExploreMissing
	}
	was := s.head.Relation
	s.head.Relation = relationAfter(was, follow)
	// 按过之后关系是**确定**的，哪怕刚才那次查询是拿另一个账号做的。
	s.head.relationFor, s.head.relationKnown = snapshot.SelfMid, true
	s.head.relationErr = ""
	s.followGen++
	followGen := s.followGen
	s.mu.Unlock()

	err = cli.ModifyRelation(ctx, s.mid, act)
	s.mu.Lock()
	staleAccount := !s.app.accountSnapshotCurrent(snapshot)
	staleOperation := s.followGen != followGen || s.closed
	if staleAccount {
		// 这个空间按 mid 缓存，但关系按账号缓存。旧账号的乐观状态不能留给新账号。
		s.head.Relation = RelationNone
		s.head.relationFor, s.head.relationKnown = 0, false
		s.head.relationErr = ""
	}
	if err != nil && !staleAccount && !staleOperation {
		s.head.Relation = was // 回滚：按钮要变回原来那个样子
	}
	s.mu.Unlock()

	if staleAccount || staleOperation {
		if staleAccount {
			if wake := s.app.wakeLocked(); wake != nil {
				wake()
			}
		}
		return ErrStaleGeneration
	}
	if err != nil {
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

// selfMidOf 是当前登录账号的 mid，0 是没登录。
func selfMidOf(a *App) int64 { return a.SelfMid() }

// relationAfter 是「按完这个按钮之后关系该是什么」。
//
// 关注之后是 6（互相关注）而不是 2 吗？——**不是**。我们不知道他有没有关注
// 我们（B 站的 be_relation 我们没查），所以老老实实写成 2（已关注）。
// 写成 6 的话按钮照样显示「已关注」（`Relation.Followed` 两个值都算），
// 而下次刷新时 B 站给回 6 也对得上——两种写法在界面上分不出差别，
// 写 2 是不把不知道的事编出来。
func relationAfter(was SpaceRelation, follow bool) SpaceRelation {
	if follow {
		if was == RelationMutual {
			return was
		}
		return RelationFollowed
	}
	return RelationNone
}

// ─────────────────────────────────────────────────────────────────────────────
// 四个 Tab 和动态流的取数
// ─────────────────────────────────────────────────────────────────────────────
//
// 这一组全是**同步**的：它们由 `list.PagedList` 的 Fetcher 在后台 goroutine
// 里调（`paged.go` 的 load/fetchOne），结果投递和叫醒界面那一套 PagedList
// 自己管。所以这里只做三件事：拿到客户端、转发、把「没挂上」变成能判的错。

func (a *App) UpVideos(ctx context.Context, q SpaceVideoQuery) (SpaceVideoPage, error) {
	cli := a.explore()
	if cli == nil {
		return SpaceVideoPage{}, errExploreMissing
	}
	pg, err := cli.SpaceVideos(ctx, bili.UpVideoQuery{Mid: q.Mid, Page: q.Page, Size: q.Size, Keyword: q.Keyword, Order: q.Order})
	if err != nil {
		return SpaceVideoPage{}, err
	}
	out := SpaceVideoPage{Total: pg.Total, HasMore: pg.HasMore, Videos: make([]SpaceVideo, 0, len(pg.Videos))}
	for _, v := range pg.Videos {
		out.Videos = append(out.Videos, mapSpaceVideo(v))
	}
	return out, nil
}

func (a *App) UpCollections(ctx context.Context, mid int64, page int) (SpaceCollectionPage, error) {
	cli := a.explore()
	if cli == nil {
		return SpaceCollectionPage{}, errExploreMissing
	}
	pg, err := cli.SpaceFavFolders(ctx, mid, page, 0)
	if err != nil {
		return SpaceCollectionPage{}, err
	}
	out := SpaceCollectionPage{Count: pg.Count, HasMore: pg.HasMore, Items: make([]SpaceCollection, 0, len(pg.Folders))}
	for _, v := range pg.Folders {
		out.Items = append(out.Items, mapCollection(v))
	}
	return out, nil
}

func (a *App) UpSeries(ctx context.Context, mid int64, page, size int) (SpaceCollectionPage, error) {
	cli := a.explore()
	if cli == nil {
		return SpaceCollectionPage{}, errExploreMissing
	}
	pg, err := cli.SpaceSeries(ctx, mid, page, size)
	if err != nil {
		return SpaceCollectionPage{}, err
	}
	out := SpaceCollectionPage{HasMore: pg.HasMore, Items: make([]SpaceCollection, 0, len(pg.Collections))}
	for _, v := range pg.Collections {
		out.Items = append(out.Items, mapCollection(v))
	}
	return out, nil
}

func (a *App) UpDynamics(ctx context.Context, mid int64, offset string) (DynamicPage, error) {
	cli := a.explore()
	if cli == nil {
		return DynamicPage{}, errExploreMissing
	}
	pg, err := cli.SpaceDynamics(ctx, mid, offset)
	if err != nil {
		return DynamicPage{}, err
	}
	return mapDynamicPage(pg), nil
}

func (a *App) exploreAt(snapshot AccountSnapshot) (*bili.Client, error) {
	a.mu.Lock()
	cli, clientGeneration := a.exploreClient, a.exploreGen
	a.mu.Unlock()
	if cli == nil {
		return nil, errExploreMissing
	}
	if !a.accountSnapshotCurrent(snapshot) || clientGeneration != snapshot.ClientGeneration {
		return nil, ErrStaleGeneration
	}
	return cli, nil
}

func (a *App) dynamicFeedForAccount(ctx context.Context, snapshot AccountSnapshot, offset string) (DynamicPage, error) {
	cli, err := a.exploreAt(snapshot)
	if err != nil {
		return DynamicPage{}, err
	}
	pg, err := cli.DynamicFeed(ctx, offset)
	if err != nil {
		return DynamicPage{}, err
	}
	if !a.accountSnapshotCurrent(snapshot) {
		return DynamicPage{}, ErrStaleGeneration
	}
	return mapDynamicPage(pg), nil
}

func (a *App) followedAuthorsForAccount(ctx context.Context, snapshot AccountSnapshot, page int) (FollowAuthorsPage, error) {
	cli, err := a.exploreAt(snapshot)
	if err != nil {
		return FollowAuthorsPage{}, err
	}
	pg, err := cli.Followings(ctx, snapshot.SelfMid, page)
	if err != nil {
		return FollowAuthorsPage{}, err
	}
	if !a.accountSnapshotCurrent(snapshot) {
		return FollowAuthorsPage{}, ErrStaleGeneration
	}
	out := FollowAuthorsPage{Total: pg.Total, HasMore: pg.HasMore, Authors: make([]FollowAuthor, 0, len(pg.Authors))}
	for _, v := range pg.Authors {
		out.Authors = append(out.Authors, FollowAuthor{Mid: v.Mid, Name: v.Name, Face: v.Face})
	}
	return out, nil
}

func (a *App) DynamicFeed(ctx context.Context, offset string) (DynamicPage, error) {
	return a.dynamicFeedForAccount(ctx, a.AccountSnapshot(), offset)
}

func (a *App) DynamicFeedForAccount(ctx context.Context, snapshot AccountSnapshot, offset string) (DynamicPage, error) {
	return a.dynamicFeedForAccount(ctx, snapshot, offset)
}

func (a *App) FollowedAuthorsForAccount(ctx context.Context, snapshot AccountSnapshot, page int) (FollowAuthorsPage, error) {
	return a.followedAuthorsForAccount(ctx, snapshot, page)
}

func (a *App) FollowedAuthors(ctx context.Context, vmid int64, page int) (FollowAuthorsPage, error) {
	snapshot := a.AccountSnapshot()
	snapshot.SelfMid = vmid
	return a.followedAuthorsForAccount(ctx, snapshot, page)
}

// LikeDynamic 给一条动态点赞 / 取消点赞。**立刻返回**，结果走 done。
//
// 🔴 **没有登录态就立刻说，不发请求**：B 站对写接口回 -101，而界面上那一下
// 是乐观的（先点亮再发），所以等它失败再告诉用户「没登录」的话，赞已经先亮
// 过一下又灭回去了——两次跳变。
func (a *App) LikeDynamic(dynID string, like bool, done func(error)) {
	if a.explore() == nil {
		sayThumbDone(done, errExploreMissing)
		return
	}
	go func() {
		err := a.explore().LikeDynamic(context.Background(), dynID, like)
		if err != nil {
			log.Printf("biu: 给动态 %s 点赞失败：%v", dynID, err)
		}
		sayThumbDone(done, err)
	}()
}

// sayThumbDone 是那个结果回调。done 是 nil 的调用方什么都不用做。
func sayThumbDone(done func(error), err error) {
	if done != nil {
		done(err)
	}
}
