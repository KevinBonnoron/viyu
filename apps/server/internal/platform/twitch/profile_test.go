package twitch

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform"
)

func profilePlatform(t *testing.T, body string) *Platform {
	t.Helper()
	return graphQLPlatform(t, func(w http.ResponseWriter, r *http.Request) {
		payload := readBody(t, r)
		if r.Header.Get("Client-Id") != publicClientID || r.Header.Get("Authorization") != "" || !strings.Contains(string(payload), `"login":"hardos31"`) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, body)
	}, Options{})
}

func TestChannelProfileSuppliesWhatTheVODListingOmits(t *testing.T) {
	t.Parallel()
	p := profilePlatform(t, `{"data":{"user":{"login":"hardos31","displayName":"Hardos31","description":"Streams","profileImageURL":"https://static-cdn.example/hardos31-150x150.png","followers":{"totalCount":1234}}}}`)
	profile, err := p.channelProfile(context.Background(), "hardos31")
	if err != nil || profile.ID != "hardos31" || profile.Name != "Hardos31" || profile.Thumbnail != "https://static-cdn.example/hardos31-150x150.png" || profile.SubscriberCount != 1234 || profile.Platform != ID {
		t.Fatalf("profile: %+v %v", profile, err)
	}

	listing := platform.Channel{ID: "hardos31", Platform: ID, Name: "hardos31"}
	p.completeChannel(context.Background(), &listing, 0)
	if listing.Thumbnail != profile.Thumbnail || listing.Name != "Hardos31" || listing.Description != "Streams" || listing.SubscriberCount != 1234 {
		t.Fatalf("listing was not completed: %+v", listing)
	}

	// Values the listing already has are kept.
	known := platform.Channel{ID: "hardos31", Platform: ID, Name: "Chosen name", Thumbnail: "https://static-cdn.example/known.png", SubscriberCount: 5}
	p.completeChannel(context.Background(), &known, 0)
	if known.Name != "Chosen name" || known.Thumbnail != "https://static-cdn.example/known.png" || known.SubscriberCount != 5 {
		t.Fatalf("listing values were overwritten: %+v", known)
	}
}

func TestChannelProfileFailuresLeaveTheListingUntouched(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"unknown channel": `{"data":{"user":null}}`,
		"graphql error":   `{"errors":[{"message":"upstream"}],"data":null}`,
		"no data":         `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := profilePlatform(t, body)
			if _, err := p.channelProfile(context.Background(), "hardos31"); err == nil {
				t.Fatal("failure was reported as a profile")
			}
			listing := platform.Channel{ID: "hardos31", Platform: ID, Name: "hardos31"}
			p.completeChannel(context.Background(), &listing, 0)
			if listing != (platform.Channel{ID: "hardos31", Platform: ID, Name: "hardos31"}) {
				t.Fatalf("failed lookup changed the listing: %+v", listing)
			}
		})
	}
}

// A flat yt-dlp listing names the VODs but has no dates, durations or views. They come from the same
// GraphQL request as the profile, and what the listing already knows is kept.
func TestChannelVODsAreCompletedFromTheProfileRequest(t *testing.T) {
	t.Parallel()
	p := profilePlatform(t, `{"data":{"user":{"login":"hardos31","displayName":"Hardos31","videos":{"edges":[{"node":{"id":"2001","title":"Stream","createdAt":"2026-11-06T12:30:00Z","lengthSeconds":3600,"viewCount":42,"previewThumbnailURL":"https://static-cdn.example/2001.jpg"}},{"node":null}]}}}}`)
	listing := platform.Channel{ID: "hardos31", Platform: ID}
	vods := p.completeChannel(context.Background(), &listing, 2)
	if len(vods) != 1 {
		t.Fatalf("vods: %+v", vods)
	}

	video := entry{ID: "v2001", Title: "Stream"}.video()
	if video.ID != "2001" || video.Platform != ID || !video.OriginalTitleVerified || video.OriginalTitle != "Stream" {
		t.Fatalf("a Twitch VOD has one title, so it is its own original: %+v", video)
	}
	vods["2001"].fill(&video)
	if video.Published != "2026-11-06T12:30:00Z" || video.Duration != 3600 || video.ViewCount != 42 || video.Thumbnail != "https://static-cdn.example/2001.jpg" {
		t.Fatalf("listing was not completed: %+v", video)
	}

	known := platform.Video{Published: "2025-01-01", Duration: 10, ViewCount: 1, Thumbnail: "https://static-cdn.example/known.jpg", Title: "Known"}
	vods["2001"].fill(&known)
	if known.Published != "2025-01-01" || known.Duration != 10 || known.ViewCount != 1 || known.Thumbnail != "https://static-cdn.example/known.jpg" || known.Title != "Known" {
		t.Fatalf("known values were overwritten: %+v", known)
	}
}

func TestChannelThumbnailPrefersTheExplicitAvatar(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		entry entry
		want  string
	}{
		"explicit avatar": {entry{ChannelThumbnail: "https://static-cdn.example/avatar.png", Thumbnail: "https://static-cdn.example/other.png"}, "https://static-cdn.example/avatar.png"},
		"none":            {entry{}, ""},
	} {
		if got := tc.entry.channelThumbnail(); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}
