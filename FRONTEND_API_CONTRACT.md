# Contrato backend → frontend

Atualizado em: 2026-08-18
Versão da API: `v1`
Status: autenticação, conversas, vínculo profissional–paciente e compartilhamento de contexto implementados.

Este arquivo é a fonte de verdade para o agente do frontend. Ele deve ser atualizado sempre que uma rota, payload, status HTTP ou regra de autenticação mudar.

## Convenções gerais

- Base local: `http://localhost:8080`
- API do usuário/paciente: `/v1/app`
- API do profissional: `/v1/professional`
- JSON usa `snake_case`.
- Datas usam RFC 3339 em UTC, por exemplo `2026-08-18T15:00:00Z`.
- Enviar `Content-Type: application/json` nos requests com corpo.
- O backend rejeita campos JSON desconhecidos.
- O corpo JSON tem limite de 1 MiB.
- Requests autenticados usam `Authorization: Bearer <access_token>`.
- Requests do browser devem usar `credentials: "include"` para receber/enviar o refresh cookie.

Resposta de erro padrão:

```json
{
  "error": {
    "code": "validation_failed",
    "message": "descrição segura para exibição ou tratamento"
  }
}
```

O frontend deve controlar fluxo por `error.code`, não comparar `message`.

Health check público: `GET /health` responde `200` com `{"status":"ok"}`. Ele confirma que o processo HTTP está ativo; um readiness check de dependências será adicionado antes do deploy.

## Modelo de autenticação

Existem duas contas e dois públicos independentes:

- `app`: usuário/paciente.
- `professional`: psicólogo, psiquiatra, psicanalista ou terapeuta.

Uma pessoa que use os dois aplicativos terá duas contas. Tokens e cookies de um público não autenticam o outro.

O access token é um JWT EdDSA curto. Guarde-o somente em memória. Não use `localStorage`, `sessionStorage` ou cookie criado por JavaScript.

O refresh token é opaco, rotativo e enviado pelo backend em cookie `HttpOnly`. O JavaScript não deve tentar lê-lo. Cookies:

- `anamnesys_app_refresh`
- `anamnesys_professional_refresh`

Ao recarregar a aplicação, chame a rota de refresh com `credentials: "include"` e mantenha o novo access token em memória.

Faça refresh em single-flight: se várias chamadas receberem `401` juntas, apenas uma chama `/refresh`; as demais aguardam o resultado. Se o refresh falhar, limpe o estado local e leve o usuário ao login.

## Tipos compartilhados

Token emitido:

```json
{
  "access_token": "eyJ...",
  "token_type": "Bearer",
  "expires_at": "2026-08-18T15:10:00Z"
}
```

O refresh token nunca aparece no JSON do browser; ele vem no `Set-Cookie`.

Sessão:

```json
{
  "id": "uuid",
  "created_ip": "127.0.0.1",
  "last_used_ip": "127.0.0.1",
  "user_agent": "Mozilla/5.0 ...",
  "mfa_verified": true,
  "expires_at": "2026-09-17T15:00:00Z",
  "last_used_at": "2026-08-18T15:00:00Z",
  "created_at": "2026-08-18T15:00:00Z",
  "revoked_at": null,
  "current_session": true
}
```

## Rotas comuns aos dois públicos

Substitua `{audience}` por `app` ou `professional`.

### Cadastro

`POST /v1/{audience}/auth/register`

```json
{
  "email": "nome@exemplo.com",
  "password": "uma senha longa com 12+ caracteres",
  "display_name": "Nome da Pessoa"
}
```

Regras atuais:

- senha entre 12 e 128 caracteres;
- e-mail normalizado para minúsculas, máximo de 254 caracteres;
- nome entre 1 e 120 caracteres.

Resposta `201`:

```json
{
  "account_id": "uuid",
  "email": "nome@exemplo.com",
  "verification_required": true,
  "development_token": "somente no ambiente development"
}
```

O `development_token` não existirá em staging/produção.

### Solicitar e confirmar e-mail

`POST /v1/{audience}/auth/email-verification/request`

```json
{ "email": "nome@exemplo.com" }
```

Resposta sempre genérica `202`, exista ou não a conta:

```json
{
  "message": "if the account exists, instructions will be sent",
  "development_token": "somente em development"
}
```

`POST /v1/{audience}/auth/email-verification/confirm`

```json
{ "token": "token-recebido-por-email" }
```

Resposta: `204 No Content`.

### Login

`POST /v1/{audience}/auth/login`

```json
{
  "email": "nome@exemplo.com",
  "password": "uma senha longa com 12+ caracteres"
}
```

Para `app`, resposta `200`:

```json
{
  "tokens": {
    "access_token": "eyJ...",
    "token_type": "Bearer",
    "expires_at": "2026-08-18T15:10:00Z"
  },
  "passkey_required": false
}
```

Para `professional`, a resposta é uma união de estados descrita na seção de passkeys.

### Refresh

`POST /v1/{audience}/auth/refresh`

Browser: corpo vazio e `credentials: "include"`.

Resposta `200`: objeto de token emitido, sem wrapper `tokens`. O cookie é rotacionado no mesmo response.

Nunca repita um refresh token antigo. Se o backend detectar reutilização, revoga toda a família daquela sessão.

### Logout

`POST /v1/{audience}/auth/logout`

Requer access token. Resposta `204`; revoga a família da sessão atual e remove o cookie.

`POST /v1/{audience}/auth/logout-all`

Requer access token. Resposta `204`; revoga todas as sessões da conta e remove o cookie local.

### Recuperação de senha

`POST /v1/{audience}/auth/password-reset/request`

```json
{ "email": "nome@exemplo.com" }
```

Resposta genérica `202`, no mesmo formato da solicitação de verificação de e-mail.

`POST /v1/{audience}/auth/password-reset/confirm`

```json
{
  "token": "token-recebido-por-email",
  "new_password": "nova senha longa com 12+ caracteres"
}
```

Resposta `204`. Todas as sessões anteriores são revogadas.

### Conta atual e sessões

`GET /v1/{audience}/me`

Resposta `200`:

```json
{
  "id": "uuid",
  "email": "nome@exemplo.com",
  "display_name": "Nome da Pessoa",
  "status": "active",
  "email_verified_at": "2026-08-18T15:00:00Z",
  "audience": "professional",
  "mfa_verified": true
}
```

`GET /v1/{audience}/auth/sessions`

```json
{ "sessions": [] }
```

`DELETE /v1/{audience}/auth/sessions/{sessionID}`

Resposta `204`. O backend só permite revogar uma sessão pertencente à própria conta e público.

## Passkeys dos profissionais

Passkeys são o segundo fator dos profissionais. O navegador usa WebAuthn; biometria, reconhecimento facial, PIN do dispositivo, chave física e aprovação por celular são decididos pelo sistema operacional/navegador.

Não implemente captura própria de biometria. O frontend apenas entrega as opções ao WebAuthn e devolve a credencial resultante ao backend.

Biblioteca recomendada no frontend web: `@simplewebauthn/browser`. Ela converte corretamente os campos binários base64url.

```ts
import { startAuthentication, startRegistration } from "@simplewebauthn/browser";
```

`public_key` deve ser tratado como payload opaco produzido pelo backend.

### Primeiro login do profissional, ainda sem passkey

Após senha correta, `POST /v1/professional/auth/login` retorna:

```json
{
  "tokens": {
    "access_token": "eyJ...",
    "token_type": "Bearer",
    "expires_at": "2026-08-18T15:10:00Z"
  },
  "passkey_required": false,
  "passkey_enrollment_needed": true
}
```

O frontend deve levar o profissional diretamente ao cadastro da primeira passkey. Esse access token tem `mfa=false` e não deve liberar recursos profissionais sensíveis.

### Cadastrar passkey

1. `POST /v1/professional/auth/passkeys/registration/options`

Requer access token profissional. Corpo vazio.

Resposta `200`:

```json
{
  "ceremony_token": "token-opaco-de-uso-unico",
  "public_key": {
    "challenge": "...",
    "rp": {},
    "user": {},
    "pubKeyCredParams": [],
    "timeout": 300000
  }
}
```

2. Criar a credencial no browser:

```ts
const credential = await startRegistration({
  optionsJSON: optionsResponse.public_key,
});
```

3. `POST /v1/professional/auth/passkeys/registration/verify`

```json
{
  "ceremony_token": "o-mesmo-token-da-etapa-1",
  "label": "MacBook pessoal",
  "credential": {
    "id": "...",
    "rawId": "...",
    "response": {},
    "type": "public-key",
    "clientExtensionResults": {}
  }
}
```

Envie em `credential` exatamente o objeto retornado pela biblioteca.

Resposta `201`:

```json
{
  "passkey": {
    "id": "uuid",
    "label": "MacBook pessoal",
    "created_at": "2026-08-18T15:00:00Z"
  },
  "recovery_codes": [
    "ABCDEFGH-JKLMNPQR"
  ],
  "access_token": "eyJ...",
  "token_type": "Bearer",
  "expires_at": "2026-08-18T15:10:00Z"
}
```

Troque imediatamente o access token em memória pelo retornado: ele possui `mfa=true`. O refresh cookie atual passa a representar a sessão verificada.

`recovery_codes` só é retornado no cadastro da primeira passkey. Mostre-os uma única vez, peça que o profissional os salve fora do aplicativo e não os persista no frontend.

Ao cadastrar a primeira passkey, o backend revoga outras sessões profissionais que haviam provado somente a senha.

Cada profissional pode manter no máximo 10 passkeys ativas.

### Login profissional com passkey existente

Após senha correta, o login retorna `200` sem tokens:

```json
{
  "passkey_required": true,
  "passkey_ceremony": {
    "ceremony_token": "token-opaco-de-uso-unico",
    "public_key": {
      "challenge": "...",
      "allowCredentials": [],
      "timeout": 300000,
      "userVerification": "required"
    }
  }
}
```

Execute:

```ts
const credential = await startAuthentication({
  optionsJSON: loginResponse.passkey_ceremony.public_key,
});
```

Depois chame `POST /v1/professional/auth/passkeys/authentication/verify`:

```json
{
  "ceremony_token": "token-opaco-de-uso-unico",
  "credential": {
    "id": "...",
    "rawId": "...",
    "response": {},
    "type": "public-key",
    "clientExtensionResults": {}
  }
}
```

Resposta `200`: objeto de token emitido. O refresh cookie profissional também será criado.

Se o navegador oferecer “usar outro dispositivo”, poderá mostrar um QR code para aprovação no celular. Isso é um fluxo WebAuthn híbrido nativo; o frontend não cria o QR code.

### Entrar com código de recuperação

Só ofereça esta opção depois que o login com senha retornar `passkey_required=true`.

`POST /v1/professional/auth/passkeys/authentication/recovery`

```json
{
  "ceremony_token": "token-da-cerimonia-de-login",
  "recovery_code": "ABCDEFGH-JKLMNPQR"
}
```

Resposta `200`: objeto de token emitido e refresh cookie profissional. Cada código funciona uma única vez.

### Listar, remover e recuperar

`GET /v1/professional/auth/passkeys`

```json
{
  "passkeys": [
    {
      "id": "uuid",
      "label": "MacBook pessoal",
      "created_at": "2026-08-18T15:00:00Z",
      "last_used_at": "2026-08-18T16:00:00Z"
    }
  ]
}
```

`DELETE /v1/professional/auth/passkeys/{passkeyID}`

Requer sessão com `mfa=true`. Resposta `204`. A última passkey não pode ser removida; nesse caso retorna `409` com `last_passkey`.

`POST /v1/professional/auth/passkeys/recovery-codes/regenerate`

Requer sessão com `mfa=true`. Corpo vazio.

```json
{
  "recovery_codes": ["ABCDEFGH-JKLMNPQR"]
}
```

Todos os códigos anteriores são invalidados. Mostre os novos códigos uma única vez.

## Códigos de erro relevantes

| HTTP | `error.code` | Tratamento esperado |
|---:|---|---|
| 400 | `invalid_json` | Corrigir serialização/payload. |
| 401 | `invalid_credentials` | E-mail ou senha incorretos. |
| 401 | `invalid_access_token` | Tentar refresh uma vez; depois deslogar. |
| 401 | `invalid_token` | Cerimônia/token inválido, expirado ou reutilizado. Reiniciar o fluxo. |
| 401 | `invalid_recovery_code` | Código inválido, já usado ou expirado. |
| 403 | `email_not_verified` | Abrir fluxo de confirmação do e-mail. |
| 403 | `account_unavailable` | Bloquear entrada e orientar suporte. |
| 403 | `mfa_required` | Exigir autenticação forte/passkey. |
| 403 | `consent_required` | Exibir e aceitar as versões atuais dos termos, privacidade e processamento por IA. |
| 403 | `forbidden` | Operação não permitida no estado atual. |
| 403 | `origin_not_allowed` | Erro de configuração da origem do frontend. |
| 404 | `not_found` | Recurso inexistente ou não pertencente à conta. |
| 409 | `account_exists` | E-mail já cadastrado naquele público. |
| 409 | `passkey_exists` | Credencial já cadastrada. |
| 409 | `last_passkey` | Cadastrar outra passkey antes de remover. |
| 409 | `passkey_limit` | Remover uma passkey antiga antes de cadastrar outra. |
| 409 | `state_conflict` | Atualizar os dados; o recurso mudou ou não aceita a operação no estado atual. |
| 422 | `validation_failed` | Mostrar erros de validação. |
| 429 | `rate_limited` | Respeitar `Retry-After`; não repetir automaticamente em loop. |
| 500 | `internal_error` | Mostrar erro genérico e registrar observabilidade no frontend. |

## Configuração WebAuthn por ambiente

As origens precisam coincidir exatamente, incluindo esquema e porta.

Desenvolvimento atual:

- RP ID: `localhost`
- Origin: `http://localhost:3000`
- API: `http://localhost:8080`

Produção exige HTTPS. O RP ID é somente o domínio, sem `https://`, porta ou caminho. Exemplo:

- frontend: `https://app.anamnesys.com.br`
- RP ID: `anamnesys.com.br`
- origin permitida: `https://app.anamnesys.com.br`

Defina `AUTH_COOKIE_SECURE=true` em produção. Prefira frontend e API no mesmo site registrável para manter o refresh cookie `SameSite=Lax` sem abrir uma política cross-site mais fraca.

## Consentimentos do app

O usuário precisa aceitar as versões atuais de `terms`, `privacy` e `ai_processing` antes de enviar mensagens ao companion. O backend escolhe a versão vigente; o frontend não envia nem inventa `policy_version`.

`GET /v1/app/consents`

Resposta `200`:

```json
{
  "consents": [
    {
      "type": "ai_processing",
      "policy_version": "2026-08-18",
      "granted_at": "2026-08-18T16:00:00Z"
    }
  ]
}
```

`POST /v1/app/consents`

```json
{
  "consent_types": ["terms", "privacy", "ai_processing"]
}
```

Resposta `200`: objeto `consents` com todos os consentimentos ativos.

`DELETE /v1/app/consents/{consentType}`

Resposta `204`. Revogar qualquer consentimento obrigatório impede novas mensagens. O histórico já armazenado não é apagado por esta rota; exclusão e retenção terão fluxo próprio.

## Conversas do companion

Todo conteúdo de título e mensagem é cifrado pelo backend antes de chegar ao PostgreSQL. O frontend recebe texto normal após autorização.

### Criar e listar conversas

`POST /v1/app/conversations`

```json
{ "title": "Minha semana" }
```

`title` é opcional, possui no máximo 120 caracteres e assume `Nova conversa` quando vazio.

Resposta `201`:

```json
{
  "id": "uuid",
  "title": "Minha semana",
  "status": "active",
  "created_at": "2026-08-18T16:00:00Z",
  "updated_at": "2026-08-18T16:00:00Z"
}
```

`GET /v1/app/conversations`

Resposta `200` com até 50 conversas recentes:

```json
{ "conversations": [] }
```

`DELETE /v1/app/conversations/{conversationID}`

Resposta `204`. É um arquivamento lógico; mensagens não são apagadas.

### Listar mensagens

`GET /v1/app/conversations/{conversationID}/messages?limit=50&before_sequence=120`

- `limit`: opcional, padrão 50, máximo 100.
- `before_sequence`: opcional; implementa paginação para trás.
- A resposta sempre vem em ordem cronológica crescente.

```json
{
  "messages": [
    {
      "id": "uuid",
      "conversation_id": "uuid",
      "sequence": 1,
      "role": "user",
      "content": "Hoje foi um dia difícil.",
      "generation_status": "completed",
      "created_at": "2026-08-18T16:00:00Z"
    },
    {
      "id": "uuid",
      "conversation_id": "uuid",
      "sequence": 2,
      "role": "assistant",
      "content": "Quer me contar o que tornou o dia difícil?",
      "in_reply_to_message_id": "uuid-da-mensagem-do-usuario",
      "generation_status": "completed",
      "ai_provider": "openai",
      "ai_model": "modelo-configurado-no-fastapi",
      "prompt_version": "companion-v1",
      "created_at": "2026-08-18T16:00:01Z"
    }
  ]
}
```

### Enviar mensagem

`POST /v1/app/conversations/{conversationID}/messages`

Header obrigatório:

```http
Idempotency-Key: 550e8400-e29b-41d4-a716-446655440000
```

Gere uma chave UUID nova para cada ação de envio e reutilize a mesma chave em retries de rede. O texto possui limite de 8.000 caracteres.

```json
{ "content": "Hoje foi um dia difícil." }
```

Quando o FastAPI responde, status `201`:

```json
{
  "user_message": {},
  "assistant_message": {},
  "assistant_status": "completed"
}
```

`assistant_status` pode ser:

- `completed`: resposta normal;
- `blocked`: o serviço de safety bloqueou a geração e devolveu uma mensagem segura;
- `failed`: FastAPI indisponível ou resposta inválida;
- `pending`: já existe uma geração recente para a mesma mensagem.

Se a IA não estiver disponível, a mensagem do usuário continua salva e a API responde `202`:

```json
{
  "user_message": {
    "id": "uuid",
    "generation_status": "failed",
    "failure_code": "companion_unavailable"
  },
  "assistant_status": "failed"
}
```

Não mostre `failure_code` diretamente ao usuário. Exiba uma ação amigável como “Não consegui responder agora. Tentar novamente”.

### Repetir geração

`POST /v1/app/messages/{userMessageID}/retry`

Corpo vazio. Use somente o ID de uma mensagem com `role=user`.

- `200`: geração concluída ou já concluída anteriormente;
- `202`: ainda pendente ou falhou novamente;
- `409 state_conflict`: a mensagem não pode ser retomada naquele estado.

Uma geração `pending` só pode ser reclamada por retry depois de 60 segundos. Isso evita chamadas duplicadas ao modelo e também recupera mensagens interrompidas por reinício do backend.

## Fluxo profissional e vínculo com paciente

Todas as rotas profissionais abaixo exigem access token `professional` e sessão com MFA verificado. Se faltar MFA, a resposta é `403 mfa_required`.

### Perfil e onboarding profissional

`GET /v1/professional/profile` retorna o perfil, organização solo, membership e plano. Antes do onboarding retorna `404 not_found`.

`PUT /v1/professional/profile` cria ou atualiza o perfil e, na primeira chamada, provisiona atomicamente a organização solo, membership de owner e assinatura trial `single`:

```json
{
  "profession_type": "psychologist",
  "registration_country_code": "BR",
  "registration_region": "SP",
  "registration_number": "06/123456",
  "bio": "Apresentação profissional",
  "certifications": ["Formação em terapia cognitivo-comportamental"]
}
```

`profession_type`: `psychologist`, `psychiatrist`, `psychoanalyst`, `therapist`, `psychotherapist`, `occupational_therapist`, `counselor` ou `other`. `bio` tem até 2.000 caracteres; são permitidas até 50 certificações de 200 caracteres cada.

### Convites

- `POST /v1/professional/invitations` com `{"email":"paciente@exemplo.com"}` retorna `201` e o `invitation_token`. O frontend monta seu próprio link, por exemplo `/convite/{invitation_token}`.
- `GET /v1/professional/invitations` retorna `{"invitations": [...]}`.
- `DELETE /v1/professional/invitations/{invitationID}` retorna `204`.
- `GET /v1/app/invitations/{token}` é público e retorna somente nome/profissão/organização, e-mail mascarado e expiração.
- `POST /v1/app/invitations/{token}/accept` exige conta `app` autenticada com o mesmo e-mail do convite.

Aceite:

```json
{
  "consent_scopes": ["summaries", "events", "marked_topics"]
}
```

Os escopos possíveis são `summaries`, `events` e `marked_topics`. O backend deliberadamente não permite compartilhar mensagens brutas. A resposta `201` contém `connection_id` e `status: "active"`.

### Conexões e dashboard

App/paciente:

- `GET /v1/app/connections` → `{"connections": [...]}`;
- `PUT /v1/app/connections/{connectionID}/consents` substitui todos os escopos atuais e retorna `204`;
- `DELETE /v1/app/connections/{connectionID}` encerra o vínculo e revoga consentimentos, retornando `204`.

Profissional:

- `GET /v1/professional/patients` → `{"patients": [...]}`;
- `GET /v1/professional/patients/{connectionID}` → detalhes do paciente e consentimentos atuais;
- `POST /v1/professional/patients/{connectionID}/end` encerra o vínculo e retorna `204`.

O objeto de conexão inclui IDs, status, organização, nome/e-mail do paciente, nome/profissão do profissional, `consent_scopes`, `activated_at`, `ended_at` e `created_at`.

## Relatórios de Contexto e Jornada

O profissional não acessa o histórico do chatbot. O Go envia o período autorizado ao serviço de IA e cifra o relatório estruturado. Todo relatório precisa ser revisado pelo paciente antes de aparecer para o profissional.

`POST /v1/professional/patients/{connectionID}/contexts`

```json
{
  "period_start": "2026-08-11T00:00:00Z",
  "period_end": "2026-08-18T00:00:00Z"
}
```

Regras: período posterior à ativação do vínculo, no máximo 31 dias e consentimento vigente para `summaries`. A rota não espera a IA: retorna sempre `202` depois de enfileirar o trabalho.

```json
{
  "job_id": "uuid",
  "status": "queued"
}
```

Consulte `GET /v1/professional/context-jobs/{jobID}` até atingir um estado terminal:

```json
{
  "id": "uuid",
  "connection_id": "uuid",
  "period_start": "2026-08-11T00:00:00Z",
  "period_end": "2026-08-18T00:00:00Z",
  "status": "processing",
  "attempt_count": 1,
  "created_at": "2026-08-19T12:00:00Z",
  "updated_at": "2026-08-19T12:00:01Z"
}
```

Estados: `queued`, `processing`, `completed` ou `failed`. `completed` significa que o relatório está pronto para revisão do paciente; ainda não significa que o profissional pode lê-lo.

Paciente:

- `GET /v1/app/context-reports` retorna relatórios pendentes, aprovados e rejeitados;
- `POST /v1/app/context-reports/{reportID}/review` aprova ou rejeita;
- na aprovação, `excluded_item_ids` remove itens que o paciente não quer compartilhar.

```json
{
  "decision": "approved",
  "excluded_item_ids": ["uuid-do-item"],
  "excluded_timeline_entry_ids": ["uuid-da-timeline"]
}
```

`decision` aceita `approved` ou `rejected`. Rejeição não aceita exclusões. A operação retorna `204` e só pode ser realizada uma vez enquanto o status for `pending_review`.

`GET /v1/professional/patients/{connectionID}/contexts` retorna:

```json
{
  "contexts": [
    {
      "id": "uuid",
      "connection_id": "uuid",
      "schema_version": "journey-report-v1",
      "title": "Relatório de Contexto e Jornada",
      "period_start": "2026-08-11T00:00:00Z",
      "period_end": "2026-08-18T00:00:00Z",
      "coverage": {
        "conversation_count": 3,
        "user_message_count": 24,
        "active_day_count": 5,
        "completeness": "partial",
        "note": "Cobre apenas os assuntos mencionados."
      },
      "summary": "Panorama factual do período",
      "timeline": [
        {
          "id": "uuid",
          "description": "Relatou uma nova responsabilidade no trabalho.",
          "occurred_at": "2026-08-14T10:00:00Z"
        }
      ],
      "items": [
        {
          "id": "uuid",
          "kind": "challenge",
          "title": "Pressão no trabalho",
          "description": "Relatou dificuldade para iniciar uma entrega.",
          "impact": "Descreveu autocobrança ao fim do dia.",
          "evidence_strength": "explicit_once",
          "limitations": [],
          "included": true
        }
      ],
      "limitations": [],
      "provider": "openai",
      "model": "gpt-5.6-terra",
      "prompt_version": "journey-report-v1",
      "graph_version": "journey-report-graph-v1",
      "review_status": "approved",
      "reviewed_at": "2026-08-18T00:05:00Z",
      "created_at": "2026-08-18T00:00:01Z"
    }
  ]
}
```

O endpoint profissional retorna somente relatórios `approved` e itens `included=true`. IDs das mensagens-fonte são guardados para rastreabilidade interna, mas nunca enviados ao profissional ou ao paciente.

Erros imediatos específicos: `403 context_consent_required` e `409 context_processing`. Período sem mensagens, limite de 500 mensagens, indisponibilidade da IA e respostas inválidas são tratados pelo worker e aparecem como job `failed` após a política de tentativas.

## Fora do MVP funcional atual

Cobrança real, gestão de clínicas/membros, upload e verificação documental, notificações e preferências ainda não possuem rotas. Os planos e a estrutura de organização já existem no domínio para essas próximas etapas.
