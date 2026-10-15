package flacdec_test

import (
	"bytes"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/audio/flacdec"
	_ "github.com/ShellMonster/Biu_Go/internal/service/audio/flacdec/mygoflac"

	"github.com/egoist/mygo/audio"
)

// This file is where the decoder is checked against MyGo's audio package rather
// than against the interface it happens to satisfy: the same paths a player
// takes, with MyGo's own machinery in between.

// TestMyGoInterface is the compile-time half: a Decoder is usable as an
// audio.Decoder, and is closed by whoever owns it.
var (
	_ audio.Decoder = (*flacdec.Decoder)(nil)
	_ io.Closer     = (*flacdec.Decoder)(nil)
)

// TestMyGoNewDecoder goes through MyGo's own format lookup.
func TestMyGoNewDecoder(t *testing.T) {
	data := readFile(t, "testdata/stereo16.flac")
	d, name, err := audio.NewDecoder(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if name != "flac" {
		t.Errorf("MyGo named the format %q, want \"flac\"", name)
	}
	if d.SampleRate() != 44100 || d.Channels() != 2 {
		t.Errorf("%d Hz, %d channels", d.SampleRate(), d.Channels())
	}
	if n, err := readAllErr2(d); err != io.EOF || len(n) != 352800 {
		t.Errorf("decoded %d samples, %v; want 352800 and io.EOF", len(n), err)
	}
}

// TestMyGoReadSound runs the whole track through MyGo's in-memory path, which is
// the one that checks the interface most strictly: it asks for Len before
// reading, subtracts n % Channels from every read, and keeps the first two
// channels of a wider track. Sound's fields are unexported, so what is checked
// is that the path completes and that the length it reports is the track's own.
func TestMyGoReadSound(t *testing.T) {
	for _, c := range []struct {
		wav, flac string
		seconds   float64
	}{
		{"stereo16.wav", "stereo16.flac", 4},
		{"stereo24.wav", "stereo24.flac", 4},
		{"mono16.wav", "mono16.flac", 2},
	} {
		t.Run(c.flac, func(t *testing.T) {
			d, err := flacdec.New(readFile(t, filepath.Join("testdata", c.flac)))
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			sound, err := audio.ReadSound(d)
			if err != nil {
				t.Fatal(err)
			}
			want := time.Duration(c.seconds * float64(time.Second))
			if got := sound.Duration(); got != want {
				t.Errorf("the sound is %v long, want %v", got, want)
			}
			// It is a playable sound: MyGo accepts it and reports the same length.
			if got := sound.NewPlayer().Duration(); got != want {
				t.Errorf("the player says %v, want %v", got, want)
			}
		})
	}
}

// TestMyGoPlayer drives a real MyGo player: it starts a decode goroutine that
// runs about a second ahead of playback, seeks and re-seeks, and closes.
// wantLength is the length each fixture is, which MyGo computes from Len.
func wantLength(name string) time.Duration {
	if name == "mono16.mp4" {
		return 2 * time.Second
	}
	return 4 * time.Second
}

func TestMyGoPlayer(t *testing.T) {
	for _, name := range []string{"stereo16.flac", "stereo16.mp4", "mono16.mp4"} {
		t.Run(name, func(t *testing.T) {
			d, err := flacdec.OpenFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			p := audio.NewPlayer(d)
			// Duration comes from Len, which is what the player's seek range is
			// clamped to, so it has to be the real length of the track.
			total := p.Duration()
			if want := wantLength(name); total != want {
				t.Errorf("Duration %v, want %v", total, want)
			}
			// Seeking anywhere in the range, forward and back, must not make the
			// stream end: MyGo treats a SetPosition error as the end of playback.
			for _, at := range []time.Duration{
				0, total / 4, total * 5 / 8, total / 8, 0, total,
			} {
				p.Seek(at)
				if got := p.Position(); got != at {
					t.Errorf("Seek(%v) left the player at %v", at, got)
				}
			}
			p.Close()
		})
	}
}

// TestMyGoPlayerClosesTheFile is the leak the t-0013 reference implementation has,
// seen through MyGo: MyGo closes a Decoder that is an io.Closer when the player
// closes, so a decoder that keeps its file handle leaks one per track played.
//
// The close is not immediate: Player.Close tells the decode goroutine to stop,
// and that goroutine calls Close on the decoder when it wakes up. The count is
// therefore waited for rather than read straight away.
func TestMyGoPlayerClosesTheFile(t *testing.T) {
	before := openFiles(t)
	for i := 0; i < 8; i++ {
		d, err := flacdec.OpenFile(filepath.Join("testdata", "stereo16.mp4"))
		if err != nil {
			t.Fatal(err)
		}
		audio.NewPlayer(d).Close()
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		after := openFiles(t)
		if after <= before {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("after playing and closing 8 tracks, %d files are still open, was %d", after, before)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestMyGoLoop covers looping, which seeks the decoder back to the start after it
// reaches the end: a decoder that cannot seek back is audible as a gap.
func TestMyGoLoop(t *testing.T) {
	d, err := flacdec.OpenFile(filepath.Join("testdata", "stereo16.flac"))
	if err != nil {
		t.Fatal(err)
	}
	p := audio.NewPlayer(d).SetLoop(true)
	defer p.Close()
	if !p.Loop() {
		t.Fatal("SetLoop(true) did not take")
	}
	// Seek to the very end and back, which is what the loop does.
	p.Seek(p.Duration())
	p.Seek(0)
	if got := p.Position(); got != 0 {
		t.Errorf("after seeking to the end and back, the player is at %v", got)
	}
}

// readAllErr2 decodes a whole track from an audio.Decoder, which only says how
// many channels it has.
func readAllErr2(d audio.Decoder) ([]float32, error) {
	ch := d.Channels()
	buf := make([]float32, 4096*ch)
	var out []float32
	for {
		n, err := d.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			return out, err
		}
		if n == 0 {
			return out, io.ErrNoProgress
		}
	}
}
