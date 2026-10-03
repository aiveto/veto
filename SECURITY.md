# Security

Report a leaked token, a secret in a trace, or a destructive call that ran without confirmation. Open a private security advisory on [aiveto/veto](https://github.com/aiveto/veto/security/advisories). Do not open a public issue.

Name the environment variable and the operation id. Do not paste the token, the approval value, or the trace body.

## Trust

Veto calls the server URL declared in the contract. Point it at APIs you trust. An untrusted OpenAPI document is a request veto will send.

`veto serve` and `veto approve` share `VETO_APPROVAL_NONCE_DIR` and `VETO_APPROVAL_SECRET`. A signing secret without that shared directory can be consumed twice until the approval expires. Unset, the confirmation gate stays on. `confirmation: false` in `veto.yaml` is the deployment switch that turns it off.

`trace_export: stdout` and `trace_export: otlp` export the same allowlisted attributes. A response body is not in that set. `GET /healthz` and `GET /readyz` do not require `Veto-Caller`. Every other HTTP request does.
