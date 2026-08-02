package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://ai-kvm.spitz-pickerel.ts.net"
const defaultSshHost = "earth"

type config struct {
	baseURL            string
	username           string
	password           string
	insecureSkipVerify bool
	timeout            time.Duration
	debug              bool
	keepAwake          bool
	sshTarget          string
}

func parseArgs(args []string) (config, string, error) {
	cfg := config{
		baseURL:            envDefault("GLKVM_URL", defaultBaseURL),
		username:           envDefault("GLKVM_USER", "admin"),
		password:           os.Getenv("GLKVM_PASSWORD"),
		insecureSkipVerify: envBoolDefault("GLKVM_INSECURE_SKIP_VERIFY", true),
		timeout:            30 * time.Second,
		debug:              envBoolDefault("GLKVM_DEBUG", false),
		keepAwake:          envBoolDefault("GLKVM_KEEP_AWAKE", false),
		sshTarget:          envDefault("GLKVM_SSH_TARGET", defaultSshHost),
	}

	fs := flag.NewFlagSet("svr-mgmt", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.baseURL, "url", cfg.baseURL, "GLKVM base URL, for example https://ai-kvm")
	fs.StringVar(&cfg.username, "user", cfg.username, "GLKVM username")
	fs.StringVar(&cfg.password, "password", cfg.password, "GLKVM password")
	fs.BoolVar(&cfg.insecureSkipVerify, "insecure-skip-verify", cfg.insecureSkipVerify, "skip TLS certificate verification for self-signed KVM certs")
	fs.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "request timeout")
	fs.BoolVar(&cfg.debug, "debug", cfg.debug, "log GLKVM API requests and responses to stderr")
	fs.BoolVar(&cfg.keepAwake, "keep-awake", cfg.keepAwake, "enable the GNOME Caffeine extension on the managed server so it does not sleep while in use")
	fs.BoolVar(&cfg.keepAwake, "ka", cfg.keepAwake, "short alias for -keep-awake")
	fs.StringVar(&cfg.sshTarget, "ssh-target", cfg.sshTarget, "SSH target of the managed server to enable Caffeine on, e.g. user@server or an SSH config alias")

	if err := fs.Parse(args); err != nil {
		return cfg, "", err
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		return cfg, "", nil
	}
	if len(remaining) > 1 {
		return cfg, "", fmt.Errorf("expected one command, got %d: %s", len(remaining), strings.Join(remaining, " "))
	}

	return cfg, remaining[0], nil
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: svr-mgmt [flags] <command>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  status       Show ATX power/HDD LED state")
	fmt.Fprintln(w, "  on           Power on if currently off")
	fmt.Fprintln(w, "  off          Soft power off via ACPI power button")
	fmt.Fprintln(w, "  force-off    Long-press power button")
	fmt.Fprintln(w, "  reset        Hardware reset")
	fmt.Fprintln(w, "  click        Short press power button")
	fmt.Fprintln(w, "  click-long   Long press power button")
	fmt.Fprintln(w, "  reset-click  Short press reset button")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags can also be provided with env vars:")
	fmt.Fprintln(w, "  -url                   GLKVM_URL, defaults to https://ai-kvm")
	fmt.Fprintln(w, "  -user                  GLKVM_USER, defaults to admin")
	fmt.Fprintln(w, "  -password              GLKVM_PASSWORD, required")
	fmt.Fprintln(w, "  -insecure-skip-verify  GLKVM_INSECURE_SKIP_VERIFY, defaults to true")
	fmt.Fprintln(w, "  -timeout               defaults to 30s")
	fmt.Fprintln(w, "  -debug                 GLKVM_DEBUG, log GLKVM API requests/responses to stderr")
	fmt.Fprintln(w, "  -keep-awake / -ka      GLKVM_KEEP_AWAKE, enable GNOME Caffeine on the managed server so it does not sleep")
	fmt.Fprintln(w, "  -ssh-target             GLKVM_SSH_TARGET, SSH target of the managed server to run Caffeine on (required with -keep-awake)")
}

func envDefault(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envBoolDefault(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
