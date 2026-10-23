// Package mp3 implements an MPEG-1/2/2.5 Layer III (MP3) decoder, in pure
// Go without cgo.
//
// The decoder is a port of minimp3 (CC0-1.0); see PROVENANCE.md.
//
// ⚠️ **上游还有一半编码器，这里没搬**（本项目只解码）。搬进来的部分见
// ../PROVENANCE.md：「哪些文件原样搬了、哪些没搬、为什么」。
package mp3
