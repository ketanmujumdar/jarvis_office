package memstore_test

import (
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/memstore"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/storetest"
)

func TestConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Env {
		s := memstore.New()
		return storetest.Env{
			Store:          s,
			SetCompletedAt: func(_ *testing.T, id string, at time.Time) { s.SetPaymentCompletedAt(id, at) },
		}
	})
}
