# gowatchrun

A simple Go command-line tool to watch specified directories for file changes (creation, modification, deletion) that match given patterns. When a change is detected, it executes a user-defined command template, substituting placeholders with event details.

## Features

*   **Directory Watching:** Monitor one or more directories for file system events.
*   **Recursive Watching:** Optionally monitor directories and their subdirectories recursively.
*   **Pattern Matching:** Filter file events using glob patterns (e.g., `*.go`, `config/*.yaml`).
*   **Event Filtering:** Configure which specific file system events trigger the command (e.g., `write`, `create`, `remove`, or `all`).
*   **Command Templating:** Execute a user-provided command template using Go's `text/template` syntax.
*   **Placeholder Substitution:** Inject event details into the command template.

## Installation

Ensure you have Go installed. You can install `gowatchrun` using:

```bash
go install github.com/s0up4200/gowatchrun@latest 
```

This will place the `gowatchrun` binary in your `$GOPATH/bin` or `$HOME/go/bin` directory (ensure this is in your system's PATH).

## Usage

```bash
gowatchrun [flags]
```

### Flags

*   `-w, --watch <dir>`: Directory(ies) to watch. Can be specified multiple times. (Default: `.`)
*   `-p, --pattern <glob>`: Glob pattern(s) for files to watch. Can be specified multiple times. (Default: `*.*`)
*   `-e, --event <type>`: Event type(s) to trigger on. Valid types: `write`, `create`, `remove`, `rename`, `chmod`, `all`. Can be specified multiple times. (Default: `all`)
*   `-c, --command <template>`: Command template to execute. This flag is **required**.
*   `-r, --recursive`: Watch directories recursively. (Default: `false`)
*   `-x, --exclude <dir>`: Directory path(s) to exclude when watching recursively. Can be specified multiple times. (Default: none)
*   `-h, --help`: Display help information.

### Command Template Placeholders

The `--command` flag accepts a Go template string where the following placeholders can be used:

*   `{{.Path}}`: The full path to the file that triggered the event (e.g., `/home/user/project/src/main.go`).
*   `{{.Name}}`: The base name of the file (e.g., `main.go`).
*   `{{.Event}}`: The type of event detected as a string (e.g., `WRITE`, `CREATE`, `REMOVE`). Note: `fsnotify` might report multiple ops sometimes (e.g., `WRITE|CHMOD`); the first matched allowed event is used here.
*   `{{.Ext}}`: The file extension, including the dot (e.g., `.go`).
*   `{{.Dir}}`: The directory containing the file (e.g., `/home/user/project/src`).
*   `{{.BaseName}}`: The base name of the file without the extension (e.g., `main`).

## Examples

### General file watching

1.  **Run Go Tests on Change:** Watch for changes in `.go` files and run tests.
    ```bash
    gowatchrun -w . -r -p "*.go" -e write -c "go test ./..."
    ```

2.  **Run Go Tests, Excluding Vendor Directory:** Watch for changes in `.go` files recursively, but ignore the `vendor/` directory.
    ```bash
    gowatchrun -w . -r -p "*.go" -e write -x vendor -c "go test ./..."
    ```

### Seedbox & media automation examples

These examples demonstrate common automation tasks in a seedbox or media server environment. Ensure `gowatchrun` runs with appropriate permissions for the commands being executed.

1.  **Auto-unpack Archives:** Automatically unpack archives (`.zip`, `.rar`, `.7z`, etc.) when they appear in a completed downloads directory.
    ```bash
    # Watches for new archive files and attempts to unpack them.
    # Uses 'unar' if available (recommended), falling back to 'unzip' for .zip.
    # Logs activity to a file.
    gowatchrun \
      -w /srv/seedbox/downloads/complete \
      -p "*.zip" -p "*.rar" -p "*.7z" \
      -e create \
      -c "LOG_FILE=/var/log/gowatchrun_unpack.log; \
          echo \"[\$(date)] Detected archive: {{.Name}}\" >> \$LOG_FILE; \
          if command -v unar >/dev/null 2>&1; then \
            echo \"[\$(date)] Unarchiving {{.Name}} with unar...\" >> \$LOG_FILE; \
            unar -f -o {{.Dir}} {{.Path}} >> \$LOG_FILE 2>&1 && echo \"[\$(date)] Unarchived {{.Name}} successfully.\" >> \$LOG_FILE || echo \"[\$(date)] Failed to unarchive {{.Name}} with unar.\" >> \$LOG_FILE; \
          elif [[ '{{.Ext}}' == '.zip' ]] && command -v unzip >/dev/null 2>&1; then \
            echo \"[\$(date)] Unzipping {{.Name}} (excluding __MACOSX)...\" >> \$LOG_FILE; \
            unzip -o {{.Path}} -d {{.Dir}} -x '__MACOSX/*' >> \$LOG_FILE 2>&1 && echo \"[\$(date)] Unzipped {{.Name}} successfully.\" >> \$LOG_FILE || echo \"[\$(date)] Failed to unzip {{.Name}}.\" >> \$LOG_FILE; \
          else \
            echo \"[\$(date)] Cannot unpack {{.Name}}: unar/unzip not found or unsupported format.\" >> \$LOG_FILE; \
          fi"
    ```

2.  **Trigger Plex Library Scan:** Scan a specific Plex library section when a new media file appears.
    ```bash
    # Watches for new video files in the final media directory.
    # Triggers a Plex library scan for a specific section ID (replace 'YOUR_SECTION_ID').
    # Assumes Plex Media Scanner is in the PATH or uses the full path.
    # Ensure the user running gowatchrun has permission to execute the scanner.
    gowatchrun \
      -w /srv/media/movies \
      -w /srv/media/tv \
      -r \
      -p "*.mkv" -p "*.mp4" -p "*.avi" \
      -e create -e write \
      -c "echo 'New media {{.Name}} detected, triggering Plex scan...' && \
          /usr/lib/plexmediaserver/Plex\ Media\ Scanner --scan --refresh --section YOUR_SECTION_ID"
    ```

3.  **Trigger Jellyfin/Emby Library Scan (via API):** Scan all libraries when new media appears, using their respective APIs with `curl`.
    ```bash
    # Watches for new video files and triggers a Jellyfin/Emby library scan via API.
    # Replace 'YOUR_JELLYFIN_EMBY_URL' and 'YOUR_API_KEY'.
    # Ensure 'curl' is installed.
    # --- Jellyfin Example ---
    gowatchrun \
      -w /srv/media/movies -w /srv/media/tv -r \
      -p "*.mkv" -p "*.mp4" \
      -e create -e write \
      -c "echo 'New media {{.Name}} detected, triggering Jellyfin scan...' && \
          curl -X POST 'http://YOUR_JELLYFIN_URL:8096/Library/Refresh' \
          -H 'Authorization: MediaBrowser Token=\"YOUR_API_KEY\"' \
          -H 'Content-Length: 0'"
    ```

4.  **Move Completed Media & Associated Files:** Move video files *and* their corresponding subtitle files (`.srt`) from a completed download directory to a media library.
    ```bash
    # Watches for finished video files (.mkv, .mp4).
    # Moves the video AND any matching .srt file to the media library.
    gowatchrun \
      -w /srv/seedbox/downloads/complete \
      -p "*.mkv" -p "*.mp4" \
      -e write \
      -c "MEDIA_DEST=/srv/media/staging/; \
          SUB_FILE={{.Dir}}/{{.BaseName}}.srt; \
          echo 'Processing {{.Name}}...'; \
          mv {{.Path}} \$MEDIA_DEST && echo 'Moved {{.Name}} to \$MEDIA_DEST'; \
          if [ -f \"\$SUB_FILE\" ]; then \
            mv \"\$SUB_FILE\" \$MEDIA_DEST && echo 'Moved {{.BaseName}}.srt too.'; \
          fi"
    ```

5.  **Trigger rclone:** Automatically trigger an `rclone` sync when a new file appears in a specific directory (e.g., after processing).
    ```bash
    # Watches for any new file in the staging directory and syncs that directory to a cloud remote.
    gowatchrun \
      -w /srv/media/staging \
      -p "*.*" \
      -e create -e write \
      -c "echo 'Change detected in staging, syncing {{.Dir}}...' && \
          rclone copy --log-file=/var/log/rclone_sync.log {{.Dir}} myremote:backup/media/"
    ```

6.  **Send Notification via ntfy.sh:** Send a push notification using ntfy.sh when a specific download completes.
    ```bash
    # Watches for a specific large file finishing writing.
    # Sends a notification to a ntfy.sh topic (replace 'your_ntfy_topic').
    # Requires 'curl'.
    gowatchrun \
      -w /srv/seedbox/downloads/complete \
      -p "my_important_download.zip" \
      -e write \
      -c "echo 'Notifying about {{.Name}} completion...' && \
          curl -d 'Download finished: {{.Name}} in {{.Dir}}' ntfy.sh/your_ntfy_topic"
    ```

7.  **Clean Up .torrent Files:** Automatically remove `.torrent` files from a watch directory shortly after creation (assuming the client picks them up). (Slightly revised existing example)
    ```bash
    # Watches for new .torrent files and removes them after a 15-second delay.
    # Adjust delay based on your torrent client's pickup speed.
    gowatchrun \
      -w /srv/seedbox/watch \
      -p "*.torrent" \
      -e create \
      -c "echo 'Detected {{.Name}}, scheduling cleanup in 15s...' && \
          sleep 15 && \
          echo 'Cleaning up torrent file: {{.Path}}...' && \
          rm {{.Path}}"
    ```

8.  **Clean Up Auxiliary Files Recursively:** Remove common leftover files like `.nfo` or samples from your final media library.
    ```bash
    # Watches recursively in media directories for .nfo or sample files and removes them.
    # Use with caution! Double-check patterns.
    gowatchrun \
      -w /srv/media/movies \
      -w /srv/media/tv \
      -r \
      -p "*.nfo" -p "sample.*" -p "Sample.*" -p "*[Ss]ample*" \
      -e create -e write \
      -c "echo 'Detected unwanted file: {{.Name}} in {{.Dir}}. Removing...' && \
          rm {{.Path}}"
    ```

## Building from Source

Clone the repository and run:

```bash
go build -o gowatchrun .
```

## License

This project is licensed under the MIT License.
