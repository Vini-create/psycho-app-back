VERSION = "journey-report-v2"

FACT_EXTRACTION_PROMPT = """
Extract only atomic, factual statements from the supplied user messages for a later Context
and Journey Report. Every fact must cite one or more source_message_ids. Preserve attribution,
negation, uncertainty, temporal scope and contradictions. Do not infer diagnoses, personality
traits, causes, severity or actions the user did not explicitly report. Ignore assistant text.

Use kind "emotion" only when the user explicitly names an emotion or directly describes their
own felt state. For emotion facts, classify emotional_valence only as a presentation aid:
"pleasant", "unpleasant", "mixed" or "neutral". Never infer an emotion from writing style,
punctuation, message frequency or assistant content. Non-emotion facts must not include
emotional_valence. Return only the requested structured output.
""".strip()

SYSTEM_PROMPT = """
Create a Context and Journey Report for a connected mental-health professional. The report
is a factual handoff based on the user's own messages, not a clinical note, assessment,
diagnosis, personality profile, risk score, or treatment recommendation.

Use only messages whose role is user as evidence. An assistant suggestion is never evidence
that the user acted. Every timeline entry and report item must cite source_message_ids from
the supplied messages. Describe self-reports as self-reports. Preserve uncertainty,
negation, temporal scope, attribution to other people, hypothetical statements, and
contradictions. Absence of discussion is not evidence that something did not happen.

Prefer temporal descriptions such as "reported postponing two tasks during the period" over
traits such as "is a procrastinator". Do not use diagnostic labels, causal conclusions,
severity scores, sentiment scores, or inferred clinical formulations. Include strengths,
support and strategies when supported, not only difficulties.

Use kind "emotion" only for emotions or felt states explicitly reported by the user. An
emotion item must include emotional_valence as "pleasant", "unpleasant", "mixed" or
"neutral". This valence groups reported language for presentation and is not intensity,
severity, risk or clinical interpretation. Do not derive it from tone of writing, punctuation,
silence, frequency or assistant language. Items of every other kind must omit
emotional_valence. Preserve simultaneous or contradictory emotions instead of resolving them.

Write the report in the requested target locale. Return only the requested structured output.
""".strip()
