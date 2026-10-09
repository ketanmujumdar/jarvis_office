import 'package:fl_chart/fl_chart.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:jarvis_office/core/api/models.dart';
import 'package:jarvis_office/core/theme/tokens.dart';
import 'package:jarvis_office/features/orders/order_detail_page.dart';
import 'package:jarvis_office/features/orders/orders_metrics.dart';
import 'package:jarvis_office/features/orders/orders_page.dart';
import 'package:jarvis_office/features/orders/widgets/audit_timeline.dart';

import '../../helpers.dart';
import '../approvals/fixtures.dart';

Payment payment({
  String id = 'pay-1',
  String requestId = 'req-1',
  String merchant = 'ErgoTune',
  int quoted = 64900,
  int? finalCents,
  PaymentStatus status = PaymentStatus.completed,
}) => Payment(
  id: id,
  requestId: requestId,
  merchantName: merchant,
  itemsCents: quoted - 1000,
  shippingCents: 1000,
  taxCents: 0,
  quotedCents: quoted,
  finalCents: finalCents,
  currency: 'SGD',
  status: status,
  reapOrderId: finalCents == null ? '' : 'R-$id',
);

OrderSummary order({
  required String id,
  required RequestStatus status,
  Decision decision = Decision.autoApprove,
  int total = 12000,
  int? finalCents,
  List<String> vendors = const ['Common Man Coffee'],
  User? approver,
  String utterance = 'Coffee beans',
}) => OrderSummary(
  request: request(
    id: id,
    status: status,
    decision: decision,
    totalCents: total,
    utterance: utterance,
  ),
  requester: maya,
  approver: approver,
  vendors: vendors,
  itemCount: 2,
  payments: [
    payment(
      id: 'pay-$id',
      requestId: id,
      quoted: total,
      finalCents: finalCents,
      status: finalCents == null
          ? PaymentStatus.requiresAction
          : PaymentStatus.completed,
    ),
  ],
);

RequestDetail detailWithOffers(String id, {int selected = 1000}) =>
    RequestDetail(
      request: request(id: id, status: RequestStatus.ordered),
      lineItems: [
        lineItem(id: 'li-$id', requestId: id, selectedOfferId: 's-$id'),
      ],
      offers: [
        offer('s-$id', lineItemId: 'li-$id', merchant: 'A', landed: selected),
        offer('n-$id', lineItemId: 'li-$id', merchant: 'B', landed: 1450),
        offer(
          'x-$id',
          lineItemId: 'li-$id',
          merchant: 'C',
          landed: 1300,
          available: false,
        ),
      ],
      approvals: const [],
      payments: const [],
    );

void main() {
  late FakeApi api;
  late Harness h;

  setUp(() {
    api = FakeApi()
      ..orders = [
        order(
          id: 'req-a',
          status: RequestStatus.ordered,
          total: 12000,
          finalCents: 11800,
          utterance: 'Coffee beans for the pantry',
        ),
        order(
          id: 'req-b',
          status: RequestStatus.ordered,
          decision: Decision.needsApproval,
          total: 64900,
          finalCents: 64900,
          vendors: ['ErgoTune', 'Metro', 'Anker'],
          approver: daniel,
          utterance: 'Two ergonomic chairs',
        ),
        order(
          id: 'req-c',
          status: RequestStatus.awaitingPayment,
          total: 4500,
          utterance: 'A4 paper, 5 reams',
          vendors: ['Popular'],
        ),
        order(
          id: 'req-d',
          status: RequestStatus.failed,
          decision: Decision.autoApprove,
          total: 3000,
          utterance: 'Dish soap',
          vendors: ['Shoppy'],
        ),
      ]
      ..spend = const SpendSummary(
        currency: 'SGD',
        monthlyBudgetCents: 300000,
        monthToDateCents: 76700,
        months: [
          MonthSpend(month: '2026-10', spendCents: 76700, orders: 2),
          MonthSpend(month: '2026-08', spendCents: 310000, orders: 9),
          MonthSpend(month: '2026-09', spendCents: 180000, orders: 6),
        ],
      )
      ..details = {
        'req-a': detailWithOffers('req-a', selected: 1000),
        'req-b': detailWithOffers('req-b', selected: 1200),
      }
      ..audits = {
        'req-b': [
          AuditEvent(
            id: 2,
            requestId: 'req-b',
            actorType: 'system',
            type: 'policy.evaluated',
            payload: const {'decision': 'NEEDS_APPROVAL', 'total_cents': 64900},
            at: now.subtract(const Duration(hours: 3)),
          ),
          AuditEvent(
            id: 1,
            requestId: 'req-b',
            actorType: 'user',
            actorId: maya.id,
            type: 'request.created',
            payload: const {'utterance': 'Two ergonomic chairs'},
            at: now.subtract(const Duration(hours: 4)),
          ),
          AuditEvent(
            id: 3,
            requestId: 'req-b',
            actorType: 'user',
            actorId: daniel.id,
            type: 'approval.decided',
            payload: const {
              'approval': {'status': 'approved', 'comment': 'Go ahead'},
            },
            at: now.subtract(const Duration(hours: 2)),
          ),
        ],
      };
    h = Harness(api);
  });

  Future<void> pumpOrders(WidgetTester tester, {Size? size}) async {
    setSurface(tester, size ?? const Size(1440, 1800));
    await tester.pumpWidget(
      h.router(initial: '/orders', [
        GoRoute(
          path: '/orders',
          builder: (_, _) => const Scaffold(body: OrdersPage()),
          routes: [
            GoRoute(
              path: ':id',
              builder: (_, s) => Scaffold(
                body: OrderDetailPage(requestId: s.pathParameters['id']!),
              ),
            ),
          ],
        ),
      ]),
    );
    await tester.pumpAndSettle();
  }

  String kpiText(WidgetTester tester, String key) => tester
      .widgetList<Text>(
        find.descendant(of: find.byKey(Key(key)), matching: find.byType(Text)),
      )
      .map((t) => t.data)
      .join('|');

  testWidgets('KPI tiles summarise spend, savings and auto-approval', (
    tester,
  ) async {
    await pumpOrders(tester);

    expect(kpiText(tester, 'kpi-spend'), contains(r'S$767.00'));
    expect(kpiText(tester, 'kpi-spend'), contains('26%'));
    // Savings: (1450-1000) + (1450-1200); the unavailable 1300 offer is ignored.
    expect(kpiText(tester, 'kpi-savings'), contains(r'S$7.00'));
    expect(kpiText(tester, 'kpi-savings'), contains('2 orders'));
    // 3 of 4 decided orders were auto-approved.
    expect(kpiText(tester, 'kpi-auto'), contains('75%'));
    expect(kpiText(tester, 'kpi-orders'), contains('2'));
    expect(kpiText(tester, 'kpi-orders'), contains('1 in progress'));
  });

  testWidgets('spend chart plots months in order against the budget', (
    tester,
  ) async {
    await pumpOrders(tester);

    final chart = tester.widget<BarChart>(find.byType(BarChart));
    final ys = chart.data.barGroups.map((g) => g.barRods.single.toY).toList();
    expect(ys, [3100.0, 1800.0, 767.0]);
    expect(chart.data.extraLinesData.horizontalLines.single.y, 3000.0);
    // The over-budget month is drawn in the danger colour.
    expect(
      chart.data.barGroups.first.barRods.single.color,
      JarvisColors.light.danger,
    );
    expect(find.text('Monthly spend vs budget'), findsOneWidget);
    expect(find.textContaining(r'S$2,233.00 left'), findsOneWidget);
  });

  testWidgets('history table shows vendors, approver, totals and status', (
    tester,
  ) async {
    await pumpOrders(tester);

    expect(find.text('Coffee beans for the pantry'), findsOneWidget);
    expect(find.text('ErgoTune, Metro +1'), findsOneWidget);
    expect(find.text('Daniel Lim'), findsOneWidget);
    expect(find.text('Auto-approved'), findsNWidgets(3));
    // Final amount wins over the request total once paid.
    expect(find.text(r'S$118.00'), findsOneWidget);
    expect(find.text('Ordered'), findsNWidgets(2));
    expect(find.text('Approve payment'), findsOneWidget);
    expect(find.text('Failed'), findsOneWidget);
  });

  testWidgets('filters and search narrow the table', (tester) async {
    await pumpOrders(tester);

    await tester.tap(find.byKey(const Key('filter-issues')));
    await tester.pumpAndSettle();
    expect(find.text('Dish soap'), findsOneWidget);
    expect(find.text('Coffee beans for the pantry'), findsNothing);

    await tester.tap(find.byKey(const Key('filter-all')));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(const Key('orders-search')), 'metro');
    await tester.pumpAndSettle();
    expect(find.text('Two ergonomic chairs'), findsOneWidget);
    expect(find.text('Dish soap'), findsNothing);

    await tester.enterText(find.byKey(const Key('orders-search')), 'zzz');
    await tester.pumpAndSettle();
    expect(find.text('No matching orders'), findsOneWidget);
  });

  testWidgets('opening an order shows its audit timeline in order', (
    tester,
  ) async {
    await pumpOrders(tester);
    await tester.tap(find.byKey(const Key('order-row-req-b')));
    await tester.pumpAndSettle();

    expect(find.text('Audit trail'), findsOneWidget);
    final created = tester.getTopLeft(find.text('Request created')).dy;
    final policy = tester.getTopLeft(find.text('Policy: Needs approval')).dy;
    final approved = tester.getTopLeft(find.text('Approved')).dy;
    expect(created < policy && policy < approved, isTrue);
    expect(find.text('"Go ahead"'), findsOneWidget);
    expect(find.text(r'Total S$649.00'), findsOneWidget);
  });

  testWidgets('order detail renders in dark mode at tablet width', (
    tester,
  ) async {
    api.details['req-b'] = RequestDetail(
      request: request(id: 'req-b', status: RequestStatus.ordered),
      lineItems: [lineItem(id: 'li-1', requestId: 'req-b')],
      offers: [offer('o-1', merchant: 'ErgoTune', landed: 32450)],
      approvals: [approvalView(status: ApprovalStatus.approved).approval],
      payments: [payment(requestId: 'req-b', finalCents: 64900)],
    );
    setSurface(tester, const Size(800, 2400));
    await tester.pumpWidget(
      h.wrap(const OrderDetailPage(requestId: 'req-b'), dark: true),
    );
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    expect(find.text('Order REQ-B'), findsOneWidget);
    expect(find.text('Paid'), findsWidgets);
    expect(find.text('Audit trail'), findsOneWidget);
  });

  testWidgets('narrow screens render order cards', (tester) async {
    await pumpOrders(tester, size: const Size(400, 2600));
    expect(find.byKey(const Key('order-row-req-a')), findsOneWidget);
    expect(find.text('ORDER'), findsNothing);
  });

  testWidgets('live order events refresh history', (tester) async {
    await pumpOrders(tester);
    final before = api.listOrdersCalls;

    h.emit(SseTypes.agentMessage);
    await tester.pumpAndSettle();
    expect(api.listOrdersCalls, before);

    h.emit(SseTypes.orderCompleted, requestId: 'req-c');
    await tester.pumpAndSettle();
    expect(api.listOrdersCalls, greaterThan(before));
  });

  testWidgets('empty history shows a friendly empty state', (tester) async {
    api.orders = [];
    await pumpOrders(tester);
    expect(find.text('No orders yet'), findsOneWidget);
    expect(kpiText(tester, 'kpi-auto'), contains('—'));
  });

  group('OrdersMetrics', () {
    test('orderTotalCents', () {
      final cases = <(String, OrderSummary, int)>[
        (
          'all paid uses finals',
          order(
            id: 'a',
            status: RequestStatus.ordered,
            total: 100,
            finalCents: 90,
          ),
          90,
        ),
        (
          'unpaid uses request total',
          order(id: 'b', status: RequestStatus.awaitingPayment, total: 100),
          100,
        ),
      ];
      for (final (name, o, want) in cases) {
        expect(OrdersMetrics.orderTotalCents(o), want, reason: name);
      }
    });

    test('autoApproveRate', () {
      final cases = <(String, List<OrderSummary>, double?)>[
        ('empty', [], null),
        (
          'half',
          [
            order(id: 'a', status: RequestStatus.ordered),
            order(
              id: 'b',
              status: RequestStatus.ordered,
              decision: Decision.needsApproval,
            ),
            order(
              id: 'c',
              status: RequestStatus.ordered,
              decision: Decision.none,
            ),
          ],
          0.5,
        ),
      ];
      for (final (name, input, want) in cases) {
        expect(OrdersMetrics.autoApproveRate(input), want, reason: name);
      }
    });

    test('savingsCents', () {
      expect(
        OrdersMetrics.savingsCents(detailWithOffers('r', selected: 1000)),
        450,
      );
      // Selected is already more expensive than the alternative: no savings.
      expect(
        OrdersMetrics.savingsCents(detailWithOffers('r', selected: 2000)),
        0,
      );
      final noSelection = RequestDetail(
        request: request(),
        lineItems: [lineItem(selectedOfferId: null)],
        offers: const [],
        approvals: const [],
        payments: const [],
      );
      expect(OrdersMetrics.savingsCents(noSelection), 0);
    });

    test('matches filters', () {
      final cases = <(RequestStatus, OrderFilter, bool)>[
        (RequestStatus.ordered, OrderFilter.ordered, true),
        (RequestStatus.ordered, OrderFilter.active, false),
        (RequestStatus.paying, OrderFilter.active, true),
        (RequestStatus.cancelled, OrderFilter.issues, true),
        (RequestStatus.rejected, OrderFilter.issues, true),
        (RequestStatus.awaitingPayment, OrderFilter.all, true),
      ];
      for (final (status, f, want) in cases) {
        expect(
          OrdersMetrics.matches(order(id: 'x', status: status), f),
          want,
          reason: '$status/$f',
        );
      }
    });
  });

  group('AuditCopy', () {
    AuditEvent ev(String type, Json payload) => AuditEvent(
      id: 1,
      actorType: 'system',
      type: type,
      payload: payload,
      at: now,
    );
    final cases = <(String, Json, String, String, Tone)>[
      (
        'request.status_changed',
        {'from': 'quoted', 'to': 'pending_approval'},
        'Status: Needs approval',
        'From Ready to confirm',
        Tone.neutral,
      ),
      (
        'request.status_changed',
        {'from': 'paying', 'to': 'failed', 'failure_reason': 'card declined'},
        'Status: Failed',
        'From Paying · card declined',
        Tone.danger,
      ),
      (
        'search.completed',
        {'vendor_domain': 'popular.com.sg', 'offers': 3},
        'Searched popular.com.sg',
        '3 offers found',
        Tone.info,
      ),
      (
        'reap.price_drift',
        {'approved_cents': 10000, 'live_cents': 11000},
        'Price changed at checkout',
        r'S$100.00 → S$110.00',
        Tone.warning,
      ),
      (
        'reap.checkout_status',
        {'status': 'COMPLETED', 'order_id': 'ord_1'},
        'Payment completed',
        'ord_1',
        Tone.success,
      ),
      ('admin.changed', {}, 'Admin changed', '', Tone.neutral),
    ];
    for (final (type, payload, title, detail, tone) in cases) {
      test('$type → $title', () {
        final c = AuditCopy.of(ev(type, payload));
        expect(c.title, title);
        expect(c.detail, detail);
        expect(c.tone, tone);
      });
    }

    test('actor labels', () {
      final e = AuditEvent(
        id: 1,
        actorType: 'user',
        actorId: 'u1',
        type: 'x',
        payload: const {},
        at: now,
      );
      expect(actorLabel(e, names: {'u1': 'Priya'}), 'Priya');
      expect(actorLabel(e), 'User');
    });
  });
}
