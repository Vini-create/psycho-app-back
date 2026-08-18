# Contrato Go API → FastAPI Companion

Atualizado em: 2026-08-18
Status: o cliente HTTP no backend Go está implementado; o serviço FastAPI ainda será criado.

## Transporte e autenticação

O Go chama:

```http
POST {COMPANION_BASE_URL}/v1/companion/respond
Authorization: Bearer {COMPANION_API_KEY}
Content-Type: application/json
Accept: application/json
X-Request-ID: {user_message_id}
```

Em produção, `COMPANION_BASE_URL` obrigatoriamente usa HTTPS. Redirects não são seguidos. O timeout atual é configurado por `COMPANION_TIMEOUT`, com valor local de 20 segundos.

O FastAPI deve comparar a API key de forma segura, nunca registrar seu valor e retornar `401` para credencial inválida. A chave é serviço-a-serviço, não deve chegar ao frontend.

## Request

```json
{
  "request_id": "uuid-da-mensagem-do-usuario",
  "conversation_id": "uuid-da-conversa",
  "user_id": "uuid-opaco-do-usuario",
  "message": "Mensagem atual do usuário",
  "history": [
    {
      "role": "user",
      "content": "Mensagem anterior"
    },
    {
      "role": "assistant",
      "content": "Resposta anterior"
    }
  ]
}
```

Regras:

- `request_id` é estável e deve ser usado como chave idempotente no FastAPI.
- `message` possui no máximo 8.000 caracteres.
- `history` vem em ordem cronológica.
- O Go envia no máximo `COMPANION_HISTORY_MESSAGES`, atualmente 20.
- O contexto total enviado é limitado a aproximadamente 60.000 caracteres.
- O backend Go só chama o serviço após verificar os consentimentos vigentes.
- Não registrar conteúdo de mensagens em logs, traces ou ferramentas de erro.

## Response de sucesso

Status obrigatório: `200 OK`.

```json
{
  "content": "Resposta acolhedora e segura ao usuário.",
  "provider": "openai",
  "model": "modelo-utilizado",
  "prompt_version": "companion-v1",
  "blocked": false
}
```

Caso safety bloqueie a resposta original:

```json
{
  "content": "Mensagem segura que será mostrada ao usuário.",
  "provider": "openai",
  "model": "modelo-utilizado",
  "prompt_version": "companion-v1",
  "blocked": true,
  "block_reason": "self_harm_escalation"
}
```

O JSON é estrito: campos desconhecidos fazem o Go rejeitar a resposta.

Limites da resposta:

- corpo HTTP máximo processado: 64 KiB;
- `content`: 1 a 12.000 caracteres;
- `provider`: até 100 caracteres;
- `model`: até 160 caracteres;
- `prompt_version`: até 100 caracteres.

`block_reason` não é persistido nem enviado ao frontend nesta primeira versão. Use somente códigos internos sem texto clínico ou dados pessoais.

## Erros

Qualquer status diferente de `200`, timeout, JSON inválido, campo inesperado ou resposta vazia é tratado como indisponibilidade. O corpo de erro do FastAPI não é repassado ao frontend.

O Go:

1. preserva a mensagem original cifrada;
2. marca a geração como `failed`;
3. responde `202` ao frontend;
4. permite retry explícito.

Portanto, o FastAPI deve ser idempotente por `request_id`: retries não podem cobrar ou gerar múltiplas respostas de forma desnecessária.

## Responsabilidades do FastAPI

- system prompt e suas versões;
- seleção do provedor/modelo;
- moderação e safety;
- política específica para risco e crise;
- retries internos do provedor, dentro do timeout global;
- métricas de latência, tokens e custo sem conteúdo sensível;
- avaliações de qualidade;
- resposta natural do companion.

O FastAPI não grava conversas como fonte da verdade. O PostgreSQL controlado pelo backend Go continua sendo o registro oficial de mensagens, consentimentos e vínculos.

## Processamento de contexto periódico

O mesmo serviço também expõe:

```http
POST {COMPANION_BASE_URL}/v1/context/process
Authorization: Bearer {COMPANION_API_KEY}
Content-Type: application/json
Accept: application/json
X-Request-ID: {context_job_id}
```

Request:

```json
{
  "request_id": "uuid-do-job",
  "connection_id": "uuid-opaco-do-vinculo",
  "user_id": "uuid-opaco-do-usuario",
  "period_start": "2026-08-11T00:00:00Z",
  "period_end": "2026-08-18T00:00:00Z",
  "messages": [
    {
      "id": "uuid-da-mensagem",
      "role": "user",
      "content": "Conteúdo decifrado apenas em trânsito",
      "created_at": "2026-08-12T14:00:00Z"
    }
  ]
}
```

O período possui no máximo 31 dias e 500 mensagens. `request_id` é a chave idempotente. Não persista nem registre o conteúdo recebido no FastAPI.

Response obrigatória `200 OK`:

```json
{
  "summary": "Síntese objetiva do período.",
  "items": [
    {
      "kind": "theme",
      "description": "Tema recorrente identificado.",
      "confidence": 0.91,
      "occurred_at": "2026-08-12T14:00:00Z",
      "source_message_ids": ["uuid-da-mensagem"]
    }
  ],
  "provider": "openai",
  "model": "modelo-utilizado",
  "prompt_version": "context-v1"
}
```

Regras da resposta:

- `summary`: 1 a 12.000 caracteres;
- no máximo 100 itens;
- `kind`: `theme`, `event` ou `marked_topic`;
- `description`: 1 a 4.000 caracteres;
- `confidence`, quando presente: entre 0 e 1;
- cada item deve citar ao menos um `source_message_id` recebido no request;
- IDs desconhecidos invalidam a resposta inteira;
- `provider`, `model` e `prompt_version` são obrigatórios;
- JSON estrito, sem campos adicionais; corpo máximo de 256 KiB.

O resumo deve ser descritivo e fiel às fontes, sem diagnóstico automático. Os itens devem separar observações da conversa de inferências. A política de crise/safety e a versão do prompt precisam ser testáveis. Qualquer erro, timeout ou resposta inválida faz o Go marcar o job como `failed`; nenhum detalhe interno é exposto ao profissional.

O Go persiste os IDs-fonte somente para auditoria e rastreabilidade. Nem os IDs nem as mensagens originais aparecem nas respostas destinadas ao profissional.
