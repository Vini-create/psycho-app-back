from functools import lru_cache

from app.core.config import get_settings
from app.providers.mock import MockProvider
from app.providers.openai import OpenAIProvider
from app.services.base import AIProvider


@lru_cache
def get_provider() -> AIProvider:
    settings = get_settings()
    if settings.provider == "mock":
        return MockProvider(settings)
    if settings.provider == "openai":
        return OpenAIProvider(settings)
    raise RuntimeError(f"unsupported AI provider: {settings.provider}")
