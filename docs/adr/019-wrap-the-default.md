# ADR 019: A wrap calls the default

`Loop.WrapPolicy` wraps the policy already configured. An empty next is builtin. Confirmation and a permission denial stay in force when an extension allows the call. `buildLoop` constructs `local`, `builtin`, `scripted`, and `openai` from the keys. Empty `model_base_url` is `https://api.openai.com/v1`.

Refused: a plugin registry. A full invoke-step chain.
