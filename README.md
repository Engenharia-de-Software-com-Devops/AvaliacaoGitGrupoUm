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

O segundo comando executa os testes do verificador, gera o perfil de cobertura do Go e calcula separadamente as **linhas físicas executáveis** de `api.go` e `db.go` a partir dos intervalos instrumentados. Uma linha é considerada coberta se pelo menos um trecho executável nela foi executado; linhas vazias, comentários e linhas apenas com delimitadores são excluídas. O comando falha se faltar cobertura ou dados para qualquer arquivo. A porcentagem de *statements* do Go é exibida à parte e não é tratada como equivalente à cobertura de linhas.

Para verificar a integração real com o PostgreSQL e a API, depois de exportar as credenciais acima:

```bash
docker compose --profile test run --build --rm integration
```

O teste usa o usuário configurado, verifica login válido e inválido, sessão, healthcheck e hash da senha no banco. A CLI e as funcionalidades de tarefas serão adicionadas nas próximas issues.

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
