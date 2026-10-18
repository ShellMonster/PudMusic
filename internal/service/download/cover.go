// 下载完一首歌之后，在它旁边存一份封面图。
//
// # 为什么要单独存
//
// 规格里点名了这一条（#91 的用户故事 6）：以后做本地音乐库时封面已经在那儿了。
// 不存的话那一页永远只能去问 B 站要封面，而那个地址是会失效的——用户下了半年的
// 歌，本地那一页就半张封面都画不出来。
//
// # 文件名怎么来的
//
// **从落盘的音频名推出来**（`CoverPathFor`），不是从歌名重新算一遍。理由是
// 「文件名跟音频对得上」这句话本身：`歌名.m4a` 旁边躺着 `歌名.jpg`，一眼就知道
// 那是它的封面。重新算一遍的话，同名避让（`歌名 (1).m4a`，见 `DestFor`）会让
// 两边算出**两个不同的名字**——而用户看到的是两个文件、一首歌。
//
// # 视频那一路不存
//
// 这一份是给本地音乐库准备的，而音乐库读的是歌。视频那一路落的是交织出来的
// `.mp4`，B 站那个地址是**稿件的封面**（和那一帧画面不是一回事），存下来是
// 一张骗人的图——本地播放器会拿它当视频的第一帧。
//
// # 失败只记日志
//
// 封面拿不到**不许**让这一首算成失败：歌已经在 `Dest` 上了，把它标成「出错」
// 是拿一整首歌去换一个几百 KB 的图，而界面上那一行的红字只有这一件事可说。

package download

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// coverExt 是封面落盘的后缀。
//
// ⚠️ **固定 `.jpg`，不跟地址上的后缀走**。B 站的图床给回来的地址一会儿
// `.jpg` 一会儿 `.png`，而「跟音频对得上」这条要求的是用户能猜到那个名字：
// 音频是 `.flac` 还是 `.m4a` 随音质变，封面不该跟着变——同一个歌名下次下出
// 一个 `.flac`，用户找的仍然是同一个 `歌名.jpg`。
//
// 正文不是 JPEG 也不打紧：这一份是给「以后扫本地目录建库」用的，那一步读图
// 按内容嗅格式（`ui.DecodeBitmap` 五种格式都认），不靠后缀。
const coverExt = ".jpg"

// maxCoverBytes 是一张封面能有多大。B 站的图床给的最大也就几百 KB，
// 超了说明拿到的不是图（多半是错误页），写下去只会占地方。
//
// ⚠️ 上限和 `imgcache` 的那个是两独立的旋钮，别合并：一个是界面内存缓存，
// 一个是用户的下载目录，而这一份**是要留很久的**。
const maxCoverBytes = 8 << 20

// CoverPathFor 算一首歌的封面存在哪儿。
//
// 音频是 `/下载/歌名.m4a` → 封面是 `/下载/歌名.jpg`。
//
// ⚠️ 用的是 `filepath.Ext(dest)` 而不是 `Task.Ext`：落盘名可能在入队之后
// 又改过（`prepare` 会按取流地址上的真后缀重算，`destToLand` 还会在落盘前
// 因为重名让一步），而这里拿到的是**盘上那个**名字——名字对不上就等于封面
// 存到了另一个地方，而那个地方没有歌。
//
// dest 是空串时返回空串（那一项没有落盘位置，封面也就无处可放）。
func CoverPathFor(dest string) string {
	if dest == "" {
		return ""
	}
	return strings.TrimSuffix(dest, filepath.Ext(dest)) + coverExt
}

// saveCover 把这一项的封面存到它那首歌旁边。
//
// 返回的错误只用来**记日志**（调用方一律不上交，见文件头）：歌已经下完了。
func (m *Manager) saveCover(ctx context.Context, id string) error {
	cover, dest, ok := m.coverOf(id)
	if !ok || cover == "" {
		return nil
	}
	path := CoverPathFor(dest)
	if path == "" {
		return nil
	}
	// 已经有了一份就不重下。绝大多数时候它是上一次重试时存下的（内容一样），
	// 而这一发是几十 KB 的网络——为一个已有的文件花一次往返不值。
	if exists(path) {
		return nil
	}
	data, err := m.fetchCover(ctx, cover)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("download: 存封面 %s：%w", path, err)
	}
	return nil
}

// coverOf 拿这一项要存封面的那两样。**必须在锁下调。**
//
// 视频那一路报 `ok == false`（见文件头「视频那一路不存」）。没有封面地址的
// 也一样——歌单是从本地扫进来的那些行根本没有图。
func (m *Manager) coverOf(id string) (cover, dest string, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tasks[id]
	if t == nil || t.Video {
		return "", "", false
	}
	return t.Cover, t.Dest, true
}

// fetchCover 把那张图取下来。
//
// ⚠️ **不自己设 Referer**：`Options.Client` 必须是
// `app.CredentialTransport.Client(nil)`，而 Referer 是那一层每次都带上的
// （B 站的图床不给 Referer 会拒，见 `Options.Client` 的注释）。在这里再补一个
// 的话，两个地方各有一份，而哪一份真的生效要看调用方递的是哪个客户端——
// 症状是「真机上封面全下不来，测试里全绿」。
func (m *Manager) fetchCover(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("download: 发封面请求：%w", err)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: 拉封面：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("download: 图床回了 HTTP %d", resp.StatusCode)
	}

	// ⚠️ 读 `maxCoverBytes+1` 而不是 `maxCoverBytes`，然后**多读到了就报错**：
	// 照着上限截下来的是一张**半张**图，而它的字节数是正常的——写下去之后
	// 本地那一页画出来的是一片花的，而没有任何一处说过「这张图是坏的」。
	// 超了本身就是「拿到的不是图」的证据，报错比存一张坏图好。
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCoverBytes+1))
	if err != nil {
		return nil, fmt.Errorf("download: 读封面：%w", err)
	}
	if len(data) > maxCoverBytes {
		return nil, fmt.Errorf("download: 封面超过 %d 字节，拿到的不是图", maxCoverBytes)
	}
	return data, nil
}

// logCoverErr 记一句「封面没存下来」。抽出来是为了让那一行日志的措辞只有一处。
func logCoverErr(id string, err error) {
	log.Printf("download: %s 的封面没存下来（歌已经下完，不影响这一首）：%v", id, err)
}
