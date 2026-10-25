package audio

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// 这一组盯「走顶层盒子」那一步（票 #478 的容器层修法的第一半）。
//
// # 它盯的是什么
//
// 走错一步的后果是**静默**的：
//
//	把 `moov` 在前面的文件当成「在末尾」	→ 多发一条 Range 请求（白花）
//	把末尾的起点算错一分			→ 取回来的不是 `moov`，
//							而 AudioToolbox 照样等整个文件（白忙）
//	把不是 MP4 的文件当成 MP4		→ 算出一个乱的偏移，
//							而缓存里被写进了错的字节（**解出来是噪声**）
//
// 所以这一组用**手造出来的盒子**把每一种形状都摆一遍。

// box 造一个顶层盒子。
//
// ⚠️ **size 是盒子自己的长度（含头）**——而 `mdat` 那种「一直到文件末尾」
// 的形状是 size == 0，那一个单独造（`lastBox`）。
func box(typ string, payload []byte) []byte {
	b := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(b, uint32(8+len(payload)))
	copy(b[4:8], typ)
	copy(b[8:], payload)
	return b
}

// lastBox 造一个「一直到文件末尾」的盒子（size == 0）。
func lastBox(typ string, payload []byte) []byte {
	b := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(b, 0)
	copy(b[4:8], typ)
	copy(b[8:], payload)
	return b
}

// bigBox 造一个「长度是 64 位」的盒子（size == 1）。
func bigBox(typ string, payload []byte) []byte {
	b := make([]byte, 16+len(payload))
	binary.BigEndian.PutUint32(b, 1)
	copy(b[4:8], typ)
	binary.BigEndian.PutUint64(b[8:], uint64(16+len(payload)))
	copy(b[16:], payload)
	return b
}

// bytesReader 是一个「读得到全部字节」的 io.ReaderAt。
type bytesReader struct{ b []byte }

func (r bytesReader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= int64(len(r.b)) {
		return 0, errors.New("越界")
	}
	n := copy(p, r.b[off:])
	return n, nil
}

// TestTheTopLevelWalkFindsTheTrailingMoov 盯「末尾那个 `moov` 的起点」。
func TestTheTopLevelWalkFindsTheTrailingMoov(t *testing.T) {
	ftyp := box("ftyp", make([]byte, 20))
	free := box("free", make([]byte, 8))
	mdat := box("mdat", make([]byte, 1000))
	moov := box("moov", make([]byte, 100))
	data := append(append(append(append([]byte{}, ftyp...), free...), mdat...), moov...)

	moovAt, tailAt, err := mp4TopLevel(bytesReader{data}, int64(len(data)))
	if err != nil {
		t.Fatalf("走顶层盒子：%v", err)
	}
	if moovAt >= 0 {
		t.Errorf("`moov` 被当成了在前面（%d）", moovAt)
	}
	want := int64(len(ftyp) + len(free) + len(mdat))
	if tailAt != want {
		t.Errorf("末尾那一段的起点 = %d，want %d", tailAt, want)
	}
}

// TestTheTopLevelWalkFindsTheMoovAtTheFront 盯「`moov` 在前面时不取尾部」。
func TestTheTopLevelWalkFindsTheMoovAtTheFront(t *testing.T) {
	ftyp := box("ftyp", make([]byte, 20))
	moov := box("moov", make([]byte, 100))
	mdat := box("mdat", make([]byte, 1000))
	data := append(append(append([]byte{}, ftyp...), moov...), mdat...)

	moovAt, tailAt, err := mp4TopLevel(bytesReader{data}, int64(len(data)))
	if err != nil {
		t.Fatalf("走顶层盒子：%v", err)
	}
	if moovAt != int64(len(ftyp)) {
		t.Errorf("`moov` 的位置 = %d，want %d", moovAt, len(ftyp))
	}
	if tailAt >= 0 {
		t.Errorf("`moov` 在前面却说了要取尾部（%d）", tailAt)
	}
}

// TestTheTopLevelWalkGivesUpOnTheOtherShapes 盯「别的形状**不要**去取尾部」。
func TestTheTopLevelWalkGivesUpOnTheOtherShapes(t *testing.T) {
	for _, c := range []struct {
		name string
		data []byte
	}{
		{"不是 MP4（FLAC）", []byte("fLaC\x00\x00\x00\x22rest")},
		{"分片流（有 moof）", concat(
			box("styp", make([]byte, 12)),
			box("moof", make([]byte, 40)),
			box("mdat", make([]byte, 500)),
		)},
		{"mdat 一直到文件末尾", concat(
			box("ftyp", make([]byte, 20)),
			lastBox("mdat", make([]byte, 500)),
		)},
	} {
		_, tailAt, err := mp4TopLevel(bytesReader{c.data}, int64(len(c.data)))
		if !errors.Is(err, errMP4NotOurShape) {
			t.Errorf("%s：err = %v，want errMP4NotOurShape", c.name, err)
		}
		if tailAt >= 0 {
			t.Errorf("%s：说了要取尾部（%d）", c.name, tailAt)
		}
	}
}

// TestTheTopLevelWalkHandlesTheSixtyFourBitSize 盯「长度是 64 位的那种盒子」。
//
// ⚠️ **它是一个真的形状**：大文件（> 4 GB）的 `mdat` 就是这么写的，而
// 把 8 字节的长度当 4 字节读会算出一个完全错的偏移——而那个偏移会
// 被写进缓存文件。
func TestTheTopLevelWalkHandlesTheSixtyFourBitSize(t *testing.T) {
	ftyp := box("ftyp", make([]byte, 20))
	mdat := bigBox("mdat", make([]byte, 1000))
	moov := box("moov", make([]byte, 100))
	data := concat(ftyp, mdat, moov)

	_, tailAt, err := mp4TopLevel(bytesReader{data}, int64(len(data)))
	if err != nil {
		t.Fatalf("走顶层盒子：%v", err)
	}
	if want := int64(len(ftyp) + len(mdat)); tailAt != want {
		t.Errorf("末尾那一段的起点 = %d，want %d", tailAt, want)
	}
}

// TestTheTopLevelWalkOnTheRealFixtures 盯**本仓库自己的素材**。
//
// 🔴 **素材的形状是会变的**：换一份素材（或者 B 站改了封装方式），
// 这一条会当场红——而那正是它该做的。
func TestTheTopLevelWalkOnTheRealFixtures(t *testing.T) {
	for _, c := range []struct {
		file    string
		moovEnd bool // `moov` 在不在末尾
	}{
		{"stereo16.mp4", true},             // B 站形状：`moov` 在末尾
		{"reference-24bit-mp4.m4a", false}, // faststart：`moov` 在前面
	} {
		b, err := os.ReadFile(filepath.Join("flacdec", "testdata", c.file))
		if err != nil {
			t.Skipf("拿不到素材 %s：%v", c.file, err)
		}
		moovAt, tailAt, err := mp4TopLevel(bytesReader{b}, int64(len(b)))
		if err != nil {
			t.Errorf("%s：走顶层盒子：%v", c.file, err)
			continue
		}
		if c.moovEnd {
			if moovAt >= 0 {
				t.Errorf("%s：`moov` 不在末尾（%d）", c.file, moovAt)
			}
			if tailAt <= 0 {
				t.Errorf("%s：没有算出末尾那一段的起点", c.file)
			}
			// 末尾那一段的起点必须落在文件里，而且不能是文件头。
			if tailAt >= int64(len(b)) {
				t.Errorf("%s：末尾那一段的起点越界（%d / %d）", c.file, tailAt, len(b))
			}
		} else {
			if moovAt < 0 {
				t.Errorf("%s：没找到 `moov`", c.file)
			}
			if tailAt >= 0 {
				t.Errorf("%s：faststart 的文件不该说「要取尾部」（%d）", c.file, tailAt)
			}
		}
	}
}

// concat 把几段接起来。
func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
