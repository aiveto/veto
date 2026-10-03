# ADR 011: Evals use scripted model and real policy

`veto eval` runs yaml cases against `agent.Scripted` and builtin policy. The shipped case is "delete order 123": confirmation required, `orders.delete` named.

Refused: a network LLM on the default eval path.
