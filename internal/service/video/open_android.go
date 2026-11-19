//go:build android && cgo

package video

/*
#cgo LDFLAGS: -lmediandk
#include <media/NdkImage.h>
#include <media/NdkImageReader.h>
#include <media/NdkMediaCodec.h>
#include <media/NdkMediaExtractor.h>
#include <fcntl.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

typedef struct {
	AMediaExtractor *extractor;
	AMediaCodec *codec;
	AImageReader *reader;
} Decoder;

static void close_decoder(Decoder *decoder) {
	if (decoder->codec) AMediaCodec_delete(decoder->codec);
	if (decoder->reader) AImageReader_delete(decoder->reader);
	if (decoder->extractor) AMediaExtractor_delete(decoder->extractor);
	free(decoder);
}

static Decoder *open_decoder(const char *path) {
	Decoder *decoder = calloc(1, sizeof(Decoder));
	if (!decoder) return NULL;
	decoder->extractor = AMediaExtractor_new();
	int file = open(path, O_RDONLY | O_CLOEXEC);
	struct stat metadata;
	bool source_ready = decoder->extractor && file >= 0 && fstat(file, &metadata) == 0 &&
		AMediaExtractor_setDataSourceFd(decoder->extractor, file, 0, metadata.st_size) == AMEDIA_OK;
	if (file >= 0) close(file);
	if (!source_ready) {
		close_decoder(decoder);
		return NULL;
	}
	for (size_t index = 0; index < AMediaExtractor_getTrackCount(decoder->extractor); ++index) {
		AMediaFormat *format = AMediaExtractor_getTrackFormat(decoder->extractor, index);
		if (!format) continue;
		const char *mime;
		int32_t width, height;
		bool usable = AMediaFormat_getString(format, AMEDIAFORMAT_KEY_MIME, &mime) &&
			strncmp(mime, "video/", 6) == 0 &&
			AMediaFormat_getInt32(format, AMEDIAFORMAT_KEY_WIDTH, &width) &&
			AMediaFormat_getInt32(format, AMEDIAFORMAT_KEY_HEIGHT, &height) &&
			width > 0 && height > 0;
		if (!usable) {
			AMediaFormat_delete(format);
			continue;
		}
		ANativeWindow *surface = NULL;
		bool ready = AMediaExtractor_selectTrack(decoder->extractor, index) == AMEDIA_OK &&
			AImageReader_new(width, height, AIMAGE_FORMAT_YUV_420_888, 2, &decoder->reader) == AMEDIA_OK &&
			AImageReader_getWindow(decoder->reader, &surface) == AMEDIA_OK;
		if (ready) decoder->codec = AMediaCodec_createDecoderByType(mime);
		ready = decoder->codec && AMediaCodec_configure(decoder->codec, format, surface, NULL, 0) == AMEDIA_OK &&
			AMediaCodec_start(decoder->codec) == AMEDIA_OK;
		AMediaFormat_delete(format);
		if (ready) return decoder;
		break;
	}
	close_decoder(decoder);
	return NULL;
}

static int push_sample(Decoder *decoder) {
	ssize_t slot = AMediaCodec_dequeueInputBuffer(decoder->codec, 0);
	if (slot < 0) return slot == AMEDIACODEC_INFO_TRY_AGAIN_LATER ? 0 : -1;
	size_t capacity;
	uint8_t *buffer = AMediaCodec_getInputBuffer(decoder->codec, slot, &capacity);
	if (!buffer) return -1;
	ssize_t count = AMediaExtractor_readSampleData(decoder->extractor, buffer, capacity);
	if (count < 0) {
		return AMediaCodec_queueInputBuffer(decoder->codec, slot, 0, 0, 0, AMEDIACODEC_BUFFER_FLAG_END_OF_STREAM) == AMEDIA_OK ? 2 : -1;
	}
	if ((size_t)count > capacity || AMediaCodec_queueInputBuffer(decoder->codec, slot, 0, count,
		AMediaExtractor_getSampleTime(decoder->extractor), 0) != AMEDIA_OK) return -1;
	AMediaExtractor_advance(decoder->extractor);
	return 1;
}

typedef struct {
	uint8_t *data;
	int length, row, pixel;
} Plane;

static bool plane_data(AImage *image, int index, Plane *plane) {
	return AImage_getPlaneData(image, index, &plane->data, &plane->length) == AMEDIA_OK &&
		AImage_getPlaneRowStride(image, index, &plane->row) == AMEDIA_OK &&
		AImage_getPlanePixelStride(image, index, &plane->pixel) == AMEDIA_OK &&
		plane->length > 0 && plane->row > 0 && plane->pixel > 0;
}

static uint8_t color_byte(int value) {
	return value < 0 ? 0 : value > 255 ? 255 : (uint8_t)value;
}

static bool copy_image(AImage *image, AImageCropRect crop, uint8_t *rgba) {
	Plane y, u, v;
	if (!plane_data(image, 0, &y) || !plane_data(image, 1, &u) || !plane_data(image, 2, &v)) return false;
	int right = crop.right - 1, bottom = crop.bottom - 1;
	if ((int64_t)bottom*y.row + (int64_t)right*y.pixel >= y.length ||
		(int64_t)(bottom/2)*u.row + (int64_t)(right/2)*u.pixel >= u.length ||
		(int64_t)(bottom/2)*v.row + (int64_t)(right/2)*v.pixel >= v.length) return false;
	for (int row = crop.top; row < crop.bottom; ++row) {
		for (int col = crop.left; col < crop.right; ++col) {
			int light = 298 * (y.data[row*y.row + col*y.pixel] - 16);
			int cb = u.data[(row/2)*u.row + (col/2)*u.pixel] - 128;
			int cr = v.data[(row/2)*v.row + (col/2)*v.pixel] - 128;
			rgba[0] = color_byte((light + 459*cr + 128) >> 8);
			rgba[1] = color_byte((light - 55*cb - 136*cr + 128) >> 8);
			rgba[2] = color_byte((light + 541*cb + 128) >> 8);
			rgba[3] = 255;
			rgba += 4;
		}
	}
	return true;
}
*/
import "C"

import (
	"errors"
	"time"
	"unsafe"
)

const supported = true

func Open(path string, pool BufferPool) (*Source, error) {
	name := C.CString(path)
	decoder := C.open_decoder(name)
	C.free(unsafe.Pointer(name))
	if decoder == nil {
		return nil, errors.New("video: Android 无法打开视频轨")
	}
	source := newSource(pool)
	go source.decodeAndroid(decoder)
	return source, nil
}

func (s *Source) decodeAndroid(decoder *C.Decoder) {
	defer close(s.done)
	defer close(s.frames)
	defer C.close_decoder(decoder)
	inputEnded := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-s.quit:
			return
		default:
		}
		if !inputEnded {
			pushed := C.push_sample(decoder)
			if pushed < 0 {
				s.fail(errors.New("video: Android 无法读取视频样本"))
				return
			}
			inputEnded = pushed == 2
		}
		var info C.AMediaCodecBufferInfo
		slot := C.AMediaCodec_dequeueOutputBuffer(decoder.codec, &info, 10000)
		if slot < 0 {
			switch slot {
			case C.AMEDIACODEC_INFO_TRY_AGAIN_LATER, C.AMEDIACODEC_INFO_OUTPUT_FORMAT_CHANGED, C.AMEDIACODEC_INFO_OUTPUT_BUFFERS_CHANGED:
				continue
			default:
				s.fail(errors.New("video: Android 视频解码失败"))
				return
			}
		}
		end := info.flags&C.AMEDIACODEC_BUFFER_FLAG_END_OF_STREAM != 0
		render := info.size > 0 && info.flags&C.AMEDIACODEC_BUFFER_FLAG_CODEC_CONFIG == 0
		if C.AMediaCodec_releaseOutputBuffer(decoder.codec, C.size_t(slot), C.bool(render)) != C.AMEDIA_OK {
			s.fail(errors.New("video: Android 无法输出视频画面"))
			return
		}
		if render && !s.readAndroidFrame(decoder.reader, time.Duration(info.presentationTimeUs)*time.Microsecond) {
			return
		}
		if end {
			return
		}
		deadline = time.Now().Add(3 * time.Second)
	}
	s.fail(errors.New("video: Android 视频解码超时"))
}

func (s *Source) readAndroidFrame(reader *C.AImageReader, pts time.Duration) bool {
	var frame *C.AImage
	deadline := time.NewTimer(500 * time.Millisecond)
	defer deadline.Stop()
	retry := time.NewTicker(2 * time.Millisecond)
	defer retry.Stop()
	for C.AImageReader_acquireNextImage(reader, &frame) != C.AMEDIA_OK {
		select {
		case <-s.quit:
			return false
		case <-deadline.C:
			s.fail(errors.New("video: Android 视频画面读取超时"))
			return false
		case <-retry.C:
		}
	}
	defer C.AImage_delete(frame)
	var crop C.AImageCropRect
	if C.AImage_getCropRect(frame, &crop) != C.AMEDIA_OK || crop.left < 0 || crop.top < 0 || crop.right <= crop.left || crop.bottom <= crop.top {
		s.fail(errors.New("video: Android 视频画面尺寸无效"))
		return false
	}
	width, height := int(crop.right-crop.left), int(crop.bottom-crop.top)
	pixels, err := s.acquire(int64(width) * int64(height) * 4)
	if err != nil {
		s.fail(err)
		return false
	}
	if !C.copy_image(frame, crop, (*C.uint8_t)(unsafe.Pointer(&pixels[0]))) {
		s.Recycle(pixels)
		s.fail(errors.New("video: Android 视频画面格式无效"))
		return false
	}
	select {
	case s.frames <- Frame{PTS: pts, W: width, H: height, Pix: pixels}:
		return true
	case <-s.quit:
		s.Recycle(pixels)
		return false
	}
}
