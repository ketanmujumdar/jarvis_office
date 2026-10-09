import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/models.dart';
import '../../core/providers.dart';
import 'orders_metrics.dart';

/// SSE event types that change order history or spend.
const orderEventTypes = {
  SseTypes.requestStatusChanged,
  SseTypes.paymentStatusChanged,
  SseTypes.orderCompleted,
  SseTypes.approvalDecided,
  SseTypes.checkoutQuoted,
};

/// Order history, newest first.
final ordersProvider = FutureProvider.autoDispose<List<OrderSummary>>(
  (ref) => ref.watch(apiProvider).listOrders(limit: 200),
  retry: (_, _) => null,
);

/// Monthly completed spend vs budget for the last six months.
final spendProvider = FutureProvider.autoDispose<SpendSummary>(
  (ref) => ref.watch(apiProvider).monthlySpend(months: 6),
  retry: (_, _) => null,
);

/// Savings vs the next-best quote over the most recent completed orders.
/// Fetches request details (bounded) and tolerates individual failures.
final savingsProvider = FutureProvider.autoDispose<SavingsSummary>((ref) async {
  final api = ref.watch(apiProvider);
  final orders = await ref.watch(ordersProvider.future);
  final done = orders
      .where((o) => o.request.status == RequestStatus.ordered)
      .take(25)
      .toList();
  final results = await Future.wait(
    done.map((o) async {
      try {
        return OrdersMetrics.savingsCents(await api.getRequest(o.request.id));
      } catch (_) {
        return 0;
      }
    }),
  );
  return SavingsSummary(
    totalCents: results.fold(0, (s, v) => s + v),
    orders: done.length,
  );
}, retry: (_, _) => null);

/// One request with lines, offers, approvals and payments (order drill-in).
final orderDetailProvider = FutureProvider.autoDispose
    .family<RequestDetail, String>(
      (ref, id) => ref.watch(apiProvider).getRequest(id),
      retry: (_, _) => null,
    );

/// Audit trail for one request, oldest first.
final orderAuditProvider = FutureProvider.autoDispose
    .family<List<AuditEvent>, String>((ref, id) async {
      final events = await ref.watch(apiProvider).requestAudit(id);
      return [...events]..sort((a, b) {
        final c = a.at.compareTo(b.at);
        return c != 0 ? c : a.id.compareTo(b.id);
      });
    }, retry: (_, _) => null);

class OrderFilterController extends Notifier<OrderFilter> {
  @override
  OrderFilter build() => OrderFilter.all;
  void set(OrderFilter f) => state = f;
}

final orderFilterProvider =
    NotifierProvider<OrderFilterController, OrderFilter>(
      OrderFilterController.new,
    );

class OrderSearchController extends Notifier<String> {
  @override
  String build() => '';
  void set(String q) => state = q;
}

final orderSearchProvider = NotifierProvider<OrderSearchController, String>(
  OrderSearchController.new,
);
