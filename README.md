**Your whole API. Three tools.**

A hundred endpoints. A thousand. Still three tools.

Stop turning every endpoint into another tool the agent has to carry. Point veto at the OpenAPI you already have.

<img src="docs/veto.gif" width="1280" alt="Three tools: search, describe, invoke. The search command runs, then order 123 follows customerId 7 to customers.get. The delete command runs, then the call is held until you approve.">

It finds order 123, follows the customer you named, and holds the delete.

**search → describe → invoke**

Search finds the operation from what you asked, including retire for delete. Describe returns that call, and the next one. Invoke runs it. The raw spec stays out of the agent's context.

**Who placed order 123?**

`orders.get` → customerId 7 → `customers.get`

You declared that link. The agent follows it. A field name alone is not a call.

**One catalog. Four ways in.**

| | |
| --- | --- |
| MCP | search, describe, and invoke for the agent |
| CLI | the same three, in your terminal |
| JSON lines | `veto serve --json`, for scripts |
| Go client | `veto generate` writes a client for this runtime |

Your services stay where they are. The Go client calls veto. It is not a new client for every upstream API. Module `github.com/aiveto/veto`.

**Delete order 123.**

The agent can ask. Veto holds that caller, that operation, and those parameters. Nothing is sent until you approve that call. Credentials resolve on the call. The trace records the decision and leaves the secret out.

```bash
brew install aiveto/veto/veto
```

```bash
veto init orders.yaml customers.yaml
veto serve --stdio
```

The [setup guide](docs/guide.md#mcp) has the block for Claude, Cursor, or ChatGPT.

`go install github.com/aiveto/veto/cmd/veto@latest` needs Go 1.27.1. `brew` does not. Binaries are on [GitHub Releases](https://github.com/aiveto/veto/releases). A release also pushes `ghcr.io/aiveto/veto`.

[veto-demo](https://github.com/aiveto/veto-demo) is the longer walk: Harbor, orders, customers, and billing. `make demo` runs it over the CLI, then over MCP.

This repo runs the same story with no model key.

```bash
git clone https://github.com/aiveto/veto.git
cd veto
go run ./examples/two-apis
```

```text
Someone asked who placed order 123.
orders.get returned customerId 7.
customers.get was called for 7 because the note said Order.customerId identifies customers.get.
orders.delete did not call HTTP until approved.
The trace left the secret out.
```

```bash
veto validate --config testdata/veto.yaml
veto serve --config testdata/veto.yaml --stdio
veto eval --config testdata/veto.yaml --case testdata/delete.yaml
```

<img src="docs/veto-runtime.png" width="1280" alt="Claude, Cursor, or ChatGPT talks to veto over MCP. A CLI or script uses search, describe, and invoke. Search and describe read the catalog. Invoke goes to policy and confirmation, then HTTP.">

**Pre-1.0.** Public APIs may change.

[Setup guide](docs/guide.md) | [Current limits](docs/guide.md#limits)
