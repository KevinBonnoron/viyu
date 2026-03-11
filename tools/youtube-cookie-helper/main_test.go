package main

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/browserutils/kooky"
)

func TestExportCookiesScopesAndFormat(t *testing.T) {
	now := time.Unix(1700000000, 0)
	cookie := func(domain, name, value string) *kooky.Cookie {
		return &kooky.Cookie{Cookie: http.Cookie{Domain: domain, Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true}}
	}
	partitioned := cookie(".youtube.com", "SID", "must-not-export")
	partitioned.Partitioned = true
	jar, err := exportCookies([]*kooky.Cookie{cookie(".youtube.com", "SAPISID", "synthetic-auth"), cookie(".youtube.com", "LOGIN_INFO", "synthetic-login"), cookie(".notyoutube.com", "SID", "must-not-export"), cookie(".google.com", "SID", "must-not-export"), partitioned}, now)
	if err != nil {
		t.Fatal(err)
	}
	text := string(jar)
	if strings.Contains(text, "must-not-export") || !strings.Contains(text, "#HttpOnly_.youtube.com\tTRUE\t/\tTRUE\t0\tSAPISID\tsynthetic-auth\n") {
		t.Fatalf("unexpected exported cookie format: %q", text)
	}
}

func TestExportCookiesRejectsUnsafeOrIncompleteSession(t *testing.T) {
	now := time.Unix(1700000000, 0)
	auth := &kooky.Cookie{Cookie: http.Cookie{Domain: ".youtube.com", Path: "/", Name: "SAPISID", Value: "synthetic"}}
	login := &kooky.Cookie{Cookie: http.Cookie{Domain: ".youtube.com", Path: "/", Name: "LOGIN_INFO", Value: "synthetic"}}
	if _, err := exportCookies([]*kooky.Cookie{auth}, now); err == nil {
		t.Fatal("accepted incomplete session")
	}
	login.Value = "bad\tvalue"
	if _, err := exportCookies([]*kooky.Cookie{auth, login}, now); err == nil {
		t.Fatal("accepted invalid field")
	}
	login.Value = "synthetic"
	login.Container = "work"
	if _, err := exportCookies([]*kooky.Cookie{auth, login}, now); err == nil {
		t.Fatal("accepted Firefox container cookie")
	}
}
