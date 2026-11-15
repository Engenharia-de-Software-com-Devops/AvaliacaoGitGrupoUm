# Ciclo de issues e integração da equipe

Este guia organiza o trabalho de **Marcos Vinicius, Mateus e Jaime** em quatro ciclos. A fonte de verdade dos requisitos e critérios de aceite é a [avaliação](Avaliacao_Git_GitHub.md); consulte também as regras do projeto em [AGENTS.md](../AGENTS.md). O plano abaixo não cria issues, PRs, milestones, tags ou releases automaticamente.

O domínio escolhido é uma **ToDo List**: os registros da avaliação representam tarefas. A Web e a CLI permitirão gerenciá-las por meio da mesma API REST e do mesmo banco PostgreSQL, com back-end em Go. Campos, validações, filtros e indicadores das tarefas ainda devem ser definidos pela equipe; as funcionalidades serão implementadas progressivamente nas issues abaixo.

## 1. Participantes e calendário de integração

| Integrante | Nome | Ciclo 1 | Ciclo 2 | Ciclo 3 | Ciclo 4 |
|---|---|---|---|---|---|
| A | Marcos Vinicius | #1 — Estrutura Web | #4 — Busca Web | #7 — Estrutura CLI | #10 — Busca CLI |
| B | Mateus | #2 — Cadastro/edição Web | #5 — CSV Web | #8 — Cadastro/edição CLI | #11 — CSV CLI |
| C | Jaime | #3 — Listagem/exclusão Web | #6 — Dashboard Web | #9 — Listagem/exclusão CLI | #12 — Indicadores CLI |

**Ordem sugerida de integração:**

1. Ciclo 1: #1 → #2 → #3.
2. Ciclo 2: #4 → #5 → #6.
3. Ciclo 3: #7 → #8 → #9.
4. Ciclo 4: #10 → #11 → #12.

A ordem facilita o atendimento das dependências: a estrutura Web precede as demais funções Web; o cadastro precede listagem e filtros; a estrutura CLI precede as demais funções CLI; busca, CSV e indicadores CLI podem ser comparados às funções Web correspondentes. A equipe pode mudar a ordem dentro de um ciclo, desde que preserve as dependências e as demais regras.

**Barreira obrigatória:** o ciclo seguinte só começa depois que os três integrarem **uma PR principal cada** no ciclo atual. Não basta abrir, aprovar ou concluir localmente a PR. Conta a autoria da PR integrada, não quem clicou no botão de merge. Correções adicionais não contam como PR principal nem antecipam outro ciclo.

## 2. Preparação antes do primeiro ciclo

1. Confirmar que o repositório principal será público e que os três têm acesso para criar issues, revisar PRs e executar as tarefas de integração. O prazo da avaliação é de **9 dias corridos**.
2. Para as tarefas da ToDo List, definir **4–6 campos**, dos quais pelo menos **4 obrigatórios**, além de pelo menos **3 filtros combináveis** e dados para **3 gráficos**. Registrar os campos, validações e indicadores acordados antes de implementar. A stack escolhida é **Go e PostgreSQL**.
3. Cada integrante cria seu próprio fork e prepara seu ambiente Git com acesso ao fork e ao repositório principal. Cada issue deve ser desenvolvida em uma branch e um **git worktree**; só usar branches não satisfaz a avaliação.
4. Criar no GitHub as labels propostas `web` e `cli` e as milestones `Ciclo 1`, `Ciclo 2`, `Ciclo 3` e `Ciclo 4`, ou combinar nomes equivalentes e aplicá-los de forma consistente.
5. Usar a spec da próxima seção para criar as issues no repositório principal. **Cada responsável cria, assume e entrega suas próprias quatro issues.** Coordenar a criação por ordem numérica: os números do GitHub são automáticos e também podem ser consumidos por PRs. Se os números reais diferirem dos da avaliação, registrar o mapeamento e usar os números reais nos vínculos `Closes`, `Fixes` e `Refs`.
6. Confirmar que todas as issues têm descrição, critérios de aceite, responsável GitHub, labels e milestone. A atribuição abaixo é planejamento; conferir os campos efetivamente cadastrados no GitHub.
7. Definir como a equipe vai acompanhar a ordem dos merges, as versões e as revisões. Após a criação do repositório, inclusive para documentação e infraestrutura, não fazer commits diretamente no `main`: usar PRs.

## 3. Spec das 12 issues

As labels e milestones abaixo são propostas para cadastro. Todas as issues também seguem os requisitos comuns de arquivos, endpoints, autenticação, logs, testes, cobertura, Docker Compose e README da avaliação. Na criação, copiar cada descrição e aceite, atribuir o responsável indicado e conferir o texto integral na avaliação.

| Issue | Responsável | Label | Milestone | Descrição e critérios de aceite específicos |
|---|---|---|---|---|
| #1 — Estrutura Web | Marcos Vinicius | `web` | Ciclo 1 | Criar página inicial e login, API e banco; `GET /api/web/health` retorna 200; implementar `POST /api/web/login` e usuário inicial configurável. `docker compose up` funciona e o README explica início e login. |
| #2 — Cadastro/edição Web | Mateus | `web` | Ciclo 1 | Formulário com 4–6 campos, ao menos 4 obrigatórios; validar e exibir erros; criar e editar via API e banco, persistindo os dados. |
| #3 — Listagem/exclusão Web | Jaime | `web` | Ciclo 1 | Carregar dados do banco ao abrir a página; tabela com 4 campos visíveis, 5 itens por página, ordenação, seleção e exclusão. |
| #4 — Busca avançada Web | Marcos Vinicius | `web` | Ciclo 2 | Oferecer pelo menos 3 filtros combináveis, consulta assíncrona, atualização da tabela e limpeza dos filtros; combinações devem retornar os resultados corretos. |
| #5 — Exportação CSV Web | Mateus | `web` | Ciclo 2 | Criar botão, endpoint e consulta próprios; exportar todos os resultados filtrados, não apenas a página atual; sem filtros, exportar todos. |
| #6 — Dashboard Web | Jaime | `web` | Ciclo 2 | Mostrar 3 gráficos com dados reais do banco via API; atualizar valores ao recarregar e explicar os indicadores no README. |
| #7 — Estrutura CLI | Marcos Vinicius | `cli` | Ciclo 3 | Criar CLI em arquivo único com ajuda/subcomandos, `GET /api/cli/health`, `POST /api/cli/token` e Bearer token. Funcionar via Compose e retornar 401 em acesso protegido sem token válido. Documentar uso no README. |
| #8 — Cadastro/edição CLI | Mateus | `cli` | Ciclo 3 | Implementar comandos com os mesmos campos e validações da Web; persistir registros e torná-los visíveis na Web. |
| #9 — Listagem/exclusão CLI | Jaime | `cli` | Ciclo 3 | Implementar paginação, ordenação e exclusão por identificador; alterações devem aparecer na Web. |
| #10 — Busca avançada CLI | Marcos Vinicius | `cli` | Ciclo 4 | Implementar pelo menos 3 filtros combináveis; os mesmos parâmetros devem produzir resultados equivalentes aos da Web. |
| #11 — Exportação CSV CLI | Mateus | `cli` | Ciclo 4 | Exportar resultados com filtros sem limitar à página atual e explicar no README como acessar o arquivo. |
| #12 — Indicadores CLI | Jaime | `cli` | Ciclo 4 | Exibir em texto os mesmos 3 indicadores e valores do dashboard Web. |

As issues #13 a #16 da avaliação são exclusivas de equipes de quatro pessoas e não fazem parte deste plano.

## 4. Passo a passo de cada issue principal

### 4.1. Responsável: iniciar o trabalho

1. Confira se o ciclo atual foi liberado e se as dependências da issue já foram integradas. Leia a issue, esta spec e os critérios da avaliação.
2. Confirme que a issue no GitHub foi criada e atribuída a você, com labels e milestone corretas.
3. Atualize as referências do `main` do repositório principal e crie uma branch de trabalho em um **worktree próprio**, partindo da versão integrada apropriada. Trabalhe no seu fork; a PR deve ter como destino o `main` do repositório principal.
4. Combine com os colegas como lidar com alterações simultâneas nos arquivos compartilhados. Não é obrigatório gerar conflitos nem fazer rebase local, mas a versão entregue precisa incluir as integrações necessárias.

### 4.2. Responsável: desenvolver e testar

1. Use **TDD**: escreva ou ajuste testes, implemente e execute os testes ao fim de cada rodada; garanta que passem e que a alteração funcione. Código e comentários de código devem estar em inglês.
2. Respeite o limite de quatro arquivos de produção: `index.html` com HTML/CSS/JS embutidos, um arquivo CLI, um arquivo API e um arquivo de acesso a dados, com equivalentes em Go. Testes, dependências, infraestrutura e configuração são permitidos à parte, mas cada arquivo auxiliar criado ou alterado deve ser listado e justificado na PR.
3. Em cada PR principal **Web**, altere `index.html`, API, banco e `README.md`. Em cada PR principal **CLI**, altere CLI, API, banco e `README.md`. Acrescente ao README uma seção para a funcionalidade. A #1 documenta início/login Web e a #7 documenta uso da CLI.
4. Web e CLI usam a mesma API REST e banco, mas têm endpoints separados; a CLI nunca acessa o banco diretamente e as funcionalidades Web têm consultas SQL próprias. Implemente os métodos e caminhos da tabela de endpoints da avaliação.
5. Proteja endpoints funcionais: credenciais ausentes ou inválidas retornam **HTTP 401**; healthchecks podem ser públicos. Na Web, use o login previsto; na CLI, obtenha o token no endpoint próprio, envie `Authorization: Bearer <token>` e aceite token por variável de ambiente. Não armazene senhas em texto puro nem registre segredos.
6. Registre cada operação em JSON no stdout da API e, quando houver acesso a dados, também na camada de banco, inclusive leituras. Correlacione ambas pelo mesmo `request_id`; inclua `timestamp` ISO 8601 UTC, `level`, `layer`, `client`, `operation`, `status`, `request_id` e `message`. Registre sucessos e falhas de autenticação sem expor segredos.
7. Execute tudo **em contêineres via Docker Compose**. `docker compose up` deve iniciar Web, API e banco; CLI, testes e cobertura devem usar `docker compose run` ou `docker compose exec`. Não pressuponha linguagens, banco ou ferramentas de teste instalados no host.
8. Execute os testes unitários das alterações em CLI, API e banco, conforme aplicável, e os testes anteriores. Comprove **100% de cobertura de linhas** de todo o código desses arquivos já integrado, medindo apenas os arquivos existentes nas primeiras PRs. `index.html`, testes, infraestrutura e configuração não entram na cobertura obrigatória. A cobertura padrão do Go mede *statements*: ela não comprova, por si só, cobertura de linhas; escolha e documente uma verificação específica de linhas.
9. Atualize o README com os comandos completos que já existirem para execução, autenticação, uso, dados, logs, testes e cobertura. A configuração Compose e os comandos de teste ainda precisam ser implementados; não os trate como já disponíveis.

### 4.3. Responsável: abrir a PR

1. Envie a branch ao seu fork e abra **uma única PR principal**, de sua autoria, apontando para o `main` do repositório principal. Não divida a issue em PRs separadas por camada.
2. Preencha [`.github/PULL_REQUEST_TEMPLATE.md`](../.github/PULL_REQUEST_TEMPLATE.md). Inclua `Closes #N` ou `Fixes #N` com o número real da issue no repositório principal.
3. Descreva funcionalidade, arquivos alterados, justificativa individual dos arquivos auxiliares, endpoints, autenticação e logs aplicáveis. Registre comandos reais via Compose, resultados dos testes, evidência de 100% de cobertura de linhas e como verificou o funcionamento.
4. Solicite revisão aos **dois outros integrantes**. Se houver correções antes do merge, atualize a mesma PR e execute novamente as verificações necessárias.

### 4.4. Os dois colegas: revisar e aprovar

Cada colega deve, individualmente:

1. Conferir os critérios da issue, o destino da PR, os arquivos obrigatórios e as justificativas de arquivos auxiliares.
2. Verificar documentação, comportamento, endpoints, autenticação, logs, testes, cobertura de linhas e execução via Compose, conforme aplicável.
3. Solicitar ajustes se necessário e, após conferir a versão corrigida, registrar uma **aprovação no GitHub**. Comentários e aprovação do autor não substituem as duas aprovações dos colegas.
4. Guardar uma PR revisada de cada colega para explicar no vídeo o que verificou e por que aprovou.

### 4.5. Integração, tag e release

1. Confirme as duas aprovações válidas, a posição da issue no ciclo, as dependências, testes e verificações. Garanta que a integração preserve o histórico linear.
2. Integre a PR no `main` principal **somente com Rebase and merge**. Não use merge commit, Squash and merge, commits diretos nem force push no `main`.
3. Antes de integrar a próxima PR principal, identifique o **commit do `main` resultante** deste merge. Crie nele uma tag única em `vMAJOR.MINOR.PATCH`, com versão maior que as anteriores, e publique uma release identificando a funcionalidade, a issue e a PR. Aponte a tag para o commit integrado, não para o branch da feature.
4. Confirme que a issue foi fechada pelo vínculo da PR, registre a integração e avise a equipe. Repita para os demais autores do ciclo. O próximo ciclo só é liberado quando as três PRs principais atuais estiverem integradas.

## 5. Correções após o merge

Quando um problema for descoberto após a integração:

1. Abra uma **PR adicional**, sem criar uma nova issue para a correção, e explique o motivo usando `Refs #N`, com o número real da issue original.
2. Use fork, branch e worktree, preencha o template e altere apenas os arquivos necessários. A correção não precisa alterar os quatro arquivos exigidos de uma PR principal.
3. Se alterar CLI, API ou banco, preserve os testes e os **100% de cobertura de linhas** dos arquivos cobertos. Execute as verificações aplicáveis via Compose.
4. Obtenha a aprovação registrada dos dois colegas e integre somente com **Rebase and merge**. A correção não conta no ciclo, não libera o próximo ciclo e não exige tag ou release.

## 6. Checklist de encerramento e apresentação

- [ ] Há 12 issues e 12 PRs principais: quatro criadas, assumidas e entregues por cada integrante; cada ciclo tem uma PR integrada por pessoa.
- [ ] Todas as PRs, inclusive corretivas, apontaram para o `main` principal, tiveram duas aprovações dos colegas e foram integradas com Rebase and merge; o histórico está linear.
- [ ] Há ao menos 12 tags SemVer únicas e crescentes, cada uma no commit resultante da respectiva PR principal, e 12 releases identificando funcionalidade, issue e PR. A última release corresponde ao `main` final funcionando; qualquer tag/release extra é justificada no GitHub e no vídeo.
- [ ] A partir de um clone do `main` final, Web, API, banco, CLI, testes e cobertura funcionam exclusivamente via Compose. O host precisa apenas de Docker, Docker Compose, navegador e terminal.
- [ ] O README final ensina a iniciar, autenticar, preparar dados, usar Web/CLI, acessar CSV e logs, executar testes e verificar cobertura de linhas. Foram conferidos logs correlacionados de uma operação Web e uma CLI que acessam o banco.
- [ ] Há um único vídeo de até **18 minutos**, com câmera ligada durante a explicação. Cada pessoa apresenta suas quatro issues/PRs, correções, funcionalidades e tags/releases; explica uma PR revisada de cada colega; mostra worktrees, branches, histórico e ciclos; e relaciona os arquivos exigidos à documentação. Não é necessário demonstrar comandos Git. Preserve os worktrees até a apresentação.
