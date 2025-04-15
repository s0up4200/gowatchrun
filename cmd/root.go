package cmd

import (
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/s0up4200/gowatchrun/internal/executor"
	"github.com/s0up4200/gowatchrun/internal/watcher"
)

var (
	watchDirs     []string
	excludeDirs   []string
	patterns      []string
	eventTypes    []string
	commandTmpl   string
	recursive     bool
	logLevel      string
	delayStr      string
	clearTerminal bool
)

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
		debounceDelay, parseErr := time.ParseDuration(delayStr)
		if parseErr != nil {
			log.Warn().Msgf("Invalid --delay duration '%s', defaulting to 0s. Error: %v", delayStr, parseErr)
			debounceDelay = 0
		} else if debounceDelay < 0 {
			log.Warn().Msgf("--delay duration '%s' is negative, defaulting to 0s.", delayStr)
			debounceDelay = 0
		}

		config := watcher.Config{
			WatchDirs:     watchDirs,
			ExcludeDirs:   excludeDirs,
			Patterns:      patterns,
			EventTypes:    eventTypes,
			CommandTmpl:   commandTmpl,
			Recursive:     recursive,
			DebounceDelay: debounceDelay,
			ClearTerminal: clearTerminal,
		}

		log.Info().Msg("Starting gowatchrun...")
		err := watcher.Run(config, executor.Execute)
		if err != nil {
			log.Error().Err(err).Msg("Watcher exited with error")
			os.Exit(1)
		}
		log.Info().Msg("gowatchrun finished.")
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.Flags().StringSliceVarP(&watchDirs, "watch", "w", []string{"."}, "Directory(ies) to watch. Can be specified multiple times.")
	rootCmd.Flags().StringSliceVarP(&excludeDirs, "exclude", "x", []string{}, "Directory path(s) to exclude when watching recursively. Can be specified multiple times.")
	rootCmd.Flags().StringSliceVarP(&patterns, "pattern", "p", []string{"*.*"}, "Glob pattern(s) for files to watch. Can be specified multiple times.")
	rootCmd.Flags().StringSliceVarP(&eventTypes, "event", "e", []string{"all"}, "Event type(s) to trigger on. Valid types: write, create, remove, rename, chmod, open, read, closewrite, closeread, all. Can be specified multiple times.")
	rootCmd.Flags().StringVarP(&commandTmpl, "command", "c", "", "Command template to execute. This flag is required.")
	rootCmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "Watch directories recursively.")
	rootCmd.Flags().StringVar(&logLevel, "log-level", "info", "Set the logging level (e.g., debug, info, warn, error).")
	rootCmd.Flags().StringVar(&delayStr, "delay", "0s", "Debounce delay before executing the command after a change (e.g., 300ms, 1s). Waits for a period of inactivity.")
	rootCmd.Flags().BoolVarP(&clearTerminal, "clear", "C", false, "Clear terminal before executing command.")

	if err := rootCmd.MarkFlagRequired("command"); err != nil {
		log.Fatal().Err(err).Msg("Failed to mark 'command' flag as required")
	}
}
