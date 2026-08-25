from functools import lru_cache
from typing import Literal

from pydantic import Field, SecretStr, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_prefix="AI_",
        env_file=".env",
        extra="ignore",
    )

    environment: Literal["development", "test", "production"] = "development"
    service_api_key: str = Field(min_length=32)
    provider: Literal["mock", "openai"] = "mock"
    openai_api_key: SecretStr | None = None
    openai_store: bool = False
    request_timeout_seconds: float = Field(default=15, ge=1, le=60)
    max_retries: int = Field(default=1, ge=0, le=3)

    conversation_model: str = Field(default="gpt-5.6-terra", min_length=1, max_length=160)
    auxiliary_model: str = Field(default="gpt-5.6-luna", min_length=1, max_length=160)
    report_model: str = Field(default="gpt-5.6-terra", min_length=1, max_length=160)
    moderation_model: str = Field(default="omni-moderation-latest", min_length=1, max_length=160)
    conversation_reasoning: Literal["none", "low", "medium"] = "low"
    auxiliary_reasoning: Literal["none", "low", "medium"] = "low"
    report_reasoning: Literal["low", "medium", "high"] = "medium"
    report_direct_input_characters: int = Field(default=80_000, ge=10_000, le=300_000)
    report_chunk_characters: int = Field(default=40_000, ge=5_000, le=100_000)
    report_extraction_concurrency: int = Field(default=4, ge=1, le=16)

    prompt_version: str = Field(default="companion-v2", min_length=1, max_length=100)
    context_prompt_version: str = Field(default="journey-report-v2", min_length=1, max_length=100)
    conversation_graph_version: str = Field(default="conversation-graph-v1", max_length=100)
    report_graph_version: str = Field(default="journey-report-graph-v2", max_length=100)

    @model_validator(mode="after")
    def validate_provider_credentials(self) -> "Settings":
        if self.provider == "openai" and self.openai_api_key is None:
            raise ValueError("AI_OPENAI_API_KEY is required when AI_PROVIDER=openai")
        return self


@lru_cache
def get_settings() -> Settings:
    return Settings()  # type: ignore[call-arg]
