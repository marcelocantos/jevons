# web/ — deprecated reference cockpit

**Do not land product UI work here.** The active cockpit is `ui/`
(Vite + React, 🎯T540). This tree stays as the behavioural and visual
oracle while React reaches parity (🎯T540.1). Daily `:13705` may still
serve `index.html` until owner cutover (🎯T505 / 🎯T540.2) — that is
not licence to grow vanilla.

Allowed touches: reference reads, parity oracles, and rare mechanical
fixes that keep the frozen surface bootable (embed list, broken script
refs). New features, plan-ticker polish, chat UX, and fleet chrome go
in `ui/`.

See `AGENTS.md` § Cockpit UI and `ui/README.md`.
