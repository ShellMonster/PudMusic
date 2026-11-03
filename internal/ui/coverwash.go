package ui

import "github.com/egoist/mygo/ui"

// coverWash 把封面主色轻轻染进一块底色（播放条、迷你播放器的底）。
//
// 设计稿 docs/design-mockups/playbar.png：整条随当前歌曲的封面色略微偏色，
// 浅色底染 7%、深色底染 10%。**只在设置里「封面取色」开着、且已经取到颜色时染**；
// 否则原样返回，画面和以前一模一样。只改颜色，不动任何布局。
func (s *Shell) coverWash(t *ui.Theme, base ui.Color) ui.Color {
	// ⚠️ **走窄接口，不走 `SettingsSnapshot()`**：这一句一帧被播放条 / 评论区
	// 入口 / 点赞计数 / 迷你播放器调三到五次，整份快照每次是锁 + 几百字节
	// 值拷贝，而这里要的只是「封面取色」那一枚开关（`CoverAccentEnabled`）。
	if !s.app.CoverAccentEnabled() {
		return base
	}
	return washWith(base, t.Accent, s.coverSat(), int(t.Background.R)+int(t.Background.G)+int(t.Background.B) < 3*128)
}

// coverSat 是当前封面取到的饱和度（0 = 没取到）。
func (s *Shell) coverSat() float64 {
	// ⚠️ **走窄接口，不走 `Snapshot()`**：`Snapshot()` 要拷整份 `State`（含
	// 章节切片分配和 BiliURL 拼接），而这里要的只是封面地址那一个串
	// （`App.CoverURL()` 是同一把锁、同一个字段，读到的一样）。
	_, sat := s.tint.sync(s.app.CoverURL(), s.wake.invalidate)
	return sat
}

// washWith 是纯函数那一半：sat 为 0 时原样返回。
func washWith(base, accent ui.Color, sat float64, dark bool) ui.Color {
	if sat <= 0 {
		return base
	}
	k := float32(0.07)
	if dark {
		k = 0.10
	}
	return base.Mix(accent, k)
}
