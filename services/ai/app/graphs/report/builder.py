import asyncio
import re
from datetime import UTC, datetime
from typing import Literal, cast
from uuid import UUID

from langgraph.graph import END, START, StateGraph
from langgraph.graph.state import CompiledStateGraph

from app.core.config import Settings
from app.domain.models import AtomicFact, JourneyReportDraft, ReportGenerationInput
from app.domain.schemas import ContextRequest, ContextResponse
from app.graphs.report.state import ReportState
from app.services.base import AIProvider

_CLINICAL_ASSERTION = re.compile(
    r"(?i)\b(has|has signs of|suffers from|tem|apresenta|sofre de|tiene|presenta)\s+"
    r"(depression|depressão|depresión|bipolar|adhd|tdah|personality disorder|"
    r"transtorno de personalidade)\b"
)


class ReportGraphRunner:
    def __init__(self, provider: AIProvider, settings: Settings) -> None:
        self._provider = provider
        self._settings = settings
        self._graph = self._build_graph()

    def _build_graph(
        self,
    ) -> CompiledStateGraph[ReportState, None, ReportState, ReportState]:
        graph = StateGraph(ReportState)
        graph.add_node("prepare", self._prepare)
        graph.add_node("generate_direct", self._generate_direct)
        graph.add_node("extract_large", self._extract_large)
        graph.add_node("synthesize", self._synthesize)
        graph.add_node("validate", self._validate)
        graph.add_node("finalize", self._finalize)
        graph.add_edge(START, "prepare")
        graph.add_conditional_edges(
            "prepare",
            self._after_prepare,
            {"direct": "generate_direct", "chunked": "extract_large"},
        )
        graph.add_edge("generate_direct", "validate")
        graph.add_edge("extract_large", "synthesize")
        graph.add_edge("synthesize", "validate")
        graph.add_edge("validate", "finalize")
        graph.add_edge("finalize", END)
        return graph.compile()

    async def process(self, request: ContextRequest) -> ContextResponse:
        state = cast(ReportState, await self._graph.ainvoke({"request": request}))
        return state["response"]

    async def _prepare(self, state: ReportState) -> ReportState:
        request = state["request"]
        user_messages = [message for message in request.messages if message.role == "user"]
        if not user_messages:
            raise ValueError("report requires at least one user message")
        serialized = [
            {
                "id": str(message.id),
                "conversation_id": str(message.conversation_id)
                if message.conversation_id is not None
                else "",
                "role": message.role,
                "content": message.content,
                "created_at": message.created_at.astimezone(UTC).isoformat(),
            }
            for message in user_messages
        ]
        generation_input = ReportGenerationInput(
            period_start=request.period_start,
            period_end=request.period_end,
            target_locale=request.target_locale,
            messages=serialized,
        )
        total_characters = sum(len(message["content"]) for message in serialized)
        if total_characters <= self._settings.report_direct_input_characters:
            return {"generation_input": generation_input, "chunks": []}
        chunks = [
            ReportGenerationInput(
                period_start=request.period_start,
                period_end=request.period_end,
                target_locale=request.target_locale,
                messages=messages,
            )
            for messages in chunk_messages(serialized, self._settings.report_chunk_characters)
        ]
        return {"generation_input": generation_input, "chunks": chunks}

    async def _after_prepare(self, state: ReportState) -> Literal["direct", "chunked"]:
        return "chunked" if state["chunks"] else "direct"

    async def _generate_direct(self, state: ReportState) -> ReportState:
        draft = await self._provider.generate_report(state["generation_input"])
        return {"draft": draft}

    async def _extract_large(self, state: ReportState) -> ReportState:
        semaphore = asyncio.Semaphore(self._settings.report_extraction_concurrency)

        async def extract(chunk: ReportGenerationInput) -> list[AtomicFact]:
            async with semaphore:
                result = await self._provider.extract_facts(chunk)
            allowed = {UUID(message["id"]) for message in chunk.messages}
            for fact in result.facts:
                if any(source_id not in allowed for source_id in fact.source_message_ids):
                    raise ValueError("extracted fact cites a source outside its chunk")
                if fact.occurred_at is not None and (
                    fact.occurred_at < chunk.period_start or fact.occurred_at >= chunk.period_end
                ):
                    raise ValueError("extracted fact timestamp is outside the report period")
            return result.facts

        extracted = await asyncio.gather(*(extract(chunk) for chunk in state["chunks"]))
        facts = deduplicate_facts([fact for group in extracted for fact in group])
        if not facts or len(facts) > 500:
            raise ValueError("large report extraction produced an invalid number of facts")
        return {"facts": facts}

    async def _synthesize(self, state: ReportState) -> ReportState:
        original = state["generation_input"]
        reduced = ReportGenerationInput(
            period_start=original.period_start,
            period_end=original.period_end,
            target_locale=original.target_locale,
            facts=state["facts"],
        )
        draft = await self._provider.generate_report(reduced)
        return {"draft": draft}

    async def _validate(self, state: ReportState) -> ReportState:
        errors = validate_report(state["request"], state["draft"])
        if errors:
            raise ValueError("invalid grounded report: " + ", ".join(errors))
        return {"validation_errors": []}

    async def _finalize(self, state: ReportState) -> ReportState:
        draft = state["draft"]
        user_messages = [message for message in state["request"].messages if message.role == "user"]
        active_days = len({message.created_at.date() for message in user_messages})
        conversation_ids = {
            message.conversation_id
            for message in user_messages
            if message.conversation_id is not None
        }
        coverage = draft.coverage.model_copy(
            update={
                "conversation_count": len(conversation_ids)
                if conversation_ids
                else draft.coverage.conversation_count,
                "user_message_count": len(user_messages),
                "active_day_count": active_days,
            }
        )
        response = ContextResponse(
            title=draft.title,
            coverage=coverage,
            summary=draft.summary,
            timeline=draft.timeline,
            items=draft.items,
            limitations=draft.limitations,
            provider=self._provider.name,
            model=self._provider.model_name("report"),
            prompt_version=self._settings.context_prompt_version,
            graph_version=self._settings.report_graph_version,
        )
        return {"response": response}


def validate_report(request: ContextRequest, draft: JourneyReportDraft) -> list[str]:
    user_messages = {message.id: message for message in request.messages if message.role == "user"}
    errors: list[str] = []

    def validate_sources(
        source_ids: list[UUID], occurred_at: datetime | None, strength: str | None = None
    ) -> None:
        unique = set(source_ids)
        if any(source_id not in user_messages for source_id in unique):
            errors.append("unknown_or_non_user_source")
        if strength == "explicit_repeated" and len(unique) < 2:
            errors.append("repeated_claim_has_one_source")
        if occurred_at is not None:
            if occurred_at < request.period_start or occurred_at >= request.period_end:
                errors.append("occurred_at_outside_period")

    for entry in draft.timeline:
        validate_sources(entry.source_message_ids, entry.occurred_at)
    for item in draft.items:
        validate_sources(item.source_message_ids, item.occurred_at, item.evidence_strength)
        if _CLINICAL_ASSERTION.search(f"{item.title} {item.description} {item.impact or ''}"):
            errors.append("clinical_assertion")
    if _CLINICAL_ASSERTION.search(draft.summary):
        errors.append("clinical_assertion")
    return sorted(set(errors))


def chunk_messages(messages: list[dict[str, str]], limit: int) -> list[list[dict[str, str]]]:
    chunks: list[list[dict[str, str]]] = []
    current: list[dict[str, str]] = []
    current_size = 0
    for message in messages:
        message_size = len(message["content"])
        if current and current_size + message_size > limit:
            chunks.append(current)
            current = []
            current_size = 0
        current.append(message)
        current_size += message_size
    if current:
        chunks.append(current)
    return chunks


def deduplicate_facts(facts: list[AtomicFact]) -> list[AtomicFact]:
    unique: list[AtomicFact] = []
    seen: set[tuple[str, tuple[UUID, ...]]] = set()
    for fact in facts:
        key = (fact.description.casefold().strip(), tuple(sorted(fact.source_message_ids)))
        if key in seen:
            continue
        seen.add(key)
        unique.append(fact)
    return unique
