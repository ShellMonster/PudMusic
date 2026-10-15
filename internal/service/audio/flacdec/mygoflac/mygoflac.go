// Package mygoflac registers the FLAC decoder with MyGo's audio package, so
// audio.OpenStream, audio.DecodeSound and audio.NewDecoder find FLAC the way
// they find WAV, MP3 and Ogg Vorbis.
//
// Import it for its side effect:
//
//	import (
//		"github.com/ShellMonster/Biu_Go/internal/service/audio/flacdec"
//		_ "github.com/ShellMonster/Biu_Go/internal/service/audio/flacdec/mygoflac"
//	)
//
// A bare ".flac" stream is registered on its magic, "fLaC". FLAC inside an
// ISO-BMFF file has no magic at the start of the file -- it is a sequence of
// boxes -- and MyGo's format table can only match one literal string, so the
// MP4 half is not registered here. A player that knows a file is FLAC should
// call flacdec.OpenFile, which sniffs the box type itself; OpenFile accepts
// both containers and is the path this project should use.
package mygoflac

import (
	"io"

	"github.com/ShellMonster/Biu_Go/internal/service/audio/flacdec"

	"github.com/egoist/mygo/audio"
)

func init() {
	audio.RegisterFormat("flac", "fLaC", func(r io.ReadSeeker) (audio.Decoder, error) {
		return flacdec.Open(r)
	})
}
