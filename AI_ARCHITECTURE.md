# Arquitetura da camada de IA

## Limites do sistema

O Go é o control plane: autentica usuários, aplica consentimentos, cifra mensagens, mantém a
fila durável e persiste resultados. O FastAPI é um inference plane stateless. Ele recebe apenas
o recorte necessário, executa o LangGraph e devolve JSON estrito. Essa separação permite
escalar API, workers e inferência independentemente.

## Grafo conversacional

```text
prepare
  ├─ detect_language (local)
  └─ input_gateway (regras locais + moderação)
           │
       route_input
      ┌────┼──────────────┐
 security  safety review  normal
      │         │           │
 safe template │       generate once
                │           │
          crisis/boundary  quality gate
                │        valid │ repair once
                └──────────────┴─ finalize
```

O caminho normal faz uma única geração paga. Idioma é detectado localmente; não há tradução
obrigatória. O prompt envia orçamento de zero ou uma pergunta, histórico recente e idioma. O
modelo devolve conteúdo, modo e movimentos estruturados. Regras determinísticas barram
diagnóstico explícito, dependência emocional, tamanho inválido e excesso de perguntas.

## Grafo do relatório

Relatórios pequenos e médios fazem uma única síntese estruturada. Acima de 80 mil caracteres,
as mensagens são divididas em blocos de 40 mil caracteres. Extrações atômicas rodam com
concorrência limitada; a síntese final recebe apenas os fatos rastreáveis. O request inteiro é
limitado a 300 mil caracteres e 500 mensagens para impedir custo e memória sem limite.

Todo item e entrada de timeline cita IDs de mensagens do usuário. O validator rejeita fontes
do assistente, IDs desconhecidos, timestamps fora do período, falsa recorrência e afirmações
diagnósticas. O relatório nasce `pending_review`; o paciente pode excluir itens e entradas da
timeline, aprovar ou
rejeitar. O profissional só consulta relatórios aprovados.

## Providers e versionamento

`AIProvider` desacopla os grafos de OpenAI e mock. Prompts, modelos, schema e versões de grafo
são persistidos com o relatório/resposta. A troca de modelo não altera handlers ou regras de
negócio. O mock cobre todos os caminhos sem custo e é o padrão do Compose.

Configuração inicial:

- conversa e síntese: `gpt-5.6-terra`;
- classificação e extração condicional: `gpt-5.6-luna`;
- moderação: `omni-moderation-latest`;
- Responses API com `store=false`.

Antes de produção real, o ambiente precisa de secret manager, TLS entre serviços, política de
retenção aprovada e avaliação com o modelo real.
