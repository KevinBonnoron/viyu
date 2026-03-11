package series

import (
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

func TestOCRSettingIsStoredAndReportsTransitions(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	if enabled, err := OCREnabled(f.app); err != nil || !enabled {
		t.Fatalf("the shipped default is enabled: %v %v", enabled, err)
	}
	for _, step := range []struct {
		set, changed bool
	}{{true, false}, {false, true}, {false, false}, {true, true}} {
		changed, err := SetOCREnabled(f.app, step.set)
		if err != nil || changed != step.changed {
			t.Fatalf("set %v: changed=%v err=%v, want changed=%v", step.set, changed, err, step.changed)
		}
		if enabled, err := OCREnabled(f.app); err != nil || enabled != step.set {
			t.Fatalf("setting not persisted: %v %v", enabled, err)
		}
	}
}

func TestOCRSettingJoinsTheCallersTransaction(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	err := f.app.RunInTransaction(func(tx core.App) error {
		if changed, err := SetOCREnabled(tx, false); err != nil || !changed {
			t.Errorf("changed=%v err=%v", changed, err)
		}
		return errRollback
	})
	if err != errRollback {
		t.Fatal(err)
	}
	if enabled, err := OCREnabled(f.app); err != nil || !enabled {
		t.Fatalf("a rolled back transaction changed the setting: %v %v", enabled, err)
	}
}

type rollback string

func (r rollback) Error() string { return string(r) }

const errRollback = rollback("rollback")
