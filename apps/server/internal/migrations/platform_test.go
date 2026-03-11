package migrations

import (
	"slices"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/platform/all"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform/twitch"
	"github.com/KevinBonnoron/viyu/apps/server/internal/platform/youtube"
	"github.com/KevinBonnoron/viyu/apps/server/internal/ytdlp"
)

// The schema's platform selects and the platform registry list the same platforms, so adding a platform to
// one without the other fails here: the registry knows a platform the schema would refuse to store, or the
// schema accepts one nothing implements.
func TestPlatformSelectsMatchTheRegistry(t *testing.T) {
	app := freshApp(t)
	runner := ytdlp.New(ytdlp.Options{})
	registry := all.Registry(all.Options{YouTube: youtube.Options{Runner: runner}, Twitch: twitch.Options{Runner: runner}}).IDs()
	for _, name := range []string{"channels", "videos", "source_connections", "source_activations", "channel_catalogs"} {
		field, ok := collection(t, app, name).Fields.GetByName("platform").(*core.SelectField)
		if !ok || field.MaxSelect != 1 || !field.Required {
			t.Errorf("%s.platform must be a required single select: %#v", name, field)
			continue
		}
		for _, id := range registry {
			if !slices.Contains(field.Values, id) {
				t.Errorf("%s.platform does not accept the registered platform %q: %v", name, id, field.Values)
			}
		}
		for _, value := range field.Values {
			if !slices.Contains(registry, value) {
				t.Errorf("%s.platform accepts %q, which no registered platform has: %v", name, value, registry)
			}
		}
	}
}
