import logging
from contextvars import ContextVar, Token
from time import monotonic
from uuid import UUID, uuid4

from fastapi import Request, Response
from starlette.middleware.base import BaseHTTPMiddleware, RequestResponseEndpoint

request_id_context: ContextVar[str] = ContextVar("request_id", default="unavailable")
logger = logging.getLogger("uvicorn.error")


class SafeRequestLoggingMiddleware(BaseHTTPMiddleware):
    async def dispatch(self, request: Request, call_next: RequestResponseEndpoint) -> Response:
        request_id = validated_request_id(request.headers.get("X-Request-ID"))
        token: Token[str] = request_id_context.set(request_id)
        started_at = monotonic()
        status_code = 500
        try:
            response = await call_next(request)
            status_code = response.status_code
            response.headers["X-Request-ID"] = request_id
            return response
        finally:
            duration_ms = round((monotonic() - started_at) * 1000, 2)
            logger.info(
                "request_completed request_id=%s method=%s route=%s status_code=%d "
                "duration_ms=%.2f",
                request_id,
                request.method,
                request.url.path,
                status_code,
                duration_ms,
            )
            request_id_context.reset(token)


def validated_request_id(value: str | None) -> str:
    if value is None:
        return str(uuid4())
    try:
        return str(UUID(value))
    except ValueError:
        return str(uuid4())
