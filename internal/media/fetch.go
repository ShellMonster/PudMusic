// 取封面：把一个地址变成一段字节。仅此而已，别的什么都不做。

package media

import (
	"fmt"
	"io"
)

// maxArtworkBytes 是单张封面读进来的上限。
//
// 封面是 B 站给的静态图，真图是几十 KB 到几 MB。给个上限不是为了省内存，
// 是为了**不信任长度**：地址可以被换成指向别的东西的那种，读进来的是不是
// 一张图由系统去判断，这一层只负责别把它整个吞进内存。
const maxArtworkBytes = 16 << 20 // 16 MiB

// httpStatusError 是一个不是 200 的响应。
//
// 单独一个类型而不是直接 `fmt.Errorf`：封面取不到**不是**错误状态，只是
// 这一张没有。调用方要能一眼把它和「网络断了」分开——后者值得重试。
type httpStatusError struct {
	Status string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("封面地址回了 %s", e.Status)
}

// readAllLimit 读 body，最多 maxArtworkBytes 字节。
//
// 读满了**不**当错：封面本来就该有上限，超了就是地址后面接的不是我们以为
// 那个东西，那让它去系统的图片解码那里失败（那里报的是「图解不开」，
// 比这里报「图太大了」有用）。
func readAllLimit(body io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(body, maxArtworkBytes))
}
