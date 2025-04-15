package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	watchDirs   []string
	excludeDirs []string
	patterns    []string
	eventTypes  []string
	commandTmpl string
	recursive   bool
	logLevel    string
	delayStr    string // New flag for delay duration string
)

var debounceDelay time.Duration // Variable to store parsed duration

type EventData struct {
	Path     string
	Name     string
	Event    string
	Ext      string
	Dir      string
	BaseName string
}

var rootCmd = &cobra.Command{
	Use:   "gowatchrun",
	Short: "Watches files and runs a command template on changes.",
	Long: `gowatchrun monitors specified directories for file changes
matching given patterns and executes a command template,
substituting placeholders with event details.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		level, err := zerolog.ParseLevel(logLevel)
		if err != nil {
			log.Warn().Msgf("Invalid log level '%s', defaulting to 'info'. Error: %v", logLevel, err)
			level = zerolog.InfoLevel
		}
		zerolog.SetGlobalLevel(level)
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
		log.Debug().Msgf("Log level set to: %s", level.String())
	},
	Run: func(cmd *cobra.Command, args []string) {
		runWatcher()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.Flags().StringSliceVarP(&watchDirs, "watch", "w", []string{"."}, "Directory(ies) to watch (can be specified multiple times)")
	rootCmd.Flags().StringSliceVarP(&patterns, "pattern", "p", []string{"*.*"}, "Glob pattern(s) for files to watch (can be specified multiple times)")
	rootCmd.Flags().StringSliceVarP(&eventTypes, "event", "e", []string{"all"}, "Event type(s) to trigger on (write, create, remove, rename, chmod, open, read, closewrite, closeread, all - can be specified multiple times). 'open', 'read', 'closewrite', 'closeread' are only supported on Linux and FreeBSD.")
	rootCmd.Flags().StringVarP(&commandTmpl, "command", "c", "", "Command template to execute (required)")
	rootCmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "Watch directories recursively")
	rootCmd.Flags().StringSliceVarP(&excludeDirs, "exclude", "x", []string{}, "Directory path(s) to exclude (can be specified multiple times)")
	rootCmd.Flags().StringVar(&logLevel, "log-level", "info", "Set the logging level (e.g., debug, info, warn, error, fatal, panic)")
	rootCmd.Flags().StringVar(&delayStr, "delay", "0s", "Debounce delay (e.g., 500ms, 1s, 2s)") // Add the delay flag

	if err := rootCmd.MarkFlagRequired("command"); err != nil {
		log.Fatal().Msgf("Failed to mark 'command' flag as required: %v", err)
	}

	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
}

func runWatcher() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal().Msgf("Failed to create watcher: %v", err)
	}
	defer watcher.Close()

	// Parse the delay duration
	var parseErr error // Use a different variable name to avoid redeclaration issues initially
	debounceDelay, parseErr = time.ParseDuration(delayStr)
	if parseErr != nil {
		log.Warn().Msgf("Invalid --delay duration '%s', defaulting to 0s. Error: %v", delayStr, parseErr)
		debounceDelay = 0
	} else if debounceDelay < 0 {
		log.Warn().Msgf("--delay duration '%s' is negative, defaulting to 0s.", delayStr)
		debounceDelay = 0
	} else if debounceDelay > 0 { // Only log if delay is actually used
		log.Info().Msgf("Debounce delay set to: %s", debounceDelay)
	}

	allowedEvents := processEventTypes(eventTypes)

	done := make(chan bool)
	go func() {
		defer close(done)
		var debounceTimer *time.Timer  // Timer for debouncing
		var lastEventData *EventData   // Store the last event data during debounce
		var timerChan <-chan time.Time // Channel to use in select, nil when timer inactive

		for {
			// Set timerChan based on debounceTimer's state *before* the select
			if debounceTimer != nil {
				timerChan = debounceTimer.C
			} else {
				timerChan = nil // Disable the timer case in select
			}

			select {
			case event, ok := <-watcher.Events:
				if !ok { // Event channel closed
					return
				}
				// Filter the event first
				eventData := filterEvent(event, allowedEvents, patterns)
				if eventData == nil {
					continue // Event didn't pass filters
				}

				// Debounce logic
				lastEventData = eventData // Store the latest event data
				if debounceDelay > 0 {
					log.Debug().Msgf("Debouncing event for %s", eventData.Path)
					if debounceTimer == nil {
						debounceTimer = time.NewTimer(debounceDelay)
					} else {
						if !debounceTimer.Stop() {
							// Drain the channel if Stop() returns false, indicating the timer already fired.
							// This is unlikely in typical scenarios but good practice.
							select {
							case <-debounceTimer.C:
							default:
							}
						}
						debounceTimer.Reset(debounceDelay)
					}
				} else {
					// No delay, execute immediately
					executeCommand(commandTmpl, eventData)
				}

			case <-timerChan: // Use the controlled channel here
				log.Debug().Msg("Debounce timer fired.")
				if lastEventData != nil {
					executeCommand(commandTmpl, lastEventData)
					lastEventData = nil // Clear data after execution
				}
				debounceTimer = nil // Mark timer as inactive

			case err, ok := <-watcher.Errors:
				if !ok {
					return // Error channel closed
				}
				log.Error().Msgf("Watcher error: %v", err)
			}
		}
	}()

	log.Info().Msgf("Starting watcher for directories: %v", watchDirs)
	if recursive {
		log.Info().Msg("Recursive mode enabled.")
	}
	log.Info().Msgf("Watching for patterns: %v", patterns)
	log.Info().Msgf("Triggering on events: %v", eventTypes)
	log.Info().Msgf("Executing command template: %s", commandTmpl)

	absExcludedDirs := make(map[string]bool)
	if len(excludeDirs) > 0 {
		log.Info().Msgf("Excluding directories: %v", excludeDirs)
		for _, exDir := range excludeDirs {
			absExDir, err := filepath.Abs(exDir)
			if err != nil {
				log.Warn().Msgf("Could not get absolute path for excluded directory %s: %v", exDir, err)
				continue
			}
			absExcludedDirs[absExDir] = true
			//log.Debug().Msgf("Absolute excluded path added: %s", absExDir)
		}
	}

	for _, dir := range watchDirs {
		if recursive {
			err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					log.Warn().Msgf("Error accessing path %q: %v", path, err)
					return err // Propagate error to stop Walk if needed
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
			if err != nil {
				log.Error().Msgf("Error walking the path %q: %v", dir, err)
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
			lookup[fsnotify.Op(1<<5)] = true // xUnportableOpen
			lookup[fsnotify.Op(1<<6)] = true // xUnportableRead
			lookup[fsnotify.Op(1<<7)] = true // xUnportableCloseWrite
			lookup[fsnotify.Op(1<<8)] = true // xUnportableCloseRead
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
				lookup[fsnotify.Op(1<<5)] = true // xUnportableOpen
			} else {
				log.Error().Msg("'open' event is only supported on Linux and FreeBSD; exiting.")
				os.Exit(1)
			}
		case "read":
			if isUnportableSupported() {
				lookup[fsnotify.Op(1<<6)] = true // xUnportableRead
			} else {
				log.Error().Msg("'read' event is only supported on Linux and FreeBSD; exiting.")
				os.Exit(1)
			}
		case "closewrite":
			if isUnportableSupported() {
				lookup[fsnotify.Op(1<<7)] = true // xUnportableCloseWrite
			} else {
				log.Error().Msg("'closewrite' event is only supported on Linux and FreeBSD; exiting.")
				os.Exit(1)
			}
		case "closeread":
			if isUnportableSupported() {
				lookup[fsnotify.Op(1<<8)] = true // xUnportableCloseRead
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

// filterEvent checks if an event matches the criteria and returns EventData if it does, otherwise nil.
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

	// Handle adding watch for newly created directories in recursive mode
	// Note: This might still have race conditions or miss rapid creations.
	// A more robust solution might involve periodic rescans or a different watcher library.
	if recursive && event.Has(fsnotify.Create) {
		info, err := os.Stat(event.Name)
		if err == nil && info.IsDir() {
			log.Debug().Msgf("Adding recursive watch for newly created directory: %s", event.Name)
			// TODO: Implement dynamic addition of created directories in recursive mode.
		}
	}

	log.Info().Msgf("Detected %s event for: %s", eventStr, event.Name) // Keep this info log

	ext := filepath.Ext(fileName)
	return &EventData{ // Return the data instead of executing
		Path:     event.Name,
		Name:     fileName,
		Event:    eventStr,
		Ext:      ext,
		Dir:      filepath.Dir(event.Name),
		BaseName: strings.TrimSuffix(fileName, ext),
	}
}

// executeCommand takes the command template and event data, then executes the command.
func executeCommand(commandTmpl string, data *EventData) {
	if data == nil {
		log.Warn().Msg("Attempted to execute command with nil event data.")
		return
	}

	tmpl, err := template.New("command").Parse(commandTmpl)
	if err != nil {
		log.Error().Msgf("Error parsing command template: %v", err)
		return
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		log.Error().Msgf("Error executing command template with data %+v: %v", data, err)
		return
	}

	cmdString := buf.String()
	log.Info().Msgf("Executing: %s", cmdString)

	// Note: Consider adding process management here later (kill/queue/ignore)
	cmdExec := exec.Command("sh", "-c", cmdString)
	cmdExec.Stdout = os.Stdout
	cmdExec.Stderr = os.Stderr
	cmdExec.Stdin = os.Stdin // Allow command to receive stdin

	startTime := time.Now()
	err = cmdExec.Run()
	duration := time.Since(startTime)

	if err != nil {
		// Log error with event details for better debugging
		log.Error().
			Str("command", cmdString).
			Str("event_path", data.Path).
			Str("event_type", data.Event).
			Dur("duration", duration.Round(time.Millisecond)).
			Err(err).
			Msg("Command execution failed")
	} else {
		log.Info().
			Str("command", cmdString).
			Dur("duration", duration.Round(time.Millisecond)).
			Msg("Command executed successfully")
	}
}
