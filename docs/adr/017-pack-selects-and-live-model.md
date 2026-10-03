# ADR 017: The pack selects, relations become sentences, openai is a key

The pack holds the search hits and their neighbors, not the whole catalog. A declared relation becomes a sentence on the note. `model: openai` is a provider key and reads `OPENAI_API_KEY`. `scripted` stays the default. An empty key or an empty pack is an error.

Refused: a full-catalog index. A guessed relation. An OpenAI SDK dependency.
