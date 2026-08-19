import re

from app.domain.models import GatewayDecision, SecurityThreat

_SECURITY_PATTERNS: tuple[tuple[SecurityThreat, re.Pattern[str]], ...] = (
    (
        "system_prompt_extraction",
        re.compile(
            r"(?i)(reveal|show|print|repeat|mostre|revele|imprima|muestre|revela).{0,40}"
            r"(system prompt|developer message|hidden instructions|prompt do sistema|"
            r"instrucciones ocultas)"
        ),
    ),
    (
        "prompt_injection",
        re.compile(
            r"(?i)(ignore|disregard|forget|ignore todas?|desconsidere|olvida).{0,40}"
            r"(previous|above|prior|instructions|regras|instruções|instrucciones)"
        ),
    ),
    (
        "jailbreak",
        re.compile(
            r"(?i)\b(jailbreak|do anything now|developer mode|modo dan|modo desenvolvedor)\b"
        ),
    ),
    (
        "data_exfiltration",
        re.compile(
            r"(?i)(other users?|outros usuários|otros usuarios|database dump|"
            r"dados de pacientes|patient data)"
        ),
    ),
    (
        "tool_abuse",
        re.compile(r"(?i)(run shell|execute command|delete database|drop table|execute código)"),
    ),
)

_ENCODED_PAYLOAD = re.compile(r"(?:[A-Za-z0-9+/]{80,}={0,2})")


def scan_input(text: str) -> GatewayDecision:
    threats: list[SecurityThreat] = []
    for threat, pattern in _SECURITY_PATTERNS:
        if pattern.search(text):
            threats.append(threat)
    if _ENCODED_PAYLOAD.search(text):
        threats.append("encoded_payload")
    if not threats:
        return GatewayDecision()
    return GatewayDecision(
        decision="review",
        threats=threats,
        reason_code="suspicious_instruction_pattern",
    )


def needs_safety_review(text: str) -> bool:
    normalized = text.casefold()
    triggers = (
        "me matar",
        "me machucar",
        "suicid",
        "kill myself",
        "hurt myself",
        "matarme",
        "hacerme daño",
        "dor no peito",
        "chest pain",
        "dolor en el pecho",
        "não consigo ficar seguro",
        "can't stay safe",
        "no puedo estar seguro",
        "dobrar meu remédio",
        "double my medication",
        "duplicar mi medicamento",
        "pare meu remédio",
        "stop my medication",
        "dejar mi medicamento",
        "qual remédio",
        "qual medicamento",
        "which medication",
        "what medication",
        "qué medicamento",
        "você é meu único apoio",
        "you are my only support",
        "eres mi único apoyo",
    )
    return any(trigger in normalized for trigger in triggers)
