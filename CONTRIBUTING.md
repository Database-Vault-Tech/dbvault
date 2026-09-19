# Contributing to DBVault

Thanks for helping make PostgreSQL backups boring (in the good way). This
guide gets you from clone to pull request.

## Ground rules

- **Correctness over features.** DBVault handles people's data. A change that
  can report a false success, lose a backup, or leak a secret will not be
  merged, however nice the feature.
- **Use battle-tested tooling.** We shell out to `pg_dump`/`pg_restore` and use
  established crypto (`age`, AES-GCM). Please don't propose custom formats or
  custom cryptography.
- **Keep it understandable.** DBVault is a modular monolith. Prefer a clear
  function over a new abstraction.

## Development setup

See [docs/development.md](docs/development.md) for the full guide. The short
version:

```bash
./scripts/setup.sh                 # .env with random secrets
docker compose up -d postgres redis minio minio-init verify-postgres mailpit
cd backend && go run ./cmd/api     # plus ./cmd/worker and ./cmd/scheduler
cd frontend && npm install && API_URL=http://localhost:8080 npm run dev
```

## Tests

Every change needs tests at the right level:

| What you changed                        | Run                                   |
| --------------------------------------- | ------------------------------------- |
| Go logic (retention, crypto, parsing…)  | `cd backend && go test ./...`         |
| Backup/restore/queue/API behaviour      | `./scripts/test-integration.sh`       |
| CLI                                     | `cd cli && go test ./... && go vet ./...` |
| Frontend                                | `cd frontend && npm run lint && npm run typecheck && npm test` |
| End-to-end flow                         | `cd frontend && npm run test:e2e` (needs the stack running) |

CI runs all of these on every pull request.

## Pull requests

1. Open an issue first for anything larger than a bug fix, so we can agree on
   the approach.
2. Keep PRs focused; one logical change per PR.
3. Write a description that explains *why*, and how you tested it.
4. Update the docs (`README.md`, `docs/`) when behaviour or configuration
   changes. New environment variables must be documented in `.env.example`.
5. Security-sensitive changes (auth, crypto, storage credentials, restore)
   get an extra review.

## Commit style

Short imperative subject line (`Add R2 endpoint validation`), wrapped body
explaining the reasoning when it isn't obvious.

## Reporting security issues

Please **do not** open public issues for vulnerabilities. See
[SECURITY.md](SECURITY.md).

## License

By contributing you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE).
