package main

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// caffeineSchema is the gsettings schema id for the GNOME Caffeine extension.
const caffeineSchema = "org.gnome.shell.extensions.caffeine"

// caffeineGSettingsArgs builds the gsettings invocations executed on the
// managed server to toggle the GNOME Caffeine extension.
//
// The extension treats `cli-toggle` as a one-shot, non-persistent signal: it
// fires the toggle for the current session but its in-memory state resets to
// off every time GNOME Shell reloads the extension. To keep the server awake
// indefinitely, we therefore also persist `user-enabled` and `restore-state`,
// which the extension reads on startup to restore Caffeine as enabled.
func caffeineGSettingsArgs(enabled bool) [][]string {
	val := strconv.FormatBool(enabled)
	return [][]string{
		{"gsettings", "set", caffeineSchema, "cli-toggle", val},
		{"gsettings", "set", caffeineSchema, "user-enabled", val},
		{"gsettings", "set", caffeineSchema, "restore-state", val},
	}
}

func enableCaffeine(ctx context.Context, sshTarget string) error {
	for _, args := range caffeineGSettingsArgs(true) {
		full := append([]string{sshTarget}, args...)
		cmd := exec.CommandContext(ctx, "ssh", full...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("ssh %s %s: %w: %s", sshTarget, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// keepAwakePollInterval is how long keepAwake waits between SSH retry
// attempts. It is a variable so tests can shorten it.
var keepAwakePollInterval = 5 * time.Second

// Retry timer in case the machine is still booting
var keepAwakeConnectTimeout = 10 * time.Second

// keepAwake enables the GNOME Caffeine extension on the managed server over
// SSH.
func keepAwake(ctx context.Context, sshTarget string) error {
	pollInterval := keepAwakePollInterval

	for {
		attemptCtx, cancel := context.WithTimeout(ctx, keepAwakeConnectTimeout)
		err := enableCaffeine(attemptCtx, sshTarget)
		cancel()
		if err == nil {
			return nil
		}

		// Don't burn the whole timeout on a single doomed attempt. Wait for the
		// machine to finish booting before trying again.
		select {
		case <-ctx.Done():
			return fmt.Errorf("server did not become reachable over SSH in time: %w", err)
		case <-time.After(pollInterval):
			fmt.Println("server not yet reachable over SSH, retrying...")
		}
	}
}
