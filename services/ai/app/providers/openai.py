from collections.abc import AsyncIterator
from typing import Any, cast

from langchain_core.messages import HumanMessage, SystemMessage
from langchain_openai import ChatOpenAI
from openai import AsyncOpenAI

from app.core.config import Settings
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
from app.prompts.companion_v2 import (
    SAFETY_PROMPT,
    SECURITY_PROMPT,
    STREAMING_SYSTEM_PROMPT,
    SYSTEM_PROMPT,
)
from app.prompts.context_v2 import FACT_EXTRACTION_PROMPT
from app.prompts.context_v2 import SYSTEM_PROMPT as REPORT_SYSTEM_PROMPT


class OpenAIProvider:
    name = "openai"

    def __init__(self, settings: Settings) -> None:
        if settings.openai_api_key is None:
            raise ValueError("OpenAI API key is required")
        api_key = settings.openai_api_key.get_secret_value()
        common: dict[str, Any] = {
            "api_key": api_key,
            "timeout": settings.request_timeout_seconds,
            "max_retries": settings.max_retries,
            "use_responses_api": True,
            "store": settings.openai_store,
        }
        self._conversation_model = ChatOpenAI(
            model=settings.conversation_model,
            reasoning_effort=settings.conversation_reasoning,
            **common,
        )
        self._auxiliary_model = ChatOpenAI(
            model=settings.auxiliary_model,
            reasoning_effort=settings.auxiliary_reasoning,
            **common,
        )
        self._report_model = ChatOpenAI(
            model=settings.report_model,
            reasoning_effort=settings.report_reasoning,
            **common,
        )
        self._moderation_model = settings.moderation_model
        self._client = AsyncOpenAI(
            api_key=api_key,
            timeout=settings.request_timeout_seconds,
            max_retries=settings.max_retries,
        )

    async def moderate(self, text: str) -> ModerationDecision:
        response = await self._client.moderations.create(
            model=self._moderation_model,
            input=text,
        )
        result = response.results[0]
        categories = [key for key, value in result.categories.model_dump().items() if value is True]
        return ModerationDecision(flagged=result.flagged, categories=categories)

    async def classify_security(self, text: str, language: str) -> GatewayDecision:
        model = self._auxiliary_model.with_structured_output(GatewayDecision, method="json_schema")
        result = await model.ainvoke(
            [SystemMessage(content=SECURITY_PROMPT), HumanMessage(content=text)]
        )
        return cast(GatewayDecision, result)

    async def classify_safety(self, text: str, language: str) -> SafetyDecision:
        model = self._auxiliary_model.with_structured_output(SafetyDecision, method="json_schema")
        result = await model.ainvoke(
            [SystemMessage(content=SAFETY_PROMPT), HumanMessage(content=text)]
        )
        return cast(SafetyDecision, result)

    async def generate_conversation(
        self, request: ConversationGenerationInput
    ) -> ConversationModelOutput:
        model = self._conversation_model.with_structured_output(
            ConversationModelOutput, method="json_schema"
        )
        result = await model.ainvoke(
            [
                SystemMessage(content=SYSTEM_PROMPT),
                HumanMessage(content=request.model_dump_json()),
            ]
        )
        return cast(ConversationModelOutput, result)

    async def stream_conversation(self, request: ConversationGenerationInput) -> AsyncIterator[str]:
        """Emite apenas texto visível; reasoning e metadados nunca entram no stream."""
        async for chunk in self._conversation_model.astream(
            [
                SystemMessage(content=STREAMING_SYSTEM_PROMPT),
                HumanMessage(content=request.model_dump_json()),
            ]
        ):
            text = chunk.text
            if text:
                yield text

    async def repair_conversation(
        self, request: ConversationGenerationInput, issues: list[str]
    ) -> ConversationModelOutput:
        model = self._conversation_model.with_structured_output(
            ConversationModelOutput, method="json_schema"
        )
        repair_prompt = (
            f"{SYSTEM_PROMPT}\n\nThe previous draft failed deterministic validation for: "
            f"{', '.join(issues)}. Produce a new response and do not discuss the validation."
        )
        result = await model.ainvoke(
            [
                SystemMessage(content=repair_prompt),
                HumanMessage(content=request.model_dump_json()),
            ]
        )
        return cast(ConversationModelOutput, result)

    async def extract_facts(self, request: ReportGenerationInput) -> AtomicFacts:
        model = self._auxiliary_model.with_structured_output(AtomicFacts, method="json_schema")
        result = await model.ainvoke(
            [
                SystemMessage(content=FACT_EXTRACTION_PROMPT),
                HumanMessage(content=request.model_dump_json()),
            ]
        )
        return cast(AtomicFacts, result)

    async def generate_report(self, request: ReportGenerationInput) -> JourneyReportDraft:
        model = self._report_model.with_structured_output(JourneyReportDraft, method="json_schema")
        result = await model.ainvoke(
            [
                SystemMessage(content=REPORT_SYSTEM_PROMPT),
                HumanMessage(content=request.model_dump_json()),
            ]
        )
        return cast(JourneyReportDraft, result)
