package executor

import (
	"bytes"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"text/template"

	"github.com/rs/zerolog/log"

	"github.com/s0up4200/gowatchrun/internal/watcher"
)

func Execute(cfg watcher.Config, data *watcher.EventData) {
	var templateData interface{}
	if data != nil {
		templateData = data
	} else {
		templateData = struct{}{}
	}

	if cfg.ClearTerminal {
		var clearCmd *exec.Cmd
		if runtime.GOOS == "windows" {
			clearCmd = exec.Command("cmd", "/c", "cls")
		} else {
			clearCmd = exec.Command("clear")
		}
		clearCmd.Stdout = os.Stdout
		clearCmd.Stderr = os.Stderr
		if err := clearCmd.Run(); err != nil {
			log.Warn().Err(err).Msg("Failed to clear terminal")
		}
	}

	if data != nil {
		log.Debug().Msgf("Executing command for event: %s on %s", data.Event, data.Path)
	} else {
		log.Debug().Msg("Executing command for initial run (--run-on-start)")
	}

	tmpl, err := template.New("command").Parse(cfg.CommandTmpl)
	if err != nil {
		log.Error().Msgf("Error parsing command template: %v", err)
		return
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, templateData); err != nil {
		log.Error().Msgf("Error executing command template with data %+v: %v", templateData, err)
		return
	}

	cmdString := buf.String()
	log.Info().Msgf("Executing: %s", cmdString)

	// TODO: Consider adding process management here later (kill/queue/ignore)
	cmdExec := exec.Command("sh", "-c", cmdString)
	cmdExec.Stdin = os.Stdin

	outputBytes, err := cmdExec.CombinedOutput()
	outputString := strings.TrimSpace(string(outputBytes))

	if err != nil {
		logEntry := log.Error().Err(err)
		if outputString != "" {
			logEntry = logEntry.Str("output", outputString)
		}
		logEntry.Msg("Command execution failed")
	} else {
		logEntry := log.Info()
		if outputString != "" {
			logEntry = logEntry.Str("output", outputString)
		}
		logEntry.Msg("Command executed successfully")
	}
}
