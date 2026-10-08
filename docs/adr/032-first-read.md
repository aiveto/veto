# ADR 032: The first read and the saved task

`veto init` writes the auth env name for each scheme on the suggested read, and an `agent.yaml` that names those operations. It previews that read. It calls the read when the preview is clean and the credential is set. The response is not stored.

A task in the flow file is a check. `veto check` runs it without an eval case. An eval case still runs when one is configured.

Refused: a saved response, a second case file, a host-specific client config.
