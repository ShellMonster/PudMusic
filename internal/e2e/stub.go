//go:build !e2e

package e2e

// ⚠️ **这一份只是为了让 `go list ./...` 在没带 `-tags e2e` 时也能看到本包**。
//
// `internal/e2e` 下所有真文件都标了 `//go:build e2e`——不带那个标签时整包
// 「build constraints exclude all Go files」，而 `go list ./...`（默认验收
// 命令、`go test $(go list ./...)`）会因为**包本身列不进去**而静默跳过它；
// 可一旦有人在别处 `go test ./internal/e2e/`（忘了带标签），报的就是一句
// 莫名其妙的「build constraints exclude all Go files」。
//
// 留一个永远编进来的空文件：包永远「存在」，没标签时是**零条用例**的
// 空包（`ok … [no tests to run]`），带 `-tags e2e` 时真文件照常进来。
