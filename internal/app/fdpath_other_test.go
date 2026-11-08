//go:build !darwin

package app_test

import "testing"

// fdsOn 靠 macOS 的 F_GETPATH 认出文件；别的平台上用到它的用例本来就跳过。
func fdsOn(t *testing.T, path string) int {
	t.Helper()
	t.Skip("按文件数句柄只在 macOS 上做")
	return 0
}
