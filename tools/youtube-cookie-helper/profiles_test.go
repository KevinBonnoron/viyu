package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestZenProfiles(t *testing.T) {
	root := t.TempDir()
	profileDir := filepath.Join(root, "relative profile")
	if err := os.Mkdir(profileDir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(profileDir, "cookies.sqlite")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	ini := "[Profile0]\nName=Work\nIsRelative=1\nPath=relative profile\n[Profile1]\nName=Missing\nIsRelative=1\nPath=missing\n"
	if err := os.WriteFile(filepath.Join(root, "profiles.ini"), []byte(ini), 0600); err != nil {
		t.Fatal(err)
	}
	profiles, errs := zenProfiles([]string{filepath.Join(root, "absent"), root, root})
	for _, p := range profiles {
		defer p.store.Close()
	}
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if len(profiles) != 1 || profiles[0].browser != "zen" || profiles[0].name != "Work" || profiles[0].store.FilePath() != path {
		t.Fatalf("unexpected profiles: %#v", profiles)
	}
}
