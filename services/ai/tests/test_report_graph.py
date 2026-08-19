from datetime import UTC, datetime, timedelta
from uuid import uuid4

from app.core.config import Settings
from app.domain.models import AtomicFacts, JourneyReportDraft, ReportGenerationInput
from app.domain.schemas import (
    ContextItem,
    ContextMessage,
    ContextRequest,
    ReportCoverage,
    TimelineEntry,
)
from app.graphs.report.builder import ReportGraphRunner, validate_report
from app.providers.mock import MockProvider

NOW = datetime(2026, 8, 19, tzinfo=UTC)


def context_request() -> ContextRequest:
    return ContextRequest(
        request_id=uuid4(),
        connection_id=uuid4(),
        user_id=uuid4(),
        period_start=NOW - timedelta(days=7),
        period_end=NOW,
        messages=[
            ContextMessage(
                id=uuid4(),
                role="user",
                content="Adiei a tarefa e fiquei frustrado comigo.",
                created_at=NOW - timedelta(days=2),
            ),
            ContextMessage(
                id=uuid4(),
                role="assistant",
                content="Você pode tentar dividir a tarefa.",
                created_at=NOW - timedelta(days=2) + timedelta(minutes=1),
            ),
        ],
    )


async def test_report_uses_only_user_messages_as_evidence() -> None:
    app_settings = Settings(service_api_key="test-service-key-that-is-at-least-32-bytes")
    runner = ReportGraphRunner(MockProvider(app_settings), app_settings)
    request = context_request()

    response = await runner.process(request)

    assert response.coverage.user_message_count == 1
    assert response.items[0].source_message_ids == [request.messages[0].id]
    assert request.messages[1].id not in response.items[0].source_message_ids


class CountingReportProvider(MockProvider):
    def __init__(self, app_settings: Settings) -> None:
        super().__init__(app_settings)
        self.extraction_calls = 0
        self.report_calls = 0

    async def extract_facts(self, request: ReportGenerationInput) -> AtomicFacts:
        self.extraction_calls += 1
        return await super().extract_facts(request)

    async def generate_report(self, request: ReportGenerationInput) -> JourneyReportDraft:
        self.report_calls += 1
        return await super().generate_report(request)


async def test_large_report_uses_bounded_parallel_map_reduce() -> None:
    app_settings = Settings(
        service_api_key="test-service-key-that-is-at-least-32-bytes",
        report_direct_input_characters=10_000,
        report_chunk_characters=5_000,
    )
    provider = CountingReportProvider(app_settings)
    runner = ReportGraphRunner(provider, app_settings)
    request = ContextRequest(
        request_id=uuid4(),
        connection_id=uuid4(),
        user_id=uuid4(),
        period_start=NOW - timedelta(days=7),
        period_end=NOW,
        messages=[
            ContextMessage(
                id=uuid4(),
                role="user",
                content="a" * 6_000,
                created_at=NOW - timedelta(days=2),
            ),
            ContextMessage(
                id=uuid4(),
                role="user",
                content="b" * 6_000,
                created_at=NOW - timedelta(days=1),
            ),
        ],
    )

    response = await runner.process(request)

    assert response.coverage.user_message_count == 2
    assert provider.extraction_calls == 2
    assert provider.report_calls == 1


def test_grounding_validator_rejects_assistant_source_and_diagnosis() -> None:
    request = context_request()
    draft = JourneyReportDraft(
        title="Relatório",
        coverage=ReportCoverage(
            conversation_count=1,
            user_message_count=1,
            active_day_count=1,
            completeness="limited",
            note="Somente o que foi mencionado.",
        ),
        summary="O usuário tem depressão.",
        timeline=[
            TimelineEntry(
                description="Sugestão do assistente tratada como fato.",
                occurred_at=request.messages[1].created_at,
                source_message_ids=[request.messages[1].id],
            )
        ],
        items=[
            ContextItem(
                kind="emotion",
                title="Diagnóstico",
                description="O usuário apresenta depressão.",
                evidence_strength="explicit_repeated",
                source_message_ids=[request.messages[0].id],
            )
        ],
    )

    errors = validate_report(request, draft)

    assert "unknown_or_non_user_source" in errors
    assert "clinical_assertion" in errors
    assert "repeated_claim_has_one_source" in errors
