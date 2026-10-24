// 「B 站同一个字段可能给两种 JSON 类型」的那几个字段住的地方。
//
// # 为什么需要它
//
// ⚠️ **`encoding/json` 遇到「类型对不上」会让整个反序列化失败**，而失败的那
// 一条会被当成「读不懂」丢掉——于是**一个字段改了类型，一整页动态一条都不
// 剩**，而界面上只是一个空列表（没有报错、没有提示、什么都没有）。
//
// 实测过（2026-10-24，票 #300）：B 站的 `module_author.pub_ts` 从 Unix 秒
// （数字）改成了**字符串**（`"1792720812"`），于是 UP 空间的「动态」那一格
// 每一页 12 条**全部**被丢掉，页面永远转圈、永远拿不到内容。
//
// 所以凡是「B 站有可能写成字符串的数字」都用 `flexInt`，反过来也一样。
//
// # 这三个类型**绝不返回错误**
//
// 返回错误就等于让 `encoding/json` 失败，那正是这一层要消灭的事。所以：
// 读不出来就当零值，**绝不**把一条动态带走。
//
// ⚠️ **「读不出来」和「没有」不是一回事，但这一层宁可要后者**：一个时间戳
// 读不出来时 `pub_time`（"3 分钟前"）还在，一个计数读不出来时画成 0 无害，
// 而一条动态被带走是**整页**的事。
package bili

import (
	"encoding/json"
	"strconv"
	"strings"
)

// flexString 是「B 站可能给字符串、也可能给数字」的那些字段。
//
// 数字**原样**当字符串留着（不转成 float）：动态的 id 是 19 位的大整数，
// 走一遍 float 就已经掉精度了。
type flexString string

// UnmarshalJSON 读一个字符串、数字或布尔。对象和数组读成空串。
func (f *flexString) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	switch {
	case s == "" || s == "null":
		*f = ""
	case s[0] == '"':
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			*f = ""
			return nil
		}
		*f = flexString(v)
	case s[0] == '{' || s[0] == '[':
		// 对象和数组不是「一句话」，当空串（那一格于是画不出来，而动态本身
		// 还在——比整条被带走好）。
		*f = ""
	default:
		*f = flexString(s)
	}
	return nil
}

// String 是它本来的样子。
func (f flexString) String() string { return string(f) }

// flexInt 是「B 站可能给数字、也可能给数字字符串」的那些字段。
type flexInt int64

// UnmarshalJSON 读一个数字、数字字符串或空值。读不出来就当 0。
func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if len(s) >= 2 && s[0] == '"' {
		s = s[1 : len(s)-1]
	}
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*f = flexInt(n)
		return nil
	}
	if fl, err := strconv.ParseFloat(s, 64); err == nil {
		*f = flexInt(int64(fl))
		return nil
	}
	// 读不出来（日期串之类）：当 0，**别把整条动态带走**。
	*f = 0
	return nil
}

// Int64 是它本来的样子。
func (f flexInt) Int64() int64 { return int64(f) }

// flexBool 是「B 站可能给布尔、也可能给 0/1」的那些字段。
type flexBool bool

// UnmarshalJSON 读一个布尔、0/1 或 "true"/"false"。
func (f *flexBool) UnmarshalJSON(b []byte) error {
	switch strings.Trim(strings.TrimSpace(string(b)), `"`) {
	case "true", "1":
		*f = true
	default:
		*f = false
	}
	return nil
}

// Bool 是它本来的样子。
func (f flexBool) Bool() bool { return bool(f) }
