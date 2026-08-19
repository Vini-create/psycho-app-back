import asyncio

from app.core.config import Settings
from app.domain.schemas import (
    CompanionRequest,
    CompanionResponse,
    ContextItem,
    ContextRequest,
    ContextResponse,
)


class MockAIService:
    def __init__(self, settings: Settings) -> None:
        self._settings = settings

    async def respond(self, request: CompanionRequest) -> CompanionResponse:
        await asyncio.sleep(0)
        return CompanionResponse(
            content=(
                "Recebi sua mensagem. Este é o provedor mock; substitua-o pelo adapter "
                "do modelo quando começar a implementar a IA."
            ),
            provider="mock",
            model=self._settings.model,
            prompt_version=self._settings.prompt_version,
            blocked=False,
        )

    async def process_context(self, request: ContextRequest) -> ContextResponse:
        await asyncio.sleep(0)
        source = request.messages[-1]
        return ContextResponse(
            summary=(
                f"Resumo mock do período com {len(request.messages)} mensagens. "
                "Implemente aqui a extração estruturada do provedor real."
            ),
            items=[
                ContextItem(
                    kind="theme",
                    description="Tema mock rastreável até a última mensagem do período.",
                    confidence=1.0,
                    source_message_ids=[source.id],
                )
            ],
            provider="mock",
            model=self._settings.model,
            prompt_version=self._settings.context_prompt_version,
        )
