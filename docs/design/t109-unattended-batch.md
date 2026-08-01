# 🎯T109 unattended safe batch

Parent target: **Unattended safe batch of frontier leaves** can be completed
by agents **without owner decision or validation gates**.

## Membership (`depends_on`)

| ID | Leaf |
|----|------|
| T102 | Journey step library in Go |
| T33 | Message-integrity on thread status |
| T64 | Activity-strip tool arg summary |
| T69 | Composer height / scrollbar flicker |
| T70 / T70.1 | Composer vs latest reply layout |
| T73 | Remove obsolete TMUX/detail panel |
| T74 | Syntax highlighting for fenced code |
| T75 | Owner quote notation |
| T80 | Dictation/bulk paste tidy |
| T84 | GitHub icon for compacted workdirs |
| T88 | History nav + primary redo |
| T91 | Timestamp tooltip |
| T107 | Journeys = live agent E2E; cache OK |

## Exclusion rule

Anything that needs **decide / ratify / second machine / needs-owner /
device lab / class-3 human gate** stays **out** of this batch (e.g. T37,
T27.1 design accept, T47 second-user drill, T31/T98/T96 doctrine ratify,
T67 enter-in-list, T83 viz placement, T29 generative UI, T14.1/T7 mobile).

## Oracles

Package + hermetic UI (`scripts/chat-ui-test/batch-t109-test.js` and peers)
and Universe-B `make test-journey` for journey leaves. Parent 🎯T109 is
achieved only when every leaf is `achieved` in bullseye.
