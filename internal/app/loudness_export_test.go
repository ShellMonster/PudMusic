package app

// LoudnessCachedForTest 说 path 这首本地歌的实时测量结果是否已经进了响度缓存
// （给外部测试包用）。测量函数返回到结果入表之间有一段空隙，测试只能等这一步，
// 不能等「测量开始了」。
func LoudnessCachedForTest(a *App, path string) bool {
	_, ok := a.loudID.get(playbackIDLocal + path)
	return ok
}
