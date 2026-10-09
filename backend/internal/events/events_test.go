package events

import (
	"testing"
	"time"
)

func recv(t *testing.T, ch <-chan Event) (Event, bool) {
	t.Helper()
	select {
	case e, ok := <-ch:
		return e, ok
	case <-time.After(time.Second):
		t.Fatal("timeout")
		return Event{}, false
	}
}

func TestMemoryBusFiltersByRequest(t *testing.T) {
	b := NewMemory(4)
	all, cancelAll := b.Subscribe("")
	defer cancelAll()
	r1, cancelR1 := b.Subscribe("r1")
	defer cancelR1()

	b.Publish(RequestCreated, "r2", map[string]string{"x": "y"})
	e1 := b.Publish(RequestStatusChanged, "r1", nil)

	if e, _ := recv(t, all); e.RequestID != "r2" || e.Type != RequestCreated {
		t.Errorf("all[0] = %+v", e)
	}
	if e, _ := recv(t, all); e.ID != e1.ID {
		t.Errorf("all[1] = %+v", e)
	}
	if e, _ := recv(t, r1); e.ID != e1.ID || e.RequestID != "r1" {
		t.Errorf("r1[0] = %+v", e)
	}
	select {
	case e := <-r1:
		t.Errorf("r1 got unexpected %+v", e)
	default:
	}
}

func TestMemoryBusDropsWhenFullAndCancelCloses(t *testing.T) {
	b := NewMemory(1)
	ch, cancel := b.Subscribe("")
	b.Publish(Heartbeat, "", nil)
	b.Publish(Heartbeat, "", nil) // dropped, must not block
	if e, ok := recv(t, ch); !ok || e.ID != 1 {
		t.Errorf("got %+v ok=%v", e, ok)
	}
	cancel()
	cancel() // idempotent
	if _, ok := <-ch; ok {
		t.Error("channel should be closed")
	}
	b.Publish(Heartbeat, "", nil) // no subscribers, no panic
}

func TestEventIDsMonotonic(t *testing.T) {
	b := NewMemory(0)
	a := b.Publish(Heartbeat, "", nil)
	c := b.Publish(Heartbeat, "", nil)
	if c.ID <= a.ID {
		t.Errorf("ids %d %d", a.ID, c.ID)
	}
	if _, err := c.MarshalData(); err != nil {
		t.Fatal(err)
	}
}
