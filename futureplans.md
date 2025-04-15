# Future Plans for gowatchrun

Based on research into similar tools (like `mitranim/gow`) and common watcher functionalities, here are potential features to consider adding to `gowatchrun`:

1.  **[DONE] Terminal Clearing (`--clear`, `-C`)**
    *   Add an option to clear the terminal screen before each execution of the command template.
    *   Benefit: Cleaner output, especially for verbose commands.
    *   Example: `gowatchrun --clear -c "go test ./..."`

2.  **[DONE] Debouncing (`--delay <duration>`)**
    *   Introduce a delay mechanism (e.g., `--delay 500ms`).
    *   When a file change is detected, wait for the specified duration of inactivity before running the command. If more changes occur within the delay period, reset the timer.
    *   Benefit: Prevents rapid-fire command executions caused by editors saving multiple times or build processes generating intermediate files quickly.
    *   Example: `gowatchrun --delay 300ms -c "echo {{.Path}} changed"`

3.  **[DONE] Initial Run (`--run-on-start`)**
    *   Add a flag to execute the command template once immediately when `gowatchrun` starts, before any file changes are detected.
    *   Benefit: Useful for initial setup, running tests on startup, or ensuring the command works correctly from the beginning.
    *   Example: `gowatchrun --run-on-start -c "go build ."`

4.  Process Management (`--on-busy kill|queue|ignore`)**
    *   Define behavior when a new file change occurs while the previously triggered command is still running.
    *   Options:
        *   `kill` (Default?): Terminate the ongoing command and start the new one. Common behavior for build/run watchers.
        *   `queue`: Wait for the current command to finish before starting the next one.
        *   `ignore`: Ignore new file changes detected while a command is executing.
    *   Benefit: Provides control over how overlapping command executions are handled.

5.  **Hotkeys (`--hotkeys`)**
    *   *Advanced Feature:* Add optional support for interactive hotkeys in the terminal.
    *   Examples: Ctrl+R to manually trigger a run/restart, Ctrl+C to kill the currently running command process.
    *   Benefit: Offers interactive control during development sessions.
    *   Complexity: Requires careful handling of terminal raw mode, likely only suitable for interactive TTY sessions, and may conflict with other raw-mode tools or multiple `gowatchrun` instances.

6.  **Configuration File Support**
    *   Allow specifying configuration options (watch directories, patterns, exclusions, command, flags like `--delay`, `--clear`, etc.) in a configuration file (e.g., `.gowatchrun.yaml`, `gowatchrun.toml`, `gowatchrun.json`).
    *   `gowatchrun` could look for this file in the current directory or allow specifying a path via a flag (e.g., `--config myconfig.yaml`).
    *   Benefit: Simplifies usage for complex configurations and makes setups more shareable and repeatable compared to long command-line strings.
