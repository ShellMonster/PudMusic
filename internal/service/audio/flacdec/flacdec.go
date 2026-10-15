// Package flacdec decodes FLAC audio into the interleaved float32 samples
// MyGo's audio package plays.
//
// It covers both containers a FLAC track arrives in:
//
//   - a bare ".flac" stream ("fLaC" magic, then metadata blocks, then frames),
//     which is what a user's local music library is mostly made of;
//   - FLAC inside ISO-BMFF (an fLaC sample entry with a dfLa FLACSpecificBox),
//     which is what Bilibili's FLAC audio streams are, demuxed by go-m4a.
//
// A Decoder satisfies audio.Decoder:
//
//	func SampleRate() int
//	func Channels() int
//	func Read([]float32) (int, error) // n is samples, not frames
//	func SetPosition(frame int64) error
//	func Len() int64
//
// plus io.Closer. The package deliberately does not import MyGo, so it builds
// and tests without the audio backend; subpackage mygoflac wires it into
// audio.RegisterFormat.
//
// One Decoder decodes one playback path. MyGo drives a Decoder from a single
// goroutine (audio.NewPlayer), so a mini player and the main player need one
// Decoder each.
package flacdec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"github.com/mewkiz/flac/frame"
)

// Errors this package returns.
var (
	// ErrFormat reports data that is neither a bare FLAC stream nor an ISO-BMFF
	// file carrying FLAC.
	ErrFormat = errors.New("flacdec: not a FLAC stream")

	// ErrTruncated reports a stream that ended before the sample count its
	// STREAMINFO block promised: the data is cut short, and it is reported as an
	// error rather than as a clean end, so a partial download never plays as if it
	// were the whole track.
	ErrTruncated = errors.New("flacdec: truncated stream")

	// ErrSeekRange reports a SetPosition outside [0, Len()].
	ErrSeekRange = errors.New("flacdec: seek out of range")

	errClosed = errors.New("flacdec: decoder is closed")

	// isoBMFFBoxes are the box types a FLAC-in-MP4 file may start with. The first
	// box of a well-formed file is ftyp, but a streamed one starts with styp, and
	// some muxers put moov, free, skip, wide or mdat first.
	isoBMFFBoxes = []string{"ftyp", "styp", "moov", "free", "skip", "mdat", "wide"}
)

// Decoder decodes one FLAC track. It is not safe for concurrent use.
type Decoder struct {
	src    source
	closer io.Closer // the file OpenFile opened, if any
	ch     int
	rate   int
	bits   int
	scale  float32
	total  int64 // presented frames, -1 when unknown

	buf  []float32 // the current frame, interleaved and scaled
	head int       // next sample of buf to hand out
	skip int       // samples still to drop, from a seek or the stream's priming

	pos     int64 // frames the source has yielded since the last SetPosition
	seeked  bool
	atEnd   bool
	closed  bool
	pending error // sticky error, so a broken stream stops decoding
}

// source is the container half: it yields FLAC frames one at a time and can
// position itself at a sample. Both halves hand frames to the same
// mewkiz/flac frame parser, so everything after this interface is identical.
type source interface {
	// nextFrame parses the next frame, reporting its samples per channel. It
	// returns io.EOF only at a clean end of stream.
	nextFrame() (*frame.Frame, int, error)
	// seekTo moves the next frame to the one holding sample, and reports how many
	// samples at its start must be dropped to land exactly on it.
	seekTo(sample int64) (skip int, err error)
	close() error
}

// Open returns a Decoder reading r, which it does not close. r's contents decide
// the container: it may sit at any offset, and Open reads the header from there.
func Open(r io.ReadSeeker) (*Decoder, error) {
	start, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	var head [12]byte
	// Too short to identify is not a format, not a read error: a file that is not
	// a FLAC stream has no business opening with anything else.
	n, _ := io.ReadFull(r, head[:])
	if n < 12 {
		return nil, fmt.Errorf("%w: only %d bytes to identify", ErrFormat, n)
	}
	if _, err := r.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	switch {
	case n >= 4 && string(head[:4]) == "fLaC":
		return openNative(r)
	case n >= 12 && isBMFF(head[4:8]):
		return openMP4(r)
	default:
		return nil, fmt.Errorf("%w: starts with % x", ErrFormat, head[:min(n, 12)])
	}
}

// OpenFile opens path and returns a Decoder that closes the file with Close.
// This is what a player that owns its file wants; Open is for a caller that
// hands out a file it closes itself, such as a reader fed by a download.
func OpenFile(path string) (*Decoder, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	d, err := Open(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	d.closer = f
	return d, nil
}

// New returns a Decoder over data, which it copies what it needs of. Handy for
// a track already in memory, such as one just downloaded.
func New(data []byte) (*Decoder, error) {
	return Open(bytes.NewReader(data))
}

func isBMFF(b []byte) bool {
	for _, box := range isoBMFFBoxes {
		if string(b) == box {
			return true
		}
	}
	return false
}

// SampleRate returns the frames per second of the track.
func (d *Decoder) SampleRate() int { return d.rate }

// Channels returns the channel count of the track.
func (d *Decoder) Channels() int { return d.ch }

// Len returns the frames from the start to the end of the track, or -1 when the
// stream does not say. It is the presented length: a container's priming is not
// part of it, so it matches what Read hands out.
func (d *Decoder) Len() int64 { return d.total }

// BitsPerSample returns the track's sample size, which is what Read divides by.
func (d *Decoder) BitsPerSample() int { return d.bits }

// Close releases the decoder and, for one OpenFile opened, the file under it.
// It is safe to call more than once. MyGo calls it when a player closes.
func (d *Decoder) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	err := d.src.close()
	if d.closer != nil {
		if cerr := d.closer.Close(); err == nil {
			err = cerr
		}
		d.closer = nil
	}
	return err
}

// Read decodes the next samples into dst and returns how many it wrote, which is
// a whole number of frames: L0,R0,L1,R1,... for stereo. It returns io.EOF at
// the end of the track, with a short count if the track ended inside dst.
func (d *Decoder) Read(dst []float32) (int, error) {
	if d.closed {
		return 0, errClosed
	}
	if d.pending != nil {
		return 0, d.pending
	}
	// A decoder seeked to its own end has nothing left, and saying so before
	// decoding keeps a later read from handing out the frame after the one the
	// seek skipped past.
	if d.atEnd {
		return 0, io.EOF
	}
	want := len(dst) / d.ch * d.ch
	n := 0
	for n < want {
		if d.head >= len(d.buf) {
			if err := d.refill(); err != nil {
				if err == io.EOF {
					return n, io.EOF
				}
				// A stream that broke stays broken: a caller that keeps reading
				// must not be handed silence that looks like the track.
				d.pending = err
				return n, err
			}
		}
		m := copy(dst[n:want], d.buf[d.head:])
		if m == 0 {
			// refill leaves at least one sample to hand out unless it reported the
			// end of the track, and it did not. A source that is not making progress
			// is an error rather than a spin.
			return n, io.ErrNoProgress
		}
		d.head += m
		n += m
	}
	return n, nil
}

// refill decodes the next frame into d.buf and positions d.head at the first
// sample to hand out. On return d.head is inside d.buf, unless the track ended,
// which it reports as io.EOF.
//
// A seek can name a frame boundary that is further back than the target, and then
// whole frames have to be dropped before the first sample to hand out. That is
// why this loops: it returns only once there is something to give back.
func (d *Decoder) refill() error {
	for {
		f, samples, err := d.src.nextFrame()
		if err != nil {
			if err != io.EOF {
				return err
			}
			d.buf, d.head = d.buf[:0], 0
			if err := d.checkComplete(); err != nil {
				return err
			}
			return io.EOF
		}
		if err := d.checkFrame(f, samples); err != nil {
			return err
		}
		d.decode(f, samples)
		if d.skip >= samples {
			// The target is beyond this frame, so drop all of it. The frames it
			// carried are before the seek point and are not part of the position.
			d.skip -= samples
			d.head = 0
			d.buf = d.buf[:0]
			continue
		}
		drop := d.skip
		d.skip = 0
		d.pos += int64(samples - drop)
		d.head = drop * d.ch
		return nil
	}
}

// checkComplete compares what the source has yielded against the length it
// promised. It only speaks for a stream read from its start: after a seek, pos
// counts from the seek point, where adding the seek target back would need a
// target exact to the sample, and a container seek is only exact to its tick.
func (d *Decoder) checkComplete() error {
	if d.seeked || d.total < 0 {
		return nil
	}
	if d.pos != d.total {
		return fmt.Errorf("flacdec: stream ended after %d of %d frames: %w", d.pos, d.total, ErrTruncated)
	}
	return nil
}

// checkFrame rejects a frame that disagrees with the stream it is in.
func (d *Decoder) checkFrame(f *frame.Frame, samples int) error {
	if got := f.Channels.Count(); got != d.ch {
		return fmt.Errorf("flacdec: frame has %d channels, stream has %d", got, d.ch)
	}
	// A frame may state its own sample size, or leave it to STREAMINFO; either way
	// it has to be the stream's, or the samples mean different scales.
	if f.BitsPerSample != 0 && int(f.BitsPerSample) != d.bits {
		return fmt.Errorf("flacdec: frame is %d bits, stream is %d", f.BitsPerSample, d.bits)
	}
	if f.SampleRate != 0 && int(f.SampleRate) != d.rate {
		return fmt.Errorf("flacdec: frame is %d Hz, stream is %d Hz", f.SampleRate, d.rate)
	}
	if len(f.Subframes) != d.ch {
		return fmt.Errorf("flacdec: frame has %d subframes, stream has %d channels", len(f.Subframes), d.ch)
	}
	for c, sf := range f.Subframes {
		if len(sf.Samples) != samples {
			return fmt.Errorf("flacdec: channel %d has %d samples, channel 0 has %d", c, len(sf.Samples), samples)
		}
	}
	if samples <= 0 {
		return errors.New("flacdec: frame of no samples")
	}
	return nil
}

// decode writes the frame into d.buf, interleaved and scaled to [-1, 1].
//
// The order is the whole point. A FLAC frame holds one subframe per channel and
// each subframe holds one channel's samples for the whole block: the frame is
// planar, L0,L1,...,LN-1 then R0,R1,...,RN-1. MyGo's audio.Decoder wants frames
// of interleaved samples, L0,R0,L1,R1,..., so the innermost loop must run over
// the channels and the outermost over the samples. Appending subframe by
// subframe instead -- which is what the t-0013 reference implementation did --
// hands the mixer half a second of left channel followed by half a second of
// right: a phase inversion of everything after the first half block, audible as
// a widening that turns into a wobble. It is easy to miss, because a stereo
// signal whose two channels are nearly identical (most music) sounds almost
// right, and an RMS comparison does not notice at all.
func (d *Decoder) decode(f *frame.Frame, samples int) {
	need := samples * d.ch
	if cap(d.buf) < need {
		d.buf = make([]float32, 0, need)
	}
	out := d.buf[:0]
	for i := 0; i < samples; i++ {
		for c := 0; c < d.ch; c++ {
			out = append(out, float32(f.Subframes[c].Samples[i])*d.scale)
		}
	}
	d.buf = out
	d.head = 0
}

// SetPosition moves to frame, counted from the start of the presented audio. It
// may be called at any time, including after the end, and seeking to Len() is
// allowed and makes the next Read report io.EOF.
func (d *Decoder) SetPosition(frame int64) error {
	if d.closed {
		return errClosed
	}
	if d.pending != nil {
		return d.pending
	}
	if frame < 0 || d.total >= 0 && frame > d.total {
		return fmt.Errorf("flacdec: frame %d outside [0, %d]: %w", frame, d.total, ErrSeekRange)
	}
	d.buf, d.head = d.buf[:0], 0
	d.atEnd = frame == d.total
	if d.atEnd {
		// The container cannot seek to its own end: SeekTime refuses a time at or
		// past the presented window. Answering with a decoder that reads as ended
		// keeps the contract the same as WAV's, which seeks to its length fine.
		d.skip, d.pos = 0, 0
		return nil
	}
	skip, err := d.src.seekTo(frame)
	if err != nil {
		return err
	}
	d.skip, d.pos, d.seeked = skip, 0, true
	return nil
}

// sampleScale returns the factor that maps a sample of bps bits to [-1, 1]. It is
// 1/(1<<(bps-1)), the same convention MyGo's own WAV decoder uses, so a WAV and
// a FLAC of the same recording land on the same float32.
func sampleScale(bps int) float32 {
	if bps < 1 || bps > 32 {
		return 0
	}
	return 1 / float32(int64(1)<<uint(bps-1))
}

// presentedFrames converts a container's presented duration into frames.
//
// go-m4a reports the window in ticks scaled to nanoseconds with the remainder
// truncated, so dividing back by a second lands a fraction of a frame short: for
// a window of exactly 72073 frames at 48 kHz it reports 1501520833 ns, and
// 1501520833*48000/1e9 is 72072.99984. Rounding up recovers the exact count,
// because a truncated nanosecond count is always less than one frame when the
// media timescale is the sample rate, which is every file the package writes.
func presentedFrames(d time.Duration, rate int) int64 {
	if d <= 0 || rate <= 0 {
		return -1
	}
	ns := int64(d)
	if ns > math.MaxInt64/int64(rate) {
		return -1
	}
	return (ns*int64(rate) + int64(time.Second) - 1) / int64(time.Second)
}
