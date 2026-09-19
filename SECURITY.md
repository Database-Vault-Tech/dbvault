# Security Policy

DBVault stores database credentials and backups of production data. We treat
security reports as our highest priority.

## Supported versions

Security fixes are released for the latest minor version. Self-hosted users
should track the latest release.

## Reporting a vulnerability

**Please do not open a public issue.** Instead, use GitHub's
[private vulnerability reporting](https://github.com/dbvault/dbvault/security/advisories/new)
or email **security@dbvault.dev**.

Include:

- a description of the issue and its impact,
- steps to reproduce (a proof of concept if possible),
- affected versions/commit and your deployment mode.

We will acknowledge your report within **3 business days**, keep you informed
of progress, and credit you in the advisory (unless you prefer otherwise).
Please give us a reasonable window (normally 90 days) to ship a fix before
public disclosure.

## Scope

In scope: the API, worker, scheduler, CLI, frontend and the Docker
configuration in this repository — for example authentication/authorization
bypasses, cross-organization data access, credential or key disclosure,
injection into `pg_dump`/`pg_restore`, SSRF, and cryptographic weaknesses.

Out of scope: vulnerabilities in third-party services you connect to DBVault,
issues requiring a compromised host or leaked `ENCRYPTION_KEY`, and
denial-of-service via unrealistic request volumes against self-hosted
instances without rate limits configured.

See [docs/security.md](docs/security.md) for DBVault's security model.
