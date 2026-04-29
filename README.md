# procyon-cli

CLI helper for working with Procyon projects.

This tool is intended to live as a separate repository from the Procyon backend template. Its job is to initialize new projects from the template and, over time, provide project maintenance commands such as module generation.

## Installation

The simplest installation method is `go install`.

Requirements:

- Go
- Git

Install the latest version:

```bash
go install github.com/bartek5186/procyon-cli@latest
```

Go installs the binary into `$(go env GOPATH)/bin`, which is usually `~/go/bin`.
Make sure that directory is available in your `PATH`:

```bash
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

After that, the CLI can be used from any directory:

```bash
procyon-cli init
```

## Current Scope

Implemented:

```bash
go run . init
```

Non-interactive example:

```bash
go run . init \
  --name demo-api \
  --module github.com/acme/demo-api \
  --db postgres \
  --out ../demo-api
```

The `init` command:

- downloads the Procyon template from `https://github.com/bartek5186/procyon`,
- copies the template into the output directory,
- rewrites module name, app name, config files, and Docker names,
- keeps Kratos, Casbin/RBAC, and admin configuration enabled,
- optionally removes Docker files or the example `hello` feature,
- prepares the generated project for normal `go run . -migrate=true` startup.

## Planned Scope

This CLI is also the right place for future project-level commands, for example:

```bash
procyon-cli module create invoice
```

Module generation should live here rather than inside the backend template, so the generated application does not carry scaffolding tooling as runtime source code.
