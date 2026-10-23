package bili

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// 这个文件钉的是**实测**（2026-10-23，issue #252）：B 站投稿那一路的无损流
// 是「MP4 里的 FLAC」，不是裸 FLAC。
//
// 素材和形状说明在 `testdata/lossless/README.md`——那里写着怎么抓的、
// 抓了哪几个投稿、以及**哪些还没核实**。
//
// # 为什么值得钉一条
//
// 这一条直接决定 Windows 上无损流能不能放：容器形状决定走哪个解码器。
// 仓库里对这件事有过三种说法（裸 FLAC / MP4 里的 FLAC / 两种都可能），
// 而**没有一种是被验过的**——以前那几条注释都是照着公开资料和猜的写的。
// 现在有实测了，形状就该由素材钉住，而不是由注释钉住。

// losslessFixtureDir 是实测素材的目录。
const losslessFixtureDir = "testdata/lossless"

// bareFLACMagic 是**裸** FLAC 的头四个字节。
//
// ⚠️ 它和 `flacdec` 认的那四个字节是同一个（`flacdec/native.go:90`），
// 这里写死是为了让「B 站给的不是这个」这件事一眼能看懂。
const bareFLACMagic = "fLaC"

// TestTheLosslessStreamIsAnMP4Container 钉「B 站的无损流是 MP4 盒」。
//
// 判据是**头几个字节**：实测抓到的是 `ftyp` 盒（`00 00 00 20 66 74 79 70`），
// 而不是裸 FLAC 的 `fLaC`。差这四个字节，走的就是完全不同的解码器：
// `ftyp` 走 MyGo 的格式表（`????ftyp` → 系统解码器），
// `fLaC` 走 `flacdec`。
func TestTheLosslessStreamIsAnMP4Container(t *testing.T) {
	for _, name := range []string{
		"flac-stream-head-fragmented.bin",
		"flac-stream-head-progressive.bin",
	} {
		t.Run(name, func(t *testing.T) {
			head := readLosslessFixture(t, name)
			if len(head) < 16 {
				t.Fatalf("素材只有 %d 字节，连前 16 字节都不够", len(head))
			}
			if bytes.HasPrefix(head, []byte(bareFLACMagic)) {
				t.Fatalf("素材以 %q 开头——那是裸 FLAC，和实测对不上", bareFLACMagic)
			}
			// ISO-BMFF 的第一个盒是 `ftyp`：四字节盒长 + "ftyp"。
			if got := string(head[4:8]); got != "ftyp" {
				t.Fatalf("头 4–8 字节是 %q，want \"ftyp\"（那是 MP4 盒的标记）", got)
			}
		})
	}
}

// TestTheLosslessStreamHeadIsPinned 钉住实测抓到的**前 16 字节**。
//
// 这 16 个字节是「裸 FLAC 还是 MP4」这一问的全部答案：
//
//	00 00 00 20  66 74 79 70  69 73 6f 35  00 00 00 01
//	└─盒长 32─┘  └─"ftyp"─┘  └─"iso5"─┘  └─盒长 1─┘
//
// 写死它是为了让「B 站改形状」这件事变成一次**测试红**，而不是又一次
// 「用户说 Windows 上放不出来」。
func TestTheLosslessStreamHeadIsPinned(t *testing.T) {
	// 分片 MP4 那一支：`ftyp` 的 brand 是 `iso5`，后面 `00 00 00 01` 是
	// 下一个盒的长度（一个字节的兼容 brand `iso5`）。
	wantFragmented := "000000206674797069736f3500000001"
	// 非分片那一支：`ftyp` 的 brand 是 `isom`，后面 `00 00 02 00` 是兼容
	// brand 列表的长度（512 字节：`isom iso2 mp41 iso5`）。
	wantProgressive := "000000206674797069736f6d00000200"

	for _, tc := range []struct {
		file string
		want string
	}{
		{"flac-stream-head-fragmented.bin", wantFragmented},
		{"flac-stream-head-progressive.bin", wantProgressive},
	} {
		t.Run(tc.file, func(t *testing.T) {
			head := readLosslessFixture(t, tc.file)
			got := hex.EncodeToString(head[:16])
			if got != tc.want {
				t.Errorf("前 16 字节 = %s，want %s", got, tc.want)
			}
		})
	}
}

// TestTheLosslessStreamShapeFromTheRealResponse 钉「真接口回来的那一段」。
//
// 这一条走的是**仓库自己的解析路径**：把实测的 `data.dash` 喂给
// `dashInfo`，再走 `pickAudio`——所以它验的是「解析 + 挑流」这一整条，
// 而不只是几个字节。
//
// 顺带钉住实测到的 `codecs` 是 **`"fLaC"`（首字母大写）**：
// `strings.Contains(codecs, "flac")` 对它**判不出来**，
// 而仓库以前的 fixture 写的是小写 `"flac"`——那是猜的。
func TestTheLosslessStreamShapeFromTheRealResponse(t *testing.T) {
	raw := readLosslessFixture(t, "dash-flac-audio.json")
	var d dashInfo
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("实测的 dash 读不懂: %v", err)
	}
	if d.Flac == nil || d.Flac.Audio == nil {
		t.Fatal("实测的 dash 里没有 dash.flac.audio")
	}

	flac := d.Flac.Audio
	if flac.Codecs != "fLaC" {
		t.Errorf("codecs = %q，want \"fLaC\"（实测值，首字母大写）", flac.Codecs)
	}
	if flac.MimeType != "audio/mp4" {
		t.Errorf("mimeType = %q，want \"audio/mp4\"", flac.MimeType)
	}
	if flac.ID != 30251 {
		t.Errorf("id = %d，want 30251（实测值）", flac.ID)
	}

	// 无损这一档必须被挑出来。
	picked, q, ok := pickAudio(&d, AudioLossless)
	if !ok {
		t.Fatal("挑不出无损流")
	}
	if picked.Codecs != "fLaC" {
		t.Errorf("挑中的流 codecs = %q，want \"fLaC\"", picked.Codecs)
	}
	if q != Quality(picked.ID) {
		t.Errorf("挑中的流音质 = %d，want %d", q, picked.ID)
	}

	// 容器是 **MP4**：实测的头 64 个字节是 `ftyp` 盒。
	if got := containerOf(picked); got != "mp4" {
		t.Errorf("容器 = %q，want mp4（实测：这条流是 MP4 盒里的 FLAC）", got)
	}
}

// readLosslessFixture 读一份实测素材。
func readLosslessFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(losslessFixtureDir, name))
	if err != nil {
		t.Fatalf("读实测素材 %s: %v", name, err)
	}
	return b
}
