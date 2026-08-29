import pytest
from pydantic import ValidationError

from app.core.config import Settings


def test_production_rejects_mock_provider() -> None:
    with pytest.raises(ValidationError, match="AI_PROVIDER=mock cannot be used in production"):
        Settings(
            environment="production",
            service_api_key="a" * 32,
            provider="mock",
        )


def test_production_accepts_openai_with_api_key() -> None:
    settings = Settings(
        environment="production",
        service_api_key="a" * 32,
        provider="openai",
        openai_api_key="test-openai-key",
    )

    assert settings.provider == "openai"


def test_deepinfra_requires_api_key() -> None:
    with pytest.raises(
        ValidationError,
        match="AI_DEEPINFRA_API_KEY is required when AI_PROVIDER=deepinfra",
    ):
        Settings(
            service_api_key="a" * 32,
            provider="deepinfra",
        )


def test_production_accepts_deepinfra_with_api_key() -> None:
    settings = Settings(
        environment="production",
        service_api_key="a" * 32,
        provider="deepinfra",
        deepinfra_api_key="test-deepinfra-key",
    )

    assert settings.provider == "deepinfra"
    assert settings.deepinfra_model == "meta-llama/Meta-Llama-3.1-8B-Instruct"
    assert settings.deepinfra_conversation_max_tokens == 320
    assert settings.deepinfra_classification_max_tokens == 192
    assert settings.deepinfra_extraction_max_tokens == 4_096
    assert settings.deepinfra_report_max_tokens == 3_072
    assert settings.deepinfra_conversation_temperature == 0.65
    assert settings.deepinfra_structured_temperature == 0.1
