package app

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// acctJSON encodes a request body.
func acctJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// acctDecode decodes the JSON answer of a request into target.
func acctDecode(t *testing.T, res *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(res.Body.Bytes(), target); err != nil {
		t.Fatalf("answer is not the expected JSON: %v: %s", err, res.Body)
	}
}

// acctExpect fails unless the answer has the status and, for an error, the code of the one error envelope
// ({"code","message"}: nothing else is an error shape of the API).
func acctExpect(t *testing.T, res *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if res.Code != status {
		t.Fatalf("status=%d, want %d: %s", res.Code, status, res.Body)
	}
	if code == "" {
		return
	}
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("error answer is not JSON: %v: %s", err, res.Body)
	}
	if body["code"] != code {
		t.Fatalf("error code=%v, want %q: %s", body["code"], code, res.Body)
	}
	if _, legacy := body["error"]; legacy {
		t.Fatalf("answer still carries the {\"error\"} shape: %s", res.Body)
	}
	if message, _ := body["message"].(string); message == "" {
		t.Fatalf("error without a message: %s", res.Body)
	}
}
