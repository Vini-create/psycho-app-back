from app.domain.schemas import (
    CompanionRequest,
    CompanionResponse,
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

    async def process_context(self, request: ContextRequest) -> ContextResponse:
        return await self._report.process(request)
