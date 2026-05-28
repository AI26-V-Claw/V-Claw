# providers

LLM provider adapters live here.

The agent loop should call a provider interface instead of hard-coding any single LLM vendor. This keeps Anthropic, OpenAI, OpenAI-compatible APIs, hosted routers, and local models interchangeable.

Planned submodules:

- `anthropic`: Claude models through Anthropic's native API.
- `openai`: OpenAI models and official APIs.
- `openai-compatible`: compatible HTTP APIs from other vendors or local runtimes.
- `gemini`: Gemini-family models if used directly.
- `openrouter`: hosted model routing if enabled.
- `local`: Ollama, LM Studio, or other local model runtimes.
- `router`: provider selection, fallback, model registry, usage accounting.

