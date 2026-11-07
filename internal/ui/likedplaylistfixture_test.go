// 喜欢歌单绑定那一票的测试脚手架：一台会答「我创建的收藏夹」分页、
// 新建收藏夹和 deal 写接口的假 B 站，加上一个挂着它的界面。
//
// # 它要钉死的事
//
// 这一票测的是**用户看得见的流程**（见 likefixture_test.go 同一套原则）：
//
//   - 第一次点「喜欢」时弹「创建 / 选择已有 / 暂时不用」；
//   - 选定之后绑定那一格，后续喜欢直接进它；
//   - 设置里能更换绑定；
//   - 绑定的那一格不见了（B 站删了 / deal 回错）时退回原收藏夹选择；
//   - 播放条 / 满屏上不再出现赞和收藏那两枚键（连同它们的数字），但喜欢入口还在。
//
// 断言在各用例文件里；这里只放「假 B 站怎么答」和「怎么把界面造起来」。
package ui

import (
	"net/url"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// ---- 假 B 站上喜欢歌单那几路的「答案」与「记下来」 ----

// onCreatedList 换掉「我创建的收藏夹」那一次的回包。
//
// rows 的每一项是 created/list 里 `list[]` 的形状（id / title / attr /
// media_count / fav_state）。给 nil 就是「这个账号一个收藏夹都没有」。
func (f *biliFixture) onCreatedList(rows []map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createdList = rows
}

// failFolderAdd 让「新建收藏夹」回一个业务错误（0 = 恢复成功）。
func (f *biliFixture) failFolderAdd(code int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.folderAddErr = code
}

// failDeal 让「写收藏」回一个业务错误（0 = 恢复成功）。
//
// ⚠️ **它卡的是写接口**：「绑定的那一格不见了」造的就是它——喜欢那条路
// 看到 deal 回错才退回原收藏夹选择（绑定丢失回退）。
func (f *biliFixture) failDeal(code int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dealErr = code
}

// folderAddCalls 是「新建收藏夹」被叫了几次（每一次记得整份表单）。
func (f *biliFixture) folderAddCalls() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.folderAddLog...)
}

// dealCalls 是「写收藏」被叫了几次（每一次记得整份表单）。
//
// `add_media_ids` 那一格就是「收进了哪一格」——绑定歌单的断言靠它。
func (f *biliFixture) dealCalls() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.favoriteCalls...)
}

// newLikedBindShell 建一份正在放 B 站 mv 的界面，假 B 站已登录、已挂收藏夹。
//
// boundID 为 0 时设置里没有绑定（首次喜欢会弹「创建 / 选择已有 / 暂时不用」）。
// 非 0 时先把 `likedPlaylist` 那一格种进设置（「后续喜欢自动加入」那一档）。
//
// ⚠️ **设置是启动前种下的**：走「先写盘、再 MountSettings」那条生产路，
// 和真程序读设置一样（`withSettings`）。
func newLikedBindShell(t *testing.T, f *biliFixture, boundID int64) (*Shell, *mygoui.Tester) {
	t.Helper()
	st := app.DefaultSettings("")
	if boundID != 0 {
		st.LikedPlaylist = &app.LikedPlaylist{FolderID: boundID, Title: "绑定的那一格"}
	}
	b := newTestShell(t, at(likeWinW, likeWinH), withSettings(st))
	s, tt := b.s, b.tt
	s.app.MountLogin(app.NewMemoryStore("liked-bind"), f.client())
	s.app.MountStreaming(app.Streaming{
		Client: f.client(), Media: f.Client(), TempDir: t.TempDir(),
	})
	s.app.MountCollections(app.CollectionRefs{Client: f.client()})
	if err := s.app.Enqueue(likeItem()); err != nil {
		t.Fatalf("把在线的那首放进队列：%v", err)
	}
	// ⚠️ **等歌真的放起来**：`Enqueue` 把第一项放起来是异步的（`a.player`
	// 要等 goroutine 装上），`Snapshot().HasTrack`（= `player != nil`）要等
	// 那一趟回来才 true。播放条的封面、满屏的入口都拿 `HasTrack` 当门——
	// 不等它的话，用例拿到的第一帧封面还没画出来，满屏那一条「点开全屏」
	// 就找不到锚点。喜欢心摆出来靠 `Likeable()`（它看队列当前项，不看
	// `HasTrack`），那一档不用等；这里等的是**封面 / 满屏**这一头。
	waitFrames(t, tt, "歌放起来了", func() bool { return s.app.Snapshot().HasTrack })
	return s, tt
}
