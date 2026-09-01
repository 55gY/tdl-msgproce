package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractTwitterLinks(t *testing.T) {
	text := "#tag https://x.com/user/status/123，https://twitter.com/user/status/456 https://x.com/user/status/123"
	got := extractTwitterLinks(text)
	if len(got) != 2 || got[0] != "https://x.com/user/status/123" || got[1] != "https://twitter.com/user/status/456" {
		t.Fatalf("unexpected links: %#v", got)
	}
}

func TestExtractHashtagCaption(t *testing.T) {
	got := extractHashtagCaption("正文 #one #two https://x.com/user/status/123")
	if got != "#one #two" {
		t.Fatalf("unexpected caption: %q", got)
	}
}

func TestParseTwitterMedia(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{"tweetResult": map[string]any{"result": map[string]any{
			"legacy": map[string]any{"extended_entities": map[string]any{"media": []any{
				map[string]any{"type": "photo", "media_url_https": "https://pbs.twimg.com/media/a.jpg"},
				map[string]any{"type": "video", "video_info": map[string]any{"variants": []any{
					map[string]any{"content_type": "video/mp4", "bitrate": float64(128000), "url": "https://video.twimg.com/a-low.mp4"},
					map[string]any{"content_type": "video/mp4", "bitrate": float64(512000), "url": "https://video.twimg.com/a-high.mp4"},
				}}},
			}}},
		}}},
	}
	media, err := parseTwitterMedia(payload)
	if err != nil || len(media) != 2 {
		t.Fatalf("parse failed: %v %#v", err, media)
	}
	if !strings.Contains(media[0].URL, "name=orig") || media[1].URL != "https://video.twimg.com/a-high.mp4" {
		t.Fatalf("unexpected media: %#v", media)
	}
}

func TestProbeTwitterMediaSizeRequiresContentLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Write([]byte("must not be fetched"))
	}))
	defer server.Close()
	_, err := probeTwitterMediaSize(context.Background(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "未返回可靠的 Content-Length") {
		t.Fatalf("expected strict content-length error, got %v", err)
	}
}

func TestProbeTwitterMediaSize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "42")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	size, err := probeTwitterMediaSize(context.Background(), server.URL)
	if err != nil || size != 42 {
		t.Fatalf("unexpected result: size=%d err=%v", size, err)
	}
}

func TestUniqueBotLinks(t *testing.T) {
	got := uniqueBotLinks([]string{
		"https://x.com/user/status/123",
		"https://sub.example/a",
		"https://x.com/user/status/123",
	})
	if len(got) != 2 || got[0] != "https://x.com/user/status/123" || got[1] != "https://sub.example/a" {
		t.Fatalf("unexpected unique links: %#v", got)
	}
}

func TestIsTwitterTombstone(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{
			"tweetResult": map[string]any{
				"result": map[string]any{"__typename": "TweetTombstone"},
			},
		},
	}
	if !isTwitterTombstone(payload) {
		t.Fatal("expected tombstone payload")
	}
}
