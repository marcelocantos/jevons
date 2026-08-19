// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcelocantos/jevons/internal/relayroute"
)

// 🎯T309.3: ONE deliver-by-name path for every agent in the fleet layer.
//
// deliverByName is the single implementation every message-to-an-agent
// eventually reaches, whichever door the caller came through:
//
//	MCP    jevons_agent_send            → sendToAgentAs(actor) → deliverByNameAs
//	HTTP   POST /api/agents/{name}/send → DeliverAgentMessageAs → deliverByName
//	fleet  worker reply notify, worker-idle, daemon-restarted, fleet health
//	                                     → notify/emit* → deliverByName (owner surface)
//
// Before this slice the fleet layer had a privileged overseer-only wire:
// everything addressed to the overseer went through notifyJevon (a bare
// func(text) injection set from main), while everything addressed to a
// worker went through sendToAgent. Two consequences, both bugs:
//
//   - jevons_agent_send addressed to the overseer resolved through the
//     registry and called proc.Send directly, bypassing the owner chat
//     journal, the owner-visible broadcast, and the queue-on-busy retry.
//     A PO reporting up by name could land in the overseer's session with
//     no owner-visible record — or hit "prompt already in flight" and be
//     dropped, the 🎯T62 failure this daemon has fixed once already.
//   - Which peers an agent could reach depended on which API it could
//     call, not on the fleet hierarchy. Hierarchy is lineage, not API
//     surface: the same call by name serves worker→PO, PO→overseer, and
//     owner→any-agent.
//
// The overseer is now simply the arm of deliverByName that resolves to the
// owner-chat delivery seam (SetOverseerDeliver → server.DeliverToOverseerAs),
// which owns journalling, owner bubbles, and the notify queue. No caller in
// this package sends to the overseer any other way.
//
// SILENT DROPS ARE ERRORS (acceptance 3). Every arm returns an error the
// caller can surface: an unregistered peer, an unwired overseer seam, and a
// failed overseer delivery all come back as errors; a busy peer comes back
// as a "queued" result, never a discarded message.

// SendOrigin marks who is speaking on a deliver-by-name call. It changes
// framing only at the overseer arm (🎯T309.2): an owner turn carries the
// owner marker and paints an owner bubble, an agent turn is an injected
// notification. Fleet agents receive the text either way — their provider
// session has no owner/agent distinction to make.
type SendOrigin string

const (
	// OriginOwner is the owner speaking (owner bubble + owner marker).
	OriginOwner SendOrigin = "owner"
	// OriginAgent is an agent or system notification (no owner bubble).
	OriginAgent SendOrigin = "agent"
)

// OverseerDeliverFunc delivers text to the overseer with origin semantics.
// Wired from main to server.DeliverToOverseerAs so the overseer arm of
// deliverByName reuses the owner-chat journal + notify queue rather than
// re-implementing them.
type OverseerDeliverFunc func(text string, origin SendOrigin) error

// SetOverseerDeliver wires the overseer arm of the single deliver path.
// Without it, an overseer-addressed send falls back to the legacy notify
// injection for agent-origin text and is a loud error for owner-origin text
// (never a silent misdelivery).
func (s *Server) SetOverseerDeliver(fn OverseerDeliverFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overseerDeliver = fn
}

// senderResolver returns a live sender for a fleet agent, rehydrating a
// registered-but-stopped one. The seam lets hermetic tests drive the whole
// deliver path without launching provider processes (same pattern as
// server.notifySender). Nil — the product path — resolves via the registry.
type senderResolver func(name string) (proc agentSender, rehydrated bool, err error)

// SetSenderResolver overrides fleet-agent process resolution. Test seam.
func (s *Server) SetSenderResolver(fn senderResolver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolveSender = fn
}

// sendConfirmation says WHO decides whether a send became a turn (🎯T416).
//
// Two confirmations on one send is not twice as safe, it is a contradiction
// waiting to fire: the spawn path opened its own watch before the send
// (🎯T387) and reads the returned status, so if the send path also judged, the
// spawn would see a status its own predicate does not recognise and fail a
// healthy brief. Whoever opened the watch owns the verdict.
type sendConfirmation int

const (
	// confirmHere: deliverToSender judges, against this payload.
	confirmHere sendConfirmation = iota
	// confirmByCaller: the caller opened a watch before calling and will
	// judge the outcome itself. Only the spawn path does this, and only
	// because its evidence question is the different one — a fresh seat has
	// an empty composer, so "did the agent stir" cannot be confused with an
	// earlier message finally being submitted.
	confirmByCaller
)

// deliverByName is THE agent-addressed delivery op of the fleet layer:
// one call reaches ANY agent by name — worker, PO, or overseer — with
// hierarchy carried by lineage rather than by which API the caller holds.
// Delivery from the owner surface (and daemon-internal system notes); an
// in-fleet sender uses deliverByNameAs so its lineage is on the record.
func (s *Server) deliverByName(name, text string, origin SendOrigin, interrupt bool) (agentSendResult, error) {
	return s.deliverByNameAs(ActorOwnerSurface, name, text, origin, interrupt)
}

// deliverByNameConfirmedByCaller is deliverByName for a caller that has
// already opened its own turn watch — the spawn path, and nothing else.
func (s *Server) deliverByNameConfirmedByCaller(name, text string, origin SendOrigin, interrupt bool) (agentSendResult, error) {
	return s.deliverByNameWith(ActorOwnerSurface, name, text, origin, interrupt, confirmByCaller)
}

// deliverByNameAs is the same path with the sender named, so authorization is
// decided by lineage/policy here (🎯T309.3 acceptance 2) rather than by which
// API the caller could reach. See deliver_policy.go for the rules.
func (s *Server) deliverByNameAs(actor, name, text string, origin SendOrigin, interrupt bool) (agentSendResult, error) {
	return s.deliverByNameWith(actor, name, text, origin, interrupt, confirmHere)
}

// deliverByNameWith is deliverByNameAs with the confirmation owner named.
func (s *Server) deliverByNameWith(actor, name, text string, origin SendOrigin, interrupt bool, confirm sendConfirmation) (agentSendResult, error) {
	name = strings.TrimSpace(name)
	actor = strings.TrimSpace(actor)
	if name == "" || strings.TrimSpace(text) == "" {
		return agentSendResult{}, fmt.Errorf("name and text are required")
	}

	rel, err := AuthorizeDeliver(s.registry, actor, name, origin, s.isOverseerAgent)
	if err != nil {
		slog.Warn("agent_send",
			"component", "agent_send",
			"name", name,
			"actor", actor,
			"relation", string(rel),
			"origin", string(origin),
			"status", "denied",
			"err", err.Error(),
		)
		return agentSendResult{}, err
	}

	// 🎯T452: the destination this call actually resolved to, which is not
	// always the name it was addressed by — the overseer arm answers for every
	// name that resolves to the owner-chat seat, and that resolution consults a
	// session id. Everything below delivers into `dest`, so `dest` is what the
	// payload's own identity header has to agree with.
	overseerArm := s.isOverseerAgent(name)
	dest := name
	if overseerArm {
		dest = s.overseerSeatName()
	}
	// A message whose header names someone else is not a message to retry. It
	// would hand this seat another agent's name, parent and target, in the
	// second person, through the one instrument 🎯T425 told every agent to
	// trust without checking. Refusing is loud and delivers nothing.
	if err := CheckBriefAddressing(dest, text); err != nil {
		slog.Error("🎯T452 refused a misaddressed brief",
			"component", "agent_send",
			"addressed_to", IdentityHeaderName(text),
			"addressed_by_caller", name,
			"destination", dest,
			"overseer_arm", overseerArm,
			"actor", actor,
			"origin", string(origin),
		)
		return agentSendResult{}, err
	}

	// The first MCP send can carry a daemon-authored identity + standing-brief
	// envelope. Routing and its summary belong to the sender's report, not to
	// doctrine that happens to contain phrases such as "needs-owner" / "oracle".
	report := relayReportBody(text)

	// 🎯T392.7: a named worker report that needs no product judgement skips the
	// PO hop. The PO still gets a one-line record. Owner directs and daemon-
	// composed owner-surface traffic (empty actor) are never rerouted — those
	// are not worker reports, and their identity doctrine alone can trip the
	// keyword classifier (🎯T515).
	who := strings.TrimSpace(actor)
	if origin == OriginAgent && !overseerArm && isPOName(dest) &&
		who != "" && who != ActorOwnerSurface &&
		relayroute.Classify(report) == relayroute.RouteOverseer {
		po := dest
		reason := relayroute.Reason(report)
		summary := relayroute.ReportSummary(report)
		s.LogEvent("relayroute", relayroute.RouteOverseer.String(), map[string]any{
			"msg":         fmt.Sprintf("worker report rerouted to overseer: %s (%s): %s", who, reason, summary),
			"worker":      who,
			"po":          po,
			"route_class": relayroute.RouteOverseer.String(),
			"reason":      reason,
			"summary":     summary,
		})
		record := relayroute.RecordLine(who, reason, summary)
		if _, err := s.deliverByName(po, record, OriginAgent, false); err != nil {
			slog.Info("T392.7 PO record undelivered", "po", po, "err", err)
		}
		return s.deliverToOverseer(s.overseerSeatName(), text, origin)
	}

	if overseerArm {
		return s.deliverToOverseer(name, text, origin)
	}

	// 🎯T401: an auto-reaped name is still a reachable address. Detect it
	// before ensureAgentProcess's bare "not running", hold the payload in
	// sendq, and name the recovery call. Never-registered stays not-found.
	if s.registry == nil || s.registry.Def(name) == nil {
		if rec, ok := LookupReapedRecord(s.fleetIntent(), name); ok {
			return s.holdSendForReaped(name, text, rec), nil
		}
	}

	s.mu.Lock()
	resolve := s.resolveSender
	s.mu.Unlock()

	if resolve == nil {
		if s.registry == nil {
			return agentSendResult{}, fmt.Errorf("agent registry not available")
		}
		resolve = func(n string) (agentSender, bool, error) {
			proc, rehydrated, err := s.ensureAgentProcess(n)
			if err != nil {
				return nil, false, err
			}
			return proc, rehydrated, nil
		}
	}

	proc, rehydrated, err := resolve(name)
	if err != nil {
		return agentSendResult{}, err
	}
	return deliverToSenderWith(s, name, text, interrupt, proc, rehydrated, confirm)
}

// relayReportBody returns the sender's report for 🎯T392.7 classification and
// the PO record summary. Daemon envelopes must not participate: the standing
// brief and the 🎯T425 identity doctrine both contain classifier bait
// ("needs-owner", "oracle", "done").
func relayReportBody(text string) string {
	if i := strings.Index(text, FleetStandingBrief); i >= 0 {
		return text[i+len(FleetStandingBrief):]
	}
	return stripLeadingIdentityEnvelope(text)
}

// identityDoctrineCloser is the last fixed sentence FormatIdentityHeader
// writes before role-addressed doctrine. Everything after that doctrine is
// the payload withIdentity appended.
const identityDoctrineCloser = "carry on with your own work."

func stripLeadingIdentityEnvelope(text string) string {
	body := strings.TrimLeft(text, " \t\r\n")
	if !strings.HasPrefix(body, IdentityHeaderMarker) {
		return text
	}
	// Idle / recover wires put the event marker immediately after the header.
	if i := strings.Index(body, "\n[event:"); i >= 0 {
		return strings.TrimLeft(body[i+1:], " \t\r\n")
	}
	i := strings.Index(body, identityDoctrineCloser)
	if i < 0 {
		return text
	}
	rest := strings.TrimLeft(body[i+len(identityDoctrineCloser):], " \t\r\n")
	if !strings.HasPrefix(rest, "You are ") {
		return rest
	}
	lines := strings.Split(rest, "\n")
	out := 1 // skip the "You are …" line
	for out < len(lines) {
		line := lines[out]
		if line == "" {
			out++
			break
		}
		if !strings.HasPrefix(line, "- ") {
			break
		}
		out++
	}
	return strings.Join(lines[out:], "\n")
}

// deliverToOverseer is the overseer arm of the single path. Delivery itself
// belongs to the owner-chat layer (journal, owner bubble, notify queue), so
// this resolves the seam and reports the outcome in the same shape a fleet
// send reports — the caller cannot tell which arm ran.
func (s *Server) deliverToOverseer(name, text string, origin SendOrigin) (agentSendResult, error) {
	s.mu.Lock()
	deliver := s.overseerDeliver
	legacy := s.notifyJevon
	s.mu.Unlock()

	if deliver == nil && legacy != nil {
		if origin == OriginOwner {
			// The legacy injection cannot carry owner framing. Refusing is
			// the honest outcome: an owner turn delivered as an agent note
			// would misattribute the owner's own words.
			return agentSendResult{}, fmt.Errorf(
				"overseer %q cannot take an owner turn: no origin-carrying delivery seam wired", name)
		}
		deliver = func(t string, _ SendOrigin) error { legacy(t); return nil }
	}
	if deliver == nil {
		// 🎯T61/T62: an unreachable overseer is an error the caller sees and
		// can retry, never a logged-and-forgotten drop.
		return agentSendResult{}, fmt.Errorf("overseer %q is not reachable: no delivery seam wired", name)
	}

	// 🎯T416 clause 5. The overseer is the fleet's reporting sink, so a silent
	// drop here loses every escalation in the hierarchy at once — and it did:
	// its composer was found holding four accumulated pastes, each of which had
	// been answered "Message delivered to overseer". The seam returning nil
	// means the text was accepted into the notify queue, which is a statement
	// about the queue and not about the overseer.
	//
	// The overseer's turn boundaries are not visible from this package (its
	// event stream is owned by the chat layer, not the fleet event sink), so
	// its flight state is permanently unknown here. That is the honest input:
	// a payload that appears is confirmed, and one that does not is reported
	// unconfirmed rather than delivered — never as the defect, because a note
	// legitimately waiting behind an owner turn looks identical from here.

	// 🎯T428. Every notification source — sentinel, RSI coach, fleet health,
	// worker report notify, jevons_event_push — arrives here, so the channel
	// is where a batch the overseer already holds is refused. Owner turns are
	// exempt: the owner may say the same thing twice and mean it twice.
	var ticket notifyReplayTicket
	if origin != OriginOwner {
		var dec notifyReplayDecision
		ticket, dec = s.notifyReplays().Offer(text)
		if !dec.Admit {
			slog.Info("agent_send",
				"component", "agent_send",
				"name", name,
				"origin", string(origin),
				"status", StatusSuppressedReplay,
				"digest", dec.Digest,
				"offers", dec.Offers,
				"delivered", dec.Delivered,
				"reason", dec.Reason,
			)
			return agentSendResult{
				Status:  StatusSuppressedReplay,
				Message: describeReplaySuppression(name, dec, s.notifyReplays().Now()),
			}, nil
		}
	}

	watch := s.watchAgentTurnFor(name, text)
	if err := deliver(text, origin); err != nil {
		// The batch never reached the seam, so remembering it would refuse the
		// retry that is the correct response to this failure.
		ticket.Abandon()
		slog.Warn("agent_send",
			"component", "agent_send",
			"name", name,
			"origin", string(origin),
			"status", "failed",
			"err", err.Error(),
		)
		return agentSendResult{}, fmt.Errorf("send to overseer %q failed: %w", name, err)
	}

	ev := watch()

	// 🎯T428 seals on the RECEIVER's records, never on deliver returning nil:
	// the seam accepting text is a statement about the notify queue. A user
	// message carrying the payload, a queue record draining it into the running
	// turn, or an enqueue record holding it behind one all say the overseer has
	// it (🎯T416 / 🎯T429); anything else leaves the batch retryable.
	ticket.Settle(ev.PayloadSeen || ev.PayloadEnteredTurn || ev.PayloadQueued)

	outcome := ClassifySendOutcome(FlightUnknown, ev)
	res := agentSendResult{
		Status:  "sent",
		Message: fmt.Sprintf("Message delivered to overseer %q.", name),
	}
	if outcome != OutcomeBegun {
		res = agentSendResult{
			Status: "delivered_unconfirmed",
			Message: fmt.Sprintf(
				"Message accepted for overseer %q but NOT confirmed as a turn: %s. "+
					"It may be waiting behind an in-flight turn, or it may be sitting unsubmitted. "+
					"Treat as undelivered until the overseer acts on it.",
				name, evidenceDetail(ev)),
		}
	}
	slog.Info("agent_send",
		"component", "agent_send",
		"name", name,
		"origin", string(origin),
		"status", res.Status,
		"queued", 0,
		"rehydrated", false,
		"outcome", string(outcome),
		"payload_seen", ev.PayloadSeen,
		"evidence", ev.Detail,
	)
	return res, nil
}
