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

`AI_PROVIDER=mock` é o padrão seguro e não chama serviços externos. Providers disponíveis:

- `openai`: configure `AI_OPENAI_API_KEY`;
- `deepinfra`: configure `AI_DEEPINFRA_API_KEY`; por padrão usa
  `meta-llama/Meta-Llama-3.1-8B-Instruct` no endpoint OpenAI-compatible da DeepInfra.

Guarde chaves apenas no ambiente local ignorado pelo Git ou no secret manager. Nunca grave uma
chave real no repositório.

Configuração mínima para testar o Llama pela DeepInfra:

```env
AI_PROVIDER=deepinfra
AI_DEEPINFRA_API_KEY=replace-with-deepinfra-api-key
AI_DEEPINFRA_MODEL=meta-llama/Meta-Llama-3.1-8B-Instruct
AI_DEEPINFRA_CONVERSATION_MAX_TOKENS=320
AI_DEEPINFRA_CLASSIFICATION_MAX_TOKENS=192
AI_DEEPINFRA_EXTRACTION_MAX_TOKENS=4096
AI_DEEPINFRA_REPORT_MAX_TOKENS=3072
AI_DEEPINFRA_CONVERSATION_TEMPERATURE=0.65
AI_DEEPINFRA_STRUCTURED_TEMPERATURE=0.1
```

Os limites de tokens são tetos de geração, não uma reserva cobrada integralmente. Conversa e
classificações usam tetos curtos para controlar custo e verbosidade. Extração e relatório mantêm
mais espaço porque podem produzir JSON proporcional ao histórico; reduzi-los demais aumenta o
risco de truncamento e de uma segunda chamada para reparo. A temperatura maior vale somente para
a fala conversacional. Classificações e demais saídas estruturadas ficam em `0.1` para preservar
consistência e validade do JSON.

O adapter DeepInfra usa Chat Completions e texto puro para conversa e streaming, seguido pelos
validadores determinísticos locais. Isso evita enviar um JSON Schema grande em cada mensagem e
deixa o modelo concentrado no texto que o usuário verá. Moderação, extração e relatório usam JSON
mode com o contrato no prompt e validação Pydantic local, pois o Llama 3.1 8B selecionado não aceita
`response_format=json_schema` na DeepInfra. Como a DeepInfra não oferece o endpoint especializado
`omni-moderation`, o próprio modelo executa a
triagem semântica de moderação. Isso acrescenta uma chamada curta por mensagem; regras locais
continuam sendo aplicadas antes das rotas especiais. Valide a política de retenção da DeepInfra
antes de enviar dados reais de saúde.

## Fluxos e custo

- conversa normal com OpenAI: moderação dedicada + uma geração com `gpt-5.6-terra`;
- conversa normal com DeepInfra: classificação estruturada + uma geração com o Llama configurado;
- mensagem suspeita: classificador `gpt-5.6-luna` somente quando as regras locais detectam
  ambiguidade;
- risco/boundary: classificador auxiliar somente quando moderação ou palavras-gatilho exigem;
- tradução: não há chamada dedicada; o modelo responde diretamente no idioma detectado;
- relatório normal: uma geração estruturada com `gpt-5.6-terra`;
- relatório grande: extrações paralelas e limitadas com `gpt-5.6-luna`, seguidas por uma
  síntese com `gpt-5.6-terra`.

Não existe RAG no fluxo. O serviço não persiste conversas. O adapter OpenAI usa `store=false`;
o adapter DeepInfra não envia esse parâmetro porque usa Chat Completions compatível, e fica
sujeito aos controles de dados contratados com a DeepInfra.

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
