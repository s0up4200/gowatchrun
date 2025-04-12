package cmd

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

var (
	watchDirs   []string
	patterns    []string
	eventTypes  []string
	commandTmpl string
	recursive   bool
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

	if err := rootCmd.MarkFlagRequired("command"); err != nil {
		log.Fatalf("Failed to mark 'command' flag as required: %v", err)
	}
}

func runWatcher() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("Failed to create watcher: %v", err)
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
				log.Printf("Watcher error: %v", err)
			}
		}
	}()

	log.Printf("Starting watcher for directories: %v", watchDirs)
	if recursive {
		log.Println("Recursive mode enabled.")
	}
	log.Printf("Watching for patterns: %v", patterns)
	log.Printf("Triggering on events: %v", eventTypes)
	log.Printf("Executing command template: %s", commandTmpl)

	for _, dir := range watchDirs {
		if recursive {
			err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					log.Printf("Error accessing path %q: %v", path, err)
					return err
				}
				if info.IsDir() {
					log.Printf("Adding recursive watch for: %s", path)
					if watchErr := watcher.Add(path); watchErr != nil {
						log.Printf("Failed to add recursive watch for %s: %v", path, watchErr)
					}
				}
				return nil
			})
			if err != nil {
				log.Printf("Error walking the path %q: %v", dir, err)
			}
		} else {
			log.Printf("Adding watch for: %s", dir)
			if err = watcher.Add(dir); err != nil {
				log.Printf("Failed to add watch for %s: %v", dir, err)
			}
		}
	}

	<-done
	log.Println("Watcher stopped.")
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
			log.Printf("Warning: Unknown event type '%s' ignored.", t)
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
			log.Printf("Error matching pattern '%s' with file '%s': %v", pattern, fileName, err)
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
			log.Printf("Adding recursive watch for newly created directory: %s", event.Name)
			// TODO: Implement dynamic addition of created directories in recursive mode.
		}
	}

	log.Printf("Detected %s event for: %s", eventStr, event.Name)

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
		log.Printf("Error parsing command template: %v", err)
		return
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		log.Printf("Error executing command template: %v", err)
		return
	}

	cmdString := buf.String()
	log.Printf("Executing: %s", cmdString)

	cmdExec := exec.Command("sh", "-c", cmdString)
	cmdExec.Stdout = os.Stdout
	cmdExec.Stderr = os.Stderr
	cmdExec.Stdin = os.Stdin

	startTime := time.Now()
	err = cmdExec.Run()
	duration := time.Since(startTime)

	if err != nil {
		log.Printf("Command execution failed (duration: %s): %v", duration.Round(time.Millisecond), err)
	} else {
		log.Printf("Command executed successfully (duration: %s)", duration.Round(time.Millisecond))
	}
}
