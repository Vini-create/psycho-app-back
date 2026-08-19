import os
from collections.abc import AsyncIterator

import pytest
from httpx import ASGITransport, AsyncClient

os.environ.setdefault("AI_SERVICE_API_KEY", "test-service-key-that-is-at-least-32-bytes")

from app.main import app  # noqa: E402

HEADERS = {"Authorization": f"Bearer {os.environ['AI_SERVICE_API_KEY']}"}


@pytest.fixture
async def client() -> AsyncIterator[AsyncClient]:
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as test_client:
        yield test_client


async def test_health_is_public(client: AsyncClient) -> None:
    response = await client.get("/health")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


async def test_companion_requires_service_auth(client: AsyncClient) -> None:
    response = await client.post("/v1/companion/respond", json={})
    assert response.status_code == 401


async def test_companion_mock_contract(client: AsyncClient) -> None:
    response = await client.post(
        "/v1/companion/respond",
        headers=HEADERS,
        json={
            "request_id": "778243e3-24ea-4c17-98d6-b176db72bce5",
            "conversation_id": "68d94ac0-e6e1-450f-940a-f517f26d5fb7",
            "user_id": "0dbce025-9bd1-46b7-aadc-37ccbcfe97e7",
            "message": "Olá",
            "history": [],
            "locale_hint": "pt-BR",
        },
    )
    assert response.status_code == 200
    body = response.json()
    assert body["provider"] == "mock"
    assert body["route"] == "normal"
    assert body["language"] == "pt-BR"


async def test_context_preserves_source_traceability(client: AsyncClient) -> None:
    message_id = "6558cb14-f38f-4474-b634-29b73408285e"
    response = await client.post(
        "/v1/context/process",
        headers=HEADERS,
        json={
            "request_id": "778243e3-24ea-4c17-98d6-b176db72bce5",
            "connection_id": "68d94ac0-e6e1-450f-940a-f517f26d5fb7",
            "user_id": "0dbce025-9bd1-46b7-aadc-37ccbcfe97e7",
            "period_start": "2026-08-18T00:00:00Z",
            "period_end": "2026-08-19T00:00:00Z",
            "target_locale": "pt-BR",
            "messages": [
                {
                    "id": message_id,
                    "role": "user",
                    "content": "Mensagem de teste",
                    "created_at": "2026-08-18T12:00:00Z",
                }
            ],
        },
    )
    assert response.status_code == 200
    body = response.json()
    assert body["schema_version"] == "journey-report-v1"
    assert body["items"][0]["source_message_ids"] == [message_id]


async def test_context_rejects_oversized_input_before_inference(client: AsyncClient) -> None:
    messages = [
        {
            "id": f"00000000-0000-4000-8000-{index:012d}",
            "role": "user",
            "content": "a" * 8_000,
            "created_at": "2026-08-18T12:00:00Z",
        }
        for index in range(38)
    ]
    response = await client.post(
        "/v1/context/process",
        headers=HEADERS,
        json={
            "request_id": "778243e3-24ea-4c17-98d6-b176db72bce5",
            "connection_id": "68d94ac0-e6e1-450f-940a-f517f26d5fb7",
            "user_id": "0dbce025-9bd1-46b7-aadc-37ccbcfe97e7",
            "period_start": "2026-08-18T00:00:00Z",
            "period_end": "2026-08-19T00:00:00Z",
            "messages": messages,
        },
    )
    assert response.status_code == 422
