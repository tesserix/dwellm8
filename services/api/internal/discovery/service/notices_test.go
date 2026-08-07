package service_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/discovery/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/push"
)

// A viewing that is called off has to reach the person holding it (#332).
// Nothing here sends anything itself: the fact is already in the outbox, and
// this is the consumer that turns it into a notification.

type fakeTokens struct {
	byProspect map[string][]string
	asked      []string
}

func (f *fakeTokens) ForProspect(_ context.Context, id string) ([]string, error) {
	f.asked = append(f.asked, id)
	return f.byProspect[id], nil
}

type fakeSender struct{ sent []push.Message }

func (f *fakeSender) Send(_ context.Context, msgs []push.Message) ([]string, error) {
	f.sent = append(f.sent, msgs...)
	return nil, nil
}

func notices(t *testing.T) (service.Notices, *fakeTokens, *fakeSender) {
	t.Helper()
	tok := &fakeTokens{byProspect: map[string][]string{"p1": {"ExponentPushToken[a]", "ExponentPushToken[b]"}}}
	send := &fakeSender{}
	return service.Notices{Tokens: tok, Sender: send, Log: slog.Default()}, tok, send
}

func TestALetFlatTellsThePeopleBookedIn(t *testing.T) {
	n, _, send := notices(t)
	body := []byte(`{"type":"discovery.inspection.cancelled","tenant_id":"t1",
		"subject":{"kind":"enquiry","id":"e1"},
		"data":{"by":"listing_let","prospect_id":"p1","listing_id":"l1"}}`)

	if err := n.Handle(context.Background(), body); err != nil {
		t.Fatalf("handling: %v", err)
	}
	if len(send.sent) != 2 {
		t.Fatalf("sent %d notifications; want one per device", len(send.sent))
	}
	// The reason is the message: "called off" without a why reads as a mistake.
	if got := send.sent[0].Body; got == "" || !contains(got, "let") {
		t.Errorf("body = %q; want it to say the home was let", got)
	}
}

func TestAnOwnersCancellationSaysSoInstead(t *testing.T) {
	n, _, send := notices(t)
	body := []byte(`{"type":"discovery.inspection.cancelled","tenant_id":"t1",
		"subject":{"kind":"enquiry","id":"e1"},
		"data":{"by":"cancelled_by_owner","prospect_id":"p1","listing_id":"l1"}}`)

	if err := n.Handle(context.Background(), body); err != nil {
		t.Fatalf("handling: %v", err)
	}
	if len(send.sent) != 2 || contains(send.sent[0].Body, "let") {
		t.Fatalf("body = %q; want the owner's cancellation, not the letting", send.sent[0].Body)
	}
}

// A prospect who cancelled their own viewing knows already, and a prospect with
// no device registered is not an error.
func TestNothingIsSentWhereNobodyNeedsTelling(t *testing.T) {
	n, tok, send := notices(t)
	own := []byte(`{"type":"discovery.inspection.cancelled","tenant_id":"t1",
		"subject":{"kind":"enquiry","id":"e1"},
		"data":{"by":"cancelled_by_prospect","prospect_id":"p1"}}`)
	if err := n.Handle(context.Background(), own); err != nil {
		t.Fatalf("handling: %v", err)
	}
	if len(send.sent) != 0 || len(tok.asked) != 0 {
		t.Fatalf("told a prospect about their own cancellation")
	}

	quiet := []byte(`{"type":"discovery.inspection.cancelled","tenant_id":"t1",
		"subject":{"kind":"enquiry","id":"e2"},
		"data":{"by":"listing_let","prospect_id":"nodevice"}}`)
	if err := n.Handle(context.Background(), quiet); err != nil {
		t.Fatalf("handling with no device: %v", err)
	}
	if len(send.sent) != 0 {
		t.Fatalf("sent %d notifications to nobody", len(send.sent))
	}
}

// A request answered in silence is a request the prospect keeps waiting on
// (#331): every answer reaches them, and each says which answer it was.
func TestEveryAnswerToARequestReachesTheProspect(t *testing.T) {
	for _, tc := range []struct {
		event string
		says  string
	}{
		{"discovery.inspection.confirmed", "confirmed"},
		{"discovery.inspection.countered", "another time"},
		{"discovery.inspection.declined", "declined"},
	} {
		t.Run(tc.event, func(t *testing.T) {
			n, _, send := notices(t)
			body := []byte(`{"type":"` + tc.event + `","tenant_id":"t1",
				"subject":{"kind":"enquiry","id":"e1"},
				"data":{"prospect_id":"p1","listing_id":"l1","kind":"inspection"}}`)

			if err := n.Handle(context.Background(), body); err != nil {
				t.Fatalf("handling: %v", err)
			}
			if len(send.sent) != 2 {
				t.Fatalf("sent %d notifications; want one per device", len(send.sent))
			}
			if !contains(send.sent[0].Body, tc.says) {
				t.Errorf("body = %q; want it to say %q", send.sent[0].Body, tc.says)
			}
		})
	}
}

func TestOtherFactsPassThrough(t *testing.T) {
	n, _, send := notices(t)
	if err := n.Handle(context.Background(),
		[]byte(`{"type":"discovery.inspection.booked","subject":{"id":"e1"}}`)); err != nil {
		t.Fatalf("handling: %v", err)
	}
	if len(send.sent) != 0 {
		t.Fatalf("a booking sent a cancellation notice")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
