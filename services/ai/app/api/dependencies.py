import secrets
from typing import Annotated

from fastapi import Depends, HTTPException, Security, status
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer

from app.core.config import Settings, get_settings
from app.services.base import AIService
from app.services.factory import get_ai_service

bearer = HTTPBearer(auto_error=False)


async def provide_settings() -> Settings:
    return get_settings()


async def provide_ai_service() -> AIService:
    return get_ai_service()


async def require_service_auth(
    credentials: Annotated[HTTPAuthorizationCredentials | None, Security(bearer)],
    settings: Annotated[Settings, Depends(provide_settings)],
) -> None:
    valid = (
        credentials is not None
        and credentials.scheme.lower() == "bearer"
        and secrets.compare_digest(credentials.credentials, settings.service_api_key)
    )
    if not valid:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="invalid service credential",
            headers={"WWW-Authenticate": "Bearer"},
        )


ServiceAuth = Annotated[None, Depends(require_service_auth)]
AIServiceDependency = Annotated[AIService, Depends(provide_ai_service)]
