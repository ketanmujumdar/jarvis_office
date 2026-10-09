import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/api/models.dart';
import '../../core/format.dart';
import '../../core/providers.dart';
import '../../core/theme/tokens.dart';
import '../../core/widgets/widgets.dart';
import '../approvals/widgets/live_badge.dart';
import 'orders_metrics.dart';
import 'orders_providers.dart';
import 'widgets/orders_table.dart';
import 'widgets/spend_chart.dart';

/// Path of the order drill-in route (registered under the orders branch).
String orderPath(String requestId) => '/orders/$requestId';

void _invalidateAll(WidgetRef ref) {
  ref.invalidate(ordersProvider);
  ref.invalidate(spendProvider);
  ref.invalidate(savingsProvider);
}

/// Orders: KPI tiles, monthly spend vs budget, and the order history table
/// with drill-in to each order's audit trail. Updates live.
class OrdersPage extends ConsumerWidget {
  const OrdersPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    ref.listen(eventStreamProvider(null), (_, next) {
      if (next case AsyncData(:final value)
          when orderEventTypes.contains(value.type)) {
        _invalidateAll(ref);
      }
    });
    final orders = ref.watch(ordersProvider);
    final spend = ref.watch(spendProvider);

    return PageScaffold(
      header: PageHeader(
        title: 'Orders',
        subtitle: 'History, spend against budget and audit trail.',
        actions: [
          const LiveBadge(),
          IconButton.outlined(
            key: const Key('orders-refresh'),
            tooltip: 'Refresh',
            onPressed: () => _invalidateAll(ref),
            icon: const Icon(Icons.refresh_rounded, size: 20),
          ),
        ],
      ),
      children: [
        const _KpiRow(),
        const SizedBox(height: AppSpace.xl),
        AsyncValueView<SpendSummary>(
          value: spend,
          onRetry: () => ref.invalidate(spendProvider),
          loading: const _Skeleton(height: 340),
          data: (s) => SpendChartCard(spend: s),
        ),
        const SizedBox(height: AppSpace.xl),
        AsyncValueView<List<OrderSummary>>(
          value: orders,
          onRetry: () => ref.invalidate(ordersProvider),
          loading: const _Skeleton(height: 240),
          data: (list) => _History(orders: list),
        ),
      ],
    );
  }
}

class _Skeleton extends StatelessWidget {
  const _Skeleton({required this.height});
  final double height;

  @override
  Widget build(BuildContext context) => Container(
    height: height,
    decoration: BoxDecoration(
      color: context.jc.surfaceMuted,
      borderRadius: AppRadius.lgAll,
      border: Border.all(color: context.jc.border),
    ),
  );
}

class _KpiRow extends ConsumerWidget {
  const _KpiRow();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final spend = ref.watch(spendProvider).value;
    final orders = ref.watch(ordersProvider).value;
    final savings = ref.watch(savingsProvider);

    final rate = orders == null ? null : OrdersMetrics.autoApproveRate(orders);
    final completed = orders
        ?.where((o) => o.request.status == RequestStatus.ordered)
        .length;
    final active = orders?.where((o) => !o.request.status.isTerminal).length;

    final tiles = <StatTile>[
      StatTile(
        key: const Key('kpi-spend'),
        label: 'Spend this month',
        value: spend == null ? '—' : Fmt.money(spend.monthToDateCents),
        caption: spend == null
            ? 'Loading…'
            : '${Fmt.percent(spend.monthToDateCents, spend.monthlyBudgetCents)}'
                  ' of ${Fmt.moneyCompact(spend.monthlyBudgetCents)} budget',
        icon: Icons.payments_outlined,
        tone:
            spend != null &&
                spend.monthlyBudgetCents > 0 &&
                spend.monthToDateCents >= spend.monthlyBudgetCents
            ? Tone.danger
            : Tone.brand,
      ),
      StatTile(
        key: const Key('kpi-savings'),
        label: 'Savings',
        value: switch (savings) {
          AsyncData(:final value) => Fmt.money(value.totalCents),
          AsyncError() => '—',
          _ => '…',
        },
        caption: switch (savings) {
          AsyncData(:final value) =>
            'vs next-best quote · ${value.orders} '
                '${value.orders == 1 ? 'order' : 'orders'}',
          AsyncError() => 'Unavailable',
          _ => 'Comparing quotes…',
        },
        icon: Icons.savings_outlined,
        tone: Tone.success,
      ),
      StatTile(
        key: const Key('kpi-auto'),
        label: 'Auto-approved',
        value: rate == null ? '—' : '${(rate * 100).round()}%',
        caption: 'Orders within policy',
        icon: Icons.bolt_rounded,
        tone: Tone.info,
      ),
      StatTile(
        key: const Key('kpi-orders'),
        label: 'Orders',
        value: completed == null ? '—' : '$completed',
        caption: active == null ? '' : '$active in progress',
        icon: Icons.inventory_2_outlined,
        tone: Tone.neutral,
      ),
    ];

    return LayoutBuilder(
      builder: (context, c) {
        // 4 across on desktop, a 2x2 grid otherwise (compact tiles on phones).
        final cols = c.maxWidth >= 1000 ? 4 : 2;
        final compact = c.maxWidth < 600;
        final gap = compact ? AppSpace.md : AppSpace.lg;
        final w = (c.maxWidth - gap * (cols - 1)) / cols;
        return Wrap(
          spacing: gap,
          runSpacing: gap,
          children: [
            for (final t in tiles)
              SizedBox(width: w, child: compact ? t.asCompact() : t),
          ],
        );
      },
    );
  }
}

class _History extends ConsumerWidget {
  const _History({required this.orders});
  final List<OrderSummary> orders;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final filter = ref.watch(orderFilterProvider);
    final q = ref.watch(orderSearchProvider);
    final visible = orders
        .where((o) => OrdersMetrics.matches(o, filter))
        .where((o) => OrdersMetrics.search(o, q))
        .toList();
    final counts = {
      for (final f in OrderFilter.values)
        f: orders.where((o) => OrdersMetrics.matches(o, f)).length,
    };

    return AppCard(
      padding: EdgeInsets.zero,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.all(AppSpace.lg),
            child: LayoutBuilder(
              builder: (context, c) {
                final narrow = c.maxWidth < 600;
                final title = Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text('Order history', style: context.tt.titleMedium),
                    const SizedBox(height: AppSpace.xxs),
                    Text(
                      'Select an order to see its audit trail.',
                      style: context.tt.bodySmall,
                    ),
                  ],
                );
                final search = TextField(
                  key: const Key('orders-search'),
                  decoration: const InputDecoration(
                    isDense: true,
                    prefixIcon: Icon(Icons.search_rounded, size: 18),
                    hintText: 'Search orders',
                  ),
                  onChanged: (v) =>
                      ref.read(orderSearchProvider.notifier).set(v),
                );
                final chips = Wrap(
                  spacing: AppSpace.sm,
                  runSpacing: AppSpace.sm,
                  crossAxisAlignment: WrapCrossAlignment.center,
                  children: [
                    for (final f in OrderFilter.values)
                      ChoiceChip(
                        key: Key('filter-${f.name}'),
                        label: Text('${f.label} ${counts[f]}'),
                        selected: f == filter,
                        showCheckmark: false,
                        onSelected: (_) =>
                            ref.read(orderFilterProvider.notifier).set(f),
                      ),
                    if (!narrow) SizedBox(width: 220, child: search),
                  ],
                );
                if (narrow) {
                  // Phones: title, then a full-width search, then the filters.
                  return Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      title,
                      const SizedBox(height: AppSpace.md),
                      search,
                      const SizedBox(height: AppSpace.md),
                      chips,
                    ],
                  );
                }
                return Wrap(
                  spacing: AppSpace.lg,
                  runSpacing: AppSpace.md,
                  crossAxisAlignment: WrapCrossAlignment.center,
                  alignment: WrapAlignment.spaceBetween,
                  children: [title, chips],
                );
              },
            ),
          ),
          Divider(height: 1, color: context.jc.border),
          if (visible.isEmpty)
            EmptyState(
              icon: orders.isEmpty
                  ? Icons.inventory_2_outlined
                  : Icons.filter_alt_off_outlined,
              title: orders.isEmpty ? 'No orders yet' : 'No matching orders',
              message: orders.isEmpty
                  ? 'Orders appear here once a request reaches checkout.'
                  : 'Try another filter or search term.',
              compact: true,
            )
          else
            Padding(
              padding: const EdgeInsets.only(bottom: AppSpace.xs),
              child: OrdersTable(
                orders: visible,
                onOpen: (o) => context.push(orderPath(o.request.id)),
              ),
            ),
        ],
      ),
    );
  }
}
