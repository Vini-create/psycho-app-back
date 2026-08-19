from fastapi import APIRouter

from app.api.dependencies import AIServiceDependency, ServiceAuth
from app.domain.schemas import ContextRequest, ContextResponse

router = APIRouter(tags=["context"])


@router.post("/context/process", response_model=ContextResponse)
async def process_context(
    request: ContextRequest,
    _: ServiceAuth,
    service: AIServiceDependency,
) -> ContextResponse:
    return await service.process_context(request)
