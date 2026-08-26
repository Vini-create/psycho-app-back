# Anamnesys AI

Serviço interno FastAPI/LangGraph para conversa, segurança e geração do Relatório de
Contexto e Jornada. O backend Go continua sendo a fonte da verdade para autenticação,
consentimento, criptografia, persistência, fila e autorização.

## Desenvolvimento

```bash
cd services/ai
uv sync --extra dev
cp .env.example .env
uv run uvicorn app.main:app --reload --port 8000
```

`AI_PROVIDER=mock` é o padrão seguro e não chama serviços externos. Para testar o adapter
real, configure `AI_PROVIDER=openai` e `AI_OPENAI_API_KEY` somente no ambiente local ou no
secret manager. Nunca grave a chave no repositório.

## Fluxos e custo

- conversa normal: moderação gratuita + uma geração com `gpt-5.6-terra`;
- mensagem suspeita: classificador `gpt-5.6-luna` somente quando as regras locais detectam
  ambiguidade;
- risco/boundary: classificador auxiliar somente quando moderação ou palavras-gatilho exigem;
- tradução: não há chamada dedicada; o modelo responde diretamente no idioma detectado;
- relatório normal: uma geração estruturada com `gpt-5.6-terra`;
- relatório grande: extrações paralelas e limitadas com `gpt-5.6-luna`, seguidas por uma
  síntese com `gpt-5.6-terra`.

Não existe RAG no fluxo. O serviço não persiste conversas e usa `store=false` no provider.

## Validação

```bash
uv run ruff format --check app tests
uv run ruff check app tests
uv run mypy app
uv run pytest -q
```

Endpoints internos:

- `GET /health`
- `POST /v1/companion/respond`
- `POST /v1/companion/respond/stream` (`application/x-ndjson`, deltas validados)
- `POST /v1/context/process`

Os dois endpoints de inferência exigem `Authorization: Bearer <AI_SERVICE_API_KEY>`. Consulte
`../../COMPANION_SERVICE_CONTRACT.md`, `../../AI_ARCHITECTURE.md` e
`../../AI_SECURITY.md`.
