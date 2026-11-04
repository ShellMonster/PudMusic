// 热榜名次的本地记录：算「本周最热」那一列的升降。
//
// B 站的榜接口只给当期名次，不给上一期，所以升降只能自己记：每拉到一期新的榜，就把
// 上一期的名次留下来，两期一比就是升降。记在应用数据目录里的一个小 JSON 里，
// 读不出来、写不进去都只是「没有升降」，不影响榜本身。
//
// 没有上一期（第一次用、或者新上榜）的歌没有升降，界面画「—」。
package app

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

const chartHistoryFile = "chart-history.json"

// ChartHistoryPath 是名次记录在应用数据目录里的路径。
func ChartHistoryPath(dir string) string { return filepath.Join(dir, chartHistoryFile) }

// chartRanks 是一期榜的名次：行身份 → 名次（从 1 起）。
type chartRanks struct {
	Period int64          `json:"period"`
	Ranks  map[string]int `json:"ranks"`
}

// chartHistory 是落盘的内容：当期和上一期。
type chartHistory struct {
	Current chartRanks `json:"current"`
	Prev    chartRanks `json:"prev"`
}

// SetChartHistoryPath 挂上名次记录并读盘。**开窗口之前调**；path 为空就只在内存里记。
func (r *Recommend) SetChartHistoryPath(path string) {
	if r == nil {
		return
	}
	var h chartHistory
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			if json.Unmarshal(b, &h) != nil {
				h = chartHistory{}
			}
		}
	}
	r.mu.Lock()
	r.chartPath, r.chart = path, h
	r.mu.Unlock()
}

// noteChart 记下刚拉到的这一期（ids 按名次排）。同一期重复拉只刷新当期，不动上一期；
// 换了新的一期就把当期挪成上一期。
func (r *Recommend) noteChart(period int64, ids []string) {
	if period == 0 || len(ids) == 0 {
		return
	}
	ranks := make(map[string]int, len(ids))
	for i, id := range ids {
		if _, dup := ranks[id]; !dup {
			ranks[id] = i + 1
		}
	}
	r.mu.Lock()
	if r.chart.Current.Period != period {
		if r.chart.Current.Period != 0 {
			r.chart.Prev = r.chart.Current
		}
		r.chart.Current = chartRanks{Period: period, Ranks: ranks}
	} else {
		r.chart.Current.Ranks = ranks
	}
	h, path := r.chart, r.chartPath
	r.mu.Unlock()
	if path == "" {
		return
	}
	b, err := json.Marshal(h)
	if err == nil {
		err = os.WriteFile(path, b, 0o600)
	}
	if err != nil {
		log.Printf("pudmusic: 保存热榜名次失败：%v", err)
	}
}

// ChartTrend 报这一首相对上一期升降了几名：正数是上升、负数是下降、0 是持平。
// 没有上一期的记录、或者上一期不在榜上时 ok 为 false（界面画「—」）。
func (r *Recommend) ChartTrend(id string) (delta int, ok bool) {
	if r == nil {
		return 0, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, okc := r.chart.Current.Ranks[id]
	prev, okp := r.chart.Prev.Ranks[id]
	if !okc || !okp {
		return 0, false
	}
	return prev - cur, true
}
