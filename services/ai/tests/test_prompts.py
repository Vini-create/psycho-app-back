from app.core.config import Settings
from app.prompts.companion_v3 import SYSTEM_PROMPT, VERSION
from app.prompts.context_v2 import FACT_EXTRACTION_PROMPT
from app.prompts.context_v2 import SYSTEM_PROMPT as REPORT_SYSTEM_PROMPT


def test_companion_identity_is_si_and_versioned() -> None:
    settings = Settings(service_api_key="test-service-key-that-is-at-least-32-bytes")

    assert VERSION == "companion-v3"
    assert settings.prompt_version == VERSION
    assert "You are Si" in SYSTEM_PROMPT
    assert "Sinapsa is the platform" in SYSTEM_PROMPT
    assert "an AI, not a person or therapist" in SYSTEM_PROMPT
    assert "Never use emojis" in SYSTEM_PROMPT
    assert "one to four short sentences and under 100 words" in SYSTEM_PROMPT
    assert "Match the user's vocabulary" in SYSTEM_PROMPT
    assert "Treat question_budget as a hard limit" in SYSTEM_PROMPT
    assert "ask at most one\n  focused question" in SYSTEM_PROMPT
    assert "Mental\n  busyness is not automatically anxiety" in SYSTEM_PROMPT
    assert "Examples show tone, not a fixed template" in SYSTEM_PROMPT
    assert "Ask about the closest lived detail" in SYSTEM_PROMPT
    assert "Do not assume fear" in SYSTEM_PROMPT
    assert "For an\n  achievement or good news, celebrate" in SYSTEM_PROMPT
    assert "Answer a direct question before reflecting" in SYSTEM_PROMPT
    assert "never analyze the greeting" in SYSTEM_PROMPT


def test_report_prompt_calibrates_coverage_and_avoids_literal_journey_translation() -> None:
    assert 'use "limited" for a\nsingle message' in REPORT_SYSTEM_PROMPT
    assert "never translate this product concept as a literal trip" in REPORT_SYSTEM_PROMPT
    assert 'never "relatou sentir cansado"' in REPORT_SYSTEM_PROMPT
    assert "Do not emit duplicate facts" in FACT_EXTRACTION_PROMPT
