from datetime import datetime, timedelta
from typing import Annotated, Literal
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field, model_validator


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class HistoryMessage(StrictModel):
    id: UUID | None = None
    role: Literal["user", "assistant"]
    content: str = Field(min_length=1, max_length=12_000)
    created_at: datetime | None = None


class CompanionRequest(StrictModel):
    request_id: UUID
    conversation_id: UUID
    user_id: UUID
    message: str = Field(min_length=1, max_length=8_000)
    history: list[HistoryMessage] = Field(default_factory=list, max_length=50)
    locale_hint: str | None = Field(default=None, min_length=2, max_length=35)
    country_code: str | None = Field(default=None, pattern=r"^[A-Z]{2}$")
    timezone: str | None = Field(default=None, min_length=1, max_length=100)

    @model_validator(mode="after")
    def validate_history_size(self) -> "CompanionRequest":
        if sum(len(item.content) for item in self.history) > 60_000:
            raise ValueError("history exceeds the allowed context size")
        return self


class CompanionResponse(StrictModel):
    content: str = Field(min_length=1, max_length=12_000)
    provider: str = Field(min_length=1, max_length=100)
    model: str = Field(min_length=1, max_length=160)
    prompt_version: str = Field(min_length=1, max_length=100)
    blocked: bool = False
    block_reason: str | None = Field(default=None, max_length=100)
    language: str = Field(default="en", min_length=2, max_length=35)
    route: Literal["normal", "boundary", "crisis", "security_block"] = "normal"
    graph_version: str = Field(default="conversation-graph-v1", min_length=1, max_length=100)


class CompanionStreamEvent(StrictModel):
    """Evento interno do stream Go <- serviço de IA.

    `delta` contém somente texto que já atravessou as validações determinísticas
    disponíveis naquele limite de frase. O evento `done` carrega o contrato
    completo para o Go persistir exatamente o que foi exibido.
    """

    type: Literal["delta", "done"]
    delta: str | None = None
    response: CompanionResponse | None = None


class ContextMessage(StrictModel):
    id: UUID
    conversation_id: UUID | None = None
    role: Literal["user", "assistant"]
    content: str = Field(min_length=1, max_length=12_000)
    created_at: datetime


class ContextRequest(StrictModel):
    request_id: UUID
    connection_id: UUID
    user_id: UUID
    period_start: datetime
    period_end: datetime
    messages: list[ContextMessage] = Field(min_length=1, max_length=500)
    source_locale: str | None = Field(default=None, min_length=2, max_length=35)
    target_locale: str = Field(default="pt-BR", min_length=2, max_length=35)

    @model_validator(mode="after")
    def validate_period(self) -> "ContextRequest":
        if self.period_end <= self.period_start:
            raise ValueError("period_end must be after period_start")
        if self.period_end - self.period_start > timedelta(days=31):
            raise ValueError("context period cannot exceed 31 days")
        if any(
            message.created_at < self.period_start or message.created_at >= self.period_end
            for message in self.messages
        ):
            raise ValueError("every message must belong to the requested period")
        if sum(len(message.content) for message in self.messages) > 300_000:
            raise ValueError("context messages exceed the allowed input size")
        return self


EvidenceStrength = Literal[
    "explicit_once",
    "explicit_repeated",
    "uncertain",
    "contradictory",
]

EmotionalValence = Literal[
    "pleasant",
    "unpleasant",
    "mixed",
    "neutral",
]

ReportItemKind = Literal[
    "priority",
    "event",
    "challenge",
    "emotion",
    "thought",
    "behavior",
    "strategy",
    "support",
    "change",
    "open_topic",
    "safety_context",
]

ReportLimitation = Annotated[str, Field(min_length=1, max_length=1_000)]


class ReportCoverage(StrictModel):
    conversation_count: int = Field(ge=1, le=500)
    user_message_count: int = Field(ge=1, le=500)
    active_day_count: int = Field(ge=1, le=31)
    completeness: Literal["limited", "partial", "substantial"]
    note: str = Field(min_length=1, max_length=1_000)


class TimelineEntry(StrictModel):
    description: str = Field(min_length=1, max_length=2_000)
    occurred_at: datetime | None = None
    source_message_ids: list[UUID] = Field(min_length=1, max_length=50)


class ContextItem(StrictModel):
    kind: ReportItemKind
    title: str = Field(min_length=1, max_length=240)
    description: str = Field(min_length=1, max_length=4_000)
    impact: str | None = Field(default=None, max_length=2_000)
    evidence_strength: EvidenceStrength
    emotional_valence: EmotionalValence | None = None
    occurred_at: datetime | None = None
    source_message_ids: list[UUID] = Field(min_length=1, max_length=50)
    limitations: list[ReportLimitation] = Field(default_factory=list, max_length=10)

    @model_validator(mode="after")
    def validate_emotional_valence(self) -> "ContextItem":
        if self.kind == "emotion" and self.emotional_valence is None:
            raise ValueError("emotion items require emotional_valence")
        if self.kind != "emotion" and self.emotional_valence is not None:
            raise ValueError("emotional_valence is only valid for emotion items")
        return self


class ContextResponse(StrictModel):
    schema_version: Literal["journey-report-v2"] = "journey-report-v2"
    title: str = Field(min_length=1, max_length=240)
    coverage: ReportCoverage
    summary: str = Field(min_length=1, max_length=12_000)
    timeline: list[TimelineEntry] = Field(default_factory=list, max_length=100)
    items: list[ContextItem] = Field(default_factory=list, max_length=100)
    limitations: list[ReportLimitation] = Field(default_factory=list, max_length=20)
    provider: str = Field(min_length=1, max_length=100)
    model: str = Field(min_length=1, max_length=160)
    prompt_version: str = Field(min_length=1, max_length=100)
    graph_version: str = Field(default="journey-report-graph-v2", min_length=1, max_length=100)

    @model_validator(mode="after")
    def validate_sources(self) -> "ContextResponse":
        timeline_sources = [
            source for entry in self.timeline for source in entry.source_message_ids
        ]
        item_sources = [source for item in self.items for source in item.source_message_ids]
        if not timeline_sources and not item_sources:
            raise ValueError("report must contain at least one source reference")
        return self
