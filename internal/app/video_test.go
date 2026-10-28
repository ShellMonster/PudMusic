package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

func rangeServer(t *testing.T, body string, hits *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		parts := strings.SplitN(strings.TrimPrefix(r.Header.Get("Range"), "bytes="), "-", 2)
		from, _ := strconv.Atoi(parts[0])
		to, _ := strconv.Atoi(parts[1])
		to = min(to+1, len(body))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte(body[from:to]))
	}))
}

func TestVideoFetchFallsBackToTheBackupAddress(t *testing.T) {
	var bad, good int
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { bad++; w.WriteHeader(403) }))
	defer dead.Close()
	ok := rangeServer(t, "0123456789", &good)
	defer ok.Close()
	v := &videoSource{media: http.DefaultClient, ref: &bili.StreamRef{URLs: []string{dead.URL, ok.URL}}}
	b, err := v.fetch(context.Background(), 2, 6)
	if err != nil || string(b) != "2345" {
		t.Fatalf("got %q err=%v", b, err)
	}
	if bad != 1 {
		t.Fatalf("dead address tried %d times", bad)
	}
}

func TestVideoFetchRefreshesTheStreamWhenEveryAddressFails(t *testing.T) {
	var bad, good, refreshed int
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { bad++; w.WriteHeader(403) }))
	defer dead.Close()
	ok := rangeServer(t, "0123456789", &good)
	defer ok.Close()
	v := &videoSource{
		media: http.DefaultClient,
		ref:   &bili.StreamRef{URLs: []string{dead.URL}},
		refresh: func(context.Context) (*bili.StreamRef, error) {
			refreshed++
			return &bili.StreamRef{URLs: []string{ok.URL}}, nil
		},
	}
	b, err := v.fetch(context.Background(), 0, 4)
	if err != nil || string(b) != "0123" || refreshed != 1 {
		t.Fatalf("got %q err=%v refreshed=%d", b, err, refreshed)
	}
	// 换过之后不再回头试旧地址。
	if _, err := v.fetch(context.Background(), 4, 8); err != nil || bad != 1 {
		t.Fatalf("err=%v bad=%d", err, bad)
	}
}

func TestVideoStopsWhenTheTrackChanges(t *testing.T) {
	a := newAnnounceApp(t)
	a.vid.phase, a.vid.key = VideoOn, "online|BV1xx|111"
	if got := a.VideoPhase(); got != VideoOff {
		t.Fatalf("no current item but phase=%v", got)
	}
}
