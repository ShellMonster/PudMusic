# B 站无损流的实测形状

**2026-10-23 实测**（issue #252）。素材是从真的 `dash.flac.audio` 流上取的，
**只读**：只发过 `GET /x/player/wbi/playurl`（取流）、`GET /x/frontend/finger/spi`
（拿 buvid）和几次 `Range`（取头字节）／整条流（只为读盒结构和解码）。没有碰任何写接口。

## 结论

**B 站投稿那一路的无损流是「MP4 里的 FLAC」，不是裸 FLAC。**

判据是头几个字节：

| | 裸 FLAC | B 站的无损流（实测） |
|---|---|---|
| 头 4 字节 | `66 4c 61 43`（`fLaC`） | `00 00 00 20`（盒长） |
| 头 4–7 字节 | `fLaC` | `66 74 79 70`（`ftyp`） |

也就是说：**按容器认的话，它走的是 MP4 那一条路**（`????ftyp`），
**不是** `flacdec` 的裸 FLAC 那条。

## 盒结构

实测两条流的盒结构**是同一种**：

```
ftyp → [free → free] → moov → sidx → (moof → mdat) × N
```

**初始化段 + 分片**（fMP4）：`moov` 里有轨道（`fLaC` 采样条目 + 它的 `dfLa` 盒），
声音在后面的 `moof`/`mdat` 分片里。

⚠️ **`moov` 必须在**——`fLaC` 采样条目和 `dfLa` 盒都在它里面，
那是 `flacdec` 的 MP4 那条路（`internal/service/audio/flacdec/mp4.go`）
找 STREAMINFO 和帧的地方。少了 `moov`，`go-m4a` 就没有轨道可读。

## 文件

| 文件 | 是什么 |
|---|---|
| `dash-flac-audio.json` | 真接口回的那一段 `data.dash`（只留音频相关的字段，签名地址换成了 host+path） |
| `flac-stream-head-dash.bin` | 一条流的头 **512 字节**：`ftyp`(brand `iso5`，兼容 brand `dsms msi xdash`) → 两个 `free` 填充盒 → `moov` 在 **114** |
| `flac-stream-head-isom.bin` | 另一条流的头 **512 字节**：`ftyp`(brand `isom`，兼容 brand `isom iso2 mp41 iso5`) → `moov` 在 **32** |

两支**都是**「初始化段 + 分片」，差别只在 `ftyp` 的 brand 和 `moov` 前面
有没有 `free` 填充盒。

## 实测到的字段

`dash.flac.audio`：

```json
{
  "id": 30251,
  "bandwidth": 1541569,
  "codecs": "fLaC",
  "mimeType": "audio/mp4",
  "baseUrl": "…/1361516894-1-30251.m4s"
}
```

⚠️ **`codecs` 是 `"fLaC"`，首字母大写**——`strings.Contains(codecs, "flac")`
**判不出来**。仓库以前的 fixture 写的是小写 `"flac"`，那是猜的。

同一段响应里另外两档（也是实测）：

| 字段 | id | codecs | 容器 |
|---|---|---|---|
| `dash.audio[]` | 30216 / 30232 / 30280 | `mp4a.40.5` / `mp4a.40.2` | MP4 |
| `dash.dolby.audio[]` | 30250 | `ec-3` | MP4 |
| `dash.flac.audio` | **30251** | **`fLaC`** | **MP4** |

## `flacdec` 能解它（实测）

把整条流（40 MB 左右）下下来交给 `flacdec.Open`：

```
opened: 48000 Hz, 2 ch, 24 bits
read 20480 frames of PCM without error
```

**48000 Hz / 2 声道 / 24 bit**，两支形状都一样。→ 纯 Go 的 `flacdec` 能解
B 站的无损流，**Windows 上没有系统解码器时落回 `flacdec` 那条路走得通**
（见 `internal/service/audio/player.go` 的 `openDecoder`）。

⚠️ 整条流**没有签进仓库**（40 MB），所以这一条是**记录下来的实测**，
不是一条能跑的测试。

## 样本清单

`BV1Sc411U7m6`（`cid` 1361516894）、`BV1WkXaBbEtt`、
`BV1qFuQz2EVx`、`BV1ky4y1Z7bx`、`BV1VQoEBSEJm`、`BV1vpWEzXEqA`、
`BV1FPjy6TEiE`、`BV1bjJo64Eyr`、`BV1nutz6mEgY`——九个投稿的无损流字段
**全都是** `id=30251` + `codecs="fLaC"` + `mimeType="audio/mp4"`。
其中五个的头字节取过，全是 `ftyp` 盒；两支的**整条流**下下来解过，全是
`48000/2/24` 且 `flacdec` 解得动。

## 还没核实的（别当结论用）

- **`dash.flac.audio.id` 到底是几**：实测是 **30251**，而仓库的
  `quality.go` 把 30251 叫「杜比 Hi-Res」、把 FLAC 记成 30257。
  **两者对不上。** 公开资料说 30251 = 杜比 Hi-Res、30257 = Hi-Res 无损，
  而真接口在 `dash.flac` 里给的是 30251。**这一条本票没裁决**
  （音质标签是另一张票的事），fixture 如实记下实测值。
- **`dash.dolby` 里的 30251**：真接口**一次都没给过**那一档，
  抓到的全是 30250（`ec-3`）。所以「杜比 Hi-Res」这个 id 在 `dash.dolby`
  里的含义**仍然未知**。
- **音乐库那一路**（`/audio/music-service-c/url`，按 `sid` 问）
  **没测**：要拿一个 `sid` 得列收藏夹内容，而 B 站对那个接口一直回
  `-400` / HTTP 412。所以「音乐库的无损是不是裸 FLAC」**仍然未知**——
  仓库里 `audio.go` 写的「`.flac` 在无损那一档」**没有实测支撑**。
