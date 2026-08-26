from collections.abc import AsyncIterator

from app.domain.schemas import (
    CompanionRequest,
    CompanionResponse,
    CompanionStreamEvent,
    ContextRequest,
    ContextResponse,
)
from app.graphs.conversation.builder import ConversationGraphRunner
from app.graphs.report.builder import ReportGraphRunner


class GraphAIService:
    def __init__(
        self,
        conversation: ConversationGraphRunner,
        report: ReportGraphRunner,
    ) -> None:
        self._conversation = conversation
        self._report = report

    async def respond(self, request: CompanionRequest) -> CompanionResponse:
        return await self._conversation.respond(request)

    async def stream(self, request: CompanionRequest) -> AsyncIterator[CompanionStreamEvent]:
        async for event in self._conversation.stream(request):
            yield event

    async def process_context(self, request: ContextRequest) -> ContextResponse:
        return await self._report.process(request)
