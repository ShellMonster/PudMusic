package flacdec

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"sort"

	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"
)

// checkpointSeconds is how far apart the frame boundaries a bare-FLAC seek may
// land on are. The index holds one entry per this many frames of audio: a 4
// minute track is 240 entries and 4 KB, and a backward seek costs at most a
// second of decoding, about 4 ms. Sequential playback fills the index in as it
// goes, so only the first seek into ground nobody has played yet pays for a walk.
const checkpointSeconds = 1

// nativeSource reads a bare ".flac" stream: the "fLaC" marker, then the metadata
// blocks, then the frames back to back.
//
// This is the shape a user's local music is in, and it is the half the t-0013
// reference implementation did not have: it went straight to m4a.NewReader, which
// reports ErrFormat on anything that is not an ISO-BMFF file.
type nativeSource struct {
	r  io.ReadSeeker
	br *bufio.Reader
	si meta.StreamInfo

	first  int64 // file offset of the first frame header
	points []meta.SeekPoint
	// usePoints is whether the seek table can still be believed. A stream whose
	// seek points do not hold is read the slow way from then on.
	usePoints bool

	index    []checkpoint // frame boundaries, ascending by sample
	nextMark int64        // the sample the next checkpoint is due at

	origin int64 // the sample the next frame starts at
	offset int64 // that frame's file offset
}

// checkpoint is a frame boundary a seek can start from.
type checkpoint struct {
	sample, offset int64
}

func openNative(r io.ReadSeeker) (*Decoder, error) {
	s := &nativeSource{r: r, br: bufio.NewReaderSize(r, 64<<10), usePoints: true}
	if err := s.readMetadata(); err != nil {
		return nil, err
	}
	bits := int(s.si.BitsPerSample)
	if s.si.NChannels == 0 || s.si.SampleRate == 0 {
		return nil, fmt.Errorf("%w: STREAMINFO says %d channels at %d Hz",
			ErrFormat, s.si.NChannels, s.si.SampleRate)
	}
	if bits < 1 || bits > 32 {
		return nil, fmt.Errorf("%w: %d bits per sample", ErrFormat, bits)
	}
	d := &Decoder{
		src:   s,
		ch:    int(s.si.NChannels),
		rate:  int(s.si.SampleRate),
		bits:  bits,
		scale: sampleScale(bits),
	}
	// A stream that is still being written says zero total samples. That is
	// unknown, not empty: reporting 0 would make every read of it look truncated.
	if s.si.NSamples > 0 {
		d.total = int64(s.si.NSamples)
	} else {
		d.total = -1
	}
	s.place(0, s.first)
	return d, nil
}

// readMetadata walks the metadata blocks. The first is STREAMINFO and is
// mandatory; the rest are read only for the SEEKTABLE, and anything unrecognised
// is stepped over. Only the blocks that matter are read into memory: a PADDING
// block, often the largest thing in the header, is skipped by seeking.
func (s *nativeSource) readMetadata() error {
	var magic [4]byte
	if _, err := io.ReadFull(s.br, magic[:]); err != nil {
		return fmt.Errorf("flacdec: reading fLaC marker: %w: %w", err, ErrTruncated)
	}
	if string(magic[:]) != "fLaC" {
		return fmt.Errorf("%w: starts with %q", ErrFormat, magic)
	}
	first := true
	for {
		var h [4]byte
		if _, err := io.ReadFull(s.br, h[:]); err != nil {
			// Metadata ends with a last-block flag and at least one frame follows
			// it, so running out here is always a cut-off file.
			return fmt.Errorf("flacdec: reading metadata block header: %w: %w", err, ErrTruncated)
		}
		last := h[0]&0x80 != 0
		typ := meta.Type(h[0] & 0x7f)
		n := int64(h[1])<<16 | int64(h[2])<<8 | int64(h[3])
		switch {
		case first && typ != meta.TypeStreamInfo:
			return fmt.Errorf("%w: first metadata block is %v, not stream info", ErrFormat, typ)
		case typ == meta.TypeStreamInfo && s.si.NChannels == 0:
			if err := s.parseStreamInfo(h[:], n); err != nil {
				return err
			}
		case typ == meta.TypeSeekTable && len(s.points) == 0:
			// A seek table that does not parse is not fatal: the stream still
			// plays, just with the index as its only way to seek.
			if err := s.parseSeekTable(h[:], n); err != nil {
				if err := s.skip(n); err != nil {
					return fmt.Errorf("flacdec: skipping seek table: %w: %w", err, ErrTruncated)
				}
			}
		default:
			if err := s.skip(n); err != nil {
				return fmt.Errorf("flacdec: skipping %v block: %w: %w", typ, err, ErrTruncated)
			}
		}
		first = false
		if last {
			break
		}
	}
	pos, err := s.logical()
	if err != nil {
		return err
	}
	s.first = pos
	return nil
}

// parseStreamInfo reads the mandatory first block through mewkiz/flac's meta
// package, which checks the block's length and the fields the format constrains.
func (s *nativeSource) parseStreamInfo(hdr []byte, n int64) error {
	block, err := readBlock(s.br, hdr, n)
	if err != nil {
		return fmt.Errorf("flacdec: reading stream info: %w: %w", err, ErrTruncated)
	}
	parsed, err := meta.Parse(bytes.NewReader(block))
	if err != nil {
		return fmt.Errorf("flacdec: stream info: %w", err)
	}
	si, ok := parsed.Body.(*meta.StreamInfo)
	if !ok {
		return fmt.Errorf("flacdec: stream info block parsed as %T", parsed.Body)
	}
	s.si = *si
	return nil
}

// parseSeekTable reads the optional seek points, which let a seek land on a frame
// without decoding anything before it.
func (s *nativeSource) parseSeekTable(hdr []byte, n int64) error {
	if n < 18 || n%18 != 0 {
		return fmt.Errorf("flacdec: seek table of %d bytes", n)
	}
	block, err := readBlock(s.br, hdr, n)
	if err != nil {
		return err
	}
	parsed, err := meta.Parse(bytes.NewReader(block))
	if err != nil {
		return err
	}
	table, ok := parsed.Body.(*meta.SeekTable)
	if !ok {
		return fmt.Errorf("flacdec: seek table block parsed as %T", parsed.Body)
	}
	// Placeholder points carry no sample number. Drop them and keep the rest in
	// ascending order, which is what the binary search below needs.
	points := make([]meta.SeekPoint, 0, len(table.Points))
	for _, p := range table.Points {
		if p.SampleNum != meta.PlaceholderPoint {
			points = append(points, p)
		}
	}
	s.points = points
	return nil
}

// readBlock returns a metadata block's header and body together, which is the
// shape meta.Parse reads.
func readBlock(br *bufio.Reader, hdr []byte, n int64) ([]byte, error) {
	if n < 0 || n > 1<<22 {
		return nil, fmt.Errorf("metadata block of %d bytes", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(br, body); err != nil {
		return nil, err
	}
	return append(append([]byte{}, hdr...), body...), nil
}

// logical returns the offset the stream is at, which is behind the buffered
// reader's own offset by however much it has read ahead.
func (s *nativeSource) logical() (int64, error) {
	pos, err := s.r.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	return pos - int64(s.br.Buffered()), nil
}

// skip steps over n bytes, seeking past them rather than reading them: a PADDING
// block can be megabytes and means nothing.
func (s *nativeSource) skip(n int64) error {
	if n == 0 {
		return nil
	}
	pos, err := s.logical()
	if err != nil {
		return err
	}
	if pos += n; pos < 0 {
		return fmt.Errorf("offset %d before the start of the stream", pos)
	}
	if _, err := s.r.Seek(pos, io.SeekStart); err != nil {
		return err
	}
	s.br.Reset(s.r)
	return nil
}

// step is the checkpoint interval in samples.
func (s *nativeSource) step() int64 {
	if n := int64(s.si.SampleRate) * checkpointSeconds; n > 0 {
		return n
	}
	return 1
}

// place puts the stream on the frame that starts at sample, at file offset
// offset, and lines the index up to record boundaries from there on.
func (s *nativeSource) place(sample, offset int64) {
	s.origin, s.offset = sample, offset
	if len(s.index) == 0 {
		s.index = append(s.index, checkpoint{sample, offset})
	}
	s.nextMark = (sample/s.step() + 1) * s.step()
}

// index records a frame boundary if the index has not reached it yet. Entries
// stay ascending by sample, which is what lets a seek binary search them, and a
// boundary the walk crosses twice is kept once.
func (s *nativeSource) index_(sample, offset int64) {
	if sample < s.nextMark {
		return
	}
	s.index = append(s.index, checkpoint{sample, offset})
	s.nextMark = (sample/s.step() + 1) * s.step()
}

// nextFrame parses the next frame. It reports io.EOF only when the stream runs
// out cleanly on a frame boundary; a frame cut in half comes back as an error
// from mewkiz/flac, which is a bad sync code, a CRC-8, a CRC-16 or
// io.ErrUnexpectedEOF.
func (s *nativeSource) nextFrame() (*frame.Frame, int, error) {
	off, err := s.logical()
	if err != nil {
		return nil, 0, err
	}
	f, err := frame.Parse(s.br)
	if err != nil {
		if err == io.EOF {
			return nil, 0, io.EOF
		}
		return nil, 0, fmt.Errorf("flacdec: frame at offset %d: %w", off, err)
	}
	if len(f.Subframes) == 0 {
		return nil, 0, fmt.Errorf("flacdec: frame at offset %d has no subframes", off)
	}
	// A boundary can only be recorded before its frame is parsed, from the
	// position the previous one left.
	s.index_(s.origin, off)
	samples := len(f.Subframes[0].Samples)
	s.origin += int64(samples)
	s.offset = off
	return f, samples, nil
}

// seekTo moves to the frame holding sample and reports how many samples at its
// start to drop.
//
// Two ways to land there. A stream with a SEEKTABLE names its frame boundaries,
// so the seek is a file seek and nothing is decoded. A stream without one has to
// be walked: the newest index entry at or before the target, then frames decoded
// and dropped on the way. The walk fills the index as it goes, so it is paid
// once per stretch of ground rather than once per seek.
func (s *nativeSource) seekTo(sample int64) (int, error) {
	if sample < 0 {
		return 0, fmt.Errorf("flacdec: frame %d: %w", sample, ErrSeekRange)
	}
	if p, ok := s.seekPointFor(sample); ok {
		// A seek table is a claim by whoever wrote the file, not a promise: points
		// past the end of the audio are common, and points with a wrong offset or a
		// wrong sample number turn up. Parsing the frame the point names settles
		// both -- the CRC-8 catches a boundary that is not one, and the frame's own
		// sample number catches a point that names the right frame but the wrong
		// place in it. One bad point costs a walk, and the table is not trusted
		// again.
		if err := s.seekToOffset(int64(p.SampleNum), s.first+int64(p.Offset)); err == nil {
			if err := s.confirmBoundary(int64(p.SampleNum)); err == nil {
				return int(sample - int64(p.SampleNum)), nil
			}
		}
		s.usePoints = false
	}
	c, ok := s.checkpointFor(sample)
	if !ok {
		c = checkpoint{0, s.first}
	}
	if err := s.seekToOffset(c.sample, c.offset); err != nil {
		return 0, err
	}
	return int(sample - c.sample), nil
}

// confirmBoundary parses the frame at the current position, checks that it is a
// frame of this stream that starts at want, and puts the stream back where it
// was. The frame is parsed twice, once to check and once to hand out: a seek is
// rare next to a decode, and a frame is a fraction of a millisecond.
func (s *nativeSource) confirmBoundary(want int64) error {
	f, err := frame.Parse(s.br)
	if err != nil {
		return err
	}
	if got := f.Channels.Count(); got != int(s.si.NChannels) {
		return fmt.Errorf("frame at offset %d has %d channels, stream has %d", s.offset, got, s.si.NChannels)
	}
	// A frame's own sample number is the only way to tell whether a seek point's
	// SampleNum means anything. SampleNumber reads the header's own rule: a frame
	// number times the block size when the block size is fixed, the sample number
	// itself when it is not. Reading the number and multiplying by hand is what
	// gets the last frame of a stream wrong.
	if got := f.SampleNumber(); got != uint64(want) {
		return fmt.Errorf("frame at offset %d starts at sample %d, the seek point says %d", s.offset, got, want)
	}
	if _, err := s.r.Seek(s.offset, io.SeekStart); err != nil {
		return err
	}
	s.br.Reset(s.r)
	return nil
}

// seekToOffset puts the stream on the frame that starts at sample, at file offset
// offset. It does no I/O beyond the seek: whether the bytes there really are a
// frame boundary is settled by the parser, on the next nextFrame for an indexed
// boundary and by confirmBoundary for one a seek table named.
func (s *nativeSource) seekToOffset(sample, offset int64) error {
	if offset < s.first {
		return fmt.Errorf("%w: frame boundary %d before the first frame at %d", ErrFormat, offset, s.first)
	}
	if _, err := s.r.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	s.br.Reset(s.r)
	s.place(sample, offset)
	return nil
}

// seekPointFor returns the newest seek point at or before sample.
func (s *nativeSource) seekPointFor(sample int64) (meta.SeekPoint, bool) {
	if !s.usePoints || len(s.points) == 0 {
		return meta.SeekPoint{}, false
	}
	i := sort.Search(len(s.points), func(i int) bool { return s.points[i].SampleNum > uint64(sample) })
	if i == 0 {
		// The target is before the first point, so the first frame is the answer.
		return meta.SeekPoint{}, true
	}
	return s.points[i-1], true
}

// checkpointFor returns the newest indexed boundary at or before sample.
func (s *nativeSource) checkpointFor(sample int64) (checkpoint, bool) {
	if len(s.index) == 0 {
		return checkpoint{}, false
	}
	i := sort.Search(len(s.index), func(i int) bool { return s.index[i].sample > sample })
	if i == 0 {
		return checkpoint{}, false
	}
	return s.index[i-1], true
}

func (s *nativeSource) close() error { return nil }
