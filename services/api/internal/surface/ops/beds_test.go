package ops_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// Beds in a hostel or a PG (#299). Until this existed the PM's bed board was
// invented in the app and matched nothing anybody could be billed for.
//
// Labels are unique per run: these tests commit, and a fixed label collides
// with the row the last run left behind.

func bedLabel(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()%1_000_000)
}

type bedList struct {
	Beds []struct {
		ID        string `json:"id"`
		Label     string `json:"label"`
		Room      string `json:"room"`
		Floor     int    `json:"floor"`
		Sharing   int    `json:"sharing"`
		RentMinor int64  `json:"rent_amount_minor"`
		State     string `json:"state"`
		LeaseID   string `json:"lease_id"`
	} `json:"beds"`
}

func TestTheFirmAddsABedAndReadsItBack(t *testing.T) {
	mux := serve(t)
	label := bedLabel("103A")

	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost,
		"/v1/ops/units/"+isolationtest.UnitSibling+"/beds",
		fmt.Sprintf(`{"label":%q,"rent_amount_minor":1200000}`, label))
	if w.Code != http.StatusCreated {
		t.Fatalf("adding a bed: %d %s", w.Code, w.Body.String())
	}

	w = call(t, mux, isolationtest.OrgOwner, http.MethodGet,
		"/v1/ops/properties/"+isolationtest.PropertyGranted+"/beds")
	if w.Code != http.StatusOK {
		t.Fatalf("listing beds: %d %s", w.Code, w.Body.String())
	}
	var out bedList
	decode(t, w, &out)

	for _, b := range out.Beds {
		if b.Label != label {
			continue
		}
		if b.Room != "103" || b.RentMinor != 1200000 || b.State != "vacant" {
			t.Fatalf("the bed reads back as it was filed, got %+v", b)
		}
		if b.Sharing < 1 {
			t.Errorf("sharing is how many beds the room holds, got %d", b.Sharing)
		}
		return
	}
	t.Fatalf("the bed just added is not on the board, got %+v", out.Beds)
}

func TestABedIsVacatedByAllocatingItToNobody(t *testing.T) {
	mux := serve(t)

	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost,
		"/v1/ops/units/"+isolationtest.UnitSibling+"/beds",
		fmt.Sprintf(`{"label":%q,"rent_amount_minor":900000}`, bedLabel("103V")))
	if w.Code != http.StatusCreated {
		t.Fatalf("adding a bed: %d %s", w.Code, w.Body.String())
	}
	var made struct {
		Bed struct {
			ID string `json:"id"`
		} `json:"bed"`
	}
	decode(t, w, &made)

	w = callWith(t, mux, isolationtest.OrgOwner, http.MethodPost,
		"/v1/ops/beds/"+made.Bed.ID+"/allocation", `{"state":"reserved"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("reserving the bed: %d %s", w.Code, w.Body.String())
	}

	w = callWith(t, mux, isolationtest.OrgOwner, http.MethodPost,
		"/v1/ops/beds/"+made.Bed.ID+"/allocation", `{"state":"vacant"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("vacating the bed: %d %s", w.Code, w.Body.String())
	}
}

// An occupied bed is one somebody is billed for; the state cannot be claimed
// without naming the lease that carries the rent.
func TestABedCannotBeOccupiedWithoutNamingTheLease(t *testing.T) {
	mux := serve(t)

	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost,
		"/v1/ops/units/"+isolationtest.UnitSibling+"/beds",
		fmt.Sprintf(`{"label":%q,"rent_amount_minor":900000}`, bedLabel("103X")))
	if w.Code != http.StatusCreated {
		t.Fatalf("adding a bed: %d %s", w.Code, w.Body.String())
	}
	var made struct {
		Bed struct {
			ID string `json:"id"`
		} `json:"bed"`
	}
	decode(t, w, &made)

	w = callWith(t, mux, isolationtest.OrgOwner, http.MethodPost,
		"/v1/ops/beds/"+made.Bed.ID+"/allocation", `{"state":"occupied"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("occupying a bed with no lease behind it must be refused, got %d %s",
			w.Code, w.Body.String())
	}
}

// Only a second bed of the same label is a conflict. Anything else the store
// says is a fault, and reporting it as "already in this room" sent a warden
// looking for a bed that was never created.
func TestOnlyARepeatedLabelIsAConflict(t *testing.T) {
	mux := serve(t)
	label := bedLabel("103D")
	body := fmt.Sprintf(`{"label":%q,"rent_amount_minor":900000}`, label)
	path := "/v1/ops/units/" + isolationtest.UnitSibling + "/beds"

	if w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost, path, body); w.Code != http.StatusCreated {
		t.Fatalf("adding a bed: %d %s", w.Code, w.Body.String())
	}
	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost, path, body)
	if w.Code != http.StatusConflict {
		t.Fatalf("the same label twice is a conflict, got %d %s", w.Code, w.Body.String())
	}
}

func TestCannotReadTheBedsOfAnotherFirmsProperty(t *testing.T) {
	mux := serve(t)

	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet,
		"/v1/ops/properties/"+isolationtest.PropertySecond+"/beds")
	if w.Code != http.StatusNotFound {
		t.Fatalf("another firm's board must not be readable, got %d %s", w.Code, w.Body.String())
	}
}
