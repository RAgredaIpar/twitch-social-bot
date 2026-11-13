package twitch_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/twitch-social-bot/internal/twitch"
)

func TestHelixClient_GetChannelInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/channels" || r.URL.Query().Get("broadcaster_id") != "12345" {
			t.Fatalf("unexpected request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"broadcaster_id":"12345","game_name":"Minecraft","title":"Construyendo la base"}]}`))
	}))
	defer srv.Close()

	client := twitch.NewHelixClient(twitch.HelixConfig{
		ClientID: "cid",
		BaseURL:  srv.URL,
	})
	info, err := client.GetChannelInfo(context.Background(), "12345")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.GameName != "Minecraft" || info.Title != "Construyendo la base" {
		t.Fatalf("unexpected channel info: %+v", info)
	}
}
