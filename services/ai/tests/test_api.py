import os

os.environ.setdefault("AI_SERVICE_API_KEY", "test-service-key-that-is-at-least-32-bytes")

from fastapi.testclient import TestClient  # noqa: E402

from app.main import app  # noqa: E402

client = TestClient(app)
headers = {"Authorization": f"Bearer {os.environ['AI_SERVICE_API_KEY']}"}


def test_health_is_public() -> None:
    response = client.get("/health")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_companion_requires_service_auth() -> None:
    response = client.post("/v1/companion/respond", json={})
    assert response.status_code == 401


def test_companion_mock_contract() -> None:
    response = client.post(
        "/v1/companion/respond",
        headers=headers,
        json={
            "request_id": "778243e3-24ea-4c17-98d6-b176db72bce5",
            "conversation_id": "68d94ac0-e6e1-450f-940a-f517f26d5fb7",
            "user_id": "0dbce025-9bd1-46b7-aadc-37ccbcfe97e7",
            "message": "Olá",
            "history": [],
        },
    )
    assert response.status_code == 200
    assert response.json()["provider"] == "mock"


def test_context_preserves_source_traceability() -> None:
    message_id = "6558cb14-f38f-4474-b634-29b73408285e"
    response = client.post(
        "/v1/context/process",
        headers=headers,
        json={
            "request_id": "778243e3-24ea-4c17-98d6-b176db72bce5",
            "connection_id": "68d94ac0-e6e1-450f-940a-f517f26d5fb7",
            "user_id": "0dbce025-9bd1-46b7-aadc-37ccbcfe97e7",
            "period_start": "2026-08-18T00:00:00Z",
            "period_end": "2026-08-19T00:00:00Z",
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
    assert response.json()["items"][0]["source_message_ids"] == [message_id]
