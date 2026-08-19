# Anamnesys AI

Serviço interno FastAPI responsável somente por inferência, prompts, safety e extração estruturada. O backend Go continua responsável por autenticação, consentimentos, persistência e autorização.

## Desenvolvimento local

```bash
cd services/ai
uv sync --extra dev
cp .env.example .env
uv run uvicorn app.main:app --reload --port 8000
```

Enquanto `AI_PROVIDER=mock`, os endpoints devolvem respostas determinísticas sem chamar ou cobrar um provedor externo.

## Endpoints internos

- `GET /health`
- `POST /v1/companion/respond`
- `POST /v1/context/process`

Os endpoints de IA exigem `Authorization: Bearer <AI_SERVICE_API_KEY>`. O contrato completo está em `../../COMPANION_SERVICE_CONTRACT.md`.
