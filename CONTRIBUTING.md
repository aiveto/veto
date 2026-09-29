# Contributing

Run `gofmt` and `go test ./...` before a pull request. CI runs both.

Return an error to the caller. The command handles it once.

Do not add a package named `util`, `common`, or `model`.

A test locks a behavior.

The rest of the Go rules are in `CLAUDE.md`.
