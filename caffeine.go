package main

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// caffeineGSettingsArgs builds the gsettings invocation executed on the
// managed server to toggle the GNOME Caffeine extension.
func caffeineGSettingsArgs(enabled bool) []string {
	return []string{
		"gsettings",
		"set",
		"org.gnome.shell.extensions.caffeine",
		"cli-toggle",
		strconv.FormatBool(enabled),
	}
}

func enableCaffeine(ctx context.Context, sshTarget string) error {
	args := append([]string{sshTarget}, caffeineGSettingsArgs(true)...)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh %s: %w: %s", sshTarget, err, strings.TrimSpace(string(out)))
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
