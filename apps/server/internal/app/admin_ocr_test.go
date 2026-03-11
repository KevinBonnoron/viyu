package app

import (
	"net/http"
	"testing"

	"github.com/KevinBonnoron/viyu/apps/server/internal/series"
	"github.com/KevinBonnoron/viyu/apps/server/internal/series/ocr"
)

// The administrator's switch for thumbnail recognition: validation, persistence and what the build can do.
// (Who may use the routes is covered with the other administration routes; what the switch does to the series
// is covered in series_ocr_test.go.)
func TestAdminSeriesOCRSwitchWithAnEngine(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t, func(o *Options) { o.OCR = &serReader{} })
	// A request from anyone but an administrator changes nothing.
	_, ordinary := admUser(t, app, "ocr-user@example.test", "user")
	acctExpect(t, send(t, handler, http.MethodPatch, "/api/admin/series-ocr", ordinary, `{"enabled":false}`), http.StatusForbidden, "ADMIN_REQUIRED")
	if enabled, err := series.OCREnabled(app); err != nil || !enabled {
		t.Fatalf("a refused request changed the setting: %v %v", enabled, err)
	}
	_, admin := admUser(t, app, "ocr-admin@example.test", "admin")

	for _, body := range []string{`{}`, `{"enabled":"yes"}`, `{"enabled":true,"other":1}`, `not-json`} {
		acctExpect(t, send(t, handler, http.MethodPatch, "/api/admin/series-ocr", admin, body), http.StatusBadRequest, "ADMIN_VALIDATION")
	}
	res := request(t, handler, "/api/admin/series-ocr", admin)
	if res.Code != http.StatusOK || res.Body.String() != `{"enabled":true,"available":true}` || res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("initial setting: %d %s", res.Code, res.Body)
	}
	if res := send(t, handler, http.MethodPatch, "/api/admin/series-ocr", admin, `{"enabled":false}`); res.Code != http.StatusOK || res.Body.String() != `{"enabled":false,"available":true}` {
		t.Fatalf("disable: %d %s", res.Code, res.Body)
	}
	if enabled, err := series.OCREnabled(app); err != nil || enabled {
		t.Fatalf("the setting was not persisted: %v %v", enabled, err)
	}
	if res := request(t, handler, "/api/admin/series-ocr", admin); res.Body.String() != `{"enabled":false,"available":true}` {
		t.Fatalf("setting read after the change: %s", res.Body)
	}
	if res := send(t, handler, http.MethodPatch, "/api/admin/series-ocr", admin, `{"enabled":true}`); res.Code != http.StatusOK || res.Body.String() != `{"enabled":true,"available":true}` {
		t.Fatalf("enable: %d %s", res.Code, res.Body)
	}
	if enabled, err := series.OCREnabled(app); err != nil || !enabled {
		t.Fatalf("the setting was not persisted: %v %v", enabled, err)
	}
}

// A build without the recognition engine can read the setting and turn it off, never on. A build with the
// engine (-tags ocr) always has one: the test then checks it reports so.
func TestAdminSeriesOCRSwitchReportsWhatTheBuildCanDo(t *testing.T) {
	t.Parallel()
	app, handler := newApp(t)
	_, admin := admUser(t, app, "ocr-build-admin@example.test", "admin")

	if ocr.Available() {
		if res := request(t, handler, "/api/admin/series-ocr", admin); res.Body.String() != `{"enabled":true,"available":true}` {
			t.Fatalf("a build with the engine reports: %s", res.Body)
		}
		return
	}
	if res := request(t, handler, "/api/admin/series-ocr", admin); res.Code != http.StatusOK || res.Body.String() != `{"enabled":true,"available":false}` {
		t.Fatalf("a build without the engine reports: %d %s", res.Code, res.Body)
	}
	acctExpect(t, send(t, handler, http.MethodPatch, "/api/admin/series-ocr", admin, `{"enabled":true}`), http.StatusConflict, "OCR_UNAVAILABLE")
	if res := send(t, handler, http.MethodPatch, "/api/admin/series-ocr", admin, `{"enabled":false}`); res.Code != http.StatusOK || res.Body.String() != `{"enabled":false,"available":false}` {
		t.Fatalf("disable: %d %s", res.Code, res.Body)
	}
	acctExpect(t, send(t, handler, http.MethodPatch, "/api/admin/series-ocr", admin, `{"enabled":true}`), http.StatusConflict, "OCR_UNAVAILABLE")
	if enabled, err := series.OCREnabled(app); err != nil || enabled {
		t.Fatalf("a refused enable changed the setting: %v %v", enabled, err)
	}
}
