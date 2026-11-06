# Diagrams

Owner: **Khushi** (PR K4). Mermaid source plus exported PNG, kept in sync with `docs/SPEC.md`, the code, and the team Miro board (https://miro.com/app/board/uXjVERbFfok=/).

Planned files:
- `architecture.mmd` / `architecture.png`: all components (client API, write path, in-memory index, log writer, commit-marker logic, recovery routine, compaction routine, disk-full guard) and the global RWMutex. Embedded in ARCHITECTURE.md.
- `file-layout.mmd` / `file-layout.png`: 24-byte header and record layout with byte offsets. Used in README.md.
- `write-path.mmd` / `write-path.png`: the numbered write order from SPEC section 4.
- `recovery.mmd` / `recovery.png`: the recovery flow from SPEC section 6.
- `compaction.mmd` / `compaction.png`: temp file, fsync, rename, directory fsync, handle swap.
