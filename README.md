# DBPull

Refresh a local MariaDB database from a remote source database over SSH.

DBPull is a small CLI for developers who want a repeatable way to rebuild a local database from a source database. It opens the SSH tunnel, rebuilds table schema, copies data, and shows progress in one command.

## Features

- Interactive setup with `dbpull init`
- Interactive config editing with `dbpull config`
- Environment checks with `dbpull doctor`
- Dry-run planning with `dbpull plan`
- Full sync with `DROP + CREATE + INSERT`
- Base-table-only sync for MariaDB
- Table filtering with `exclude_tables` and `exclude_data`
- Progress output with tables copied, speed, elapsed time, and ETA
- Sync only selected tables when needed

## Installation

Build locally:

```bash
go build -o dbpull .
```

Install to your Go bin directory:

```bash
go install .
```

Check that the CLI is available:

```bash
dbpull version
```

## Quick Start

Initialize configuration:

```bash
dbpull init
```

Verify SSH access and both databases:

```bash
dbpull doctor
```

Run the synchronization:

```bash
dbpull sync
```

If you want to review the plan first:

```bash
dbpull plan
```

## Configuration

DBPull stores its configuration in `dbpull.yml`.

```yaml
source:
  database: app_source
  username: remote_user
  password: source_password

ssh:
  host: db.example.com
  port: 22
  user: deploy
  private_key: ~/.ssh/id_rsa

target:
  host: 127.0.0.1
  port: 3306
  database: app_local
  username: root
  password: local_password

sync:
  batch_size: 1000
  exclude_tables: []
  exclude_data:
    - audits
    - failed_jobs
    - jobs
    - job_batches
    - telescope_*
    - "*_temps"
```

Use a different config file when needed:

```bash
dbpull --config path/to/dbpull.yml sync
```

## Commands

| Command | Description |
| --- | --- |
| `dbpull init` | Create the initial configuration interactively |
| `dbpull config` | View and edit configuration interactively |
| `dbpull doctor` | Check SSH access, tunnel, and database connectivity |
| `dbpull plan` | Show what would be synchronized without changing the target |
| `dbpull sync [tables...]` | Synchronize all planned tables or only selected tables |
| `dbpull list-tables` | List source base tables |
| `dbpull version` | Print the current version |

## Example

Plan a sync:

```text
$ dbpull plan
Tables to sync       640
Schema only          12
Excluded             19
Target database      olshoperp_local

No changes were made.
```

Run the sync:

```text
$ dbpull sync
DBPull Sync

SSH ✓  Source ✓  Target ✓  Plan ✓  Schema ✓  Data ◌

████████████████████████░░░░░░░░  95%
637 / 671 tables
16,934,393 rows copied · 17,868 rows/s
Elapsed 15m48s · ETA 51s
```

Sync only a few tables:

```bash
dbpull sync users products orders
```

## Documentation

For architecture, design decisions, package responsibilities, lifecycle details, and other implementation notes, see [TDD.md](TDD.md).

`TDD.md` is the authoritative technical document for this project.

## Roadmap

- Better release packaging
- End-to-end integration coverage
- More polish around config and sync UX

## Contributing

Issues and pull requests are welcome.

Before opening a change, please:

1. Read [TDD.md](TDD.md) for the current technical design.
2. Keep changes simple and aligned with the current scope.
3. Run tests and basic Go checks before submitting.

## License

No license file is included yet.
