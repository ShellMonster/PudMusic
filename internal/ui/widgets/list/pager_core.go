package list

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"slices"
	"sync"

	"github.com/egoist/mygo/ui"
)

// ErrPagerPanic identifies a fetcher panic converted into a page error.
var ErrPagerPanic = errors.New("拉页的代码崩了")

// diagnostics; all triggers use the same loading state machine.
type PagerTrigger uint8

const (
	PagerFirstRender PagerTrigger = iota
	PagerScrollEnd
	PagerRefill
	PagerRetry
)

func (t PagerTrigger) String() string {
	switch t {
	case PagerFirstRender:
		return "首次渲染检查叫的"
	case PagerScrollEnd:
		return "滚到底叫的"
	case PagerRetry:
		return "用户点了重试"
	default:
		return "容器没填满叫的"
	}
}

// PagerPage is the transport-independent result of one page request.
type PagerPage[T any] struct {
	Items   []T
	HasMore bool
}

// PagerStatus is the shared lifecycle state used by paged views.
type PagerStatus struct {
	Page    int
	Loaded  int
	HasMore bool
	Loading bool
	Blocked bool
	Err     error
}

// PagerFetcher loads one page. The query is captured before the goroutine starts.
type PagerFetcher[T, Q any] func(ctx context.Context, page, loaded int, query Q) (PagerPage[T], error)

// Pager owns pagination lifecycle state. The view keeps its row model and calls
// apply on the UI thread to append and deduplicate rows. This keeps rendering
// concerns out of the shared state machine while centralizing cancellation,
// generations, retry blocking, mailbox delivery, and multi-window wakeups.
type Pager[T, Q any] struct {
	fetch  PagerFetcher[T, Q]
	query  func() Q
	loaded func() int
	apply  func(PagerPage[T]) int
	key    func(T) string
	onFail func(page int, trigger PagerTrigger, err error)

	page    int
	hasMore bool
	loading bool
	blocked bool
	err     error
	why     PagerTrigger
	gen     uint64
	cancel  context.CancelFunc
	seen    map[string]struct{}

	mu  sync.Mutex
	box []pagerResult[T]

	wakeMu sync.Mutex
	wakes  []pagerWake
	frame  uint64
}

type pagerResult[T any] struct {
	gen    uint64
	page   int
	result PagerPage[T]
	err    error
	why    PagerTrigger
}

type pagerWake struct {
	svc  ui.Services
	wake func()
	seen uint64
}

const pagerMaxWakes = 4

// NewPager constructs a pager without starting a request.
func NewPager[T, Q any](fetch PagerFetcher[T, Q], key func(T) string, query func() Q) *Pager[T, Q] {
	return &Pager[T, Q]{
		fetch: fetch, key: key, query: query,
		hasMore: true, seen: make(map[string]struct{}),
	}
}

// SetApply installs the UI-thread callback that receives only newly seen items.
func (p *Pager[T, Q]) SetApply(fn func(PagerPage[T]) int) { p.apply = fn }

// SetFailureHandler installs the UI-thread callback for a failed page.
func (p *Pager[T, Q]) SetFailureHandler(fn func(page int, trigger PagerTrigger, err error)) {
	p.onFail = fn
}

// Status returns the current shared pagination state.
func (p *Pager[T, Q]) Status() PagerStatus {
	return PagerStatus{
		Page: p.page, Loaded: p.loadedCount(), HasMore: p.hasMore,
		Loading: p.loading, Blocked: p.blocked, Err: p.err,
	}
}

func (p *Pager[T, Q]) loadedCount() int {
	return len(p.seen)
}

// PagingState adapts the pager to the common first-page and sentinel predicates.
func (p *Pager[T, Q]) PagingState() PagingState {
	return PagingState{
		Fetched: p.page > 0, Loading: p.loading,
		Blocked: p.blocked, HasMore: p.hasMore,
	}
}

// Generation returns the current query generation.
func (p *Pager[T, Q]) Generation() uint64 { return p.gen }

// Invalidate wakes every window registered with this pager.
func (p *Pager[T, Q]) Invalidate() { p.invalidate() }

// Forget removes a key after the owning view deletes its row.
func (p *Pager[T, Q]) Forget(key string) { delete(p.seen, key) }

// Seed installs already loaded items as a completed first page.
func (p *Pager[T, Q]) Seed(items []T) {
	p.stop()
	p.gen++
	p.page = 1
	p.hasMore = false
	p.err = nil
	p.blocked = false
	p.seen = make(map[string]struct{}, len(items))
	for _, item := range items {
		if p.key != nil {
			p.seen[p.key(item)] = struct{}{}
		}
	}
}

// Reset cancels the active request and clears lifecycle state without loading.
func (p *Pager[T, Q]) Reset() {
	p.stop()
	p.gen++
	p.page = 0
	p.hasMore = true
	p.err = nil
	p.blocked = false
	p.seen = make(map[string]struct{})
}

// Reload resets the pager and immediately starts page one.
func (p *Pager[T, Q]) Reload() {
	p.Reset()
	p.Load(PagerFirstRender)
}

// Retry resumes after a failed request.
func (p *Pager[T, Q]) Retry() {
	p.blocked = false
	p.Load(PagerRetry)
}

// Sync registers the current window, adopts completed results, and starts the
// first page when needed. It must run on the UI thread.
func (p *Pager[T, Q]) Sync(svc ui.Services) {
	p.setWake(svc)
	p.adopt()
	if DecideFirstPage(p.PagingState()).Action == PagingLoadFirst {
		p.Load(PagerFirstRender)
	}
}

// Load requests the next page when the shared lifecycle gates allow it.
func (p *Pager[T, Q]) Load(why PagerTrigger) {
	if p.loading || p.blocked || p.fetch == nil {
		return
	}
	if p.page > 0 && !p.hasMore {
		return
	}
	next := p.page + 1
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel, p.loading, p.err, p.why = cancel, true, nil, why
	gen := p.gen
	query := p.snapshot()
	loaded := p.loadedCount()

	go func() {
		defer cancel()
		result, err := p.fetchOne(ctx, next, loaded, query)
		p.deliver(pagerResult[T]{gen: gen, page: next, result: result, err: err, why: why})
	}()
}

func (p *Pager[T, Q]) snapshot() Q {
	if p.query == nil {
		var zero Q
		return zero
	}
	return p.query()
}

func (p *Pager[T, Q]) fetchOne(ctx context.Context, page, loaded int, query Q) (result PagerPage[T], err error) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("biu: 第 %d 页的拉页器崩了：%v\n%s", page, rec, debug.Stack())
			result, err = PagerPage[T]{}, fmt.Errorf("%w：%v", ErrPagerPanic, rec)
		}
	}()
	return p.fetch(ctx, page, loaded, query)
}

func (p *Pager[T, Q]) adopt() {
	p.mu.Lock()
	box := p.box
	p.box = nil
	p.mu.Unlock()
	for _, r := range box {
		p.applyResult(r)
	}
}

func (p *Pager[T, Q]) applyResult(r pagerResult[T]) {
	if r.gen != p.gen {
		return
	}
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.loading = false
	if r.err != nil {
		p.err = r.err
		p.blocked = true
		if p.onFail != nil {
			p.onFail(r.page, r.why, r.err)
		}
		return
	}

	p.err = nil
	p.blocked = false
	p.page = r.page
	p.hasMore = r.result.HasMore
	added := 0
	if p.apply != nil {
		fresh := make([]T, 0, len(r.result.Items))
		for _, item := range r.result.Items {
			key := ""
			if p.key != nil {
				key = p.key(item)
			}
			if _, ok := p.seen[key]; ok {
				continue
			}
			p.seen[key] = struct{}{}
			fresh = append(fresh, item)
		}
		added = p.apply(PagerPage[T]{Items: fresh, HasMore: r.result.HasMore})
	} else {
		for _, item := range r.result.Items {
			key := ""
			if p.key != nil {
				key = p.key(item)
			}
			if _, ok := p.seen[key]; ok {
				continue
			}
			p.seen[key] = struct{}{}
			added++
		}
	}
	if added == 0 {
		p.hasMore = false
	}
}

func (p *Pager[T, Q]) deliver(r pagerResult[T]) {
	p.mu.Lock()
	p.box = append(p.box, r)
	p.mu.Unlock()
	p.invalidate()
}

func (p *Pager[T, Q]) stop() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.loading = false
}

func (p *Pager[T, Q]) setWake(svc ui.Services) {
	p.wakeMu.Lock()
	defer p.wakeMu.Unlock()
	p.frame++
	for i := range p.wakes {
		if p.wakes[i].svc == svc {
			p.wakes[i].seen = p.frame
			return
		}
	}
	if len(p.wakes) >= pagerMaxWakes {
		oldest := 0
		for i := range p.wakes {
			if p.wakes[i].seen < p.wakes[oldest].seen {
				oldest = i
			}
		}
		p.wakes = append(p.wakes[:oldest], p.wakes[oldest+1:]...)
	}
	p.wakes = append(p.wakes, pagerWake{svc: svc, wake: svc.Invalidate, seen: p.frame})
}

func (p *Pager[T, Q]) invalidate() {
	p.wakeMu.Lock()
	wakes := slices.Clone(p.wakes)
	p.wakeMu.Unlock()
	for _, w := range wakes {
		if w.wake != nil {
			w.wake()
		}
	}
}
