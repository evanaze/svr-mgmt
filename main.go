package main

import (
	"context"
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cfg, command, err := parseArgs(args)
	if err != nil {
		return err
	}

	if command == "" || command == "help" {
		printUsage(os.Stdout)
		return nil
	}

	if cfg.keepAwake && cfg.sshTarget == "" {
		return errors.New("keep-awake requires an SSH target; set GLKVM_SSH_TARGET or pass -ssh-target")
	}

	if cfg.password == "" {
		return errors.New("missing password; set GLKVM_PASSWORD or pass -password")
	}

	client, err := newAPIClient(cfg)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	if err := client.login(ctx); err != nil {
		return err
	}

	// When -keep-awake is combined with a power-on command (e.g. "on"), the
	// machine may be off at first, so the SSH connection can only be made after
	// it has booted. Run the power action first, then retry Caffeine over SSH
	// until the server comes up.
	switch command {
	case "status":
		state, err := client.atxState(ctx)
		if err != nil {
			return err
		}
		printState(state)
	case "on":
		if err := client.powerOn(ctx); err != nil {
			return err
		}
	case "off":
		return client.setPower(ctx, "off")
	case "force-off", "off-hard":
		return client.setPower(ctx, "off_hard")
	case "reset", "reset-hard":
		return client.setPower(ctx, "reset_hard")
	case "click":
		return client.click(ctx, "power")
	case "click-long":
		return client.click(ctx, "power_long")
	case "reset-click":
		return client.click(ctx, "reset")
	default:
		return fmt.Errorf("unknown command %q", command)
	}

	if cfg.keepAwake {
		if err := keepAwake(ctx, cfg.sshTarget); err != nil {
			return fmt.Errorf("keep-awake: %w", err)
		}
		fmt.Println("caffeine enabled on server")
	}

	return nil
}
