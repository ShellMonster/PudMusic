//go:build !darwin && !windows

// 没有系统保护存储的平台（Linux、FreeBSD…）。
//
// # 这里为什么是 ErrUnavailable 而不是「降级成加密文件」
//
// 因为降级方案一旦存在，它就会在某个阶段被悄悄用上：某个 Linux 打包
// 出了个问题 → 有人为了「让它至少能用」加了降级 → 发行版里就有一半用户
// 的账号躺在一个自己加密的文件里，而那把密钥就在同一个目录旁边。
//
// 所以这里**明确地失败**，让「这个平台的登录态存不住」变成一个
// 看得见的问题。发布平台是 macOS + Windows（`SPEC.md` 的既定决定），
// Linux 首发本来就不在这一轮。
//
// 界面上这一态显示成「当前平台没有可用的凭据后端，请重新登录后重试」，
// 而不是静悄悄把登录态丢在内存里——后者更糟：用户登进去了，
// 以为成功了，下次打开发现登出。
package app

import "fmt"

// NewSystemStore 在这些平台上永远返回一个 ErrUnavailable。
func NewSystemStore(string) (Store, error) {
	return nil, fmt.Errorf("%w：这个平台没有系统保护存储", ErrUnavailable)
}
