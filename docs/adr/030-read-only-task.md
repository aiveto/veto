# ADR 030: A task is a read-only flow

A task is a flow with a question and the response fields that answer it. A later step is bound by one declared relation, or by `output` and `to` on the step. Search returns the task. Check fails when the binding field or an answer field is gone, the next operation has no parameter for it, or a step is not a read.

Refused: a second workflow engine, a saved response body, a graph.
