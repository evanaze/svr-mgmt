package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

var caffeineSchemaCandidates = func() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".local/share/gnome-shell/extensions/caffeine@patapon.info/schemas"),
		"/run/current-system/sw/share/gnome-shell/extensions/caffeine@patapon.info/schemas",
		"/usr/share/gnome-shell/extensions/caffeine@patapon.info/schemas",
		"/usr/local/share/gnome-shell/extensions/caffeine@patapon.info/schemas",
	}
}

func defaultCaffeineSchemaDir() string {
	candidates := caffeineSchemaCandidates()
	for _, dir := range candidates {
		if _, err := os.Stat(filepath.Join(dir, "gschemas.compiled")); err == nil {
			return dir
		}
	}
	return candidates[0]
}

// caffeineGSettingsArgs builds the gsettings invocation for toggling Caffeine.
func caffeineGSettingsArgs(schemaDir string, enabled bool) []string {
	return []string{
		"--schemadir", schemaDir,
		"set",
		"org.gnome.shell.extensions.caffeine",
		"cli-toggle",
		strconv.FormatBool(enabled),
	}
}

func enableCaffeine(ctx context.Context, schemaDir string) error {
	args := caffeineGSettingsArgs(schemaDir, true)
	cmd := exec.CommandContext(ctx, "gsettings", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gsettings %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func boolQuery(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
