package testutil

import "testing"

func TestNewAppHasTheRealSchema(t *testing.T) {
	app := NewApp(t)
	for _, name := range []string{"videos", "channels", "subscriptions", "watch_history", "series", "profiles"} {
		if _, err := app.FindCollectionByNameOrId(name); err != nil {
			t.Fatalf("collection %s: %v", name, err)
		}
	}
}
