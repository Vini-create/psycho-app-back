import asyncio
import re
from collections.abc import AsyncIterator, Iterator
from typing import Literal, cast

from langgraph.graph import END, START, StateGraph
from langgraph.graph.state import CompiledStateGraph

from app.core.config import Settings
from app.domain.models import ConversationGenerationInput
from app.domain.schemas import CompanionRequest, CompanionResponse, CompanionStreamEvent
from app.graphs.conversation.state import ConversationState
from app.localization.detector import LocalLanguageDetector
from app.safety.gateway import needs_safety_review, scan_input
from app.safety.templates import safe_response
from app.services.base import AIProvider

_DEPENDENCY_PATTERNS = (
    "you only need me",
    "você só precisa de mim",
    "no necesitas a nadie más",
    "i am your therapist",
    "sou seu terapeuta",
    "soy tu terapeuta",
)

_DIAGNOSIS_PATTERN = re.compile(
    r"(?i)\b(you have|você tem|tienes)\s+(depression|depressão|depresión|bipolar|adhd|tdah)\b"
)

_SENTENCE_BOUNDARY = re.compile(r"[.!?…][\"'”’)]*(?:\s+|$)")


class ConversationGraphRunner:
    def __init__(
        self,
        provider: AIProvider,
        settings: Settings,
        detector: LocalLanguageDetector | None = None,
    ) -> None:
        self._provider = provider
        self._settings = settings
        self._detector = detector or LocalLanguageDetector(default_locale=settings.default_locale)
        self._graph = self._build_graph()

    def _build_graph(
        self,
    ) -> CompiledStateGraph[ConversationState, None, ConversationState, ConversationState]:
        graph = StateGraph(ConversationState)
        graph.add_node("prepare", self._prepare)
        graph.add_node("detect_language", self._detect_language)
        graph.add_node("input_gateway", self._input_gateway)
        graph.add_node("route_input", self._route_input)
        graph.add_node("classify_safety", self._classify_safety)
        graph.add_node("safe_response", self._safe_response)
        graph.add_node("generate", self._generate)
        graph.add_node("validate", self._validate)
        graph.add_node("repair", self._repair)
        graph.add_node("finalize", self._finalize)

        graph.add_edge(START, "prepare")
        graph.add_edge("prepare", "detect_language")
        graph.add_edge("prepare", "input_gateway")
        graph.add_edge(["detect_language", "input_gateway"], "route_input")
        graph.add_conditional_edges(
            "route_input",
            self._after_input,
            {
                "block": "safe_response",
                "review": "classify_safety",
                "normal": "generate",
            },
        )
        graph.add_conditional_edges(
            "classify_safety",
            self._after_safety,
            {"safe": "generate", "special": "safe_response"},
        )
        graph.add_edge("safe_response", "finalize")
        graph.add_edge("generate", "validate")
        graph.add_conditional_edges(
            "validate",
            self._after_validation,
            {"valid": "finalize", "repair": "repair"},
        )
        graph.add_edge("repair", "finalize")
        graph.add_edge("finalize", END)
        return graph.compile()

    async def respond(self, request: CompanionRequest) -> CompanionResponse:
        state = cast(ConversationState, await self._graph.ainvoke({"request": request}))
        route = cast(
            Literal["normal", "boundary", "crisis", "security_block"],
            state.get("route", "normal"),
        )
        return CompanionResponse(
            content=state["final_content"],
            provider=self._provider.name,
            model=state["model_used"],
            prompt_version=self._settings.prompt_version,
            blocked=route != "normal",
            block_reason=state.get("block_reason"),
            language=state["language"],
            route=route,
            graph_version=self._settings.conversation_graph_version,
        )

    async def stream(self, request: CompanionRequest) -> AsyncIterator[CompanionStreamEvent]:
        """Executa o gateway antes de liberar texto e valida cada frase.

        A validação por limite de frase é deliberada: entrega uma resposta
        progressiva sem expor fragmentos que ainda possam completar uma
        alegação diagnóstica ou linguagem de dependência.
        """
        state: ConversationState = {
            "request": request.model_copy(update={"message": request.message.strip()})
        }
        language_result, gateway_result = await asyncio.gather(
            self._detect_language(state), self._input_gateway(state)
        )
        state.update(language_result)
        state.update(gateway_result)
        state.update(await self._route_input(state))

        if state["route"] != "security_block" and state["requires_safety_review"]:
            state.update(await self._classify_safety(state))

        route = cast(
            Literal["normal", "boundary", "crisis", "security_block"],
            state["route"],
        )
        if route != "normal":
            content = safe_response(route, state["language"])
            for delta in _display_chunks(content):
                yield CompanionStreamEvent(type="delta", delta=delta)
            yield CompanionStreamEvent(
                type="done",
                response=self._response(
                    content=content,
                    language=state["language"],
                    route=route,
                    block_reason=state.get("block_reason"),
                    model=self._model_name("auxiliary"),
                ),
            )
            return

        generation_input = self._generation_input(state)
        accepted = ""
        pending = ""
        async for token in self._provider.stream_conversation(generation_input):
            pending += token
            complete, pending = _take_complete_sentences(pending)
            if not complete:
                continue
            candidate = accepted + complete
            issues = validate_conversation_output(candidate, generation_input.question_budget)
            if issues:
                async for event in self._stream_boundary_after(accepted, state):
                    yield event
                return
            accepted = candidate
            yield CompanionStreamEvent(type="delta", delta=complete)

        final_content = accepted + pending
        issues = validate_conversation_output(final_content, generation_input.question_budget)
        if issues:
            async for event in self._stream_boundary_after(accepted, state):
                yield event
            return
        if pending:
            yield CompanionStreamEvent(type="delta", delta=pending)
        yield CompanionStreamEvent(
            type="done",
            response=self._response(
                content=final_content.strip(),
                language=state["language"],
                route="normal",
                model=self._model_name("conversation"),
            ),
        )

    async def _stream_boundary_after(
        self,
        accepted: str,
        state: ConversationState,
    ) -> AsyncIterator[CompanionStreamEvent]:
        fallback = safe_response("boundary", state["language"])
        separator = "\n\n" if accepted.strip() else ""
        suffix = separator + fallback
        for delta in _display_chunks(suffix):
            yield CompanionStreamEvent(type="delta", delta=delta)
        yield CompanionStreamEvent(
            type="done",
            response=self._response(
                content=(accepted + suffix).strip(),
                language=state["language"],
                route="boundary",
                block_reason="invalid_generated_response",
                model=self._model_name("conversation"),
            ),
        )

    def _response(
        self,
        *,
        content: str,
        language: str,
        route: Literal["normal", "boundary", "crisis", "security_block"],
        model: str,
        block_reason: str | None = None,
    ) -> CompanionResponse:
        return CompanionResponse(
            content=content,
            provider=self._provider.name,
            model=model,
            prompt_version=self._settings.prompt_version,
            blocked=route != "normal",
            block_reason=block_reason,
            language=language,
            route=route,
            graph_version=self._settings.conversation_graph_version,
        )

    async def _prepare(self, state: ConversationState) -> ConversationState:
        request = state["request"]
        return {"request": request.model_copy(update={"message": request.message.strip()})}

    async def _detect_language(self, state: ConversationState) -> ConversationState:
        request = state["request"]
        detection_text = request.message
        if len(detection_text.strip()) < 20:
            recent_user_messages = [
                item.content for item in request.history[-6:] if item.role == "user"
            ]
            if recent_user_messages:
                detection_text = " ".join([*recent_user_messages, detection_text])
        return {"language": self._detector.detect(detection_text, request.locale_hint)}

    async def _input_gateway(self, state: ConversationState) -> ConversationState:
        request = state["request"]
        local = scan_input(request.message)
        moderation = await self._provider.moderate(request.message)
        gateway = local
        model_used: str | None = None
        if local.decision == "review":
            gateway = await self._provider.classify_security(request.message, "unknown")
            model_used = self._model_name("auxiliary")
        result: ConversationState = {
            "gateway": gateway,
            "moderation": moderation,
            "requires_safety_review": moderation.flagged or needs_safety_review(request.message),
        }
        if model_used is not None:
            result["model_used"] = model_used
        return result

    async def _route_input(self, state: ConversationState) -> ConversationState:
        gateway = state["gateway"]
        if gateway.decision == "block":
            return {"route": "security_block", "block_reason": gateway.reason_code}
        return {"route": "normal"}

    async def _after_input(self, state: ConversationState) -> Literal["block", "review", "normal"]:
        if state["route"] == "security_block":
            return "block"
        if state["requires_safety_review"]:
            return "review"
        return "normal"

    async def _classify_safety(self, state: ConversationState) -> ConversationState:
        decision = await self._provider.classify_safety(state["request"].message, state["language"])
        return {
            "safety": decision,
            "route": decision.route,
            "block_reason": decision.reason_code,
            "model_used": self._model_name("auxiliary"),
        }

    async def _after_safety(self, state: ConversationState) -> Literal["safe", "special"]:
        return "safe" if state["route"] == "normal" else "special"

    async def _safe_response(self, state: ConversationState) -> ConversationState:
        route = cast(Literal["boundary", "crisis", "security_block"], state["route"])
        return {"final_content": safe_response(route, state["language"])}

    async def _generate(self, state: ConversationState) -> ConversationState:
        generation_input = self._generation_input(state)
        generated = await self._provider.generate_conversation(generation_input)
        return {
            "generation_input": generation_input,
            "generated": generated,
            "model_used": self._model_name("conversation"),
        }

    def _generation_input(self, state: ConversationState) -> ConversationGenerationInput:
        request = state["request"]
        recent_assistant = [
            item.content for item in request.history[-4:] if item.role == "assistant"
        ]
        recent_question_count = sum(content.count("?") for content in recent_assistant)
        asks_only_listening = any(
            phrase in request.message.casefold()
            for phrase in (
                "só quero desabafar",
                "não quero conselho",
                "just listen",
                "solo escucha",
            )
        )
        question_budget: Literal[0, 1] = (
            0 if asks_only_listening or recent_question_count >= 2 else 1
        )
        return ConversationGenerationInput(
            language=state["language"],
            message=request.message,
            history=[
                {
                    "id": str(item.id) if item.id is not None else "",
                    "role": item.role,
                    "content": item.content,
                    "created_at": item.created_at.isoformat()
                    if item.created_at is not None
                    else "",
                }
                for item in request.history
            ],
            question_budget=question_budget,
            recent_question_count=min(recent_question_count, 10),
            country_code=request.country_code,
        )

    async def _validate(self, state: ConversationState) -> ConversationState:
        generated = state["generated"]
        request = state["generation_input"]
        issues = validate_conversation_output(generated.content, request.question_budget)
        return {"validation_issues": issues}

    async def _after_validation(self, state: ConversationState) -> Literal["valid", "repair"]:
        return "repair" if state["validation_issues"] else "valid"

    async def _repair(self, state: ConversationState) -> ConversationState:
        repaired = await self._provider.repair_conversation(
            state["generation_input"], state["validation_issues"]
        )
        remaining = validate_conversation_output(
            repaired.content, state["generation_input"].question_budget
        )
        if remaining:
            return {
                "generated": repaired,
                "final_content": safe_response("boundary", state["language"]),
                "route": "boundary",
                "block_reason": "invalid_generated_response",
            }
        return {"generated": repaired, "validation_issues": []}

    async def _finalize(self, state: ConversationState) -> ConversationState:
        if "final_content" in state:
            return {}
        return {"final_content": state["generated"].content.strip(), "route": "normal"}

    def _model_name(self, purpose: Literal["conversation", "auxiliary"]) -> str:
        return self._provider.model_name(purpose)


def validate_conversation_output(content: str, question_budget: int) -> list[str]:
    issues: list[str] = []
    stripped = content.strip()
    if not stripped or len(stripped) > 12_000:
        issues.append("invalid_length")
    if stripped.count("?") > question_budget:
        issues.append("question_budget_exceeded")
    lowered = stripped.casefold()
    if any(pattern in lowered for pattern in _DEPENDENCY_PATTERNS):
        issues.append("dependency_language")
    if _DIAGNOSIS_PATTERN.search(stripped):
        issues.append("diagnostic_claim")
    return issues


def _take_complete_sentences(buffer: str) -> tuple[str, str]:
    matches = list(_SENTENCE_BOUNDARY.finditer(buffer))
    if not matches:
        return "", buffer
    boundary = matches[-1].end()
    return buffer[:boundary], buffer[boundary:]


def _display_chunks(content: str, target_size: int = 56) -> Iterator[str]:
    """Quebra textos seguros em unidades legíveis sem cortar palavras."""
    start = 0
    while len(content) - start > target_size:
        boundary = content.rfind(" ", start, start + target_size + 1)
        if boundary <= start:
            boundary = min(start + target_size, len(content))
        else:
            boundary += 1
        yield content[start:boundary]
        start = boundary
    if start < len(content):
        yield content[start:]
