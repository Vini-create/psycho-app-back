# Anamnesys Backend

Monorepo do backend Go e do serviço interno de IA em FastAPI.

## Serviços

- `api`: API pública Go na porta `8080`;
- `ai`: FastAPI interna na porta `8000`, usando provider `mock` inicialmente;
- `postgres`: fonte da verdade e fila durável dos jobs;
- `migrate`: runner versionado do Goose, executado antes da API.

## Subir o ambiente completo

Crie o `.env` a partir do exemplo e preencha chaves reais de desenvolvimento:

```bash
cp .env.example .env
docker compose up --build --wait
```

Para deploy, use `.env.production.example` como checklist completo de variáveis.

Health checks:

```bash
curl http://localhost:8080/health
curl http://localhost:8000/health
```

O Compose altera internamente `COMPANION_BASE_URL` para `http://ai:8000`. `ai` é resolvido pelo DNS da rede Docker; não é uma chamada direta entre linguagens.

## Desenvolvimento da IA sem Docker

```bash
cd services/ai
uv sync --extra dev
cp .env.example .env
uv run uvicorn app.main:app --reload --port 8000
```

Nesse modo, execute o Go na raiz com `COMPANION_BASE_URL=http://localhost:8000`, `COMPANION_ENABLED=true` e `AI_CONTEXT_WORKER_ENABLED=true`.

## Processamento assíncrono

Pedidos de resumo retornam `202 queued`. Workers Go reivindicam jobs com `FOR UPDATE SKIP LOCKED`, usam lease para recuperar jobs abandonados e fazem retry com backoff. O número de workers por instância é configurado por `AI_CONTEXT_WORKER_CONCURRENCY`.

O chat permanece request/response porque o paciente aguarda a resposta, mas cada request Go roda em sua própria goroutine e o cliente HTTP reutiliza conexões. Streaming poderá ser adicionado depois sem permitir que o frontend contorne o backend Go.

E-mails de verificação e recuperação usam uma outbox PostgreSQL. O token cifrado
é gravado na mesma transação da conta, e workers enviam pela API da Brevo com
lease, retry e backoff. Em desenvolvimento, `EMAIL_PROVIDER=mock` valida o fluxo
sem mensagens reais; produção exige `EMAIL_PROVIDER=brevo`, worker ativo e HTTPS.

O login Google usa Google Identity Services no frontend e valida o ID token no
backend. Configure o mesmo Web Client ID em `AUTH_GOOGLE_CLIENT_ID` e
`NEXT_PUBLIC_GOOGLE_CLIENT_ID`; cada tentativa usa um nonce de uso único.

## Testes

```bash
go test -race ./...
docker build --target test -t anamnesys-ai-test ./services/ai
```

Contratos:

- `FRONTEND_API_CONTRACT.md`
- `COMPANION_SERVICE_CONTRACT.md`
