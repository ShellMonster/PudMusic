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
	{Name: "音频文件", Extensions: []string{"flac", "m4a", "m4s", "aac", "mp3", "wav", "ogg", "aiff", "aif", "aifc"}},
	{Name: "FLAC", Extensions: []string{"flac"}},
	{Name: "AAC / M4A", Extensions: []string{"m4a", "m4s", "aac"}},
	{Name: "MP3", Extensions: []string{"mp3"}},
	{Name: "AIFF", Extensions: []string{"aiff", "aif", "aifc"}},
	{Name: "WAV", Extensions: []string{"wav"}},
	{Name: "OGG", Extensions: []string{"ogg"}},
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

// imageFilters 是选封面时的格式白名单。
//
// ⚠️ **只有 jpeg / png 两项，末位的「所有文件」必须留着**：白名单只是把该
// 看见的摆到前面，不是拦路虎——真有一张 `.webp` 的封面时，拒掉它不如让用户
// 选（选错了上层会说「只支持 jpeg 或 png」，那句话指得准；而不给选，用户
// 只会看到「怎么不能选这张」）。
//
// webp / avif **不在**这一项里：B 站自己的封面全是 `.webp`，所以上游那个
// 收藏夹列表在传 `.avif` 参数（PLAN §0.9 已改成 `.webp`）——但那说的是**读**，
// 上传那边 Biu 只收 jpeg / png（`image-upload/index.tsx:82`）。
var imageFilters = []mygo.FileFilter{
	{Name: "图片文件", Extensions: []string{"jpg", "jpeg", "png"}},
	{Name: "JPEG", Extensions: []string{"jpg", "jpeg"}},
	{Name: "PNG", Extensions: []string{"png"}},
	{Name: "所有文件", Extensions: []string{"*"}},
}

// PickImageFile 弹系统文件框让用户选一张本地的图片当收藏夹封面。
//
// 单选：封面只可能是一张，而多选的文件框会让「选了两张」这种结果只能靠
// 静默丢掉第一张来解释。
//
// ⚠️ 调用约束和 `PickAudioFile` 一样：**必须从非主线程调**。
func PickImageFile(parent *mygo.Window) ([]string, error) {
	return mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:  parent,
		Title:   "选择封面图片",
		Message: "选一张本地的图片，会裁成 16:9",
		Filters: imageFilters,
	})
}

// PickPath 弹系统对话框让用户挑一条路径：dir 为 true 是挑文件夹，否则挑文件。
// 返回空串表示用户取消（不是错误——「用户不想选」是正常结果，不是出错）。
//
// 两行字由调用方给：同一个系统对话框，问的问题不一样——本地音乐那页问的是
// 「挑的音频会出现在哪儿」，设置页的下载目录问的是「下载的东西会存到哪儿」。
// 把它们写死在一个函数里，就有一边在说错的话。
//
// ⚠️ 和 PickAudioFile 一样**必须从非主线程调**：MyGo 的对话框把请求 post 到主线程
// 再等系统回调，主线程自己进去等就是死等，事件循环再也转不起来。
func PickPath(parent *mygo.Window, dir bool, title, message string) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:    parent,
		Title:     title,
		Message:   message,
		Directory: dir,
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// PickDirectory 弹系统对话框让用户选一个文件夹，返回选中的路径。用户取消时
// 返回空串（不是错误——「用户不想选」是正常结果，不是出错）。
//
// 用于本地音乐页的「添加目录」。parent 非空时对话框挂在那个窗口下面。
func PickDirectory(parent *mygo.Window) (string, error) {
	return PickPath(parent, true,
		"选择音乐文件夹", "选一个文件夹，它的**第一层**里的音频文件会出现在本地音乐里")
}

// jsonFilters 是配置文件对话框的白名单。
//
// 摆「所有文件」是为了不拦路：有人把 Biu 导出的那份改名成了 `biu.json`，或者
// 从别的机器上拷来的文件名完全没有规律。白名单在这里只是让「配置 JSON」排在
// 最前面，不是「别的文件不让选」。
var jsonFilters = []mygo.FileFilter{
	{Name: "配置文件", Extensions: []string{"json"}},
	{Name: "所有文件", Extensions: []string{"*"}},
}

// PickConfigFile 弹系统对话框让用户挑一份配置文件，返回路径。用户取消时返回
// 空串（不是错误）。
//
// ⚠️ 和 PickPath 一样**必须从非主线程调**。
func PickConfigFile(parent *mygo.Window) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:  parent,
		Title:   "选择配置文件",
		Message: "选一份本应用或别的机器导出的配置 JSON",
		Filters: jsonFilters,
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// SaveConfigAs 弹系统对话框让用户选导出到哪，返回路径。用户取消时返回空串。
//
// ⚠️ 和 PickPath 一样**必须从非主线程调**。
//
// 用保存对话框而不是「挑个文件夹我们自己起名」：导出文件名里带时间
// （`app.ExportFileName`），让用户能改这个文件名才是他期待的——同名覆盖时系统
// 会自己问一句，而我们自己往一个目录里写固定名字的话，那次覆盖是静悄悄的。
func SaveConfigAs(parent *mygo.Window, defaultName string) (string, error) {
	return mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:            parent,
		Title:             "导出配置",
		Message:           "把设置存成一份 JSON，可以在别的机器上导入",
		DefaultPath:       defaultName,
		Filters:           jsonFilters,
		CreateDirectories: true,
	})
}
