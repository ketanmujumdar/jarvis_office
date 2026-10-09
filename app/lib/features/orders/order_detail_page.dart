import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/api/models.dart';
import '../../core/format.dart';
import '../../core/providers.dart';
import '../../core/router.dart';
import '../../core/theme/tokens.dart';
import '../../core/widgets/widgets.dart';
import '../approvals/widgets/live_badge.dart';
import 'orders_metrics.dart';
import 'orders_providers.dart';
import 'widgets/audit_timeline.dart';

/// Order drill-in (`/orders/:id`): totals, items, payments, approvals and the
/// full audit timeline. Refreshes on live events for this request.
class OrderDetailPage extends ConsumerWidget {
  const OrderDetailPage({super.key, required this.requestId});
  final String requestId;

  void _refresh(WidgetRef ref) {
    ref.invalidate(orderDetailProvider(requestId));
    ref.invalidate(orderAuditProvider(requestId));
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    ref.listen(eventStreamProvider(requestId), (_, next) {
      if (next case AsyncData(:final value)
          when value.type != SseTypes.heartbeat) {
        _refresh(ref);
      }
    });
    final detail = ref.watch(orderDetailProvider(requestId));
    final audit = ref.watch(orderAuditProvider(requestId));
    final me = ref.watch(currentUserProvider);

    return PageScaffold(
      header: PageHeader(
        title: 'Order ${OrdersMetrics.shortId(requestId)}',
        subtitle: detail.value == null
            ? 'Loading order…'
            : 'Placed ${Fmt.dateTime(detail.value!.request.createdAt)}',
        leading: IconButton(
          tooltip: 'Back to orders',
          onPressed: () =>
              context.canPop() ? context.pop() : context.go(Routes.orders),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
        actions: [
          LiveBadge(requestId: requestId),
          IconButton.outlined(
            tooltip: 'Refresh',
            onPressed: () => _refresh(ref),
            icon: const Icon(Icons.refresh_rounded, size: 20),
          ),
        ],
      ),
      children: [
        AsyncValueView<RequestDetail>(
          value: detail,
          onRetry: () => _refresh(ref),
          data: (d) {
            final timeline = AppCard(
              title: 'Audit trail',
              subtitle: 'Every step, oldest first',
              leading: Icon(Icons.history_rounded, color: context.jc.brand),
              child: AsyncValueView<List<AuditEvent>>(
                value: audit,
                onRetry: () => ref.invalidate(orderAuditProvider(requestId)),
                data: (events) => AuditTimeline(
                  events: events,
                  userNames: {if (me != null) me.id: me.name},
                ),
              ),
            );
            return LayoutBuilder(
              builder: (context, c) {
                final main = _Main(detail: d);
                if (c.maxWidth < 980) {
                  return Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      main,
                      const SizedBox(height: AppSpace.xl),
                      timeline,
                    ],
                  );
                }
                return Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Expanded(flex: 3, child: main),
                    const SizedBox(width: AppSpace.xl),
                    Expanded(flex: 2, child: timeline),
                  ],
                );
              },
            );
          },
        ),
      ],
    );
  }
}

class _Main extends StatelessWidget {
  const _Main({required this.detail});
  final RequestDetail detail;

  @override
  Widget build(BuildContext context) {
    final r = detail.request;
    final paid = detail.payments
        .where((p) => p.finalCents != null)
        .fold<int>(0, (s, p) => s + p.finalCents!);
    final jc = context.jc;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        AppCard(
          highlight: StatusChip.requestTone(r.status),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Wrap(
                spacing: AppSpace.sm,
                runSpacing: AppSpace.sm,
                children: [
                  StatusChip.request(r.status),
                  StatusChip.decision(r.decision),
                ],
              ),
              if (r.rawUtterance.isNotEmpty) ...[
                const SizedBox(height: AppSpace.md),
                Text('"${r.rawUtterance}"', style: context.tt.titleMedium),
              ],
              if (r.failureReason.isNotEmpty) ...[
                const SizedBox(height: AppSpace.sm),
                Text(
                  r.failureReason,
                  style: context.tt.bodyMedium?.copyWith(color: jc.danger),
                ),
              ],
              const SizedBox(height: AppSpace.lg),
              Wrap(
                spacing: AppSpace.xxl,
                runSpacing: AppSpace.md,
                children: [
                  _Figure(label: 'Subtotal', cents: r.subtotalCents),
                  _Figure(label: 'Shipping', cents: r.shippingCents),
                  _Figure(label: 'Approved total', cents: r.totalCents),
                  if (paid > 0)
                    _Figure(label: 'Paid', cents: paid, strong: true),
                ],
              ),
              if (detail.address != null) ...[
                const SizedBox(height: AppSpace.lg),
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Icon(Icons.place_outlined, size: 18, color: jc.textMuted),
                    const SizedBox(width: AppSpace.sm),
                    Expanded(
                      child: Text(
                        '${detail.address!.label} · ${detail.address!.oneLine}',
                        style: context.tt.bodySmall,
                      ),
                    ),
                  ],
                ),
              ],
            ],
          ),
        ),
        const SizedBox(height: AppSpace.lg),
        AppCard(
          title: 'Items',
          subtitle:
              '${detail.lineItems.length} '
              '${detail.lineItems.length == 1 ? 'line' : 'lines'}',
          child: Column(
            children: [
              for (final (i, li) in detail.lineItems.indexed) ...[
                if (i > 0) Divider(height: AppSpace.xl, color: jc.border),
                _LineRow(item: li, offer: detail.selectedOffer(li)),
              ],
            ],
          ),
        ),
        if (detail.payments.isNotEmpty) ...[
          const SizedBox(height: AppSpace.lg),
          AppCard(
            title: 'Payments',
            subtitle: 'One checkout per merchant via Reap',
            child: Column(
              children: [
                for (final (i, p) in detail.payments.indexed) ...[
                  if (i > 0) Divider(height: AppSpace.xl, color: jc.border),
                  _PaymentRow(payment: p),
                ],
              ],
            ),
          ),
        ],
        if (detail.approvals.isNotEmpty) ...[
          const SizedBox(height: AppSpace.lg),
          AppCard(
            title: 'Approvals',
            child: Column(
              children: [
                for (final a in detail.approvals)
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: AppSpace.xs),
                    child: Row(
                      children: [
                        StatusChip.approval(a.status),
                        const SizedBox(width: AppSpace.md),
                        Expanded(
                          child: Text(
                            [
                              a.isPriceDrift ? 'Price change' : 'Policy',
                              Fmt.money(a.amountCents),
                              if (a.comment.isNotEmpty) '"${a.comment}"',
                            ].join(' · '),
                            style: context.tt.bodyMedium,
                          ),
                        ),
                        Text(
                          Fmt.date(a.decidedAt ?? a.createdAt),
                          style: context.tt.bodySmall,
                        ),
                      ],
                    ),
                  ),
              ],
            ),
          ),
        ],
      ],
    );
  }
}

class _Figure extends StatelessWidget {
  const _Figure({
    required this.label,
    required this.cents,
    this.strong = false,
  });
  final String label;
  final int cents;
  final bool strong;

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    mainAxisSize: MainAxisSize.min,
    children: [
      Text(label.toUpperCase(), style: context.tt.labelSmall),
      const SizedBox(height: AppSpace.xxs),
      MoneyText(
        cents,
        style: (strong ? context.tt.titleLarge : context.tt.titleMedium)
            ?.copyWith(
              color: strong ? context.jc.success : null,
              fontWeight: FontWeight.w700,
            ),
      ),
    ],
  );
}

class _LineRow extends StatelessWidget {
  const _LineRow({required this.item, required this.offer});
  final LineItem item;
  final Offer? offer;

  @override
  Widget build(BuildContext context) {
    final o = offer;
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(item.description, style: context.tt.titleSmall),
              const SizedBox(height: AppSpace.xxs),
              Text(
                [
                  'Qty ${item.qty}',
                  if (o != null) o.merchantName,
                  if (o != null && o.title.isNotEmpty) o.title,
                ].join(' · '),
                style: context.tt.bodySmall,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
              ),
            ],
          ),
        ),
        const SizedBox(width: AppSpace.md),
        if (o != null)
          MoneyText(o.landedCostCents, style: context.tt.titleSmall)
        else
          Text('No offer', style: context.tt.bodySmall),
      ],
    );
  }
}

class _PaymentRow extends StatelessWidget {
  const _PaymentRow({required this.payment});
  final Payment payment;

  @override
  Widget build(BuildContext context) {
    final p = payment;
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(p.merchantName, style: context.tt.titleSmall),
              const SizedBox(height: AppSpace.xxs),
              Text(
                [
                  if (p.reapOrderId.isNotEmpty) 'Order ${p.reapOrderId}',
                  'Items ${Fmt.money(p.itemsCents)}',
                  'Shipping ${Fmt.money(p.shippingCents)}',
                  if (p.error.isNotEmpty) p.error,
                ].join(' · '),
                style: context.tt.bodySmall,
              ),
            ],
          ),
        ),
        const SizedBox(width: AppSpace.md),
        Column(
          crossAxisAlignment: CrossAxisAlignment.end,
          children: [
            MoneyText(
              p.finalCents ?? p.quotedCents,
              style: context.tt.titleSmall,
            ),
            const SizedBox(height: AppSpace.xs),
            StatusChip.payment(p.status),
          ],
        ),
      ],
    );
  }
}
