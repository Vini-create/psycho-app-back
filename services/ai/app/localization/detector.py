from functools import lru_cache

from lingua import LanguageDetector, LanguageDetectorBuilder


class LocalLanguageDetector:
    def __init__(
        self,
        detector: LanguageDetector | None = None,
        default_locale: str = "pt-BR",
    ) -> None:
        self._detector = detector
        self._default_locale = normalize_locale(default_locale) or "pt-BR"

    def detect(self, text: str, locale_hint: str | None = None) -> str:
        hint = normalize_locale(locale_hint)
        if hint is not None:
            return hint
        if len(text.strip()) < 20:
            return self._default_locale
        detector = self._detector or _build_detector()
        language = detector.detect_language_of(text)
        if language is None or language.iso_code_639_1 is None:
            return self._default_locale
        code = language.iso_code_639_1.name.lower()
        return code


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
    return LanguageDetectorBuilder.from_all_languages().with_low_accuracy_mode().build()
