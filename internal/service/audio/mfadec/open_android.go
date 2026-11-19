//go:build android && cgo

package mfadec

/*
#cgo LDFLAGS: -lmediandk
#include <media/NdkMediaCodec.h>

static const char *aac_mime = "audio/mp4a-latm";
enum { pcm_16_bit = 2 };

static AMediaCodec *create_aac_decoder(void) {
	return AMediaCodec_createDecoderByType(aac_mime);
}

static media_status_t configure_aac_decoder(AMediaCodec *codec, int rate, int channels,
		const void *asc, size_t asc_size, bool adts) {
	AMediaFormat *format = AMediaFormat_new();
	if (format == NULL) {
		return AMEDIA_ERROR_UNKNOWN;
	}
	AMediaFormat_setString(format, AMEDIAFORMAT_KEY_MIME, aac_mime);
	AMediaFormat_setInt32(format, AMEDIAFORMAT_KEY_SAMPLE_RATE, rate);
	AMediaFormat_setInt32(format, AMEDIAFORMAT_KEY_CHANNEL_COUNT, channels);
	AMediaFormat_setInt32(format, "pcm-encoding", pcm_16_bit);
	AMediaFormat_setBuffer(format, "csd-0", asc, asc_size);
	if (adts) {
		AMediaFormat_setInt32(format, AMEDIAFORMAT_KEY_IS_ADTS, 1);
	}
	media_status_t status = AMediaCodec_configure(codec, format, NULL, NULL, 0);
	AMediaFormat_delete(format);
	return status;
}

static bool get_pcm_format(AMediaCodec *codec, int32_t *rate, int32_t *channels, int32_t *encoding) {
	AMediaFormat *format = AMediaCodec_getOutputFormat(codec);
	if (format == NULL) {
		return false;
	}
	bool ok = AMediaFormat_getInt32(format, AMEDIAFORMAT_KEY_SAMPLE_RATE, rate) &&
		AMediaFormat_getInt32(format, AMEDIAFORMAT_KEY_CHANNEL_COUNT, channels);
	if (!AMediaFormat_getInt32(format, "pcm-encoding", encoding)) {
		*encoding = pcm_16_bit;
	}
	AMediaFormat_delete(format);
	return ok;
}
*/
import "C"

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
	"unsafe"
)

const (
	codecKeepsPriming = true
	codecWaitTimeout  = 2 * time.Second
)

type androidCodec struct {
	codec        *C.AMediaCodec
	rate, chans  int
	unitLen      int
	queuedFrames int64
	pcm          []float32
}

func platformAvailable() error { return nil }

func openCodec(src *frameSource) (aacCodec, error) {
	if src.rate <= 0 || src.chans <= 0 || len(src.asc) == 0 {
		return nil, fmt.Errorf("mfadec: Android AAC: invalid input format: %w", ErrFormat)
	}
	codec := C.create_aac_decoder()
	if codec == nil {
		return nil, fmt.Errorf("mfadec: Android AAC: MediaCodec could not create a decoder: %w", ErrUnsupported)
	}
	status := C.configure_aac_decoder(codec, C.int(src.rate), C.int(src.chans),
		unsafe.Pointer(&src.asc[0]), C.size_t(len(src.asc)), C.bool(src.payloadType == aacPayloadADTS))
	if status != C.AMEDIA_OK {
		C.AMediaCodec_delete(codec)
		return nil, fmt.Errorf("mfadec: Android AAC: configure: MediaCodec status %d", status)
	}
	if status := C.AMediaCodec_start(codec); status != C.AMEDIA_OK {
		C.AMediaCodec_delete(codec)
		return nil, fmt.Errorf("mfadec: Android AAC: start: MediaCodec status %d", status)
	}
	return &androidCodec{codec: codec, rate: src.rate, chans: src.chans, unitLen: src.unitLen}, nil
}

func (c *androidCodec) SampleRate() int { return c.rate }

func (c *androidCodec) Channels() int { return c.chans }

func (c *androidCodec) Close() error {
	if c.codec == nil {
		return nil
	}
	status := C.AMediaCodec_delete(c.codec)
	c.codec = nil
	if status != C.AMEDIA_OK {
		return fmt.Errorf("mfadec: Android AAC: release decoder: MediaCodec status %d", status)
	}
	return nil
}

func (c *androidCodec) Feed(frame []byte) ([]float32, error) {
	return c.process(frame, false)
}

func (c *androidCodec) Drain() ([]float32, error) {
	return c.process(nil, true)
}

func (c *androidCodec) process(frame []byte, drain bool) ([]float32, error) {
	c.pcm = c.pcm[:0]
	inputDeadline := time.Now().Add(codecWaitTimeout)
	for {
		remaining := min(time.Until(inputDeadline), 10*time.Millisecond)
		if remaining <= 0 {
			return nil, errors.New("mfadec: Android AAC: timed out waiting for a MediaCodec input buffer")
		}
		index := C.AMediaCodec_dequeueInputBuffer(c.codec, C.int64_t(remaining.Microseconds()))
		if index >= 0 {
			if err := c.queueInput(C.size_t(index), frame, drain); err != nil {
				return nil, err
			}
			if err := c.pullOutput(drain); err != nil {
				return nil, err
			}
			return c.pcm, nil
		}
		if index != C.AMEDIACODEC_INFO_TRY_AGAIN_LATER {
			return nil, fmt.Errorf("mfadec: Android AAC: dequeue input: MediaCodec status %d", index)
		}
		if err := c.pullOutput(false); err != nil {
			return nil, err
		}
	}
}

func (c *androidCodec) queueInput(index C.size_t, frame []byte, drain bool) error {
	var flags C.uint32_t
	if drain {
		flags = C.AMEDIACODEC_BUFFER_FLAG_END_OF_STREAM
	} else {
		var capacity C.size_t
		input := C.AMediaCodec_getInputBuffer(c.codec, index, &capacity)
		if input == nil || C.size_t(len(frame)) > capacity {
			return fmt.Errorf("mfadec: Android AAC: input buffer cannot hold a %d-byte frame", len(frame))
		}
		copy(unsafe.Slice((*byte)(unsafe.Pointer(input)), len(frame)), frame)
	}
	pts := C.uint64_t(c.queuedFrames * int64(time.Second/time.Microsecond) / int64(c.rate))
	status := C.AMediaCodec_queueInputBuffer(c.codec, index, 0, C.size_t(len(frame)), pts, flags)
	if status != C.AMEDIA_OK {
		return fmt.Errorf("mfadec: Android AAC: queue input: MediaCodec status %d", status)
	}
	c.queuedFrames += int64(c.unitLen)
	return nil
}

func (c *androidCodec) pullOutput(drain bool) error {
	deadline := time.Now().Add(codecWaitTimeout)
	for {
		var wait C.int64_t
		if drain {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return errors.New("mfadec: Android AAC: timed out waiting for MediaCodec end of stream")
			}
			wait = C.int64_t(remaining.Microseconds())
		}
		var info C.AMediaCodecBufferInfo
		index := C.AMediaCodec_dequeueOutputBuffer(c.codec, &info, wait)
		switch {
		case index >= 0:
			if err := c.readOutput(C.size_t(index), &info); err != nil {
				return err
			}
			if info.flags&C.AMEDIACODEC_BUFFER_FLAG_END_OF_STREAM != 0 {
				if !drain {
					return errors.New("mfadec: Android AAC: unexpected MediaCodec end of stream")
				}
				return nil
			}
		case index == C.AMEDIACODEC_INFO_OUTPUT_FORMAT_CHANGED:
			if err := c.checkOutputFormat(); err != nil {
				return err
			}
		case index == C.AMEDIACODEC_INFO_OUTPUT_BUFFERS_CHANGED:
		case index == C.AMEDIACODEC_INFO_TRY_AGAIN_LATER:
			if !drain {
				return nil
			}
		default:
			return fmt.Errorf("mfadec: Android AAC: dequeue output: MediaCodec status %d", index)
		}
	}
}

func (c *androidCodec) checkOutputFormat() error {
	var rate, chans, encoding C.int32_t
	if !C.get_pcm_format(c.codec, &rate, &chans, &encoding) {
		return errors.New("mfadec: Android AAC: MediaCodec did not report its output format")
	}
	if int(rate) != c.rate || int(chans) != c.chans || encoding != C.pcm_16_bit {
		return fmt.Errorf("mfadec: Android AAC: unexpected PCM format: %d Hz, %d channels, encoding %d (want %d Hz, %d channels, 16-bit PCM)",
			rate, chans, encoding, c.rate, c.chans)
	}
	return nil
}

func (c *androidCodec) readOutput(index C.size_t, info *C.AMediaCodecBufferInfo) error {
	var readErr error
	size := int(info.size)
	if size != 0 && info.flags&C.AMEDIACODEC_BUFFER_FLAG_CODEC_CONFIG == 0 {
		// API 21–35 only reports a valid byte count in AMediaCodecBufferInfo.size.
		output := C.AMediaCodec_getOutputBuffer(c.codec, index, nil)
		if output == nil || size < 0 || size%(2*c.chans) != 0 {
			readErr = fmt.Errorf("mfadec: Android AAC: invalid %d-byte MediaCodec PCM buffer", size)
		} else {
			pcm := unsafe.Slice((*byte)(unsafe.Pointer(output)), size)
			for sample := 0; sample < len(pcm); sample += 2 {
				c.pcm = append(c.pcm, float32(int16(binary.LittleEndian.Uint16(pcm[sample:])))/32768)
			}
		}
	}
	status := C.AMediaCodec_releaseOutputBuffer(c.codec, index, false)
	if status != C.AMEDIA_OK {
		return errors.Join(readErr, fmt.Errorf("mfadec: Android AAC: release output buffer: MediaCodec status %d", status))
	}
	return readErr
}
