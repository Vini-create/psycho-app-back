# Roadmap do MVP do Backend

Objetivo: entregar um monólito modular em Go, seguro para dados sensíveis, preparado para IA e com contrato HTTP estável para integração com o frontend.

1. **Fundação da aplicação** — Estruturar os módulos, carregar e validar configurações, iniciar o servidor HTTP, implementar health checks e graceful shutdown.
2. **Padrões da API** — Definir versionamento, JSON, validação, paginação, erros padronizados, request IDs, CORS e contrato OpenAPI.
3. **Persistência** — Integrar PostgreSQL, migrations versionadas, pool de conexões, transações e testes de integração com banco real.
4. **Identidade e autenticação** — Implementar contas, login, recuperação de acesso, sessões/tokens e armazenamento seguro de credenciais.
5. **Autorização e multi-tenancy** — Modelar organizações, profissionais, papéis, permissões e isolamento de dados por tenant.
6. **Pacientes e vínculos de acompanhamento** — Criar pacientes, relações profissional–paciente, convites com expiração e ciclo de encerramento do acompanhamento.
7. **Consentimento e auditoria** — Versionar termos, registrar consentimentos e revogações e manter trilha de auditoria para operações sensíveis.
8. **Conversas** — Implementar threads e mensagens, autoria, ordenação, histórico paginado e streaming de respostas para o frontend.
9. **Integração com IA** — Criar uma porta independente de provedor para o companion, com prompts versionados, limites, timeouts, retries, safety e controle de custos.
10. **Processamento assíncrono confiável** — Adicionar jobs, workers, idempotência, política de retry e rastreamento de falhas para tarefas de IA.
11. **Extração de eventos** — Gerar eventos estruturados com categoria, relevância, confiança e referências rastreáveis às mensagens de origem.
12. **Memória longitudinal** — Consolidar eventos e resumos periódicos, recuperar contexto relevante e preservar a distinção entre relato e inferência.
13. **Experiência do profissional** — Expor timeline, temas recorrentes, itens para a próxima sessão e resumo desde a última sessão.
14. **Privacidade e confiabilidade** — Implementar minimização, retenção, exportação e exclusão de dados, além de testes unitários, integração, E2E e segurança.
15. **Produção e integração** — Finalizar logs, métricas e traces, CI/CD, secrets, deploy, readiness, documentação OpenAPI publicada e ambiente consumível pelo frontend.

Cada etapa deve ser entregue como uma vertical slice testável e acompanhada por uma decisão arquitetural quando houver trade-offs relevantes.
