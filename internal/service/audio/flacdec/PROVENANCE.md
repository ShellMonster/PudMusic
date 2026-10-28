# flacdec —— 从验证资产搬进来的 FLAC 解码器

这是 t-0017 验证过的实现，搬进本仓库，**代码逐字未改**，只改了 import 路径
（`biu.audio/flacdec` → `github.com/ShellMonster/PudMusic/internal/service/audio/flacdec`）。
测试、测试素材一起搬过来了，所以 `go test ./...` 在本仓库里仍然能跑那 64 项验证。

原始资产在 `~/.herdr-projects/biu-mygo/library/flac-decoder-verified/`，
完整验证记录（逐项实测数字、四个已复现的缺陷及其修法）在那里的 `VERIFICATION.md`
与 `FORMAT-CONTRACT.md`。没搬过来的是：

| 没搬 | 为什么 |
|---|---|
| `reference-defects/` | t-0013 旧实现的缺陷复现证据，是历史档案，不是本项目的代码 |
| `gen/` | 重新生成测试素材要用 ffmpeg；素材已经签进来了 |
| `README.md` / `VERIFICATION.md` / `FORMAT-CONTRACT.md` | 文档，留在原处；关键的坑已经写进源码注释 |

## 用法

```go
d, err := flacdec.OpenFile(path)   // 裸 FLAC 和 MP4 里的 FLAC 都收
if err != nil {
	return err
}
defer d.Close()                     // 关文件句柄
audio.NewPlayer(d)                  // MyGo 驱动的流式播放
```

本项目里实际走的是 `internal/service/audio.Open()`（本地文件）与
`OpenStream()`（B 站在线流），两条路调的是**同一个** `openDecoder`：按头几个字节
查 MyGo 的注册表，认不出来才落到这个包的 `Open`。

## 搬进来之后要留意的一件事

`mygoflac` 子包（注册裸 FLAC 到 `audio.RegisterFormat`）也一起搬了，但**本项目不 import 它**：
注册表是对字面量做匹配的，给变长的 magic（`fLaC` + STREAMINFO）注册不了，
所以 `openDecoder` 在注册表认不出来之后**试一次** `flacdec.Open`。
MP4 里的 FLAC 头几个字节是 `ftyp`，注册表认得出来，走的是系统解码器那条路。
