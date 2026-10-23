# mp3dec —— vendor 进来的 MP3 解码器

这一票把 MP3 解码从「MyGo 自带的 `mygo/audio/mp3`」换成 `tphakala/go-mp3`。
换掉的是**三件事**：

1. **补 MPEG-2.5**——旧的那个（`hajimehoshi/go-mp3`）在 `internal/frame/frame.go`
   里明写着遇到 MPEG-2.5 就报错（实测：`mp3: MPEG version 2.5 is not supported`）。
2. **补 LAME gapless**——旧的那个**完全不做** gapless，编码器延迟（1105 个样本）
   原样留在输出里：歌的开头多一段静音，结尾也多一段。
3. **打开一个 MP3 不再全文件扫帧头**——旧的那个 `NewDecoder` 要把整个文件
   一帧一帧读一遍才算出长度（`ensureFrameStartsAndLength`），而且给每一帧
   记一个偏移。本地音乐列表扫几百个文件时，那是几百次全文件读。

## 搬的是哪一份

| | |
|---|---|
| 上游 | https://github.com/tphakala/go-mp3 |
| 版本 | `v0.2.0`（tag） |
| commit | `13809b237467733bd8d7e0ec0d27e7a98fe2f1e0`（2026-10-14） |
| 许可证 | **MIT**，Copyright (c) 2026 Tomi P. Hakala |
| 许可证原文 | `gomp3/LICENSE`（逐字副本），随包发出去的那份在 `resources/licenses/github.com-tphakala-go-mp3-LICENSE.txt` |
| 上游自己的来路说明 | `gomp3/PROVENANCE.md`（**逐字**搬过来的，讲解码器是 minimp3 的移植、CC0-1.0） |

⚠️ **许可证是 MIT，不是 BSD-3**——调研报告 §6.3 那一行写的是「BSD-3」，而
上游仓库根目录的 `LICENSE` 文件写着 MIT。**以原文为准**：MIT 的「保留声明」
那一条要求随附的那份声明是真的那份，所以原文逐字放在 `gomp3/LICENSE`，
随包发出去的那一份由 `go generate ./internal/licensing/` 从它**逐字复制**
（不是手抄——`internal/licensing` 的 `AssetLicenseSources` 记着这个映射，
`TestTheShippedAssetLicensesExist` 盯着两份一字不差）。

## 搬了哪些文件，哪些没搬

`gomp3/` 底下是上游的目录结构，**代码逐字未改**，只改了 import 路径
（`github.com/tphakala/go-mp3` → `github.com/ShellMonster/Biu_Go/internal/service/audio/mp3dec/gomp3`）。

搬进来的：

| 上游路径 | 是什么 |
|---|---|
| `decoder.go`、`doc.go` | 根包 `mp3`：逐帧解码器（`DecodeFrame`） |
| `internal/dec/*.go` | 真正的解码器（帧头、哈夫曼、IMDCT、立体声、合成窗……） |
| `internal/bits/*.go` | 位读取器（`internal/dec` 用） |
| `pcm/decoder.go` 等 | 流式那一层：缓冲、ID3v2 跳过、Xing/Info/VBRI、**LAME gapless 裁剪**、精确 seek |
| `pcm/convert.go`、`pcm/decode_oneshot.go` | 解码器这一侧的另两个文件（格式转换、一次解完整份的便捷函数） |
| `LICENSE` | 许可证原文 |
| `PROVENANCE.md` | 上游自己的来路说明（讲 minimp3 那个 CC0 移植） |

**没搬**（本项目只解码，用不上）：

| 没搬 | 为什么 |
|---|---|
| 根包 `encoder.go` | 编码器。它定义 `mp3.TotalDelay` 等符号，**只有 `pcm` 的编码器那一半用得到**；搬它会连着拖进 `internal/enc/`（三十几个文件，含 amd64/arm64 汇编）和 `golang.org/x/sys`——那是一个**新依赖**，而本项目「不新增依赖」 |
| `pcm/encoder.go`、`pcm/encode_oneshot.go`、`pcm/pcm.go`、`pcm/infotag.go` | 编码器那一半（`Config`、`NewEncoder`、写 LAME 标签） |
| `internal/enc/`、`internal/quality/`、`internal/testsignal/` | 编码器、编码质量度量、测试信号 |
| `rules/`、`tools/` | 上游自己的 lint 规则（`go-ruleguard`）和 oracle/质量工具链 |
| 全部 `*_test.go` | 上游的测试。**这一条是本票最舍不得的**：上游有一整套对着 minimp3 oracle 的逐位比对测试，我们搬不了 oracle（那是 C 代码 + 构建脚本），搬测试只会留下一堆跑不起来的空壳 |

搬进来的这份**没有任何外部依赖**（只用标准库）——这是「不新增依赖」那条
不变量的直接结果。

⚠️ 搬进来之后有两处「**留着但用不上**」的死代码：`pcm/convert.go` 里的
`deinterleaveS16` 和 `f32FromS16Scale`（只有编码器那一半用），以及
`internal/bits/writer.go`（只有编码器那一半用）。它们留着是为了让搬进来的
文件**逐字等于上游那一份**——少一个文件就多一处「和上游对不上」的地方，
而那种地方没人会去核。Go 编译器不报「函数没人用」，`go vet` 也不报。

## 改了什么（只有两处）

1. **import 路径**：`github.com/tphakala/go-mp3` → 本仓库的路径。
2. **两段文档注释**：
   - `gomp3/doc.go`：上游那一句说这个包同时有解码器和编码器——编码器没搬，
     照抄就是**说谎**（AGENTS.md §7：「注释说谎比没有注释更坏」）。
   - `gomp3/pcm/doc.go` 的 `# Encoding` 一节：同上。

   **代码一个字没动。**

## 为什么不进 `go.mod`，而是 vendor

三条理由：

1. **上游没有发布版本。** `go list -m -versions github.com/tphakala/go-mp3`
   只有 `v0.1.0` 和 `v0.2.0`，而 `v0.2.0` 之后还有一串提交（其中
   `13809b2` 正是「认 ffmpeg 写的 LAME gapless 标签」那个修复——**它就是
   `v0.2.0` 本身**）。要进 `go.mod` 就得钉一个 pseudo-version，而 pseudo-version
   的形式（`v0.0.0-2026...`）在可读性上比 tag 差一截。
2. **进 `go.mod` 就进了 `go list -deps`，也就进了许可清单那张表**——而
   `internal/licensing` 的三道门是照着「清单 == 构建产物」核的。vendor 进来
   的代码不进那张表，于是它**另有一条**随包发出去的路
   （`AssetLicenseRelPaths` + `AssetLicenseSources`）。两条路各有各的代价，
   但 vendor 这一条**不引入任何新的构建依赖**。
3. **本项目要的是「一份固定的、逐字可查的代码」**，不是一个会跟着
   `go get -u` 动的模块指针。vendor 进来的代码就在仓库里，谁改了什么，
   `git blame` 一眼看得出。

## 复现「搬进来」这件事

```sh
git clone https://github.com/tphakala/go-mp3
cd go-mp3 && git checkout v0.2.0   # 13809b237467733bd8d7e0ec0d27e7a98fe2f1e0
# 照上面两张表把文件拷进 internal/service/audio/mp3dec/gomp3/，
# 然后把 import 路径改掉（见「改了什么」）。
```

## 本项目怎么用它

```go
d, err := mp3dec.Open(r)   // r 是 io.ReadSeeker
```

本项目里实际走的是 `internal/service/audio.Open()`（本地文件）与
`OpenStream()`（B 站在线流），两条路调的是**同一个** `openDecoder`：按头几个
字节查 MyGo 的注册表，认不出来才落到 `flacdec.Open`。

MP3 的注册在 `mp3dec` 包的 `init()` 里（ID3v2 标签 + MPEG-1/2/2.5 Layer III
的六个同步字）。**`internal/service/audio/player.go` 里那一行
`_ "github.com/egoist/mygo/audio/mp3"` 已经撤掉**——留着的话两个 mp3
解码器都会注册，认出哪一个取决于 init 顺序，而那个顺序不是能靠读代码
定下来的东西。
