# ADR 008: Cobra for the veto binary only

`cmd/veto` uses `github.com/spf13/cobra`. Generated per-API CLIs stay on `flag`. Core packages do not import Cobra.
