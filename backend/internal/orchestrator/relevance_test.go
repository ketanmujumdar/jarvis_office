package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm/fakellm"
)

// scriptedMatcher answers every candidate with the same verdict.
func scriptedMatcher(isMatch bool, pack int, err error) OfferMatcher {
	return func(_ context.Context, _ llm.MatchTarget, offers []llm.OfferCandidate) ([]llm.OfferMatch, error) {
		if err != nil {
			return nil, err
		}
		out := make([]llm.OfferMatch, len(offers))
		for i := range offers {
			out[i] = llm.OfferMatch{Index: i, IsMatch: isMatch, PackSize: pack}
		}
		return out, nil
	}
}

func TestFlow_Matcher_FakeLLM_PaperAndCoffee(t *testing.T) {
	runPaperAndCoffee(t, newEnv(t, withMatcher(LLMOfferMatcher(fakellm.New()))))
}

func TestFlow_Matcher(t *testing.T) {
	tests := []struct {
		name       string
		matcher    OfferMatcher
		wantStatus domain.RequestStatus
		check      func(t *testing.T, d domain.RequestDetail)
	}{
		{
			name:       "no offer matches: every line rejected with NO_OFFER",
			matcher:    scriptedMatcher(false, 1, nil),
			wantStatus: domain.StatusRejected,
			check: func(t *testing.T, d domain.RequestDetail) {
				if len(d.Offers) != 0 {
					t.Fatalf("offers kept: %d", len(d.Offers))
				}
				for _, li := range d.LineItems {
					if !hasReason(li.Reasons, domain.ReasonNoOffer) {
						t.Fatalf("line %s reasons %+v", li.Description, li.Reasons)
					}
				}
			},
		},
		{
			name:       "matcher pack size is applied",
			matcher:    scriptedMatcher(true, 2, nil),
			wantStatus: domain.StatusQuoted,
			check: func(t *testing.T, d domain.RequestDetail) {
				if len(d.Offers) == 0 {
					t.Fatal("no offers")
				}
				for _, of := range d.Offers {
					if of.PackSize != 2 {
						t.Fatalf("offer %q pack %d, want 2", of.Title, of.PackSize)
					}
				}
			},
		},
		{
			name:       "matcher error keeps unfiltered offers",
			matcher:    scriptedMatcher(true, 1, errors.New("model down")),
			wantStatus: domain.StatusQuoted,
			check: func(t *testing.T, d domain.RequestDetail) {
				if len(d.Offers) == 0 {
					t.Fatal("offers dropped on matcher error")
				}
			},
		},
		{
			name: "wrong-length answer is ignored",
			matcher: func(context.Context, llm.MatchTarget, []llm.OfferCandidate) ([]llm.OfferMatch, error) {
				return []llm.OfferMatch{{Index: 0, IsMatch: false}}, nil
			},
			wantStatus: domain.StatusQuoted,
			check: func(t *testing.T, d domain.RequestDetail) {
				if len(d.Offers) < 2 {
					t.Fatalf("offers = %d", len(d.Offers))
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, withMatcher(tc.matcher))
			r := e.create("We're out of printer paper and coffee pods, reorder the usual.")
			tc.check(t, e.wantStatus(r.ID, tc.wantStatus))
		})
	}
}

func TestMatchTarget(t *testing.T) {
	li := domain.LineItem{Description: "standing desk"}
	if got := matchTarget(li, nil); got.Name != "standing desk" || got.SKU != "" {
		t.Fatalf("off-list target = %+v", got)
	}
	it := domain.CatalogItem{SKU: "a4", Name: "A4 paper", Unit: "ream", MaxUnitPriceCents: 900}
	if got := matchTarget(li, &it); got.SKU != "a4" || got.Unit != "ream" || got.MaxUnitPriceCents != 900 {
		t.Fatalf("catalog target = %+v", got)
	}
}
