from collections.abc import AsyncIterator
from typing import Protocol

from app.domain.models import (
    AtomicFacts,
    ConversationGenerationInput,
    ConversationModelOutput,
    GatewayDecision,
    JourneyReportDraft,
    ModerationDecision,
    ReportGenerationInput,
    SafetyDecision,
)
from app.domain.schemas import (
    CompanionRequest,
    CompanionResponse,
    CompanionStreamEvent,
    ContextRequest,
    ContextResponse,
)


class AIProvider(Protocol):
    name: str

    async def moderate(self, text: str) -> ModerationDecision: ...

    async def classify_security(self, text: str, language: str) -> GatewayDecision: ...

    async def classify_safety(self, text: str, language: str) -> SafetyDecision: ...

    async def generate_conversation(
        self, request: ConversationGenerationInput
    ) -> ConversationModelOutput: ...

    def stream_conversation(self, request: ConversationGenerationInput) -> AsyncIterator[str]: ...

    async def repair_conversation(
        self, request: ConversationGenerationInput, issues: list[str]
    ) -> ConversationModelOutput: ...

    async def extract_facts(self, request: ReportGenerationInput) -> AtomicFacts: ...

    async def generate_report(self, request: ReportGenerationInput) -> JourneyReportDraft: ...


class AIService(Protocol):
    async def respond(self, request: CompanionRequest) -> CompanionResponse: ...

    def stream(self, request: CompanionRequest) -> AsyncIterator[CompanionStreamEvent]: ...

    async def process_context(self, request: ContextRequest) -> ContextResponse: ...
