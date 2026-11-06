# Diagrams

Owner: **Khushi** (PR K4). Mermaid source plus exported PNG, kept in sync with
`docs/SPEC.md` and the code. (The team Miro board could not be opened from
this machine — no session — so sync against it is recorded as *not verified*
in `progress/khushi.md`; the SPEC and the code were checked line by line.)

| File | Shows | Source of truth |
|---|---|---|
| `architecture.mmd` / `.png` | Every component: CLI, serve (goroutine per writer, ACK-after-fsync), torture, the **global `sync.RWMutex`**, write path, disk-full guard, in-memory index, log file, recovery, compaction, read path | SPEC §1, §4–§7 |
| `file-layout.mmd` / `.png` | 24-byte header and record layout with byte offsets, the three payload formats, the CRC commit rule | SPEC §3 |
| `write-path.mmd` / `.png` | The numbered write order 1–7 with the disk-full guard, the failure rule (truncate → `ErrFailed`) and `// CRASH:` notes | SPEC §4, `src/engine/write.go` |
| `recovery.mmd` / `.png` | The recovery flow step by step: leftover cleanup, header check, scan loop, torn-tail truncation (idempotent), `nextSeq` | SPEC §6, `src/engine/recover.go` |
| `compaction.mmd` / `.png` | Temp file → fsync → rename → directory fsync → handle swap, the E4 abort, the triggers, crash outcomes C3/C4/C5 | SPEC §7, `src/engine/compact.go` |

## Editing a diagram

Edit the `.mmd`, then re-export so the PNG never drifts from the source:

```sh
npx -y @mermaid-js/mermaid-cli@latest -i diagrams/<name>.mmd -o diagrams/<name>.png -b white
```

(First run downloads the tool and its headless Chromium; afterwards it is
offline. In a sandbox add `-p` with a puppeteer config containing
`{"args":["--no-sandbox"]}`.) Both the `.mmd` and the `.png` are committed
together — never commit one without the other.
