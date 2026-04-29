# procyon-cli

CLI helper for working with Procyon projects.

This tool is intended to live as a separate repository from the Procyon backend template. Its job is to initialize new projects from the template and, over time, provide project maintenance commands such as module generation.

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
  --auth kratos-casbin \
  --out ../demo-api
```

The `init` command:

- finds the Procyon template root,
- copies the template into the output directory,
- rewrites module name, app name, config files, and Docker names,
- optionally removes Docker files or the example `hello` feature,
- prepares the generated project for normal `go run . -migrate=true` startup.

## Planned Scope

This CLI is also the right place for future project-level commands, for example:

```bash
procyon-cli module create invoice
```

Module generation should live here rather than inside the backend template, so the generated application does not carry scaffolding tooling as runtime source code.
