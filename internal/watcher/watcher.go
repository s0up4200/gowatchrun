package watcher

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
)

type EventData struct {
	Path     string
	Name     string
	Event    string
	Ext      string
	Dir      string
	BaseName string
}

type ExecutorFunc func(commandTmpl string, data *EventData)

type Config struct {
	WatchDirs     []string
	ExcludeDirs   []string
	Patterns      []string
	EventTypes    []string
	CommandTmpl   string
	Recursive     bool
	DebounceDelay time.Duration
}

func Run(cfg Config, execFunc ExecutorFunc) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal().Msgf("Failed to create watcher: %v", err)
		return err
	}
	defer watcher.Close()

	if cfg.DebounceDelay > 0 {
		log.Info().Msgf("Debounce delay set to: %s", cfg.DebounceDelay)
	}

	allowedEvents := processEventTypes(cfg.EventTypes)

	done := make(chan bool)
	go func() {
		defer close(done)
		var debounceTimer *time.Timer
		var lastEventData *EventData
		var timerChan <-chan time.Time

		for {
			if debounceTimer != nil {
				timerChan = debounceTimer.C
			} else {
				timerChan = nil
			}

			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				eventData := filterEvent(event, allowedEvents, cfg.Patterns)
				if eventData == nil {
					continue
				}

				lastEventData = eventData
				if cfg.DebounceDelay > 0 {
					log.Debug().Msgf("Debouncing event for %s", eventData.Path)
					if debounceTimer == nil {
						debounceTimer = time.NewTimer(cfg.DebounceDelay)
					} else {
						if !debounceTimer.Stop() {
							select {
							case <-debounceTimer.C:
							default:
							}
						}
						debounceTimer.Reset(cfg.DebounceDelay)
					}
				} else {
					execFunc(cfg.CommandTmpl, eventData)
				}

			case <-timerChan:
				log.Debug().Msg("Debounce timer fired.")
				if lastEventData != nil {
					execFunc(cfg.CommandTmpl, lastEventData)
					lastEventData = nil
				}
				debounceTimer = nil

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Error().Msgf("Watcher error: %v", err)
			}
		}
	}()

	log.Info().Msgf("Starting watcher for directories: %v", cfg.WatchDirs)
	if cfg.Recursive {
		log.Info().Msg("Recursive mode enabled.")
	}
	log.Info().Msgf("Watching for patterns: %v", cfg.Patterns)
	log.Info().Msgf("Triggering on events: %v", cfg.EventTypes)
	log.Info().Msgf("Command template configured: %s", cfg.CommandTmpl)

	absExcludedDirs := make(map[string]bool)
	if len(cfg.ExcludeDirs) > 0 {
		log.Info().Msgf("Excluding directories: %v", cfg.ExcludeDirs)
		for _, exDir := range cfg.ExcludeDirs {
			absExDir, err := filepath.Abs(exDir)
			if err != nil {
				log.Warn().Msgf("Could not get absolute path for excluded directory %s: %v", exDir, err)
				continue
			}
			absExcludedDirs[absExDir] = true
		}
	}

	for _, dir := range cfg.WatchDirs {
		if cfg.Recursive {
			walkErr := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					log.Warn().Msgf("Error accessing path %q: %v", path, err)
					return err
				}

				if info.IsDir() {
					absPath, pathErr := filepath.Abs(path)
					if pathErr != nil {
						log.Warn().Msgf("Could not get absolute path for %s: %v", path, pathErr)
						return nil
					}

					for exPath := range absExcludedDirs {
						if strings.HasPrefix(absPath+string(filepath.Separator), exPath+string(filepath.Separator)) {
							log.Debug().Msgf("Skipping excluded directory: %s", path)
							return filepath.SkipDir
						}
					}

					log.Debug().Msgf("Adding recursive watch for: %s", path)
					if watchErr := watcher.Add(path); watchErr != nil {
						log.Warn().Msgf("Failed to add recursive watch for %s: %v", path, watchErr)
					}
				}
				return nil
			})
			if walkErr != nil {
				log.Error().Msgf("Error walking the path %q: %v", dir, walkErr)
			}
		} else {
			log.Info().Msgf("Adding watch for: %s", dir)
			if err = watcher.Add(dir); err != nil {
				log.Warn().Msgf("Failed to add watch for %s: %v", dir, err)
			}
		}
	}

	<-done
	log.Info().Msg("Watcher stopped.")
	return nil
}

func processEventTypes(types []string) map[fsnotify.Op]bool {
	lookup := make(map[fsnotify.Op]bool)
	hasAll := false
	for _, t := range types {
		if strings.ToLower(t) == "all" {
			hasAll = true
			break
		}
	}

	isUnportableSupported := func() bool {
		return runtime.GOOS == "linux" || runtime.GOOS == "freebsd"
	}

	if hasAll {
		lookup[fsnotify.Create] = true
		lookup[fsnotify.Write] = true
		lookup[fsnotify.Remove] = true
		lookup[fsnotify.Rename] = true
		lookup[fsnotify.Chmod] = true
		if isUnportableSupported() {
			lookup[fsnotify.Op(1<<5)] = true // Inotify: IN_OPEN
			lookup[fsnotify.Op(1<<6)] = true // Inotify: IN_ACCESS
			lookup[fsnotify.Op(1<<7)] = true // Inotify: IN_CLOSE_WRITE
			lookup[fsnotify.Op(1<<8)] = true // Inotify: IN_CLOSE_NOWRITE
		}
		return lookup
	}

	for _, t := range types {
		switch strings.ToLower(t) {
		case "create":
			lookup[fsnotify.Create] = true
		case "write":
			lookup[fsnotify.Write] = true
		case "remove":
			lookup[fsnotify.Remove] = true
		case "rename":
			lookup[fsnotify.Rename] = true
		case "chmod":
			lookup[fsnotify.Chmod] = true
		case "open":
			if isUnportableSupported() {
				lookup[fsnotify.Op(1<<5)] = true
			} else {
				log.Error().Msg("'open' event is only supported on Linux and FreeBSD; exiting.")
				os.Exit(1)
			}
		case "read":
			if isUnportableSupported() {
				lookup[fsnotify.Op(1<<6)] = true
			} else {
				log.Error().Msg("'read' event is only supported on Linux and FreeBSD; exiting.")
				os.Exit(1)
			}
		case "closewrite":
			if isUnportableSupported() {
				lookup[fsnotify.Op(1<<7)] = true
			} else {
				log.Error().Msg("'closewrite' event is only supported on Linux and FreeBSD; exiting.")
				os.Exit(1)
			}
		case "closeread":
			if isUnportableSupported() {
				lookup[fsnotify.Op(1<<8)] = true
			} else {
				log.Error().Msg("'closeread' event is only supported on Linux and FreeBSD; exiting.")
				os.Exit(1)
			}
		default:
			log.Warn().Msgf("Warning: Unknown event type '%s' ignored.", t)
		}
	}
	return lookup
}

func filterEvent(event fsnotify.Event, allowedEvents map[fsnotify.Op]bool, patterns []string) *EventData {
	triggered := false
	var eventStr string
	for op, allowed := range allowedEvents {
		if allowed && event.Has(op) {
			triggered = true
			eventStr = op.String()
			break
		}
	}
	if !triggered {
		log.Trace().Msgf("Ignoring event type %s for %s", event.Op.String(), event.Name)
		return nil
	}

	matchedPattern := false
	fileName := filepath.Base(event.Name)
	for _, pattern := range patterns {
		match, err := filepath.Match(pattern, fileName)
		if err != nil {
			log.Error().Msgf("Error matching pattern '%s' with file '%s': %v", pattern, fileName, err)
			continue
		}
		if match {
			matchedPattern = true
			break
		}
	}
	if !matchedPattern {
		log.Trace().Msgf("Ignoring file %s (no pattern match)", event.Name)
		return nil
	}

	log.Info().Msgf("Detected %s event for: %s", eventStr, event.Name)

	ext := filepath.Ext(fileName)
	return &EventData{
		Path:     event.Name,
		Name:     fileName,
		Event:    eventStr,
		Ext:      ext,
		Dir:      filepath.Dir(event.Name),
		BaseName: strings.TrimSuffix(fileName, ext),
	}
}
