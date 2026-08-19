from typing import TypedDict

from app.domain.models import AtomicFact, JourneyReportDraft, ReportGenerationInput
from app.domain.schemas import ContextRequest, ContextResponse


class ReportState(TypedDict, total=False):
    request: ContextRequest
    generation_input: ReportGenerationInput
    chunks: list[ReportGenerationInput]
    facts: list[AtomicFact]
    draft: JourneyReportDraft
    validation_errors: list[str]
    response: ContextResponse
