// 下载完一首歌之后，在它旁边存一份封面图。
//
// # 为什么要单独存
//
// 规格里点名了这一条（#91 的用户故事 6）：以后做本地音乐库时封面已经在那儿了。
// 不存的话那一页永远只能去问 B 站要封面，而那个地址是会失效的——用户下了半年的
// 歌，本地那一页就半张封面都画不出来。
//
// # 🔴 它**不许挡在「完成」前面**，也不许无限期地拉
//
// 这一步是**装饰**：歌已经在 `Dest` 上了，几百 KB 的图下不下来，歌照样是好的。
// 而它原本挂在 `run` 的末尾（状态落成 `StatusDone` 之前），于是：
//
//	图床接了连接不回包 → 那一行永远停在「下载中 100%」
//	                 → 而 `loop` 是一次下一首、下完才下一首（`queue.go` 的 `loop`）
//	                 → **后面排着的歌一个字节都下不了**
//
// 而那个挂住是**不会自己解开的**：`Options.Client` 走的是
// `app.CredentialTransport.Client(nil)`，那个 `http.Client` 没有设 `Timeout`，
// `DefaultTransport` 的 `ResponseHeaderTimeout` 也是 0。只有用户自己按暂停 / 退出
// 才解得开——而那意味着「一批歌卡住了，用户能做的只有逐个按暂停」。
//
// 所以两样都要：**后台存**（`startCoverSave` 立刻返回）+ **自己带超时**
// （`coverFetchTimeout`）。前者解决「挡着队列」，后者解决「那条线自己永远不回来」。
//
// # 文件名怎么来的
//
// **从落盘的音频名推出来**（`CoverPathFor`），不是从歌名重新算一遍。理由是
// 「文件名跟音频对得上」这句话本身：`歌名.m4a` 旁边躺着 `歌名.jpg`，一眼就知道
// 那是它的封面。重新算一遍的话，同名避让（`歌名 (1).m4a`，见 `DestFor`）会让
// 两边算出**两个不同的名字**——而用户看到的是两个文件、一首歌。
//
// # 🔴 写盘要原子，和队列文件同一条约定
//
// `store.go` 顶上把「写到一半断电的话 JSON 读不出来 → 整个队列丢」当成本仓队列文件
// 必须走「临时文件 → fsync → rename」的理由。封面是同一件东西，只是丢了的代价小
// 一点（小图 vs 整个队列）——**而它坏起来的样子更隐蔽**：直接写最终名，写到一半
// 断电 / 磁盘满会在盘上留一份半张图，而 `exists()` 只看「有没有」，于是之后**每一次
// 重试都被挡掉**，那一首从此没有封面，界面上那一行还写着「下载完成」。
//
// 所以走「同目录的临时文件 → rename」，并把临时名固定成一个短前缀（`coverTmpPrefix`）：
// 用户歌名的词干已经被 `DestFor` 顶到 255 字节的上限了（它只给 `.part` 留了 5 字节），
// 而 `歌名.jpg.tmp-1234567890` 要 19 字节——那会 `file name too long`。
//
// # 视频那一路不存
//
// 这一份是给本地音乐库准备的，而音乐库读的是歌。视频那一路落的是交织出来的
// `.mp4`，B 站那个地址是**稿件的封面**（和那一帧画面不是一回事），存下来是
// 一张骗人的图——本地播放器会拿它当视频的第一帧。
//
// # 失败只记日志
//
// 封面拿不到**不许**让这一首算成失败，而界面也看不到这一条：它落在状态**之后**，
// 这一行早就写着「下载完成」了（而那是对的——歌真的下完了）。

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
	"time"
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
// ⚠️ 上限和 `imgcache` 的那个是两个独立的旋钮，别合并：一个是界面内存缓存，
// 一个是用户的下载目录，而这一份**是要留很久的**。
const maxCoverBytes = 8 << 20

// coverFetchTimeout 是取一张图的上限。
//
// ⚠️ **必须有**，因为 `Options.Client` 没有 `Timeout`（见文件头）：一台接了连接
// 不回包的图床会让那条线永远挂着。15 秒照的是 `imgcache` 建默认客户端时用的那一个
// ——同一件事（等一台图片服务器回话）在同一个仓库里就该是同一个数。
const coverFetchTimeout = 15 * time.Second

// coverTmpPrefix 是落盘时那个临时文件名前缀。
//
// ⚠️ **不带上歌名**：词干已经被 `DestFor` 顶到 255 字节的上限了（它只给 `.part`
// 留了 5 字节预算），而 `歌名.jpg.tmp-XXXXXXXXXX` 要 19 字节——那会
// `file name too long`，于是这一首连封面都存不下来。
const coverTmpPrefix = ".biu-cover-"

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

// startCoverSave 起一条线去存这一项的封面，**立刻返回**。
//
// 🔴 **它不挡队列**（理由见文件头）。所以界面上「下载完成」会**比**封面落地早那么
// 一小会儿——而那是对的：歌已经在盘上了，封面是装饰。反过来（拿一张图去押着整条
// 队列）是把最贵的那件事交给最不要紧的那件事。
//
// ⚠️ **「已经有一份就不存」在起那条线之前判**，而且是**拿着锁判的 `coverFor`、
// 放开锁再判的 `hasCover`**：后者要 `stat` 盘，挂在 `m.mu` 下面的话，界面每读
// 一次队列快照都得等这一下 `stat`。
//
// 这里不会有「两条线都看到还没有于是都去写」：`startCoverSave` 只有 `loop` 会调，
// 而 `loop` 是串行的。
func (m *Manager) startCoverSave(id string) {
	cover, dest, ok := m.coverFor(id)
	if !ok || cover == "" {
		return
	}
	if hasCover(dest) {
		return
	}

	// 🔴 `Add` 和 `Close` 里的 `Wait` **不会撞上**：只有 `loop` 会走到这里，而
	// `Close` 是**先等 `loop` 退出**（`m.wg.Wait()`）**之后**才等这一组
	// （见 `Close` 里那句 `m.coverWG.Wait()`）。所以这个 WaitGroup 不需要另一把锁。
	m.coverWG.Add(1)
	go func() {
		defer m.coverWG.Done()
		if err := m.saveCover(cover, dest); err != nil {
			logCoverErr(id, err)
		}
	}()
}

// coverFor 拿这一项要存封面的那两样。**必须在锁下调。**
//
// 视频那一路报 `ok == false`（见文件头「视频那一路不存」）。没有封面地址的
// 也一样——从本地扫进来的那些行根本没有图。关掉了的也不起那条线：它等一下就
// 会被 `Close` 等，而那时候 `m.ctx` 已经取消，它只会白跑一趟。
func (m *Manager) coverFor(id string) (cover, dest string, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tasks[id]
	if t == nil || t.Video || m.closed {
		return "", "", false
	}
	return t.Cover, t.Dest, true
}

// hasCover 报这个落盘位置上是不是**已经有一份能用的封面**。
//
// ⚠️ 三个条件缺一不可，少一个的后果是「这一首从此没有封面，而界面上是下完的」：
//
//	在            连得上、不是空串（`os.Stat` 成功）
//	是**普通文件**  一个**目录**在那个名字上，`os.Stat` 同样成功——而
//	              「目录已经在那儿了」不等于「封面已经在那儿了」。写也写不进去
//	              （rename 到一个目录上会失败），所以只当它不存在，让那一步报错，
//	              而不是把这一个目录当成现成的封面交出去
//	非空          0 字节的文件是**写失败**留下的（磁盘满、断电）。它不是一张图，
//	              而拿它当「已经有了」的话，之后每一次重试都被挡掉
//
// ⚠️ 不做「这到底是不是一张图」的校验：`imgcache` 那边也不做（它把字节直接交给
// `ui.DecodeBitmap` 认格式），而在这里引一个解码器只为了「存在性判断」不值当。
// 真正保证「不会是半张图」的是下面的**原子写**。
func hasCover(dest string) bool {
	path := CoverPathFor(dest)
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular() && st.Size() > 0
}

// saveCover 把那张图取下来存到 `dest` 旁边。
//
// 返回的错误只用来**记日志**（调用方一律不上交，见文件头）：歌已经下完了。
func (m *Manager) saveCover(cover, dest string) error {
	path := CoverPathFor(dest)
	if path == "" {
		return nil
	}
	// 这一次和 `startCoverSave` 里那次隔了一会儿，位置可能已经被人占了
	// （用户自己放了张封面进去）。再判一次，省掉一次几十 KB 的往返。
	if hasCover(dest) {
		return nil
	}

	// 🔴 **自己带超时**：父 ctx 是 `m.ctx`，所以退出应用会立刻把它掐掉，而一台
	// 接了连接不回包的图床由 `coverFetchTimeout` 兜底（见文件头）。
	ctx, cancel := context.WithTimeout(m.ctx, coverFetchTimeout)
	defer cancel()
	data, err := m.fetchCover(ctx, cover)
	if err != nil {
		return err
	}
	return writeCoverAtomically(path, data)
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

// writeCoverAtomically 写一份封面：同目录的临时文件 → rename。
//
// 🔴 **rename 是那一刻的提交点**（理由见文件头）：在它之前崩溃 / 磁盘满，盘上只有
// 一个 `.biu-cover-*` 的临时文件，没有「半张图」；在它之后，那要么是一张完整的
// 封面，要么什么都没有。直接写最终名的话，坏掉的那一种是「一份半张图 + 之后
// 每一次重试都被 `hasCover` 挡掉」。
func writeCoverAtomically(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), coverTmpPrefix)
	if err != nil {
		return fmt.Errorf("download: 建 %s 的临时文件：%w", path, err)
	}
	tmp := f.Name()
	// ⚠️ 失败的那几条**都要把临时文件删掉**：留在用户的下载目录里的话，
	// 每失败一次多一个垃圾文件，而它对用户没有任何意义。
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()

	if _, err = f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("download: 写封面 %s：%w", tmp, err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("download: 关封面 %s：%w", tmp, err)
	}
	// ⚠️ rename 到一个**目录**上会失败（ENOTEMPTY / EISDIR / ENOTDIR）——而
	// `hasCover` 已经把那种位置判成「没有封面」了，所以这里的失败说明那个名字
	// 在这两行之间被谁抢成了目录。报出来，不覆盖它。
	if err = os.Rename(tmp, path); err != nil {
		return fmt.Errorf("download: 落封面 %s：%w", path, err)
	}
	return nil
}

// logCoverErr 记一句「封面没存下来」。抽出来是为了让那一行日志的措辞只有一处。
func logCoverErr(id string, err error) {
	log.Printf("download: %s 的封面没存下来（歌已经下完，不影响这一首）：%v", id, err)
}
