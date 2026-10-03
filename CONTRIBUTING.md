# Contributing

Run `make ci` before a pull request. That is gofmt, golangci-lint, `go vet`, `go test -race ./...`, and `veto check`. CI runs the same checks, plus `govulncheck`.

The code of conduct is [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

Return an error to the caller. The command handles it once. Do not log an error and also return it.

Do not add a package named `util`, `common`, or `pkg`. A new package needs a caller in this repo.

A test locks a behavior. Use the standard `testing` package and testify.

The layout and the Go style for this repo are in `CLAUDE.md`.
