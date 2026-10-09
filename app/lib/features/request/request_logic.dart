import 'package:flutter/material.dart';

import '../../core/api/models.dart';
import '../../core/format.dart';
import '../../core/theme/tokens.dart';

// Pure presentation logic for a purchase request: progress stages, the live
// activity feed built from SSE events, and per-line quote comparison.
// Everything here is UI-free apart from icons and tones, so it is unit-tested.

/// The high-level steps shown in the progress tracker.
enum ProgressStage {
  understand('Understand'),
  search('Find prices'),
  review('Review'),
  approve('Approve'),
  pay('Pay'),
  done('Ordered');

  const ProgressStage(this.label);
  final String label;
}

/// Maps a request status to the stage it belongs to (null for terminal
/// failures, which are shown as a banner instead).
ProgressStage? stageFor(RequestStatus s) => switch (s) {
  RequestStatus.parsing => ProgressStage.understand,
  RequestStatus.searching => ProgressStage.search,
  RequestStatus.quoted => ProgressStage.review,
  RequestStatus.pendingApproval ||
  RequestStatus.approved => ProgressStage.approve,
  RequestStatus.checkingOut ||
  RequestStatus.awaitingPayment ||
  RequestStatus.paying => ProgressStage.pay,
  RequestStatus.ordered => ProgressStage.done,
  RequestStatus.rejected ||
  RequestStatus.failed ||
  RequestStatus.cancelled ||
  RequestStatus.unknown => null,
};

/// One row in the live activity feed.
@immutable
class ActivityEntry {
  const ActivityEntry({
    required this.id,
    required this.icon,
    required this.title,
    required this.at,
    this.detail,
    this.tone = Tone.neutral,
  });

  final int id;
  final IconData icon;
  final String title;
  final String? detail;
  final Tone tone;
  final DateTime at;
}

String _s(Object? v) => v is String ? v : '';
int _i(Object? v) => v is num ? v.toInt() : 0;
Json _m(Object? v) => v is Map ? v.cast<String, dynamic>() : const {};
List<dynamic> _l(Object? v) => v is List ? v : const [];

/// Builds a human-readable feed entry for an SSE event. Returns null for
/// events that carry no user-facing progress (heartbeats, chat messages).
ActivityEntry? activityFromEvent(SseEvent e) {
  final d = e.data;
  ActivityEntry entry(
    IconData icon,
    String title, {
    String? detail,
    Tone tone = Tone.neutral,
  }) => ActivityEntry(
    id: e.id,
    icon: icon,
    title: title,
    detail: (detail == null || detail.isEmpty) ? null : detail,
    tone: tone,
    at: e.at,
  );

  switch (e.type) {
    case SseTypes.requestCreated:
      return entry(
        Icons.add_task_rounded,
        'Request received',
        detail: _s(_m(d['request'])['raw_utterance']),
        tone: Tone.brand,
      );
    case SseTypes.requestStatusChanged:
      final to = RequestStatus.fromJson(d['to']);
      final reason = _s(d['failure_reason']);
      return entry(
        to == RequestStatus.failed
            ? Icons.error_outline_rounded
            : Icons.sync_alt_rounded,
        to.label,
        detail: reason,
        tone: switch (to) {
          RequestStatus.failed || RequestStatus.rejected => Tone.danger,
          RequestStatus.ordered => Tone.success,
          RequestStatus.pendingApproval ||
          RequestStatus.awaitingPayment => Tone.warning,
          _ => Tone.info,
        },
      );
    case SseTypes.lineItemsParsed:
      final items = _l(d['line_items']);
      final names = items
          .map((i) => '${_i(_m(i)['qty'])} × ${_s(_m(i)['description'])}')
          .join(', ');
      return entry(
        Icons.checklist_rounded,
        'Understood ${items.length} ${items.length == 1 ? 'item' : 'items'}',
        detail: names,
        tone: Tone.brand,
      );
    case SseTypes.searchStarted:
      final vendors = _l(d['vendors']).whereType<String>().toList();
      return entry(
        Icons.travel_explore_rounded,
        'Searching ${vendors.length} ${vendors.length == 1 ? 'vendor' : 'vendors'}',
        detail: vendors.join(', '),
        tone: Tone.info,
      );
    case SseTypes.searchVendorResult:
      final vendor = _s(d['vendor_domain']);
      final err = _s(d['error']);
      if (err.isNotEmpty) {
        return entry(
          Icons.cloud_off_rounded,
          '$vendor unavailable',
          detail: err,
          tone: Tone.warning,
        );
      }
      final n = _i(d['offers_found']);
      return entry(
        n > 0 ? Icons.sell_outlined : Icons.search_off_rounded,
        '$vendor: $n ${n == 1 ? 'offer' : 'offers'}',
        tone: n > 0 ? Tone.info : Tone.neutral,
      );
    case SseTypes.offersRanked:
      final offers = _l(d['offers']);
      if (offers.isEmpty) {
        return entry(
          Icons.search_off_rounded,
          'No allowed vendor stocks this item',
          tone: Tone.warning,
        );
      }
      final best = Offer.fromJson(_m(offers.first));
      return entry(
        Icons.emoji_events_outlined,
        'Best offer: ${best.merchantName}',
        detail: '${best.title} · ${Fmt.money(best.landedCostCents)}',
        tone: Tone.success,
      );
    case SseTypes.policyEvaluated:
      final decision = Decision.fromJson(d['decision']);
      final reasons = _l(d['reasons'])
          .map((r) => _s(_m(r)['message']))
          .where((m) => m.isNotEmpty);
      return entry(
        Icons.policy_outlined,
        'Policy: ${decision.label}',
        detail: reasons.join(' · '),
        tone: switch (decision) {
          Decision.autoApprove => Tone.success,
          Decision.needsApproval => Tone.warning,
          Decision.reject => Tone.danger,
          Decision.none => Tone.neutral,
        },
      );
    case SseTypes.approvalRequested:
      final a = Approval.fromJson(_m(d['approval']));
      return entry(
        Icons.how_to_reg_outlined,
        a.isPriceDrift
            ? 'Re-approval requested (price changed)'
            : 'Sent for approval',
        detail: Fmt.money(a.amountCents),
        tone: Tone.warning,
      );
    case SseTypes.approvalDecided:
      final a = Approval.fromJson(_m(d['approval']));
      final ok = a.status == ApprovalStatus.approved;
      return entry(
        ok ? Icons.verified_outlined : Icons.block_rounded,
        ok ? 'Approved' : 'Rejected by approver',
        detail: a.comment,
        tone: ok ? Tone.success : Tone.danger,
      );
    case SseTypes.checkoutQuoted:
      final p = Payment.fromJson(_m(d['payment']));
      return entry(
        Icons.request_quote_outlined,
        'Live quote from ${p.merchantName}',
        detail: Fmt.money(p.quotedCents),
        tone: Tone.info,
      );
    case SseTypes.checkoutPriceDrift:
      final pct = (d['pct'] as num?)?.toDouble() ?? 0;
      return entry(
        Icons.trending_up_rounded,
        'Price changed ${pct.toStringAsFixed(1)}%',
        detail:
            '${Fmt.money(_i(d['approved_cents']))} → ${Fmt.money(_i(d['live_cents']))}',
        tone: Tone.warning,
      );
    case SseTypes.paymentActionRequired:
      final p = Payment.fromJson(_m(d['payment']));
      return entry(
        Icons.lock_open_rounded,
        'Approve payment with Reap',
        detail: '${p.merchantName} · ${Fmt.money(p.quotedCents)}',
        tone: Tone.warning,
      );
    case SseTypes.paymentStatusChanged:
      final p = Payment.fromJson(_m(d['payment']));
      return entry(
        Icons.payments_outlined,
        '${p.merchantName}: ${p.status.label}',
        detail: p.error,
        tone: switch (p.status) {
          PaymentStatus.completed => Tone.success,
          PaymentStatus.failed || PaymentStatus.expired => Tone.danger,
          PaymentStatus.requiresAction => Tone.warning,
          _ => Tone.info,
        },
      );
    case SseTypes.orderCompleted:
      return entry(
        Icons.celebration_outlined,
        'Order placed',
        detail: '${_l(d['payments']).length} merchant order(s) confirmed',
        tone: Tone.success,
      );
    case SseTypes.paymentAlert:
      return entry(
        Icons.report_gmailerrorred_rounded,
        'Payment needs a look',
        detail: d['message'] as String? ?? '',
        tone: Tone.danger,
      );
    default:
      return null;
  }
}

/// Event types that change server state and warrant re-fetching the request.
bool eventChangesRequest(SseEvent e) =>
    e.type != SseTypes.heartbeat &&
    e.type != SseTypes.agentMessage &&
    e.type != SseTypes.searchStarted;

/// Quote comparison for one line item.
@immutable
class LineQuote {
  const LineQuote({
    required this.lineItem,
    required this.offers,
    this.best,
    this.savingsCents = 0,
    this.runnerUp,
  });

  final LineItem lineItem;

  /// Offers ordered best first.
  final List<Offer> offers;

  /// The selected offer (or the top-ranked available one).
  final Offer? best;

  /// The next cheapest available offer after [best], if any.
  final Offer? runnerUp;

  /// How much [best] saves against [runnerUp] (0 when there is none).
  final int savingsCents;
}

/// Builds the comparison for [li]: best = the backend's selected offer, else
/// the best-ranked available offer; savings are measured against the next
/// cheapest available offer by landed cost.
LineQuote quoteFor(RequestDetail d, LineItem li) {
  final offers = d.offersFor(li.id);
  final available = offers.where((o) => o.available).toList();
  final best =
      d.selectedOffer(li) ?? (available.isNotEmpty ? available.first : null);
  if (best == null) return LineQuote(lineItem: li, offers: offers);
  final others = available.where((o) => o.id != best.id).toList()
    ..sort((a, b) => a.landedCostCents.compareTo(b.landedCostCents));
  final runnerUp = others.isEmpty ? null : others.first;
  final savings = runnerUp == null
      ? 0
      : (runnerUp.landedCostCents - best.landedCostCents).clamp(0, 1 << 52);
  return LineQuote(
    lineItem: li,
    offers: offers,
    best: best,
    runnerUp: runnerUp,
    savingsCents: savings,
  );
}

/// Sum of per-line savings across the request.
int totalSavings(RequestDetail d) =>
    d.lineItems.fold(0, (sum, li) => sum + quoteFor(d, li).savingsCents);

/// The pending approval, if the request is waiting on one.
Approval? pendingApproval(RequestDetail d) {
  for (final a in d.approvals.reversed) {
    if (a.status == ApprovalStatus.pending) return a;
  }
  return null;
}

/// True when the manager can confirm (pick an address and submit).
bool canConfirm(RequestDetail d) =>
    d.request.status == RequestStatus.quoted &&
    d.request.decision != Decision.reject &&
    d.lineItems.any((li) => li.policyDecision != Decision.reject);
