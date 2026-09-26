# Meta-postmortem: diagnosing the 2026-09-26 repeat storm

The incident itself is [2026-09-26-omp-repeat-storm.md](2026-09-26-omp-repeat-storm.md). This note is about the diagnosis. The question at the end was "is Jevons prompting this seat, or reseating it, or is the harness replaying text?" A turn trace would have answered that in one query. What follows is the work the missing fields forced.

## What was hard

**The handoff pointed at the wrong corpus and the wrong token.** The note listed grok-home `chat_history.jsonl`, `updates.jsonl`, `events.jsonl`, and `~/.omp/`, and it spelled the leaked token `<|eolos|>`. The "I'll inspect" text is only in the sidecar spool, `~/.jevons/spool/events-2026-09-26.log`. The grok-home files for the live session contain older T219 boilerplate and none of those sentences. `~/.omp/` was empty. The bytes of the token are `<|eos|>`. A screenshot reading and the handoff both inserted an extra syllable. The first searches missed.

**Every turn dumps the whole agent, so search counts replays.** A `turn_end` line is the agent state: system prompt, tools, and every message so far. On 25 September that line grew from 8KB to 381KB, and the day's spool is 113MB. Searching for `T219` returned 120 `turn_end` lines, all seat `jevons`, spread across the afternoon, which looks like a storm. It was one user message riding inside every later snapshot. The real growth was 2 messages to 224. Those two facts are different, and the log does not distinguish them. There is no turn id, so a hit is not "said once" or "said again."

**The thing that started a bubble is not a log line.** The spool records assistant deltas and, at the end, that snapshot. Restart nudge, sentinel, forwarded worker, and impatience close are visible only by opening the snapshot and reading the user messages backwards. Those messages have no source enum. Joining them to a `turn_end` timestamp was manual: print the user texts from the last snapshot, then line them up with the assistant turns. `Send` on the Jevons side and `turn_end` on the sidecar side do not share an id.

**Resume and prompt are different logs.** The broker says `seat resumed how=launched`. The nudge text inside the agent says `host restarted at 11:37:34` and again at `11:37:59`. The spool `ready` events stop after that, which is how a later bubble is known to be a prompt rather than another launch. None of those three facts is a single event. The 12:20 daemon bounce, which did not relaunch, is a third phrasing again (`consumer connection closed; seat kept running`).

**Delta, snapshot, and many turns look the same in the UI.** The original handoff already had the right table, and nothing emits the row. Classifying the wall meant grouping 1,063 text events between two `turn_end`s, measuring that none of them starts with the previous event's full text, and counting sentences. `chatWireFromSpool` then emits each of those deltas as its own assistant line, so a wire reader sees words while the cockpit shows one bubble. You have to know which consumer you are staring at.

**`T219` is in too many files to be a query.** A walk of `~/.jevons` and `~/.local/state/claudia` returned hundreds of files containing that string, because the sentinel notice is boilerplate. The useful question was "which user turns did this seat receive after 11:37, and did any of them call a tool?" The logs can answer it only after a script.

**The owner could not point at the top of the thread.** Follow-tail kept the view on the newest bubble while new prompts were still arriving, so the screenshots are the bottom of the incident (the T65 loop) and not the T219 history above it. Disk has the history. The handoff therefore started from the bottom, which is the loop, and the older sentinel copies were a search problem rather than a picture.

## What a turn trace would have shown

One record per prompt, written when the seat accepts it and closed when the turn ends. Search hits this record. It does not hit a 381KB replay of the conversation.

- `turn_id`, `seat`, `session_id`
- `cause`: `owner`, `restart-nudge`, `sentinel`, `impatience`, `agent-forward`, `rsi`, `capacity`, `steer`
- one line of `cause_detail` (which agent, which incident), not the transcript
- `started_at`, `ended_at`
- `stop`: `end_turn`, `abort`, `error`, `stop_token`
- `tool_calls`, `deltas`, `chars`
- `stop_token` if one appeared in the text
- `resume`: absent, or `adopted` / `launched` / `reminted`, and whether a nudge was sent

The 26 September sequence is then:

- two rows, `cause=restart-nudge`, `resume=launched`, same `session_id`, 25 seconds apart
- a run of rows, `cause` in {`sentinel`, `agent-forward`, `impatience`}, `tool_calls=0`, `chars` around 150–220
- one row, `chars=4314`, `deltas=1063`, `tool_calls=0`

That is the whole postmortem. The double launch, the re-prompt loop, and the single degenerate generation are three different rows. Today they are one undifferentiated bubble stream plus a snapshot you have to unpack.

Two companion rules, or the trace drowns the same way the spool did:

- Stop putting the full message list in the line a search walks. A digest (`message_count`, last cause, last `chars`) is the searchable event. The snapshot can stay available for resume, off to the side.
- A stop token is a field on the turn, and it is not appended to the owner-visible text. `sidecar/seat.ts` currently forwards every `text_delta`.

The delta log was already enough to clear the harness, once the right file was found. The missing data is who prompted the seat and whether that turn called a tool. That trace is 🎯T870. The behaviour it would have named is 🎯T869.
