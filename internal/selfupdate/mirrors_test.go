package selfupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRankSourcesOrdersByLatency(t *testing.T) {
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("x"))
	}))
	defer fast.Close()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(60 * time.Millisecond)
		_, _ = w.Write([]byte("x"))
	}))
	defer slow.Close()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusBadGateway)
	}))
	defer down.Close()

	srcs := []source{
		{name: "down", base: down.URL + "/?u="},
		{name: "slow", base: slow.URL + "/?u="},
		{name: "fast", base: fast.URL + "/?u="},
	}
	ranked := rankSources(context.Background(), srcs, "https://github.com/o/r/releases/download/v1/checksums.txt")
	if len(ranked) != 3 {
		t.Fatalf("ranked %d sources, want 3", len(ranked))
	}
	if ranked[0].name != "fast" || ranked[1].name != "slow" || ranked[2].name != "down" {
		t.Fatalf("order = %s, %s, %s; want fast, slow, down", ranked[0].name, ranked[1].name, ranked[2].name)
	}
	if ranked[2].delay != -1 {
		t.Fatalf("unreachable source delay = %v, want -1", ranked[2].delay)
	}
}

func TestFetchViaFallsBackToReachableSource(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer good.Close()
	srcs := []source{
		{name: "bad", base: bad.URL + "/?u="},
		{name: "good", base: good.URL + "/?u="},
	}
	data, src, err := fetchVia(context.Background(), srcs, "https://github.com/o/r/x", 100)
	if err != nil {
		t.Fatalf("fetchVia: %v", err)
	}
	if string(data) != "payload" || src != "good" {
		t.Fatalf("data = %q src = %q", data, src)
	}
}

func TestApplicableSkipsMirrorsForLocalURLs(t *testing.T) {
	srcs := applicable("http://127.0.0.1:1/file")
	if len(srcs) != 1 || srcs[0].base != "" || srcs[0].name != "direct" {
		t.Fatalf("local URL got sources: %+v", srcs)
	}
	github := applicable("https://github.com/o/r/releases/download/v1/x")
	if len(github) < 2 {
		t.Fatalf("github URL got %d sources, want direct + mirrors", len(github))
	}
}

func TestSourceURLJoinsPrefix(t *testing.T) {
	s := source{name: "m", base: "https://gh-proxy.com/"}
	want := "https://gh-proxy.com/https://github.com/o/r/x"
	if got := s.url("https://github.com/o/r/x"); got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
	if got := (source{name: "direct"}).url("https://github.com/o/r/x"); got != "https://github.com/o/r/x" {
		t.Fatalf("direct url = %q", got)
	}
}
