-- +goose Up

CREATE TABLE app_consents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_user_id UUID NOT NULL REFERENCES app_users (id) ON DELETE RESTRICT,
    consent_type TEXT NOT NULL,
    policy_version TEXT NOT NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,
    ip_address INET,
    user_agent TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT app_consents_type_check
        CHECK (consent_type IN ('terms', 'privacy', 'ai_processing')),
    CONSTRAINT app_consents_policy_version_length_check
        CHECK (char_length(btrim(policy_version)) BETWEEN 1 AND 64),
    CONSTRAINT app_consents_user_agent_length_check
        CHECK (user_agent IS NULL OR char_length(user_agent) <= 1024),
    CONSTRAINT app_consents_revoked_at_check
        CHECK (revoked_at IS NULL OR revoked_at >= granted_at)
);

CREATE UNIQUE INDEX app_consents_active_type_unique
    ON app_consents (app_user_id, consent_type)
    WHERE revoked_at IS NULL;

CREATE INDEX app_consents_user_history_idx
    ON app_consents (app_user_id, created_at DESC);

CREATE TABLE chat_conversations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_user_id UUID NOT NULL REFERENCES app_users (id) ON DELETE RESTRICT,
    title_ciphertext BYTEA,
    status TEXT NOT NULL DEFAULT 'active',
    next_sequence BIGINT NOT NULL DEFAULT 1,
    last_message_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chat_conversations_title_ciphertext_length_check
        CHECK (title_ciphertext IS NULL OR octet_length(title_ciphertext) BETWEEN 1 AND 2048),
    CONSTRAINT chat_conversations_status_check
        CHECK (status IN ('active', 'archived')),
    CONSTRAINT chat_conversations_next_sequence_check
        CHECK (next_sequence > 0),
    CONSTRAINT chat_conversations_archived_at_check
        CHECK ((status = 'archived') = (archived_at IS NOT NULL))
);

CREATE INDEX chat_conversations_app_user_recent_idx
    ON chat_conversations (app_user_id, COALESCE(last_message_at, created_at) DESC);

CREATE TABLE chat_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES chat_conversations (id) ON DELETE RESTRICT,
    sequence BIGINT NOT NULL,
    role TEXT NOT NULL,
    content_ciphertext BYTEA NOT NULL,
    in_reply_to_message_id UUID REFERENCES chat_messages (id) ON DELETE RESTRICT,
    client_request_hash CHAR(64),
    generation_status TEXT NOT NULL,
    ai_provider TEXT,
    ai_model TEXT,
    prompt_version TEXT,
    failure_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chat_messages_sequence_check CHECK (sequence > 0),
    CONSTRAINT chat_messages_role_check CHECK (role IN ('user', 'assistant')),
    CONSTRAINT chat_messages_content_ciphertext_length_check
        CHECK (octet_length(content_ciphertext) BETWEEN 1 AND 65536),
    CONSTRAINT chat_messages_client_request_hash_check
        CHECK (client_request_hash IS NULL OR client_request_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chat_messages_client_request_role_check
        CHECK ((role = 'user') = (client_request_hash IS NOT NULL)),
    CONSTRAINT chat_messages_reply_role_check
        CHECK ((role = 'assistant') = (in_reply_to_message_id IS NOT NULL)),
    CONSTRAINT chat_messages_generation_status_check
        CHECK (generation_status IN ('pending', 'completed', 'failed', 'blocked')),
    CONSTRAINT chat_messages_user_status_check
        CHECK (role <> 'user' OR generation_status IN ('pending', 'completed', 'failed', 'blocked')),
    CONSTRAINT chat_messages_assistant_status_check
        CHECK (role <> 'assistant' OR generation_status IN ('completed', 'blocked')),
    CONSTRAINT chat_messages_ai_provider_length_check
        CHECK (ai_provider IS NULL OR char_length(btrim(ai_provider)) BETWEEN 1 AND 100),
    CONSTRAINT chat_messages_ai_model_length_check
        CHECK (ai_model IS NULL OR char_length(btrim(ai_model)) BETWEEN 1 AND 160),
    CONSTRAINT chat_messages_prompt_version_length_check
        CHECK (prompt_version IS NULL OR char_length(btrim(prompt_version)) BETWEEN 1 AND 100),
    CONSTRAINT chat_messages_failure_code_length_check
        CHECK (failure_code IS NULL OR char_length(btrim(failure_code)) BETWEEN 1 AND 100),
    CONSTRAINT chat_messages_conversation_sequence_unique UNIQUE (conversation_id, sequence),
    CONSTRAINT chat_messages_reply_unique UNIQUE (in_reply_to_message_id)
);

CREATE UNIQUE INDEX chat_messages_user_idempotency_unique
    ON chat_messages (conversation_id, client_request_hash)
    WHERE role = 'user';

CREATE INDEX chat_messages_conversation_history_idx
    ON chat_messages (conversation_id, sequence DESC);

-- +goose Down

DROP TABLE IF EXISTS chat_messages;
DROP TABLE IF EXISTS chat_conversations;
DROP TABLE IF EXISTS app_consents;
