# Configuration

Workspace Steward reads project-local `workspace.yml` from the canonical root checkout. The [configuration schema](../schema/workspace.schema.json) describes the file structure. Configuration version 1 declares the root trunk, child repositories, dependency order, and lifecycle hooks. State version 5 separately records each Managed Workspace and its custody.

```yaml
version: 1
root:
  trunk: main
  hooks:
    create-after:
      - id: prepare-root
        shell: ./scripts/prepare-workspace.sh
children:
  - name: api
    path: repos/api
    url: git@example.test:product/api.git
    trunk: main
    depends_on: []
```

Repository names are stable configuration identities. Paths are relative to the root. URLs and trunks must match existing canonical child checkouts. `ws bootstrap` clones missing children; it never replaces an existing path.

By default creation selects the root and every child. `--only api,web` selects exactly those children and `--except devtools` selects all other children. The root is always selected. Dependencies must be present in the selection.

## Lifecycle hooks

Supported phases are `create-before`, `create-after`, `merge-before`, `merge-after`, `drop-before`, and `drop-after`. Hook definitions have a stable `id` and one configured runner. Hooks run in dependency order where required and are checkpointed individually for safe retry.

| Phase | Checkout |
| --- | --- |
| `create-before` | Canonical repository checkout |
| `create-after` | Managed Workspace checkout |
| `merge-before` | Managed Workspace checkout |
| `merge-after` | Canonical repository checkout after release |
| `drop-before` | Managed Workspace checkout |
| `drop-after` | Canonical repository checkout after release |

Available environment includes `WS_WORKSPACE_ID`, `WS_WORKSPACE_NAME`, `WS_ROOT`, `WS_ROOT_ORIGIN`, `WS_REPOSITORY_NAME`, `WS_REPOSITORY_PATH`, `WS_REPOSITORY_ORIGIN`, `WS_REPOSITORY_BASE`, and the selected repository inventory.

Hooks own their output: a successful hook must leave its checkout clean. Hooks must be idempotent because a process can stop after an external effect but before its completion checkpoint is persisted.

For an adopted external root, Workspace Steward records the initial root `HEAD` and creation timestamp before hooks. Root `create-before` may advance the canonical trunk; Workspace Steward then fast-forwards the still-clean external root before creating children from their configured trunks.

## Configuration drift

Workspace state records the selected repositories and executable hook definitions. Drift blocks lifecycle mutation until it is inspected and explicitly adopted with `ws recover <workspace-id> --adopt-config`. Repository identities, paths, origins, and trunks cannot be rebound through adoption.

Active state from versions before 5 is mutation-incompatible and must be finished with the previous binary. Completed version 4 records remain read-only.
