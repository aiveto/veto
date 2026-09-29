# veto

Veto is one guarded way for an agent to call the APIs you already have. You bring the API files.

One catalog. Three tools, however many URLs are in the files: find a call, read it, and make it. The agent, the command line, and the generated Go client use that same door.

The short note for the turn is the context. It has the matching calls, the link sentence, and the rules. The API files stay out. That note is why the next call can happen.

Semantics is that sentence in the note. It is derived from the link file you write. Veto does not guess the connection. Most calls need no line.

A delete does not go out until someone says yes. Then the same call goes out. A company rule can sit in front of that stop. The extra check runs, and the stop still runs.

Replay reads the attempt later: what was asked, whether it was allowed, and what was sent. The secret is left out.

Eval fails when an API file or a link changes, including a delete that must not send before someone says yes.

Real API files are large. They have many calls, more than one host, auth, and bodies. Veto does not make them simple. Orders and customers are the small picture.

Module: `github.com/aiveto/veto`

## Orders and customers

An orders API and a customers API sit on different hosts. Someone asks who placed order 123. The matching call is `orders.get`. It returns `customerId` 7.

When a field on one service is the id for a call on another, you write it:

```yaml
relations:
  - schema: Order
    field: customerId
    to: customers.get
```

That line is `Order.customerId identifies customers.get`. The context note carries it, and the next call is `customers.get` for 7.

If the ask is to delete the order, no request goes out until someone says yes.

## Run

From this repository:

```bash
go build -o veto ./cmd/veto
go run ./examples/two-apis
```

```bash
go test ./...
go vet ./...
```

The example is `examples/two-apis`: `orders.yaml`, `customers.yaml`, and `relations.yaml`.
