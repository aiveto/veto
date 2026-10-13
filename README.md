<img src="docs/veto-social.png" width="1280" alt="Your whole API. Three tools.">

**Your whole API. Three tools.**

Your APIs are already built. Make them ready for an agent.

Point veto at the OpenAPI you have.

- **Three tools**, however large the catalog grows. Search, describe, and invoke.
- **The next call is one you declared.** Search finds the operation. A relation names what comes next. The raw spec stays out.
- **MCP, the CLI, JSON lines, and a Go client** for this runtime.
- **A hold on the delete.** You approve that caller, that operation, and those parameters. Policy, credentials, and a trace come with the call.

One catalog. One runtime. Your services stay where they are.

<img src="docs/veto.gif" width="1280" alt="Three tools: search, describe, invoke. The search command runs, then order 123 follows customerId 7 to customers.get. The delete command runs, then the call is held until you approve.">

Watch it discover an order, follow the relationship you declared, and hold the delete. Still three tools.

**Who placed order 123?**

`orders.get` → customerId 7 → `customers.get`

You declared that link. The agent follows it. A field name alone is not a call.

| | |
| --- | --- |
| MCP | search, describe, and invoke for the agent |
| CLI | the same three, in your terminal |
| JSON lines | `veto serve --json`, for scripts |
| Go client | `veto generate` writes a client for this runtime, not a new client for every upstream API |

**Delete order 123.**

The agent can ask. Veto holds that call. Nothing is sent until you approve it.

```bash
brew install aiveto/veto/veto
```

```bash
veto init orders.yaml customers.yaml
veto serve --stdio
```

The [setup guide](docs/guide.md#mcp) has the block for Claude, Cursor, or ChatGPT. Install with Go, and the container image, are there too.

<img src="docs/veto-runtime.png" width="1280" alt="Claude, Cursor, or ChatGPT talks to veto over MCP. A CLI or script uses search, describe, and invoke. Search and describe read the catalog. Invoke goes to policy and confirmation, then HTTP.">

[veto-demo](https://github.com/aiveto/veto-demo) is Harbor: orders, customers, and billing, the relationship, and the hold. `make demo` runs it over the CLI, then over MCP.

This repo runs that story with no model key.

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

Your APIs are already built. Point veto at them.

[Setup guide](docs/guide.md) | [Current limits](docs/guide.md#limits)
