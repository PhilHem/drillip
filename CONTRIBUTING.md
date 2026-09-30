# Contributing to Drillip

Read the [architecture explanation](docs/explanation/architecture.md) for the
source layout, port responsibilities, and dependency rules.

## Build from a checkout

Install the Go version specified in [go.mod](go.mod), currently Go 1.26.
Run the commands below from the repository root:

```sh
go build .
```

This produces the `drillip` executable (`drillip.exe` on Windows). To install
the current checkout into your Go binary directory, use:

```sh
go install .
```

Add the directory that contains the installed `drillip` executable to your
shell's `PATH`. Confirm that the shell can find the command:

```sh
drillip --help
```

## Check a change

Run the same test, race-detector, and vet commands as CI. The race detector
requires CGO and a working C compiler on a supported platform.

```sh
go test -short ./... -count=1
go test -short -race ./... -count=1
go vet ./...
```

These tests include the production import checks in
[internal/architecture_test.go](internal/architecture_test.go). Keep tests
beside the code they exercise. Integration tests can connect concrete
adapters and services.

To include the longer SQLite state-machine and property tests, omit `-short`:

```sh
go test ./... -count=1
```

CI also runs `govulncheck`. The complete workflow is in
[.github/workflows/ci.yml](.github/workflows/ci.yml).

## Update documentation

Keep reference details in [docs/reference](docs/reference), procedures in
[docs/how-to](docs/how-to), learning exercises in [docs/tutorials](docs/tutorials),
and conceptual explanations in [docs/explanation](docs/explanation). Link new
entry points from the README when they help readers find the right document.
Check relative links and run:

```sh
git diff --check
```
