# Avaliacao Git Grupo Um

## Sobre o projeto

O projeto será uma **ToDo List** para gerenciar tarefas, desenvolvida como parte da avaliação prática de Git e GitHub. A aplicação terá interfaces Web e CLI para cadastrar, editar, listar, excluir e buscar tarefas, além de exportar dados em CSV e consultar indicadores.

As duas interfaces utilizarão a mesma API REST e o mesmo banco PostgreSQL; o back-end será desenvolvido em Go. A aplicação, a configuração Docker Compose e os comandos de testes ainda serão implementados ao longo das issues. Os campos das tarefas, suas validações, os filtros e os indicadores serão definidos pela equipe, respeitando os requisitos da avaliação.

## Grupo

| Integrante | Nome | Matrícula | Issues principais |
|---|---|---|---|
| A | Marcos Vinicius | 2651654 | #1, #4, #7 e #10 |
| B | Mateus | 2650377 | #2, #5, #8 e #11 |
| C | Jaime | 2650365 | #3, #6, #9 e #12 |

Consulte o [guia do ciclo de issues](docs/Ciclo_de_Issues.md) para a ordem de integração, responsabilidades e checklists da equipe.

## Issue #1 — Estrutura Web e login

### Pré-requisitos e configuração

São necessários somente Docker com Docker Compose, navegador e terminal. Os comandos abaixo usam Bash; execute-os na raiz do repositório. Informe valores próprios: não coloque senhas em arquivos versionados ou no histórico do terminal.

```bash
read -r -p 'Usuário inicial: ' INITIAL_USERNAME
read -r -s -p 'Senha inicial da aplicação: ' INITIAL_PASSWORD
printf '\n'
read -r -s -p 'Senha do PostgreSQL: ' DB_PASSWORD
printf '\n'
export INITIAL_USERNAME INITIAL_PASSWORD DB_PASSWORD
```

O usuário inicial é criado se ainda não existir, e a senha da aplicação é armazenada no banco como hash bcrypt. Como o banco persiste em um volume, alterar as variáveis posteriormente não altera uma senha já criada; mantenha também a mesma senha do PostgreSQL em reinicializações. As variáveis precisam estar disponíveis no terminal ao executar os comandos Compose. Não compartilhe a saída de `docker inspect`, pois a configuração dos contêineres inclui variáveis de ambiente.

### Iniciar e acessar

```bash
docker compose up --build
```

Acesse <http://localhost:8080>, informe o usuário e a senha configurados e entre. O serviço HTTP entrega a página e a API; o PostgreSQL não é exposto ao host. `GET /api/web/health` é público; `POST /api/web/login` autentica e `GET /api/web/session` exige sessão válida, retornando HTTP 401 quando ela está ausente ou inválida. A sessão usa cookie `HttpOnly` com `SameSite=Strict` e expira após 12 horas; reiniciar a aplicação invalida as sessões.

Esta configuração é para execução local: a porta é vinculada a `127.0.0.1` e a conexão entre API e banco não usa TLS. Em uma implantação HTTPS, configure `COOKIE_SECURE=true` e providencie HTTPS e proteção apropriada para a comunicação com o banco.

### Logs

Em outro terminal com as mesmas variáveis exportadas:

```bash
docker compose logs -f app
```

Cada linha da API e da camada de banco é JSON. Em uma operação que acessa dados, relacione as duas linhas pelo mesmo `request_id`. Senhas e tokens não devem aparecer nos logs.

### Testes e cobertura

Os testes unitários usam um banco simulado e não precisam das variáveis de credenciais:

```bash
docker compose --profile test run --build --rm tests
docker compose --profile test run --build --rm tests sh tools/coverage.sh
```

O segundo comando executa os testes do verificador, gera o perfil de cobertura do Go e calcula separadamente as **linhas físicas executáveis** de `api.go`, `db.go` e `cli/main.go` a partir dos intervalos instrumentados. Uma linha é considerada coberta se pelo menos um trecho executável nela foi executado; linhas vazias, comentários e linhas apenas com delimitadores são excluídas. O comando falha se faltar cobertura ou dados para qualquer arquivo. A porcentagem de *statements* do Go é exibida à parte e não é tratada como equivalente à cobertura de linhas.

Para verificar a integração real com o PostgreSQL e a API, depois de exportar as credenciais acima:

```bash
docker compose --profile test run --build --rm integration
```

O teste usa o usuário configurado e verifica login, sessão, healthchecks, hash da senha, funcionalidades Web e autenticação da CLI.

## Issue #2 — Cadastro e edição de tarefas (Web)

### Campos da tarefa

| Campo | Obrigatório | Regra de validação |
|---|---|---|
| `title` (Título) | sim | 3 a 100 caracteres |
| `description` (Descrição) | não | até 500 caracteres |
| `priority` (Prioridade) | sim | `low`, `medium` ou `high` |
| `status` (Situação) | sim | `pending`, `in_progress` ou `done` |
| `due_date` (Data de entrega) | sim | data válida no formato `AAAA-MM-DD` |
| `category` (Categoria) | sim | 2 a 50 caracteres |

Espaços no início e no fim dos textos são removidos antes da validação. As regras são verificadas no navegador e novamente na API; o banco também restringe `priority` e `status` com `CHECK`. A tabela `tasks` é criada automaticamente na inicialização da aplicação.

### Usar pela Web

1. Inicie a aplicação com `docker compose up --build` e entre em <http://localhost:8080>.
2. Após o login, preencha o formulário **Nova tarefa** e clique em **Cadastrar tarefa**. Campos inválidos ficam destacados, com a mensagem de erro ao lado.
3. Para editar, clique em **Editar** em uma tarefa da tabela **Tarefas** (issue #3) ou informe o ID em **Editar tarefa existente** e clique em **Carregar**. Altere os campos e clique em **Salvar alterações**; **Cancelar edição** volta ao modo de cadastro.

### Endpoints

Todos exigem a sessão obtida em `POST /api/web/login` e retornam **HTTP 401** sem sessão válida.

| Método e rota | Função | Respostas |
|---|---|---|
| `POST /api/web/records` | Cadastra uma tarefa | 201 com a tarefa criada; 400 JSON inválido; 422 campos inválidos |
| `PUT /api/web/records/{id}` | Edita uma tarefa | 200 com a tarefa atualizada; 400; 404 tarefa inexistente; 422 |
| `GET /api/web/records/{id}` | Carrega uma tarefa para edição | 200 com a tarefa; 404 tarefa inexistente |

A resposta 422 tem o formato `{"errors": {"campo": "mensagem"}}`, com uma mensagem por campo inválido.

### Logs

Cada requisição gera uma linha `layer: "api"` (`operation` igual a `create`, `update` ou `get`) e, quando chega ao banco, uma linha `layer: "db"` (`insert`, `update` ou `select`) com o mesmo `request_id`. Requisições rejeitadas por autenticação ou validação geram somente o log da API, com `status: "error"`. Para acompanhar:

```bash
docker compose logs -f app | grep -E '"operation":"(create|update|get|insert|select)"'
```

### Testes

Os testes unitários de `api.go` e `db.go` cobrem validação, autenticação, respostas 400/401/404/422/500 e as consultas SQL, e rodam com os mesmos comandos da seção de testes da issue #1. O teste de integração também cadastra, valida, edita e recarrega uma tarefa, conferindo os valores gravados no PostgreSQL.

## Issue #3 — Listagem e exclusão de tarefas (Web)

### Usar pela Web

1. Inicie a aplicação com `docker compose up --build` e entre em <http://localhost:8080>.
2. Após o login, a tabela **Tarefas** é carregada do banco automaticamente. Ela mostra 4 campos — **Título**, **Prioridade**, **Situação** e **Data de entrega** — com **5 tarefas por página**.
3. Use **Anterior** e **Próxima** para navegar; o rodapé mostra a página atual, o total de páginas e o total de tarefas.
4. Clique no nome de uma coluna para ordenar por ela; um novo clique inverte a ordem (▲ crescente, ▼ decrescente). Prioridade e situação seguem a ordem lógica (baixa → média → alta; pendente → em andamento → concluída), e o título ignora maiúsculas e minúsculas.
5. Marque as tarefas desejadas (ou a caixa do cabeçalho para selecionar todas da página) e clique em **Excluir selecionadas**. Após a confirmação, as tarefas são removidas do banco e a tabela é recarregada. Se alguma não puder ser excluída (por exemplo, já removida em outra aba), a mensagem informa qual.

Cadastrar ou editar uma tarefa também recarrega a tabela.

### Endpoints

Ambos exigem a sessão obtida em `POST /api/web/login` e retornam **HTTP 401** sem sessão válida.

| Método e rota | Função | Respostas |
|---|---|---|
| `GET /api/web/records` | Lista uma página de tarefas | 200 com a página; 400 parâmetro inválido |
| `DELETE /api/web/records/{id}` | Exclui uma tarefa | 204 sem corpo; 404 tarefa inexistente |

Parâmetros opcionais de `GET /api/web/records` (query string):

| Parâmetro | Valores | Padrão |
|---|---|---|
| `page` | inteiro de 1 a 100000 | `1` |
| `sort` | `id`, `title`, `priority`, `status`, `due_date` | `id` |
| `order` | `asc`, `desc` | `asc` |

O tamanho da página é fixo em 5. Exemplo de resposta:

```json
{"items":[{"id":8,"title":"Estudar Git","description":"","priority":"high","status":"in_progress","due_date":"2026-11-18","category":"estudos"}],"page":1,"page_size":5,"total":7,"total_pages":2}
```

Uma página além da última retorna `items` vazio; a Web volta automaticamente para a última página existente (por exemplo, depois de excluir todas as tarefas da última página).

### Logs

A listagem gera `operation: "list"` na API e `operation: "select"` no banco; a exclusão gera `operation: "delete"` nas duas camadas, sempre com o mesmo `request_id`. Requisições sem sessão ou com parâmetros inválidos geram somente o log da API, com `status: "error"`. Para acompanhar:

```bash
docker compose logs -f app | grep -E '"operation":"(list|select|delete)"'
```

### Testes

Os testes unitários cobrem a leitura e a validação dos parâmetros, as respostas 200/204/400/401/404/500, a ordenação de cada coluna, a paginação e as falhas do banco, e rodam com os mesmos comandos da seção de testes da issue #1. O teste de integração cria tarefas suficientes para duas páginas, confere 5 itens na primeira página e a ordenação por título, exclui uma tarefa, confirma o 404 ao repetir a exclusão e verifica no PostgreSQL que o registro foi removido.

Em clones no Windows com `core.autocrlf=true`, o arquivo `.gitattributes` mantém os scripts `.sh` com quebras de linha LF, para que `tools/coverage.sh` rode no contêiner Linux.

## Issue #4 — Busca avançada de tarefas (Web)

### Usar pela Web

Após entrar, use **Busca avançada** acima da tabela para combinar os filtros **Título contém**, **Prioridade** e **Situação**. Clique em **Buscar** para atualizar a tabela sem recarregar a página. O título é procurado como texto literal, sem diferenciar maiúsculas de minúsculas; prioridade e situação usam os valores exatos escolhidos. Campos vazios não restringem os resultados.

A busca preserva a ordenação e a paginação de cinco tarefas, calculando os totais sobre os resultados filtrados. Buscar ou clicar em **Limpar filtros** volta à primeira página; limpar remove os filtros e retorna à listagem completa. A tabela permanece filtrada ao ordenar, navegar, cadastrar, editar ou excluir tarefas.

### Endpoint

`GET /api/web/records/search` exige a sessão obtida no login e retorna **401** sem sessão válida. Aceita os seguintes parâmetros opcionais, combinados com `AND`:

| Parâmetro | Valores |
|---|---|
| `title` | Trecho literal de até 100 caracteres; ignora maiúsculas/minúsculas e espaços nas extremidades |
| `priority` | `low`, `medium` ou `high` |
| `status` | `pending`, `in_progress` ou `done` |
| `page`, `sort`, `order` | Mesmos valores e padrões da listagem da issue #3 |

Valores inválidos retornam HTTP 400. Sem filtros, a busca retorna todas as tarefas, paginadas; a resposta tem o mesmo formato da listagem (`items`, `page`, `page_size`, `total`, `total_pages`). O endpoint usa consulta SQL própria e parametrizada.

### Logs e testes

A busca gera `operation: "search"` na API e no banco, correlacionados pelo mesmo `request_id`; requisições rejeitadas antes do acesso ao banco geram somente o log da API. Para acompanhar:

```bash
docker compose logs -f app | grep '"operation":"search"'
```

Os testes e a cobertura usam os comandos da issue #1. O teste de integração verifica combinações de filtros, paginação, caracteres literais, filtros inválidos e acesso sem sessão no PostgreSQL real.

## Issue #5 — Exportação CSV (Web)

### Usar pela Web

Na seção **Tarefas**, clique em **Exportar CSV**. O navegador baixa o arquivo `tarefas.csv` com:

- **todas as tarefas da busca ativa**, ou seja, dos últimos filtros enviados em **Buscar**, e não apenas as cinco linhas da página exibida;
- **todas as tarefas cadastradas**, quando não há busca ativa (ao abrir a página ou depois de **Limpar filtros**).

As linhas seguem a mesma ordenação da tabela. Filtros digitados mas ainda não enviados com **Buscar** não entram na exportação.

### Formato do arquivo

Arquivo CSV em UTF-8, separado por vírgulas, com uma linha de cabeçalho:

```csv
id,title,description,priority,status,due_date,category
7,Write report,Quarterly numbers,high,pending,2026-11-20,work
```

Valores com vírgula, aspas ou quebra de linha ficam entre aspas, e as aspas internas são duplicadas (RFC 4180). `priority` e `status` usam os códigos da API (`low`/`medium`/`high` e `pending`/`in_progress`/`done`) e `due_date` usa o formato `AAAA-MM-DD`. Ao abrir no Excel com separador `;`, use **Dados → De Texto/CSV** e escolha vírgula como delimitador.

### Endpoint

`GET /api/web/records/export` exige a sessão obtida no login e retorna **401** sem sessão válida. Aceita os mesmos parâmetros opcionais da busca da issue #4 (`title`, `priority`, `status`, combinados com `AND`) e a ordenação da listagem (`sort`, `order`). O parâmetro `page` é ignorado, pois a exportação nunca é paginada. Valores inválidos retornam **400**.

A resposta usa `Content-Type: text/csv; charset=utf-8` e `Content-Disposition: attachment; filename="tarefas.csv"`. O endpoint tem consulta SQL própria, parametrizada e sem `LIMIT`/`OFFSET`.

### Logs e testes

A exportação gera `operation: "export"` na API e no banco, correlacionados pelo mesmo `request_id`; requisições rejeitadas antes do acesso ao banco geram somente o log da API. O conteúdo das tarefas e os filtros não são registrados. Para acompanhar:

```bash
docker compose logs -f app | grep '"operation":"export"'
```

Os testes e a cobertura usam os comandos da issue #1. Os testes unitários verificam o cabeçalho, o escape de vírgulas, aspas e quebras de linha, os filtros repassados ao banco, a ausência de paginação na consulta e as respostas 400, 401 e 500. O teste de integração exporta, com o PostgreSQL real, seis tarefas filtradas (mais de uma página), confere a ordenação, os caracteres literais e um filtro sem resultados, e verifica que a exportação sem filtros contém todas as tarefas da tabela.

## Issue #6 — Dashboard (Web)

### Usar pela Web

1. Inicie a aplicação com `docker compose up --build` e entre em <http://localhost:8080>.
2. Após o login, role até a seção **Dashboard**, abaixo da tabela de tarefas. Os três gráficos são carregados do banco pela API.
3. Os valores são atualizados ao **recarregar a página** e também logo depois de cadastrar, editar ou excluir uma tarefa.

### Indicadores

| Indicador | Gráfico | O que mostra |
|---|---|---|
| **Tarefas por situação** | Rosca | Quantidade e percentual de tarefas `pending` (Pendente), `in_progress` (Em andamento) e `done` (Concluída). O número no centro é o total de tarefas. |
| **Tarefas por prioridade** | Colunas | Quantidade de tarefas `low` (Baixa), `medium` (Média) e `high` (Alta). |
| **Tarefas por categoria** | Barras horizontais | Quantidade de tarefas em cada categoria cadastrada, da mais numerosa para a menos numerosa (empate em ordem alfabética). |

Situação e prioridade sempre exibem as três opções, inclusive as que têm zero tarefas. As categorias são agrupadas exatamente como foram digitadas. Sem tarefas, o dashboard mostra zeros e a mensagem "Nenhuma categoria cadastrada.". Os indicadores consideram todas as tarefas, independentemente da busca ativa na tabela. Os mesmos três indicadores serão exibidos em texto pela CLI na issue #12.

### Endpoint

`GET /api/web/dashboard` exige a sessão obtida em `POST /api/web/login` e retorna **HTTP 401** sem sessão válida. Responde 200 com os indicadores calculados no PostgreSQL (`GROUP BY`), com consultas próprias da Web:

```json
{"total":15,"by_status":[{"key":"pending","count":10},{"key":"in_progress","count":2},{"key":"done","count":3}],"by_priority":[{"key":"low","count":2},{"key":"medium","count":3},{"key":"high","count":10}],"by_category":[{"key":"tests","count":7},{"key":"estudos","count":3},{"key":"trabalho","count":3},{"key":"casa","count":2}]}
```

### Logs e testes

Cada carregamento gera `operation: "dashboard"` na API e no banco, com o mesmo `request_id`; sem sessão, só o log da API com `status: "error"`.

```bash
docker compose logs -f app | grep '"operation":"dashboard"'
```

Os testes unitários cobrem as três consultas, a soma do total, o dashboard vazio, as falhas do banco e as respostas 200/401/500. O teste de integração confere o 401, cria uma tarefa e verifica que o total aumenta ao recarregar, compara cada indicador da API com uma contagem feita diretamente no PostgreSQL e confirma que o total volta ao valor anterior após excluir a tarefa.

## Issue #7 — Estrutura CLI

A CLI está em `cli/main.go`, usa somente a API e roda pelo serviço Compose `cli`. Antes dos comandos abaixo, configure as variáveis da seção **Issue #1** e inicie a aplicação com `docker compose up --build` em outro terminal. Neste terminal, as mesmas variáveis também precisam estar exportadas.

### Ajuda e healthcheck

```bash
docker compose --profile cli run --rm --no-deps cli help
docker compose --profile cli run --rm --no-deps cli health
```

O primeiro mostra os subcomandos disponíveis; o segundo consulta `GET /api/cli/health` e imprime `OK` quando a API responde HTTP 200.

### Obter e usar um token

O usuário e a senha são os mesmos configurados na inicialização. Leia a senha sem eco, para não colocá-la no histórico de comandos:

```bash
TODO_USERNAME="$INITIAL_USERNAME"
read -r -s -p 'Senha da aplicação: ' TODO_PASSWORD
printf '\n'
export TODO_USERNAME TODO_PASSWORD
TODO_TOKEN="$(docker compose --profile cli run --rm --no-deps -T cli token)"
export TODO_TOKEN
unset TODO_PASSWORD
docker compose --profile cli run --rm --no-deps cli session
```

`token` envia as credenciais a `POST /api/cli/token` e retorna o token na saída padrão **para uso pelo operador**, não em logs. O comando `session` envia `Authorization: Bearer <token>` a `GET /api/cli/session` e imprime o usuário autenticado. Tokens ausentes ou inválidos são rejeitados com HTTP 401 pela API; a CLI também avisa quando a variável `TODO_TOKEN` está ausente. Tokens duram 12 horas e são invalidados ao reiniciar a aplicação.

Não compartilhe a saída do comando `token`, a variável `TODO_TOKEN` ou a saída de `docker inspect`; variáveis de ambiente dos contêineres podem conter segredos. Após o uso, remova o token do ambiente com `unset TODO_TOKEN`. A comunicação HTTP sem TLS é apenas para o ambiente local do Compose.

### Logs, testes e cobertura

```bash
docker compose logs -f app | grep '"client":"cli"'
docker compose --profile test run --build --rm tests
docker compose --profile test run --build --rm tests sh tools/coverage.sh
docker compose --profile test run --build --rm integration
```

Os eventos JSON da API usam `client: "cli"`; quando a autenticação consulta o banco, a camada de dados também gera um evento com o mesmo `request_id`. Senhas e tokens não são registrados. Os testes cobrem CLI, API e banco; a verificação de linhas inclui os três arquivos de produção existentes.

O teste de integração da exportação CSV (#5) passou a obter a ordem esperada do próprio PostgreSQL: a collation `en_US.utf8` da imagem ignora pontuação como `%_` ao ordenar, e a comparação byte a byte do Go discordava do banco, fazendo `docker compose --profile test run --build --rm integration` falhar.

## Issue #8 — Cadastro e edição de tarefas (CLI)

Os comandos usam o token obtido na seção **Issue #7**, lido da variável `TODO_TOKEN`; a aplicação precisa estar rodando com `docker compose up --build` em outro terminal. A CLI conversa somente com a API: nunca acessa o banco.

### Cadastrar

```bash
docker compose --profile cli run --rm --no-deps cli create \
  --title "Revisar PR da issue #9" \
  --description "Conferir testes e cobertura" \
  --priority high \
  --due-date 2026-11-20 \
  --category estudos
```

`--status` é opcional no cadastro e vale `pending` quando omitido, como no formulário Web. Em caso de sucesso, a CLI imprime `Task <id> created.` seguido de todos os campos gravados.

### Editar

```bash
docker compose --profile cli run --rm --no-deps cli update 12 \
  --title "Revisar PR da issue #9" \
  --priority medium \
  --status in_progress \
  --due-date 2026-11-22 \
  --category estudos
```

A edição substitui **todos** os campos da tarefa, como o `PUT` da Web. Informe novamente cada campo obrigatório; uma descrição omitida fica vazia. O `<id>` é o mesmo exibido na tabela Web e na saída do `create`.

### Campos e validação

As opções `--title`, `--description`, `--priority`, `--status`, `--due-date` e `--category` correspondem aos campos da tabela da **Issue #2**. A API aplica exatamente as mesmas regras da Web. Quando algum campo é inválido, a CLI lista cada erro e termina com código 1:

```text
Validation failed:
  due_date: Informe uma data de entrega válida (AAAA-MM-DD).
  priority: Escolha a prioridade: baixa, média ou alta.
```

| Situação | Mensagem | Código de saída |
|---|---|---|
| Opção desconhecida, argumento extra ou `update` sem id | ajuda de uso | 2 |
| `TODO_TOKEN` ausente | `Set TODO_TOKEN.` | 2 |
| Campos inválidos (HTTP 422) | `Validation failed:` e um erro por campo | 1 |
| Tarefa inexistente no `update` (HTTP 404) | `Task <id> not found.` | 1 |
| Token inválido ou expirado (HTTP 401) | `API returned HTTP 401.` | 1 |

### Endpoints

| Método e rota | Função | Respostas |
|---|---|---|
| `POST /api/cli/records` | Cadastra uma tarefa | 201 com a tarefa criada; 400; 401; 422 |
| `PUT /api/cli/records/{id}` | Edita uma tarefa | 200 com a tarefa atualizada; 400; 401; 404; 422 |

Os dois exigem `Authorization: Bearer <token>`. O cookie de sessão da Web não é aceito nesses endpoints, e o token da CLI não é aceito nos endpoints Web. As consultas SQL de cadastro e edição da CLI são próprias, separadas das usadas pela Web, mas gravam na mesma tabela `tasks`. Por isso as tarefas criadas ou editadas pela CLI aparecem imediatamente na tabela Web, depois de recarregar a página ou atualizar a listagem.

### Logs e testes

Cada comando gera um evento `layer: "api"` com `client: "cli"` (`operation` igual a `create` ou `update`) e, ao chegar ao banco, um evento `layer: "db"` com `client: "cli"` (`insert` ou `update`) e o mesmo `request_id`. O token não é registrado.

```bash
docker compose logs -f app | grep '"client":"cli"' | grep -E '"operation":"(create|update|insert)"'
```

Os testes e a cobertura usam os comandos da issue #7. Os testes unitários cobrem as opções e os códigos de saída da CLI, o corpo e os cabeçalhos enviados, as respostas 201/200/401/404/422, a rejeição do cookie Web nos endpoints CLI e as consultas SQL da CLI. O teste de integração executa a CLI real contra a API do Compose, cadastra e edita uma tarefa e confirma pela API Web que ela foi persistida e aparece na Web.

## Issue #9 — Listagem e exclusão de tarefas (CLI)

Os comandos usam o token da seção **Issue #7** (variável `TODO_TOKEN`), com a aplicação rodando por `docker compose up --build` em outro terminal. A CLI chama somente endpoints próprios da API; nunca acessa o banco.

### Listar

```bash
docker compose --profile cli run --rm --no-deps cli list
docker compose --profile cli run --rm --no-deps cli list --page 2 --page-size 3
docker compose --profile cli run --rm --no-deps cli list --sort priority --order desc
```

| Opção | Valores | Padrão |
|---|---|---|
| `--page` | número da página, a partir de 1 | `1` |
| `--page-size` | tarefas por página, de 1 a 100 | `10` |
| `--sort` | `id`, `title`, `priority`, `status` ou `due_date` | `id` |
| `--order` | `asc` ou `desc` | `asc` |

A saída é uma tabela em texto seguida do rodapé de paginação:

```text
ID  TITLE         PRIORITY  STATUS   DUE DATE    CATEGORY
22  Comprar café  low       pending  2026-11-16  casa
23  Revisar PR    high      pending  2026-11-17  trabalho
24  Gravar vídeo  medium    pending  2026-11-24  estudos
Page 2 of 2 (6 tasks)
```

A ordenação segue as mesmas regras da tabela Web (issue #3): título sem diferenciar maiúsculas e minúsculas, prioridade de baixa para alta, situação de pendente para concluída, e o `id` como desempate. Uma página além da última mostra `No tasks found.`.

### Excluir

```bash
docker compose --profile cli run --rm --no-deps cli delete 24
```

Imprime `Task 24 deleted.`. O `<id>` é o mesmo exibido no `list`, na saída do `create` e na tabela Web. A tarefa excluída some da tabela Web ao recarregar a página ou atualizar a listagem.

| Situação | Mensagem | Código de saída |
|---|---|---|
| Opção desconhecida, argumento extra ou `delete` sem id | ajuda de uso | 2 |
| Id não numérico ou menor que 1 | `Task id must be a positive integer.` | 2 |
| `TODO_TOKEN` ausente | `Set TODO_TOKEN.` | 2 |
| Opção de listagem fora dos valores aceitos (HTTP 400) | `Invalid list options: ...` | 2 |
| Tarefa inexistente no `delete` (HTTP 404) | `Task <id> not found.` | 1 |
| Token inválido ou expirado (HTTP 401) | `API returned HTTP 401.` | 1 |

### Endpoints

| Método e rota | Função | Respostas |
|---|---|---|
| `GET /api/cli/records?page=&page_size=&sort=&order=` | Lista uma página de tarefas | 200 com `items`, `page`, `page_size`, `total` e `total_pages`; 400; 401 |
| `DELETE /api/cli/records/{id}` | Exclui uma tarefa | 204 sem corpo; 401; 404 |

Os dois exigem `Authorization: Bearer <token>`; o cookie de sessão da Web não é aceito, e o token da CLI não é aceito em `GET /api/web/records` nem em `DELETE /api/web/records/{id}`. As consultas SQL de contagem, listagem e exclusão da CLI são próprias, separadas das usadas pela Web, mas operam sobre a mesma tabela `tasks`.

### Logs e testes

A listagem gera `operation: "list"` na API e `operation: "select"` no banco; a exclusão gera `operation: "delete"` nas duas camadas. Todos usam `client: "cli"` e o mesmo `request_id`. O token não é registrado.

```bash
docker compose logs -f app | grep '"client":"cli"' | grep -E '"operation":"(list|select|delete)"'
```

Os testes e a cobertura usam os comandos da issue #7. Os testes unitários cobrem as opções, a tabela e os códigos de saída da CLI, os parâmetros e as respostas 200/204/400/401/404/500 da API, a separação entre token da CLI e cookie da Web e as consultas SQL da CLI. O teste de integração executa a CLI real: lista com ordenação e uma tarefa por página, confere que o total é igual ao da listagem Web, exclui a tarefa e confirma pela API Web que ela não existe mais.

O teste de integração do `update` de tarefa inexistente (#8) passou a informar `--status`: sem ele a API responde 422 (situação obrigatória na edição) antes de procurar a tarefa, e o teste esperava a mensagem de tarefa não encontrada.

## Issue #10 — Busca avançada de tarefas (CLI)

Após iniciar a aplicação e obter o token conforme a **Issue #7**, use o comando `search` da CLI. Todos os filtros são opcionais e podem ser combinados:

```bash
docker compose --profile cli run --rm --no-deps cli search \
  --title "Revisar" --priority high --status pending \
  --page 1 --page-size 5 --sort title --order asc
```

| Opção | Comportamento |
|---|---|
| `--title` | Trecho literal do título, sem diferenciar maiúsculas e minúsculas; até 100 caracteres |
| `--priority` | `low`, `medium` ou `high` |
| `--status` | `pending`, `in_progress` ou `done` |
| `--page`, `--page-size`, `--sort`, `--order` | Mesmas opções da listagem CLI da issue #9 |

Os filtros preenchidos são combinados com `AND`; sem filtros, a busca retorna todas as tarefas, paginadas. O título é literal: `%` e `_` não são curingas. A CLI usa `GET /api/cli/records/search` com `Authorization: Bearer <token>` e uma consulta SQL CLI própria. Sem token válido, a API retorna **401**; filtros ou opções inválidos retornam **400**. Com `TODO_TOKEN` ausente, a CLI avisa antes de fazer a requisição.

Os mesmos filtros e a mesma ordenação produzem os mesmos resultados da busca Web. Para comparar páginas diretamente, use `--page-size 5`: a Web mostra cinco itens por página, enquanto a CLI usa dez por padrão.

Cada busca gera eventos JSON com `client: "cli"` e `operation: "search"` na API e no banco, correlacionados pelo mesmo `request_id`. Requisições rejeitadas antes de acessar dados geram apenas o evento da API. Para acompanhar:

```bash
docker compose logs -f app | grep '"operation":"search"'
```

Os testes unitários, a cobertura de linhas e a integração com PostgreSQL usam os comandos da seção **Issue #1**. A integração compara resultados Web e CLI, filtros combinados, paginação, busca sem resultados, caracteres literais e rejeição de tokens ausentes ou inválidos.

## Issue #11 — Exportação CSV (CLI)

O comando `export` usa o token obtido na seção **Issue #7** (variável `TODO_TOKEN`), com a aplicação rodando via `docker compose up --build` em outro terminal. Ele exporta **todas** as tarefas que atendem aos filtros, sem paginação: não existem `--page` nem `--page-size`. Sem filtros, exporta todas as tarefas.

### Gerar o arquivo e acessá-lo no host

A CLI roda dentro de um contêiner. Para o arquivo ficar disponível no seu computador, grave-o na pasta `exports/`, que o serviço `cli` monta a partir de `./exports` na raiz do repositório:

```bash
docker compose --profile cli run --rm --no-deps cli export \
  --title relatorio --priority high --status pending \
  --sort due_date --order desc \
  --output exports/tarefas.csv
```

A CLI responde `CSV saved to exports/tarefas.csv.`, e o arquivo fica em **`./exports/tarefas.csv`** no host, pronto para abrir no editor ou na planilha:

```bash
cat exports/tarefas.csv
```

O Docker cria a pasta `./exports` se ela não existir; ela não é versionada (`.gitignore`). Em hosts Linux, o arquivo pode pertencer ao usuário `root` do contêiner; use `sudo chown "$USER" exports/tarefas.csv` se precisar alterá-lo. Um caminho fora de `exports/` (por exemplo `--output /tmp/x.csv`) é gravado apenas dentro do contêiner e se perde ao final do comando.

Também é possível omitir `--output`. Nesse caso o CSV sai na saída padrão e pode ser redirecionado diretamente para um arquivo do host. A opção `-T` desativa o pseudo-terminal, para o conteúdo não ser alterado:

```bash
docker compose --profile cli run --rm --no-deps -T cli export --status done > concluidas.csv
```

### Opções

| Opção | Valores |
|---|---|
| `--title`, `--priority`, `--status` | Mesmos filtros da busca da issue #10, combinados com `AND` e todos opcionais |
| `--sort`, `--order` | Mesma ordenação da listagem CLI (`id`, `title`, `priority`, `status`, `due_date`; `asc`/`desc`) |
| `--output FILE` | Arquivo de destino; sem ela, o CSV vai para a saída padrão |

Opções desconhecidas ou valores inválidos terminam com código 2; falhas da API, token inválido (HTTP 401) e erros de gravação terminam com código 1. O arquivo só é criado depois que a API aceita a requisição, e um arquivo incompleto é removido se o download falhar.

### Formato e equivalência com a Web

O conteúdo é **idêntico ao da exportação Web (#5)** para os mesmos filtros e ordenação: UTF-8, separado por vírgulas, com o cabeçalho `id,title,description,priority,status,due_date,category` e escape RFC 4180 de vírgulas, aspas e quebras de linha.

### Endpoint

`GET /api/cli/records/export` exige `Authorization: Bearer <token>`; o cookie da sessão Web não é aceito. Sem token válido, retorna **401**. Aceita `title`, `priority`, `status`, `sort` e `order`. Os parâmetros `page` e `page_size` são ignorados, e valores inválidos retornam **400**. A resposta usa `Content-Type: text/csv; charset=utf-8` e `Content-Disposition: attachment; filename="tarefas.csv"`. A consulta SQL é própria da CLI, parametrizada e sem `LIMIT`/`OFFSET`.

### Logs e testes

A exportação gera `operation: "export"` com `client: "cli"` na API e no banco, com o mesmo `request_id`. Requisições rejeitadas antes do banco geram somente o log da API. Token, filtros e conteúdo das tarefas não são registrados.

```bash
docker compose logs -f app | grep '"client":"cli"' | grep '"operation":"export"'
```

Os testes e a cobertura usam os comandos da seção **Issue #7**. Os testes unitários cobrem as opções e os códigos de saída do comando, a gravação na saída padrão e em arquivo, a remoção de arquivo incompleto, o endpoint (CSV exato, filtros, paginação ignorada, 400/401/500, cookie Web recusado) e a consulta SQL sem paginação. O teste de integração compara, com o PostgreSQL real, o CSV da CLI com o da Web: pelo endpoint com `page` e `page_size`, pela CLI na saída padrão, com `--output` e sem filtros.

## Issue #12 — Indicadores (CLI)

O comando usa o token da seção **Issue #7** (variável `TODO_TOKEN`), com a aplicação rodando por `docker compose up --build` em outro terminal. A CLI chama somente o endpoint próprio da API; nunca acessa o banco.

### Exibir os indicadores

```bash
docker compose --profile cli run --rm --no-deps cli dashboard
```

A saída mostra, em texto, **os mesmos três indicadores e valores do dashboard Web** (seção **Issue #6**): tarefas por situação, por prioridade e por categoria, cada valor com sua quantidade e o percentual do total.

```text
Total: 5 tasks

Tasks by status
  pending          2   40%
  in_progress      1   20%
  done             2   40%

Tasks by priority
  low              1   20%
  medium           2   40%
  high             2   40%

Tasks by category
  casa             2   40%
  estudos          2   40%
  trabalho         1   20%
```

| Seção | Corresponde no dashboard Web | Ordem |
|---|---|---|
| `Tasks by status` | Rosca **Tarefas por situação** | `pending`, `in_progress`, `done`, sempre as três, inclusive com zero |
| `Tasks by priority` | Colunas **Tarefas por prioridade** | `low`, `medium`, `high`, sempre as três, inclusive com zero |
| `Tasks by category` | Barras **Tarefas por categoria** | da categoria mais numerosa para a menos numerosa; empate em ordem alfabética |

Os percentuais são arredondados como na Web. Sem tarefas, o comando mostra zeros e `(none)` em categorias.

| Situação | Mensagem | Código de saída |
|---|---|---|
| Argumento extra | ajuda de uso | 2 |
| `TODO_TOKEN` ausente | `Set TODO_TOKEN.` | 2 |
| Token inválido ou expirado (HTTP 401) | `API returned HTTP 401.` | 1 |

### Endpoint

`GET /api/cli/dashboard` exige `Authorization: Bearer <token>` e retorna **HTTP 401** sem token válido; o cookie de sessão da Web não é aceito, e o token da CLI não é aceito em `GET /api/web/dashboard`. Responde 200 com o mesmo formato JSON do endpoint Web (`total`, `by_status`, `by_priority`, `by_category`), calculado no PostgreSQL por consultas `GROUP BY` próprias da CLI, separadas das consultas da Web.

### Logs e testes

Cada execução gera `operation: "dashboard"` com `client: "cli"` na API e no banco, com o mesmo `request_id`; sem token válido, só o log da API com `status: "error"`. O token não é registrado.

```bash
docker compose logs -f app | grep '"client":"cli"' | grep '"operation":"dashboard"'
```

Os testes e a cobertura usam os comandos da issue #7. Os testes unitários cobrem a saída e os códigos de saída da CLI (inclusive banco vazio e alinhamento das colunas), as respostas 200/401/500 da API, a separação entre token da CLI e cookie da Web e as três consultas da CLI. O teste de integração executa o comando real `dashboard` e confere, linha a linha, que seções, chaves e quantidades são iguais às do `GET /api/web/dashboard` no mesmo banco.
