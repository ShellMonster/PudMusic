package flacdec_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/service/audio/flacdec"
)

// openFiles counts the file descriptors this process holds, or skips the test when
// the platform does not let it ask. /dev/fd is the portable-enough way: it exists
// on Linux and on macOS.
func openFiles(t *testing.T) int {
	t.Helper()
	f, err := os.Open("/dev/fd")
	if err != nil {
		t.Skipf("cannot count open files: %v", err)
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		t.Skipf("cannot count open files: %v", err)
	}
	// /dev/fd itself is one of them, and it is open while we count.
	return len(names)
}

// TestCloseReleasesTheFile is the defect the t-0013 reference implementation has:
// it opens the file, hands the reader to the decoder and drops the handle on the
// floor, so Close does nothing and every song change leaks a descriptor. A player
// that skips through a hundred tracks would run out.
func TestCloseReleasesTheFile(t *testing.T) {
	for _, name := range []string{"stereo16.flac", "stereo16.mp4", "stereo24.flac", "mono16.mp4"} {
		t.Run(name, func(t *testing.T) {
			before := openFiles(t)
			// Open several at once: a leak shows up as a rise that never comes back.
			var decoders []*flacdec.Decoder
			for i := 0; i < 8; i++ {
				d, err := flacdec.OpenFile(filepath.Join("testdata", name))
				if err != nil {
					t.Fatal(err)
				}
				// Read something, so the decoder is genuinely in use.
				if _, err := d.Read(make([]float32, 4096)); err != nil && !errors.Is(err, os.ErrClosed) {
					// A short or clean end is fine here.
					_ = err
				}
				decoders = append(decoders, d)
			}
			held := openFiles(t)
			if held < before+8 {
				t.Fatalf("after opening 8 decoders the process holds %d files, was %d: nothing was opened?",
					held, before)
			}
			for _, d := range decoders {
				if err := d.Close(); err != nil {
					t.Errorf("Close: %v", err)
				}
			}
			if after := openFiles(t); after > before {
				t.Errorf("after closing, %d files are still open, was %d before opening any", after, before)
			}
		})
	}
}

// TestCloseIsIdempotent and TestReadAfterClose check the rest of the lifecycle: a
// second Close is harmless, and a decoder that has been closed refuses to read
// rather than reading from a handle that is gone.
func TestCloseIsIdempotent(t *testing.T) {
	d, err := flacdec.OpenFile("testdata/stereo16.flac")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Errorf("second Close: %v, want nil", err)
	}
	if n, err := d.Read(make([]float32, 4096)); n != 0 || err == nil {
		t.Errorf("read after Close: n=%d err=%v, want 0 and an error", n, err)
	}
	if err := d.SetPosition(0); err == nil {
		t.Error("SetPosition after Close succeeded")
	}
}

// TestOpenDoesNotOwnItsReader is the other half of the contract: a caller that
// passes its own io.ReadSeeker keeps it, so closing the decoder does not close a
// file the caller still wants to read.
func TestOpenDoesNotOwnItsReader(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "stereo16.flac"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := flacdec.Open(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	// The caller's file is untouched.
	buf := make([]byte, 4)
	if _, err := f.ReadAt(buf, 0); err != nil {
		t.Errorf("the caller's file was closed with the decoder: %v", err)
	}
	f.Close()
}
