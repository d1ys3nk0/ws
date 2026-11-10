# Workspace Steward

Workspace Steward manages one workspace across a root Git repository and any configured child repositories. A Managed Workspace has two distinct values:

- Workspace ID: an opaque `ws-<32 lowercase hex>` identifier used by Workspace Steward commands and persisted state.
- Workspace Name: the Git branch shared by every repository in the workspace. It is not an identity or command selector.

Workspace Steward keeps project policy in lifecycle hooks and Git history in Git.

## Install

```sh
curl -fsSL https://github.com/d1ys3nk0/ws/releases/latest/download/install.sh | sh
```

Release binaries support macOS and Linux on amd64 and arm64. The installer resolves one release, verifies its archive against that release's SHA-256 checksums, and installs into `/usr/local/bin`. To select a release or destination, download and inspect the installer, then run `WS_VERSION=v0.0.1 WS_INSTALL_DIR="$HOME/.local/bin" sh install.sh`.

Initialize the canonical root checkout and declare children in `workspace.yml`:

```sh
ws init
ws bootstrap
```

See [configuration](docs/configuration.md) for the complete schema and hook contract.

## Moving from VCM

Finish or abandon active workspaces with VCM and remove its harness integrations before enabling the Workspace Steward integrations. Rename `vcm.yml` to `workspace.yml` and update lifecycle hook scripts from `VCM_*` to `WS_*`. Workspace Steward stores new workspace state in its own Git directory namespace and does not import VCM state.

## Create a workspace

Manual creation keeps Workspace Steward in custody of the root worktree and its branch:

```sh
ws create improve-search
```

A harness can create the root linked worktree and then ask Workspace Steward to adopt it:

```sh
ws create --existing-root /path/to/linked-root
```

The existing root must be clean, registered, non-canonical, from the same Git common directory, and exactly at the configured root trunk. An attached root branch is reused and remains harness-owned. For a detached root, an explicit name is used when supplied; otherwise Workspace Steward generates `YYMMDDHHMMSS-<12-character-root-HEAD>` in UTC. Workspace Steward records custody before attaching that generated branch so an interrupted attempt can be resumed safely.

Workspace Steward creates each selected child worktree on the same Workspace Name and runs lifecycle hooks only after the required checkpoints are durable. `--only` selects exact comma-separated children; `--except` excludes them. Dependencies must be selected explicitly.

## Harness integration

Install merge-safe project-local integration with:

```sh
ws integrate codex
ws integrate claude
ws integrate opencode
```

Use `--dry-run` to preview and `--remove` to remove the exact Workspace Steward-managed fragment. Repeated installation and removal are idempotent. Unrelated JSON settings are preserved, and Workspace Steward refuses to overwrite a modified managed fragment.

- Codex installs `.codex/hooks.json` with a `SessionStart` hook for `startup|resume`. Project hooks must be reviewed and trusted through `/hooks`.
- Claude installs `.claude/settings.json` with a `SessionStart` hook and leaves `WorktreeCreate` and `WorktreeRemove` untouched.
- OpenCode installs `.opencode/plugins/ws.ts` and handles `worktree.ready` through the supported plugin `worktree` context.

Adapters ignore the canonical checkout and resume an already managed path. Setup failures are surfaced to the harness with the fallback command `ws create <name> --existing-root <cwd>`.

## Selectors and lifecycle

Commands select a version 5 Managed Workspace only by its exact Workspace ID, its exact path (or a path inside it), or the current directory when inside it. Workspace Names are never selectors.

```sh
ws status ws-0123456789abcdef0123456789abcdef
ws diff /path/to/workspace
ws merge ws-0123456789abcdef0123456789abcdef
```

| Command | Purpose |
| --- | --- |
| `list [--all]` | List active Managed Workspaces; include completed records with `--all` |
| `status [workspace]` | Inspect trees, divergence, lifecycle checkpoints, and hook failures |
| `add [workspace] --only api,web` | Add selected child repositories |
| `diff [workspace]` | Compare recorded baselines with tracked workspace contents |
| `refresh [workspace]` | Merge local trunks into workspace branches |
| `publish [workspace]` | Publish frozen workspace revisions, children before root |
| `merge [workspace]` | Gate and squash into local trunks, then release resources |
| `cleanup [workspace]` | Remove a retained integrated workspace |
| `drop [workspace]` | Release an unneeded workspace; `--force` preserves recovery evidence |
| `path [workspace]` | Print the selected checkout path |
| `switch [workspace]` | Enter the selected checkout through shell integration |

Merge defaults to `chore(ws): integrate workspace`. `--message` accepts a single-line Conventional Commit subject. Each changed repository receives the chosen subject and a `Commits:` inventory of source commits not reachable from its recorded target.

For an externally owned root, merge, drop, and retained cleanup remove Workspace Steward-owned children but never delete the root directory. Workspace Steward detaches and deletes a root branch only when Workspace Steward created that branch. A harness-owned branch is released without deletion. Removing the external root before Workspace Steward releases it is a recoverable error; removing it after release is valid.

## Hook environment

Lifecycle hooks receive workspace-oriented context including `WS_WORKSPACE_ID`, `WS_WORKSPACE_NAME`, `WS_ROOT`, `WS_ROOT_ORIGIN`, `WS_REPOSITORY_NAME`, `WS_REPOSITORY_PATH`, and `WS_REPOSITORY_BASE`.

Hooks must be idempotent, leave their checkout clean when successful, and own staging and committing their outputs. See [configuration](docs/configuration.md) and [recovery](docs/recovery.md).

## Safety and development

Workspace Steward records checkout custody and branch custody independently. Audit and prune authorize registered external roots and externally owned branches but never offer to delete them. Destructive cleanup verifies ownership, path, branch, revision, worktree cleanliness, and nested repositories before mutation.

Human results use stdout and progress/errors use stderr. `--json` emits stable command-specific objects. Use `--dry-run` before destructive or remote operations.

```sh
task verify
```
