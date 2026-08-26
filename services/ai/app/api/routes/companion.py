import asyncio
import json
from collections.abc import AsyncIterator
from contextlib import suppress

from fastapi import APIRouter
from fastapi.responses import StreamingResponse

from app.api.dependencies import AIServiceDependency, ServiceAuth
from app.domain.schemas import CompanionRequest, CompanionResponse, CompanionStreamEvent

router = APIRouter(tags=["companion"])


@router.post("/companion/respond", response_model=CompanionResponse)
async def respond(
    request: CompanionRequest,
    _: ServiceAuth,
    service: AIServiceDependency,
) -> CompanionResponse:
    return await service.respond(request)


@router.post("/companion/respond/stream")
async def respond_stream(
    request: CompanionRequest,
    _: ServiceAuth,
    service: AIServiceDependency,
) -> StreamingResponse:
    async def events() -> AsyncIterator[bytes]:
        # Abre a resposta antes de moderação/classificação. O heartbeat impede
        # que proxies encerrem uma geração silenciosa mais demorada.
        yield b'{"type":"start"}\n'
        iterator = service.stream(request).__aiter__()
        pending: asyncio.Future[CompanionStreamEvent] = asyncio.ensure_future(anext(iterator))
        try:
            while True:
                completed, _ = await asyncio.wait({pending}, timeout=10)
                if not completed:
                    yield b'{"type":"heartbeat"}\n'
                    continue
                try:
                    event = pending.result()
                except StopAsyncIteration:
                    break
                yield (
                    json.dumps(
                        event.model_dump(mode="json", exclude_none=True),
                        ensure_ascii=False,
                        separators=(",", ":"),
                    ).encode("utf-8")
                    + b"\n"
                )
                pending = asyncio.ensure_future(anext(iterator))
        except Exception:
            yield b'{"type":"error","code":"generation_failed"}\n'
        finally:
            if not pending.done():
                pending.cancel()
                with suppress(asyncio.CancelledError):
                    await pending

    return StreamingResponse(
        events(),
        media_type="application/x-ndjson",
        headers={
            "Cache-Control": "no-cache, no-transform",
            "X-Accel-Buffering": "no",
        },
    )
