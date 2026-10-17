package localmusic

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 删文件是这个应用里**唯一**会永久毁掉用户数据的动作，所以这一组测试盯的
// 不是「有没有报错」，而是**磁盘上还剩下什么**。
//
// 每一条规矩都有一个「拿掉修复会不会红」的对照，注释里写清楚那是哪一种拿法。

// exists 报路径还在不在。
func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	return err == nil
}

// trOf 造一个「扫描时看到的那一项」：路径是列表里那个，Canonical 是那一刻
// 解析出来的真实路径。
//
// 这一层现在收的是整个 `Track`（它要拿 Canonical 做身份检查），而这些用例
// 大多只想验删除那几条规矩、不想先跑一遍扫描，所以自己造一份。真实的
// 扫描走的是 `Scan`，那条路由 `TestDeleteRefusesAPathWhoseFileMoved` 那几条验。
func trOf(t *testing.T, path string) Track {
	t.Helper()
	canon, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s)：%v", path, err)
	}
	return Track{Path: path, Canonical: canon}
}

// TestDeleteActuallyRemovesTheFile 钉住最基本的那一条：**真的删**。
//
// 规格第 83 条是「我想真的从磁盘删掉某个本地音乐文件」。只把列表里那一行
// 去掉（不发一条 unlink）是最像的一种「实现了」——用户关了应用一看歌还在。
func TestDeleteActuallyRemovesTheFile(t *testing.T) {
	root := t.TempDir()
	song := songPath(t, root, "歌.mp3")

	if err := Delete(trOf(t, song), []string{root}); err != nil {
		t.Fatal(err)
	}
	if exists(t, song) {
		t.Fatal("文件还在盘上")
	}
	// 目录本身不能跟着没了。
	if !exists(t, root) {
		t.Fatal("目录被一起删了")
	}
}

// TestDeleteRefusesAPathOutsideEveryRoot 钉住规矩 1。
//
// 拿掉修复的方式：去掉 `underAnyRoot` 那一段检查。这条立刻红。
//
// 为什么要有这一条：列表里的路径是从设置里的根拼出来的，正常情况下不会
// 落在外面。会有的是——设置被改过（用户删了一个目录，而界面上还是旧的一
// 帧）、别的代码塞了路径进来、或者一份落盘状态还没刷新。没有这道门，
// 「删一首」就可能变成「删掉任何一个用户能说出路径的文件」。
func TestDeleteRefusesAPathOutsideEveryRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	victim := songPath(t, outside, "别人的歌.mp3")

	err := Delete(trOf(t, victim), []string{root})

	if !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("错是 %v，want ErrOutsideRoots", err)
	}
	if !exists(t, victim) {
		t.Fatal("音乐目录外面的文件被删了")
	}
}

// TestDeleteRefusesASiblingDirectoryWithTheSamePrefix 钉住前缀比较那个坑。
//
// `/音乐` 和 `/音乐备份`：字符串前缀比较会说 `/音乐备份/歌.mp3` 在
// `/音乐` 下面。差一点就是删掉用户另一个目录里的歌。
//
// 拿掉修复的方式：把 `filepath.Rel` 换成 `strings.HasPrefix`。
func TestDeleteRefusesASiblingDirectoryWithTheSamePrefix(t *testing.T) {
	parent := t.TempDir()
	music := filepath.Join(parent, "音乐")
	sibling := filepath.Join(parent, "音乐备份")
	if err := os.MkdirAll(music, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := songPath(t, sibling, "歌.mp3")

	if err := Delete(trOf(t, victim), []string{music}); !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("错是 %v，want ErrOutsideRoots", err)
	}
	if !exists(t, victim) {
		t.Fatal("隔壁目录的文件被删了")
	}
}

// TestDeleteRefusesADirectory 钉住规矩 2 的头一条。
//
// `os.Remove` 对**空**目录是成功的——所以没有这道检查的话，「删一首」在
// 用户把歌删光之后就会把他整个音乐文件夹删掉，而且不报任何错。
func TestDeleteRefusesADirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "空的文件夹")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := Delete(trOf(t, dir), []string{root})

	if !errors.Is(err, ErrNotRegular) {
		t.Fatalf("错是 %v，want ErrNotRegular", err)
	}
	if !exists(t, dir) {
		t.Fatal("目录被删了")
	}
}

// TestDeleteRefusesANonEmptyDirectory 钉住同一件事在**非空**时的表现。
//
// 这里不只验「没删掉」：`os.Remove` 对非空目录会失败并报 `directory not
// empty`，那句话对用户毫无意义——他看到的是「删这首失败：directory not
// empty」，而他点的是一个歌名。
func TestDeleteRefusesANonEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "专辑")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	songPath(t, dir, "里面的歌.mp3")

	err := Delete(trOf(t, dir), []string{root})

	if !errors.Is(err, ErrNotRegular) {
		t.Fatalf("错是 %v，want ErrNotRegular", err)
	}
	if !exists(t, filepath.Join(dir, "里面的歌.mp3")) {
		t.Fatal("里面的歌被删了")
	}
	if !exists(t, dir) {
		t.Fatal("目录被删了")
	}
}

// TestDeleteRemovesTheSymlinkAndNotWhatItPointsAt 是这一票的**安全线**。
//
// 符号链接处理错了就会「删到别处去」：用户的音乐目录里放着一个指向别处的
// 链接（很常见——歌词、专辑大文件放在另一块盘上），他在列表里点删除，
// 结果别处的原文件没了，而列表上那首歌还在。
//
// ⚠️ 断言的是**两件事同时成立**：链接没了，被指向的文件还在。
// 只断言其中一件都验不到 bug：只查「链接没了」的话，实现成「先
// EvalSymlinks 再删」照样绿——它把链接和目标一起处理掉了，链接确实没了。
//
// 拿掉修复的方式：在 Delete 里先 `EvalSymlinks` 再 `os.Remove`（Biu 上游
// 那边为了算身份做过 `realpath`，很容易顺手就用它删）。
func TestDeleteRemovesTheSymlinkAndNotWhatItPointsAt(t *testing.T) {
	base := t.TempDir()
	library := filepath.Join(base, "音乐")
	outside := filepath.Join(base, "别处")
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	target := songPath(t, outside, "真正的文件.mp3")
	link := filepath.Join(library, "指向别处.mp3")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("这台机器建不了符号链接：%v", err)
	}

	// 扫一遍拿列表里那一行真实的 Path（不是我们自己拼的），
	// 免得测试验的是「我们碰巧传对了」而不是「列表里那一行删得对」。
	res := Scan([]string{library}, nil)
	var row Track
	for _, tr := range res.Tracks {
		if strings.HasSuffix(tr.Path, "指向别处.mp3") {
			row = tr
		}
	}
	if row.Path == "" {
		t.Fatalf("链接那一行不在列表里：%v", pathsOf(res.Tracks))
	}

	if err := Delete(row, []string{library}); err != nil {
		t.Fatal(err)
	}
	if exists(t, link) {
		t.Error("链接还在")
	}
	if !exists(t, target) {
		t.Fatal("链接指向的文件被删了——这就是「删到别处去」")
	}
}

// TestDeleteRefusesAnEmptyRootList 钉住「用户没告诉我能删哪儿」。
//
// 传空不是「有权限删」，是「没有依据删」。这条守的是设置没挂上、
// 或者一次调用漏了参数的那条路。
func TestDeleteRefusesAnEmptyRootList(t *testing.T) {
	root := t.TempDir()
	song := songPath(t, root, "歌.mp3")

	if err := Delete(trOf(t, song), nil); !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("错是 %v，want ErrOutsideRoots", err)
	}
	if !exists(t, song) {
		t.Fatal("没有根的时候也删了")
	}
}

// TestDeleteReportsAMissingFileSeparately 钉住「它已经不在了」。
//
// 这是最常见的一种（用户先在访达里删了，再回应用点删除）。它不是一个
// 失败：界面上该说的是「已经不在了」，而不是一句红字报错。
func TestDeleteReportsAMissingFileSeparately(t *testing.T) {
	root := t.TempDir()
	gone := filepath.Join(root, "早没了.mp3")

	// 这一项**从没存在过**，所以没有 Canonical 可记（空的）——删除那一侧
	// 遇到空的 Canonical 就跳过身份检查，交给 Lstat 报「不在了」。
	if err := Delete(Track{Path: gone}, []string{root}); !errors.Is(err, ErrMissing) {
		t.Fatalf("错是 %v，want ErrMissing", err)
	}
}

// TestDeleteAcceptsAPathThatLooksLikeAnEscape 钉住 `..` 那种写法。
//
// 路径是先 `Clean` 再比的：`/音乐/../别处/歌.mp3` 清理之后就是
// `/别处/歌.mp3`，落在根外面，拒。
//
// 顺带钉住另一个坑：真叫 `..foo` 的文件不该被当成「在根外面」
// （用 `HasPrefix(rel, "..")` 判就会误伤）。
func TestDeleteAcceptsAPathThatLooksLikeAnEscape(t *testing.T) {
	parent := t.TempDir()
	music := filepath.Join(parent, "音乐")
	outside := filepath.Join(parent, "别处")
	for _, d := range []string{music, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	victim := songPath(t, outside, "歌.mp3")

	escape := filepath.Join(music, "..", "别处", "歌.mp3")
	if err := Delete(trOf(t, escape), []string{music}); !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("错是 %v，want ErrOutsideRoots", err)
	}
	if !exists(t, victim) {
		t.Fatal("用 .. 绕出去的文件被删了")
	}

	// 真叫 `..foo` 的文件在音乐目录**里面**，要能删。
	dotted := songPath(t, music, "..foo.mp3")
	if err := Delete(trOf(t, dotted), []string{music}); err != nil {
		t.Fatalf("叫 ..foo 的文件删不掉：%v", err)
	}
	if exists(t, dotted) {
		t.Error("..foo 没删掉")
	}
}

// TestDeleteAcceptsRootInItsOwnFlavor 钉住根的两种写法都对。
//
// 用户在设置里存的是他当初点的那个路径，而扫描时 `Scan` 会把它变成绝对
// 路径。两者不完全一样（比如带了个 `..`），Delete 不能再要求一次转换
// 就对得上。
func TestDeleteAcceptsRootInItsOwnFlavor(t *testing.T) {
	base := t.TempDir()
	music := filepath.Join(base, "音乐")
	if err := os.MkdirAll(music, 0o755); err != nil {
		t.Fatal(err)
	}
	song := songPath(t, music, "歌.mp3")

	// 一个带 `..`、一个带尾斜杠、一个是相对路径。
	for _, root := range []string{
		filepath.Join(base, "别的", "..", "音乐"),
		music + string(filepath.Separator),
		music,
	} {
		put(t, song, []byte("again"))
		if err := Delete(trOf(t, song), []string{root}); err != nil {
			t.Errorf("根写成 %q 时删不掉：%v", root, err)
		}
	}
}

// TestDeleteRefusesARootWithATrailingSpace 钉住「路径不许 TrimSpace」。
//
// ⚠️ 这一条来自评审：`Music `（末尾带一个空格）是完全合法的目录名，而我们
// 在根检查和路径清理上都写了 `strings.TrimSpace` —— `"/Music "` 被裁成
// `"/Music"`，**那是隔壁一个文件夹**。用户选的是 `Music `，文件却从
// `Music` 里没的。
//
// 两个方向都要断：**根**带空格被换掉，和**被删的文件**路径带空格被换掉。
// 拿掉修复的方式：把任何一处的 `TrimSpace` 加回去。
func TestDeleteRefusesARootWithATrailingSpace(t *testing.T) {
	base := t.TempDir()
	plain := filepath.Join(base, "Music")
	spaced := filepath.Join(base, "Music ") // 末尾一个空格
	for _, d := range []string{plain, spaced} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// 用户勾的是带空格那个，里面只有这一首歌。
	chosen := songPath(t, spaced, "chosen-only.mp3")
	// 隔壁那个里有**同名**的歌：一旦根被裁成 `Music`，删的就会是它。
	other := songPath(t, plain, "chosen-only.mp3")

	res := Scan([]string{spaced}, nil)
	if len(res.Tracks) != 1 {
		t.Fatalf("扫到 %d 首，want 1：%v", len(res.Tracks), pathsOf(res.Tracks))
	}
	if err := Delete(res.Tracks[0], []string{spaced}); err != nil {
		t.Fatal(err)
	}
	if exists(t, chosen) {
		t.Error("用户选的那首还在")
	}
	if !exists(t, other) {
		t.Fatal("隔壁目录里的同名文件被删了——根路径被 TrimSpace 裁成了另一个目录")
	}
}

// TestDeleteRefusesAFileWhoseOwnPathEndsInASpace 是上面那条的另一半：
// **被删的那一条路径**末尾带空格时也不能被裁。
//
// ⚠️ 只有**末尾**的空格才有危险（`TrimSpace` 只动两端），所以这一条不能拿
// `.mp3` 文件来造——`"歌 .mp3"` 里的空格在中间，裁不掉。这里造一个名字
// 真的以空格结尾的文件（`"歌 "`），再放一个去掉空格的同名诱饵。
//
// 扫出来的音乐文件不会长这样（那样就没有扩展名、进不了白名单），但
// `Delete` 是**安全线本身**：它不该在任何输入下把用户指的那个换成另一个。
//
// 拿掉修复的方式：`Delete` 里把 `filepath.Clean(path)` 改回
// `filepath.Clean(strings.TrimSpace(path))`。
func TestDeleteRefusesAFileWhoseOwnPathEndsInASpace(t *testing.T) {
	root := t.TempDir()
	spaced := songPath(t, root, "歌 ") // 名字以空格结尾
	decoy := songPath(t, root, "歌")   // 裁掉空格之后就是它

	if err := Delete(trOf(t, spaced), []string{root}); err != nil {
		t.Fatal(err)
	}
	if exists(t, spaced) {
		t.Error("带末尾空格的那个还在")
	}
	if !exists(t, decoy) {
		t.Fatal("诱饵被删了——路径被 TrimSpace 裁成了另一个文件")
	}
}

// TestDeleteRefusesAPathWhoseFileMoved 钉住规矩 4：根被换成指向别处的
// 符号链接之后，旧列表项不能把**库外**那个文件删掉。
//
// ⚠️ 这一条来自评审，也是「只查在不在某个根下面」**拦不住**的那一种：
// 把根 `library` 改名、再在原处建一个指向 `outside` 的符号链接，那么
// `library/song.mp3` 解析出来已经是 `outside/song.mp3`——一个从没被扫过、
// 也不在音乐库里的文件，而它在**词法上**仍然规规矩矩待在 `library` 底下。
//
// 拿掉修复的方式：去掉 `Delete` 里比对 `Track.Canonical` 的那三行。
func TestDeleteRefusesAPathWhoseFileMoved(t *testing.T) {
	base := t.TempDir()
	library := filepath.Join(base, "library")
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}
	songPath(t, library, "song.mp3")

	// 先扫：这一行记的是**扫描那一刻**的 `library/song.mp3`。
	res := Scan([]string{library}, nil)
	if len(res.Tracks) != 1 {
		t.Fatalf("扫到 %d 首", len(res.Tracks))
	}
	row := res.Tracks[0]

	// 扫完之后把根换掉：真身挪走，原路径变成一个指向库外的符号链接。
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := songPath(t, outside, "song.mp3") // 库外、从没被扫过
	if err := os.Rename(library, filepath.Join(base, "library-gone")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, library); err != nil {
		t.Skipf("这台机器建不了符号链接：%v", err)
	}

	err := Delete(row, []string{library})

	if !errors.Is(err, ErrMoved) {
		t.Fatalf("错是 %v，want ErrMoved", err)
	}
	if !exists(t, victim) {
		t.Fatal("库外那个从没被扫过的文件被删了")
	}
}
