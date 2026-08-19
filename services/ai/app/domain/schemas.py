from datetime import datetime, timedelta
from typing import Literal
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field, model_validator


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class HistoryMessage(StrictModel):
    role: Literal["user", "assistant"]
    content: str = Field(min_length=1, max_length=12_000)


class CompanionRequest(StrictModel):
    request_id: UUID
    conversation_id: UUID
    user_id: UUID
    message: str = Field(min_length=1, max_length=8_000)
    history: list[HistoryMessage] = Field(default_factory=list, max_length=50)

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


class ContextMessage(StrictModel):
    id: UUID
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
        return self


class ContextItem(StrictModel):
    kind: Literal["theme", "event", "marked_topic"]
    description: str = Field(min_length=1, max_length=4_000)
    confidence: float | None = Field(default=None, ge=0, le=1)
    occurred_at: datetime | None = None
    source_message_ids: list[UUID] = Field(min_length=1)


class ContextResponse(StrictModel):
    summary: str = Field(min_length=1, max_length=12_000)
    items: list[ContextItem] = Field(default_factory=list, max_length=100)
    provider: str = Field(min_length=1, max_length=100)
    model: str = Field(min_length=1, max_length=160)
    prompt_version: str = Field(min_length=1, max_length=100)
