from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

from fastapi import FastAPI

from app.api.routes import companion, context, health
from app.core.config import get_settings
from app.core.observability import SafeRequestLoggingMiddleware
from app.providers.factory import get_provider

settings = get_settings()


@asynccontextmanager
async def lifespan(_: FastAPI) -> AsyncIterator[None]:
    yield
    if get_provider.cache_info().currsize:
        await get_provider().aclose()


app = FastAPI(
    title="Anamnesys AI",
    version="0.1.0",
    docs_url="/docs" if settings.environment == "development" else None,
    redoc_url=None,
    lifespan=lifespan,
)

app.add_middleware(SafeRequestLoggingMiddleware)

app.include_router(health.router)
app.include_router(companion.router, prefix="/v1")
app.include_router(context.router, prefix="/v1")
