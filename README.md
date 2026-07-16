# DBPull

> Refresh your local MariaDB database from a remote source database over SSH with one command.

## Overview

DBPull is a Go CLI for developers who need a fast way to rebuild a local MariaDB database from a remote source database. It handles the SSH tunnel, recreates table schema, copies data, and shows progress in a single workflow.

## Features

- Connects to a remote source MariaDB database through SSH
- Rebuilds target tables with `DROP + CREATE + INSERT`
- Synchronizes base tables only
- Supports full table exclusion with `exclude_tables`
- Supports schema-only tables with `exclude_data`
- Interactive configuration with `dbpull init`
- Interactive config editing with `dbpull config`
- Environment checks with `dbpull doctor`
- Dry-run planning with `dbpull plan`
- Single-binary builds for Linux, macOS, and Windows

## Why DBPull?

Refreshing a local database often looks like this:

- open an SSH tunnel
- export the source database
- move the dump around
- import it locally
- repeat when the data changes

DBPull reduces that to a small, repeatable CLI workflow:

- configure once
- verify access
- preview the plan
- run the sync

## How It Works

```text
Remote Source MariaDB
        │
        ▼
     SSH Tunnel
        │
        ▼
      DBPull
        │
        ▼
 Local Target MariaDB
```

DBPull reads schema and data from the source database, then rebuilds the configured target database locally.

## Installation

For published versions, GitHub Releases should be the preferred installation method.

### Linux

Download the matching release artifact for your CPU architecture, extract it, and place `dbpull` somewhere in your `PATH`.

### macOS

Download the matching release artifact for your CPU architecture, extract it, and place `dbpull` somewhere in your `PATH`.

### Windows

Download the Windows release archive, extract it, and place `dbpull.exe` in a directory on your `PATH`.

### Build From Source

Build the current platform:

```bash
make build
```

Or build directly with Go:

```bash
CGO_ENABLED=0 go build -trimpath -buildvcs=false -o dbpull .
```

Check the installed binary:

```bash
dbpull version
```

## Quick Start

Initialize configuration:

```bash
dbpull init
```

Verify SSH access and database connectivity:

```bash
dbpull doctor
```

Preview what will be synchronized:

```bash
dbpull plan
```

Run the synchronization:

```bash
dbpull sync
```

Synchronize only selected tables when needed:

```bash
dbpull sync users products orders
```

## Configuration

DBPull reads configuration from `dbpull.yml`.

The file contains four sections:

- `source`: the remote source database to read from
- `ssh`: the SSH connection used to reach the source database
- `target`: the local target database to rebuild
- `sync`: batch size and table filtering rules

### Table filtering

- `exclude_tables`: skip matching tables completely
- `exclude_data`: recreate schema for matching tables, but do not copy rows

### Example

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
  exclude_tables:
    - cache_snapshots
  exclude_data:
    - audits
    - failed_jobs
    - jobs
    - job_batches
    - telescope_*
    - "*_temps"
```

Use a non-default config path if needed:

```bash
dbpull --config path/to/dbpull.yml sync
```

## Commands

| Command | Description |
| --- | --- |
| `dbpull init` | Create an initial configuration interactively |
| `dbpull config` | View and edit configuration interactively |
| `dbpull doctor` | Check SSH access, tunnel, and database connectivity |
| `dbpull plan` | Show what would be synchronized without changing the target |
| `dbpull sync [tables...]` | Synchronize all planned tables or only selected tables |
| `dbpull list-tables` | List source base tables |
| `dbpull version` | Print build metadata for the current binary |

## Build

Build the current platform:

```bash
make build
```

Build all supported binaries:

```bash
make build-all
```

Run the local release pipeline:

```bash
make release
```

This creates versioned binaries, release archives, and checksums in `dist/`.

## Versioning

Check the current binary metadata:

```bash
dbpull version
```

Release builds are designed to inject version, commit, and build date through linker flags. Published releases should follow Semantic Versioning tags such as `v0.1.0`.

## Safety

DBPull rebuilds the configured target database.

- Never point the target configuration at production.
- The source database is treated as read-only.
- The target database is dropped and recreated table by table.
- Credentials are stored in `dbpull.yml`.
- Protect that file appropriately and avoid sharing it carelessly.

If you want to inspect the plan before changing anything, run:

```bash
dbpull plan
```

## Platform Support

| Component | Status |
| --- | --- |
| Linux amd64 | Supported by current build pipeline |
| Linux arm64 | Supported by current build pipeline |
| macOS amd64 | Supported by current build pipeline |
| macOS arm64 | Supported by current build pipeline |
| Windows amd64 | Supported by current build pipeline |
| MariaDB source | Implemented |
| MariaDB target | Implemented |
| MySQL | Not documented as supported |

## Roadmap

- Publish GitHub Releases
- Improve end-to-end release documentation
- Broaden compatibility guidance after more verification

## Contributing

Issues and pull requests are welcome.

Please keep changes small, testable, and consistent with the current scope of the project.

For implementation details, see [TDD.md](TDD.md).

## License

No license file is included yet.
