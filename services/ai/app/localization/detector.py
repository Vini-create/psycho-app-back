from functools import lru_cache

from lingua import LanguageDetector, LanguageDetectorBuilder


class LocalLanguageDetector:
    def __init__(self, detector: LanguageDetector | None = None) -> None:
        self._detector = detector

    def detect(self, text: str, locale_hint: str | None = None) -> str:
        hint = normalize_locale(locale_hint)
        if len(text.strip()) < 12 and hint is not None:
            return hint
        detector = self._detector or _build_detector()
        language = detector.detect_language_of(text)
        if language is None or language.iso_code_639_1 is None:
            return hint or "en"
        code = language.iso_code_639_1.name.lower()
        if hint is not None and hint.split("-", 1)[0] == code:
            return hint
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
