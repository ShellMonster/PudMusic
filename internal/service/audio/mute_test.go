package audio

import (
	"io"
	"testing"
)

// TestTheLastStageIsMutedWhenTestsRun 钉住「go test 之下最后一级是哑的」。
//
// 这一条是「测试的时候喇叭嘟嘟嘟嘟响很久」的那道闸：macOS 的输出接的是默认
// 输出设备（扬声器），而测试里有一批真的会 `Play()` 的用例——最后一级不哑的
// 话，每跑一次测试，喇叭就把假解码器的台阶波、flacdec 的测试音挨个放一遍。
// 判据是 `maybeMuted` 在 `testing.Testing()` 之下给的那一级**是哑的**。
func TestTheLastStageIsMutedWhenTestsRun(t *testing.T) {
	if !testing.Testing() {
		t.Skip("这一条只在 go test 之下有意义")
	}
	if _, ok := maybeMuted(newFakeDec(44100, 2, 44100)).(muteDecoder); !ok {
		t.Fatal("go test 之下最后一级不是哑的——测试一跑，喇叭就把测试音放一遍")
	}

	// 哑的那一级：读出来的样本全零，EOF 和位置语义照常透传。
	m := muteDecoder{newFakeDec(44100, 2, 44100)}
	buf := make([]float32, 4096)
	n, err := m.Read(buf)
	if n == 0 || err != nil {
		t.Fatalf("哑级读不出样本：n=%d err=%v", n, err)
	}
	for i, v := range buf[:n] {
		if v != 0 {
			t.Fatalf("哑级的第 %d 个采样是 %v——它该是全零", i, v)
		}
	}
	// 位置和 EOF 照常透传：跳到末尾再读就是 io.EOF。
	if err := m.SetPosition(44100); err != nil {
		t.Fatalf("哑级没透传 SetPosition：%v", err)
	}
	if _, err := m.Read(buf); err != io.EOF {
		t.Fatalf("哑级没透传 EOF：%v", err)
	}
}
