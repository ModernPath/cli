# ModernPath CLI source

This directory contains the Go implementation of the `modernpath` command.

The single user and technical guide is
[`docs/cli.md`](../../docs/cli.md). Do not maintain installation, connection,
or command examples in this contributor README; update the canonical guide and
the relevant Cobra help together.

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
server controllers cited by the canonical guide.
