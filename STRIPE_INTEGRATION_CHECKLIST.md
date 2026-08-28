# Checklist de integração segura com Stripe

> Escopo inicial do Sinapsa: assinaturas SaaS mensais B2C para pacientes e B2B
> para profissionais, equipes e clínicas. O Sinapsa cobra apenas pelo acesso à
> própria plataforma e não recebe consultas nem repassa valores a profissionais.
> Portanto, o Stripe Connect não é necessário; ele só deve ser avaliado se o
> produto passar a receber valores em nome de terceiros.

Este documento considera o uso de **Stripe Billing**, **Checkout hospedado** e
**Customer Portal**. Essa combinação delega ao Stripe a coleta dos dados de
pagamento e boa parte da gestão da assinatura, mantendo no Sinapsa apenas a
integração, a autorização e as regras de acesso.

A emissão fiscal é separada do pagamento. O Stripe confirma e gerencia a
cobrança, enquanto o backend envia os dados fiscais mínimos a um provedor
brasileiro de NFS-e, inicialmente NFE.io ou Focus NFe. A fatura ou o recibo do
Stripe não substitui a NFS-e brasileira.

## 0. O que o responsável pelo negócio precisa providenciar

Estas ações não dependem da implementação e bloqueiam somente a cobrança real;
o desenvolvimento e os testes em sandbox podem começar antes delas.

- [ ] Confirmar o município onde a empresa será registrada.
- [ ] Formalizar a empresa e obter CNPJ e inscrição municipal adequados.
- [ ] Levar a descrição real do Sinapsa a um contador, Sala do Empreendedor ou
      atendimento especializado e obter por escrito: CNAE, regime, código do
      serviço, alíquota/retenção de ISS, descrição da nota e momento de emissão.
- [ ] Confirmar se o município exige certificado digital e providenciá-lo.
- [ ] Criar/regularizar a conta empresarial do Stripe em nome da empresa.
- [ ] Comparar NFE.io e Focus NFe para o município e escolher um provedor.
- [ ] Criar primeiro as contas sandbox do Stripe e do provedor fiscal.
- [ ] Entregar ao backend, via Railway e nunca por chat ou Git, as credenciais de
      sandbox e depois as credenciais live.
- [ ] Aprovar preços, limites, política de cancelamento, tolerância de pagamento,
      reembolso e tratamento de disputas antes do go-live.
- [ ] Disponibilizar termos, política de privacidade, dados de suporte e endereço
      fiscal exibidos no Checkout.
- [ ] Realizar uma compra real controlada e pedir validação do primeiro documento
      fiscal antes de vender ao público.

## Princípios não negociáveis

- [ ] O backend é a única autoridade sobre assinatura e permissões.
- [ ] O frontend nunca libera recursos com base apenas no retorno do Checkout.
- [ ] Preços, descontos, moedas e períodos são resolvidos no backend; o cliente
      envia somente um identificador interno de plano permitido.
- [ ] A chave secreta do Stripe e o segredo do webhook existem somente no
      secret manager do backend.
- [ ] Nenhum dado clínico, conversa, relatório, check-in, hipótese, diagnóstico
      ou nome de paciente é enviado ao Stripe, nem mesmo em `metadata`.
- [ ] O Stripe recebe apenas os dados mínimos de cobrança e identificadores
      internos opacos.
- [ ] Todo evento de webhook tem assinatura verificada e processamento
      idempotente.
- [ ] A aplicação continua segura quando eventos chegam duplicados, atrasados ou
      fora de ordem.
- [ ] Ambientes sandbox e produção usam chaves, produtos, preços, webhooks e
      bancos separados.
- [ ] Logs nunca contêm chaves, payloads completos, dados de cartão, URLs de
      sessão ou informações sensíveis do usuário.
- [ ] A NFS-e nunca contém conteúdo clínico, nome de paciente vinculado,
      relatório, conversa, check-in, diagnóstico ou qualquer dado de saúde.
- [ ] Pagamento e emissão fiscal são processos assíncronos e independentes: uma
      indisponibilidade da prefeitura não pode duplicar a cobrança nem derrubar
      o restante da aplicação.

## 1. Definições de produto antes do código

- [x] Permitir assinatura por paciente, profissional individual e organização.
- [x] Começar somente com cobrança mensal; não oferecer plano anual inicialmente.
- [x] Não oferecer teste grátis automático. Testes comerciais serão concedidos
      manualmente, com prazo, responsável e trilha de auditoria.
- [ ] Consolidar códigos internos estáveis e benefícios definitivos dos planos:
  - [ ] Paciente Free: até 30 mensagens por dia;
  - [ ] Paciente Plus: até 120 mensagens por dia;
  - [ ] Profissional Free: acesso e até 2 conexões, sem solicitar relatórios nem
        criar, enviar ou solicitar coleta de check-ins;
  - [ ] Profissional Plus: acesso completo e até 7 pacientes;
  - [ ] Profissional Pro: acesso completo e até 20 pacientes;
  - [ ] Profissional Consultório: acesso completo e até 45 pacientes;
  - [ ] Team: até 4 profissionais e até 180 pacientes;
  - [ ] Clinic: contratação assistida, limites definidos em contrato.
- [ ] Confirmar os preços finais antes de criar os Prices em produção.
- [ ] Definir política de upgrade, downgrade e rateio proporcional.
- [ ] Definir política de cancelamento imediato ou ao fim do período.
- [ ] Definir retentativas e curto período de tolerância para falha transitória.
- [x] Quando a assinatura for definitivamente cancelada ou ficar inadimplente
      após as retentativas, retornar ao Free e revogar os benefícios pagos.
- [ ] Definir o comportamento seguro após cancelamento ou inadimplência:
  - [ ] bloquear novas ações com custo;
  - [x] preservar o histórico criado enquanto o plano estava ativo;
  - [x] não apagar dados automaticamente por causa de um evento de cobrança;
  - [ ] separar suspensão comercial de encerramento do vínculo terapêutico.
- [ ] Definir política de reembolso, contestação e fraude.
- [ ] Validar termos de uso, política de cancelamento, impostos e emissão de
      documentos fiscais com assessoria jurídica/contábil.

## 2. Preparação e segurança da conta Stripe

- [ ] Criar a conta empresarial com os dados jurídicos corretos.
- [ ] Ativar MFA resistente a phishing para proprietários e administradores.
- [ ] Criar usuários individuais; não compartilhar uma única conta da equipe.
- [ ] Aplicar menor privilégio às funções dos membros.
- [ ] Ativar alertas de pagamentos, disputas, falhas e alterações críticas.
- [ ] Revisar descritor da cobrança, dados públicos e contatos de suporte.
- [ ] Configurar domínio, marca, termos e política de privacidade no Dashboard.
- [ ] Criar primeiro todos os produtos e preços no sandbox.
- [ ] Fixar conscientemente a versão da API Stripe usada pela integração.
- [ ] Usar uma chave restrita no backend quando as permissões necessárias forem
      compatíveis; caso contrário, proteger e limitar a chave secreta padrão.
- [ ] Restringir a chave por IP somente se o backend possuir saída estática e um
      processo confiável para manutenção da allowlist.
- [ ] Documentar e testar a rotação emergencial das chaves.
- [ ] Adicionar varredura de segredos ao CI e ao fluxo de pre-commit.

## 3. Catálogo e fonte de verdade

- [ ] Criar produtos e preços no Stripe sem reutilizar IDs entre ambientes.
- [ ] Manter um mapa server-side de `plan_code -> stripe_price_id`.
- [ ] Nunca aceitar do frontend um preço monetário, moeda, desconto ou
      `price_id` arbitrário.
- [ ] Tratar valores do catálogo Stripe como configuração versionada e
      auditável.
- [ ] Criar novos Prices para mudanças de preço; não reinterpretar IDs antigos.
- [ ] Definir quais cupons e promotion codes podem ser utilizados.
- [ ] Manter os limites e benefícios do produto no domínio do Sinapsa, não em
      textos exibidos pelo Stripe.

## 4. Modelo de dados no backend

- [ ] Criar uma tabela de contas pagantes/organizações independente do usuário.
- [ ] Garantir relação única entre a conta pagante e `stripe_customer_id`.
- [ ] Criar uma tabela de assinaturas com, no mínimo:
  - [ ] identificador interno;
  - [ ] conta pagante ou organização;
  - [ ] `provider` (`stripe`);
  - [ ] `provider_customer_id` único;
  - [ ] `provider_subscription_id` único;
  - [ ] `plan_code` interno;
  - [ ] `provider_price_id` observado;
  - [ ] `status` normalizado;
  - [ ] início e fim do período atual;
  - [ ] `cancel_at_period_end`;
  - [ ] fim do trial, quando aplicável;
  - [ ] data do último evento aplicado;
  - [ ] timestamps de criação e atualização.
- [ ] Criar uma tabela de eventos de pagamento com `stripe_event_id` único.
- [ ] Registrar apenas metadados operacionais mínimos do evento, evitando
      persistir payloads completos com PII sem necessidade.
- [ ] Criar uma tabela de perfis fiscais da conta pagante, separada dos dados
      clínicos, contendo somente nome/razão social, CPF/CNPJ, e-mail e endereço
      exigidos para emissão.
- [ ] Criar uma tabela de documentos fiscais com, no mínimo:
  - [ ] conta pagante e cobrança relacionadas;
  - [ ] `provider`, `external_id` idempotente e `stripe_invoice_id` únicos;
  - [ ] identificador no provedor, número, código de verificação e status;
  - [ ] valor, código de serviço e descrição fiscal utilizados;
  - [ ] referências privadas para XML e PDF;
  - [ ] motivo de rejeição/cancelamento e timestamps.
- [ ] Criar uma outbox ou fila fiscal durável para separar o webhook do Stripe
      da chamada ao provedor de NFS-e.
- [ ] Criar uma camada explícita de `entitlements` ou um resolvedor server-side
      de permissões por plano e status.
- [ ] Garantir constraints e transações para impedir duas assinaturas
      conflitantes para a mesma conta.
- [ ] Criar migrations reversíveis e testá-las com uma cópia sem dados reais.

## 5. Endpoints internos

- [ ] `POST /billing/checkout-session`
  - [ ] exige autenticação;
  - [ ] verifica que o usuário pode contratar pela conta/organização;
  - [ ] recebe somente `plan_code` permitido;
  - [ ] resolve o Price no backend;
  - [ ] reutiliza ou cria o Customer da conta de forma idempotente;
  - [ ] usa uma chave de idempotência por tentativa lógica;
  - [ ] usa URLs de sucesso/cancelamento obtidas de uma allowlist server-side;
  - [ ] retorna apenas a URL temporária necessária para o redirecionamento.
- [ ] `POST /billing/portal-session`
  - [ ] exige autenticação e autorização sobre a conta pagante;
  - [ ] resolve `stripe_customer_id` pelo banco, nunca pelo request;
  - [ ] cria a sessão sob demanda;
  - [ ] usa uma `return_url` permitida pelo backend.
- [ ] `GET /billing/subscription`
  - [ ] retorna o estado normalizado e os benefícios, sem objetos Stripe brutos;
  - [ ] não retorna chaves, segredos ou informações desnecessárias de cobrança.
- [ ] `POST /webhooks/stripe`
  - [ ] é público somente no sentido de não usar login comum;
  - [ ] aceita apenas `POST` e limita tamanho do corpo;
  - [ ] preserva o corpo bruto para validar a assinatura;
  - [ ] não usa CORS como mecanismo de segurança;
  - [ ] responde rapidamente e delega trabalho pesado para processamento seguro.

## 6. Criação do Checkout

- [ ] Preferir Stripe Checkout hospedado no primeiro lançamento.
- [ ] Usar `mode=subscription` para planos recorrentes.
- [ ] Associar somente um identificador interno opaco em `client_reference_id`
      ou `metadata` estritamente necessária.
- [ ] Não colocar e-mail como autoridade de vínculo; usar IDs internos.
- [ ] Validar no backend se já existe assinatura ativa antes de criar outra.
- [ ] Tornar clique duplo e retry de rede seguros com idempotência.
- [ ] Definir `success_url` apenas para UX; ela não confirma pagamento.
- [ ] Exibir na página de retorno um estado “confirmando assinatura” até o
      backend refletir o webhook.
- [ ] Nunca consultar ou confiar em parâmetros livres da URL para conceder
      acesso.

## 7. Webhook — parte crítica da integração

- [ ] Criar endpoints e segredos diferentes para sandbox e produção.
- [ ] Selecionar somente os tipos de evento realmente utilizados.
- [ ] Verificar `Stripe-Signature` com o SDK oficial e o corpo bruto.
- [ ] Rejeitar assinatura inválida sem executar nenhum efeito colateral.
- [ ] Não registrar o segredo, assinatura ou payload completo em logs.
- [ ] Inserir `event.id` sob constraint única antes de aplicar efeitos.
- [ ] Se o evento já foi processado, retornar sucesso sem repetir a alteração.
- [ ] Processar alterações de banco em transação.
- [ ] Não assumir ordem de chegada dos eventos.
- [ ] Comparar o estado recebido com o estado atual e usar timestamps/consulta à
      API quando necessário para evitar regressão por evento antigo.
- [ ] Retornar `2xx` somente após o evento ter sido aceito para processamento
      durável; falhas transitórias devem permitir retry do Stripe.
- [ ] Implementar retentativas com backoff e uma dead-letter queue ou estado
      equivalente para inspeção manual.
- [ ] Alertar quando um evento falhar repetidamente.
- [ ] Tratar, no mínimo, conforme o modelo escolhido:
  - [ ] `checkout.session.completed` para correlacionar a conclusão do fluxo;
  - [ ] `customer.subscription.created`;
  - [ ] `customer.subscription.updated`;
  - [ ] `customer.subscription.deleted`;
  - [ ] `invoice.paid`;
  - [ ] `invoice.payment_failed`;
  - [ ] eventos adicionais exigidos por trial, disputas ou reembolsos.
- [ ] Não conceder acesso vitalício a partir de
      `checkout.session.completed`; refletir o estado vigente da assinatura.
- [ ] Disponibilizar uma reconciliação periódica entre Stripe e banco para
      corrigir eventos perdidos ou falhas operacionais.

## 8. Emissão automática de NFS-e

### 8.1. Preparação fiscal antes da produção

- [ ] Formalizar a empresa antes da primeira cobrança real.
- [ ] Com orientação contábil ou atendimento público especializado, confirmar:
  - [ ] natureza jurídica, regime tributário e CNAE;
  - [ ] inscrição municipal e credenciamento para emitir NFS-e;
  - [ ] código municipal/nacional do serviço;
  - [ ] alíquota, retenção e regras de ISS;
  - [ ] momento correto da emissão: pagamento, competência ou outra regra;
  - [ ] descrição fiscal padrão do serviço;
  - [ ] regras para cancelamento, substituição, estorno e inadimplência;
  - [ ] necessidade e tipo de certificado digital.
- [ ] Não deixar o backend inferir CNAE, serviço ou imposto. Esses valores devem
      ser configurações fiscais previamente validadas.
- [ ] Escolher NFE.io ou Focus NFe e validar cobertura da cidade, preço, SLA,
      sandbox, webhooks, LGPD, exportação e requisitos de certificado.
- [ ] Considerar integração direta com a API Nacional somente se o custo de
      manter credenciamento, certificados e mudanças compensar.

### 8.2. Fluxo técnico

- [ ] Usar `invoice.paid` como gatilho inicial da emissão, sujeito à regra fiscal
      validada para a empresa.
- [ ] No webhook Stripe, persistir a cobrança e publicar uma tarefa fiscal na
      mesma transação/outbox; nunca esperar a prefeitura responder ali.
- [ ] Gerar `external_id` determinístico a partir do ambiente e da cobrança para
      impedir emissão duplicada durante retries.
- [ ] O worker fiscal deve carregar os dados do banco, validar campos, enviar a
      emissão, persistir o protocolo/status e repetir falhas transitórias com
      backoff; falhas definitivas devem gerar alerta e pendência manual.
- [ ] Usar descrição genérica, como “Licenciamento mensal da plataforma
      Sinapsa”, sem qualquer conteúdo clínico.
- [ ] Receber webhook autenticado e idempotente do provedor para atualizar os
      estados `processing`, `issued`, `failed` e `cancelled` ou equivalentes.
- [ ] Guardar XML e PDF em storage privado ou buscá-los sob demanda com URL curta
      e autenticada; nunca usar URL pública permanente.
- [ ] Após a autorização, enviar a nota por e-mail e disponibilizá-la na área de
      cobrança da própria conta pagante.
- [ ] A nota do profissional ou clínica refere-se apenas à assinatura deles.
      Pacientes vinculados não aparecem no documento.

### 8.3. Reembolsos e exceções

- [ ] Cancelar assinatura interrompe cobranças e notas futuras; não cancela
      automaticamente documentos válidos de períodos anteriores.
- [ ] Modelar reembolso total/parcial, disputa e chargeback conforme as regras
      municipais de cancelamento ou substituição.
- [ ] `invoice.payment_failed` nunca cancela uma nota: não houve pagamento novo
      confirmado nessa tentativa.
- [ ] Reembolso posterior à emissão cria uma tarefa fiscal específica e
      auditável; se o prazo municipal expirou, abrir pendência manual.
- [ ] Criar reconciliação diária para encontrar cobranças pagas sem NFS-e,
      documentos duplicados ou presos, rejeições sem alerta e reembolsos sem
      tratamento fiscal.

### 8.4. Dados fiscais no produto

- [ ] Coletar e validar antes da contratação, conforme a exigência aplicável:
  - [ ] pessoa física ou jurídica;
  - [ ] nome completo ou razão social;
  - [ ] CPF ou CNPJ;
  - [ ] e-mail fiscal;
  - [ ] endereço, CEP, município e UF;
  - [ ] inscrição municipal, quando aplicável.
- [ ] Informar a finalidade fiscal desses dados e permitir correção controlada.
- [ ] Impedir uma nova contratação quando faltarem dados indispensáveis à nota.
- [ ] Nunca preencher o perfil fiscal usando perfil clínico, paciente vinculado
      ou conteúdo terapêutico.

## 9. Estados e autorização

- [ ] Documentar explicitamente quais estados concedem acesso:

| Estado normalizado | Decisão sugerida | Observação |
| --- | --- | --- |
| `trialing` | liberar conforme o plano | somente se trial fizer parte do produto |
| `active` | liberar | assinatura vigente |
| `past_due` | aplicar tolerância definida | avisar o pagador e limitar após o prazo |
| `unpaid` | bloquear ações pagas | preservar acesso exigido a dados próprios |
| `canceled` | bloquear no fim do direito vigente | respeitar `current_period_end` |
| `incomplete` | não liberar | pagamento inicial ainda não concluído |
| `incomplete_expired` | não liberar | contratação não concluída |
| `paused` | aplicar política explícita | não tratar como `active` por padrão |

- [ ] Aplicar autorização no backend em toda operação protegida, inclusive:
  - [ ] criação de convites;
  - [ ] ativação de novos pacientes;
  - [ ] solicitação e geração de relatórios;
  - [ ] criação e coleta de check-ins;
  - [ ] uso de IA que gere custo;
  - [ ] funções administrativas da clínica.
- [ ] Não remover acesso já concedido ao paciente com base apenas em estado de
      UI ou cache do frontend.
- [ ] Invalidar ou atualizar caches de permissão quando a assinatura mudar.
- [ ] Registrar auditoria de mudanças de plano e decisões de autorização sem
      incluir conteúdo clínico.

## 10. Frontend

- [ ] Mostrar planos usando dados públicos controlados e códigos internos.
- [ ] Solicitar a sessão de Checkout ao backend e redirecionar para a URL
      retornada.
- [ ] Não embutir `STRIPE_SECRET_KEY` ou `STRIPE_WEBHOOK_SECRET`.
- [ ] Se Checkout hospedado não exigir Stripe.js no cliente, não adicionar uma
      publishable key sem necessidade.
- [ ] Se uma publishable key for necessária, expor somente `pk_*`; ela nunca
      substitui autorização no backend.
- [ ] Criar estados claros de carregamento, confirmação, falha e nova tentativa.
- [ ] Prevenir cliques repetidos enquanto a sessão está sendo criada.
- [ ] Exibir plano e benefícios vindos do backend.
- [ ] Disponibilizar “Gerenciar assinatura” por meio do Customer Portal.
- [ ] Disponibilizar dados fiscais, histórico de cobranças e download autenticado
      das NFS-e autorizadas para a própria conta pagante.
- [ ] Não armazenar objetos de cobrança em `localStorage` ou `sessionStorage`.
- [ ] Não enviar eventos de analytics contendo URL do Checkout, IDs de sessão,
      dados pessoais ou informações clínicas.

## 11. Variáveis e segredos

### Backend — secret manager da Railway

- [ ] `STRIPE_SECRET_KEY`
- [ ] `STRIPE_WEBHOOK_SECRET`
- [ ] IDs de Price de produção ou configuração equivalente protegida
- [ ] URL permitida do app profissional
- [ ] URL permitida do app do paciente, se usada em comunicações
- [ ] versão fixada da API/SDK, quando configurável
- [ ] `FISCAL_PROVIDER` (`nfeio` ou `focusnfe` em produção; `mock` somente em
      desenvolvimento e testes)
- [ ] `FISCAL_API_KEY`
- [ ] `FISCAL_WEBHOOK_SECRET`, quando suportado
- [ ] identificador da empresa no provedor fiscal
- [ ] código do serviço e configurações tributárias previamente validadas
- [ ] referência segura do certificado e senha, somente se exigidos

### Frontend

- [ ] Nenhuma chave secreta.
- [ ] `NEXT_PUBLIC_STRIPE_PUBLISHABLE_KEY` somente se uma funcionalidade
      client-side do Stripe realmente exigir.
- [ ] URLs públicas continuam sendo configuração de build, sem tokens.

### Higiene de segredos

- [ ] Não copiar chaves para tickets, chat, documentação ou screenshots.
- [ ] Não colocar valores reais em `.env.example`.
- [ ] Garantir que `.env*` reais estejam ignorados pelo Git.
- [ ] Procurar por `sk_live_`, `rk_live_` e `whsec_` antes de cada release.
- [ ] Se uma chave aparecer em commit ou log, tratá-la como comprometida e
      rotacioná-la imediatamente.

## 12. Testes automatizados e sandbox

- [ ] Criar testes unitários para o mapeamento plano/preço e status/permissões.
- [ ] Criar testes de autorização para usuário, profissional e organização.
- [ ] Criar testes do webhook com assinatura válida e inválida.
- [ ] Testar evento duplicado: o efeito deve acontecer uma única vez.
- [ ] Testar eventos fora de ordem e eventos antigos.
- [ ] Testar falha de banco durante o webhook e posterior retry.
- [ ] Testar clique duplo na criação do Checkout.
- [ ] Testar timeout após o Stripe criar uma sessão, antes de o backend responder.
- [ ] Testar login de outro usuário tentando abrir portal ou assinatura alheia.
- [ ] Testar Price inexistente, inativo ou não permitido.
- [ ] Testar os seguintes fluxos no sandbox:
  - [ ] primeira assinatura aprovada;
  - [ ] pagamento recusado;
  - [ ] autenticação adicional do pagamento;
  - [ ] renovação bem-sucedida;
  - [ ] renovação com falha e recuperação posterior;
  - [ ] cancelamento imediato;
  - [ ] cancelamento ao fim do período;
  - [ ] reativação antes do fim do período;
  - [ ] upgrade e downgrade;
  - [ ] trial iniciado, encerrado e convertido;
  - [ ] assinatura incompleta expirada;
  - [ ] reembolso e disputa, se aplicáveis.
- [ ] Usar Stripe Test Clocks/Simulations para renovação, trial e inadimplência.
- [ ] Testar com os dois frontends e com contas individuais e de clínica.
- [ ] Testar o provedor fiscal em sandbox, sem emissão real.
- [ ] Testar idempotência: uma cobrança gera no máximo uma NFS-e válida.
- [ ] Testar prefeitura indisponível, timeout, rejeição e retries.
- [ ] Testar webhook fiscal válido, inválido, duplicado e fora de ordem.
- [ ] Testar pagamento sem dados fiscais obrigatórios.
- [ ] Testar reembolso antes e depois da autorização da NFS-e.
- [ ] Testar cancelamento fora do prazo e abertura de pendência manual.
- [ ] Testar reconciliação de cobrança paga sem documento fiscal.
- [ ] Garantir que fixtures fiscais nunca contenham dados clínicos.
- [ ] Executar testes de segurança e revisão de dependências antes do go-live.

## 13. Observabilidade e operação

- [ ] Criar métricas para eventos recebidos, processados, duplicados e falhos.
- [ ] Correlacionar logs com IDs internos e `event.id`, sem payload sensível.
- [ ] Alertar sobre crescimento da fila, falhas definitivas e divergência de
      assinatura.
- [ ] Criar painel para assinaturas por status e falhas de renovação.
- [ ] Criar métricas para NFS-e em processamento, autorizadas, rejeitadas,
      canceladas e pendentes de ação manual.
- [ ] Criar job periódico de reconciliação com rate limit e paginação seguros.
- [ ] Criar runbooks para:
  - [ ] chave exposta;
  - [ ] segredo de webhook rotacionado;
  - [ ] webhook indisponível;
  - [ ] eventos presos na fila;
  - [ ] usuário cobrado sem acesso;
  - [ ] acesso liberado sem cobrança vigente;
  - [ ] estorno ou disputa;
  - [ ] indisponibilidade do Stripe.
  - [ ] provedor fiscal ou prefeitura indisponível;
  - [ ] cobrança paga sem NFS-e;
  - [ ] NFS-e rejeitada ou duplicada;
  - [ ] certificado digital próximo do vencimento;
  - [ ] reembolso pendente de tratamento fiscal.
- [ ] Definir quem pode efetuar reembolso e exigir trilha de auditoria.
- [ ] Revisar periodicamente acessos da equipe e logs da API Stripe.

## 14. Privacidade, LGPD e retenção

- [ ] Documentar Stripe como operador/suboperador conforme a estrutura jurídica.
- [ ] Atualizar política de privacidade com finalidade e base legal da cobrança.
- [ ] Coletar somente os dados fiscais e de cobrança necessários.
- [ ] Nunca correlacionar cobrança a condição, diagnóstico ou conteúdo clínico.
- [ ] Definir retenção para IDs, faturas, eventos e logs conforme obrigações
      legais e necessidade operacional.
- [ ] Separar exclusão da conta, retenção fiscal e exclusão de conteúdo clínico.
- [ ] Definir fluxo de atendimento a solicitações do titular.
- [ ] Avaliar transferência internacional, DPA e fornecedores com assessoria
      especializada antes da produção.
- [ ] Documentar o provedor fiscal como operador/suboperador e enviar somente os
      dados estritamente exigidos para a NFS-e.
- [ ] Separar permissões de documentos fiscais e conteúdo clínico.

## 15. Go-live controlado

- [ ] Concluir todos os testes no sandbox.
- [ ] Criar novamente produtos e Prices no modo live e conferir valores.
- [ ] Cadastrar chaves live somente no secret manager de produção.
- [ ] Cadastrar o webhook live na URL HTTPS definitiva.
- [ ] Validar assinatura do webhook live com evento de teste apropriado.
- [ ] Confirmar que sandbox não aponta para banco ou URLs de produção.
- [ ] Confirmar que produção não usa `sk_test_*`, `pk_test_*` ou dados mockados.
- [ ] Confirmar que produção não usa provedor fiscal mock ou credenciais sandbox.
- [ ] Confirmar inscrição municipal, credenciamento, configuração tributária e
      certificado válidos.
- [ ] Confirmar domínio, branding, suporte, termos e política de cancelamento.
- [ ] Fazer uma compra real de baixo risco com conta controlada.
- [ ] Confirmar no banco, UI, e-mail, Stripe e provedor fiscal todo o ciclo da
      assinatura e da NFS-e.
- [ ] Cancelar/reembolsar a compra controlada e validar a revogação correta.
- [ ] Validar o documento fiscal da compra controlada antes de abrir vendas.
- [ ] Liberar primeiro para um grupo pequeno de profissionais.
- [ ] Monitorar webhooks, erros, conversão e suporte durante o rollout.
- [ ] Manter rollback que desative novas contratações sem corromper assinaturas
      já existentes.

## 16. Critérios de conclusão

- [ ] Nenhuma chave secreta ou dado sensível está presente nos bundles.
- [ ] Nenhum recurso pago é liberado pelo frontend ou pela `success_url`.
- [ ] Todos os webhooks são autenticados, idempotentes e auditáveis.
- [ ] Eventos duplicados e fora de ordem foram testados.
- [ ] O backend protege todas as operações que consomem o plano.
- [ ] Falhas de pagamento e cancelamentos têm comportamento documentado.
- [ ] O usuário pode gerenciar pagamento e assinatura pelo Customer Portal.
- [ ] Existe reconciliação, observabilidade, alerta e runbook.
- [ ] Toda cobrança elegível gera no máximo uma NFS-e e pode ser reconciliada.
- [ ] XML e PDF são privados e acessíveis somente à conta autorizada.
- [ ] Reembolsos e exceções fiscais possuem fluxo auditável e pendência manual
      quando não puderem ser resolvidos automaticamente.
- [ ] O checkout real foi validado ponta a ponta em rollout controlado.
- [ ] Aspectos jurídicos, fiscais e de LGPD foram revisados por profissionais
      responsáveis.

## Referências oficiais

- [Stripe Checkout](https://docs.stripe.com/payments/checkout)
- [Assinaturas com Checkout](https://docs.stripe.com/payments/checkout/build-subscriptions)
- [Customer Portal](https://docs.stripe.com/customer-management)
- [Webhooks](https://docs.stripe.com/webhooks)
- [Chaves da API](https://docs.stripe.com/keys)
- [Boas práticas para chaves secretas](https://docs.stripe.com/keys-best-practices)
- [Requisições idempotentes](https://docs.stripe.com/api/idempotent_requests)
- [Testes de Billing e Test Clocks](https://docs.stripe.com/billing/testing)
- [NFE.io — primeiros passos com NFS-e](https://nfe.io/docs/documentacao/nota-fiscal-servico-eletronica/primeiros-passos/)
- [NFE.io — API REST](https://nfe.io/docs/rest-api/)
- [Focus NFe — NFS-e Nacional](https://doc.focusnfe.com.br/reference/nfse-nacional)
- [Sistema Nacional NFS-e — manual da API](https://www.gov.br/nfse/pt-br/biblioteca/documentacao-tecnica/documentacao-atual/manual-contribuintes-emissor-publico-api-sistema-nacional-nfs-e.pdf)

> Este checklist reduz riscos técnicos, mas não substitui revisão jurídica,
> fiscal, contábil, de privacidade ou de segurança independente antes do uso com
> cobranças reais.
