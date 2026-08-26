// fileenv reads environment variables with the "_FILE" suffix (configurable
// via FILEENV_SUFFIX), loads the content of the referenced file, sets a new
// environment variable without the suffix, and then executes the
// program passed as an argument via exec() (replacing
// the current process, PID is preserved -> signals work
// correctly, no additional init process needed).
//
// Example:
//
//	DB_PASSWORD_FILE=/run/secrets/db_password fileenv -- myapp --serve
//
// -> reads /run/secrets/db_password, sets DB_PASSWORD=<content>,
//
//	then exec's "myapp --serve" with the complete environment.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

const defaultFileSuffix = "_FILE"

// Names of fileenv's own configuration variables. These are never
// themselves treated as candidates for suffix-based file resolution.
const (
	suffixEnvVar = "FILEENV_SUFFIX"
)

// version is set during the release build via -ldflags "-X main.version=...".
var version = "dev"

// config holds fileenv's own settings, derived from its FILEENV_* environment variables.
type config struct {
	suffix string
}

// main is the entry point of fileenv. It checks for the "--version" flag,
// loads the configuration, resolves the "_FILE" environment variables,
// and exec's the target command.
func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("fileenv", version)
		return
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fileenv:", err)
		os.Exit(1)
	}
}

// run is the main logic of fileenv. It loads the configuration, resolves
// the "_FILE" environment variables, and exec's the target command.
func run() error {
	args := os.Args[1:]

	// Allow an optional "--" between fileenv's own options (currently none)
	// and the command to execute, but also work without "--".
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}

	if len(args) == 0 {
		return fmt.Errorf("no command given (usage: fileenv [--] <cmd> [args...])")
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if err := resolveFileEnvVars(cfg); err != nil {
		return err
	}

	binPath, err := exec.LookPath(args[0])
	if err != nil {
		return fmt.Errorf("cannot find %q: %w", args[0], err)
	}

	// syscall.Exec fully replaces the current process (like exec(3) in C).
	// This preserves PID 1 (if the binary runs as PID 1), and
	// signals (e.g. SIGTERM from "docker stop") go directly to the target process.
	env := os.Environ()
	if err := syscall.Exec(binPath, args, env); err != nil {
		return fmt.Errorf("exec of %q failed: %w", args[0], err)
	}
	return nil // never reached
}

// loadConfig reads fileenv's own FILEENV_* environment variables.
func loadConfig() (config, error) {
	cfg := config{suffix: defaultFileSuffix}

	// Allow the suffix to be overridden via FILEENV_SUFFIX.
	if v, ok := os.LookupEnv(suffixEnvVar); ok {
		if v == "" {
			return config{}, fmt.Errorf("%s must not be empty", suffixEnvVar)
		}
		cfg.suffix = v
	}

	return cfg, nil
}

// isControlVar reports whether key is one of fileenv's own
// configuration variables, which are never resolved themselves.
func isControlVar(key string) bool {
	switch key {
	case suffixEnvVar:
		return true
	default:
		return false
	}
}

// resolveFileEnvVars scans the current environment for variables with the configured suffix,
// reads the referenced files, and sets new environment variables without the suffix.
func resolveFileEnvVars(cfg config) error {
	for _, kv := range os.Environ() {
		key, val, found := strings.Cut(kv, "=")

		// Skip variables that don't have the configured suffix or are fileenv's own control variables
		if !found || isControlVar(key) || !strings.HasSuffix(key, cfg.suffix) {
			continue
		}

		targetKey := strings.TrimSuffix(key, cfg.suffix)
		if targetKey == "" {
			continue
		}

		// Read the content of the file specified by the environment variable
		data, err := os.ReadFile(val)
		if err != nil {
			return fmt.Errorf("cannot read file for %s (%s): %w", key, val, err)
		}

		// Remove trailing newline, as most secret files have
		// (e.g. created via "echo" instead of "printf").
		value := strings.TrimRight(string(data), "\r\n")

		// Set the new environment variable without the suffix
		if err := os.Setenv(targetKey, value); err != nil {
			return fmt.Errorf("cannot set %s: %w", targetKey, err)
		}
	}
	return nil
}
