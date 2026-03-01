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
	flag.StringVar(&confPath, "cfg", "configs", "configuration fils paths default ./configs/")
}

func Config() *Configs {
	return configFn()
}

var configFn = sync.OnceValue(initConfig)

type FileContentPipe struct {
	RawContent     []byte
	RawContentPipe chan []byte
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
		data, err := os.ReadFile(filepath.Join(confPath, f.Name()))
		if err != nil {
			panic(err)
		}
		x[f.Name()] = &FileContentPipe{RawContent: data}

	}
	at := atomic.Value{}
	at.Store(x)
	wc, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}
	if err := wc.Add(confPath); err != nil {
		panic(err)
	}
	ret := &Configs{
		dir:       confPath,
		filesRW:   at,
		fsWatcher: wc,
		closeCh:   make(chan struct{}),
	}
	go ret.watch()
	return ret
}

type EventFn func(event *fsnotify.Event)

type Configs struct {
	// config dir path
	dir string

	filesRW atomic.Value

	fsWatcher *fsnotify.Watcher

	closeCh chan struct{}
}

func (c *Configs) UnmarshalTo(key string, object any) error {
	if v, ok := c.Raw(key); !ok {
		return errors.New("key not exist")
	} else {
		if strings.HasSuffix(strings.ToLower(key), ".toml") {
			return toml.Unmarshal(v, object)
		}

		if strings.HasSuffix(strings.ToLower(key), ".yml") {
			return yaml.Unmarshal(v, object)
		}

		if strings.HasSuffix(strings.ToLower(key), ".json") {
			return json.Unmarshal(v, object)
		}
		return errors.New("not support config file format")
	}
}

func (c *Configs) Watch(key string) (chan []byte, error) {
	var f *FileContentPipe
	var ok bool
	if f, ok = c.Content(key); !ok {
		return nil, errors.New("key is not exist")
	}
	f.RawContentPipe = make(chan []byte, 4)
	return f.RawContentPipe, nil
}

func (c *Configs) Raw(key string) ([]byte, bool) {
	d, x := c.Content(key)
	if x {
		return d.RawContent, x
	}
	return nil, false
}

func (c *Configs) RawString(key string) (string, bool) {
	d, x := c.Content(key)
	if x {
		return string(d.RawContent), x
	}
	return "", false
}

func (c *Configs) Content(key string) (*FileContentPipe, bool) {
	v := c.filesRW.Load().(map[string]*FileContentPipe)
	f, ok := v[key]
	if ok {
		return f, ok
	}
	return nil, false
}

func (c *Configs) watch() {
	for {
		select {
		case event, ok := <-c.fsWatcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) {
				// read file name
				content, err := os.ReadFile(event.Name)
				if err != nil {
					slog.Error("readfile", "error", err.Error())
					continue
				}
				final := filepath.Base(event.Name)
				v, ok := c.filesRW.Load().(map[string]*FileContentPipe)
				if ok {
					ff, ok2 := v[final]
					if ok2 {
						ff.RawContent = content
						if ff.RawContentPipe != nil {
							ff.RawContentPipe <- content
						}
					}
				}
			}

		case e, ok := <-c.fsWatcher.Errors:
			if !ok {
				return
			}
			slog.Error("watch file", "error", e.Error())
		}
	}
}
