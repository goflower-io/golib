package cfg

// Unit tests for the cfg package.
// Run with -race to verify there are no data races.
//
// NOTE: tests that call initConfig() directly set the package-level confPath
// variable, so they must NOT run in parallel.

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// -------------------------------------------------------
// Helpers
// -------------------------------------------------------

// newTestConfigs creates a temporary directory pre-populated with files
// (name → content string), sets confPath, calls initConfig, and registers
// c.Close() via t.Cleanup.
func newTestConfigs(t *testing.T, files map[string]string) *Configs {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write test file %s: %v", name, err)
		}
	}
	confPath = dir
	c := initConfig()
	t.Cleanup(func() { c.Close() })
	return c
}

// overwrite writes new content to an existing file and flushes to disk so
// the OS emits a file-system event reliably.
func overwrite(t *testing.T, path string, content []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatalf("overwrite open %s: %v", path, err)
	}
	defer f.Close()
	if _, err := f.Write(content); err != nil {
		t.Fatalf("overwrite write %s: %v", path, err)
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("overwrite sync %s: %v", path, err)
	}
}

// recv waits up to d for a value on ch; returns (value, true) or (nil, false).
func recv(ch <-chan []byte, d time.Duration) ([]byte, bool) {
	select {
	case v, ok := <-ch:
		return v, ok
	case <-time.After(d):
		return nil, false
	}
}

// -------------------------------------------------------
// initConfig — startup behaviour
// -------------------------------------------------------

func TestInitConfig_LoadsFiles(t *testing.T) {
	c := newTestConfigs(t, map[string]string{
		"a.json": `{"a":1}`,
		"b.toml": `b = 2`,
		"c.yaml": `c: 3`,
	})
	for _, key := range []string{"a.json", "b.toml", "c.yaml"} {
		if _, ok := c.Raw(key); !ok {
			t.Errorf("key %q should be present after init", key)
		}
	}
}

func TestInitConfig_SkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	confPath = dir
	c := initConfig()
	t.Cleanup(func() { c.Close() })

	if _, ok := c.Raw("subdir"); ok {
		t.Error("subdirectory must not appear as a config key")
	}
	if _, ok := c.Raw("app.json"); !ok {
		t.Error("app.json should be loaded")
	}
}

func TestInitConfig_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	confPath = dir
	c := initConfig()
	t.Cleanup(func() { c.Close() })
	// An empty directory is valid; nothing to load.
	if _, ok := c.Raw("anything"); ok {
		t.Error("empty dir should yield no keys")
	}
}

func TestInitConfig_EmptyPathPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty confPath")
		}
	}()
	confPath = ""
	initConfig()
}

// -------------------------------------------------------
// Raw / RawString / Content
// -------------------------------------------------------

func TestRaw_KeyExists(t *testing.T) {
	c := newTestConfigs(t, map[string]string{"app.json": `{"name":"test"}`})
	data, ok := c.Raw("app.json")
	if !ok {
		t.Fatal("Raw: expected ok=true")
	}
	if string(data) != `{"name":"test"}` {
		t.Fatalf("Raw: got %q", data)
	}
}

func TestRaw_KeyNotExist(t *testing.T) {
	c := newTestConfigs(t, map[string]string{"app.json": `{}`})
	if _, ok := c.Raw("missing.json"); ok {
		t.Error("Raw: expected ok=false for missing key")
	}
}

func TestRawString_KeyExists(t *testing.T) {
	c := newTestConfigs(t, map[string]string{"note.txt": "hello"})
	s, ok := c.RawString("note.txt")
	if !ok {
		t.Fatal("RawString: expected ok=true")
	}
	if s != "hello" {
		t.Fatalf("RawString: got %q", s)
	}
}

func TestRawString_KeyNotExist(t *testing.T) {
	c := newTestConfigs(t, map[string]string{"app.json": `{}`})
	if s, ok := c.RawString("none"); ok || s != "" {
		t.Error("RawString: expected empty string and ok=false")
	}
}

func TestContent_KeyExists(t *testing.T) {
	c := newTestConfigs(t, map[string]string{"x.json": `data`})
	f, ok := c.Content("x.json")
	if !ok || f == nil {
		t.Fatal("Content: expected non-nil FileContentPipe")
	}
	if string(f.RawContent) != "data" {
		t.Fatalf("Content: RawContent = %q", f.RawContent)
	}
}

func TestContent_KeyNotExist(t *testing.T) {
	c := newTestConfigs(t, map[string]string{"x.json": `{}`})
	if f, ok := c.Content("missing"); ok || f != nil {
		t.Error("Content: expected nil, false for missing key")
	}
}

// -------------------------------------------------------
// UnmarshalTo — format dispatch
// -------------------------------------------------------

type srvCfg struct {
	Port int    `toml:"port" yaml:"port" json:"port"`
	Host string `toml:"host" yaml:"host" json:"host"`
}

func TestUnmarshalTo_JSON(t *testing.T) {
	c := newTestConfigs(t, map[string]string{
		"srv.json": `{"port":9090,"host":"localhost"}`,
	})
	var cfg srvCfg
	if err := c.UnmarshalTo("srv.json", &cfg); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if cfg.Port != 9090 || cfg.Host != "localhost" {
		t.Fatalf("JSON: got %+v", cfg)
	}
}

func TestUnmarshalTo_TOML(t *testing.T) {
	c := newTestConfigs(t, map[string]string{
		"srv.toml": "port = 8080\nhost = \"127.0.0.1\"\n",
	})
	var cfg srvCfg
	if err := c.UnmarshalTo("srv.toml", &cfg); err != nil {
		t.Fatalf("TOML: %v", err)
	}
	if cfg.Port != 8080 || cfg.Host != "127.0.0.1" {
		t.Fatalf("TOML: got %+v", cfg)
	}
}

func TestUnmarshalTo_YML(t *testing.T) {
	c := newTestConfigs(t, map[string]string{
		"srv.yml": "port: 7070\nhost: example.com\n",
	})
	var cfg srvCfg
	if err := c.UnmarshalTo("srv.yml", &cfg); err != nil {
		t.Fatalf("YML: %v", err)
	}
	if cfg.Port != 7070 || cfg.Host != "example.com" {
		t.Fatalf("YML: got %+v", cfg)
	}
}

// .yaml extension must be treated identically to .yml.
func TestUnmarshalTo_YAML(t *testing.T) {
	c := newTestConfigs(t, map[string]string{
		"srv.yaml": "port: 6060\nhost: api.example.com\n",
	})
	var cfg srvCfg
	if err := c.UnmarshalTo("srv.yaml", &cfg); err != nil {
		t.Fatalf("YAML: %v", err)
	}
	if cfg.Port != 6060 || cfg.Host != "api.example.com" {
		t.Fatalf("YAML: got %+v", cfg)
	}
}

// Extension matching must be case-insensitive.
func TestUnmarshalTo_CaseInsensitiveExtension(t *testing.T) {
	c := newTestConfigs(t, map[string]string{
		"SRV.JSON": `{"port":1234,"host":"ci"}`,
	})
	var cfg srvCfg
	if err := c.UnmarshalTo("SRV.JSON", &cfg); err != nil {
		t.Fatalf("case-insensitive: %v", err)
	}
	if cfg.Port != 1234 {
		t.Fatalf("case-insensitive: port = %d", cfg.Port)
	}
}

func TestUnmarshalTo_KeyNotExist(t *testing.T) {
	c := newTestConfigs(t, map[string]string{"a.json": `{}`})
	if err := c.UnmarshalTo("missing.json", &struct{}{}); err == nil {
		t.Error("expected error for missing key")
	}
}

func TestUnmarshalTo_UnsupportedFormat(t *testing.T) {
	c := newTestConfigs(t, map[string]string{"app.ini": "[section]\nkey=val"})
	err := c.UnmarshalTo("app.ini", &struct{}{})
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error should mention 'unsupported', got: %v", err)
	}
}

func TestUnmarshalTo_InvalidJSON(t *testing.T) {
	c := newTestConfigs(t, map[string]string{"bad.json": `{not json}`})
	if err := c.UnmarshalTo("bad.json", &struct{}{}); err == nil {
		t.Error("expected parse error for invalid JSON")
	}
}

func TestUnmarshalTo_InvalidTOML(t *testing.T) {
	c := newTestConfigs(t, map[string]string{"bad.toml": `[[[invalid`})
	if err := c.UnmarshalTo("bad.toml", &struct{}{}); err == nil {
		t.Error("expected parse error for invalid TOML")
	}
}

func TestUnmarshalTo_InvalidYAML(t *testing.T) {
	c := newTestConfigs(t, map[string]string{"bad.yaml": ":\t:\n"})
	if err := c.UnmarshalTo("bad.yaml", &struct{}{}); err == nil {
		t.Error("expected parse error for invalid YAML")
	}
}

// -------------------------------------------------------
// Watch — subscription
// -------------------------------------------------------

func TestWatch_KeyNotExist(t *testing.T) {
	c := newTestConfigs(t, map[string]string{"app.json": `{}`})
	if _, err := c.Watch("missing.json"); err == nil {
		t.Error("Watch: expected error for missing key")
	}
}

// Write event (in-place edit, e.g. nano / cat >) must be delivered.
func TestWatch_DetectsWriteEvent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.json")
	if err := os.WriteFile(path, []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	confPath = dir
	c := initConfig()
	t.Cleanup(func() { c.Close() })

	ch, err := c.Watch("app.json")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	overwrite(t, path, []byte(`{"v":2}`))

	got, ok := recv(ch, 2*time.Second)
	if !ok {
		t.Fatal("Watch: timed out waiting for Write event")
	}
	if string(got) != `{"v":2}` {
		t.Fatalf("Watch: got %q, want {\"v\":2}", got)
	}
	// In-memory copy must be updated atomically.
	raw, _ := c.Raw("app.json")
	if string(raw) != `{"v":2}` {
		t.Fatalf("Raw after update: got %q", raw)
	}
}

// Create event (vim / emacs rename-on-save) must also be delivered.
func TestWatch_DetectsCreateEvent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "app.json")
	if err := os.WriteFile(target, []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	confPath = dir
	c := initConfig()
	t.Cleanup(func() { c.Close() })

	ch, err := c.Watch("app.json")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Atomic rename: write to a temp file, then rename (vim/emacs pattern).
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, []byte(`{"v":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, target); err != nil {
		t.Fatal(err)
	}

	got, ok := recv(ch, 2*time.Second)
	if !ok {
		t.Fatal("Watch: timed out waiting for Create/Rename event")
	}
	if string(got) != `{"v":2}` {
		t.Fatalf("Watch: got %q, want {\"v\":2}", got)
	}
}

// Events for files that were not loaded at startup (swap files, temp files, …)
// must not trigger updates for registered subscribers.
func TestWatch_IgnoresUnknownFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	confPath = dir
	c := initConfig()
	t.Cleanup(func() { c.Close() })

	ch, _ := c.Watch("app.json")

	// Write to an editor swap file that was NOT in the initial load.
	_ = os.WriteFile(filepath.Join(dir, ".app.json.swp"), []byte(`swp`), 0o644)

	if _, ok := recv(ch, 300*time.Millisecond); ok {
		t.Error("swap-file write must not trigger an app.json update")
	}
}

// Each Watch call on the same key registers an independent subscriber channel;
// all of them must receive the update.
func TestWatch_MultipleSubscribers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.json")
	if err := os.WriteFile(path, []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	confPath = dir
	c := initConfig()
	t.Cleanup(func() { c.Close() })

	ch1, _ := c.Watch("app.json")
	ch2, _ := c.Watch("app.json")
	ch3, _ := c.Watch("app.json")

	overwrite(t, path, []byte(`{"v":2}`))

	for i, ch := range []<-chan []byte{ch1, ch2, ch3} {
		if _, ok := recv(ch, 2*time.Second); !ok {
			t.Errorf("subscriber %d: timed out waiting for update", i+1)
		}
	}
}

// A slow consumer whose channel buffer is full must not block the watcher
// goroutine. Subsequent updates must continue to be processed without deadlock.
func TestWatch_SlowConsumerDropsUpdates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	confPath = dir
	c := initConfig()
	t.Cleanup(func() { c.Close() })

	// Register a subscriber but never read from it — buffer (8) will fill.
	_, err := c.Watch("app.json")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Write more times than the channel buffer; watcher must not deadlock.
	for i := 0; i < 15; i++ {
		overwrite(t, path, []byte(`{}`))
		time.Sleep(5 * time.Millisecond)
	}
	// If we reach here the goroutine is still alive — test passes.
}

// -------------------------------------------------------
// Close
// -------------------------------------------------------

// Close must close all subscriber channels so range-loops can exit cleanly.
func TestClose_SubscriberChannelsAreClosed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	confPath = dir
	c := initConfig() // NOT registered in cleanup; we close manually below.

	ch, err := c.Watch("app.json")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Channel must be closed within 2 s.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed (ok=false), got ok=true")
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out: subscriber channel was not closed after Close()")
	}
}

// Close with no subscribers must not panic.
func TestClose_NoSubscribers(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	confPath = dir
	c := initConfig()
	if err := c.Close(); err != nil {
		t.Fatalf("Close: unexpected error: %v", err)
	}
}

// -------------------------------------------------------
// Concurrent read / write — run with -race
// -------------------------------------------------------

// TestConcurrentReadWrite verifies that concurrent reads (Raw, Content) racing
// against file-system-triggered copy-on-write updates produce no data races.
// Run with: go test -race ./cfg/...
func TestConcurrentReadWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.json")
	if err := os.WriteFile(path, []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	confPath = dir
	c := initConfig()
	t.Cleanup(func() { c.Close() })

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Four concurrent reader goroutines.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					c.Raw("app.json")
					c.Content("app.json")
					c.RawString("app.json")
				}
			}
		}()
	}

	// Writer goroutine that triggers file events.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 8; i++ {
			overwrite(t, path, []byte(`{"v":2}`))
			time.Sleep(15 * time.Millisecond)
		}
	}()

	// Let everything run for a moment, then signal stop.
	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}
