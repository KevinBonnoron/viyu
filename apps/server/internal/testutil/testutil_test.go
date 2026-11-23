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

func TestNewAppDatabasesAreIndependent(t *testing.T) {
	t.Parallel()
	first := NewApp(t)
	second := NewApp(t)
	for _, db := range []struct {
		name   string
		create func() error
		exists func() bool
	}{
		{
			name: "data",
			create: func() error {
				_, err := first.DB().NewQuery("CREATE TABLE fixture_only (id INTEGER)").Execute()
				return err
			},
			exists: func() bool { return second.HasTable("fixture_only") },
		},
		{
			name: "auxiliary",
			create: func() error {
				_, err := first.AuxDB().NewQuery("CREATE TABLE fixture_only (id INTEGER)").Execute()
				return err
			},
			exists: func() bool { return second.AuxHasTable("fixture_only") },
		},
	} {
		if err := db.create(); err != nil {
			t.Fatalf("%s database: %v", db.name, err)
		}
		if db.exists() {
			t.Fatalf("%s database was shared between fixtures", db.name)
		}
	}
}
