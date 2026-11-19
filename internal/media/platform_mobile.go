//go:build ios || android

package media

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

const mobileMediaPlugin = "pudmusic_media"

type mobilePlatform struct {
	native *mygo.NativePlugin
	fetch  func(string) ([]byte, error)

	mu      sync.Mutex
	ready   bool
	dirty   bool
	working bool
	latest  NowPlaying
	artwork []byte
	off     func()
}

type mobileNowPlaying struct {
	State      string  `json:"state"`
	Title      string  `json:"title"`
	Artist     string  `json:"artist"`
	Duration   float64 `json:"duration"`
	Position   float64 `json:"position"`
	ArtworkURL string  `json:"artworkURL"`
	Artwork    []byte  `json:"artwork,omitempty"`
}

func newPlatform(opts Options) (platform, error) {
	fetch := opts.Fetch
	if fetch == nil {
		fetch = defaultFetch
	}
	p := &mobilePlatform{native: mygo.Native(mobileMediaPlugin), fetch: fetch}
	mygo.App.WhenReady(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.ready = true
		p.drainLocked()
	})
	return p, nil
}

func (p *mobilePlatform) apply(n NowPlaying) {
	if n.State == StateNone {
		n = NowPlaying{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if n.ArtworkURL != p.latest.ArtworkURL {
		p.artwork = nil
		if n.ArtworkURL != "" {
			go p.fetchArtwork(n.ArtworkURL)
		}
	}
	p.latest = n
	p.dirty = true
	p.drainLocked()
}

func (p *mobilePlatform) fetchArtwork(url string) {
	artwork, err := p.fetch(url)
	if err != nil || len(artwork) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if url != p.latest.ArtworkURL {
		return
	}
	p.artwork = artwork
	p.dirty = true
	p.drainLocked()
}

func (p *mobilePlatform) drainLocked() {
	if p.ready && p.dirty && !p.working {
		p.working = true
		go p.publish()
	}
}

func (p *mobilePlatform) publish() {
	for {
		p.mu.Lock()
		if !p.dirty {
			p.working = false
			p.mu.Unlock()
			return
		}
		n := p.latest
		info := mobileNowPlaying{
			State: n.State.String(), Title: n.Title, Artist: n.Artist,
			Duration: n.Duration.Seconds(), Position: n.Position.Seconds(),
			ArtworkURL: n.ArtworkURL, Artwork: p.artwork,
		}
		p.dirty = false
		p.artwork = nil
		p.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := p.native.Call(ctx, "publish", info, nil)
		cancel()
		if err != nil {
			log.Printf("pudmusic: 更新移动端系统媒体会话失败：%v", err)
		}
	}
}

func (p *mobilePlatform) attach(fn func(Command, time.Duration)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.off != nil {
		p.off()
	}
	p.off = p.native.On("command", func(data []byte) {
		var event struct {
			Command  string
			Position float64
		}
		if json.Unmarshal(data, &event) != nil {
			return
		}
		for _, cmd := range [...]Command{CommandPlay, CommandPause, CommandToggle, CommandNext, CommandPrev, CommandStop, CommandSeek} {
			if cmd.String() == event.Command {
				fn(cmd, time.Duration(event.Position*float64(time.Second)))
				return
			}
		}
	})
}

func (p *mobilePlatform) detach() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.off != nil {
		p.off()
		p.off = nil
	}
}
