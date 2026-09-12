from datetime import datetime
from typing import Literal
from uuid import UUID

from pydantic import Field, model_validator

from app.domain.schemas import (
    ContextItem,
    EmotionalValence,
    EvidenceStrength,
    ReportCoverage,
    ReportItemKind,
    StrictModel,
    TimelineEntry,
)

ConversationMode = Literal[
    "listen",
    "explore",
    "organize",
    "clarify",
    "reframe",
    "plan",
    "gentle_challenge",
    "follow_up",
    "celebrate",
    "closure",
]

ResponseMove = Literal[
    "acknowledge",
    "reflect_content",
    "reflect_meaning",
    "stay_with_emotion",
    "offer_observation",
    "connect_previous_thread",
    "clarify",
    "invite_elaboration",
    "answer_directly",
    "offer_perspective",
    "gentle_challenge",
    "summarize",
    "offer_options",
    "suggest_next_step",
    "celebrate",
    "repair",
    "close",
]

SecurityThreat = Literal[
    "prompt_injection",
    "jailbreak",
    "system_prompt_extraction",
    "data_exfiltration",
    "encoded_payload",
    "tool_abuse",
]


class ModerationDecision(StrictModel):
    flagged: bool = False
    categories: list[str] = Field(default_factory=list, max_length=30)


class GatewayDecision(StrictModel):
    decision: Literal["allow", "review", "block"] = "allow"
    threats: list[SecurityThreat] = Field(default_factory=list)
    reason_code: str | None = Field(default=None, max_length=100)


class SafetyDecision(StrictModel):
    route: Literal["normal", "boundary", "crisis", "security_block"]
    reason_code: str | None = Field(default=None, max_length=100)


class ConversationModelOutput(StrictModel):
    content: str = Field(min_length=1, max_length=12_000)
    mode: ConversationMode
    primary_move: ResponseMove
    secondary_move: ResponseMove | None = None


class ConversationGenerationInput(StrictModel):
    language: str = Field(min_length=2, max_length=35)
    user_name: str | None = Field(default=None, min_length=1, max_length=120)
    message: str = Field(min_length=1, max_length=8_000)
    history: list[dict[str, str]] = Field(default_factory=list, max_length=50)
    question_budget: Literal[0, 1]
    recent_question_count: int = Field(ge=0, le=10)
    country_code: str | None = None


class AtomicFact(StrictModel):
    kind: ReportItemKind
    title: str = Field(min_length=1, max_length=240)
    description: str = Field(min_length=1, max_length=2_000)
    evidence_strength: EvidenceStrength
    emotional_valence: EmotionalValence | None = None
    occurred_at: datetime | None = None
    source_message_ids: list[UUID] = Field(min_length=1, max_length=50)

    @model_validator(mode="after")
    def validate_emotional_valence(self) -> "AtomicFact":
        if self.kind == "emotion" and self.emotional_valence is None:
            raise ValueError("emotion facts require emotional_valence")
        if self.kind != "emotion" and self.emotional_valence is not None:
            raise ValueError("emotional_valence is only valid for emotion facts")
        return self


class AtomicFacts(StrictModel):
    facts: list[AtomicFact] = Field(default_factory=list, max_length=500)


class ReportGenerationInput(StrictModel):
    period_start: datetime
    period_end: datetime
    target_locale: str
    messages: list[dict[str, str]] = Field(default_factory=list, max_length=500)
    facts: list[AtomicFact] = Field(default_factory=list, max_length=500)

    @model_validator(mode="after")
    def require_evidence(self) -> "ReportGenerationInput":
        if not self.messages and not self.facts:
            raise ValueError("messages or facts are required")
        return self


class JourneyReportDraft(StrictModel):
    title: str = Field(min_length=1, max_length=240)
    coverage: ReportCoverage
    summary: str = Field(min_length=1, max_length=12_000)
    timeline: list[TimelineEntry] = Field(default_factory=list, max_length=100)
    items: list[ContextItem] = Field(default_factory=list, max_length=100)
    limitations: list[str] = Field(default_factory=list, max_length=20)
