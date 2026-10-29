package poownermission

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
)

func TestParseBoundedRequestRequiresRequestID(t *testing.T) {
	if _, ok := ParseBoundedRequest("reconcile 🎯T1 and 🎯T2", ""); ok {
		t.Fatal("empty request id must not record")
	}
}

func TestParseBoundedRequestFromTargetList(t *testing.T) {
	br, ok := ParseBoundedRequest("please reconcile 🎯T10 and T11", "om-abc")
	if !ok || len(br.Items) != 2 || br.RequestID != "om-abc" {
		t.Fatalf("got %+v ok=%v", br, ok)
	}
}

func TestParseBoundedRequestFromOwnerMissionFence(t *testing.T) {
	text := "```jevons\njevons: kind owner-mission\njevons: item-count 110\n```"
	br, ok := ParseBoundedRequest(text, "om-110")
	if !ok || br.ItemCount != 110 {
		t.Fatalf("got %+v ok=%v", br, ok)
	}
}

func TestClosesMissionOnFinishOrBlocked(t *testing.T) {
	done := envelope.Format(&envelope.Message{
		Kind: envelope.KindFinishReport, Target: "T1195",
		SHA: "abcdef0123456", SilentLedger: envelope.SilentLedgerEmpty,
	})
	if !ClosesMission(done) {
		t.Fatal("finish-report must close")
	}
	blocked := envelope.Format(&envelope.Message{
		Kind: envelope.KindFinishReport, Target: "T1195",
		Status: envelope.ProgressBlocked, Blocker: "owner decision",
		SilentLedger: envelope.SilentLedgerEmpty,
	})
	if !ClosesMission(blocked) {
		t.Fatal("blocked finish must close")
	}
	if ClosesMission("still working") {
		t.Fatal("prose must not close")
	}
}
