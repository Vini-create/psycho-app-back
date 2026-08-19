from functools import lru_cache

from app.core.config import get_settings
from app.graphs.conversation.builder import ConversationGraphRunner
from app.graphs.report.builder import ReportGraphRunner
from app.providers.factory import get_provider
from app.services.base import AIService
from app.services.orchestrator import GraphAIService


@lru_cache
def get_ai_service() -> AIService:
    settings = get_settings()
    provider = get_provider()
    return GraphAIService(
        conversation=ConversationGraphRunner(provider, settings),
        report=ReportGraphRunner(provider, settings),
    )
