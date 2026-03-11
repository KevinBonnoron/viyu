package jobs

import (
	"slices"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"

	"github.com/KevinBonnoron/viyu/apps/server/internal/testutil"
)

// channel_catalogs.status is a select of the job statuses: the typed constants and the schema list the same
// values, so a status added to one without the other fails here.
func TestCatalogStatusSelectMatchesTheStatusConstants(t *testing.T) {
	t.Parallel()
	collection, err := testutil.NewApp(t).FindCollectionByNameOrId("channel_catalogs")
	if err != nil {
		t.Fatal(err)
	}
	field, ok := collection.Fields.GetByName("status").(*core.SelectField)
	if !ok || field.MaxSelect != 1 || !field.Required {
		t.Fatalf("channel_catalogs.status must be a required single select: %#v", field)
	}
	statuses := []Status{Pending, Running, Complete, Failed}
	for _, status := range statuses {
		if !slices.Contains(field.Values, string(status)) {
			t.Errorf("channel_catalogs.status does not accept %q: %v", status, field.Values)
		}
	}
	if len(field.Values) != len(statuses) {
		t.Errorf("channel_catalogs.status accepts %v, the constants are %v", field.Values, statuses)
	}
}
