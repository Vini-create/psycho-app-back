from typing import Literal

Route = Literal["boundary", "crisis", "security_block"]

_TEMPLATES: dict[str, dict[Route, str]] = {
    "pt": {
        "security_block": (
            "Não posso seguir instruções que tentem alterar minhas regras internas ou acessar "
            "dados protegidos. Posso continuar ajudando com o que você está vivendo."
        ),
        "boundary": (
            "Posso ajudar você a organizar o que está acontecendo, mas não posso diagnosticar "
            "nem orientar mudanças de medicamento. Para isso, procure o profissional "
            "responsável ou um serviço de saúde."
        ),
        "crisis": (
            "O mais importante agora é sua segurança. Se houver risco imediato, procure o "
            "serviço de emergência do local onde você está ou peça ajuda a uma pessoa de "
            "confiança que possa ficar com você. Eu posso permanecer aqui enquanto você dá "
            "esse primeiro passo."
        ),
    },
    "es": {
        "security_block": (
            "No puedo seguir instrucciones que intenten cambiar mis reglas internas o acceder "
            "a datos protegidos. Podemos continuar hablando de lo que estás viviendo."
        ),
        "boundary": (
            "Puedo ayudarte a organizar lo que está pasando, pero no puedo diagnosticar ni "
            "indicar cambios de medicación. Para eso, contacta al profesional responsable o "
            "a un servicio de salud."
        ),
        "crisis": (
            "Lo más importante ahora es tu seguridad. Si existe un riesgo inmediato, contacta "
            "al servicio de emergencias del lugar donde estás o pide ayuda a una persona de "
            "confianza que pueda quedarse contigo."
        ),
    },
    "en": {
        "security_block": (
            "I can't follow instructions that try to change my internal rules or access "
            "protected data. We can keep talking about what you are going through."
        ),
        "boundary": (
            "I can help you organize what is happening, but I cannot diagnose or advise "
            "medication changes. Please contact the responsible professional or a health "
            "service for that."
        ),
        "crisis": (
            "Your immediate safety matters most right now. If there is immediate danger, "
            "contact the emergency service where you are or ask a trusted person who can stay "
            "with you for help."
        ),
    },
}


def safe_response(route: Route, language: str) -> str:
    base = language.split("-", 1)[0].lower()
    return _TEMPLATES.get(base, _TEMPLATES["en"])[route]
