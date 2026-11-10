package app

import (
	"strconv"
	"strings"
)

const pageGroupPrefix = "pages:"

// PageSpec 是一个视频分集的队列层表示。
type PageSpec struct {
	Cid   string
	Index int
	Title string
}

// PageSet 是一个视频的完整分集列表。
type PageSet struct {
	Bvid       string
	VideoTitle string
	Parts      []PageSpec
}

// AdoptPagesResult 描述一次分集采用后的当前项位置。
type AdoptPagesResult struct {
	Current      Item
	CurrentIndex int
	Added        int
}

func pageGroupKey(bvid string) string {
	if bvid == "" {
		return ""
	}
	return pageGroupPrefix + bvid
}

// AdoptPages 将当前播放项原子地升级为真实分集队列。
//
// expected 必须仍是当前队列项的完整值。已有合集项不被页面组覆盖，避免破坏
// 合集抽屉和合集单组限制。重复采用不会插入重复分集。
func (q *Queue) AdoptPages(expected Item, pages PageSet) (AdoptPagesResult, bool) {
	q.mu.Lock()
	result, ok := q.adoptPagesLocked(expected, pages)
	q.mu.Unlock()
	if !ok {
		return AdoptPagesResult{}, false
	}
	q.changed()
	q.invalidate()
	return result, true
}

func (q *Queue) adoptPagesLocked(expected Item, pages PageSet) (AdoptPagesResult, bool) {
	if q.cur < 0 || q.cur >= len(q.items) || q.items[q.cur] != expected {
		return AdoptPagesResult{}, false
	}
	if pages.Bvid == "" || expected.Bvid == "" || pages.Bvid != expected.Bvid {
		return AdoptPagesResult{}, false
	}
	if expected.GroupKey != "" && !strings.HasPrefix(expected.GroupKey, pageGroupPrefix) {
		return AdoptPagesResult{}, false
	}

	specs := normalizePageSpecs(pages)
	if len(specs) == 0 {
		return AdoptPagesResult{}, false
	}
	currentPage, ok := pageSpecForCurrent(expected, specs)
	if !ok {
		return AdoptPagesResult{}, false
	}

	groupKey := pageGroupKey(pages.Bvid)
	groupCount := len(specs)
	oldCur := q.cur
	oldID := expected.Identity()
	known := make(map[string]bool, len(q.items))
	specByCID := make(map[string]PageSpec, len(specs))
	for _, spec := range specs {
		specByCID[spec.Cid] = spec
	}
	for _, it := range q.items {
		if it.Bvid == pages.Bvid && it.Cid != "" {
			known[it.Cid] = true
		}
	}

	apply := func(it *Item, spec PageSpec) {
		it.Cid = spec.Cid
		it.PageIndex = spec.Index
		it.TotalPage = groupCount
		it.GroupKey = groupKey
		it.GroupIndex = spec.Index
		it.GroupCount = groupCount
		it.GroupTitle = pages.VideoTitle
		it.Title = pageTitle(pages.VideoTitle, spec)
	}

	changed := false
	for i := range q.items {
		if q.items[i].Bvid != pages.Bvid {
			continue
		}
		if q.items[i].GroupKey != "" && !strings.HasPrefix(q.items[i].GroupKey, pageGroupPrefix) {
			continue
		}
		spec, hasSpec := specByCID[q.items[i].Cid]
		if (hasSpec && q.items[i].Cid != "") || (q.items[i].Cid == "" && q.items[i].Identity() == oldID) {
			if !hasSpec {
				spec = currentPage
			}
			before := q.items[i]
			apply(&q.items[i], spec)
			changed = changed || q.items[i] != before
		}
	}
	if q.items[oldCur].Cid != currentPage.Cid {
		apply(&q.items[oldCur], currentPage)
		changed = true
	}

	beforeItems := make([]Item, 0, currentPage.Index-1)
	afterItems := make([]Item, 0, groupCount-currentPage.Index)
	for _, spec := range specs {
		if spec.Cid == currentPage.Cid || known[spec.Cid] {
			continue
		}
		item := expected
		apply(&item, spec)
		if spec.Index < currentPage.Index {
			beforeItems = append(beforeItems, item)
		} else {
			afterItems = append(afterItems, item)
		}
	}
	if len(beforeItems) > 0 {
		for i, j := 0, len(beforeItems)-1; i < j; i, j = i+1, j-1 {
			beforeItems[i], beforeItems[j] = beforeItems[j], beforeItems[i]
		}
	}

	if len(beforeItems) > 0 || len(afterItems) > 0 {
		items := make([]Item, 0, len(q.items)+len(beforeItems)+len(afterItems))
		items = append(items, q.items[:oldCur]...)
		items = append(items, beforeItems...)
		newCur := len(items)
		items = append(items, q.items[oldCur])
		items = append(items, afterItems...)
		items = append(items, q.items[oldCur+1:]...)
		q.items = items
		q.cur = newCur
		if q.undo.prevCur >= oldCur {
			q.undo.prevCur += len(beforeItems)
		}
		q.bagAddItemsLocked(append(beforeItems, afterItems...))
		changed = true
	}

	if !changed {
		return AdoptPagesResult{}, false
	}
	newID := q.items[q.cur].Identity()
	q.replaceIdentityLocked(oldID, newID)
	q.bumpRevisionLocked()
	return AdoptPagesResult{Current: q.items[q.cur], CurrentIndex: q.cur, Added: len(beforeItems) + len(afterItems)}, true
}

func normalizePageSpecs(pages PageSet) []PageSpec {
	seen := make(map[string]bool, len(pages.Parts))
	specs := make([]PageSpec, 0, len(pages.Parts))
	for i, spec := range pages.Parts {
		if spec.Cid == "" || seen[spec.Cid] {
			continue
		}
		if spec.Index <= 0 {
			spec.Index = i + 1
		}
		if spec.Index <= 0 || seenIndex(specs, spec.Index) {
			continue
		}
		seen[spec.Cid] = true
		specs = append(specs, spec)
	}
	for i := range specs {
		for j := i + 1; j < len(specs); j++ {
			if specs[j].Index < specs[i].Index {
				specs[i], specs[j] = specs[j], specs[i]
			}
		}
	}
	return specs
}

func seenIndex(specs []PageSpec, index int) bool {
	for _, spec := range specs {
		if spec.Index == index {
			return true
		}
	}
	return false
}

func pageSpecForCurrent(current Item, specs []PageSpec) (PageSpec, bool) {
	if current.Cid != "" {
		for _, spec := range specs {
			if spec.Cid == current.Cid {
				return spec, true
			}
		}
		return PageSpec{}, false
	}
	if current.PageIndex > 0 {
		for _, spec := range specs {
			if spec.Index == current.PageIndex {
				return spec, true
			}
		}
	}
	return specs[0], true
}

func pageTitle(videoTitle string, spec PageSpec) string {
	if spec.Title != "" {
		return spec.Title
	}
	if videoTitle != "" {
		return videoTitle + " P" + strconv.Itoa(spec.Index)
	}
	return "P" + strconv.Itoa(spec.Index)
}
