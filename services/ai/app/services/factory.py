from functools import lru_cache

from app.core.config import get_settings
from app.services.base import AIService
from app.services.mock import MockAIService


@lru_cache
def get_ai_service() -> AIService:
    settings = get_settings()
    if settings.provider == "mock":
        return MockAIService(settings)
    raise RuntimeError(f"unsupported AI provider: {settings.provider}")
