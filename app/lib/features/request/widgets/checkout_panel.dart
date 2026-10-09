import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api/models.dart';
import '../../../core/format.dart';
import '../../../core/theme/tokens.dart';
import '../../../core/widgets/widgets.dart';
import '../request_controller.dart';
import '../request_logic.dart';
import 'progress_tracker.dart';

/// Approval and payment state after the manager confirmed: waiting for an
/// approver, price-drift re-approval, Reap payment approval links per
/// merchant, and the final order confirmation.
class CheckoutPanel extends ConsumerStatefulWidget {
  const CheckoutPanel({super.key, required this.detail});
  final RequestDetail detail;

  @override
  ConsumerState<CheckoutPanel> createState() => _CheckoutPanelState();
}

class _CheckoutPanelState extends ConsumerState<CheckoutPanel> {
  bool _refreshing = false;

  Future<void> _refresh() async {
    setState(() => _refreshing = true);
    try {
      await ref
          .read(requestDetailProvider(widget.detail.request.id).notifier)
          .refreshCheckout();
    } catch (_) {
      // The status chip keeps the last known state; SSE will catch up.
    } finally {
      if (mounted) setState(() => _refreshing = false);
    }
  }

  void _open(String url) {
    final ok = ref.read(urlOpenerProvider)(url);
    if (!ok && mounted) {
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(
        SnackBar(content: Text('Open this link to approve: $url')),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final d = widget.detail;
    final r = d.request;
    final pending = pendingApproval(d);
    final children = <Widget>[];

    if (d.address != null) {
      children.add(_AddressSummary(address: d.address!));
      children.add(const SizedBox(height: AppSpace.md));
    }

    switch (r.status) {
      case RequestStatus.pendingApproval:
        children.add(
          pending != null && pending.isPriceDrift
              ? Callout(
                  tone: Tone.warning,
                  icon: Icons.trending_up_rounded,
                  title: 'The live price changed',
                  message:
                      'Approved ${Fmt.money(pending.prevCents ?? 0)}, live total ${Fmt.money(pending.amountCents)}. An approver must re-approve.',
                )
              : Callout(
                  tone: Tone.warning,
                  icon: Icons.hourglass_top_rounded,
                  title: 'Waiting for an approver',
                  message: pending == null
                      ? 'This order is over the auto-approve limits.'
                      : pending.reasons.map((x) => x.message).join(' · '),
                ),
        );
      case RequestStatus.approved || RequestStatus.checkingOut:
        children.add(
          const Callout(
            tone: Tone.info,
            icon: Icons.request_quote_outlined,
            title: 'Getting a live quote from Reap',
            message: 'Prices are re-checked before you approve the payment.',
          ),
        );
      case RequestStatus.ordered:
        children.add(
          Callout(
            tone: Tone.success,
            icon: Icons.celebration_outlined,
            title: 'Order placed',
            message:
                'Paid ${Fmt.money(d.payments.fold(0, (s, p) => s + (p.finalCents ?? p.quotedCents)))} across ${d.payments.length} merchant order(s).',
          ),
        );
      case RequestStatus.failed || RequestStatus.cancelled
          when d.payments.any(
            (p) =>
                p.reapCheckoutId.isNotEmpty &&
                (p.status == PaymentStatus.requiresAction ||
                    p.status == PaymentStatus.processing),
          ):
        children.add(
          const Callout(
            tone: Tone.danger,
            icon: Icons.link_off_rounded,
            title: 'Do not approve the remaining Reap payment',
            message:
                'This request has ended, so its payment links are disabled here. '
                'If a payment is approved on Reap anyway, Jarvis records it and alerts an admin.',
          ),
        );
      case RequestStatus.rejected:
        children.add(
          Callout(
            tone: Tone.danger,
            icon: Icons.block_rounded,
            title: 'Rejected',
            message: d.approvals.isEmpty ? null : d.approvals.last.comment,
          ),
        );
      default:
        break;
    }

    if (d.payments.isNotEmpty) {
      if (children.isNotEmpty) {
        children.add(const SizedBox(height: AppSpace.md));
      }
      for (final p in d.payments) {
        children.add(
          _PaymentRow(
            payment: p,
            onOpen: _open,
            // Approval links only while the request is waiting for payment.
            canApprove:
                r.status == RequestStatus.awaitingPayment ||
                r.status == RequestStatus.paying,
          ),
        );
      }
    }

    final payingStates = {
      RequestStatus.awaitingPayment,
      RequestStatus.paying,
      RequestStatus.checkingOut,
    };
    return SectionCard(
      title: 'Approval & payment',
      subtitle: r.status == RequestStatus.awaitingPayment
          ? 'Approve each charge on Reap’s secure page.'
          : 'Status updates live as Reap processes the order.',
      icon: Icons.lock_outline_rounded,
      highlight: r.status == RequestStatus.awaitingPayment
          ? Tone.warning
          : null,
      trailing: payingStates.contains(r.status)
          ? IconButton(
              tooltip: 'Refresh payment status',
              onPressed: _refreshing ? null : _refresh,
              icon: _refreshing
                  ? const SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.refresh_rounded),
            )
          : null,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: children.isEmpty
            ? [Text('Nothing to approve yet.', style: context.tt.bodySmall)]
            : children,
      ),
    );
  }
}

class _AddressSummary extends StatelessWidget {
  const _AddressSummary({required this.address});
  final Address address;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return Row(
      children: [
        Icon(Icons.place_outlined, size: 18, color: jc.textMuted),
        const SizedBox(width: AppSpace.sm),
        Expanded(
          child: Text.rich(
            TextSpan(
              children: [
                TextSpan(
                  text: '${address.label}  ',
                  style: const TextStyle(fontWeight: FontWeight.w600),
                ),
                TextSpan(text: address.oneLine),
              ],
            ),
            style: context.tt.bodySmall,
          ),
        ),
      ],
    );
  }
}

class _PaymentRow extends StatelessWidget {
  const _PaymentRow({
    required this.payment,
    required this.onOpen,
    required this.canApprove,
  });
  final Payment payment;
  final void Function(String url) onOpen;
  final bool canApprove;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final p = payment;
    final needsAction =
        canApprove &&
        p.status == PaymentStatus.requiresAction &&
        p.approvalUrl.isNotEmpty;
    final amount = p.finalCents ?? p.quotedCents;
    return Container(
      margin: const EdgeInsets.only(bottom: AppSpace.sm),
      padding: const EdgeInsets.all(AppSpace.md),
      decoration: BoxDecoration(
        color: jc.surfaceMuted,
        borderRadius: AppRadius.mdAll,
        border: Border.all(color: jc.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      p.merchantName,
                      style: context.tt.bodyMedium?.copyWith(
                        fontWeight: FontWeight.w600,
                        color: jc.textPrimary,
                      ),
                    ),
                    const SizedBox(height: AppSpace.xxs),
                    Text(
                      [
                        'Items ${Fmt.money(p.itemsCents)}',
                        'Shipping ${Fmt.money(p.shippingCents)}',
                        if (p.taxCents > 0) 'Tax ${Fmt.money(p.taxCents)}',
                        if (p.reapOrderId.isNotEmpty) 'Order ${p.reapOrderId}',
                      ].join(' · '),
                      style: context.tt.bodySmall,
                    ),
                  ],
                ),
              ),
              const SizedBox(width: AppSpace.sm),
              Column(
                crossAxisAlignment: CrossAxisAlignment.end,
                children: [
                  MoneyText(
                    amount,
                    style: context.tt.titleMedium?.copyWith(
                      color: jc.textPrimary,
                    ),
                  ),
                  const SizedBox(height: AppSpace.xs),
                  StatusChip.payment(p.status),
                ],
              ),
            ],
          ),
          if (p.error.isNotEmpty) ...[
            const SizedBox(height: AppSpace.sm),
            Text(
              p.error,
              style: context.tt.bodySmall?.copyWith(color: jc.danger),
            ),
          ],
          if (needsAction) ...[
            const SizedBox(height: AppSpace.md),
            Align(
              alignment: Alignment.centerLeft,
              child: FilledButton.icon(
                key: ValueKey('approve-payment-${p.id}'),
                onPressed: () => onOpen(p.approvalUrl),
                icon: const Icon(Icons.open_in_new_rounded, size: 18),
                label: const Text('Approve payment on Reap'),
              ),
            ),
          ],
        ],
      ),
    );
  }
}
