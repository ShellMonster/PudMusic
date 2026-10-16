// Package extaf 用 macOS 自带的 AudioToolbox 解码，交给 MyGo 的 audio 包播放。
//
// 它解的是 AudioToolbox 能解的一切压缩格式：AAC / ALAC（.m4a）、MP4 容器里的
// FLAC、AIFF……我们不自己实现任何一个编解码器，只做一件 MyGo 缺的事：把
// ExtAudioFile 交出的交织 float32 PCM 塞进 MyGo 的 Decoder 接口。
//
// 为什么不走 `ExtAudioFileOpenURL`：它只接受**有文件系统支撑**的 `file://`
// URL，而且实测要求文件完整到 **98.3%** 才肯打开（`verify-streaming.md`
// §1.2）。网络流将来要「边下边播」，走的是
// `AudioFileOpenWithCallbacks` + `ExtAudioFileWrapAudioFileID`：
// 字节从哪来完全由我们说了算，AudioToolbox 只管解压。
//
// 这一层刻意不 import MyGo 的 audio 包之外的任何东西，测试时不碰音频设备。
// 子包 `extaf` 之外的接线由 audio 包做（`audio/mp4.go`）。
//
// 平台：只有 macOS 有这一层实现。其它系统上 Open 一律返回 ErrUnsupported。
package extaf

import "errors"

// ErrUnsupported 是「这个平台没有系统解码器」时 Open 返回的错误。
//
// 它和「文件解不了」是两回事：前者是平台没有能力，后者是文件本身不对，
// 两者在界面上要显示不同的提示，所以分开。
var ErrUnsupported = errors.New("extaf: this platform has no system audio decoder")
