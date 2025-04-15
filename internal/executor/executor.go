package executor

import (
	"bytes"
	"os"
	"os/exec"
	"text/template"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/s0up4200/gowatchrun/internal/watcher"
)

func Execute(commandTmpl string, data *watcher.EventData) {
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

	// TODO: Consider adding process management here later (kill/queue/ignore)
	cmdExec := exec.Command("sh", "-c", cmdString)
	cmdExec.Stdout = os.Stdout
	cmdExec.Stderr = os.Stderr
	cmdExec.Stdin = os.Stdin

	startTime := time.Now()
	err = cmdExec.Run()
	duration := time.Since(startTime)

	if err != nil {
		log.Error().
			Str("command", cmdString).
			Str("event_path", data.Path).
			Str("event_type", data.Event).
			Dur("duration", duration.Round(time.Millisecond)).
			Err(err).
			Msg("Command execution failed")
	} else {
		log.Trace().
			Str("command", cmdString).
			Dur("duration", duration.Round(time.Millisecond)).
			Msg("Command executed successfully")
	}
}
