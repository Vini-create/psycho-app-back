from typing import TypedDict

from app.domain.models import (
    ConversationGenerationInput,
    ConversationModelOutput,
    GatewayDecision,
    ModerationDecision,
    SafetyDecision,
)
from app.domain.schemas import CompanionRequest


class ConversationState(TypedDict, total=False):
    request: CompanionRequest
    language: str
    gateway: GatewayDecision
    moderation: ModerationDecision
    requires_safety_review: bool
    safety: SafetyDecision
    route: str
    block_reason: str | None
    generation_input: ConversationGenerationInput
    generated: ConversationModelOutput
    validation_issues: list[str]
    final_content: str
