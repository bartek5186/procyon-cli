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
go run . module create invoice
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

## Module Generator

Run this command inside a generated Procyon project:

```bash
procyon-cli module create invoice
```

The module name must be `snake_case` and start with a letter:

```bash
procyon-cli module create order_item
```

The generator creates model, store, service, controller, MySQL migration, and PostgreSQL migration files. It also wires the module into `AppStore`, `AppService`, `application`, routes, auto-migration, and default Casbin policies.

Existing generated files are not overwritten by default. Use `--force` to overwrite generated module files:

```bash
procyon-cli module create invoice --force
```
