# DBPull

> Refresh your local database from a remote database over SSH with one command.

DBPull synchronizes a remote database to a local database by rebuilding the target schema and copying data. It is designed for development environments where local data needs to stay in sync with staging or other remote databases.

**Tested with:** MariaDB, MySQL

## Features

- SSH tunnel support
- Interactive configuration
- Schema + data synchronization
- Table and data exclusion
- Sync preview (`plan`)
- Connection diagnostics (`doctor`)
- Cross-platform builds

## Installation

Download the latest binary from **GitHub Releases**, or build from source:

```bash
make build
```

## Quick Start

```bash
dbpull init
dbpull doctor
dbpull plan
dbpull sync
```

## Configuration

Configuration is stored in `dbpull.yml`.

```yaml
source:
  ...

ssh:
  ...

target:
  ...

sync:
  batch_size: 1000
  exclude_tables:
    - cache

  exclude_data:
    - audits
```

- `exclude_tables` skips the table completely.
- `exclude_data` keeps the schema but skips copying rows.

## Commands

| Command | Description |
|---------|-------------|
| `init` | Create configuration |
| `config` | Edit configuration |
| `doctor` | Check connections |
| `plan` | Preview synchronization |
| `sync` | Synchronize database |
| `list-tables` | List source tables |
| `version` | Show version |

## Build

```bash
make build
make build-all
make release
```

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.