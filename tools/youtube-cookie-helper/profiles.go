package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/browserutils/kooky"
	"github.com/browserutils/kooky/browser/firefox"
	"gopkg.in/ini.v1"
)

type profile struct {
	browser string
	name    string
	store   kooky.CookieStore
}

func zenRoots() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	switch runtime.GOOS {
	case "linux":
		config, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		return []string{filepath.Join(config, "zen"), filepath.Join(home, ".zen"), filepath.Join(home, ".var", "app", "app.zen_browser.zen", "config", "zen"), filepath.Join(home, ".var", "app", "app.zen_browser.zen", ".zen")}, nil
	case "darwin":
		return []string{filepath.Join(home, "Library", "Application Support", "zen")}, nil
	case "windows":
		config, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		return []string{filepath.Join(config, "zen")}, nil
	default:
		return nil, nil
	}
}

func zenProfiles(roots []string) ([]profile, []error) {
	var profiles []profile
	var failures []error
	seen := make(map[string]bool)
	for _, root := range roots {
		file := filepath.Join(root, "profiles.ini")
		config, err := ini.Load(file)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("cannot read Zen profiles at %s: %w", file, err))
			continue
		}
		for _, section := range config.Sections() {
			if !strings.HasPrefix(section.Name(), "Profile") {
				continue
			}
			path := section.Key("Path").String()
			if path == "" {
				failures = append(failures, fmt.Errorf("Zen profile %s has no path", section.Name()))
				continue
			}
			if section.Key("IsRelative").String() == "1" {
				path = filepath.Join(root, filepath.FromSlash(path))
			}
			path = filepath.Clean(filepath.Join(path, "cookies.sqlite"))
			if seen[path] {
				continue
			}
			if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				failures = append(failures, fmt.Errorf("cannot access Zen profile %s: %w", path, err))
				continue
			}
			store, err := firefox.CookieStore(path)
			if err != nil {
				failures = append(failures, fmt.Errorf("cannot open Zen profile %s: %w", path, err))
				continue
			}
			seen[path] = true
			profiles = append(profiles, profile{browser: "zen", name: section.Key("Name").String(), store: store})
		}
	}
	return profiles, failures
}

func discoverProfiles(ctx context.Context) ([]profile, []error) {
	var profiles []profile
	var failures []error
	seen := make(map[string]bool)
	for store, err := range kooky.TraverseCookieStores(ctx) {
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if store == nil {
			continue
		}
		path := store.FilePath()
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			_ = store.Close()
			continue
		} else if err != nil {
			failures = append(failures, fmt.Errorf("cannot access %s: %w", path, err))
			_ = store.Close()
			continue
		}
		key := path
		if store.Browser() == "chrome" || store.Browser() == "chromium" {
			dir := filepath.Dir(path)
			if filepath.Base(dir) == "Network" {
				dir = filepath.Dir(dir)
			}
			key = filepath.Join(dir, "Cookies")
		}
		if seen[key] {
			_ = store.Close()
			continue
		}
		seen[key] = true
		profiles = append(profiles, profile{browser: store.Browser(), name: store.Profile(), store: store})
	}
	roots, err := zenRoots()
	if err != nil {
		failures = append(failures, err)
		return profiles, failures
	}
	zen, errs := zenProfiles(roots)
	return append(profiles, zen...), append(failures, errs...)
}
