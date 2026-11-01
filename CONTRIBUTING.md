# Contributing

Issues and pull requests are welcome. This is a personal tool for one operator,
so the bar for new features is "does it remove work from that operator's day".

## Before you start

- Read `docs/design.md`: it is the only place where the shape of the tool is
  argued, and a change that contradicts it needs the document updated first.
- Run the checks locally; CI runs the same ones and does not ask twice.

```
go build ./cmd/...
go test ./...
golangci-lint run ./...
./scripts/ci-singbox-check.sh testdata/basic
```

## Conventions

### Commit message

```
type: subject in the imperative, lower case, no trailing period

Body: why, not what. The diff already says what changed. Wrap at 72.
Mention the trade-off you accepted and the case you did not cover.
```

Types used here: `state`, `render`, `validate`, `plan`, `apply`, `agent`,
`bootstrap`, `ops`, `cli`, `mobile`, `gitsync`, `docs`, `repo`, `lint`, `tests`.

The subject names the part of the tree, not the language: `render: …`, never
`feat: …`.

### Branch name

`<type>/<short-slug>`, for example `agent/install-command` or `docs/decisions`.

### Tests

- Anything that renders is compared against the golden files in `testdata`.
  `go test ./internal/render/ -update` rewrites them; review the diff before
  committing it.
- Anything that validates belongs in the table in
  `internal/validate/validate_test.go`: mutate the fixture, name the error.
- The command surface is pinned by `internal/cli/app_test.go`: adding a command
  means updating the list, and every leaf must accept `--json`.

### Golden files

Snapshots came from the Rust core this repository replaced. They are the proof
that the port is faithful, so a snapshot change must be explainable: a real
renderer change, not a formatting accident.

## Code

- The state is the only source of truth; nothing renders from the network, the
  clock or the environment.
- `internal/` for everything; `cmd/` holds two thin mains and nothing else.
- Errors carry a category when the caller decides on it: a guard refusal is not
  a failure, and section 16 of the design gives them different exit codes.
- No shell over SSH unless the agent cannot do it: the agent is the narrow path.
