from collections.abc import AsyncIterator
from typing import Any

from app.core.config import Settings
from app.domain.models import ConversationGenerationInput, ConversationModelOutput
from app.providers.deepinfra import DeepInfraProvider


def deepinfra_settings() -> Settings:
    return Settings(
        service_api_key="test-service-key-that-is-at-least-32-bytes",
        provider="deepinfra",
        deepinfra_api_key="test-deepinfra-key",
    )


def test_deepinfra_uses_openai_compatible_chat_completions() -> None:
    provider = DeepInfraProvider(deepinfra_settings())
    model = provider._conversation_model

    assert provider.name == "deepinfra"
    assert provider.model_name("conversation") == "meta-llama/Meta-Llama-3.1-8B-Instruct"
    assert model.model_name == "meta-llama/Meta-Llama-3.1-8B-Instruct"
    assert str(model.openai_api_base).rstrip("/") == "https://api.deepinfra.com/v1/openai"
    assert model.extra_body == {"max_tokens": 320}
    assert model.temperature == 0.65
    assert provider._classification_model.extra_body == {"max_tokens": 192}
    assert provider._extraction_model.extra_body == {"max_tokens": 4096}
    assert provider._report_model.extra_body == {"max_tokens": 3072}
    assert model.use_responses_api is False


class FakeConversationModel:
    def __init__(self, content: str) -> None:
        self.content = content
        self.messages: list[Any] = []
        self.invoke_calls = 0

    async def ainvoke(self, messages: list[Any]) -> Any:
        self.invoke_calls += 1
        self.messages = messages
        return type("Response", (), {"text": self.content})()

    async def astream(self, messages: list[Any]) -> AsyncIterator[Any]:
        self.messages = messages
        for text in ("Estou aqui. ", "Pode continuar no seu ritmo."):
            yield type("Chunk", (), {"text": text})()


async def test_deepinfra_generates_structured_and_streaming_conversation() -> None:
    provider = DeepInfraProvider(deepinfra_settings())
    expected = ConversationModelOutput(
        content="Estou aqui. Pode continuar no seu ritmo.",
        mode="listen",
        primary_move="reflect_meaning",
    )
    fake = FakeConversationModel(expected.content)
    provider._conversation_model = fake  # type: ignore[assignment]
    request = ConversationGenerationInput(
        language="pt-BR",
        message="Hoje foi difícil.",
        question_budget=1,
        recent_question_count=0,
    )

    generated = await provider.generate_conversation(request)
    generation_system_prompt = fake.messages[0].content
    streamed = "".join([chunk async for chunk in provider.stream_conversation(request)])

    assert generated == expected
    assert streamed == expected.content
    assert fake.invoke_calls == 1
    assert "Return only the response text" in generation_system_prompt
    assert "Return exactly one valid JSON object" not in generation_system_prompt
    assert "prefer one focused question about that thread" in fake.messages[0].content
