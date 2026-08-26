-- +goose Up

-- Check-in diário: o profissional autora um questionário de escala, envia a um
-- vínculo, e o paciente responde uma vez por dia. Nada sai do aparelho do
-- paciente sem uma autorização explícita — a colheita é um pedido separado,
-- com o mesmo desenho das solicitações de contexto.
--
-- Texto autoral (título, legenda, enunciado, rótulo de alternativa) é cifrado
-- em repouso, como toda linguagem humana neste banco. O `score` fica em claro
-- de propósito: um inteiro de 1 a 5 é ininteligível sem o enunciado, e é o
-- que permite agregar sem decifrar linha a linha.
--
-- A escala é fixa: cinco alternativas, notas de 1 a 5, sempre. O profissional
-- escreve os cinco rótulos, do menor para o maior. Escala uniforme é o que
-- permite que perguntas diferentes dividam os mesmos eixos de um radar sem
-- que a forma minta sobre a proporção.

CREATE TABLE checkin_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    professional_membership_id UUID NOT NULL,
    title_ciphertext BYTEA NOT NULL,
    legend_ciphertext BYTEA,
    status TEXT NOT NULL DEFAULT 'draft',
    published_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT checkin_templates_membership_fk
        FOREIGN KEY (professional_membership_id, organization_id)
        REFERENCES organization_memberships (id, organization_id)
        ON DELETE RESTRICT,
    CONSTRAINT checkin_templates_status_check
        CHECK (status IN ('draft', 'published', 'archived')),
    CONSTRAINT checkin_templates_title_ciphertext_length_check
        CHECK (octet_length(title_ciphertext) BETWEEN 1 AND 4096),
    CONSTRAINT checkin_templates_legend_ciphertext_length_check
        CHECK (legend_ciphertext IS NULL OR octet_length(legend_ciphertext) BETWEEN 1 AND 8192),
    CONSTRAINT checkin_templates_published_at_check
        CHECK ((status IN ('published', 'archived')) = (published_at IS NOT NULL)),
    CONSTRAINT checkin_templates_archived_at_check
        CHECK ((status = 'archived') = (archived_at IS NOT NULL))
);

CREATE INDEX checkin_templates_membership_idx
    ON checkin_templates (professional_membership_id, status, created_at DESC);

-- Um template publicado é imutável: editar cria outro. Sem isso, mudar um
-- enunciado reescreveria o significado de todo histórico já respondido.
CREATE TABLE checkin_template_questions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id UUID NOT NULL
        REFERENCES checkin_templates (id) ON DELETE CASCADE,
    position SMALLINT NOT NULL,
    prompt_ciphertext BYTEA NOT NULL,
    legend_ciphertext BYTEA,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT checkin_template_questions_position_check
        CHECK (position BETWEEN 1 AND 12),
    CONSTRAINT checkin_template_questions_prompt_ciphertext_length_check
        CHECK (octet_length(prompt_ciphertext) BETWEEN 1 AND 8192),
    CONSTRAINT checkin_template_questions_legend_ciphertext_length_check
        CHECK (legend_ciphertext IS NULL OR octet_length(legend_ciphertext) BETWEEN 1 AND 8192),
    CONSTRAINT checkin_template_questions_position_unique
        UNIQUE (template_id, position)
);

CREATE TABLE checkin_template_options (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    question_id UUID NOT NULL
        REFERENCES checkin_template_questions (id) ON DELETE CASCADE,
    position SMALLINT NOT NULL,
    label_ciphertext BYTEA NOT NULL,
    score SMALLINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT checkin_template_options_position_check
        CHECK (position BETWEEN 1 AND 5),
    -- A nota é a posição. Guardá-la explicitamente mantém a resposta legível
    -- sozinha, mas ela nunca pode divergir da ordem exibida.
    CONSTRAINT checkin_template_options_score_check
        CHECK (score = position),
    CONSTRAINT checkin_template_options_label_ciphertext_length_check
        CHECK (octet_length(label_ciphertext) BETWEEN 1 AND 4096),
    CONSTRAINT checkin_template_options_position_unique
        UNIQUE (question_id, position),
    CONSTRAINT checkin_template_options_id_question_unique
        UNIQUE (id, question_id)
);

CREATE TABLE checkin_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id UUID NOT NULL
        REFERENCES professional_patient_connections (id) ON DELETE RESTRICT,
    template_id UUID NOT NULL
        REFERENCES checkin_templates (id) ON DELETE RESTRICT,
    assigned_by_professional_user_id UUID NOT NULL
        REFERENCES professional_users (id) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'pending',
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    responded_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT checkin_assignments_status_check
        CHECK (status IN ('pending', 'active', 'declined', 'revoked', 'ended')),
    CONSTRAINT checkin_assignments_pending_check
        CHECK ((status = 'pending') = (responded_at IS NULL AND ended_at IS NULL)),
    CONSTRAINT checkin_assignments_active_check
        CHECK (status <> 'active' OR (responded_at IS NOT NULL AND ended_at IS NULL)),
    CONSTRAINT checkin_assignments_closed_check
        CHECK ((status IN ('declined', 'revoked', 'ended')) = (ended_at IS NOT NULL))
);

CREATE UNIQUE INDEX checkin_assignments_open_unique
    ON checkin_assignments (connection_id, template_id)
    WHERE status IN ('pending', 'active');

CREATE INDEX checkin_assignments_connection_idx
    ON checkin_assignments (connection_id, status, requested_at DESC);

-- Uma resposta por dia local. `entry_date` é DATE, não timestamp: o dia do
-- paciente é o dado, e o fuso de quem lê não pode deslocá-lo.
CREATE TABLE checkin_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    assignment_id UUID NOT NULL
        REFERENCES checkin_assignments (id) ON DELETE RESTRICT,
    entry_date DATE NOT NULL,
    client_request_hash CHAR(64),
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT checkin_entries_client_request_hash_check
        CHECK (client_request_hash IS NULL OR client_request_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT checkin_entries_date_unique UNIQUE (assignment_id, entry_date)
);

CREATE INDEX checkin_entries_assignment_period_idx
    ON checkin_entries (assignment_id, entry_date DESC);

CREATE TABLE checkin_entry_answers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entry_id UUID NOT NULL
        REFERENCES checkin_entries (id) ON DELETE CASCADE,
    question_id UUID NOT NULL
        REFERENCES checkin_template_questions (id) ON DELETE RESTRICT,
    option_id UUID NOT NULL,
    score SMALLINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- A alternativa precisa pertencer à pergunta respondida: sem a chave
    -- composta, uma resposta poderia apontar para a escala de outra pergunta.
    CONSTRAINT checkin_entry_answers_option_fk
        FOREIGN KEY (option_id, question_id)
        REFERENCES checkin_template_options (id, question_id)
        ON DELETE RESTRICT,
    CONSTRAINT checkin_entry_answers_score_check
        CHECK (score BETWEEN 1 AND 5),
    CONSTRAINT checkin_entry_answers_question_unique
        UNIQUE (entry_id, question_id)
);

CREATE TABLE checkin_collection_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id UUID NOT NULL
        REFERENCES professional_patient_connections (id) ON DELETE RESTRICT,
    requested_by_professional_user_id UUID NOT NULL
        REFERENCES professional_users (id) ON DELETE RESTRICT,
    period_start DATE NOT NULL,
    period_end DATE NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    responded_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT checkin_collection_requests_period_check
        CHECK (period_end >= period_start),
    CONSTRAINT checkin_collection_requests_period_length_check
        CHECK (period_end - period_start <= 92),
    CONSTRAINT checkin_collection_requests_status_check
        CHECK (status IN ('pending', 'sent', 'declined', 'expired')),
    CONSTRAINT checkin_collection_requests_responded_at_check
        CHECK ((status = 'pending') = (responded_at IS NULL))
);

-- Um pedido aberto por vínculo: o paciente não acumula fila de decisões.
CREATE UNIQUE INDEX checkin_collection_requests_open_unique
    ON checkin_collection_requests (connection_id)
    WHERE status = 'pending';

CREATE INDEX checkin_collection_requests_connection_idx
    ON checkin_collection_requests (connection_id, requested_at DESC);

-- O que o profissional lê é este retrato, calculado e congelado no aceite.
-- Se o paciente depois apagar um check-in ou corrigir um dia, o que ele
-- autorizou entregar continua sendo exatamente o que foi entregue.
CREATE TABLE checkin_collections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id UUID NOT NULL UNIQUE
        REFERENCES checkin_collection_requests (id) ON DELETE RESTRICT,
    connection_id UUID NOT NULL
        REFERENCES professional_patient_connections (id) ON DELETE RESTRICT,
    period_start DATE NOT NULL,
    period_end DATE NOT NULL,
    checkin_count SMALLINT NOT NULL,
    payload_ciphertext BYTEA NOT NULL,
    shared_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT checkin_collections_period_check
        CHECK (period_end >= period_start),
    CONSTRAINT checkin_collections_count_check
        CHECK (checkin_count BETWEEN 1 AND 10),
    CONSTRAINT checkin_collections_payload_ciphertext_length_check
        CHECK (octet_length(payload_ciphertext) BETWEEN 1 AND 1048576)
);

CREATE INDEX checkin_collections_connection_idx
    ON checkin_collections (connection_id, shared_at DESC);

-- +goose Down

DROP TABLE IF EXISTS checkin_collections;
DROP TABLE IF EXISTS checkin_collection_requests;
DROP TABLE IF EXISTS checkin_entry_answers;
DROP TABLE IF EXISTS checkin_entries;
DROP TABLE IF EXISTS checkin_assignments;
DROP TABLE IF EXISTS checkin_template_options;
DROP TABLE IF EXISTS checkin_template_questions;
DROP TABLE IF EXISTS checkin_templates;
