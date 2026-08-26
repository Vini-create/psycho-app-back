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
