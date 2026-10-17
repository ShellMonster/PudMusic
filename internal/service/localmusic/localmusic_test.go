package localmusic

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 这一组测试盯的是扫描的三件事：**只扫一层**、**元信息真的从文件里读出来**、
// **路径冲突和符号链接按说好的规矩处理**。
//
// 断言一律落在**结果本身**（哪些路径、哪些标题、几个身份）上，不落在
// 「有没有报错」上——那是最容易恒为真的一种断言。

// TestScanOnlyReadsOneLevel 钉住「只扫一层」。
//
// 这是任务里点名的一条，也是 Biu 上游的行为（`local-music.ts:20` 的
// `if (entry.isDirectory()) continue`）。少这一条的症状很隐蔽：音乐目录里
// 有个 `专辑/` 子目录（很多��的收藏都是这么放的），然后扫几千个文件扫半天，
// 界面上还多出几百行用户没说要的。
func TestScanOnlyReadsOneLevel(t *testing.T) {
	root := t.TempDir()
	songPath(t, root, "上面这层.mp3")
	songPath(t, filepath.Join(root, "专辑"), "下面那层.mp3")

	res := Scan([]string{root}, nil)

	if got := pathsOf(res.Tracks); len(got) != 1 || !strings.HasSuffix(got[0], "上面这层.mp3") {
		t.Fatalf("只该扫到上面那一层，实际是 %v", got)
	}
}

// TestScanSkipsWhatIsNotMusic 钉住白名单。
//
// 不是「扩展名对得上就是音乐」：白名单之外的一律不进。
func TestScanSkipsWhatIsNotMusic(t *testing.T) {
	root := t.TempDir()
	songPath(t, root, "歌.mp3")
	songPath(t, root, "大写.MP3") // 用户从 Windows 拷过来的歌有一半是大写的
	put(t, filepath.Join(root, "封面.jpg"), []byte("x"))
	put(t, filepath.Join(root, "歌词.txt"), []byte("x"))
	put(t, filepath.Join(root, "歌.MP3.bak"), []byte("x"))

	res := Scan([]string{root}, nil)

	got := titlesOf(res.Tracks)
	slices.Sort(got)
	// 标题是「文件名去掉扩展名」，所以大写的 .MP3 也得去掉。
	want := []string{"大写", "歌"}
	if !slices.Equal(got, want) {
		t.Errorf("白名单该是 %v，实际 %v", want, got)
	}
}

// TestTitleComesFromTheFileNotTheFileName 是这一票最要紧的那条断言：
// **标题是从标签里读出来的，不是从文件名猜的**。
//
// ⚠️ 「解码音频不等于读标签」那句话在这里落地：文件名去掉扩展名当标题是
// 编出来的，而这个文件的名字和它里面的标签**故意不一样**——只有真的读了
// 文件才答得出正确答案。
//
// 拿掉修复的方式：让 Title 直接 `return titleFromName(...)`，这一条立刻红。
func TestTitleComesFromTheFileNotTheFileName(t *testing.T) {
	root := t.TempDir()
	mp3With(t, filepath.Join(root, "文件名不是标题.mp3"), "标签里的标题")
	m4aWith(t, filepath.Join(root, "另一个也不是.m4a"), "M4A 的标题", 1024)
	flacWith(t, filepath.Join(root, "第三个也不是.flac"), "FLAC 的标题")
	wavWith(t, filepath.Join(root, "第四个也不是.wav"), "WAV 的标题")
	aiffWith(t, filepath.Join(root, "第五个也不是.aiff"), "AIFF 的标题")

	res := Scan([]string{root}, nil)

	want := map[string]bool{
		"标签里的标题":   false,
		"M4A 的标题":  false,
		"FLAC 的标题": false,
		"WAV 的标题":  false,
		"AIFF 的标题": false,
	}
	for _, tr := range res.Tracks {
		if _, ok := want[tr.Title]; !ok {
			t.Errorf("标题 %q 不在预期的五个里（%v）——多半是从文件名猜的", tr.Title, pathsOf(res.Tracks))
			continue
		}
		want[tr.Title] = true
	}
	for title, seen := range want {
		if !seen {
			t.Errorf("没读到标签里的标题 %q；扫到的标题有 %v", title, titlesOf(res.Tracks))
		}
	}
}

// TestTitleFallsBackToTheFileName 钉住优先级的另一半：标签读不出来就用文件名，
// **不给一个编出来的标题**。Ogg 和 WMA 就是这一档（见 tags.go 的表）。
func TestTitleFallsBackToTheFileName(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "没有标签的歌.wma"), []byte("ASF fake"))
	put(t, filepath.Join(root, "也没有标签.ogg"), []byte("OggS fake"))

	res := Scan([]string{root}, nil)

	got := titlesOf(res.Tracks)
	slices.Sort(got)
	want := []string{"也没有标签", "没有标签的歌"}
	if !slices.Equal(got, want) {
		t.Errorf("退回文件名该是 %v，实际 %v", want, got)
	}
}

// TestID3v3FramesUseAPlainSize 钉住 ID3 版本的那条差别。
//
// v4 的帧长是同步安全整数、v3 是普通大端，v2.2 的帧号只有 3 个字母。
// 全按 v4 解的话，v3 的标签会被解成乱码——而 v3 是当年最常见的一版。
func TestID3v3FramesUseAPlainSize(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "v3.mp3")
	// 标题 200 字节：同步安全解法会把 200 读成 200，但解一个更大的就露馅了。
	title := strings.Repeat("长", 200)
	put(t, path, append(id3v2Tag(3, title, 3, false), 0xff, 0xfb))

	if got := Title(path); got != title {
		t.Errorf("v3 的标签解成了 %q（%d 字节），want %q", got, len(got), title)
	}
}

// TestTitleHandlesUTF16AndLatin1 钉住文本编码那一层。
//
// 标签里的歌名十有八九是 UTF-16（写标签的库默认行为），而 latin1 那条路
// 不做转换的话画出来是乱码。
func TestTitleHandlesUTF16AndLatin1(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "u16.mp3"),
		append(id3v2Body(4, utf16LE("UTF-16 的歌名"), 1, true), 0xff, 0xfb))
	// ⚠️ 必须写 0xE9 而不是 "é"：编码 0 说的是 ISO-8859-1，那一个字节才是 é。
	// 直接写 "é" 是 UTF-8 的两个字节（0xC3 0xA9），那是另一回事。
	put(t, filepath.Join(root, "latin.mp3"),
		append(id3v2Body(4, []byte{'C', 'a', 'f', 0xe9}, 0, true), 0xff, 0xfb))

	if got := Title(filepath.Join(root, "u16.mp3")); got != "UTF-16 的歌名" {
		t.Errorf("UTF-16 标签解成了 %q", got)
	}
	// latin1 必须真的转成 UTF-8：直接 string(bytes) 出来的是非法字节，界面上是乱码。
	if got := Title(filepath.Join(root, "latin.mp3")); got != "Café" {
		t.Errorf("latin1 标签解成了 %q（% x），want Café", got, got)
	}
}

// TestVorbisCommentKeepsTheValueCase 钉住「只折键、不折值」。
//
// FLAC 的注释是 `TITLE=值` 这种一行一条。比较键的时候要大小写不敏感
// （现实里有小写的 `title=`），但**值必须原样留着**——
// `TITLE=Hello World` 折成 `hello world` 是在篡改用户文件里的字。
//
// 拿掉修复的方式：把 `strings.EqualFold(key, "TITLE")` 换成
// `strings.HasPrefix(strings.ToLower(field), "title=")`（值跟着折了）。
func TestVorbisCommentKeepsTheValueCase(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "大小写.flac")
	flacWith(t, path, "Hello World 混着中文")

	if got := Title(path); got != "Hello World 混着中文" {
		t.Errorf("值被改了大小写：%q", got)
	}
}

// TestMP4TitleFindsMoovAtTheEnd 是真实 mp4 最常见的形状：
// `moov` 在文件末尾（边下边播的文件都是这样）。
//
// 拿掉修复的方式：只读文件开头固定几 KB 就返回。这条立刻红。
func TestMP4TitleFindsMoovAtTheEnd(t *testing.T) {
	root := t.TempDir()
	// ftyp 后面塞 1MB 的别的原子，moov 落在远远超出任何「读个头」的范围内。
	path := filepath.Join(root, "尾巴上有 moov.m4a")
	m4aWith(t, path, "在很远的地方", 1<<20)

	if got := Title(path); got != "在很远的地方" {
		t.Errorf("moov 在文件末尾时读出了 %q", got)
	}
}

// TestTitleSurvivesTruncatedFiles 钉住「读不出来就当没有」。
//
// 音乐目录里半删的文件、被中断的下载留下的 .part 改名来的文件都很常见。
// 解析器必须**返回空串**而不是 panic——它在后台扫描里跑，崩了就是进程没了。
func TestTitleSurvivesTruncatedFiles(t *testing.T) {
	root := t.TempDir()
	cases := map[string][]byte{
		"断在 ID3 头里.mp3": {'I', 'D', '3', 4, 0, 0, 0x7f, 0x7f, 0x7f, 0x7f},
		// 帧头说「后面有 400 字节」，但文件到这儿就没了。
		"断在帧头里.mp3": append(append([]byte("ID3"), 4, 0, 0, 0, 0, 0x02),
			append(append([]byte("TIT2"), syncsafeBytes(400)...), 0, 0)...),
		"断在 FLAC 块里.flac": append([]byte("fLaC"), 0x84, 0x7f, 0xff, 0xff),
		"断在 RIFF 里.wav":   append(append([]byte("RIFF"), 0xff, 0xff, 0xff, 0xff), 'W', 'A', 'V', 'E', 'L', 'I', 'S', 'T'),
		"断在 FORM 里.aiff":  append(append([]byte("FORM"), 0xff, 0xff, 0xff, 0xff), 'A', 'I', 'F', 'F', 'N', 'A', 'M', 'E'),
		"断在原子里.m4a":       {'\x00', '\x00', '\x00', 0x40, 'm', 'o', 'o', 'v', 'x'},
		"完全是垃圾.mp3":       []byte("这不是音频这是一个文本文件"),
	}
	for name, data := range cases {
		path := put(t, filepath.Join(root, name), data)
		if got := Title(path); got != "" {
			t.Errorf("%s 解出了标题 %q，want 空串", name, got)
		}
	}
}

// ── 元信息里的另外三项 ───────────────────────────────────────────────────────

// TestScanReadsSizeAndFormatFromDisk 钉住大小和格式这两栏。
//
// 它们不花钱，所以每次重扫都重新取——这条钉住「真的取了」而不是「填了 0」。
func TestScanReadsSizeAndFormatFromDisk(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "歌.FLAC")
	put(t, path, []byte("0123456789"))

	res := Scan([]string{root}, nil)
	if len(res.Tracks) != 1 {
		t.Fatalf("扫到 %d 首", len(res.Tracks))
	}
	tr := res.Tracks[0]
	if tr.Size != 10 {
		t.Errorf("大小是 %d，want 10", tr.Size)
	}
	if tr.Format != ".flac" {
		t.Errorf("格式是 %q，want .flac（小写、带点）", tr.Format)
	}
	if tr.Created.IsZero() {
		t.Error("创建时间是零值")
	}
	if !tr.Playable() {
		t.Error("FLAC 是放得出的，Playable 却是 false")
	}
}

// TestWMAIsListedButNotPlayable 钉住 PLAN §5.4 点名的那个缺口。
//
// 白名单里有 `.wma`（照抄 Biu），但我们**播不了**。两件事同时成立，
// 界面上才有一句能说出口的「这个格式我们不支持」——而不是让用户以为文件坏了。
func TestWMAIsListedButNotPlayable(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "老歌.wma"), []byte("ASF fake"))

	res := Scan([]string{root}, nil)
	if len(res.Tracks) != 1 {
		t.Fatalf("WMA 应该进列表（白名单里有它），实际 %d 首", len(res.Tracks))
	}
	if res.Tracks[0].Playable() {
		t.Error("WMA 被说成能播——我们没有 ASF 解码器")
	}
}

// TestDurationIsReadFromTheFile 钉住「时长从文件里读出来」。
//
// 这一条不能靠造出来的假文件验：时长在容器头里（moov/mvhd、FLAC 的
// STREAMINFO），假文件里没有。所以这里用 audio 包里现成的真 testdata。
//
// 断言的是**不是零**加上一个宽松的范围——不同解码器对同一个文件的算法
// 可能差一点点，把精确值钉死只会让这条测试变成一件易碎的东西。
func TestDurationIsReadFromTheFile(t *testing.T) {
	// flacdec/testdata/stereo16.flac 是真 FLAC（见 audio 包的测试）。
	const flac = "../audio/flacdec/testdata/stereo16.flac"
	if _, err := os.Stat(flac); err != nil {
		t.Skipf("拿不到 %s：%v", flac, err)
	}

	got := ProbeDuration(flac)
	if got <= 0 {
		t.Fatalf("真 FLAC 的时长读成了 %v，want 一个正数", got)
	}
	if got < time.Second || got > time.Hour {
		t.Errorf("时长 %v 不在合理范围里", got)
	}
}

// TestDurationIsZeroWhenItCannotBeRead 钉住读不出来就是 0。
//
// 界面拿 0 画「-」。反过来（拿 0 当「0 秒」）是在骗用户。
func TestDurationIsZeroWhenItCannotBeRead(t *testing.T) {
	root := t.TempDir()
	path := put(t, filepath.Join(root, "不是音频.mp3"), []byte("这是一个文本文件"))

	if got := ProbeDuration(path); got != 0 {
		t.Errorf("读不出时长的文件给了 %v，want 0", got)
	}
}

// TestRescanReusesMetadataForUnchangedFiles 钉住元信息缓存。
//
// 这是「几千个文件也能重扫」的关键：文件没变就不该再开一次容器、读一次标签。
// 断言的是**缓存里那一项被留下来了**（而且没被清掉）。
func TestRescanReusesMetadataForUnchangedFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "歌.mp3")
	mp3With(t, path, "原来的标题")

	first := Scan([]string{root}, nil)
	if len(first.Cache) != 1 {
		t.Fatalf("第一遍缓存里 %d 项", len(first.Cache))
	}
	// 假装第二遍的扫描已经读到了（Scan 自己会往里写）。
	second := Scan([]string{root}, first.Cache)
	if len(second.Cache) != 1 {
		t.Fatalf("第二遍之后缓存里 %d 项，want 1（没变的文件该被留下）", len(second.Cache))
	}
	for id, meta := range second.Cache {
		if meta.Title != "原来的标题" {
			t.Errorf("缓存里的标题变成了 %q", meta.Title)
		}
		_ = id
	}

	// 改了内容 → 大小变了 → 必须重读。
	mp3With(t, path, "换过的标题")
	third := Scan([]string{root}, second.Cache)
	if got := titlesOf(third.Tracks); len(got) != 1 || got[0] != "换过的标题" {
		t.Errorf("改过之后标题是 %v，want [换过的标题]", got)
	}
}

// ── 路径冲突与符号链接 ───────────────────────────────────────────────────────

// TestTheSameFileReachedTwoWaysIsListedOnce 钉住规矩 2。
//
// 两种真实的撞法都试：**同一个根加了两次**（`/music` 和 `/music/` 或者
// 同一个文件夹的一个符号链接），和**一个根里同时有文件和指向它的链接**。
//
// 不去重的后果不是「多一行」：两行共用一个身份，而框架拿身份认行的状态
// ——选中一行两行都亮（widgets/list 的铁律四）。
func TestTheSameFileReachedTwoWaysIsListedOnce(t *testing.T) {
	root := t.TempDir()
	songPath(t, root, "歌.mp3")
	link := filepath.Join(root, "链接.mp3")
	if err := os.Symlink(filepath.Join(root, "歌.mp3"), link); err != nil {
		t.Skipf("这台机器建不了符号链接：%v", err)
	}

	// 同一个根用两种写法各加一次。
	res := Scan([]string{root, root + string(filepath.Separator)}, nil)

	if len(res.Tracks) != 1 {
		t.Fatalf("同一首歌列了 %d 行：%v", len(res.Tracks), pathsOf(res.Tracks))
	}
}

// TestTwoRootsReachingTheSameFileIsListedOnce 是规矩 2 的另一半。
func TestTwoRootsReachingTheSameFileIsListedOnce(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "真目录")
	songPath(t, real, "歌.mp3")
	alias := filepath.Join(base, "别名目录")
	if err := os.Symlink(real, alias); err != nil {
		t.Skipf("这台机器建不了符号链接：%v", err)
	}

	res := Scan([]string{real, alias}, nil)

	if len(res.Tracks) != 1 {
		t.Fatalf("同一首歌经两条根列了 %d 行：%v", len(res.Tracks), pathsOf(res.Tracks))
	}
}

// TestSymlinkToAFileIsListedButItsOwnPathIsKept 钉住规矩 1 和 3 的配合。
//
// 链接要进列表（用户的音乐目录里放链接是常事），但**那一行记的必须是链接
// 自己的路径**——因为将来删的是它，而删链接不会碰到它指向的文件。
//
// 这条断言的是两件事：链接在列表里，以及 `Track.Path` 不是目标路径。
// 拿掉修复的方式（`Track.Path = canonical`）第二条立刻红，而那正是「删到
// 别处去」的那一步。
func TestSymlinkToAFileIsListedButItsOwnPathIsKept(t *testing.T) {
	base := t.TempDir()
	library := filepath.Join(base, "音乐")
	outside := filepath.Join(base, "别处")
	songPath(t, library, "本地的.mp3")
	target := songPath(t, outside, "真正的文件.mp3")
	link := filepath.Join(library, "指向别处.mp3")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("这台机器建不了符号链接：%v", err)
	}

	res := Scan([]string{library}, nil)

	if len(res.Tracks) != 2 {
		t.Fatalf("扫到 %d 首，want 2（本地那个 + 链接那个）：%v", len(res.Tracks), pathsOf(res.Tracks))
	}
	var linked Track
	for _, tr := range res.Tracks {
		if tr.Path == link {
			linked = tr
		}
	}
	if linked.Path == "" {
		t.Fatalf("链接那一行不在列表里：%v", pathsOf(res.Tracks))
	}
	if linked.Path == target {
		t.Fatalf("链接那一行记的是**目标**的路径 %q——删它就会删到音乐库外面去", target)
	}
	if linked.ID == "" {
		t.Error("链接那一行没有身份")
	}
}

// TestBrokenSymlinkIsNotListed 钉住断掉的链接不进列表。
//
// 它指着一首已经不存在的歌，列出来只会让人点一个播不了的东西。
func TestBrokenSymlinkIsNotListed(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "断掉的.mp3")
	if err := os.Symlink(filepath.Join(root, "从来没存在过.mp3"), link); err != nil {
		t.Skipf("这台机器建不了符号链接：%v", err)
	}

	res := Scan([]string{root}, nil)

	if len(res.Tracks) != 0 {
		t.Fatalf("断掉的链接被列出来了：%v", pathsOf(res.Tracks))
	}
}

// TestSymlinkToADirectoryIsNotWalkedInto 钉住「不递归」在符号链接上也成立。
//
// 规矩是「只扫一层」，而链接指向的目录是另一层。走进去就等于我们替用户
// 指了一个他没指的地方。
func TestSymlinkToADirectoryIsNotWalkedInto(t *testing.T) {
	base := t.TempDir()
	library := filepath.Join(base, "音乐")
	deeper := filepath.Join(base, "另一个地方")
	songPath(t, deeper, "深处的歌.mp3")
	link := filepath.Join(library, "一个目录链接")
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(deeper, link); err != nil {
		t.Skipf("这台机器建不了符号链接：%v", err)
	}

	res := Scan([]string{library}, nil)

	if len(res.Tracks) != 0 {
		t.Fatalf("走进了链接指向的目录：%v", pathsOf(res.Tracks))
	}
}

// TestMissingRootIsReportedButTheOthersStillScan 钉住一个坏目录不拖垮整次扫描。
//
// 外接盘没插、某个目录被删了、权限不对——这些都只该让**那一个**目录不出现。
func TestMissingRootIsReportedButTheOthersStillScan(t *testing.T) {
	good := t.TempDir()
	songPath(t, good, "歌.mp3")

	res := Scan([]string{filepath.Join(good, "根本没这个"), good}, nil)

	if res.Err == nil {
		t.Error("读不到的目录没有报错")
	}
	if len(res.Tracks) != 1 {
		t.Fatalf("好目录里的歌被一起丢掉了：%v", pathsOf(res.Tracks))
	}
}

// TestIdentityIsStableAcrossRescans 钉住身份那条真正要的东西。
//
// 文件监听每有一个变动就重扫一遍（见 app 层的 LocalMusic），所以身份必须
// **在重扫之间不变**。变了的话用户选中��那一行每次刷新都跳走——而症状是
// 「有时候点一下会选到别的歌」，极难查。
//
// 拿掉修复的方式：身份里掺一个每次都不同的东西（扫描序号、时间戳）。
func TestIdentityIsStableAcrossRescans(t *testing.T) {
	root := t.TempDir()
	songPath(t, root, "歌.mp3")
	songPath(t, root, "另一首.mp3")

	first := Scan([]string{root}, nil)
	second := Scan([]string{root}, first.Cache)
	if len(first.Tracks) != 2 || len(second.Tracks) != 2 {
		t.Fatalf("扫到的首数 %d / %d", len(first.Tracks), len(second.Tracks))
	}
	for i := range first.Tracks {
		if first.Tracks[i].ID != second.Tracks[i].ID {
			t.Errorf("第 %d 首重扫之后身份变了：%s -> %s",
				i, first.Tracks[i].ID, second.Tracks[i].ID)
		}
	}
	// 两首不同的歌必须是两个身份：撞了的话界面上选中一首两首都亮。
	if first.Tracks[0].ID == first.Tracks[1].ID {
		t.Error("两首不同的歌共用了一个身份")
	}
}

// TestScanIsSortedByRootThenPath 钉住顺序。
//
// 文件系统返回的目录顺序在不同机器上不一样，而界面上的行号、测试里的
// 「第几首」都依赖它。
func TestScanIsSortedByRootThenPath(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	songPath(t, a, "b.mp3")
	songPath(t, a, "a.mp3")
	songPath(t, b, "c.mp3")

	res := Scan([]string{a, b}, nil)

	got := pathsOf(res.Tracks)
	if len(got) != 3 {
		t.Fatalf("扫到 %d 首：%v", len(got), got)
	}
	if !strings.HasSuffix(got[0], "a.mp3") || !strings.HasSuffix(got[1], "b.mp3") {
		t.Errorf("同一个根里没按路径排：%v", got)
	}
	if !strings.HasPrefix(got[2], b) {
		t.Errorf("根之间不是按加进去的顺序排：%v", got)
	}
}

// utf16LE 把 s 编码成带 BOM 的 UTF-16LE，用在 ID3 的编码 1 上。
func utf16LE(s string) []byte {
	units := make([]byte, 0, 2*len(s)+2)
	units = append(units, 0xFF, 0xFE) // BOM：小端
	for _, r := range s {
		if r < 0x10000 {
			units = append(units, byte(r), byte(r>>8))
			continue
		}
		r -= 0x10000
		hi := 0xD800 + (r >> 10)
		lo := 0xDC00 + (r & 0x3FF)
		units = append(units, byte(hi), byte(hi>>8), byte(lo), byte(lo>>8))
	}
	return units
}

// TestOggTitleComesFromTheFile 钉住 Ogg 的标题标签。
//
// ⚠️ 这一条来自评审：白名单里有 `.ogg`（照抄 Biu），而我们原来在 `Title`
// 里**没有这一支**——带真标题的 Ogg 显示的是文件名。那是「元信息从文件
// 读」这条要求上一个明明白白的缺口，Biu 那边用 music-metadata 是读了的。
//
// Ogg 的注释头和 FLAC 的 `VORBIS_COMMENT` 是同一个结构，所以底下那个
// `vorbisTitle` 两边共用。
func TestOggTitleComesFromTheFile(t *testing.T) {
	root := t.TempDir()
	oggWith(t, filepath.Join(root, "name-only.ogg"), "Embedded Ogg Title")

	res := Scan([]string{root}, nil)
	if len(res.Tracks) != 1 {
		t.Fatalf("扫到 %d 首", len(res.Tracks))
	}
	if got := res.Tracks[0].Title; got != "Embedded Ogg Title" {
		t.Errorf("标题是 %q（= 文件名），want 从 Ogg 注释里读出来的标题", got)
	}
	if !res.Tracks[0].Playable() {
		t.Error("Ogg 是放得出的（audio/vorbis 已注册），Playable 却是 false")
	}
}

// TestOggSurvivesTruncatedFiles 钉住「读不出来就当没有，而且不许崩」。
//
// 音乐目录里放着拷到一半的 Ogg 是常事。解析器必须**不 panic**——它在后台扫描
// 的 goroutine 上跑，崩了就是整个应用没了。
//
// ⚠️ 这里**不断言「截断了就读不到标题」**：注释头那一包只要还完整，截断的
// 文件读出标题是**对的**（第 63 字节那一份就还能读出整条注释）。要断的是
// 「不许崩」这一条。
func TestOggSurvivesTruncatedFiles(t *testing.T) {
	root := t.TempDir()
	full, err := os.ReadFile(oggWith(t, filepath.Join(root, "完整.ogg"), "T"))
	if err != nil {
		t.Fatal(err)
	}
	// 上界含 `len(full)` 本身。
	for n := 1; n <= len(full); n++ {
		p := put(t, filepath.Join(root, "截断"+strconv.Itoa(n)+".ogg"), full[:n])
		_ = Title(p) // 不许崩
	}
}

// TestID3v22Title 钉住 v2.2 的帧头只有「3 字节帧号 + 3 字节长度」。
//
// ⚠️ 这一条来自评审：**帧标志是 v2.3 才加的**，v2.2 没有。按 v2.3 的走法
// 无条件多跳 2 字节，`TT2` 的正文就从中间被切掉 2 字节开读，**合法的 v2.2
// 标题一律读不到**（返回空串，然后界面上退回文件名——看不出是标签坏了）。
//
// 拿掉修复的方式：把 `flagLen` 恒定回 2。
func TestID3v22Title(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "老标签.mp3")

	// 一个**没有帧标志**的 v2.2 标签（v2.3 的 TT2 之前会多两字节 0）。
	put(t, path, id3v22Tag("Tagged title"))
	if got := Title(path); got != "Tagged title" {
		t.Errorf("v2.2 的标签解成了 %q，want %q", got, "Tagged title")
	}
}

// id3v22Tag 造一个 v2.2 的 ID3 标签：一个 `TT2` 帧，**帧后面没有标志字节**。
func id3v22Tag(title string) []byte {
	body := append([]byte{3}, []byte(title)...) // 编码 3 = UTF-8
	frame := append([]byte("TT2"),
		byte(len(body)>>16), byte(len(body)>>8), byte(len(body))) // 3 字节长度
	frame = append(frame, body...)

	tag := append([]byte("ID3"), 2, 0, 0)
	tag = append(tag, syncsafeBytes(len(frame))...)
	return append(tag, frame...)
}

// TestTruncatedAudioFilesNeverPanic 钉住「畸形文件不许把应用带走」。
//
// ⚠️ 这一条来自评审，而且它当时的后果**不是某一页画不出来，是整个进程退出
// （退出码 2）**：解析器在一个四字节的 `RIFF` 上 `b[8:]` 越界，而这一段跑在
// **后台扫描的 goroutine** 上——界面层的 `Guard`、行级的 recover、
// `widgets/list` 那个 `fetchOne` 的 recover，一个都接不住它。
//
// 这里逐字节截断四种真格式（每一刀都切在某个结构的中途），要求**一个都不
// panic**。音乐目录里放着拷到一半的文件是常事。
//
// 拿掉修复的方式：把 `riffTitle` 里那句 `if len(b) < 12` 去掉。
func TestTruncatedAudioFilesNeverPanic(t *testing.T) {
	root := t.TempDir()
	full := map[string]string{
		"完整.wav":    wavPath(t, root, "完整.wav", "WAV 的标题"),
		"完整.flac":   flacPath(t, root, "完整.flac", "FLAC 的标题"),
		"完整.ogg":    oggPath(t, root, "完整.ogg", "Ogg 的标题"),
		"完整.aiff":   aiffPath(t, root, "完整.aiff", "AIFF 的标题"),
		"完整.m4a":    m4aPath(t, root, "完整.m4a", 0),
		"只有四字节.wav": put(t, filepath.Join(root, "只有四字节.wav"), []byte("RIFF")),
	}
	for name, path := range full {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// ⚠️ **上界要含 `len(b)` 本身**（少一刀就正好漏掉「四字节 `RIFF`」
		// 那个会越界的形状——它需要**恰好 4 字节**，不是「不足 4 字节」）。
		//
		// ⚠️ 文件名里**必须保住原扩展名**：`Title` 是按扩展名派发的，
		// 写成「截断_完整.wav_4」的话后缀是 `.wav_4`，解析器**根本没被调到**，
		// 这一条就白跑了。
		ext := filepath.Ext(name)
		for n := 1; n <= len(b); n++ {
			p := put(t, filepath.Join(root,
				"截断_"+strconv.Itoa(n)+ext), b[:n])
			// 只要求「不 panic」。标题读不读得出来随长度变。
			_ = Title(p)
		}
	}
}

// wavPath / flacPath / oggPath / aiffPath / m4aPath 是「造一个带标题的文件、
// 再把它的内容读回来」的小工具（`TestTruncatedAudioFilesNeverPanic` 用）。
func wavPath(t *testing.T, root, name, title string) string {
	return wavWith(t, filepath.Join(root, name), title)
}

func flacPath(t *testing.T, root, name, title string) string {
	return flacWith(t, filepath.Join(root, name), title)
}

func oggPath(t *testing.T, root, name, title string) string {
	return oggWith(t, filepath.Join(root, name), title)
}

func aiffPath(t *testing.T, root, name, title string) string {
	return aiffWith(t, filepath.Join(root, name), title)
}

func m4aPath(t *testing.T, root, name string, _ int) string {
	return m4aWith(t, filepath.Join(root, name), "M4A 的标题", 64)
}
