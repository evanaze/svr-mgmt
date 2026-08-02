package main

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// caffeineGSettingsArgs builds the gsettings invocation executed on the
// managed server to toggle the GNOME Caffeine extension.
func caffeineGSettingsArgs(enabled bool) []string {
	return []string{
		"set",
		"org.gnome.shell.extensions.caffeine",
		"cli-toggle",
		strconv.FormatBool(enabled),
	}
}

// enableCaffeine enables Caffeine on the managed server by running the
// gsettings command over SSH. sshTarget is a single configurable host string
// (e.g. "user@server" or an SSH config alias); the devices are reached over
// tailscale, so whatever target you would pass to ssh works here.
func enableCaffeine(ctx context.Context, sshTarget string) error {
	args := append([]string{sshTarget}, caffeineGSettingsArgs(true)...)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh %s: %w: %s", sshTarget, err, strings.TrimSpace(string(out)))
	}
	return nil
}
