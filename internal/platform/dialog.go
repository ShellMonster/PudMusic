// Package platform 把同一个操作在首发平台（macOS / Windows）上的差异翻过来。
//
// 它是四层结构里最下面的一层：可以做平台特有的事，但不认识业务，也不认识界面。
package platform

import "github.com/egoist/mygo"

// 打开音频文件时给用户的格式白名单。顺序就是对话框里的分组顺序。
var audioFilters = []mygo.FileFilter{
	{Name: "音频文件", Extensions: []string{"flac", "aiff", "aif", "aifc", "mp3", "wav"}},
	{Name: "FLAC", Extensions: []string{"flac"}},
	{Name: "MP3", Extensions: []string{"mp3"}},
	{Name: "AIFF", Extensions: []string{"aiff", "aif", "aifc"}},
	{Name: "WAV", Extensions: []string{"wav"}},
	{Name: "所有文件", Extensions: []string{"*"}},
}

// PickAudioFile 弹系统文件框让用户选一个本地音频文件，返回选中的路径。
// 用户取消时返回空切片。parent 非空时对话框挂在那个窗口下面（macOS 上是
// 一张纸片）。
//
// ⚠️ 必须从非主线程调用：MyGo 的对话框把请求 post 到主线程再等系统回调，
// 主线程自己进去等就是死等，事件循环再也转不起来。
func PickAudioFile(parent *mygo.Window) ([]string, error) {
	return mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:  parent,
		Title:   "打开音频文件",
		Message: "选一个本地的音频文件",
		Filters: audioFilters,
	})
}
