import json
from collections.abc import AsyncIterator
from typing import Any, cast

import httpx
from langchain_core.messages import HumanMessage, SystemMessage
from langchain_openai import ChatOpenAI
from pydantic import BaseModel

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
from app.prompts.companion_v3 import (
    MODERATION_PROMPT,
    SAFETY_PROMPT,
    SECURITY_PROMPT,
    STREAMING_SYSTEM_PROMPT,
)
from app.prompts.context_v2 import FACT_EXTRACTION_PROMPT
from app.prompts.context_v2 import SYSTEM_PROMPT as REPORT_SYSTEM_PROMPT
from app.services.base import ModelPurpose


def _json_mode_prompt(prompt: str, schema: type[BaseModel]) -> str:
    """Inclui o contrato no prompt porque este Llama aceita JSON mode, não JSON Schema."""
    schema_json = json.dumps(
        schema.model_json_schema(),
        ensure_ascii=False,
        separators=(",", ":"),
    )
    return (
        f"{prompt}\n\nReturn exactly one valid JSON object matching this JSON Schema. "
        f"Do not add markdown fences or text outside the object.\n{schema_json}"
    )


def _conversation_priority(request: ConversationGenerationInput) -> str:
    if request.question_budget == 0:
        question_rule = (
            "Use zero questions, zero question marks and zero requests for the user to answer. "
            "End with a brief reflection or optional statement that leaves room to continue."
        )
    else:
        question_rule = (
            "When the message contains an unresolved tension, meaningful change or concrete open "
            "thread, prefer one focused question about that thread. Otherwise use no question."
        )
    return (
        "Final priorities for this response: begin directly with one specific observation tied "
        "to the user's words. Do not begin with a generic empathy formula. Do not invent or "
        "strengthen emotion labels. Match the user's register lightly, without caricature. Avoid "
        "forced either-or and causal questions. Ask about the closest lived detail without "
        "assuming fear, motives, actions or improvement. For good news, celebrate before asking "
        f"about its meaning or how it feels now. {question_rule}"
    )


def _conversation_output(content: str) -> ConversationModelOutput:
    cleaned = content.strip()
    asks_question = "?" in cleaned
    return ConversationModelOutput(
        content=cleaned,
        mode="explore" if asks_question else "listen",
        primary_move="invite_elaboration" if asks_question else "reflect_meaning",
    )


def _conversation_input(request: ConversationGenerationInput) -> str:
    context = request.model_dump(exclude={"message"}, mode="json")
    context_json = json.dumps(context, ensure_ascii=False, separators=(",", ":"))
    return (
        f"Application context JSON:\n{context_json}\n\n"
        f"CURRENT USER MESSAGE — respond to this message:\n{request.message}"
    )


class DeepInfraProvider:
    """Adapter para o endpoint OpenAI-compatible da DeepInfra.

    A DeepInfra oferece Chat Completions, não a Responses API usada pelo
    adapter OpenAI. Por isso não enviamos `store` nem parâmetros de reasoning
    específicos da OpenAI. Toda saída estruturada volta a passar pelos modelos
    Pydantic do domínio.
    """

    name = "deepinfra"

    def __init__(self, settings: Settings) -> None:
        if settings.deepinfra_api_key is None:
            raise ValueError("DeepInfra API key is required")
        self._settings = settings
        api_key = settings.deepinfra_api_key.get_secret_value()
        self._http_async_client = httpx.AsyncClient(timeout=settings.request_timeout_seconds)
        common: dict[str, Any] = {
            "api_key": api_key,
            "base_url": settings.deepinfra_base_url.rstrip("/"),
            "timeout": settings.request_timeout_seconds,
            "max_retries": settings.max_retries,
            "use_responses_api": False,
            "http_async_client": self._http_async_client,
        }
        self._conversation_model = ChatOpenAI(
            model=settings.deepinfra_model,
            extra_body={"max_tokens": settings.deepinfra_conversation_max_tokens},
            temperature=settings.deepinfra_conversation_temperature,
            **common,
        )
        self._classification_model = ChatOpenAI(
            model=settings.deepinfra_model,
            extra_body={"max_tokens": settings.deepinfra_classification_max_tokens},
            temperature=settings.deepinfra_structured_temperature,
            **common,
        )
        self._extraction_model = ChatOpenAI(
            model=settings.deepinfra_model,
            extra_body={"max_tokens": settings.deepinfra_extraction_max_tokens},
            temperature=settings.deepinfra_structured_temperature,
            **common,
        )
        self._report_model = ChatOpenAI(
            model=settings.deepinfra_model,
            extra_body={"max_tokens": settings.deepinfra_report_max_tokens},
            temperature=settings.deepinfra_structured_temperature,
            **common,
        )

    def model_name(self, purpose: ModelPurpose) -> str:
        return self._settings.deepinfra_model

    async def aclose(self) -> None:
        await self._http_async_client.aclose()

    async def moderate(self, text: str) -> ModerationDecision:
        model = self._classification_model.with_structured_output(
            ModerationDecision,
            method="json_mode",
        )
        result = await model.ainvoke(
            [
                SystemMessage(content=_json_mode_prompt(MODERATION_PROMPT, ModerationDecision)),
                HumanMessage(content=text),
            ]
        )
        return cast(ModerationDecision, result)

    async def classify_security(self, text: str, language: str) -> GatewayDecision:
        model = self._classification_model.with_structured_output(
            GatewayDecision,
            method="json_mode",
        )
        result = await model.ainvoke(
            [
                SystemMessage(content=_json_mode_prompt(SECURITY_PROMPT, GatewayDecision)),
                HumanMessage(content=f"Language: {language}\nText: {text}"),
            ]
        )
        return cast(GatewayDecision, result)

    async def classify_safety(self, text: str, language: str) -> SafetyDecision:
        model = self._classification_model.with_structured_output(
            SafetyDecision,
            method="json_mode",
        )
        result = await model.ainvoke(
            [
                SystemMessage(content=_json_mode_prompt(SAFETY_PROMPT, SafetyDecision)),
                HumanMessage(content=f"Language: {language}\nText: {text}"),
            ]
        )
        return cast(SafetyDecision, result)

    async def generate_conversation(
        self, request: ConversationGenerationInput
    ) -> ConversationModelOutput:
        result = await self._conversation_model.ainvoke(
            [
                SystemMessage(
                    content=(f"{STREAMING_SYSTEM_PROMPT}\n\n{_conversation_priority(request)}")
                ),
                HumanMessage(content=_conversation_input(request)),
            ]
        )
        return _conversation_output(result.text)

    async def stream_conversation(self, request: ConversationGenerationInput) -> AsyncIterator[str]:
        async for chunk in self._conversation_model.astream(
            [
                SystemMessage(
                    content=(f"{STREAMING_SYSTEM_PROMPT}\n\n{_conversation_priority(request)}")
                ),
                HumanMessage(content=_conversation_input(request)),
            ]
        ):
            text = chunk.text
            if text:
                yield text

    async def repair_conversation(
        self,
        request: ConversationGenerationInput,
        issues: list[str],
    ) -> ConversationModelOutput:
        repair_prompt = (
            f"{STREAMING_SYSTEM_PROMPT}\n\nThe previous draft failed deterministic validation for: "
            f"{', '.join(issues)}. Produce a new response and do not discuss the validation."
        )
        result = await self._conversation_model.ainvoke(
            [
                SystemMessage(content=f"{repair_prompt}\n\n{_conversation_priority(request)}"),
                HumanMessage(content=_conversation_input(request)),
            ]
        )
        return _conversation_output(result.text)

    async def extract_facts(self, request: ReportGenerationInput) -> AtomicFacts:
        model = self._extraction_model.with_structured_output(
            AtomicFacts,
            method="json_mode",
        )
        result = await model.ainvoke(
            [
                SystemMessage(content=_json_mode_prompt(FACT_EXTRACTION_PROMPT, AtomicFacts)),
                HumanMessage(content=request.model_dump_json()),
            ]
        )
        return cast(AtomicFacts, result)

    async def generate_report(self, request: ReportGenerationInput) -> JourneyReportDraft:
        model = self._report_model.with_structured_output(
            JourneyReportDraft,
            method="json_mode",
        )
        result = await model.ainvoke(
            [
                SystemMessage(content=_json_mode_prompt(REPORT_SYSTEM_PROMPT, JourneyReportDraft)),
                HumanMessage(content=request.model_dump_json()),
            ]
        )
        return cast(JourneyReportDraft, result)
