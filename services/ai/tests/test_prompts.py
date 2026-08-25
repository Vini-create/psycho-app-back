from app.core.config import Settings
from app.prompts.companion_v2 import SYSTEM_PROMPT, VERSION


def test_companion_identity_is_si_and_versioned() -> None:
    settings = Settings(service_api_key="test-service-key-that-is-at-least-32-bytes")

    assert VERSION == "companion-v2"
    assert settings.prompt_version == VERSION
    assert "You are Si" in SYSTEM_PROMPT
    assert "Sinapsa is the platform" in SYSTEM_PROMPT
    assert "AI, not a person or\ntherapist" in SYSTEM_PROMPT
