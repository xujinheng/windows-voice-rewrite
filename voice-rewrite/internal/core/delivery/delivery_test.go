package delivery

import "testing"

func TestVoiceDeliveryDoesNotConfuseSentWithConfirmed(t *testing.T) {
    sent := Sent(Dictation)
    if sent.State != SentUnverified || sent.ShowManualResult() {
        t.Fatalf("unverified successful event must not become confirmation or fallback: %+v", sent)
    }
    if verified := Verified(ReplaceSelection); verified.State != Confirmed || verified.ShowManualResult() {
        t.Fatalf("verified rewrite: %+v", verified)
    }
    if blocked := Failure(Dictation, false); blocked.State != Blocked || !blocked.ShowManualResult() {
        t.Fatalf("blocked: %+v", blocked)
    }
    if uncertain := Failure(Dictation, true); uncertain.State != Uncertain || !uncertain.ShowManualResult() {
        t.Fatalf("uncertain: %+v", uncertain)
    }
}
