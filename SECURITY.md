# Security and privacy

Huginn stores tasks locally on a dedicated Git branch. `sync` and remote task
claims communicate with the Git remote you configure. Tasks, notes, and import
baselines may contain sensitive project information: choose remote access
accordingly. Making the application public does not make your task branch public
unless you push it to a public remote.

## No secrets in this repository

- Never commit real task exports, databases, `.env` files, API keys or tokens.
- Evaluation and importer fixtures are synthetic only.
- `scripts/check-secrets.sh` runs as a pre-commit hook
  (`scripts/install-hooks.sh`) and in CI. It has no bypass flag.
- The Jcode installer only edits MCP configuration, preserves other servers,
  and keeps timestamped backups. It never reads harness credential files.
- Release installers verify archives against published SHA256SUMS.

## Reporting

Report vulnerabilities privately through GitHub's "Report a vulnerability" on
this repository. Do not include secrets or private tasks in public issues.
