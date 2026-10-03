# Contributing

Run `gofmt` and `go test ./...` before a pull request. CI runs both, plus `golangci-lint`, `go vet`, and `govulncheck`.

Return an error to the caller. The command handles it once. Do not log an error and also return it.

Do not add a package named `util`, `common`, or `pkg`. A new package needs a caller in this repo.

A test locks a behavior. Use the standard `testing` package and testify.

The layout and the Go style for this repo are in `CLAUDE.md`.
