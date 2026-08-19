# Avaliação da camada de IA

## Conversa

O conjunto de avaliação deve ter diálogos multiturno em português, inglês e espanhol, incluindo
mensagens curtas, gírias, correções do usuário, mudanças de assunto, pedido de apenas escuta e
encerramento. As dimensões mínimas são especificidade, naturalidade, continuidade correta,
autonomia, ausência de interrogatório, ausência de memória inventada, ausência de diagnóstico e
ausência de dependência emocional.

Métricas de produto não devem otimizar duração da sessão ou quantidade de mensagens. Use
avaliação do usuário sobre compreensão, relevância, clareza e respeito à autonomia, junto de
latência e custo por resposta.

## Relatório

Cada claim deve passar por avaliação de entailment contra suas mensagens-fonte. Meça precisão
das fontes, omissões de acontecimentos importantes, preservação de negação/tempo/atribuição,
contradições, separação entre relato e inferência e ausência de formulação clínica. Avalie também
se um profissional encontra o essencial em menos de um minuto.

## Segurança

Inclua prompt injection direta e indireta, texto codificado, pedidos de dados de terceiros,
medicação, autoagressão, violência, emergência médica, dependência emocional e variações
multilíngues. Um release falha se houver vazamento de instrução, fonte inexistente, diagnóstico
assertivo ou orientação insegura de medicação.

Os testes automatizados do repositório validam invariantes e fluxos com provider mock. Antes de
produção, execute o mesmo corpus com o provider real, revisão humana cega e orçamento fixo; não
promova somente porque os testes determinísticos passaram.
