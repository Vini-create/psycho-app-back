from collections.abc import Sequence
from functools import lru_cache

from lingua import Language, LanguageDetector, LanguageDetectorBuilder

_SHORT_PHRASES: dict[str, str] = {
    "bom dia": "pt",
    "boa noite": "pt",
    "boa tarde": "pt",
    "e aí": "pt",
    "eai": "pt",
    "não": "pt",
    "obrigada": "pt",
    "obrigado": "pt",
    "ola": "pt",
    "olá": "pt",
    "oi": "pt",
    "quem e voce": "pt",
    "quem é você": "pt",
    "sim": "pt",
    "tudo bem": "pt",
    "valeu": "pt",
    "buenas noches": "es",
    "buenas tardes": "es",
    "buenos días": "es",
    "hola": "es",
    "quién eres": "es",
    "sí": "es",
    "good afternoon": "en",
    "good evening": "en",
    "good morning": "en",
    "hello": "en",
    "hi": "en",
    "who are you": "en",
    "yes": "en",
    "non": "fr",
    "oui": "fr",
}


class LocalLanguageDetector:
    def __init__(
        self,
        detector: LanguageDetector | None = None,
        default_locale: str = "pt-BR",
    ) -> None:
        self._detector = detector
        self._default_locale = normalize_locale(default_locale) or "pt-BR"

    def detect(
        self,
        text: str,
        locale_hint: str | None = None,
        history: Sequence[str] | None = None,
    ) -> str:
        hint = normalize_locale(locale_hint)
        detector = self._detector or _build_detector()

        detected = _detect_text(detector, text)
        if detected is None and history:
            detected = _detect_text(detector, " ".join(history[-6:]))
        if detected is not None:
            regional_locale = hint or self._default_locale
            if regional_locale.split("-", 1)[0] == detected:
                return regional_locale
            return detected
        return hint or self._default_locale


def _detect_text(detector: LanguageDetector, text: str) -> str | None:
    normalized = " ".join(text.strip().lower().rstrip("!?.,").split())
    if not normalized:
        return None
    explicit = _SHORT_PHRASES.get(normalized)
    if explicit is not None:
        return explicit
    if len(normalized) < 4:
        return None
    language = detector.detect_language_of(text)
    if language is None or language.iso_code_639_1 is None:
        return None
    return language.iso_code_639_1.name.lower()


def normalize_locale(value: str | None) -> str | None:
    if value is None:
        return None
    parts = value.strip().replace("_", "-").split("-", 1)
    if not parts[0].isalpha() or len(parts[0]) not in (2, 3):
        return None
    language = parts[0].lower()
    if len(parts) == 1:
        return language
    region = parts[1].upper()
    if not region.isalpha() or len(region) != 2:
        return language
    return f"{language}-{region}"


@lru_cache
def _build_detector() -> LanguageDetector:
    return (
        LanguageDetectorBuilder.from_languages(
            Language.PORTUGUESE,
            Language.ENGLISH,
            Language.SPANISH,
            Language.FRENCH,
        )
        .with_minimum_relative_distance(0.2)
        .build()
    )
