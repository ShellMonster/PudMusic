package imgcache

import (
	"log"
	"os"
	"strconv"
	"strings"
)

// Resolve defaults when constructing or explicitly resetting a cache. Both
// windows share one cache; normal operation never reads the environment per frame.
func configuredMaxBytes() int64 {
	value := strings.TrimSpace(os.Getenv("BIU_IMAGE_CACHE_MB"))
	if value == "" {
		return DefaultMaxBytes
	}
	mb, err := strconv.ParseInt(value, 10, 64)
	if err != nil || mb < 1 || mb > 4096 {
		log.Printf("biu: BIU_IMAGE_CACHE_MB 应为 1..4096 的整数（MiB），使用默认值 %d", DefaultMaxBytes>>20)
		return DefaultMaxBytes
	}
	return mb << 20
}
