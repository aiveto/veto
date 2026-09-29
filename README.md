# veto

Veto is one guarded way for an agent to call the APIs you already have.

Stripe's API file is 8316935 bytes and 612 calls. The agent still gets three tools: find a call, read it, make it. The note for "delete a customer" is 1302 bytes. The file stays out of the note. Delete customer stopped with zero HTTP. A get of that customer sent one request. The trace of the attempt leaves the secret out. When the file changes and a call or a stop disappears, a test fails.

That is the product. A small example is only how one piece looks.

## One link, when the file does not say it

An orders API and a customers API sit on different hosts. Someone asks who placed order 123. Find matches `orders.get`. The call returns `customerId` 7.

When a field on one service is the id for a call on another, and the API file does not already say so, you write one line:

```yaml
relations:
  - schema: Order
    field: customerId
    to: customers.get
```

That sentence goes in the note: `Order.customerId identifies customers.get`. Veto does not infer it. Most calls need no line. The agent then calls `customers.get` for 7.

If the agent asks to delete the order, no HTTP goes out until someone approves. Then the same call goes out.

## What you stop writing

You do not write one tool per URL. You do not paste the API files into the prompt. You do not copy the stop into the agent, the command line, and the generated Go client. Those three use the same door.

You still write the API files, a link line only when the file left the join out, a company rule if the built-in stop is not enough, and the cases you care about.

Module: `github.com/aiveto/veto`

```bash
go run ./examples/two-apis
```
