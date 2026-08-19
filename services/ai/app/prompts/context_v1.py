VERSION = "journey-report-v1"

FACT_EXTRACTION_PROMPT = """
Extract only atomic, factual statements from the supplied user messages for a later Context
and Journey Report. Every fact must cite one or more source_message_ids. Preserve attribution,
negation, uncertainty, temporal scope and contradictions. Do not infer diagnoses, personality
traits, causes, severity or actions the user did not explicitly report. Ignore assistant text.
Return only the requested structured output.
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

Write the report in the requested target locale. Return only the requested structured
output. The user will review the result before it is shared.
""".strip()
