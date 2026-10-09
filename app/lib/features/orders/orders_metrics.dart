import '../../core/api/models.dart';

/// Pure calculations behind the orders KPI tiles (unit-tested).
abstract final class OrdersMetrics {
  /// Amount actually charged for an order: the sum of final amounts when every
  /// payment has completed, otherwise the request total.
  static int orderTotalCents(OrderSummary o) {
    final ps = o.payments;
    if (ps.isNotEmpty && ps.every((p) => p.finalCents != null)) {
      return ps.fold(0, (s, p) => s + p.finalCents!);
    }
    return o.request.totalCents;
  }

  /// Share of decided orders that went through without a human approval,
  /// as 0..1, or null when there is nothing to measure.
  static double? autoApproveRate(List<OrderSummary> orders) {
    final decided = orders
        .where((o) => o.request.decision != Decision.none)
        .toList();
    if (decided.isEmpty) return null;
    final auto = decided
        .where((o) => o.request.decision == Decision.autoApprove)
        .length;
    return auto / decided.length;
  }

  /// Savings on one request: for each line, the cheapest available
  /// alternative's landed cost minus the selected offer's landed cost
  /// (never negative). Lines without a selected offer or alternatives count 0.
  static int savingsCents(RequestDetail d) {
    var total = 0;
    for (final li in d.lineItems) {
      final sel = d.selectedOffer(li);
      if (sel == null) continue;
      final alts = d
          .offersFor(li.id)
          .where((o) => o.id != sel.id && o.available)
          .toList();
      if (alts.isEmpty) continue;
      final next = alts
          .map((o) => o.landedCostCents)
          .reduce((a, b) => a < b ? a : b);
      if (next > sel.landedCostCents) total += next - sel.landedCostCents;
    }
    return total;
  }

  /// Whether the order belongs to a filter bucket.
  static bool matches(OrderSummary o, OrderFilter f) => switch (f) {
    OrderFilter.all => true,
    OrderFilter.active => !o.request.status.isTerminal,
    OrderFilter.ordered => o.request.status == RequestStatus.ordered,
    OrderFilter.issues =>
      o.request.status == RequestStatus.failed ||
          o.request.status == RequestStatus.rejected ||
          o.request.status == RequestStatus.cancelled,
  };

  /// Case-insensitive text search over utterance, vendors, people and ids.
  static bool search(OrderSummary o, String q) {
    final needle = q.trim().toLowerCase();
    if (needle.isEmpty) return true;
    final hay = [
      o.request.rawUtterance,
      o.request.id,
      ...o.vendors,
      o.requester?.name ?? '',
      o.approver?.name ?? '',
      for (final p in o.payments) p.reapOrderId,
    ].join(' ').toLowerCase();
    return hay.contains(needle);
  }

  /// Short display title for an order.
  static String title(OrderSummary o) {
    final u = o.request.rawUtterance.trim();
    if (u.isNotEmpty) return u;
    if (o.vendors.isNotEmpty) return 'Order from ${o.vendors.first}';
    return 'Order ${shortId(o.request.id)}';
  }

  static String shortId(String id) =>
      id.length <= 8 ? id.toUpperCase() : id.substring(0, 8).toUpperCase();
}

enum OrderFilter {
  all('All'),
  active('In progress'),
  ordered('Ordered'),
  issues('Issues');

  const OrderFilter(this.label);
  final String label;
}

/// Aggregate savings across recent orders.
class SavingsSummary {
  const SavingsSummary({required this.totalCents, required this.orders});
  final int totalCents;
  final int orders;
}
