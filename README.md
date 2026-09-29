# veto

You have hundreds of API calls. The agent does not get one tool per call, and it does not get the file. It gets three tools: find, read, make. It asks to delete a customer. Nothing is sent. A person says yes. Then it is sent. You can read that attempt, and the secret is not in it.

Stripe's file is 612 calls, and veto still exposes three tools. Delete sent nothing.

The Stripe file is not in this repository. The orders example is the small picture of a second call.

Someone asks who placed order 123. `orders.get` returns customerId 7. The note says `Order.customerId identifies customers.get`. `customers.get` is called for 7. `orders.delete` sends nothing until it is approved. The trace leaves the secret out.

Veto does not infer that line. Most calls need no line. If the spec already has a link, that link is used. This file is only for a join the spec left out.

```yaml
relations:
  - schema: Order
    field: customerId
    to: customers.get
```

The command line and the generated Go client use that same door.

```bash
go run ./examples/two-apis
go run ./cmd/veto validate --config examples/two-apis/veto.yaml
go run ./cmd/veto pack --config examples/two-apis/veto.yaml --message "who placed order 123"
go run ./cmd/veto serve --config examples/two-apis/veto.yaml --stdio
```

The first command is the walk above. `validate` prints the joins, including a link the spec already declared. `pack` prints the note for that question. The API file is not in the note. `serve` listens on stdio. The three tools are `capabilities_search`, `capabilities_describe`, and `capabilities_invoke`.

You still write the API files. Content-Type `application/json;v=3` is sent as written. `customer-v3.yaml` is another file. Duplicate operation ids fail the load. You write a link line only when the file left the join out. A company rule runs in front of the stop and does not remove it. `WrapPolicy` installs that rule. If the rule does not end the check, the stop still runs. `veto serve` does not load the rule. You write the cases you care about. `veto check --against` fails when a joined call disappears, confirmation is dropped without an agent.yaml change, or a case expectation changes.

Veto does not guess connections. It does not make a large file simple. It does not remember the conversation after a restart.

Module: `github.com/aiveto/veto`

```bash
go test ./...
go run ./cmd/veto check --config testdata/veto.yaml --case testdata/cases --against HEAD
```
