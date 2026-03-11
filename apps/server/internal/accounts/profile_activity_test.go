package accounts

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

func TestProfileActivityAPIRejectsForeignOwnershipAndKeepsProfilesIndependent(t *testing.T) {
	t.Parallel()
	for _, collection := range []string{"video_reactions", "watch_history"} {
		t.Run(collection, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			handler := e.handler(t)
			owner, token := e.user(t, "activity-owner@example.test", "")
			other, otherToken := e.user(t, "activity-other@example.test", "")
			first := e.firstProfile(t, owner.Id)
			second := e.profile(t, owner, "Second")
			foreign := e.firstProfile(t, other.Id)
			base := "/api/collections/" + collection + "/records"
			video := e.video(t, "shared-video")
			payload := func(profileID string) map[string]any {
				if collection == "video_reactions" {
					return map[string]any{"profile": profileID, "video": video.Id, "reaction": "like"}
				}
				return map[string]any{"profile": profileID, "video": video.Id, "watched_at": "2026-01-01 00:00:00.000Z", "position": 45, "duration_watched": 30}
			}
			for _, invalid := range []struct{ name, profile string }{{"missing profile", ""}, {"foreign profile", foreign.Id}} {
				response := send(t, handler, http.MethodPost, base, token, payload(invalid.profile))
				if response.Code != http.StatusBadRequest {
					t.Fatalf("%s assignment accepted: %d %s", invalid.name, response.Code, response.Body)
				}
			}
			ids := make([]string, 0, 2)
			for _, profile := range []*core.Record{first, second} {
				response := send(t, handler, http.MethodPost, base, token, payload(profile.Id))
				var created struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
					t.Fatal(err)
				}
				if response.Code != http.StatusOK || created.ID == "" {
					t.Fatalf("owned profile could not independently save activity: %d %s", response.Code, response.Body)
				}
				ids = append(ids, created.ID)
			}
			path := base + "/" + ids[0]
			update := map[string]any{"reaction": "dislike", "position": 90}
			for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
				if response := send(t, handler, method, path, otherToken, update); response.Code != http.StatusNotFound {
					t.Fatalf("foreign account could %s private activity: %d %s", method, response.Code, response.Body)
				}
			}
			if response := send(t, handler, http.MethodPatch, path, token, update); response.Code != http.StatusOK {
				t.Fatalf("updating existing profile activity failed: %d %s", response.Code, response.Body)
			}
			for i, id := range ids {
				record, err := e.app.FindRecordById(collection, id)
				if err != nil {
					t.Fatal(err)
				}
				if record.GetString("profile") != []*core.Record{first, second}[i].Id {
					t.Fatalf("profile activity ownership changed: %+v", record)
				}
				if collection == "video_reactions" && record.GetString("reaction") != []string{"dislike", "like"}[i] {
					t.Fatalf("one profile reaction overwrote another: %+v", record)
				}
				if collection == "watch_history" && record.GetFloat("position") != []float64{90, 45}[i] {
					t.Fatalf("one profile resume position overwrote another: %+v", record)
				}
			}
			for _, profile := range []*core.Record{first, second} {
				response := send(t, handler, http.MethodGet, base+"?filter="+url.QueryEscape("profile = '"+profile.Id+"'"), token, nil)
				var listing struct {
					Items []struct {
						Profile string `json:"profile"`
					} `json:"items"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &listing); err != nil {
					t.Fatal(err)
				}
				if response.Code != http.StatusOK || len(listing.Items) != 1 || listing.Items[0].Profile != profile.Id {
					t.Fatalf("profile-filtered activity listing mixed profiles: %d %s", response.Code, response.Body)
				}
			}
			response := send(t, handler, http.MethodGet, base, otherToken, nil)
			var listing struct {
				TotalItems int `json:"totalItems"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &listing); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK || listing.TotalItems != 0 {
				t.Fatalf("foreign account listed private activity: %d %s", response.Code, response.Body)
			}
		})
	}
}

func TestDeletingProfileCascadesOnlyItsActivity(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	handler := e.handler(t)
	owner, token := e.user(t, "cascade-activity@example.test", "")
	other, otherToken := e.user(t, "cascade-other@example.test", "")
	first := e.firstProfile(t, owner.Id)
	second := e.profile(t, owner, "Keep")
	foreign := e.firstProfile(t, other.Id)
	settings := e.record(t, "settings", map[string]any{"user": owner.Id, "hide_shorts": true})
	records := make(map[string][]*core.Record)
	video := e.video(t, "shared-video")
	for _, collection := range []string{"video_reactions", "watch_history"} {
		for _, profile := range []*core.Record{first, second, foreign} {
			values := map[string]any{"profile": profile.Id, "video": video.Id, "watched_at": "2026-01-01 00:00:00.000Z", "position": 75}
			if collection == "video_reactions" {
				values = map[string]any{"profile": profile.Id, "video": video.Id, "reaction": "dislike"}
			}
			records[collection] = append(records[collection], e.record(t, collection, values))
		}
	}
	profilePath := "/api/collections/profiles/records/" + first.Id
	if response := send(t, handler, http.MethodDelete, profilePath, otherToken, nil); response.Code != http.StatusNotFound {
		t.Fatalf("foreign account could delete a profile: %d %s", response.Code, response.Body)
	}
	if response := send(t, handler, http.MethodDelete, profilePath, token, nil); response.Code != http.StatusNoContent {
		t.Fatalf("deleting owned profile failed: %d %s", response.Code, response.Body)
	}
	for collection, activity := range records {
		if _, err := e.app.FindRecordById(collection, activity[0].Id); err == nil {
			t.Fatalf("deleting a profile left its %s activity", collection)
		}
		for _, expected := range activity[1:] {
			record, err := e.app.FindRecordById(collection, expected.Id)
			if err != nil || record.GetString("profile") != expected.GetString("profile") || (collection == "video_reactions" && record.GetString("reaction") != expected.GetString("reaction")) || (collection == "watch_history" && record.GetFloat("position") != expected.GetFloat("position")) {
				t.Fatalf("deleting one profile changed another profile's %s activity: %+v %v", collection, record, err)
			}
		}
	}
	unchanged, err := e.app.FindRecordById("settings", settings.Id)
	if err != nil || !unchanged.GetBool("hide_shorts") || unchanged.GetString("user") != owner.Id {
		t.Fatalf("profile deletion changed account-wide settings: %+v %v", unchanged, err)
	}
	for _, profile := range []*core.Record{second, foreign} {
		if _, err := e.app.FindRecordById("profiles", profile.Id); err != nil {
			t.Fatalf("profile deletion removed another profile: %v", err)
		}
	}
}
