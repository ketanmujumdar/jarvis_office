package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
)

func TestCreateRequest(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		want      int
		wantItems int
	}{
		{"utterance only", `{"utterance":"We're out of printer paper"}`, 201, 0},
		{"pre-parsed items", `{"utterance":"paper","items":[{"description":"A4 paper","qty":5,"urgency":"urgent"},{"description":"pods"}]}`, 201, 2},
		{"missing utterance", `{"items":[{"description":"x"}]}`, 400, 0},
		{"blank utterance", `{"utterance":"   "}`, 400, 0},
		{"item qty zero", `{"utterance":"x","items":[{"description":"a","qty":0}]}`, 400, 0},
		{"item no description", `{"utterance":"x","items":[{"qty":1}]}`, 400, 0},
		{"bad urgency", `{"utterance":"x","items":[{"description":"a","urgency":"asap"}]}`, 400, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			r := h.do("POST", "/api/v1/requests", managerID, tt.body)
			if r.code != tt.want {
				t.Fatalf("code = %d %s", r.code, r.body)
			}
			if tt.want != 201 {
				if len(h.orch.created) != 0 {
					t.Fatal("orchestrator called on invalid input")
				}
				return
			}
			var pr domain.PurchaseRequest
			r.json(t, &pr)
			if pr.Status != domain.StatusParsing || pr.RequesterID != managerID {
				t.Fatalf("request = %+v", pr)
			}
			in := h.orch.created[0]
			if len(in.Items) != tt.wantItems || in.RequesterID != managerID {
				t.Fatalf("input = %+v", in)
			}
			for _, it := range in.Items {
				if it.Urgency == "" {
					t.Fatal("urgency not defaulted")
				}
			}
		})
	}
}

func TestListRequestsFilters(t *testing.T) {
	h := newHarness(t)
	h.addRequest(domain.RequestDetail{Request: domain.PurchaseRequest{ID: requestID, RequesterID: managerID, Status: domain.StatusQuoted}})
	h.addRequest(domain.RequestDetail{Request: domain.PurchaseRequest{ID: missingID, RequesterID: adminID, Status: domain.StatusOrdered}})
	tests := []struct {
		query string
		code  int
		count int
	}{
		{"", 200, 2},
		{"?status=quoted,pending_approval", 200, 1},
		{"?status=ordered", 200, 1},
		{"?status=bogus", 400, 0},
		{"?mine=true", 200, 1},
		{"?mine=maybe", 400, 0},
	}
	for _, tt := range tests {
		r := h.do("GET", "/api/v1/requests"+tt.query, managerID, nil)
		if r.code != tt.code {
			t.Errorf("%s: code %d %s", tt.query, r.code, r.body)
			continue
		}
		if tt.code != 200 {
			continue
		}
		var out struct{ Requests []domain.PurchaseRequest }
		r.json(t, &out)
		if len(out.Requests) != tt.count {
			t.Errorf("%s: %d requests", tt.query, len(out.Requests))
		}
	}
}

func TestGetRequestDetailHasArrays(t *testing.T) {
	h := newHarness(t)
	h.addRequest(domain.RequestDetail{Request: domain.PurchaseRequest{ID: requestID, Status: domain.StatusSearching},
		LineItems: []domain.LineItem{{ID: "li"}}})
	r := h.do("GET", "/api/v1/requests/"+requestID, managerID, nil)
	if r.code != 200 {
		t.Fatalf("code %d", r.code)
	}
	for _, k := range []string{`"offers":[]`, `"approvals":[]`, `"payments":[]`, `"reasons":[]`} {
		if !strings.Contains(string(r.body), k) {
			t.Errorf("missing %s in %s", k, r.body)
		}
	}
	if r := h.do("GET", "/api/v1/requests/"+missingID, managerID, nil); r.code != 404 {
		t.Errorf("missing request = %d", r.code)
	}
}

func TestConfirmRequest(t *testing.T) {
	tests := []struct {
		name     string
		status   domain.RequestStatus
		body     string
		orchErr  error
		want     int
		wantCode string
	}{
		{"ok", domain.StatusQuoted, `{"address_id":"` + addressID + `"}`, nil, 200, ""},
		{"missing address", domain.StatusQuoted, `{}`, nil, 400, "validation_failed"},
		{"address not uuid", domain.StatusQuoted, `{"address_id":"hq"}`, nil, 400, "validation_failed"},
		{"wrong status", domain.StatusSearching, `{"address_id":"` + addressID + `"}`, nil, 409, "invalid_transition"},
		{"no card", domain.StatusQuoted, `{"address_id":"` + addressID + `"}`, domain.ErrNoActiveEnrollment, 409, "no_active_enrollment"},
		{"unknown address", domain.StatusQuoted, `{"address_id":"` + missingID + `"}`, fmt.Errorf("address: %w", domain.ErrNotFound), 404, "not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.orch.err = tt.orchErr
			h.addRequest(domain.RequestDetail{Request: domain.PurchaseRequest{ID: requestID, Status: tt.status}})
			r := h.do("POST", "/api/v1/requests/"+requestID+"/confirm", managerID, tt.body)
			if r.code != tt.want {
				t.Fatalf("code %d %s", r.code, r.body)
			}
			if tt.wantCode != "" {
				if c := r.errCode(t); c != tt.wantCode {
					t.Fatalf("error code %s", c)
				}
				return
			}
			var d domain.RequestDetail
			r.json(t, &d)
			if d.Request.Status != domain.StatusApproved || h.orch.confirmed[0].AddressID != addressID || h.orch.confirmed[0].UserID != managerID {
				t.Fatalf("detail = %+v, confirmed = %+v", d.Request, h.orch.confirmed)
			}
		})
	}
}

func TestCancelRequest(t *testing.T) {
	for _, tt := range []struct {
		status domain.RequestStatus
		want   int
	}{{domain.StatusQuoted, 200}, {domain.StatusPaying, 409}, {domain.StatusOrdered, 409}} {
		h := newHarness(t)
		h.addRequest(domain.RequestDetail{Request: domain.PurchaseRequest{ID: requestID, Status: tt.status}})
		if r := h.do("POST", "/api/v1/requests/"+requestID+"/cancel", managerID, nil); r.code != tt.want {
			t.Errorf("cancel from %s = %d %s", tt.status, r.code, r.body)
		}
	}
}

func TestCheckoutStatusAndRefresh(t *testing.T) {
	h := newHarness(t)
	h.addRequest(domain.RequestDetail{
		Request:  domain.PurchaseRequest{ID: requestID, Status: domain.StatusAwaitingPayment},
		Payments: []domain.Payment{{ID: "p1", Status: domain.PaymentRequiresAction, ApprovalURL: "https://reap.example/approve"}},
	})
	var cs CheckoutStatus
	r := h.do("GET", "/api/v1/requests/"+requestID+"/checkout", managerID, nil)
	r.json(t, &cs)
	if r.code != 200 || cs.RequestID != requestID || cs.Status != domain.StatusAwaitingPayment || len(cs.Payments) != 1 {
		t.Fatalf("checkout = %d %+v", r.code, cs)
	}
	if len(h.orch.refreshed) != 0 {
		t.Fatal("GET checkout must not poll Reap")
	}
	r = h.do("POST", "/api/v1/requests/"+requestID+"/checkout/refresh", managerID, nil)
	if r.code != 200 || len(h.orch.refreshed) != 1 {
		t.Fatalf("refresh = %d %v", r.code, h.orch.refreshed)
	}
	h.orch.err = fmt.Errorf("reap: %w", domain.ErrUpstream)
	if r := h.do("POST", "/api/v1/requests/"+requestID+"/checkout/refresh", managerID, nil); r.code != 502 || r.errCode(t) != "upstream_error" {
		t.Fatalf("refresh upstream error = %d %s", r.code, r.body)
	}
	if r := h.do("GET", "/api/v1/requests/"+missingID+"/checkout", managerID, nil); r.code != 404 {
		t.Fatalf("missing = %d", r.code)
	}
}

func TestRequestAudit(t *testing.T) {
	h := newHarness(t)
	h.addRequest(domain.RequestDetail{Request: domain.PurchaseRequest{ID: requestID}})
	rid := requestID
	h.st.audit = []domain.AuditEvent{{ID: 1, RequestID: &rid, Type: "request.created"}, {ID: 2, Type: "admin.changed"}}
	var out struct{ Events []domain.AuditEvent }
	r := h.do("GET", "/api/v1/requests/"+requestID+"/audit", managerID, nil)
	r.json(t, &out)
	if r.code != 200 || len(out.Events) != 1 || out.Events[0].Type != "request.created" {
		t.Fatalf("audit = %d %s", r.code, r.body)
	}
	if r := h.do("GET", "/api/v1/requests/"+missingID+"/audit", managerID, nil); r.code != 404 {
		t.Fatalf("missing = %d", r.code)
	}
}
