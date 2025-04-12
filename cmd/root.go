package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
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
)

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
	rootCmd.Flags().StringSliceVarP(&eventTypes, "event", "e", []string{"all"}, "Event type(s) to trigger on (write, create, remove, rename, chmod, all - can be specified multiple times)")
	rootCmd.Flags().StringVarP(&commandTmpl, "command", "c", "", "Command template to execute (required)")
	rootCmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "Watch directories recursively")
	rootCmd.Flags().StringSliceVarP(&excludeDirs, "exclude", "x", []string{}, "Directory path(s) to exclude (can be specified multiple times)")
	rootCmd.Flags().StringVar(&logLevel, "log-level", "info", "Set the logging level (e.g., debug, info, warn, error, fatal, panic)")

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

	allowedEvents := processEventTypes(eventTypes)

	done := make(chan bool)
	go func() {
		defer close(done)
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				handleEvent(event, allowedEvents, patterns, commandTmpl)
			case err, ok := <-watcher.Errors:
				if !ok {
					return
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

	if hasAll {
		return map[fsnotify.Op]bool{
			fsnotify.Create: true,
			fsnotify.Write:  true,
			fsnotify.Remove: true,
			fsnotify.Rename: true,
			fsnotify.Chmod:  true,
		}
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
		default:
			log.Warn().Msgf("Warning: Unknown event type '%s' ignored.", t)
		}
	}
	return lookup
}

func handleEvent(event fsnotify.Event, allowedEvents map[fsnotify.Op]bool, patterns []string, commandTmpl string) {
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
		// log.Printf("Ignoring event type %s for %s", event.Op.String(), event.Name)
		return
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
		// log.Printf("Ignoring file %s (no pattern match)", event.Name)
		return
	}

	if recursive && event.Has(fsnotify.Create) {
		info, err := os.Stat(event.Name)
		if err == nil && info.IsDir() {
			log.Debug().Msgf("Adding recursive watch for newly created directory: %s", event.Name)
			// TODO: Implement dynamic addition of created directories in recursive mode.
		}
	}

	log.Info().Msgf("Detected %s event for: %s", eventStr, event.Name)

	ext := filepath.Ext(fileName)
	data := EventData{
		Path:     event.Name,
		Name:     fileName,
		Event:    eventStr,
		Ext:      ext,
		Dir:      filepath.Dir(event.Name),
		BaseName: strings.TrimSuffix(fileName, ext),
	}

	tmpl, err := template.New("command").Parse(commandTmpl)
	if err != nil {
		log.Error().Msgf("Error parsing command template: %v", err)
		return
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		log.Error().Msgf("Error executing command template: %v", err)
		return
	}

	cmdString := buf.String()
	log.Info().Msgf("Executing: %s", cmdString)

	cmdExec := exec.Command("sh", "-c", cmdString)
	cmdExec.Stdout = os.Stdout
	cmdExec.Stderr = os.Stderr
	cmdExec.Stdin = os.Stdin

	startTime := time.Now()
	err = cmdExec.Run()
	duration := time.Since(startTime)

	if err != nil {
		log.Error().Msgf("Command execution failed (duration: %s): %v", duration.Round(time.Millisecond), err)
	} else {
		log.Info().Msgf("Command executed successfully (duration: %s)", duration.Round(time.Millisecond))
	}
}
