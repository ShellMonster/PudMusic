# B 站无损流的实测形状

**2026-10-23 实测**（issue #252）。素材是从真的 `dash.flac.audio` 流上取的，
**只读**：只发过 `GET /x/player/wbi/playurl`（取流）、`GET /x/frontend/finger/spi`
（拿 buvid）和一次 `Range: bytes=0-63`（取头 64 个字节）。没有碰任何写接口。

## 结论

**B 站投稿那一路的无损流是「MP4 里的 FLAC」，不是裸 FLAC。**

判据是头几个字节：

| | 裸 FLAC | B 站的无损流（实测） |
|---|---|---|
| 头 4 字节 | `66 4c 61 43`（`fLaC`） | `00 00 00 20`（盒长） |
| 头 4–7 字节 | `fLaC` | `66 74 79 70`（`ftyp`） |

也就是说：**按容器认的话，它走的是 MP4 那一条路**（`????ftyp`），
**不是** `flacdec` 的裸 FLAC 那条。

## 文件

| 文件 | 是什么 |
|---|---|
| `dash-flac-audio.json` | 真接口回的那一段 `data.dash`（只留音频相关的字段，签名地址换成了 host+path） |
| `flac-stream-head-fragmented.bin` | 同一条流的**头 64 字节**：`ftyp`(brand `iso5`) + 兼容 brand `dsms msi xdash` → **分片** MP4（fMP4） |
| `flac-stream-head-progressive.bin` | 另一条流的**头 64 字节**：`ftyp`(brand `isom`) + `moov`/`mvhd` → **非分片** MP4 |

两种形状都出现过（同一批投稿里就有 2 个分片、3 个非分片），**但都是 MP4 盒**。

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

采集到的样本：`BV1Sc411U7m6`（`cid` 1361516894）、`BV1WkXaBbEtt`、
`BV1qFuQz2EVx`、`BV1ky4y1Z7bx`、`BV1VQoEBSEJm`、`BV1vpWEzXEqA`、
`BV1FPjy6TEiE`、`BV1bjJo64Eyr`、`BV1nutz6mEgY`——九个投稿的无损流
**全都是** `id=30251` + `codecs="fLaC"` + `mimeType="audio/mp4"`。
其中五个的头字节取过：**两个是分片 MP4**（`BV1Sc411U7m6`、`BV1ky4y1Z7bx`）、
**三个是非分片 MP4**（`BV1WkXaBbEtt`、`BV1qFuQz2EVx`、`BV1VQoEBSEJm`）
——**五种都是 MP4 盒**。

## 还没核实的（别当结论用）

- **`dash.flac.audio.id` 到底是几**：实测是 **30251**，而仓库的
  `quality.go` 把 30251 叫「杜比 Hi-Res」、把 FLAC 记成 30257。
  **两者对不上。** 公开资料说 30251 = 杜比 Hi-Res、30257 = Hi-Res 无损，
  而真接口在 `dash.flac` 里给的是 30251。**这一条本票没裁决**
  （音质标签是另一张票的事），fixture 如实记下实测值。
- **音乐库那一路**（`/audio/music-service-c/url`，按 `sid` 问）
  **没测**：要拿一个 `sid` 得列收藏夹内容，而 B 站对那个接口一直回
  `-400` / HTTP 412。所以「音乐库的无损是不是裸 FLAC」**仍然未知**——
  仓库里 `audio.go` 写的「`.flac` 在无损那一档」**没有实测支撑**。
