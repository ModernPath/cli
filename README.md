# ModernPath CLI source

This directory contains the Go implementation of the `modernpath` command.

The single user and technical guide is
[`docs/cli.md`](https://github.com/ModernPath/cli/blob/main/docs/cli.md). Do not maintain installation, connection,
or command examples in this contributor README; update the canonical guide and
the relevant Cobra help together. In this monorepo, the canonical source is
`modernpath-core/docs/cli.md`; the source export publishes it as `docs/cli.md`
in `ModernPath/cli`. Keep the embedded guide in sync with that source.

## Contributor entry points

```sh
go build -o modernpath .
go test ./...
```

Install the working tree into every discovered `modernpath` location when
testing hooks or another repository:

```sh
scripts/install-local.sh
```

Build release archives with:

```sh
scripts/build-all.sh [version]
```

Source layout:

| Path | Purpose |
|---|---|
| `cmd/` | Cobra command registration and behavior |
| `internal/api/` | HTTP client and wire types |
| `internal/config/` | Repository-local bindings and credentials |
| `internal/rdd/` | Requirement-driven record extraction and operation building |
| `internal/opschema/` | Embedded synchronization schema and validation |
| `internal/kit/` | Embedded process package installation |
| `scripts/` | Local installation, release packaging, and process-asset refresh |

Run `modernpath --help` from the built binary to inspect the registered command
tree. Behavioral documentation must be checked against `cmd/*.go` and the
relevant server controllers.


Reverse-engineered baseline evidence is reviewed through ordinary requirement
verification and the selected Epic's normal completion review. Verification
does not change requirement content or lifecycle; completion follows the
server-backed Epic process described in `../.modernpath/rdd/PROCESS.md`. See the
canonical CLI guide for the commands currently shipped by this source tree.

## Implementation map

Paths in this section are relative to `modernpath-core`.

| Concern | Authoritative source |
|---|---|
| Command registration and global flags | `CODE:tools/modernpath/cmd/root.go:rootCmd` and each command file's `init` function |
| Authentication and credential writes | `CODE:tools/modernpath/cmd/auth.go:runAuth`, `CODE:tools/modernpath/internal/config/config.go:WriteAuth` |
| ZITADEL login and device flow | `CODE:tools/modernpath/internal/zitadel/login.go:Login`, `CODE:tools/modernpath/internal/zitadel/authcode.go:AuthCodeLogin`, `CODE:tools/modernpath/internal/zitadel/deviceflow.go:DeviceLogin` |
| Repository initialization and Knowledge Core export | `CODE:tools/modernpath/cmd/init.go:runInit`, `CODE:tools/modernpath/cmd/init.go:finalizeSystemInit` |
| Binding discovery and persistence | `CODE:tools/modernpath/internal/config/config.go:FindConfigDir`, `CODE:tools/modernpath/internal/config/config.go:WriteConfig` |
| Knowledge Core synchronization | `CODE:tools/modernpath/cmd/docs.go:runDocsSync` |
| Source upload | `CODE:tools/modernpath/cmd/source.go` |
| Requirements and native procedure/evidence commands | `CODE:tools/modernpath/cmd/requirements_list.go`, `CODE:tools/modernpath/cmd/requirements_search.go`, `CODE:tools/modernpath/cmd/compliance_native.go` |
| Epic records, specifications and saved plan commands | `CODE:tools/modernpath/cmd/epics.go`, `CODE:tools/modernpath/cmd/epic_records.go`, `CODE:tools/modernpath/cmd/epic_plan.go` |
| Agent hooks | `CODE:tools/modernpath/cmd/hooks.go:runHooksInstall`, `CODE:tools/modernpath/cmd/hooks_doctor.go:runHooksDoctor` |
| Development review | `CODE:tools/modernpath/cmd/review.go` |
| Installers and release archives | `CODE:tools/modernpath/scripts/install.sh`, `CODE:tools/modernpath/scripts/install.ps1`, `CODE:tools/modernpath/scripts/build-all.sh` |

The user guide describes supported behavior; use these sources to verify
implementation changes before updating that guide.

## Process maintenance

Edit the bundled process and skills under `internal/kit/assets/rdd/`. Rebuild
with `scripts/install-local.sh`, then run `modernpath install` from the
workspace root and commit the regenerated copies with the source changes.
Keep contributor paths and build instructions in repository documentation;
bundled guides, managed instructions and command help serve customer workspaces.
