package evidence_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/evidence"
)

// 台账自己会不会过期。
//
// 这一组只有一件事：**证据必须真的存在**。
//
// 台账里写的是测试函数名，而函数名是会变的——被删、被挪、被改成更准确的名字。
// 台账不会跟着变，于是它开始说谎：读起来「每一项都有验收证据」，而实际上
// 那条测试三个月前就没了，而且**没有任何东西会红**。
//
// 所以这里每次都去全仓库扫一遍 `func TestXxx(`。代价是一次目录遍历，
// 换来的是「台账撒谎」这件事不可能悄悄发生。

// repoRoot 是仓库根（这个包在 internal/evidence 下）。
const repoRoot = "../.."

// testFunc 认得出「这是一个测试函数」。
var testFunc = regexp.MustCompile(`^func (Test[A-Za-z0-9_]*)\(`)

// TestEveryPieceOfEvidenceReallyExists 是这一组的主线。
func TestEveryPieceOfEvidenceReallyExists(t *testing.T) {
	have := allTestNames(t)

	for _, it := range evidence.Items {
		for _, name := range it.Tests {
			if !have[name] {
				t.Errorf("台账里「%s」那条写着证据 %q，仓库里没有这个测试——"+
					"要么它被删/改名了，要么这里写错了名字", it.Spec, name)
			}
		}
	}
}

// TestEveryCategoryHasEvidence 盯「少了一整组」。
//
// 少一条会红（上面那条），少**一组**原本不会——那是一段空白的表格，
// 读起来仍然像一份完整的清单。这条把 SPEC 点名的几组钉死。
func TestEveryCategoryHasEvidence(t *testing.T) {
	got := map[string]int{}
	for _, it := range evidence.Items {
		got[it.Category] += len(it.Tests)
	}
	for _, c := range evidence.Categories {
		if got[c] == 0 {
			t.Errorf("「%s」这一组一条证据都没有（SPEC 的「各阶段要有可保存的回归证据」点名了它）", c)
		}
	}
	// 反过来也钉一下：台账里不该冒出规格没有的组——那通常意味着拼错了。
	known := map[string]bool{}
	for _, c := range evidence.Categories {
		known[c] = true
	}
	for _, it := range evidence.Items {
		if !known[it.Category] {
			t.Errorf("台账里出现了规格之外的分类 %q（写在「%s」那条上）", it.Category, it.Spec)
		}
	}
}

// TestEveryItemSaysSomethingAndCarriesSomething 盯空条目。
//
// 一条没有证据的要求在台账里长得和有证据的一模一样；一条没有说法的证据
// 则没人知道它量的是哪一半（SPEC 那句「模式切换、队列顺序、取消代次」
// 拆成三条还是一条，看的就是这个）。
func TestEveryItemSaysSomethingAndCarriesSomething(t *testing.T) {
	for i, it := range evidence.Items {
		if strings.TrimSpace(it.Spec) == "" {
			t.Errorf("台账第 %d 条没有说法", i)
		}
		if len(it.Tests) == 0 {
			t.Errorf("台账第 %d 条（%s）一条证据都没有", i, it.Spec)
		}
	}
}

// TestTheLedgerIsNotTiny 盯「台账被清空了也照样绿」。
//
// 上面四条都拦得住「写错」，拦不住「有人把 Items 清成空数组」
// ——那时候每一组都该红（`TestEveryCategoryHasEvidence`），
// 所以这一条其实只是把那个后果再说一遍：一份空的台账不是一份台账。
func TestTheLedgerIsNotTiny(t *testing.T) {
	if len(evidence.Items) < 30 {
		t.Errorf("台账只有 %d 条，清空成空数组的话没人觉得是出了问题", len(evidence.Items))
	}
	// 真机那一半要占一点——全绿的那几组里没有它，台账会显得比实际完整。
	real := 0
	for _, it := range evidence.Items {
		if it.RealOnly {
			real++
		}
	}
	if real == 0 {
		t.Error("台账里一条「真机才量得到」的都没有：SPEC 点名的红黄绿、输入法、系统音频就没人记了")
	}
}

// allTestNames 扫出整个仓库里所有测试函数的名字。
func allTestNames(t *testing.T) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// .git 和构建产物里没有测试，跳过省时间。
			if n := d.Name(); n == ".git" || n == "build" || n == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for line := range strings.SplitSeq(string(b), "\n") {
			if m := testFunc.FindStringSubmatch(line); m != nil {
				names[m[1]] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫仓库失败：%v", err)
	}
	return names
}
