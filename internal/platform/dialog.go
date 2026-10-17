// Package platform 把同一个操作在首发平台（macOS / Windows）上的差异翻过来。
//
// 它是四层结构里最下面的一层：可以做平台特有的事，但不认识业务，也不认识界面。
package platform

import "github.com/egoist/mygo"

// 打开音频文件时给用户的格式白名单。顺序就是对话框里的分组顺序。
//
// 白名单是解码器注册表（internal/service/audio 里那些 RegisterFormat 调用）在界面
// 上的投影：注册表按**文件头**分派、没有「扩展名」这个概念，所以这里得自己记一遍
// 扩展名，记漏一个的症状就是「能播却选不了」——阶段 1 把 AAC 解码器做完了却没开
// 入口，就是这么来的。dialog_test.go 拿活的注册表核这份清单。
//
// 第一项「音频文件」是后面各项的并集，加新格式时别忘了同时加进这一项。
//
// ⚠️ 末位的「所有文件」必须留着：白名单只是把该看见的摆到前面，不是拦路虎。
var audioFilters = []mygo.FileFilter{
	{Name: "音频文件", Extensions: []string{"flac", "m4a", "m4s", "aac", "mp3", "wav", "aiff", "aif", "aifc"}},
	{Name: "FLAC", Extensions: []string{"flac"}},
	{Name: "AAC / M4A", Extensions: []string{"m4a", "m4s", "aac"}},
	{Name: "MP3", Extensions: []string{"mp3"}},
	{Name: "AIFF", Extensions: []string{"aiff", "aif", "aifc"}},
	{Name: "WAV", Extensions: []string{"wav"}},
	{Name: "所有文件", Extensions: []string{"*"}},
}

// audioOpenOptions 组出打开音频文件时的对话框配置。
//
// 单独拆出来是因为 PickAudioFile 会真的弹窗，测试里调不了；配置本身是纯数据，
// 拆开就能在测试里核。
func audioOpenOptions(parent *mygo.Window) mygo.OpenDialogOptions {
	return mygo.OpenDialogOptions{
		Parent:  parent,
		Title:   "打开音频文件",
		Message: "选一个或多个本地的音频文件",
		Filters: audioFilters,
		// 一次能选多首——本地音乐本来就是按文件夹成批加进队列的，单选逼着用户
		// 重复十几次弹框。
		Multiple: true,
	}
}

// PickAudioFile 弹系统文件框让用户选本地的音频文件，返回选中的路径（可能多首）。
// 用户取消时返回空切片。parent 非空时对话框挂在那个窗口下面（macOS 上是
// 一张纸片）。
//
// ⚠️ 必须从非主线程调用：MyGo 的对话框把请求 post 到主线程再等系统回调，
// 主线程自己进去等就是死等，事件循环再也转不起来。
func PickAudioFile(parent *mygo.Window) ([]string, error) {
	return mygo.Dialog.Open(audioOpenOptions(parent))
}

// PickDirectory 弹系统对话框让用户选一个文件夹，返回选中的路径。用户取消时
// 返回空串（不是错误——「用户不想选」是正常结果，不是出错）。
//
// 用于本地音乐页的「添加目录」。parent 非空时对话框挂在那个窗口下面。
//
// ⚠️ 和 PickAudioFile 一样**必须从非主线程调**：MyGo 的对话框把请求 post 到
// 主线程再等系统回调，主线程自己进去等就是死等。
func PickDirectory(parent *mygo.Window) (string, error) {
	dirs, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:    parent,
		Title:     "选择音乐文件夹",
		Message:   "选一个文件夹，它的**第一层**里的音频文件会出现在本地音乐里",
		Directory: true,
	})
	if err != nil {
		return "", err
	}
	if len(dirs) == 0 {
		return "", nil
	}
	return dirs[0], nil
}
