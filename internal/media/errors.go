package media

// 平台那一半「没做」的时候报的那一类错误。
//
// **单独一个文件、不带平台标签**，是因为现在有三处要用它：darwin 那边
// （mp_darwin.go）、Windows 那边（smtc_windows.go 的 bindWindow），
// 和既没有 SMTC 也没有 MPRIS 的那些平台（platform_other.go）。放在其中
// 任意一个文件里，另外两个平台就编不过——而「把这个文件挪到公共目录」是
// 这种共用的小类型该有的归宿。
//
// 它是一个**具名的字符串类型**而不是 `errors.New` 的结果：为的是 `==` 就能
// 比（有人在测里要断「就是这一条」），而 `Error()` 里那个 `media:` 前缀
// 让它在日志里一眼认得出是这一层而不是别处的。
type errPlatform string

func (e errPlatform) Error() string { return "media: " + string(e) }
