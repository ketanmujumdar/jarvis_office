import 'package:flutter/material.dart';

import '../../../core/api/models.dart';
import '../../../core/format.dart';
import '../../../core/theme/tokens.dart';

/// Presentation of one audit event: icon, tone, title and an optional detail.
class AuditCopy {
  const AuditCopy(this.title, this.icon, this.tone, [this.detail = '']);
  final String title;
  final IconData icon;
  final Tone tone;
  final String detail;

  static String _s(Json p, String k) {
    final v = p[k];
    return v == null ? '' : '$v';
  }

  static int? _i(Json p, String k) {
    final v = p[k];
    return v is num ? v.toInt() : null;
  }

  static String _status(Object? v) {
    final s = RequestStatus.fromJson(v);
    return s == RequestStatus.unknown ? '${v ?? ''}' : s.label;
  }

  /// Maps an audit type (see `backend/internal/audit`) to copy. Payloads are
  /// read defensively; unknown types fall back to a humanised type name.
  static AuditCopy of(AuditEvent e) {
    final p = e.payload;
    switch (e.type) {
      case 'request.created':
        final u = _s(p, 'utterance').isNotEmpty
            ? _s(p, 'utterance')
            : _s(p, 'raw_utterance');
        return AuditCopy(
          'Request created',
          Icons.add_comment_outlined,
          Tone.brand,
          u.isEmpty ? '' : '"$u"',
        );
      case 'request.status_changed':
        final from = p['from'];
        final to = p['to'];
        final reason = _s(p, 'failure_reason');
        final toStatus = RequestStatus.fromJson(to);
        return AuditCopy(
          'Status: ${_status(to)}',
          Icons.sync_alt_rounded,
          toStatus == RequestStatus.failed || toStatus == RequestStatus.rejected
              ? Tone.danger
              : (toStatus == RequestStatus.ordered
                    ? Tone.success
                    : Tone.neutral),
          [
            if (from != null) 'From ${_status(from)}',
            if (reason.isNotEmpty) reason,
          ].join(' · '),
        );
      case 'request.confirmed':
        return const AuditCopy(
          'Confirmed by requester',
          Icons.how_to_reg_outlined,
          Tone.brand,
          'Delivery address chosen',
        );
      case 'request.cancelled':
        return const AuditCopy(
          'Request cancelled',
          Icons.block_rounded,
          Tone.neutral,
        );
      case 'line_items.parsed':
        final items = p['line_items'] ?? p['items'];
        final n = items is List ? items.length : _i(p, 'count');
        return AuditCopy(
          'Items understood',
          Icons.auto_awesome_outlined,
          Tone.info,
          n == null ? '' : '$n ${n == 1 ? 'item' : 'items'}',
        );
      case 'search.completed':
        final err = _s(p, 'error');
        final offers = p['offers'];
        final n = offers is List ? offers.length : _i(p, 'offers');
        return AuditCopy(
          'Searched ${_s(p, 'vendor_domain').isEmpty ? 'vendor' : _s(p, 'vendor_domain')}',
          err.isEmpty ? Icons.travel_explore_rounded : Icons.search_off_rounded,
          err.isEmpty ? Tone.info : Tone.warning,
          err.isNotEmpty
              ? err
              : (n == null ? '' : '$n ${n == 1 ? 'offer' : 'offers'} found'),
        );
      case 'policy.evaluated':
        final d = Decision.fromJson(p['decision']);
        final total = _i(p, 'total_cents');
        return AuditCopy(
          'Policy: ${d.label}',
          Icons.policy_outlined,
          switch (d) {
            Decision.autoApprove => Tone.success,
            Decision.needsApproval => Tone.warning,
            Decision.reject => Tone.danger,
            Decision.none => Tone.neutral,
          },
          total == null ? '' : 'Total ${Fmt.money(total)}',
        );
      case 'approval.requested':
        final a = p['approval'] is Json ? p['approval'] as Json : p;
        final amt = _i(a, 'amount_cents');
        return AuditCopy(
          'Approval requested',
          Icons.pending_actions_rounded,
          Tone.warning,
          amt == null ? '' : Fmt.money(amt),
        );
      case 'approval.decided':
        final a = p['approval'] is Json ? p['approval'] as Json : p;
        final st = ApprovalStatus.fromJson(a['status']);
        final comment = _s(a, 'comment');
        return AuditCopy(
          st == ApprovalStatus.rejected ? 'Rejected' : 'Approved',
          st == ApprovalStatus.rejected
              ? Icons.cancel_outlined
              : Icons.check_circle_outline_rounded,
          st == ApprovalStatus.rejected ? Tone.danger : Tone.success,
          comment.isEmpty ? '' : '"$comment"',
        );
      case 'reap.quote_created':
        final c = _i(p, 'final_cents');
        return AuditCopy(
          'Live quote received',
          Icons.request_quote_outlined,
          Tone.info,
          c == null ? '' : Fmt.money(c),
        );
      case 'reap.price_drift':
        final a = _i(p, 'approved_cents');
        final l = _i(p, 'live_cents');
        return AuditCopy(
          'Price changed at checkout',
          Icons.swap_vert_rounded,
          Tone.warning,
          a == null || l == null ? '' : '${Fmt.money(a)} → ${Fmt.money(l)}',
        );
      case 'reap.checkout_created':
        return const AuditCopy(
          'Checkout created',
          Icons.shopping_cart_checkout_rounded,
          Tone.brand,
          'Waiting for payment approval on Reap',
        );
      case 'reap.checkout_status':
        final st = _s(p, 'status');
        final ok = st.toUpperCase() == 'COMPLETED';
        final bad =
            st.toUpperCase() == 'FAILED' || st.toUpperCase() == 'EXPIRED';
        return AuditCopy(
          'Payment ${st.isEmpty ? 'updated' : st.toLowerCase()}',
          Icons.credit_card_rounded,
          ok ? Tone.success : (bad ? Tone.danger : Tone.info),
          _s(p, 'order_id'),
        );
      case 'order.completed':
        return const AuditCopy(
          'Order placed',
          Icons.local_shipping_outlined,
          Tone.success,
        );
      case 'agent.tool_call':
        return AuditCopy(
          'Agent used ${_s(p, 'tool').isEmpty ? 'a tool' : _s(p, 'tool')}',
          Icons.smart_toy_outlined,
          Tone.neutral,
        );
      default:
        final t = e.type.replaceAll(RegExp('[._]'), ' ').trim();
        return AuditCopy(
          t.isEmpty ? 'Event' : t[0].toUpperCase() + t.substring(1),
          Icons.circle_outlined,
          Tone.neutral,
        );
    }
  }
}

String actorLabel(AuditEvent e, {Map<String, String> names = const {}}) =>
    switch (e.actorType) {
      'agent' => 'Jarvis',
      'system' => 'System',
      'user' => names[e.actorId] ?? 'User',
      _ => e.actorType.isEmpty ? 'System' : e.actorType,
    };

/// Vertical timeline of audit events (oldest at the top).
class AuditTimeline extends StatelessWidget {
  const AuditTimeline({
    super.key,
    required this.events,
    this.userNames = const {},
  });

  final List<AuditEvent> events;

  /// Optional id → display name for user actors.
  final Map<String, String> userNames;

  @override
  Widget build(BuildContext context) {
    if (events.isEmpty) {
      return Text('No activity recorded yet.', style: context.tt.bodyMedium);
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        for (final (i, e) in events.indexed)
          _TimelineRow(
            event: e,
            isLast: i == events.length - 1,
            actor: actorLabel(e, names: userNames),
          ),
      ],
    );
  }
}

class _TimelineRow extends StatelessWidget {
  const _TimelineRow({
    required this.event,
    required this.isLast,
    required this.actor,
  });

  final AuditEvent event;
  final bool isLast;
  final String actor;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final copy = AuditCopy.of(event);
    return IntrinsicHeight(
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          SizedBox(
            width: 32,
            child: Column(
              children: [
                Container(
                  width: 28,
                  height: 28,
                  decoration: BoxDecoration(
                    color: jc.bg(copy.tone),
                    shape: BoxShape.circle,
                    border: Border.all(
                      color: jc.fg(copy.tone).withValues(alpha: 0.25),
                    ),
                  ),
                  child: Icon(copy.icon, size: 15, color: jc.fg(copy.tone)),
                ),
                if (!isLast)
                  Expanded(
                    child: Container(
                      width: 2,
                      margin: const EdgeInsets.symmetric(vertical: 2),
                      color: jc.border,
                    ),
                  ),
              ],
            ),
          ),
          const SizedBox(width: AppSpace.md),
          Expanded(
            child: Padding(
              padding: EdgeInsets.only(
                top: AppSpace.xs,
                bottom: isLast ? 0 : AppSpace.lg,
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Expanded(
                        child: Text(copy.title, style: context.tt.titleSmall),
                      ),
                      const SizedBox(width: AppSpace.sm),
                      Text(
                        Fmt.dateTime(event.at),
                        style: context.tt.labelSmall?.copyWith(
                          color: jc.textMuted,
                        ),
                      ),
                    ],
                  ),
                  if (copy.detail.isNotEmpty) ...[
                    const SizedBox(height: AppSpace.xxs),
                    Text(copy.detail, style: context.tt.bodySmall),
                  ],
                  const SizedBox(height: AppSpace.xxs),
                  Text(
                    'by $actor',
                    style: context.tt.labelSmall?.copyWith(color: jc.textMuted),
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}
