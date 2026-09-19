# dbvault CLI

A small, dependency-free binary that talks to a DBVault server's REST API.

```bash
go build -o dbvault .            # or download a release binary
./dbvault init                   # server URL + email/password (creates an API token)
./dbvault backup production      # back up now, with a live progress bar
```

Commands: `init`, `status`, `version`, `database list|add|remove|test`, `backup [db]`,
`backup list`, `backup verify <id>`, `restore <id> --new-database <name> | --existing`,
`storage list`, `schedule list`. Every command accepts `--json`.

Configuration lives in `~/.config/dbvault/config.json` (mode 0600). For CI, set
`DBVAULT_SERVER`, `DBVAULT_TOKEN` (create one under *Settings → Security → API tokens*) and
optionally `DBVAULT_ORG`.
