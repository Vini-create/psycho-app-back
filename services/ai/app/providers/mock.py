from datetime import datetime
from uuid import UUID

from app.core.config import Settings
from app.domain.models import (
    AtomicFact,
    AtomicFacts,
    ConversationGenerationInput,
    ConversationModelOutput,
    GatewayDecision,
    JourneyReportDraft,
    ModerationDecision,
    ReportGenerationInput,
    SafetyDecision,
)
from app.domain.schemas import ContextItem, ReportCoverage, TimelineEntry


class MockProvider:
    name = "mock"

    def __init__(self, settings: Settings) -> None:
        self._settings = settings

    async def moderate(self, text: str) -> ModerationDecision:
        return ModerationDecision(flagged=False)

    async def classify_security(self, text: str, language: str) -> GatewayDecision:
        return GatewayDecision(
            decision="block",
            threats=["prompt_injection"],
            reason_code="confirmed_prompt_injection",
        )

    async def classify_safety(self, text: str, language: str) -> SafetyDecision:
        normalized = text.casefold()
        if any(term in normalized for term in ("me matar", "kill myself", "matarme")):
            return SafetyDecision(route="crisis", reason_code="self_harm_immediate")
        return SafetyDecision(route="boundary", reason_code="professional_boundary")

    async def generate_conversation(
        self, request: ConversationGenerationInput
    ) -> ConversationModelOutput:
        content_by_language = {
            "pt": (
                "Estou acompanhando. Parece que há algo importante aí, e não precisamos "
                "transformar isso imediatamente em uma solução."
            ),
            "es": (
                "Te sigo. Parece que hay algo importante ahí y no necesitamos convertirlo "
                "inmediatamente en una solución."
            ),
            "en": (
                "I'm with you. There seems to be something important there, and we don't "
                "need to turn it into a solution immediately."
            ),
        }
        language = request.language.split("-", 1)[0]
        return ConversationModelOutput(
            content=content_by_language.get(language, content_by_language["en"]),
            mode="listen",
            primary_move="reflect_meaning",
        )

    async def repair_conversation(
        self, request: ConversationGenerationInput, issues: list[str]
    ) -> ConversationModelOutput:
        return await self.generate_conversation(request)

    async def extract_facts(self, request: ReportGenerationInput) -> AtomicFacts:
        message = request.messages[0]
        return AtomicFacts(
            facts=[
                AtomicFact(
                    kind="open_topic",
                    title="Tema relatado",
                    description="Tema mock extraído de uma mensagem do usuário.",
                    evidence_strength="explicit_once",
                    occurred_at=datetime.fromisoformat(message["created_at"]),
                    source_message_ids=[UUID(message["id"])],
                )
            ]
        )

    async def generate_report(self, request: ReportGenerationInput) -> JourneyReportDraft:
        created_at: datetime | None
        if request.messages:
            first = request.messages[0]
            source_id = UUID(first["id"])
            created_at = datetime.fromisoformat(first["created_at"])
            evidence_count = len(request.messages)
            active_days = len({message["created_at"][:10] for message in request.messages})
            conversation_count = (
                len(
                    {
                        message["conversation_id"]
                        for message in request.messages
                        if message.get("conversation_id")
                    }
                )
                or 1
            )
        else:
            first_fact = request.facts[0]
            source_id = first_fact.source_message_ids[0]
            created_at = first_fact.occurred_at
            evidence_count = len(request.facts)
            active_days = 1
            conversation_count = 1
        return JourneyReportDraft(
            title="Relatório de Contexto e Jornada",
            coverage=ReportCoverage(
                conversation_count=conversation_count,
                user_message_count=evidence_count,
                active_day_count=active_days,
                completeness="limited" if evidence_count < 5 else "partial",
                note="O relatório cobre apenas os assuntos mencionados nas conversas do período.",
            ),
            summary=(
                "Relatório mock baseado em uma evidência do usuário."
                if evidence_count == 1
                else f"Relatório mock baseado em {evidence_count} evidências do usuário."
            ),
            timeline=[
                TimelineEntry(
                    description="Primeiro relato considerado no período.",
                    occurred_at=created_at,
                    source_message_ids=[source_id],
                )
            ],
            items=[
                ContextItem(
                    kind="open_topic",
                    title="Tema relatado",
                    description="Tema mock rastreável à primeira mensagem do período.",
                    evidence_strength="explicit_once",
                    occurred_at=created_at,
                    source_message_ids=[source_id],
                )
            ],
            limitations=["Conteúdo produzido pelo provider mock para teste de integração."],
        )
