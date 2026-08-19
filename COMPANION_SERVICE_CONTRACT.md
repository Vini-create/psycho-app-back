# Contrato Go API → FastAPI AI

Atualizado em: 2026-08-19

O serviço é interno. Ambos os POSTs exigem `Authorization: Bearer
<AI_SERVICE_API_KEY>`, `Content-Type: application/json` e `X-Request-ID`. O Go não segue
redirect e usa timeout. Todos os schemas são estritos; campo desconhecido invalida a resposta.

## Conversa

`POST /v1/companion/respond`

```json
{
  "request_id": "uuid-da-mensagem",
  "conversation_id": "uuid-da-conversa",
  "user_id": "uuid-opaco",
  "message": "Mensagem atual",
  "history": [
    {
      "id": "uuid-opcional",
      "role": "user",
      "content": "Mensagem anterior",
      "created_at": "2026-08-19T12:00:00Z"
    }
  ],
  "locale_hint": "pt-BR",
  "country_code": "BR",
  "timezone": "America/Sao_Paulo"
}
```

`locale_hint`, `country_code`, `timezone`, IDs e datas do histórico são opcionais. Idioma e
país são conceitos independentes. Limites: mensagem 8.000 caracteres, 50 mensagens de histórico
e 60.000 caracteres no histórico.

Resposta `200`:

```json
{
  "content": "Resposta natural e segura.",
  "provider": "openai",
  "model": "gpt-5.6-terra",
  "prompt_version": "companion-v1",
  "blocked": false,
  "block_reason": null,
  "language": "pt-BR",
  "route": "normal",
  "graph_version": "conversation-graph-v1"
}
```

`route` é `normal`, `boundary`, `crisis` ou `security_block`. Rotas especiais retornam conteúdo
seguro, `blocked=true` e reason code técnico sem copiar o texto. Corpo máximo lido pelo Go: 64
KiB. Erros, timeout, JSON inválido e resposta vazia viram indisponibilidade genérica; conteúdo
interno nunca é repassado ao frontend.

## Relatório de Contexto e Jornada

`POST /v1/context/process`

O request contém `request_id`, `connection_id`, `user_id`, `period_start`, `period_end`, até 500
mensagens e no máximo 300 mil caracteres. O período máximo é 31 dias. `source_locale` é opcional
e `target_locale` define o idioma do relatório.

Resposta `200`:

```json
{
  "schema_version": "journey-report-v1",
  "title": "Relatório de Contexto e Jornada",
  "coverage": {
    "conversation_count": 3,
    "user_message_count": 24,
    "active_day_count": 5,
    "completeness": "partial",
    "note": "Cobre apenas os assuntos mencionados no período."
  },
  "summary": "Panorama factual do período.",
  "timeline": [
    {
      "description": "Relatou mudança de responsabilidade no trabalho.",
      "occurred_at": "2026-08-14T10:00:00Z",
      "source_message_ids": ["uuid-da-mensagem"]
    }
  ],
  "items": [
    {
      "kind": "challenge",
      "title": "Pressão no trabalho",
      "description": "Relatou dificuldade para iniciar uma entrega.",
      "impact": "Descreveu autocobrança ao fim do dia.",
      "evidence_strength": "explicit_once",
      "occurred_at": "2026-08-14T10:00:00Z",
      "source_message_ids": ["uuid-da-mensagem"],
      "limitations": ["Não informou se a mudança é permanente."]
    }
  ],
  "limitations": ["Não houve conversa sobre sono no período."],
  "provider": "openai",
  "model": "gpt-5.6-terra",
  "prompt_version": "journey-report-v1",
  "graph_version": "journey-report-graph-v1"
}
```

Kinds: `priority`, `event`, `challenge`, `emotion`, `thought`, `behavior`, `strategy`, `support`,
`change`, `open_topic`, `safety_context`. Força documental: `explicit_once`,
`explicit_repeated`, `uncertain`, `contradictory`.

Somente mensagens `user` podem ser evidência. Toda timeline e todo item têm de citar entre 1 e
50 IDs recebidos. IDs do assistente/desconhecidos, timestamps fora do período, recorrência com
uma fonte ou afirmação diagnóstica invalidam o relatório inteiro. O Go cifra os campos sensíveis,
persiste o resultado como `pending_review` e só libera ao profissional após aprovação do paciente.

## Operação

O FastAPI é stateless e não é a fonte da verdade. O Go mantém idempotência de mensagens, jobs
duráveis, lease, backoff e consumo concorrente com `FOR UPDATE SKIP LOCKED`. O provider mock é o
padrão local. Produção deve usar HTTPS/mTLS, secret manager e `AI_OPENAI_STORE=false`; logs e
traces nunca podem conter mensagens, prompts ou respostas.
