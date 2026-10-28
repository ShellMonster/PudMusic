package bili

// 这一组钉住「**所有头像走同一个规范化入口**」（票 #634）。
//
// # 为什么这件事值一条测试
//
// 同一个 UP 主的脸，关注列表里是「补协议 + 拼 160×160 裁剪参数」的那一个地址，
// 而空间页 / 动态流 / 收藏夹的创建者那一行是「只补协议」的那一个地址——**同一个
// 地址、同一张脸，但缓存的键是两个不同的串**：
//
//   - 两个缓存条目、两次下载、两份落盘；
//   - 而「带了裁剪参数」的那一档**永远拿不到「按显示尺寸要图」的那一份**
//     （参数已经拼在地址里了，图片缓存那一层没法再拼）——所以关注卡那一格明明
//     只要 128 像素，却固定下一张 160×160。
//
// 所以服务层**不再自己拼裁剪参数**——裁剪参数由图片缓存按显示尺寸统一决定。
//
// # ⚠️ 「统一」指的是「统一到同一条机制」，不是「统一成同一个尺寸」
//
// 头像的显示尺寸有 128（关注卡）、112（空间页）、40（动态流）、40（导航栏）各
// 不同，所以「按显示尺寸」那一档**会因地而异**。这一层管的只是**地址**。
//
// # ⚠️ 「已经带裁剪参数的地址被尊重」
//
// 别人（服务层、图床自己）裁好的那一档不该被我们改掉——再拼一层 `@` 那是坏地址
// （图床回 400）。所以**已经带 `@` 的地址原样返回**。

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// TestEveryAvatarComesFromTheSameNormalization 钉住「**所有头像走同一个
// 规范化入口**」。
//
// 判据是**逐字相同**：同一个 UP 主的那张脸，关注列表、空间页、动态流、收藏夹
// 创建者、我自己的头像——五处**一个字都不差**。
//
// # 为什么这条是「源头」
//
// 服务层各自拼各自的裁剪参数，是「两个缓存条目」这件事的**源头**——图片缓存
// 那一层拿到什么地址就存什么地址，它没法猜「这个地址上带的参数是谁拼的」。
// 所以这一层不统一，下面那一层再怎么改也还是两个条目。
func TestEveryAvatarComesFromTheSameNormalization(t *testing.T) {
	const raw = "//i0.hdslb.com/bfs/face/11.jpg"
	want := "https://i0.hdslb.com/bfs/face/11.jpg"

	// ① 关注列表那一族（`avatarURL`：原来是补协议 + 拼裁剪参数）。
	if got := avatarURL(raw); got != want {
		t.Errorf("关注列表的头像地址是 %q，want %q\n"+
			"（这一档带了裁剪参数，于是它**永远拿不到**「按显示尺寸要图」的那一份——"+
			"同一个 UP 主在关注页和空间页上是两个不同的缓存条目、两次下载）",
			got, want)
	}

	// ② 空间页 / 动态流 / 收藏夹创建者那一族（`normalizeImageURL`）。
	if got := normalizeImageURL(raw); got != want {
		t.Errorf("空间页那一族的头像地址是 %q，want %q", got, want)
	}

	// ③ 两条入口必须**逐字相同**——这是这一条真正要拦的东西。
	if got, g2 := avatarURL(raw), normalizeImageURL(raw); got != g2 {
		t.Errorf("两条入口给出的地址不一样：%q vs %q——"+
			"「统一」要的是同一个串，不只是「都能取到」", got, g2)
	}
}

// TestAvatarAddressesCarryNoCropParams 钉住「**服务层不再自己拼裁剪参数**」。
//
// 判据是**地址里没有 `@`**——B 站图床的裁剪参数一律由图片缓存按显示尺寸决定。
//
// ⚠️ **别顺手把 `_1c` 去掉**：头像要正方形，所以头像那一族要**中心裁剪**。
// 但那个参数是**图片缓存那一层**加的（它按显示尺寸决定要不要中心裁剪），
// 不在这一层——这一层只给一个**干净的**地址。
func TestAvatarAddressesCarryNoCropParams(t *testing.T) {
	for _, got := range []string{
		avatarURL("//i0.hdslb.com/bfs/face/11.jpg"),
		normalizeImageURL("//i0.hdslb.com/bfs/face/11.jpg"),
	} {
		if strings.Contains(got, "@") {
			t.Errorf("服务层给出的头像地址带裁剪参数：%q——"+
				"裁剪参数由图片缓存按显示尺寸统一决定（带了它就拿不到那一档）", got)
		}
	}
}

// TestAnAvatarThatAlreadyHasCropParamsIsLeftAlone 钉住「**已经带裁剪参数的
// 地址被尊重**」。
//
// 别人裁好的那一档不该被我们改掉——再拼一层 `@` 那是坏地址（图床回 400）。
func TestAnAvatarThatAlreadyHasCropParamsIsLeftAlone(t *testing.T) {
	const already = "https://i0.hdslb.com/bfs/face/11.jpg@160w_160h_1c_1s.webp"
	if got := avatarURL(already); got != already {
		t.Errorf("已经带裁剪参数的地址被改成了 %q，want %q（原样返回）", got, already)
	}
	if got := normalizeImageURL(already); got != already {
		t.Errorf("已经带裁剪参数的地址被改成了 %q，want %q（原样返回）", got, already)
	}
}

// TestTheOwnerFaceIsTheSameOneAsEverywhereElse 钉住「**收藏夹创建者的头像
// 和别处是同一张脸**」。
//
// 收藏夹那一族走的是 `normalizeImageURL`（本来就没有裁剪参数），而关注列表
// 走的是 `avatarURL`（原来有）——**这两条入口现在必须给出同一个串**。
func TestTheOwnerFaceIsTheSameOneAsEverywhereElse(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathFolderInfo, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"id": 12345, "attr": 2, "title": "通勤路上", "cover": "",
			"intro": "早上听", "media_count": 1, "fav_state": 0,
			"upper": map[string]any{
				"mid": 11, "name": "UP 小明",
				"face": "//i0.hdslb.com/bfs/face/11.jpg",
			},
		}))
	})
	c := f.client(t, Options{})

	folder, err := c.CollectionInfo(context.Background(), 12345, CollectionFavorite)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://i0.hdslb.com/bfs/face/11.jpg"
	if got := folder.Owner.Face; got != want {
		t.Errorf("收藏夹创建者的头像地址是 %q，want %q", got, want)
	}
}
