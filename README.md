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

Install the latest release:

```bash
curl -fsSL https://faisallbhr.github.io/dbpull/install.sh | sh
```

You can also download a binary from **GitHub Releases**, or build from source:

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

## Basic Configuration

Configuration is stored in the OS config directory by default:

- Linux: `~/.config/dbpull/dbpull.yml`
- macOS: `~/Library/Application Support/dbpull/dbpull.yml`
- Windows: `%AppData%\dbpull\dbpull.yml`

Use `--config /path/to/dbpull.yml` when you need a different file.

```yaml
source:
  ...

ssh:
  ...

target:
  ...

sync:
  exclude_tables:
    - cache
    - tmp_*
    - *_logs

  exclude_data:
    - audits
    - sessions_*
    - *_histories
```

- `exclude_tables` skips the table completely.
- `exclude_data` keeps the schema but skips copying rows.

## Advanced Performance Configuration

Most users do not need to change these values. If needed, add them manually under `sync`:

```yaml
sync:
  # Optional advanced settings
  batch_size: 10000
  workers: 2
  transaction_batches: 20
  max_batch_bytes: 16777216
```

- `batch_size`: max rows per insert batch before other limits. Default: `10000`.
- `workers`: number of tables copied in parallel. Default: `2`. Use `1` for sequential data sync.
- `transaction_batches`: commit after this many insert batches. Default: `20`.
- `max_batch_bytes`: estimated max data size per batch before flushing. Default: `16777216` (16 MiB).
- Peak batch memory is roughly `workers x max_batch_bytes`, plus row and query overhead.
- More workers can improve full sync speed, but also uses more source/target connections and memory.

Recommended starting point:

```yaml
sync:
  batch_size: 10000
  workers: 2
  max_batch_bytes: 16777216
  transaction_batches: 20
```

Schema sync remains sequential to avoid unnecessary foreign key, metadata lock, and DDL recovery risk. Data sync uses the worker setting.

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
