package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// systemdInhibitArgs builds the command executed on the managed server to keep
// it from sleeping.
//
// systemd-inhibit holds its lock only for the lifetime of the child process it
// runs, so we run a long-lived `sleep infinity` under it. To keep the lock
// alive after the SSH session closes we also detach the whole thing in the
// background with nohup, redirecting stdio so nothing holds the SSH channel
// open. The lock persists until the process is killed or the machine reboots.
func systemdInhibitArgs() []string {
	return []string{
		"nohup", "systemd-inhibit",
		"--what=sleep",
		"--who=svr-mgmt",
		"--why=Keep managed server awake",
		"sleep", "infinity",
		">/dev/null", "2>&1", "&",
	}
}

func enableWakefulness(ctx context.Context, sshTarget string) error {
	args := systemdInhibitArgs()
	full := append([]string{sshTarget}, args...)
	cmd := exec.CommandContext(ctx, "ssh", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh %s %s: %w: %s", sshTarget, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// keepAwakePollInterval is how long keepAwake waits between SSH retry
// attempts. It is a variable so tests can shorten it.
var keepAwakePollInterval = 5 * time.Second

// Retry timer in case the machine is still booting
var keepAwakeConnectTimeout = 10 * time.Second

// keepAwake holds a systemd sleep-inhibit lock on the managed server so it
// does not suspend, retrying over SSH while the machine finishes booting.
func keepAwake(ctx context.Context, sshTarget string) error {
	pollInterval := keepAwakePollInterval

	for {
		attemptCtx, cancel := context.WithTimeout(ctx, keepAwakeConnectTimeout)
		err := enableWakefulness(attemptCtx, sshTarget)
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
