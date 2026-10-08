# ADR 031: Sharing waits on a second person

The caller is who asked. The upstream credential is what the call presents to the service. They stay separate. `Veto-Caller` names the caller. Login, client credentials, token exchange, and a command stay the upstream credential.

Sharing is built when a second person needs the same deployment and the question is whose upstream credential applies.

Refused: accounts, per-user or per-tenant OAuth, expiry, revocation, and reconnection.
