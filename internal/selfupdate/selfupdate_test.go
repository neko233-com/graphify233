package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDownloadAssetFallsBackToAPIAsset(t *testing.T) {
	var browserHits, apiHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/cdn/"):
			browserHits++
			http.Error(w, "bad gateway", http.StatusBadGateway)
		case strings.HasPrefix(r.URL.Path, "/api/repos/o/r/releases/assets/7"):
			apiHits++
			if got := r.Header.Get("Accept"); got != "application/octet-stream" {
				t.Errorf("Accept = %q, want application/octet-stream", got)
			}
			_, _ = w.Write([]byte("signature-bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oldAPI, oldRepo, oldMirror := APIBase, Repo, Mirror
	APIBase, Repo, Mirror = srv.URL+"/api", "o/r", ""
	defer func() { APIBase, Repo, Mirror = oldAPI, oldRepo, oldMirror }()

	rel := &release{TagName: "v9", Assets: []asset{
		{ID: 7, Name: "checksums.txt.sig", URL: srv.URL + "/cdn/checksums.txt.sig"},
	}}
	got, err := downloadAsset(context.Background(), rel, "checksums.txt.sig", 1<<10)
	if err != nil {
		t.Fatalf("downloadAsset: %v", err)
	}
	if string(got) != "signature-bytes" {
		t.Fatalf("body = %q", got)
	}
	if browserHits != 1 || apiHits != 1 {
		t.Fatalf("browserHits=%d apiHits=%d, want 1/1", browserHits, apiHits)
	}
}

func TestDownloadAssetMissing(t *testing.T) {
	rel := &release{TagName: "v9"}
	if _, err := downloadAsset(context.Background(), rel, "nope", 10); err == nil {
		t.Fatal("expected an error for a missing asset")
	}
}

func TestVerifySignature(t *testing.T) {
	// Generate a key pair at test time; verification must reject a bad signature
	// and accept a good one over the exact checksum bytes.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b64 := base64.StdEncoding.EncodeToString
	data := []byte("abc  file\n")
	sig := ed25519.Sign(priv, data)
	if err := VerifySignature(data, b64(sig), b64(pub)); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := VerifySignature([]byte("tampered"), b64(sig), b64(pub)); err == nil {
		t.Fatal("tampered data accepted")
	}
	if err := VerifySignature(data, "not base64", b64(pub)); err == nil {
		t.Fatal("garbage signature accepted")
	}
}
