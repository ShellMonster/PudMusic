//go:build !darwin

package video

// Open 在没有解码器的平台上恒失败。
func Open(path string, pool chan []byte) (*Source, error) {
	return nil, ErrUnsupported
}
