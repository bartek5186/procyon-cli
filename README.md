# procyon-cli

CLI helper for working with Procyon projects.

This tool is intended to live as a separate repository from the Procyon backend template. Its job is to initialize new projects from the template and, over time, provide project maintenance commands such as module generation.

## Installation

Install the latest version from npm (Node.js 18 or newer):

```bash
npm install --global procyon-cli
```

The npm package downloads a verified native binary from the matching GitHub
Release, so Go is not required. Linux, macOS and Windows are supported on x64
and arm64.

Alternatively, install directly with Go:

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
procyon-cli
```

`procyon-cli self-update` preserves the installation method: npm installations
are updated with npm, while Go installations use `go install`.

## Interactive Dashboard

Running `procyon-cli` without arguments opens a context-aware interactive menu.
It inspects both `.procyon.json` and the `github.com/bartek5186/procyon-core`
dependency in `go.mod`.

Inside a Procyon project, the dashboard shows the project module, template and
Core versions, CLI version, installed plugin count and updates available in the
published module registry. From the menu you can add a published plugin, update
an outdated published plugin, create and install a minimal local plugin for
development, list installed plugins, or update Procyon Core. Local development
plugins are not compared with published releases. Legacy projects without
`.procyon.json` are still recognized from their Core dependency.

The published-plugin picker is searchable with `/`. Provider choices are not
shown in the catalog; after selecting a plugin, the CLI loads its manifest and
opens a separate multi-select only when that plugin defines providers.

Inside an empty directory, the default action initializes a new project in the
current directory. A non-empty directory that is not a Procyon project is never
overwritten; the menu only offers to create a project in a new subdirectory.

## Postman Collection

Generate a collection for the current Procyon project with:

```bash
procyon-cli postman generate
```

The command scans application routes, installed plugin routes, controller Go
documentation and module-owned `docs/postman/*.json` examples. The interactive
project dashboard exposes local generation and explicit remote synchronization.
Both commands load the project `.env`; values already present in the process
environment take precedence, while command flags take precedence over both.

Plugins can provide rich Markdown documentation for their top-level Postman
folder in `docs/postman/overview.md`. The generator places it in the folder's
**Overview** tab. Without that file it falls back to the plugin `description`
from `procyon-module.json`.

Synchronize the generated collection with every configured Postman target:

```bash
procyon-cli postman sync
```

There is no target limit. Use an unnumbered primary target and any numbered
targets (`_1`, `_2`, ...); indexes may have gaps:

```dotenv
POSTMAN_API_KEY=shared_postman_api_key
POSTMAN_COLLECTION_ID=primary_collection_id
POSTMAN_TARGET_NAME=Primary

POSTMAN_COLLECTION_ID_1=staging_collection_id
POSTMAN_TARGET_NAME_1=Staging

POSTMAN_API_KEY_8=separate_account_api_key
POSTMAN_COLLECTION_ID_8=production_collection_id
POSTMAN_TARGET_NAME_8=Production
```

Numbered targets use the shared `POSTMAN_API_KEY` unless
`POSTMAN_API_KEY_N` is set. `POSTMAN_API_COLLECTION_ID_N` remains supported as
a legacy alias for `POSTMAN_COLLECTION_ID_N`. A missing target pair fails the
sync instead of silently skipping that target. With no targets, `sync` still
generates the local collection and performs no remote request. API keys are
never printed.

Generation settings can also come from `.env`: `POSTMAN_COLLECTION_FILE`,
`POSTMAN_COLLECTION_NAME`, `POSTMAN_CONFIG_PATH`, `POSTMAN_BASE_URL`,
`POSTMAN_ADMIN_URL`, `POSTMAN_UPLOAD_URL`, `POSTMAN_ADMIN_KEY` and
`POSTMAN_AUTH_KEY`.

Explicit commands remain available for scripts and CI:

```bash
procyon-cli init
procyon-cli self-update --version v0.4.0
procyon-cli core update --version v0.4.0
procyon-cli module list
```

When required flags are omitted in a terminal, `init` opens an interactive TUI
with text fields, database and auth selectors, Docker/hello toggles, and a final
summary. New projects default to `./<project-name>` below the current directory.

Set `ACCESSIBLE=1` to use the screen-reader-friendly prompt rendering:

```bash
ACCESSIBLE=1 procyon-cli init
```

Supplying all required flags keeps the command non-interactive for scripts and
CI.

## Current Scope

Implemented:

```bash
go run . init
go run . module create invoice
go run . core update --version latest
go run . self-update --version latest
```

Non-interactive example:

```bash
go run . init \
  --name demo-api \
  --module github.com/acme/demo-api \
  --db postgres \
  --out ../demo-api
```

The CLI pins the compatible template tag by default. Override it explicitly
for development builds:

```bash
procyon-cli init --template-version main
```

The `init` command:

- downloads the Procyon template from `https://github.com/bartek5186/procyon`,
- copies the template into the output directory,
- rewrites module name, app name, config files, and Docker names,
- keeps Kratos, Casbin/RBAC, and admin configuration enabled,
- optionally removes Docker files or the example `hello` feature,
- prepares the generated project for normal `go run . -migrate=true` startup.
- writes `.procyon.json` with template, core, and minimum CLI versions.

Auth modes control which infrastructure is enabled in generated JSON configs:

- `kratos-casbin` enables Kratos, RBAC, and admin endpoints,
- `kratos` enables Kratos without RBAC,
- `admin` enables only `X-Admin-Key` endpoints,
- `none` disables all three modules.

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

The command fails if the module already exists or is already wired into the project. Existing generated files are not overwritten by default. Use `--force` to overwrite generated module files only when creating a module that is not wired yet:

```bash
procyon-cli module create invoice --force
```

## Shared Modules

`module create` generates application-owned CRUD code. Complete reusable
features are compile-time Go plugins with their own version, business logic and
dependencies:

```bash
procyon-cli module add example --published
procyon-cli module add payment-system \
  --provider stripe,google,apple \
  --published
```

The equivalent command form is also accepted:

```bash
procyon-cli add module payment-system --provider stripe
```

Preview the dependency and generated registration without changing the project:

```bash
procyon-cli module add payment-system --provider stripe --dry-run
```

Inspect installed modules recorded in `.procyon.json`:

```bash
procyon-cli module list
procyon-cli module info payment-system
```

Disable a plugin without deleting its database tables or stored installation
settings, then enable it again later:

```bash
procyon-cli module disable payment-system
procyon-cli module enable payment-system
```

Enabled plugins are registered in `plugins_gen.go`. Their namespaced runtime
defaults are composed into `config/plugins.generated.json`, while environment
variables declared by the plugin manifest are maintained in generated blocks
inside `.env.example`. Disabling a plugin removes its generated registration,
config and environment block but preserves its metadata and database data.

Update an installed plugin after publishing or selecting a newer module source:

```bash
procyon-cli module update payment-system
procyon-cli module update payment-system --source ../procyon-modules/payment-system
```

For tagged repositories, omit the local `replace` and use the published module:

```bash
procyon-cli module add payment-system --provider stripe --published
procyon-cli module update payment-system --published
```

The published update command resolves the latest version from the official
registry. Pass `--version v0.4.0` only when an exact version is required.

A shared module contains a `procyon-module.json` next to its `go.mod`. The CLI
adds it with `go get`, records the selected version and providers in
`.procyon.json`, and regenerates `plugins_gen.go`. The module remains an ordinary
Go dependency: implementation files are not copied into the application.

After both add and update, the CLI runs `go mod tidy` and `go test ./...`. It
restores `.procyon.json`, `plugins_gen.go`, `go.mod`, and `go.sum` if validation
fails. Plugins are linked into the application binary and can own routes,
policies, migrations, services and persistence; no dynamic `.so` loading is
used.

New local plugin scaffolds also contain `events.go`. Register typed handlers
there during plugin construction and publish through the shared bus supplied as
`plugins.Dependencies.Events`. Event-aware plugins reject hosts without the bus
instead of silently dropping events.

Registry discovery order:

1. `--source` for a direct module directory;
2. `--registry`;
3. `PROCYON_MODULE_REGISTRY`;
4. `procyon-modules/registry.json` or `../procyon-modules/registry.json`;
5. `PROCYON_MODULES_DIR/<module-name>`;
6. the official `procyon-modules` registry when `--published` is used.

The development registry and reference plugins live in the sibling
`procyon-modules` workspace. A local source is added with a Go `replace`
directive. Published plugins should live in independently tagged repositories;
their version is then managed by the normal Go module toolchain.

## Updating Procyon CLI

Update the CLI itself from any directory:

```bash
procyon-cli self-update
```

Pin an exact CLI release when needed:

```bash
procyon-cli self-update --version v0.4.0
```

For an npm installation the command runs `npm install --global
procyon-cli@<version>`. For a Go installation it runs `go install
github.com/bartek5186/procyon-cli@<version>` and prints the installed binary
path. Use `--dry-run` to preview the command.

## Updating Procyon Core

Projects generated by current versions of the CLI depend on the versioned
`github.com/bartek5186/procyon-core` Go module. Run this inside a generated
project:

```bash
procyon-cli core update
```

Pin a specific release when needed:

```bash
procyon-cli core update --version v0.4.0
```

The command runs `go get`, `go mod tidy`, and `go test ./...`. It updates the
shared runtime only; application models, routes, migrations, and business code
remain owned by the project. Preview the commands with `--dry-run`.

Compatibility is checked against `.procyon.json` before an update starts.
The old `procyon-cli update` spelling remains a deprecated alias for
`procyon-cli core update` so existing automation does not break immediately.
