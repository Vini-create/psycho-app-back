# Contrato backend → frontend

Atualizado em: 2026-08-24
Versão da API: `v1`
Status: autenticação, conversas com IA, renomeação/exclusão de conversa, vínculo profissional–paciente e fluxo autorizado de Relatórios de Contexto e Jornada implementados.

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

### Login com Google

Primeiro crie um nonce de uso único com `POST /v1/{audience}/auth/google/challenge`.

```json
{
  "challenge_id": "uuid",
  "nonce": "nonce-para-o-Google-Identity-Services",
  "expires_at": "2026-08-24T15:05:00Z"
}
```

Passe o `nonce` ao Google Identity Services e envie o ID token retornado para
`POST /v1/{audience}/auth/google`:

```json
{
  "challenge_id": "uuid",
  "credential": "id-token-assinado-pelo-google"
}
```

A resposta `200` tem a mesma união do login com senha. Para profissionais, uma
passkey já cadastrada continua obrigatória. O backend valida assinatura, `aud`,
`iss`, expiração, e-mail verificado e nonce; a identidade persistida usa o `sub`.

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
  "audience": "app",
  "mfa_verified": false,
  "created_at": "2026-08-18T14:00:00Z",
  "updated_at": "2026-08-18T15:00:00Z",
  "google_connected": true,
  "plan": "free"
}
```

`plan` existe para contas `app` e `professional`. Para pacientes, os códigos
vigentes são `free` e `plus`. Durante a validação inicial, toda conta
profissional recebe `pro`; o backend continua sendo a autoridade sobre as
permissões efetivas.

`PATCH /v1/{audience}/me`

```json
{ "display_name": "Nome da Pessoa" }
```

Resposta `200`: conta atualizada no mesmo formato de `GET /me`.

`PUT /v1/{audience}/auth/password`

```json
{
  "current_password": "senha atual",
  "new_password": "nova senha longa com 12+ caracteres"
}
```

Resposta `204`. As outras sessões da conta são revogadas; a sessão atual
permanece ativa. `401 invalid_credentials` indica senha atual incorreta e
`422 password_unchanged` indica que a nova senha é igual à atual.

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
    },
    "device_authorization": {
      "scan_token": "token-opaco-exclusivo-do-qr",
      "poll_token": "token-opaco-exclusivo-do-desktop",
      "confirmation_code": "482917",
      "expires_at": "2026-08-26T14:05:00Z"
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

### Autorizar o desktop pela passkey do celular

Quando o transporte híbrido nativo do navegador não estiver disponível, use os
dois portadores independentes de `device_authorization`:

1. O desktop monta uma URL da própria aplicação profissional no formato
   `/autorizar-dispositivo#<scan_token>` e gera o QR localmente. Use fragmento,
   não query string: o servidor da página e o cabeçalho `Referer` não recebem o
   token. O `poll_token` nunca entra no QR e permanece apenas em memória.
   Mostre `confirmation_code` no desktop.
2. O celular remove imediatamente o fragmento da barra e chama
   `POST /v1/professional/auth/device-authorizations/preview` com
   `{"scan_token":"..."}`. A resposta contém `public_key`,
   `confirmation_code` e `expires_at`. O celular deve pedir que a pessoa
   compare esse código com o desktop antes de autorizar.
3. Depois de `startAuthentication`, o celular chama
   `POST /v1/professional/auth/device-authorizations/approve` com o
   `scan_token` e a credencial WebAuthn. A resposta é `204`; nenhuma sessão é
   emitida para o celular.
4. A cada dois segundos, o desktop chama
   `POST /v1/professional/auth/device-authorizations/consume` com
   `{"poll_token":"..."}`. Enquanto aguarda, recebe `202` e
   `{"status":"pending"}`. Após aprovação, recebe `200` com o access token e
   o refresh cookie HttpOnly da nova sessão MFA.

O desafio expira junto com a cerimônia WebAuthn, cada portador tem 256 bits
aleatórios e tanto a aprovação quanto o consumo são atômicos e de uso único.
O QR não contém senha, passkey, token de sessão, e-mail ou identificador da
conta. A chave privada permanece no autenticador do celular.

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

Defina `AUTH_COOKIE_SECURE=true` em produção. Nesse modo, o refresh cookie usa
`SameSite=None; Secure; Partitioned` para funcionar durante a fase provisória
em que frontend e API estão em sites diferentes (`workers.dev` e
`railway.app`). CORS e a validação explícita de origem continuam obrigatórios.
Quando houver domínio próprio, prefira frontend e API no mesmo site
registrável.

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
      "prompt_version": "companion-v2",
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

O frontend não deve tomar decisões pelo conteúdo de `ai_provider`, `ai_model` ou
`prompt_version`. Esses campos existem para rastreabilidade. Uma resposta normal pode
registrar o modelo conversacional, enquanto uma resposta bloqueada pode registrar o
modelo auxiliar que tomou a decisão de segurança. Para o estado da interface, use
somente `assistant_status` e os status HTTP.

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

O workspace profissional (organização solo, membership de owner e plano Pro
ativo) é provisionado atomicamente no cadastro da conta. Contas anteriores à
migração também recebem esse workspace e o Pro ativo. O
`PUT /v1/professional/profile` cria ou atualiza somente os dados profissionais:

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

`profession_type`: `psychologist`, `psychiatrist`, `psychoanalyst`, `therapist`, `psychotherapist`, `occupational_therapist`, `counselor` ou `other`. País, região e número de registro são obrigatórios para concluir o onboarding. `bio` tem até 2.000 caracteres; são permitidas até 50 certificações de 200 caracteres cada.

O campo `plan` do perfil tem formato `{"code":"pro","status":"active"}`.

### Convites

- `POST /v1/professional/invitations` com `{"email":"paciente@exemplo.com"}` retorna `201`, `invitation_token` e `invitation_url`. O backend monta a URL completa com `EMAIL_PATIENT_APP_URL`; o frontend apenas exibe ou copia `invitation_url`.
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

O profissional não acessa o histórico do chatbot. Ele solicita um período, mas
essa ação não executa a IA. A geração começa somente quando o paciente confirma
a solicitação em Minha rede. O relatório estruturado é cifrado e fica disponível
exclusivamente no produto profissional.

### 1. Solicitação profissional

`POST /v1/professional/patients/{connectionID}/context-report-requests`

```json
{
  "period_start": "2026-08-11T00:00:00Z",
  "period_end": "2026-08-18T00:00:00Z"
}
```

Regras: vínculo ativo, período posterior à ativação do vínculo, no máximo 31 dias,
consentimento vigente para `summaries` e assinatura profissional `trialing` ou
`active`. Criar a solicitação não cria job e não envia conversas à IA. Retorna `201`:

```json
{
  "id": "uuid",
  "connection_id": "uuid",
  "period_start": "2026-08-11T00:00:00Z",
  "period_end": "2026-08-18T00:00:00Z",
  "status": "pending",
  "requested_at": "2026-08-19T12:00:00Z",
  "sent_at": null
}
```

`GET /v1/professional/patients/{connectionID}/context-report-requests` retorna
`{"requests": [...]}` em ordem decrescente de solicitação.

### 2. Confirmação do paciente

O paciente recebe apenas metadados da solicitação. Resumo, timeline, itens,
proveniência e conteúdo do relatório nunca são retornados pelo produto app.

`GET /v1/app/connections/{connectionID}/context-report-requests` retorna
`{"requests": [...]}` somente para o titular daquele vínculo.

`POST /v1/app/context-report-requests/{requestID}/send` não recebe corpo nem
período. O backend usa o período imutável definido pelo profissional, revalida
vínculo, consentimento e assinatura e cria o job de forma atômica. Retorna `202`:

```json
{
  "request_id": "uuid",
  "status": "processing"
}
```

Estados da solicitação: `pending`, `processing`, `sent`, `declined`, `expired` ou
`failed`. `sent_at` só existe em `sent`. Durante `processing`, o frontend pode
consultar novamente a listagem a cada cinco segundos. `sent` significa que o
relatório foi concluído e entregue ao profissional solicitante.

Não existem rotas app para listar, abrir, revisar ou gerar relatórios. Também não
existe rota profissional para gerar diretamente um relatório ou escolher outro
período depois da confirmação.

### 3. Leitura profissional

`GET /v1/professional/patients/{connectionID}/contexts` retorna:

```json
{
  "contexts": [
    {
      "id": "uuid",
      "connection_id": "uuid",
      "schema_version": "journey-report-v2",
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
          "kind": "emotion",
          "title": "Frustração durante a espera",
          "description": "Relatou ter ficado frustrado enquanto aguardava retorno.",
          "impact": "Descreveu ter continuado pensando no assunto ao fim do dia.",
          "evidence_strength": "explicit_once",
          "emotional_valence": "unpleasant",
          "limitations": [],
          "included": true
        }
      ],
      "limitations": [],
      "provider": "openai",
      "model": "gpt-5.6-terra",
      "prompt_version": "journey-report-v2",
      "graph_version": "journey-report-graph-v2",
      "review_status": "approved",
      "reviewed_at": "2026-08-18T00:05:00Z",
      "created_at": "2026-08-18T00:00:01Z"
    }
  ]
}
```

Em relatórios `journey-report-v2`, itens com `kind="emotion"` sempre incluem
`emotional_valence`: `pleasant`, `unpleasant`, `mixed` ou `neutral`.
O campo apenas organiza emoções explicitamente relatadas; não representa
intensidade, gravidade, risco, diagnóstico ou análise do estilo de escrita.
Itens de outros tipos omitem o campo. Relatórios v1 já persistidos continuam
válidos e podem não trazer `emotional_valence`.

O endpoint profissional retorna somente relatórios entregues, itens
`included=true` e entradas de timeline incluídas. Novos relatórios são persistidos
como `approved` porque a autorização ocorreu antes da geração; `review_status` e
`reviewed_at` permanecem no payload por compatibilidade com relatórios históricos.
IDs das mensagens-fonte são guardados para rastreabilidade interna, mas nunca são
enviados ao profissional ou ao paciente.

Fluxo esperado no frontend:

1. O profissional cria a solicitação com período fechado.
2. O paciente encontra a solicitação no vínculo e confirma o envio.
3. O backend revalida todas as autorizações e cria o job.
4. O worker gera e cifra o relatório.
5. A solicitação muda para `sent` e o relatório aparece apenas para o profissional.

Erros imediatos específicos: `402 subscription_required`,
`403 context_consent_required`, `409 profile_incomplete`, `409 context_request_conflict` e
`409 context_request_resolved`. Período sem mensagens, limite de 500 mensagens,
indisponibilidade da IA e respostas inválidas são tratados pelo worker e mudam a
solicitação para `failed` após a política de tentativas.

## Check-in diário

O profissional autora um questionário de escala, envia a um vínculo e o paciente
responde uma vez por dia. Nada é coletado sem dois aceites independentes: um
para passar a responder o check-in, outro para entregar as respostas de um
período. O profissional nunca lê resposta dia a dia por conta própria — o que
ele recebe é um retrato agregado, calculado no backend e congelado no momento
do aceite.

Todas as rotas profissionais exigem token com MFA, perfil completo e assinatura
`trialing` ou `active`. As rotas do paciente exigem apenas a conta `app`.

Datas de check-in trafegam como dia local (`YYYY-MM-DD`), nunca como timestamp:
o dia é do paciente, e o fuso de quem lê não pode deslocá-lo.

### 1. Modelos do profissional

`POST /v1/professional/checkin-templates`

```json
{
  "title": "Check-in diário",
  "legend": "Responda pensando no dia de hoje.",
  "questions": [
    {
      "prompt": "Como estava seu humor hoje?",
      "legend": "A primeira alternativa é o pior dia possível; a última, o melhor.",
      "options": [
        { "label": "Muito ruim" },
        { "label": "Ruim" },
        { "label": "Nem bom nem ruim" },
        { "label": "Bom" },
        { "label": "Muito bom" }
      ]
    }
  ]
}
```

**A escala é fixa: exatamente cinco alternativas por pergunta, notas de 1 a 5.**
A nota não vem no corpo — ela é a posição do rótulo, então a ordem importa: o
primeiro rótulo é o extremo mais baixo e o quinto é o mais alto. Escala uniforme
é o que permite perguntas diferentes dividirem os mesmos eixos de um radar sem
que a forma minta sobre a proporção, e tira do profissional uma decisão que ele
não tem por que tomar.

Limites restantes: 1–12 perguntas, título até 120 caracteres, legenda até 500,
enunciado até 200, legenda da pergunta até 300, rótulo até 60. A `legend` da
pergunta é o texto exibido ao paciente na hora de responder.

Retorna `201` com o modelo completo (`id`, `status`, perguntas e alternativas com
seus IDs). Um modelo nasce `draft`.

- `GET /v1/professional/checkin-templates` → `{"templates": [...]}` (não lista arquivados);
- `GET /v1/professional/checkin-templates/{templateID}` → o modelo;
- `PUT /v1/professional/checkin-templates/{templateID}` substitui o conteúdo, **apenas enquanto `draft`**;
- `DELETE /v1/professional/checkin-templates/{templateID}` arquiva e retorna `204`.

Enviar um modelo o publica, e um modelo publicado é imutável: já pode existir dia
respondido contra aquele enunciado, e alterá-lo reescreveria o significado do
histórico. Editar depois disso significa criar outro modelo. Tentar editar um
publicado devolve `409 checkin_template_published`.

### 2. Envio ao paciente

`POST /v1/professional/patients/{connectionID}/checkin-assignments`

```json
{ "template_id": "..." }
```

Retorna `201` com a atribuição em `pending`. Um vínculo aceita no máximo 5
check-ins abertos (`pending` + `active`); acima disso, `409 checkin_limit_reached`.
Enviar o mesmo modelo duas vezes ao mesmo vínculo com um já aberto devolve
`409 checkin_conflict`.

- `GET /v1/professional/patients/{connectionID}/checkin-assignments` → `{"assignments": [...]}`;
- `DELETE /v1/professional/patients/{connectionID}/checkin-assignments/{assignmentID}` revoga e retorna `204` — o check-in some do aplicativo do paciente na mesma hora.

A atribuição devolvida ao profissional traz status, datas e o modelo, mas
**nunca** `answered_today`, `today_entry` ou contagem de dias respondidos: isso é
resposta, e resposta só chega por colheita autorizada.

### 3. O check-in na vida do paciente

- `GET /v1/app/checkins?date=YYYY-MM-DD` → `{"checkins": [...]}` com os `active` de todos os vínculos. É o que a tela inicial usa;
- `GET /v1/app/connections/{connectionID}/checkin-assignments?status=pending,active` → `{"assignments": [...]}`;
- `POST /v1/app/checkin-assignments/{assignmentID}/accept` → `204`;
- `POST /v1/app/checkin-assignments/{assignmentID}/decline` → `204`;
- `DELETE /v1/app/checkin-assignments/{assignmentID}` → `204`, o paciente para de responder quando quiser.

Cada atribuição carrega `professional_display_name`, que é o rótulo obrigatório
na interface do paciente: ele pode ter mais de um check-in ativo, de
profissionais diferentes, e precisa saber para quem cada um responde.

Para os `active`, o backend devolve o estado do dia já resolvido:
`answered_today`, `today_entry` (com as respostas, para reabrir e corrigir),
`last_entry_date` e `answered_days`. O `date` da query é o dia local do
aparelho; sem ele, o backend usa o dia UTC.

Responder:

`POST /v1/app/checkins/{assignmentID}/entries` com `Idempotency-Key`

```json
{
  "entry_date": "2026-08-26",
  "answers": [
    { "question_id": "...", "option_id": "..." }
  ]
}
```

Regras: o check-in precisa estar `active` num vínculo `active`; **todas** as
perguntas do modelo precisam ser respondidas, uma vez cada; a alternativa precisa
pertencer à pergunta; e `entry_date` precisa estar a no máximo um dia do agora em
UTC — a janela cobre qualquer fuso do mundo sem deixar o cliente escolher a data
de um registro diário. Reenviar o mesmo dia corrige a resposta, não duplica.
Retorna `201` com o dia gravado.

`GET /v1/app/checkins/{assignmentID}/entries?from=&to=` devolve o histórico do
próprio paciente (janela máxima de 92 dias).

### 4. Colheita

`POST /v1/professional/patients/{connectionID}/checkin-collection-requests`

```json
{ "period_start": "2026-08-12", "period_end": "2026-08-26" }
```

Regras: período de no máximo 92 dias, posterior à ativação do vínculo e não
futuro. Só um pedido aberto por vínculo — o paciente não acumula fila de
decisões; um segundo devolve `409 checkin_conflict`. Retorna `201`.

- `GET /v1/professional/patients/{connectionID}/checkin-collection-requests` → `{"requests": [...]}`;
- `GET /v1/app/connections/{connectionID}/checkin-collection-requests` → o mesmo, do lado do paciente;
- `POST /v1/app/checkin-collection-requests/{requestID}/decline` → `204`.

O paciente decide **quais** check-ins entram:

`POST /v1/app/checkin-collection-requests/{requestID}/send`

```json
{ "assignment_ids": ["...", "..."] }
```

Aceita até 10 check-ins, de qualquer vínculo do próprio paciente. Sem nenhum dia
respondido no período, `422 checkin_no_entries` — um gráfico de zeros pareceria
relato de piora. Retorna `200` com `{"request_id", "status": "sent", "checkin_count"}`.

### 5. O que o profissional lê

`GET /v1/professional/patients/{connectionID}/checkin-collections` → `{"collections": [...]}`

```json
{
  "id": "...",
  "connection_id": "...",
  "request_id": "...",
  "period_start": "2026-08-12",
  "period_end": "2026-08-26",
  "shared_at": "2026-08-26T14:00:00Z",
  "checkins": [
    {
      "assignment_id": "...",
      "title": "Check-in diário",
      "legend": "Responda pensando no dia de hoje.",
      "authored_by_you": true,
      "period_day_count": 15,
      "answered_day_count": 11,
      "average": 2.4,
      "normalized": 0.6,
      "questions": [
        {
          "question_id": "...",
          "prompt": "Como estava seu humor hoje?",
          "position": 1,
          "average": 3.4,
          "normalized": 0.6,
          "score_min": 1,
          "score_max": 5,
          "answer_count": 11
        }
      ],
      "days": [
        { "date": "2026-08-12", "average": 2.5, "normalized": 0.63, "answer_count": 2 }
      ],
      "best_day": { "date": "2026-08-19", "average": 4, "normalized": 1, "answer_count": 2 },
      "worst_day": { "date": "2026-08-14", "average": 0.5, "normalized": 0.13, "answer_count": 2 }
    }
  ]
}
```

Pontos que o frontend precisa respeitar:

- **Todo número já vem calculado.** Média por pergunta, score do dia, melhor e
  pior dia e adesão são responsabilidade do backend. O app profissional desenha,
  não deriva.
- `normalized` (0 a 1) é a mesma média na régua de 0 a 1. Com a escala fixa de
  1 a 5 ela é sempre `(average - 1) / 4`; continua no payload porque é o valor
  que o desenho consome, e porque templates antigos podem ter outra escala.
  Use `average` com `score_min`/`score_max` para o rótulo textual.
- `authored_by_you` distingue o check-in que este profissional mandou de um que
  veio de outro acompanhamento. **Quem autorou o outro nunca é nomeado**: a
  existência de outro vínculo é informação do paciente, e ele não foi perguntado
  sobre revelá-la.
- Com um único dia respondido, `best_day` e `worst_day` apontam para o mesmo dia.
  A interface precisa dizer isso, não fingir que houve variação.
- O retrato é congelado no aceite. Se o paciente depois apagar o check-in ou
  corrigir um dia, o que já foi entregue continua exatamente como foi entregue.

### Erros específicos

`402 subscription_required`, `403 checkin_forbidden`, `409 profile_incomplete`,
`409 checkin_template_published`, `409 checkin_limit_reached`,
`409 checkin_conflict`, `409 checkin_request_resolved`,
`422 checkin_no_entries`, `422 validation_failed`.

## Fora do MVP funcional atual

Cobrança real, gestão de clínicas/membros, upload e verificação documental, notificações e preferências ainda não possuem rotas. Os planos e a estrutura de organização já existem no domínio para essas próximas etapas.
