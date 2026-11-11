package ui

// scrollbarWidth 是滚动条的宽，照上游 `--os-size: 10px`。
const scrollbarWidth = 10

// scrollbarGutter 是滚动条轨道盖住的那条竖带。v0.9.0 的
// `ScrollbarStyle.Resolve` 把轨道宽定成 thumb + 6；内容要让开它，否则悬停时被盖住。
const scrollbarGutter = scrollbarWidth + 6
