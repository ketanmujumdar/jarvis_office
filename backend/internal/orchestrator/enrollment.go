package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/audit"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/events"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/queue"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap"
)

type enrollmentPayload struct {
	EnrollmentID string `json:"enrollment_id"`
}

// StartEnrollment creates a Reap EXTERNAL enrollment; the user opens NextActionURL to enter the
// card on Reap's hosted page. We store only Reap's id and status, never card data.
func (o *Impl) StartEnrollment(ctx context.Context, user domain.User) (domain.Enrollment, error) {
	if user.ID == "" || strings.TrimSpace(user.Email) == "" {
		return domain.Enrollment{}, fmt.Errorf("%w: user id and email are required", domain.ErrValidation)
	}
	if domain.ReservedEmailDomain(user.Email) {
		return domain.Enrollment{}, fmt.Errorf("%w: email %q uses a reserved domain that Reap rejects", domain.ErrValidation, user.Email)
	}
	re, err := o.d.Reap.CreateEnrollment(ctx, o.d.NewID(), reap.CreateEnrollmentRequest{
		Source:       "EXTERNAL",
		Owner:        reap.Owner{Type: "CLIENT_REFERENCE", ID: user.ID, Email: user.Email, Name: user.Name},
		Presentation: reap.Presentation{Type: "REDIRECT", ReturnURL: o.d.ReapReturnURL},
	})
	if err != nil {
		return domain.Enrollment{}, fmt.Errorf("reap create enrollment: %w", err)
	}
	e := &domain.Enrollment{
		ReapEnrollmentID: re.ID, Status: domain.EnrollmentStatus(re.Status), OwnerRef: user.ID, OwnerEmail: user.Email, CreatedBy: user.ID,
	}
	if re.NextAction != nil {
		e.NextActionURL = re.NextAction.URL
	}
	if e.Status == "" {
		e.Status = domain.EnrollmentRequiresAction
	}
	if err := o.d.Store.Enrollments().Create(ctx, e); err != nil {
		return domain.Enrollment{}, fmt.Errorf("save enrollment: %w", err)
	}
	o.record(ctx, "", domain.ActorUser, user.ID, audit.EnrollmentStarted, map[string]any{"enrollment_id": e.ID, "reap_enrollment_id": e.ReapEnrollmentID, "status": e.Status})
	o.publish(events.EnrollmentUpdated, "", map[string]any{"enrollment": e})
	if e.Status == domain.EnrollmentRequiresAction {
		if _, err := o.d.Queue.EnqueueAfter(ctx, o.enrollmentPollJob(e.ID), o.d.EnrollmentPollInterval); err != nil {
			o.log.Warn("schedule enrollment poll failed", "err", err)
		}
	}
	return *e, nil
}

// CurrentEnrollment returns the ACTIVE enrollment if there is one, else the latest enrollment.
// Pending enrollments are refreshed from Reap first: the in-process poll job is lost on restart,
// so the user can finish Reap's hosted card page while nothing is watching it.
func (o *Impl) CurrentEnrollment(ctx context.Context) (domain.Enrollment, error) {
	o.refreshPendingEnrollments(ctx)
	if e, err := o.d.Store.Enrollments().LatestActive(ctx); err == nil {
		return e, nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return domain.Enrollment{}, err
	}
	return o.d.Store.Enrollments().Latest(ctx)
}

// refreshPendingEnrollments re-reads every REQUIRES_ACTION enrollment from Reap (best effort).
func (o *Impl) refreshPendingEnrollments(ctx context.Context) {
	pending, err := o.d.Store.Enrollments().ListPending(ctx)
	if err != nil {
		o.log.Warn("list pending enrollments failed", "err", err)
		return
	}
	for _, e := range pending {
		if _, err := o.refreshEnrollment(ctx, e); err != nil {
			o.log.Warn("refresh enrollment failed", "enrollment_id", e.ID, "err", err)
		}
	}
}

// activeEnrollment returns an ACTIVE enrollment, refreshing pending ones from Reap first.
func (o *Impl) activeEnrollment(ctx context.Context) (domain.Enrollment, error) {
	e, err := o.d.Store.Enrollments().LatestActive(ctx)
	if err == nil {
		return e, nil
	}
	if errors.Is(err, domain.ErrNotFound) {
		o.refreshPendingEnrollments(ctx)
		if e, err = o.d.Store.Enrollments().LatestActive(ctx); err == nil {
			return e, nil
		}
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return domain.Enrollment{}, err
	}
	latest, err := o.d.Store.Enrollments().Latest(ctx)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Enrollment{}, domain.ErrNoActiveEnrollment
		}
		return domain.Enrollment{}, err
	}
	latest, err = o.refreshEnrollment(ctx, latest)
	if err != nil {
		if reap.IsCode(err, reap.CodeEnrollmentNotFound) || reap.IsCode(err, reap.CodeCardNotFound) {
			// Reap no longer knows this enrollment (e.g. a different API key/environment): treat
			// it as gone so the user is asked to add a card, not shown an upstream error.
			if uerr := o.d.Store.Enrollments().UpdateStatus(ctx, latest.ID, domain.EnrollmentFailed, ""); uerr != nil {
				o.log.Warn("mark stale enrollment failed", "enrollment_id", latest.ID, "err", uerr)
			}
			return domain.Enrollment{}, domain.ErrNoActiveEnrollment
		}
		return domain.Enrollment{}, err
	}
	if latest.Status != domain.EnrollmentActive {
		return domain.Enrollment{}, domain.ErrNoActiveEnrollment
	}
	return latest, nil
}

func (o *Impl) refreshEnrollment(ctx context.Context, e domain.Enrollment) (domain.Enrollment, error) {
	if e.Status != domain.EnrollmentRequiresAction || e.ReapEnrollmentID == "" {
		return e, nil
	}
	re, err := o.d.Reap.GetEnrollment(ctx, e.ReapEnrollmentID)
	if err != nil {
		return e, fmt.Errorf("reap get enrollment: %w", err)
	}
	// re.PaymentMethod (network, last4, expiry) is deliberately ignored: no card data is stored.
	ns := domain.EnrollmentStatus(re.Status)
	if ns == "" || ns == e.Status {
		return e, nil
	}
	url := e.NextActionURL
	if re.NextAction != nil && re.NextAction.URL != "" {
		url = re.NextAction.URL
	}
	if err := o.d.Store.Enrollments().UpdateStatus(ctx, e.ID, ns, url); err != nil {
		return e, err
	}
	e.Status, e.NextActionURL = ns, url
	o.record(ctx, "", domain.ActorSystem, "reap", audit.EnrollmentStatus, map[string]any{"enrollment_id": e.ID, "status": ns})
	o.publish(events.EnrollmentUpdated, "", map[string]any{"enrollment": e})
	return e, nil
}

func (o *Impl) enrollmentPollJob(id string) queue.Job {
	return queue.Job{
		JobID: fmt.Sprintf("enrollment:%s:%d", id, o.d.Clock().UnixNano()), Kind: queue.KindPollEnrollment,
		Payload: mustJSON(enrollmentPayload{EnrollmentID: id}),
	}
}

func (o *Impl) handlePollEnrollment(ctx context.Context, job queue.Job) error {
	var p enrollmentPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil || p.EnrollmentID == "" {
		return queue.Permanent(fmt.Errorf("bad enrollment poll payload"))
	}
	e, err := o.d.Store.Enrollments().Get(ctx, p.EnrollmentID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return queue.Permanent(err)
		}
		return err
	}
	e, err = o.refreshEnrollment(ctx, e)
	if err != nil {
		o.log.Warn("enrollment poll failed", "enrollment_id", p.EnrollmentID, "err", err)
	}
	if e.Status != domain.EnrollmentRequiresAction || o.d.Clock().Sub(e.CreatedAt) > o.d.EnrollmentPollTimeout {
		return nil
	}
	_, err = o.d.Queue.EnqueueAfter(ctx, o.enrollmentPollJob(e.ID), o.d.EnrollmentPollInterval)
	return err
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}
