# 音频栈

> 原文在 `AGENTS.md` §5《不可协商的项目不变量》里的两条、`README.md`《已知边界 →
> 音频格式》《支持哪些格式》《在线音质》。

**什么时候用得上**：你要动 `internal/service/audio/`（解码、播放、均衡）、改
`internal/service/bili/quality.go`（音质档位）、或者在排查「某个格式放不了」。

---

## 支持哪些格式

**出处**：`README.md`《支持哪些格式》

认容器的是**头几个字节**，不是扩展名——本地文件和 B 站在线流走的是同一个
`openDecoder`（`internal/service/audio/player.go`）。

| 容器 | 谁在解 | 备注 |
|---|---|---|
| 裸 FLAC（`.flac`） | `internal/service/audio/flacdec` | 自带解码器，已逐样本验证 |
| MP4 家族（`.m4a` / `.m4s` / `.mp4`） | `internal/service/audio/extaf`（macOS）/ `internal/service/audio/mfadec`（Windows，⚠️ **还没在真 Windows 上验过**） | macOS 走 AudioToolbox；**Windows 上 AAC 接的是 Media Foundation**（`mfadec`）。⚠️ **MP4 里的 FLAC 在 macOS 上也走 AudioToolbox，在 Windows 上落回 `flacdec`** |
| 裸 ADTS（`.aac`） | 同上 | 没有容器，头两字节就是同步字。⚠️ **Windows 上接的是 Media Foundation**（同样**还没在真 Windows 上验过**） |
| MP3 | `internal/service/audio/mp3dec` | 本仓库自己解（vendor 进来的 `tphakala/go-mp3`，MIT），两个平台都能解。补了 **MPEG-2.5**、**LAME gapless**（编码器延迟被裁掉，歌和下一首接得上），打开一个文件也不再全文件扫帧头 |
| Ogg Vorbis（`.ogg`） | `mygo/audio/vorbis` | MyGo 自带，纯 Go，两个平台都能解。⚠️ 那一行 blank import 在 `internal/service/audio/player.go`——**少了它 `.ogg` 在本地音乐里列得出来、点进去报「不支持这个音频格式」** |
| AIFF / AIFC | `internal/service/audio/aiff.go` | 本项目写的，**只认未压缩的 PCM**（MAC3/IMA4 等压缩编码解不了） |
| WAV | `mygo/audio` | MyGo 自带 |

注册表认不出来、而头几个字节是 `fLaC` 的，落回 `flacdec`——那是**本地裸 `.flac` 文件**
的路子（MyGo 的表是对字面量做匹配的，给变长的 magic 注册不了）。

### ⚠️ B 站在线的无损流不是裸 FLAC

实测（2026-10-23，issue #252）它的头 16 字节是
`00 00 00 20 66 74 79 70 69 73 6f 35 00 00 00 01 69 73 6f 35`——`ftyp` 盒，也就是
**MP4 里的 FLAC**（素材与形状说明见 `internal/service/bili/testdata/lossless/`）。

所以在线的无损流走的是上表里「MP4 家族」那一行，**不是** `flacdec` 的裸 FLAC 那一条。

### 这个平台没有 AudioToolbox 时（Windows）

先问 **Media Foundation**（`internal/service/audio/mfadec`）——它解 AAC（MP4 家族和裸
ADTS 两种容器）；它认不出来（比如 MP4 里装的是 FLAC）才落回 `flacdec`——`flacdec` 能
解 MP4 里的 FLAC，于是 B 站无损流在 Windows 上照样放得出来。

**Windows 上 MP3 / WAV / FLAC / OGG / AIFF 和 MP4 里的 FLAC 能用**（不依赖 macOS 的
系统解码器）。

⚠️ **Windows 上还有一个代价**：Media Foundation 那条路靠 `go-m4a` 拆分片 MP4，而
`go-m4a` 要先把每一个 `moof` 走一遍——`moof` 在文件末尾，所以**在线**的流要等整个
文件下完才开口（本地文件不用）。macOS 上那条路走 AudioToolbox，是按需读的，边下边播。

**什么时候用得上**：你在加一种容器 / 编解码器、或者在查「本地某个文件放不了」。

---

## FLAC 解码器

**出处**：`AGENTS.md` §5《不可协商的项目不变量》FLAC 那条（+ `README.md`《支持哪些格式》末段）

**FLAC 解码器是逐样本验证过的资产，别碰它的解码路径。**

改之前先读 `internal/service/audio/flacdec` 自己的测试注释——那里记着**为什么每个判据
是那个数**。

它同时覆盖两种容器（包注释里写着）：

- 裸 `.flac` 流（`fLaC` 魔数 + 元数据块 + 帧）——用户本地音乐库大多是这种；
- ISO-BMFF 里的 FLAC（`fLaC` 采样项 + `dfLa` FLACSpecificBox）——**B 站的 FLAC 流是
  这种**，由 `go-m4a` 解复用。

搬进来时只改了 import 路径，代码逐字未动；仓库里带着它的 64 项测试和测试素材
（`internal/service/audio/flacdec/`）。

**什么时候用得上**：你看到 `flacdec` 里有「看起来可以简化」的地方，或者要在它旁边
加一个新解码器——**加可以，别动它**。

---

## macOS 那条路：AudioToolbox

**出处**：`AGENTS.md` §5《不可协商的项目不变量》macOS 音频那条

**动 macOS 那条音频路径之前，先读 `internal/service/audio/extaf/` 的注释。**

纯 Go 绑 AudioToolbox，坑全在「**哪个参数是哪个四字符码、哪一版签名不一样**」这一层，
踩过一次就写在那儿了。

要点：

- 它解的是 AudioToolbox 能解的一切压缩格式（AAC / ALAC / MP4 里的 FLAC / AIFF……），
  **我们不自己实现任何一个编解码器**。
- **不走 `ExtAudioFileOpenURL`**：它只接受有文件系统支撑的 `file://` URL，而且实测要求
  文件完整到 **98.3%** 才肯打开。网络流要「边下边播」，走的是
  `AudioFileOpenWithCallbacks` + `ExtAudioFileWrapAudioFileID`。
- `ErrUnsupported` 说的是「**这个平台没有 AudioToolbox**」，不是「没有系统解码器」——
  Windows 上**有**系统解码器（Media Foundation），只是不走这一层。这句话是用户看得见的
  （会进 `openDecoder` 的最终错误里），说成「没有系统解码器」就是一句假话。

**什么时候用得上**：你在改 `extaf/`、或者要在 macOS 上接一种新的系统能解的格式。

---

## 在线音质怎么换档

**出处**：`README.md`《在线音质》（+ `internal/service/bili/quality.go` 的注释）

B 站在线流分两档路：

| 档 | 字段 | 说明 |
|---|---|---|
| **Hi-Res 无损** | `dash.flac`（codecs `fLaC`） | 要大会员 |
| **有损 AAC** | `dash.audio[]` | 最高 192K（id `30280`） |

- 用户选的那一档在 `internal/service/bili/quality.go` 里翻译成 B 站的档位 id。
- 播放条上歌名右边的**徽章点一下就能换档**，换完立刻按新档**重新取流、从原来的
  播放位置接着播**（不需要切歌，也不需要重启）。
- ⚠️ 无损那一路在两个平台都能播；有损那一路**在 Windows 上接上了**（Media Foundation），
  但**还没在真 Windows 上验过**。

### 编号上有过两次错

- **`30251` 不是「杜比 Hi-Res」**，它是**投稿侧的无损流**（`dash.flac`），和杜比没有
  关系。以前叫 `QualityDolbyHiRes`，挂错标签会让无损判定跟着判错（票 #255）。
- **投稿侧没有 320K**：320K 是**音乐库**的 `type=2`（`MusicTier320`），那是**第三套
  编号**。投稿侧最高的有损档是 192K（`30280`）。
- 判据是**档位**不是编码名：投稿侧的无损只有 Hi-Res 一档；音乐库里的 `3` 是 FLAC——
  **两个 3 说的不是同一件事**（投稿侧的 `30250` 才是杜比全景声）。

### 「用户要什么」和「B 站给了什么」是两句话

`AudioQuality`（用户选的偏好：自动 / 无损 / 高音质 / 标准 / 省流量）和 `Quality`
（B 站给的档位）是两套类型，各有自己的 `Label`。**降级发生之后界面上要同时出现两句
时，混起来会看成「用户要无损，系统给了无损」而看不出降级。**

### 「自动」和「无损」的区别

上游 Biu 的 auto 行为是 `quality === "auto" || quality === "lossless"` 都先找 flac。
两者的唯一区别是：**auto 找不到无损不会报错，lossless 会**（用户为无损付了钱，静默降级
比报错更糟）。

**什么时候用得上**：你在改音质档位、换档 UI、或者在排查「换了档位没生效 / 挂错标签」。

---

## 解不了 / 没验过的几处

**出处**：`README.md`《已知边界 → 音频格式》

这一节是**当前边界**，各自的原因写在 `README.md`《已知边界 → 音频格式》里。

### ⚠️ 本地音乐不支持 WMA

**明确的功能缺口。** Biu 的本地扫描白名单里有 `.wma`，本项目解不了它（还没有能用的纯 Go
WMA 解码器）。

扫到 `.wma` 会列出来但**播不了**——页面顶上会明写「这个目录里有我们还放不出的格式
（.wma），它们会列出来但放不了。」，行里的作者那一格也写着「这个格式我们放不了」，不会
让人以为文件坏了。

这条是「**决定还没做，但当前边界清楚**」，不是「没排期」。将来要不要做还没拍板。

### ⚠️ Windows 上有损那一档还没在真机上验过

B 站的有损音质（`dash.audio`，最高 192K）那一档**接上了** Windows 自带的 Media Foundation
（本仓库的 `internal/service/audio/mfadec`，`IMFTransform` + `CLSID_CMSAACDecMFT`，issue
#253）——**但这条还没在真 Windows 上跑过一行**：验收它的那条 CI
（`windows-aac-spike.yml`）的 runner 一直没接上（账号付款的问题），所以「能不能放、放在哪、
多长」**都还没有真机上的数**。

### ⚠️ 杜比音轨不能手动选

杜比全景声（实测 id `30250`，`codecs` 是 `ec-3`）没有放进设置里的音质档位（只有 自动 /
无损 / 192K / 128K / 64K）。自动档在 B 站只给杜比时会取它，**能不能解出取决于平台**：

- macOS 的 AudioToolbox **解得了**；
- **Windows 上解不了**——Media Foundation 不带 E-AC-3，而且 Win11 24H2 移除了系统自带的
  AC-3 解码器（再加上 Dolby 那边的 field-of-use 授权限制）。

所以这一档在 Windows 上是**划掉**的。

⚠️ 「杜比 Hi-Res = `30251`」是**二手映射表**里的说法，不是公开资料的说法——公开资料
（「视频伴音音质代码」一节）写的是 `30251` = **Hi-Res 无损**，实测它就是 `dash.flac` 那一档
（编码 `fLaC`）。

### ⚠️ AIFF 只认未压缩的 PCM

`internal/service/audio/aiff.go` 是本项目自己写的，只解未压缩的 PCM。AIFF 容器里的压缩编码
（MAC3/IMA4 等）解不了。

### ⚠️ ALAC 解不了

`.m4a` / `.m4s` / `.mp4` 里装的 ALAC 音轨解不了，而 **B 站不发 ALAC**。

**什么时候用得上**：你在查「这个文件为什么列得出来却放不了」，或者在决定要不要补一种
解码器。

---

## 音频之外的几条已知边界

**出处**：`README.md`《已知边界 → 音频质量之外的行为》

这几条不是「不支持」，是**当前做不了 / 就是这样**，各自的原因写在
`README.md`《已知边界 → 音频质量之外的行为》里：

| 边界 | 说明 |
|---|---|
| **B 站「音乐 MV」播不了** | 本质是视频。已批准延后（恢复条件见维护者本机的 `PLAN.md` §5.2）。 |
| **变速倍率落盘** | 用户设的倍率重启之后还在（`playbackRate`，0 或缺省按原速）。 |
| **改倍率之后约 1 秒才听得出来** | MyGo 预解约一秒，改倍率之前那一秒的输出已经铺好了。这是**框架缓冲的固有延迟**，不是 bug。 |

**什么时候用得上**：你在查「为什么 MV 放不了 / 为什么改了倍率没立刻生效 / 改倍率的设置
为什么重启还在」。
