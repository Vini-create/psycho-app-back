from typing import Protocol

from app.domain.schemas import (
    CompanionRequest,
    CompanionResponse,
    ContextRequest,
    ContextResponse,
)


class AIService(Protocol):
    async def respond(self, request: CompanionRequest) -> CompanionResponse: ...

    async def process_context(self, request: ContextRequest) -> ContextResponse: ...
