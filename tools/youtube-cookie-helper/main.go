package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/chrome"
	_ "github.com/browserutils/kooky/browser/chromium"
	_ "github.com/browserutils/kooky/browser/firefox"
)

const maxJar = 256 * 1024

func youtubeDomain(domain string) bool {
	domain = strings.ToLower(strings.TrimPrefix(domain, "."))
	return domain == "youtube.com" || strings.HasSuffix(domain, ".youtube.com")
}

func safeField(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, c := range s {
		if c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}

func exportCookies(cookies []*kooky.Cookie, now time.Time) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("# Netscape HTTP Cookie File\n")
	var auth, login bool
	for _, c := range cookies {
		if c == nil || !youtubeDomain(c.Domain) || c.Partitioned {
			continue
		}
		if c.Container != "" {
			return nil, errors.New("this profile contains YouTube cookies from a Firefox container; select a profile without containers")
		}
		if c.Name == "" || !strings.HasPrefix(c.Path, "/") || !safeField(c.Domain) || !safeField(c.Path) || !safeField(c.Name) || !safeField(c.Value) {
			return nil, errors.New("a YouTube cookie contains data that cannot be safely exported")
		}
		expires := int64(0)
		if !c.Expires.IsZero() {
			expires = c.Expires.Unix()
			if expires <= now.Unix() {
				continue
			}
		}
		if c.Value != "" {
			switch c.Name {
			case "SAPISID", "__Secure-1PAPISID", "__Secure-3PAPISID":
				auth = true
			case "LOGIN_INFO":
				login = true
			}
		}
		domain := strings.ToLower(c.Domain)
		if c.HttpOnly {
			domain = "#HttpOnly_" + domain
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n", domain, boolText(strings.HasPrefix(c.Domain, ".")), c.Path, boolText(c.Secure), expires, c.Name, c.Value)
		if b.Len() > maxJar {
			return nil, errors.New("cookie export exceeds 256 KiB")
		}
	}
	if !auth || !login {
		return nil, errors.New("no complete YouTube session found in this profile; sign in and try again")
	}
	return b.Bytes(), nil
}

func boolText(b bool) string {
	if b {
		return "TRUE"
	}
	return "FALSE"
}

func copyClipboard(data []byte) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name = "pbcopy"
	case "windows":
		name = "clip.exe"
	case "linux":
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			name = "wl-copy"
		} else if os.Getenv("DISPLAY") != "" {
			name, args = "xclip", []string{"-selection", "clipboard"}
		} else {
			return errors.New("no graphical Wayland or X11 session detected")
		}
	default:
		return errors.New("unsupported operating system")
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin = bytes.NewReader(data)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clipboard copy failed using %s: %w", name, err)
	}
	return nil
}

func run() error {
	fmt.Println("Local YouTube export. Other applications may read, retain, or sync clipboard contents.")
	profiles, failures := discoverProfiles(context.Background())
	for _, err := range failures {
		fmt.Fprintln(os.Stderr, "Warning: could not inspect a browser profile:", err)
	}
	defer func() {
		for _, p := range profiles {
			_ = p.store.Close()
		}
	}()
	if len(profiles) == 0 {
		return errors.New("no accessible Zen, Firefox, Chrome, or Chromium profiles found")
	}
	for i, p := range profiles {
		fmt.Printf("%d. %s — %s (%s)\n", i+1, p.browser, p.name, p.store.FilePath())
	}
	fmt.Print("Select a profile (0 to cancel): ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return errors.New("selection interrupted")
	}
	index, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || index < 0 || index > len(profiles) {
		return errors.New("invalid selection")
	}
	if index == 0 {
		return nil
	}
	if profiles[index-1].browser == "firefox" || profiles[index-1].browser == "zen" {
		fmt.Fprintln(os.Stderr, "Warning: kooky may lose Firefox-based cookie scope and fail to distinguish containers. This prototype has not been validated with a real session.")
	}
	fmt.Print("Copy this profile's YouTube cookies to the clipboard? [y/N]: ")
	line, err = reader.ReadString('\n')
	if err != nil {
		return errors.New("confirmation interrupted")
	}
	if strings.ToLower(strings.TrimSpace(line)) != "y" {
		return nil
	}
	cookies, err := profiles[index-1].store.TraverseCookies(kooky.FilterFunc(func(c *kooky.Cookie) bool { return c != nil && youtubeDomain(c.Domain) })).ReadAllCookies(context.Background())
	if err != nil {
		return fmt.Errorf("could not read profile: %w", err)
	}
	jar, err := exportCookies(cookies, time.Now())
	if err != nil {
		return err
	}
	if err := copyClipboard(jar); err != nil {
		return err
	}
	fmt.Println("Cookies copied. Paste them into the manual import in Viyu, then clear the clipboard and its history. The clipboard is not cleared automatically.")
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
