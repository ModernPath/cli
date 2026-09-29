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


Reverse-engineered baselines can be verified from existing execution proof and
accepted through one exact human decision. Use `modernpath reverse-engineer
proof-preview`, `execution-proof`, `delivery-proof`, `acceptance-open`,
`acceptance-apply` and `acceptance-status`; each structured write takes `--file`.
The installed `rdd-reverse-engineer-verify`, `rdd-reverse-engineer-accept` and
`mp-process-cli` skills define the proof fields and sequence. The existing
`factory answer` records the human answer. Eligibility requires complete current
assertion/execution proof and a separate fetched repository integration
observation; approval applies PENDING_VERIFICATION → DONE with a durable receipt,
keeping compliance status unchanged. Normal development still uses RED/GREEN.
