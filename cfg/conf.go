package cfg

import (
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/BurntSushi/toml"
	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v2"
)

var confPath string

func init() {
	flag.StringVar(&confPath, "cfg", "configs", "configuration files path (default: ./configs/)")
}

// Config returns the singleton Configs instance, initializing it on first call.
func Config() *Configs {
	return configFn()
}

var configFn = sync.OnceValue(initConfig)

// FileContentPipe holds the raw bytes of a configuration file.
// Values are treated as immutable; every file update produces a fresh instance.
type FileContentPipe struct {
	RawContent []byte
}

func initConfig() *Configs {
	if confPath == "" {
		panic("empty cfg path")
	}
	files, err := os.ReadDir(confPath)
	if err != nil {
		panic(err)
	}
	x := make(map[string]*FileContentPipe)
	for _, f := range files {
		if f.IsDir() {
			continue // skip subdirectories
		}
		data, err := os.ReadFile(filepath.Join(confPath, f.Name()))
		if err != nil {
			panic(err)
		}
		x[f.Name()] = &FileContentPipe{RawContent: data}
	}

	wc, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}
	if err := wc.Add(confPath); err != nil {
		panic(err)
	}

	// Build the struct first, then Store — copying a used atomic.Value is UB.
	ret := &Configs{
		dir:       confPath,
		subs:      make(map[string][]chan []byte),
		fsWatcher: wc,
	}
	ret.filesRW.Store(x)

	go ret.watch()
	return ret
}

// Configs holds all configuration files loaded from the config directory
// and watches for changes, notifying registered subscribers automatically.
type Configs struct {
	dir string

	// filesRW stores map[string]*FileContentPipe using copy-on-write:
	// every update replaces the entire map so readers always see a
	// consistent snapshot without any additional locking.
	filesRW atomic.Value

	subsMu sync.RWMutex
	subs   map[string][]chan []byte // subscriber channels keyed by filename

	fsWatcher *fsnotify.Watcher
}

// Close shuts down the file watcher and closes all subscriber channels.
func (c *Configs) Close() error {
	return c.fsWatcher.Close()
}

// UnmarshalTo decodes the config file identified by key into object.
// Supported formats: .toml, .yml, .yaml, .json.
func (c *Configs) UnmarshalTo(key string, object any) error {
	v, ok := c.Raw(key)
	if !ok {
		return errors.New("key not exist")
	}
	lower := strings.ToLower(key)
	switch {
	case strings.HasSuffix(lower, ".toml"):
		return toml.Unmarshal(v, object)
	case strings.HasSuffix(lower, ".yml"), strings.HasSuffix(lower, ".yaml"):
		return yaml.Unmarshal(v, object)
	case strings.HasSuffix(lower, ".json"):
		return json.Unmarshal(v, object)
	default:
		return errors.New("unsupported config file format")
	}
}

// Watch returns a read-only channel that receives the new raw content whenever
// the file identified by key changes on disk. The channel is buffered (size 8).
// If the consumer is too slow, updates are dropped with a warning rather than
// blocking the watcher goroutine. The channel is closed when Close() is called.
func (c *Configs) Watch(key string) (<-chan []byte, error) {
	if _, ok := c.Raw(key); !ok {
		return nil, errors.New("key not exist")
	}
	ch := make(chan []byte, 8)
	c.subsMu.Lock()
	c.subs[key] = append(c.subs[key], ch)
	c.subsMu.Unlock()
	return ch, nil
}

// Raw returns the raw bytes of the config file identified by key.
func (c *Configs) Raw(key string) ([]byte, bool) {
	d, ok := c.Content(key)
	if ok {
		return d.RawContent, true
	}
	return nil, false
}

// RawString returns the content of the config file identified by key as a string.
func (c *Configs) RawString(key string) (string, bool) {
	d, ok := c.Content(key)
	if ok {
		return string(d.RawContent), true
	}
	return "", false
}

// Content returns the FileContentPipe for the given key.
func (c *Configs) Content(key string) (*FileContentPipe, bool) {
	v := c.filesRW.Load().(map[string]*FileContentPipe)
	f, ok := v[key]
	return f, ok
}

// watch listens for file system events and keeps the in-memory store in sync.
// It exits when the fsWatcher is closed (via Close()), at which point all
// subscriber channels are closed to signal consumers that no more updates follow.
func (c *Configs) watch() {
	defer func() {
		c.subsMu.Lock()
		for _, chs := range c.subs {
			for _, ch := range chs {
				close(ch)
			}
		}
		c.subsMu.Unlock()
	}()

	for {
		select {
		case event, ok := <-c.fsWatcher.Events:
			if !ok {
				return
			}
			// Write  — in-place editor save (nano, cat >)
			// Create — rename-on-save editors (vim, emacs, many IDEs)
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				c.reloadFile(event.Name)
			}

		case e, ok := <-c.fsWatcher.Errors:
			if !ok {
				return
			}
			slog.Error("config watcher error", "error", e)
		}
	}
}

// reloadFile reads the file at path, replaces its entry in the atomic map via
// copy-on-write, and notifies subscribers with a non-blocking send.
func (c *Configs) reloadFile(path string) {
	key := filepath.Base(path)

	old := c.filesRW.Load().(map[string]*FileContentPipe)
	if _, exists := old[key]; !exists {
		// Ignore events for files that were not present at startup
		// (e.g. editor swap files, .swp, #autosave#, …).
		return
	}

	content, err := os.ReadFile(path)
	if err != nil {
		slog.Error("config reload: read file", "path", path, "error", err)
		return
	}

	// Copy-on-write: build a fresh map so concurrent readers always observe a
	// consistent snapshot without holding any lock.
	newMap := make(map[string]*FileContentPipe, len(old))
	for k, v := range old {
		newMap[k] = v
	}
	newMap[key] = &FileContentPipe{RawContent: content}
	c.filesRW.Store(newMap)

	// Notify subscribers. Drop the update rather than blocking the watcher.
	c.subsMu.RLock()
	for _, ch := range c.subs[key] {
		select {
		case ch <- content:
		default:
			slog.Warn("config subscriber channel full, update dropped", "key", key)
		}
	}
	c.subsMu.RUnlock()
}
