# Your entire API platform. Just 3 MCP tools.

Your APIs are already built.

Point veto at your OpenAPI specs. An agent gets three tools: search, describe, and invoke. You name how the APIs connect. A delete waits until a person approves it.

No API rewrites. The agent does not get a tool for every endpoint.

<img src="docs/veto.gif" width="1280" alt="veto init reads the OpenAPI specs. You name the customer and invoice links. scrap means delete the order. Claude, Cursor, or ChatGPT gets search, describe, and invoke. Order 10482 is Mara Ellison, a wool coat and two cedar trays, $556. The invoice is paid. Scrap the order, and the call is held. The order is still there.">

See it yourself: [Run the Harbor demo](https://github.com/aiveto/veto-demo). Mara's order, the customer and invoice links, and a delete that waits.

## 100 endpoints. 1,000 endpoints. Still 3 MCP tools.

### search → describe → invoke

- **Search.** Find the right API using natural language.
- **Describe.** Discover its schema and what to call next.
- **Invoke.** Execute it with built-in controls.

Your API catalog grows. Your agent's tool surface stays the same.

## Your APIs are connected. Your agents should know that.

Ask: "Who placed order 123?"

`orders.get` → customerId 7 → `customers.get`

Veto uses declared relationships to guide the next API call. No guessed endpoints. No invented relationships. No loading the entire API catalog into context.

## Agents can act. You stay in control.

Ask: "Delete order 123."

Veto holds the operation until a human approves the exact caller, operation, and parameters. When approval is required, the operation cannot proceed without it.

## One OpenAPI catalog. Multiple ways to use it.

| Want to... | Veto gives you |
| --- | --- |
| Connect an agent | Search, describe, and invoke |
| Use your terminal | `veto search`, `veto describe`, `veto invoke` |
| Write a script | `veto serve --json` |
| Call it from Go | One generated client for this runtime, shared by every API |
| Follow a link to another service | Search in plain language, then the links you wrote |
| Stop a sensitive call | A person approves the exact caller, operation, and parameters |
| See what ran | A record of what ran, with secrets left out |

One catalog. One runtime. Consistent behavior across interfaces. Module `github.com/aiveto/veto`.

## Get started

Already have OpenAPI specs? You're three commands away from your first MCP server.

```bash
brew install aiveto/veto/veto
```

```bash
veto init orders.yaml customers.yaml
veto serve --stdio
```

The [setup guide](docs/guide.md#mcp) has the block for Claude, Cursor, or ChatGPT.

## See Veto in action

[Harbor](https://github.com/aiveto/veto-demo) is a working example: orders, customers, billing, the links between them, and a delete that waits. `make demo` runs it over the CLI, then over MCP.

No model API key for the local example in this repo: `go run ./examples/two-apis`.

You already have the APIs. Give your agents a better way to use them.

[Get started](docs/guide.md) · [Try the demo](https://github.com/aiveto/veto-demo) · [Explore the source](https://github.com/aiveto/veto)
