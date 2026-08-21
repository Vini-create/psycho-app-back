-- +goose Up

ALTER TABLE context_items
    ADD COLUMN emotional_valence TEXT,
    ADD CONSTRAINT context_items_emotional_valence_check
        CHECK (
            emotional_valence IS NULL
            OR (
                kind = 'emotion'
                AND emotional_valence IN ('pleasant', 'unpleasant', 'mixed', 'neutral')
            )
        );

-- Relatórios journey-report-v1 permanecem válidos com valor nulo. O serviço
-- exige o campo em novos itens de emoção produzidos pelo journey-report-v2.

-- +goose Down

ALTER TABLE context_items
    DROP CONSTRAINT IF EXISTS context_items_emotional_valence_check,
    DROP COLUMN IF EXISTS emotional_valence;
