# Segurança e privacidade da IA

## Controles implementados

- autenticação serviço-a-serviço com comparação constante;
- JSON estrito, limites de campos, corpo e período;
- mensagens cifradas no PostgreSQL pelo backend Go;
- nenhuma mensagem, prompt ou resposta em logs do serviço de IA;
- request IDs validados e métricas de rota/status/latência sem conteúdo;
- detecção local de prompt injection, extração de prompt, exfiltração, payload codificado e
  abuso de ferramentas;
- moderação do provider e triagem semântica acionada apenas quando necessário;
- templates localizados para bloqueio, limites clínicos e crise;
- nenhuma ferramenta, shell, banco, RAG ou acesso cruzado a usuários disponível ao modelo;
- validação de saída contra diagnóstico e dependência emocional;
- evidências de relatório restritas às mensagens do usuário;
- consentimento de compartilhamento e revisão do paciente antes da leitura profissional;
- `store=false` nas chamadas OpenAI.

Idioma não determina localização. Contatos de emergência não são inventados a partir do idioma;
o país deve vir de configuração explícita do usuário. Templates atuais orientam procurar o
serviço local e uma pessoa de confiança sem substituir atendimento emergencial.

## Threat model resumido

Atacantes podem tentar alterar instruções, obter prompts, acessar dados de terceiros, provocar
saída clínica perigosa, inflar custo ou inserir afirmações falsas em relatórios. As defesas são
redução de capacidades, isolamento de dados pelo Go, limites determinísticos, classificação
condicional, outputs estruturados, rastreabilidade de fontes e revisão humana.

Nenhum classificador garante risco zero. O sistema não deve ser anunciado como terapeuta,
diagnóstico, monitoramento de emergência ou prontuário clínico. Eventos críticos são tratados
na conversa atual; não aguardam relatório periódico nem presumem que o profissional conectado
esteja disponível.

## Gates antes do go-live

- revisão jurídica/LGPD e política por país;
- aprovação do fluxo de crise por profissionais habilitados;
- secret manager, rotação de chaves e TLS/mTLS interno;
- ambiente OpenAI compatível com os requisitos de retenção da empresa;
- testes reais de modelo, red team multilíngue e thresholds de qualidade aprovados;
- alertas de latência, erro, custo, bloqueios e falha de grounding;
- runbook de indisponibilidade, incidente e revogação de consentimento.
