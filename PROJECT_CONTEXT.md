# PROJECT_CONTEXT.md

## Visão do projeto

Este projeto é uma plataforma SaaS B2B voltada inicialmente para psicólogos, terapeutas, psicanalistas e clínicas.

O objetivo principal é criar uma camada de acompanhamento contínuo entre o profissional e seus pacientes, permitindo que o profissional tenha mais contexto sobre acontecimentos, comportamentos relatados, mudanças de rotina, dificuldades, conquistas e temas importantes que surgem entre uma sessão e outra.

O produto não será apenas um chatbot de anamnese nem um formulário conversacional.

A proposta central é acompanhar o paciente ao longo do tempo.

O paciente terá acesso a uma IA conversacional acolhedora, natural e agradável de utilizar, com a qual poderá conversar espontaneamente sobre seu cotidiano.

Essas conversas formarão uma memória longitudinal que será processada e organizada para gerar contexto útil ao profissional.

A interpretação clínica continuará sendo responsabilidade do profissional.

---

# Produto 1 — SaaS B2B de acompanhamento contínuo

O primeiro produto a ser desenvolvido será focado exclusivamente no mercado B2B.

O cliente pagante será:

* psicólogo;
* terapeuta;
* psicanalista;
* profissional de saúde mental compatível com a proposta da plataforma;
* consultório;
* clínica;
* organização com múltiplos profissionais.

O profissional terá acesso a um painel onde poderá cadastrar e acompanhar seus pacientes.

Ao iniciar o acompanhamento de um novo paciente, o profissional poderá gerar um convite.

Fluxo conceitual:

```text
Profissional
    ↓
cria paciente
    ↓
gera convite
    ↓
envia link
    ↓
Paciente
    ↓
entra na plataforma
    ↓
aceita os termos e consentimentos
    ↓
passa a utilizar a IA conversacional
    ↓
conversas acontecem durante dias/semanas/meses
    ↓
sistema organiza informações relevantes
    ↓
memória longitudinal é construída
    ↓
profissional recebe contexto antes e entre sessões
```

O paciente não precisa pagar pela utilização associada ao acompanhamento de um profissional.

O custo faz parte da assinatura B2B.

---

# Problema principal

Entre sessões, muitos acontecimentos relevantes ocorrem na vida do paciente.

Na sessão seguinte, o profissional depende principalmente daquilo que o paciente:

* lembra;
* considera relevante;
* consegue organizar;
* consegue verbalizar;
* decide espontaneamente mencionar.

Isso pode fazer com que acontecimentos, mudanças e padrões importantes sejam esquecidos ou percam contexto.

A plataforma busca diminuir essa perda de informação através de acompanhamento contínuo e voluntário.

O objetivo não é substituir a conversa entre paciente e profissional.

O objetivo é melhorar o contexto disponível para essa conversa.

---

# Experiência do paciente

A principal experiência do paciente será uma interface conversacional.

Ela deve ser:

* simples;
* mobile-first;
* acolhedora;
* privada;
* natural;
* não julgadora;
* confortável para uso recorrente.

O usuário deve sentir que possui um espaço onde pode conversar sobre acontecimentos da sua vida.

A IA pode conversar sobre temas como:

* rotina;
* trabalho;
* estudos;
* relacionamentos;
* família;
* amizades;
* conflitos;
* acontecimentos importantes;
* conquistas;
* frustrações;
* decisões;
* mudanças;
* sono;
* hábitos;
* preocupações;
* sentimentos relatados;
* situações que o usuário deseja lembrar posteriormente.

A experiência não deve parecer um questionário.

Perguntas podem ser feitas quando fizerem sentido dentro da conversa, mas devem surgir de forma contextual.

O objetivo é incentivar conversas espontâneas e recorrentes.

---

# Papel da IA

A IA possui dois papéis diferentes dentro do sistema.

## 1. Companion conversacional

Responsável pela experiência direta com o paciente.

Deve:

* conversar naturalmente;
* demonstrar acolhimento;
* acompanhar o contexto das conversas anteriores;
* fazer perguntas relevantes quando apropriado;
* permitir que o usuário desenvolva pensamentos;
* lembrar de contexto útil;
* ajudar o usuário a registrar acontecimentos.

O companion deve ser agradável o suficiente para que o usuário queira conversar novamente.

---

## 2. Sistema de organização de contexto

Separadamente da experiência conversacional, o sistema deve processar as conversas para organizar informações relevantes.

Esse processamento poderá identificar:

* acontecimentos;
* temas;
* recorrências;
* alterações relatadas;
* relações entre acontecimentos;
* assuntos importantes;
* assuntos marcados para próxima sessão;
* contexto relacionado a períodos específicos.

Essa camada não deve necessariamente utilizar o mesmo modelo ou fluxo da IA responsável pela conversa.

A qualidade da conversa e a qualidade da extração estruturada são problemas diferentes.

---

# Princípio de linguagem

A plataforma trabalha com relatos.

Ela não deve transformar inferências da IA em fatos clínicos.

Exemplo inadequado:

```text
O paciente apresenta ansiedade relacionada ao trabalho.
```

Exemplo adequado:

```text
O paciente relatou preocupação recorrente relacionada ao trabalho e mencionou dificuldade para deixar de pensar nessas situações após o expediente.
```

Outro exemplo inadequado:

```text
O paciente possui problemas de autoestima.
```

Exemplo adequado:

```text
Durante as últimas semanas, o paciente relatou em diferentes momentos insegurança relacionada à própria aparência e desempenho profissional.
```

O sistema descreve.

O profissional interpreta.

---

# Limites do produto

A IA não deve:

* substituir psicoterapia;
* substituir acompanhamento profissional;
* diagnosticar transtornos;
* realizar avaliação psicológica;
* prescrever medicamentos;
* prescrever tratamentos;
* definir condutas clínicas;
* afirmar hipóteses psicológicas como fatos;
* assumir o papel do profissional responsável;
* criar dependência emocional no usuário.

Esses limites fazem parte da definição central do produto.

---

# Memória longitudinal

Um dos principais diferenciais da plataforma será sua capacidade de construir contexto ao longo do tempo.

Uma conversa isolada possui pouco valor.

O valor maior aparece quando o sistema consegue relacionar informações de:

```text
hoje
+
ontem
+
última semana
+
último mês
+
meses anteriores
```

O sistema deve conseguir distinguir diferentes níveis de memória.

Exemplo conceitual:

```text
Mensagens recentes
        ↓
Eventos relevantes
        ↓
Resumos periódicos
        ↓
Contexto longitudinal
```

Nem toda mensagem precisa virar uma memória permanente.

O sistema deverá identificar quais informações possuem valor suficiente para serem utilizadas futuramente.

---

# Eventos

As conversas poderão gerar eventos estruturados.

Exemplo de mensagem:

```text
Hoje briguei de novo com meu chefe.

Estou ficando muito cansado desse trabalho.

Mesmo quando chego em casa continuo pensando nisso.
```

Possível representação interna:

```json
{
  "category": "work",
  "event_type": "conflict",
  "reported_emotions": [
    "stress",
    "frustration"
  ],
  "reported_impact": [
    "difficulty_disconnecting_from_work"
  ],
  "importance": 0.82,
  "confidence": 0.9
}
```

Essa estrutura é interna.

Ela não representa diagnóstico.

Ela ajuda o sistema a construir timeline, contexto e resumos.

---

# Timeline do paciente

O sistema deverá ser capaz de construir uma visão cronológica de acontecimentos relevantes.

Exemplo:

```text
14 AGO

Discussão relatada com gestor no trabalho.

---

12 AGO

Paciente relatou dificuldade para dormir.

---

09 AGO

Paciente descreveu encontro com amigos como um acontecimento positivo.

---

05 AGO

Conflito familiar relatado.

---

01 AGO

Início de novo projeto profissional.
```

O profissional poderá navegar por essa linha do tempo sem precisar ler todas as conversas do paciente.

---

# Resumo desde a última sessão

Uma das features centrais do Produto 1 será a geração de contexto referente apenas ao período entre sessões.

Exemplo:

```text
RESUMO DESDE A ÚLTIMA SESSÃO

Principais acontecimentos

• Relatou dois conflitos relacionados ao trabalho.
• Comentou dificuldade para dormir em diferentes dias.
• Relatou melhora na relação com a irmã.
• Começou a considerar uma mudança profissional.

Temas recorrentes

• trabalho
• sono
• família

Assuntos que o paciente deseja levar para a próxima sessão

• discussão ocorrida na terça-feira;
• possibilidade de mudar de emprego.
```

Esse resumo deve funcionar como preparação para o profissional.

Não deve tentar substituir a análise realizada durante a sessão.

---

# Marcação de assuntos importantes

Durante uma conversa, o paciente poderá sinalizar:

```text
Quero conversar sobre isso na próxima sessão.
```

Esses itens devem aparecer de maneira destacada para o profissional.

Isso cria uma conexão direta entre:

```text
experiência durante a semana
↓
registro espontâneo
↓
próxima sessão
```

---

# Painel do profissional

O profissional deverá possuir uma interface própria.

Principais áreas previstas:

```text
Dashboard

Pacientes

Paciente individual

Resumo desde última sessão

Timeline

Eventos

Temas recorrentes

Itens para próxima sessão

Histórico de resumos

Configurações
```

O dashboard deve priorizar informação útil.

O profissional não deve precisar analisar dezenas de gráficos apenas porque os dados existem.

---

# Personalização do acompanhamento

O profissional poderá definir focos de acompanhamento para determinado paciente.

Exemplo:

```text
Focos atuais

- rotina;
- sono;
- trabalho;
- relações familiares.
```

Isso não significa que a IA deverá perguntar constantemente sobre esses temas.

Os focos devem apenas influenciar a atenção do sistema durante o processamento das conversas.

O usuário continuará podendo conversar livremente.

---

# Modelo de negócio

O Produto 1 será um SaaS B2B baseado em assinatura.

O profissional ou clínica paga pela plataforma.

A cobrança poderá variar principalmente por:

* quantidade de pacientes ativos;
* quantidade de profissionais;
* volume de utilização;
* funcionalidades disponíveis;
* recursos administrativos para clínicas.

Faixa inicial hipotética:

```text
Solo
R$ 249/mês

Pro
R$ 349/mês

Pro+
R$ 499/mês

Clínica
R$ 1.490+/mês
```

Esses valores são apenas hipóteses comerciais.

O objetivo inicial não é encontrar o pricing perfeito.

O objetivo inicial é verificar se existe disposição real de pagamento.

---

# Hipótese central de negócio

A principal hipótese a ser validada é:

```text
Profissionais de saúde mental consideram valioso ter
contexto organizado sobre acontecimentos e relatos de
seus pacientes entre as sessões.
```

A plataforma só possui valor comercial se o profissional:

1. convidar pacientes;
2. tiver pacientes utilizando o sistema;
3. consultar os resumos;
4. utilizar o contexto durante seus atendimentos;
5. continuar utilizando a plataforma ao longo do tempo;
6. considerar o sistema importante o suficiente para pagar por ele.

---

# Métricas iniciais importantes

Algumas métricas relevantes:

```text
professional_activation_rate

patient_invitation_rate

patient_activation_rate

weekly_active_patients

messages_per_active_patient

summary_generation_rate

summary_open_rate

timeline_usage

professional_retention

patient_retention

professional_churn
```

No início, retenção será mais importante que crescimento.

---

# Público inicial para validação

A primeira validação poderá acontecer com poucos profissionais.

O objetivo inicial deve ser algo próximo de:

```text
1–5 profissionais
+
pacientes reais
+
algumas semanas de utilização
```

Depois:

```text
20–50 profissionais pagantes
```

Antes de buscar crescimento agressivo.

---

# Privacidade e dados sensíveis

O produto trabalha com informações extremamente sensíveis.

Privacidade não pode ser tratada como feature futura.

Desde o início, decisões do sistema devem considerar:

* consentimento;
* confidencialidade;
* isolamento de dados;
* controle de acesso;
* rastreabilidade;
* minimização de dados;
* exclusão;
* retenção;
* exportação;
* segurança.

O paciente precisa saber claramente:

* que está conversando com uma IA;
* que aquela IA não substitui um profissional;
* que as informações podem ser processadas;
* quais informações podem ser acessadas pelo profissional;
* quais dados são armazenados;
* como retirar consentimento quando aplicável.

---

# Relação profissional-paciente

A plataforma deve representar explicitamente a relação entre profissional e paciente.

Um paciente não deve simplesmente pertencer permanentemente a uma clínica.

O sistema deve ser capaz de representar relações como:

```text
patient
    ↓
professional_patient_relationship
    ↓
professional
```

Isso permitirá futuramente:

* troca de profissional;
* múltiplos profissionais autorizados;
* encerramento de acompanhamento;
* diferentes níveis de compartilhamento;
* Produto 2.

---

# Produto 2 — visão futura

O Produto 2 não faz parte do MVP inicial.

Ele representa uma possível evolução estratégica da plataforma.

No Produto 2, qualquer pessoa poderá criar uma conta independentemente de já possuir um profissional.

A pessoa poderá utilizar o companion como um aplicativo B2C.

Possível modelo:

```text
Free
+
Premium
```

O usuário construirá seu próprio histórico longitudinal.

Quando decidir iniciar acompanhamento profissional, poderá acessar um marketplace de profissionais presentes na plataforma.

Fluxo futuro:

```text
Usuário
    ↓
utiliza companion
    ↓
constrói histórico
    ↓
decide procurar profissional
    ↓
recebe recomendações
    ↓
escolhe profissional
    ↓
autoriza compartilhamento de contexto
    ↓
inicia acompanhamento
```

O profissional poderá receber um resumo preparado a partir do histórico autorizado pelo usuário.

Isso pode reduzir a barreira inicial de:

```text
"Não sei por onde começar."
```

na primeira sessão.

---

# Monetização futura do Produto 2

Possíveis fontes de receita:

## Usuários

```text
Free

Premium
~R$ 20–30/mês
```

Premium poderá oferecer:

* maior limite de conversa;
* memória longitudinal maior;
* recursos avançados;
* resumos;
* histórico;
* personalizações.

---

## Profissionais

Profissionais poderão pagar para fazer parte da rede.

O pagamento não deve comprar posição artificial no ranking.

A recomendação deve priorizar compatibilidade real.

O profissional paga pelo acesso ao ecossistema e às ferramentas.

---

# Relação entre Produto 1 e Produto 2

A estratégia prevista é começar pelo Produto 1.

```text
PRODUTO 1

SaaS B2B
↓
profissionais entram
↓
profissionais convidam pacientes
↓
pacientes criam contas
↓
base de usuários cresce
↓
produto gera receita recorrente
```

Posteriormente:

```text
parte desses usuários
↓
passa a poder utilizar a plataforma independentemente
↓
Produto 2
↓
B2C + marketplace
```

Isso permite que o Produto 1 seja simultaneamente:

```text
motor de receita
+
motor de aquisição
```

Enquanto o Produto 2 futuramente pode se tornar:

```text
motor de distribuição
+
marketplace
+
efeito de rede
```

Os produtos não devem ser tratados como negócios completamente independentes.

A visão de longo prazo é que ambos utilizem o mesmo ecossistema.

---

# Escopo inicial

O primeiro MVP deve validar apenas o loop essencial:

```text
Profissional cria conta
        ↓
cria paciente
        ↓
gera convite
        ↓
paciente entra
        ↓
paciente conversa
        ↓
mensagens são armazenadas
        ↓
informações relevantes são organizadas
        ↓
timeline é construída
        ↓
resumo é gerado
        ↓
profissional consulta o contexto
```

Se esse ciclo gerar valor real, o produto pode crescer.

---

# Fora do MVP

Não priorizar inicialmente:

```text
Produto 2

marketplace

matching de profissionais

app mobile nativo

gestão financeira de clínica

videoconferência

agenda complexa

prontuário eletrônico completo

integrações com convênios

sistema completo de gestão clínica

gamificação

features sociais
```

Esses recursos só devem aparecer quando o core estiver validado.

---

# Contexto técnico

O backend principal será desenvolvido em Go.

Um dos objetivos pessoais deste projeto é utilizar um produto real e relativamente grande para aprofundar conhecimento em Golang e engenharia de backend.

O desenvolvimento deve permitir aprendizado real de:

```text
Go

HTTP

APIs

PostgreSQL

SQL

context.Context

concorrência

goroutines

streaming

processamento assíncrono

workers

filas

autenticação

autorização

multi-tenancy

RBAC

observabilidade

testes

segurança

arquitetura de software
```

A parte de IA também será parte central do projeto.

Áreas relevantes:

```text
LLMs

agentes

structured outputs

memory

event extraction

summarization

retrieval

RAG

embeddings

safety

evaluation

observability

LLM cost optimization
```

---

# Uso de agentes de coding

Codex e outros agentes de desenvolvimento poderão ser utilizados como aceleradores.

Eles não são responsáveis pelas decisões arquiteturais do projeto.

As principais decisões devem continuar sendo tomadas pelo desenvolvedor.

O objetivo não é gerar automaticamente todo o backend.

Principalmente no backend Go, partes importantes deverão ser implementadas ou profundamente revisadas manualmente para maximizar aprendizado.

Agentes podem ajudar com:

* revisão;
* debugging;
* testes;
* boilerplate;
* pesquisa dentro do codebase;
* refactors;
* geração de código repetitivo;
* identificação de bugs;
* documentação;
* comparação de alternativas.

Eles não devem modificar decisões fundamentais sem instrução.

---

# Princípios de engenharia

## Simplicidade antes de escala artificial

Não criar infraestrutura distribuída apenas para parecer sofisticado.

Adicionar componentes quando existir uma necessidade real.

---

## Vertical slices

Sempre que possível, desenvolver funcionalidades completas de ponta a ponta.

Exemplo:

```text
criar paciente

frontend
↓
API
↓
service
↓
repository
↓
database
↓
teste
```

Depois avançar para a próxima funcionalidade.

---

## Segurança não é opcional

Não comprometer:

```text
segurança

privacidade

isolamento de tenant

controle de acesso

integridade dos dados
```

em nome de velocidade.

---

## IA não é fonte da verdade

Outputs de modelos podem estar errados.

Informações estruturadas geradas por IA devem possuir:

```text
source

confidence

traceability
```

quando fizer sentido.

---

## Dados originais devem permanecer rastreáveis

Sempre que um resumo ou evento for gerado, deve ser possível identificar de quais dados ele surgiu.

Exemplo:

```text
summary
    ↓
summary_sources
    ↓
events/messages
```

Isso permite auditoria e correção.

---

## Produto antes de arquitetura perfeita

O sistema deve ser bem projetado, mas o objetivo é construir um produto utilizado por pessoas reais.

Evitar arquiteturas enormes antes da validação.

---

# Objetivo imediato

Construir a primeira versão funcional do Produto 1.

A primeira grande pergunta que o projeto precisa responder é:

> O contexto produzido por conversas contínuas com uma IA é útil o suficiente para que um profissional de saúde mental queira incorporá-lo ao seu acompanhamento e pagar regularmente pelo sistema?

Todas as primeiras decisões de produto devem ajudar a responder essa pergunta.
