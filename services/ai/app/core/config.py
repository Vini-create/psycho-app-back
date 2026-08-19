from functools import lru_cache
from typing import Literal

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_prefix="AI_",
        env_file=".env",
        extra="ignore",
    )

    environment: Literal["development", "test", "production"] = "development"
    service_api_key: str = Field(min_length=32)
    provider: Literal["mock"] = "mock"
    model: str = Field(default="mock-v1", min_length=1, max_length=160)
    prompt_version: str = Field(default="companion-v1", min_length=1, max_length=100)
    context_prompt_version: str = Field(default="context-v1", min_length=1, max_length=100)


@lru_cache
def get_settings() -> Settings:
    return Settings()
