// fileenv reads environment variables with the "_FILE" suffix,
// loads the content of the referenced file, sets a new
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

const fileSuffix = "_FILE"

// version is set during the release build via -ldflags "-X main.version=...".
var version = "dev"

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

	if err := resolveFileEnvVars(); err != nil {
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

func resolveFileEnvVars() error {
	for _, kv := range os.Environ() {
		key, val, found := strings.Cut(kv, "=")
		if !found || !strings.HasSuffix(key, fileSuffix) {
			continue
		}

		targetKey := strings.TrimSuffix(key, fileSuffix)
		if targetKey == "" {
			continue
		}

		data, err := os.ReadFile(val)
		if err != nil {
			return fmt.Errorf("cannot read file for %s (%s): %w", key, val, err)
		}

		// Remove trailing newline, as most secret files have
		// (e.g. created via "echo" instead of "printf").
		value := strings.TrimRight(string(data), "\r\n")

		if err := os.Setenv(targetKey, value); err != nil {
			return fmt.Errorf("cannot set %s: %w", targetKey, err)
		}
	}
	return nil
}
