from fastapi import APIRouter

from app.api.dependencies import AIServiceDependency, ServiceAuth
from app.domain.schemas import CompanionRequest, CompanionResponse

router = APIRouter(tags=["companion"])


@router.post("/companion/respond", response_model=CompanionResponse)
async def respond(
    request: CompanionRequest,
    _: ServiceAuth,
    service: AIServiceDependency,
) -> CompanionResponse:
    return await service.respond(request)
