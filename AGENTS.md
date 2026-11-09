# Agent instructions

## Scope and stack

- This is a postgraduate Software Engineering with DevOps assignment for a Version Control course. The Git/GitHub collaboration process is central to the assessment.
- `docs/Avaliacao_Git_GitHub.md` is the source of truth for requirements and acceptance criteria. Use `.github/PULL_REQUEST_TEMPLATE.md` for PRs.
- The chosen stack is Go for the back end and PostgreSQL for the database. The application, Compose configuration, and test commands have not yet been implemented.
- Write code and code comments in English. Use TDD for development; at the end of every development round, run the tests and ensure they pass and the changes work.

## Application constraints

- Deliver a Web + CLI CRUD using the same REST API and database. Limit production code to four files: `index.html` with embedded HTML/CSS/JS, one CLI file, one API file, and one database-access file; Go equivalents are allowed. Additional test, dependency, infrastructure, and configuration files are allowed, but each auxiliary file created or changed must be listed and justified in the PR.
- Each main Web PR changes `index.html`, the API, database code, and README; each main CLI PR changes the CLI, API, database code, and README. Add a README section for each main PR's feature; issue #1 documents starting the Web and #7 documents using the CLI. The CLI must not access the database directly; Web and CLI use separate endpoints, and the Web has its own SQL queries. See the assessment for required paths and methods.
- Functional endpoints require authentication and return HTTP 401 for missing or invalid credentials; healthchecks may be public. The CLI uses a Bearer token and must accept it through an environment variable. Never store plaintext passwords or log secrets.
- Log each operation as JSON to stdout in the API and, when accessing data, also in the database layer, correlating events with the same `request_id`, including reads. The assessment specifies the required fields.
- Everything must run exclusively through Docker Compose: `docker compose up` starts the Web, API, and database; CLI, tests, and coverage run in containers. Document complete commands and log access in the README.
- Each main PR must include unit tests for applicable changes to the CLI, API, and database, maintaining 100% line coverage of all code already integrated in those files; measure only files that exist in early PRs. Corrective PRs changing covered files must preserve tests and coverage. `index.html`, tests, infrastructure, and configuration are excluded. Go's standard coverage reports statements, so verify the required line coverage separately rather than assuming the metrics are identical.

## Integration workflow

- Create a spec for the issues, each with description, acceptance criteria, assignee, labels, and milestone. Each member creates, takes ownership of, and delivers four main issues using their own fork and worktrees, across four cycles: everyone must integrate one main PR in the current cycle before the next begins. Respect dependencies and issue-specific acceptance criteria.
- Every PR targets the main repository's `main` and requires recorded approval from every other member. A main PR is authored by the issue owner and uses `Closes #N` or `Fixes #N`. A post-merge correction is an additional PR without a new issue, explains its reason, uses `Refs #N`, and does not count toward a cycle.
- After repository creation, do not commit directly to `main`. Integrate PRs only with **Rebase and merge**, keeping history linear; do not force-push to `main`.
- After each main PR, create a unique tag in `vMAJOR.MINOR.PATCH` format on the resulting `main` commit, increasing versions in merge order, and a release identifying the feature, issue, and PR. Corrections do not require a tag or release.
