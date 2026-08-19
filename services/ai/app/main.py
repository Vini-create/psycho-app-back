from fastapi import FastAPI

from app.api.routes import companion, context, health
from app.core.config import get_settings

settings = get_settings()

app = FastAPI(
    title="Anamnesys AI",
    version="0.1.0",
    docs_url="/docs" if settings.environment == "development" else None,
    redoc_url=None,
)

app.include_router(health.router)
app.include_router(companion.router, prefix="/v1")
app.include_router(context.router, prefix="/v1")
