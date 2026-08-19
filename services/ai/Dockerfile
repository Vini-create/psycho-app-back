FROM python:3.13-slim AS builder

ENV PIP_NO_CACHE_DIR=1 \
    UV_COMPILE_BYTECODE=1 \
    UV_LINK_MODE=copy \
    UV_PROJECT_ENVIRONMENT=/opt/venv

WORKDIR /app

RUN pip install --no-cache-dir uv==0.11.11

COPY pyproject.toml uv.lock ./
COPY app ./app

RUN uv sync --frozen --no-dev --no-editable

FROM builder AS test

COPY tests ./tests

RUN uv sync --frozen --extra dev --no-editable \
    && uv run ruff check app tests \
    && uv run pytest -q

FROM python:3.13-slim AS runtime

ENV PATH="/opt/venv/bin:$PATH" \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

WORKDIR /app

RUN groupadd --system anamnesys && useradd --system --gid anamnesys anamnesys

COPY --from=builder /opt/venv /opt/venv

USER anamnesys

EXPOSE 8000

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000"]
