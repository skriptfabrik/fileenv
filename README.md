# fileenv

A tiny, dependency-free, statically linked binary that resolves Docker/Kubernetes-style
`*_FILE` secret references into plain environment variables and then `exec`s your
actual application — designed to run in `scratch` or `distroless` images that have
no shell.

```
DB_PASSWORD_FILE=/run/secrets/db_password
        │
        ▼
fileenv reads the file
        │
        ▼
DB_PASSWORD=<file contents>  →  exec("myapp", ...)
```

## Why

Docker Swarm and Kubernetes both mount secrets as files (typically under
`/run/secrets`), but many applications only know how to read configuration from
environment variables. The common workaround is a shell-based `docker-entrypoint.sh`
with a `file_env()` function (used by the official `postgres`, `mysql`, `wordpress`
images, among others). That approach requires a shell — which distroless/scratch
images intentionally don't have.

`fileenv` implements the same idea as a single static binary with no runtime
dependencies, so it also works in minimal, shell-less images.

## Features

- Single static binary, no libc dependency (`CGO_ENABLED=0`)
- No shell, no package manager, no attack surface beyond the binary itself
- Reads any environment variable ending in `_FILE`, resolves the referenced file,
  and sets the same variable name without the suffix
- Replaces itself with the target process via `exec` (not fork) — the target
  process keeps PID 1, so signals like `SIGTERM` from `docker stop` /
  `docker service update` reach it directly, with no zombie-process risk and no
  need for a separate init system (e.g. tini)
- Trims a trailing newline from file contents (common with `echo`-generated secrets)
- ~2 MB binary, builds in seconds, multi-arch images published on every release

## How it works

1. `fileenv` iterates over its own environment.
2. For every variable `FOO_FILE=/path/to/file`, it reads `/path/to/file` and sets
   `FOO=<contents of the file>` (trailing `\n`/`\r\n` stripped).
3. It looks up the command given as arguments (via `PATH` if not an absolute path).
4. It calls `exec()`, replacing itself with that command — the target process
   inherits the now-fully-populated environment.

If a file cannot be read, `fileenv` exits with a non-zero status and an error
message on stderr; it never silently continues with a missing secret.

## Installation

### As a Docker base-image ingredient (recommended)

Copy the binary out of the published image into your own multi-stage build —
this is the pattern used by tools like `secrets-init` or `tini-static`:

```dockerfile
FROM ghcr.io/skriptfabrik/fileenv:1 AS fileenv

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=fileenv /usr/local/bin/fileenv /usr/local/bin/fileenv
COPY myapp /usr/local/bin/myapp

ENTRYPOINT ["/usr/local/bin/fileenv"]
CMD ["--", "/usr/local/bin/myapp"]
```

Images are published to both:

- GitHub Container Registry: `ghcr.io/skriptfabrik/fileenv`
- Docker Hub: `skriptfabrik/fileenv`

Available tags: `<major>`, `<major>.<minor>`, `<major>.<minor>.<patch>`, and
`latest` (tracks the most recent non-prerelease release). Pin to at least the
major tag (`:1`) in production.

### Prebuilt binaries

Every [release](https://github.com/skriptfabrik/fileenv/releases) includes
prebuilt binaries for the most common platforms, plus a `checksums.txt`:

| OS      | Architecture      | Archive                                  |
| ------- | ----------------- | ---------------------------------------- |
| Linux   | amd64              | `fileenv_<version>_linux_amd64.tar.gz`  |
| Linux   | arm64              | `fileenv_<version>_linux_arm64.tar.gz`  |
| Linux   | armv7              | `fileenv_<version>_linux_armv7.tar.gz`  |
| macOS   | amd64              | `fileenv_<version>_darwin_amd64.tar.gz` |
| macOS   | arm64 (Apple Sil.) | `fileenv_<version>_darwin_arm64.tar.gz` |
| Windows | amd64              | `fileenv_<version>_windows_amd64.zip`   |

```sh
curl -LO https://github.com/skriptfabrik/fileenv/releases/latest/download/fileenv_<version>_linux_amd64.tar.gz
curl -LO https://github.com/skriptfabrik/fileenv/releases/latest/download/checksums.txt
sha256sum --ignore-missing -c checksums.txt
tar -xzf fileenv_<version>_linux_amd64.tar.gz
```

### Via `go install`

```sh
go install github.com/skriptfabrik/fileenv@latest
```

## Usage

```sh
fileenv [--] <command> [args...]
```

The `--` is optional and only improves readability in Dockerfiles / Compose
files; `fileenv myapp --serve` and `fileenv -- myapp --serve` behave identically.

```sh
fileenv --version   # prints the build version and exits
```

### Docker Compose

```yaml
services:
  app:
    image: myapp
    entrypoint: ["fileenv", "--"]
    command: ["node", "server.js"]
    environment:
      DB_PASSWORD_FILE: /run/secrets/db_password
    volumes:
      - ./secrets/db_password:/run/secrets/db_password:ro
```

### Docker Swarm secrets

```yaml
services:
  app:
    image: myapp
    entrypoint: ["fileenv", "--"]
    command: ["node", "index.js"]
    secrets:
      - db_password
    environment:
      DB_PASSWORD_FILE: /run/secrets/db_password

secrets:
  db_password:
    external: true
```

Swarm mounts secrets at `/run/secrets/<secret_name>`; point the `_FILE` variable
at that path explicitly (the secret name and the desired environment variable
name don't have to match).

### Kubernetes

```yaml
env:
  - name: DB_PASSWORD_FILE
    value: /etc/secrets/db-password
volumeMounts:
  - name: db-password
    mountPath: /etc/secrets
    readOnly: true
```

## Configuration

fileenv itself is configured via a small number of `FILEENV_*` environment
variables. These are never treated as `_FILE` candidates themselves, and are
left in the environment when the target process is exec'd.

| Variable          | Purpose                                                            |
| ----------------- | ------------------------------------------------------------------ |
| `FILEENV_SUFFIX`  | Suffix to look for instead of the default `_FILE` (e.g. `__FILE`). |

## Behavior details / edge cases

- The suffix is `_FILE` by default; override it with `FILEENV_SUFFIX` (see
  [Configuration](#configuration)).
- If both `FOO` and `FOO_FILE` are set, `FOO_FILE` wins and overwrites `FOO`.
- Empty variable names (i.e. a variable literally named `_FILE`) are ignored.
- File contents are used as-is except for a trimmed trailing `\r\n`/`\n` — no
  further parsing, quoting, or templating is applied.
- `fileenv` never logs variable names or values.

## Security considerations

- No shell, no dynamic linking, no third-party Go dependencies (stdlib only) —
  minimal supply-chain and attack surface.
- Runs as whatever user the container is configured with; use the `:nonroot`
  distroless variant (as shown above) unless you have a specific reason not to.
- Secrets only ever exist in the target process's environment, not on disk
  beyond the original secret file, and not in any log output.

## Development

Requirements: Go 1.27+, Docker with Buildx (for multi-arch image builds).

```sh
git clone https://github.com/skriptfabrik/fileenv.git
cd fileenv
go build -o fileenv .
go vet ./...
./fileenv --version
```

Local multi-arch image build (no push):

```sh
docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7 -t fileenv:dev .
```

### Project layout

```
.
├── main.go                      # the entire implementation
├── go.mod
├── Dockerfile                   # multi-arch, distroless runtime
└── .github/workflows/
    ├── ci.yml                   # build & vet on push/PR
    └── release.yml              # binaries + Docker images on GitHub Release
```

## Releasing

Releases are fully automated via GitHub Actions and triggered by publishing a
GitHub Release with a semantic-versioning tag (`vX.Y.Z`, e.g. `v1.2.0`):

1. Create and publish a release for tag `v1.2.0` on GitHub (this can be a new
   tag or an existing one).
2. `.github/workflows/release.yml` then, in parallel:
   - builds static binaries for Linux (amd64, arm64, armv7), macOS (amd64,
     arm64) and Windows (amd64), packages them as `.tar.gz`/`.zip`, generates
     `checksums.txt`, and uploads everything to the release, and
   - builds a multi-arch (`linux/amd64`, `linux/arm64`, `linux/arm/v7`)
     Docker image and pushes it to both `ghcr.io/skriptfabrik/fileenv` and
     `skriptfabrik/fileenv` on Docker Hub, tagged `1.2.0`, `1.2`, `1`, and
     `latest`.

Required repository secrets for the Docker Hub push (GHCR authenticates with
the built-in `GITHUB_TOKEN`, no extra secret needed):

| Secret               | Description                                   |
| -------------------- | ---------------------------------------------- |
| `DOCKERHUB_USERNAME` | Docker Hub username or org                     |
| `DOCKERHUB_TOKEN`    | Docker Hub access token (Account Settings → Security → New Access Token) |

## Contributing

Issues and pull requests are welcome. Given the intentionally small scope of
this tool, please open an issue to discuss larger changes before submitting a
PR.

## License

[MIT](LICENSE)
