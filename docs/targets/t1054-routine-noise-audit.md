# T1054 — owner-visible report ingress audit (implementation handoff)

The suppression decision is at `mcpserver.Server.notifyTurn`, **after** durable `storeAgentReport` and `notifyParentReport`, and **before** the duplicate overseer copy reaches `deliverByName`. The predicate accepts only a canonical, strictly validated, payload-free `finish-report` or `scout-report` from a registry `work` seat; no decisions, fog notes, unexpected slots, blocker, or prose. A finish also requires completion status, GREEN, and a syntactically bounded oracle; `FalseGreenFlagsForReport` must be empty. The parent must actually accept delivery. A duplicate-report refusal does not qualify as parent acceptance. A parent failure or overseer parent falls through to the owner path.

Other owner-visible ingress paths, intentionally **not** suppressed:

| Path | Reason to preserve |
| --- | --- |
| `mcpserver.deliverByNameAs` / `deliverToOverseer` (including T392.7 report reroute and MCP `jevons_agent_send`) | No trustworthy record that a parent received this specific direct send; rerouted finish reports require independent gate attention. |
| `server.DeliverToOverseerAs` → `sendToOverseerAsAgent` / `sendToOverseerAsOwner` | Single queue/journal seam for agent, owner and daemon messages; filtering here would erase explicit repeated owner sends and direct escalations. |
| `server.NotifyOwner` / `NotifyOwnerNote` | Explicit notices to the owner, outside worker terminal reporting. |
| `mcpserver` sentinel, staff ops, worker-idle, replay and frontier notices | Typed operational notices, not a duplicate parent-delivered worker completion. |
| Agent output emitted by the overseer itself | It is the owner's conversation, not a worker completion; `[silent]` remains the explicit output filter. |

Precision study: `TestT1054TwentyRealReportSpecimensFailOpen` reads twenty previously committed worker-report specimens (19 `internal/mcpserver/testdata` and one `internal/relayroute/testdata`), each labeled owner-visible. **FP=0/20**. None is a pure metadata routine positive: **FN not measurable** in the real-message sample, not zero recall errors claimed. The separate 20-case adversarial/synthetic corpus has two metadata-positive controls and eighteen negative cases (including arbitrary unforeseen safety prose, owner question, blocked status, duplicate/overwritten slots). The actual stored-report pool was inspected for pure metadata routine candidates; observed samples carry payload/unknown fields or in-progress/blocked/decision slots. This is a deliberate precision-over-recall tradeoff: free-form text cannot safely be recognized as 'routine' from GREEN, SHA or familiar vocabulary alone. A hypothetical novel safety phrase must remain visible without first being anticipated in a keyword list.

`TestT1054OwnerRepeatedSendNeverSuppressed` and `TestT1054DirectOverseerSendNeverUsesRoutineSuppression` guard the separate owner/direct-send paths. **No authentication change** is involved. Integration, daemon activation and a live owner-chat probe are outstanding; this audit does not assert product achievement.
