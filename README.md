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
