package ui

// 「画出来的每一行都放得了」。
//
// 这条是被 Biu 的判据坑出来的。`internal/service/bili/collection.go` 的
// `Media.Playable()` 照抄了 Biu 的 `canNotPlay = ![2, 12].includes(type)`——
// **判据抄对了**，而那时的我们**没实现的那一条路（type=12，B 站音乐库）没有被跟上**。
// 当时于是：
//
//	收藏夹里那条照常画成一行 → 照常进队列（`itemsOfMedias` 信了判据）→
//	抽屉里还标着「B 站音乐」→ 用户按播放 → `app.playableOnline` 才拒绝
//
// 于是「可放」成了一个界面上的谎言，而**一路绿过所有测试**：判据本身是对的，
// 判据的测试也是绿的，只有「真的按下去」那一刻才会说话。
//
// （type=12 那一路 BMG-50 接上了，所以下面表里它现在没有欠条；合集 / 电影那一类
// 判据一直是对的，而 `.wma` 那一行还有一张。）
//
// 这一份钉的就是那一刻。三条：
//
//  1. **判据说的和表上写的一致**——判据不许自己长（改判据就要改这张表）；
//  2. **判据说「能放」的行，照界面那条路翻成队列项、再按播放，同步那一步不许
//     拒绝它**；
//  3. **判据说「放不了」的行，不许进队列**（不然「播放全部」会因为收藏夹里躺着
//     两个合集就整条失败）。
//
// 第 2 条对 **type=12 欠过**（BMG-47 §3 A31 → BMG-50）：那一路取流
// （`/audio/music-service-c/url`）以前没接，而判据是从 Biu 抄的、抄对了。欠着的
// 写法是 `Debt`：必须点名到票或审计编号，必须写出用户会看到的那句话。那张票接上
// 之后把 `Debt` 删掉即可——**断言会自己开始要求它真放得了**，而 `Debt` 留着的话
// 它会立刻反过来报「欠条已经过期了」。今天表上还有一张欠条，是本地音乐的 `.wma`。
//
// ⚠️ 为什么当时不写成「直接红」：那一票的边界是不动功能行为，而 type=12 要真放得了
// 得补三个接口（取流 / 元信息 / 收藏态）。把一个已知缺口写成一段红的测试，只会让
// `go test ./...` 永远是红的，于是**没有人再跑它**——那比绿着更坏。把欠条写在表里，
// 它就从「一个没人知道的事实」变成「一行有名有姓的话」，而那一行在票交付的那一刻
// 自己会红。

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/service/localmusic"
)

// verdictRow 是一类会被我们画成一行、并且带着一句「能不能放」的内容。
type verdictRow struct {
	What string
	// Type 是 `bili.FavMedia.type`。
	Type int
	// Judged 是判据（`bili.Media.Playable`）说的话。
	//
	// 它是**从表里抄回来的**，不是从判据里抄的——方向反了的话，这张表就只是判据
	// 的一份影子，判据说谎的时候表会跟着说一样的谎，而表存在的意义恰恰是当那个
	// 不一样的声音。
	Judged bool
	// Debt 是「判据说能放、实际还放不了」的欠条：点名到票或者审计编号。
	Debt string
	// Refuses 是用户真的按下去时会看到的那半句。它也一起钉住，所以那句话不会
	// 悄悄从「还没接上」变成一句看不懂的错。
	Refuses string
}

// verdictLedger 是那张表本体。
var verdictLedger = []verdictRow{
	{
		What: "投稿（type=2）", Type: bili.MediaVideo, Judged: true,
	},
	{
		// 这一行**欠过**（BMG-47 §3 A31 → BMG-50）：判据是从 Biu 抄的、抄对了，
		// 而 sid 那一路的三个接口（取流 `song/url`、元信息 `song/info`、收藏态
		// `coll/resource/check`）一个都没接。BMG-50 接上了，所以这里**没有 Debt**，
		// 第 2 条现在要求它真放得了（`TestEveryPlayableEntryCanActuallyBeStreamed`
		// 在 `internal/app` 那一侧钉着「真的取得到流」）。
		What: "B 站音乐库（type=12）",
		Type: bili.MediaAudio,
		// ⚠️ 加一种类型到 `Playable` 里就要连着在 `app.streamFor` 里加一条取流路径。
		// 少加的那一半，症状是「这一行照常画、照常进队列，按播放才报错」。
		Judged: true,
	},
	{
		// 合集 / 电影也在收藏夹里躺着，它们没有音频流。判据把它们排除在批量操作
		// 之外（而不是放进去再报错），这一半是对的。
		//
		// 🔴 **它说的话要说清是哪一种**：合集/电影是「这一条本来就是合集，
		// 里面才有单集」（下一步是打开里面的单集），而**没挂稿件的推荐条目**
		// 是「B 站没给它挂能播的稿件」（下一步是搜同名）。混成一句
		// 「不是投稿」的话，用户照着做的下一步就不对。
		What: "合集（type=21）", Type: 21, Judged: false,
		Refuses: "合集/剧集",
	},
	{
		What: "电影（type=24）", Type: 24, Judged: false,
		Refuses: "合集/剧集",
	},
}

// TestEveryRowWeDrawHasAVerdictThatMatchesWhatHappens 是那张表的主线。
func TestEveryRowWeDrawHasAVerdictThatMatchesWhatHappens(t *testing.T) {
	b := newUIExplore(t)
	s, tt, a := newSpaceShell(t, b, true, "77")
	_ = tt
	defer func() { _ = s.hist.Close() }()

	for _, r := range verdictLedger {
		t.Run(r.What, func(t *testing.T) {
			m := bili.Media{ID: 9001, Type: r.Type, Title: "一行", Bvid: "BVverdict01"}

			// ① 判据说的话必须和表上写的一致。改判据不改表（或者反过来）就红。
			if got := m.Playable(); got != r.Judged {
				t.Fatalf("%s：判据说能放=%v，表上写的是 %v——判据不许自己长", r.What, got, r.Judged)
			}

			// ② 判据说能放的必须进得了队列；说放不了的必须进不去。
			//    「播放全部」走的就是 `app.QueueItemsOfMedias`，而它信判据——判据错了，
			//    「播放全部」就会因为收藏夹里躺着两个合集而整条失败。
			items := app.QueueItemsOfMedias([]bili.Media{m})
			if r.Judged && len(items) != 1 {
				t.Fatalf("%s：判据说能放，批量「播放全部」却把它跳过了（app.QueueItemsOfMedias 出了 %d 项）", r.What, len(items))
			}
			if !r.Judged && len(items) != 0 {
				t.Fatalf("%s：判据说放不了，它却进了队列（%d 项）", r.What, len(items))
			}
			if !r.Judged {
				// 放不了的那一类，判据说出去之后界面上要有一句人话，而不是一行
				// 画出来按不动的死条目。这里钉的是 `app.playableOnline` 说的话：
				// 它要**说清是哪一种**（表里 `Refuses` 那一列），并且带着
				// 「还能怎么办」（票 #209：每一句都要答「为什么」和「怎么办」）。
				err := a.PlayFrom(onlineItemOf(m))
				if err == nil {
					t.Errorf("%s：判据说放不了，按播放却没给一句话", r.What)
				} else if !strings.Contains(err.Error(), r.Refuses) {
					t.Errorf("%s：拒它的理由是 %q，没说清是哪一种（want %q）", r.What, err, r.Refuses)
				} else if !strings.Contains(err.Error(), "打开里面的单集") {
					t.Errorf("%s：拒它的理由是 %q，没有说「还能怎么办」", r.What, err)
				}
				return
			}

			// ③ 判据说能放的那一行，按下去必须真的起得了流。这一步要走**界面
			//    用的那条路**（`onlineItemOf` → `App.PlayFrom`），不是直接调判据：
			//    判据是「我们认为它是内容」，而这一步才是「我们放得了它」。
			err := a.PlayFrom(onlineItemOf(m))
			if r.Debt == "" {
				if err != nil {
					t.Fatalf("%s：判据说能放，用户按播放却被拒了：%v", r.What, err)
				}
				return
			}
			// 欠着的那一类：拒是应该的，但必须拒得**指名道姓**，而且欠条必须
			// 写清楚差什么（否则这一行就成了万能借口）。
			//
			// 🔴 这里必须**红**，不能只 `t.Logf`。写欠条的那一半（真的放不了）
			// 是本票加的，而它自己得能被证伪：给一行**真放得了**的内容伪造一张
			// 欠条，正是「欠条赖着不走」那种形态——数据早就在那儿了，表上还写着
			// 「差三个接口」，而 `go test ./...` 一片绿。`t.Logf` 在普通的一跑里
			// 根本看不见，那时这张欠条和没写一样。`pageDebt`（realdata_test.go）
			// 的 `NowEmpty` 是同一个道理，两边都得能红。
			if err == nil {
				t.Fatalf("%s：欠条 %s 写着「这一行现在还放不了」，可它按下去根本没被拒——"+
					"欠条已经过期了，撤掉 Debt（撤掉之后这一行就会被钉成「真放得了」）", r.What, r.Debt)
			}
			if !strings.Contains(err.Error(), r.Refuses) {
				t.Errorf("%s：拒它的理由是 %q，want 含 %q（那句人话不该悄悄变）", r.What, err, r.Refuses)
			}
			t.Logf("🔴 %s：判据说能放、实际放不了（欠条 %s），用户按下去看到的是「%s」",
				r.What, r.Debt, err)
		})
	}
}

// TestEveryContentTypeInTheCodebaseIsOnTheVerdictLedger 盯「加了一类内容，表里忘了加一行」。
//
// 少一行是**静默**的：判据对那一类照样绿，而那一类从那天起就没有人问过「它到底放
// 放得了」。这里用的是台账那一套办法——**扫源码里的常量**（`evidence_test.go`
// 扫的是测试函数名，同一个思路）：`internal/service/bili` 里每一个 `MediaXxx = n`
// 都必须在表上有一行。
var mediaConst = regexp.MustCompile(`(?m)^\s*Media[A-Za-z0-9_]*\s*=\s*(\d+)\s*$`)

func TestEveryContentTypeInTheCodebaseIsOnTheVerdictLedger(t *testing.T) {
	onLedger := map[int]bool{}
	for _, r := range verdictLedger {
		onLedger[r.Type] = true
	}
	dir := filepath.Join("..", "service", "bili")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range mediaConst.FindAllStringSubmatch(string(raw), -1) {
			found++
			n, err := strconv.Atoi(m[1])
			if err != nil {
				t.Fatal(err)
			}
			if !onLedger[n] {
				t.Errorf("%s 里声明了内容类型 %d，而「画出来的每一行都放得了」那张表上没有它——"+
					"加一行：它到底放不放得了", e.Name(), n)
			}
		}
	}
	if found == 0 {
		t.Fatal("一个 Media 常量都没扫到：扫法坏了，它现在什么都拦不住")
	}
}

// TestEveryListedExtensionIsToldApartFromThePlayableOnes 是同一条要求的另一半：
// 本地音乐那一类内容。
//
// 形状不一样——B 站音乐库那条是**判据说谎**（说能放、实际放不了），本地音乐这一边
// 是**说清楚了**（`.wma` 列出来，界面上明写「我们还放不出的格式」，PLAN §5.4 的
// 原话是「不要含糊过去」）。所以这一条钉的不是「不许有放不了的」，而是
// **「放不了的必须在表上，而且必须有一张欠条」**——不许悄悄多一个白名单外的
// 扩展名。
var extLedger = []struct {
	Ext     string
	Judged  bool
	Debt    string
	Refuses string
}{
	{Ext: ".mp3", Judged: true},
	{Ext: ".flac", Judged: true},
	{Ext: ".wav", Judged: true},
	{Ext: ".m4a", Judged: true},
	{Ext: ".aac", Judged: true},
	{Ext: ".ogg", Judged: true},
	{Ext: ".aiff", Judged: true},
	{Ext: ".aif", Judged: true},
	{Ext: ".aifc", Judged: true},
	{
		// 白名单里有、我们放不了的那一个（上游那一份白名单照抄过来的）。
		// 界面上必须说清——`TestAnUnplayableFormatIsNamedAsSuch` 钉的是那一半。
		Ext: ".wma", Judged: false,
		Debt: "PLAN §5.4（拍板「待定」，不计入排期）：Windows 上的 WMA 解码没做",
	},
}

func TestEveryListedExtensionIsToldApartFromThePlayableOnes(t *testing.T) {
	onLedger := map[string]bool{}
	for _, r := range extLedger {
		onLedger[r.Ext] = true
		if got := localmusic.Playable(r.Ext); got != r.Judged {
			t.Errorf("%s：判据说能放=%v，表上写的是 %v", r.Ext, got, r.Judged)
		}
	}
	for _, ext := range localmusic.Extensions {
		if !onLedger[ext] {
			t.Errorf("扫描白名单里有 %s，而「放不放得了」那张表上没有它", ext)
		}
	}
	for _, r := range extLedger {
		if r.Judged || r.Debt == "" {
			continue
		}
		t.Logf("🔴 %s：列出来了、放不了（欠条 %s）——界面上必须有一句人话，见 TestAnUnplayableFormatIsNamedAsSuch",
			r.Ext, r.Debt)
	}
}
