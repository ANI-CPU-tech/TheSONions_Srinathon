# PROMPTS

Every prompt the team sent to any AI tool during the hackathon, unedited, with tool name and local time (IST). Responses are not included, per the rules.

The log lives in [`prompts/`](prompts/), one append-only file per source, each in chronological order:

| File | Whose prompts |
|---|---|
| [`prompts/planning-claude-ai.md`](prompts/planning-claude-ai.md) | Planning on claude.ai before coding, then the Claude Code bootstrap session (integrator) |
| [`prompts/anirudh.md`](prompts/anirudh.md) | Anirudh (engine) |
| [`prompts/khushi.md`](prompts/khushi.md) | Khushi (CLI, serve, docs) |
| [`prompts/likith.md`](prompts/likith.md) | Likith (torture tool) |

Rules:
- Claude Code appends each prompt automatically at the start of handling it (see CLAUDE.md).
- Prompts sent to any other tool (claude.ai, ChatGPT, Copilot, Cursor, ...) must be pasted into the sender's file by hand, with the tool name and time.
- Never rewrite or delete an entry. A falsified log disqualifies the team; an incomplete one costs 10 points.

For the final submission (PR B1, hour 22) the files are merged into one chronological list, possibly with `scripts/merge-prompts.sh`. The per-person files stay as the source.
