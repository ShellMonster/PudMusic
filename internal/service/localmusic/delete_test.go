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

// TestDeleteActuallyRemovesTheFile 钉住最基本的那一条：**真的删**。
//
// 规格第 83 条是「我想真的从磁盘删掉某个本地音乐文件」。只把列表里那一行
// 去掉（不发一条 unlink）是最像的一种「实现了」——用户关了应用一看歌还在。
func TestDeleteActuallyRemovesTheFile(t *testing.T) {
	root := t.TempDir()
	song := songPath(t, root, "歌.mp3")

	if err := Delete(song, []string{root}); err != nil {
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

	err := Delete(victim, []string{root})

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

	if err := Delete(victim, []string{music}); !errors.Is(err, ErrOutsideRoots) {
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

	err := Delete(dir, []string{root})

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

	err := Delete(dir, []string{root})

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

	if err := Delete(row.Path, []string{library}); err != nil {
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

	if err := Delete(song, nil); !errors.Is(err, ErrOutsideRoots) {
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

	if err := Delete(gone, []string{root}); !errors.Is(err, ErrMissing) {
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
	if err := Delete(escape, []string{music}); !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("错是 %v，want ErrOutsideRoots", err)
	}
	if !exists(t, victim) {
		t.Fatal("用 .. 绕出去的文件被删了")
	}

	// 真叫 `..foo` 的文件在音乐目录**里面**，要能删。
	dotted := songPath(t, music, "..foo.mp3")
	if err := Delete(dotted, []string{music}); err != nil {
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
		if err := Delete(song, []string{root}); err != nil {
			t.Errorf("根写成 %q 时删不掉：%v", root, err)
		}
	}
}
