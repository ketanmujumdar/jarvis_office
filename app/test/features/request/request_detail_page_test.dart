import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:jarvis_office/core/api/api_client.dart';
import 'package:jarvis_office/core/api/models.dart';
import 'package:jarvis_office/features/request/request_detail_page.dart';

import '../../helpers.dart';
import 'fakes.dart';

void main() {
  late Harness h;

  setUp(() => h = Harness());
  tearDown(() => h.dispose());

  Future<void> pumpPage(
    WidgetTester tester, {
    Size size = const Size(1400, 1800),
  }) async {
    setSurface(tester, size);
    await tester.pumpWidget(h.wrap(const RequestDetailPage(requestId: 'r1')));
    await tester.pumpAndSettle();
  }

  testWidgets(
    'quoted request shows items, quote table, best offer and savings',
    (tester) async {
      h.api.details['r1'] = sampleDetail();
      await pumpPage(tester);

      expect(find.text('Line items'), findsOneWidget);
      expect(find.text('Coffee beans 1kg'), findsWidgets);
      expect(find.text('Off-list'), findsOneWidget);
      expect(find.text('Urgent'), findsOneWidget);
      expect(find.text('Common Man Coffee Roasters SG'), findsOneWidget);
      expect(find.text('Bettr Coffee'), findsOneWidget);
      // Best offer per line is tagged; savings = 83.00 - 75.00.
      expect(find.text('Best price'), findsNWidgets(2));
      expect(find.text(r'Saves S$8.00'), findsOneWidget);
      expect(
        find.text(r'You save S$8.00 vs the next best offers'),
        findsOneWidget,
      );
      expect(find.text('Unavailable'), findsOneWidget);
      // The address picker preselects the default and shows a confirm button.
      expect(find.text('Deliver to'), findsOneWidget);
      expect(find.text('Marina One HQ'), findsOneWidget);
      expect(find.text('Default'), findsOneWidget);
      expect(find.text(r'Confirm S$100.00'), findsOneWidget);
    },
  );

  testWidgets('confirm sends the chosen address', (tester) async {
    h.api.details['r1'] = sampleDetail();
    h.api.afterConfirm = sampleDetail(status: 'approved');
    await pumpPage(tester);

    // The picker is collapsed to the default address; "Change" expands it.
    expect(find.byKey(const ValueKey('address-a2')), findsNothing);
    await tester.tap(find.byKey(const ValueKey('change-address')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('address-a2')));
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('address-a1')), findsNothing);
    await tester.ensureVisible(find.byKey(const ValueKey('confirm-order')));
    await tester.tap(find.byKey(const ValueKey('confirm-order')));
    await tester.pumpAndSettle();

    expect(h.api.confirmCalls, [('r1', 'a2')]);
    // Picker gives way to the approval/payment panel.
    expect(find.text('Deliver to'), findsNothing);
    expect(find.text('Approval & payment'), findsOneWidget);
  });

  testWidgets('no card: callout up front and confirm disabled', (tester) async {
    h.api.details['r1'] = sampleDetail();
    h.api.enrollment = null;
    await pumpPage(tester);

    expect(find.text('Add a company card first'), findsOneWidget);
    final confirm = tester.widget<FilledButton>(
      find.byKey(const ValueKey('confirm-order')),
    );
    expect(confirm.onPressed, isNull);

    await tester.ensureVisible(find.text('Set up card with Reap'));
    await tester.tap(find.text('Set up card with Reap'));
    await tester.pump();
    await tester.pump();
    expect(h.api.enrollmentStarts, 1);
    expect(h.opened, ['https://reap.example/enroll/enr_1']);
  });

  testWidgets('unfinished card enrollment reopens its Reap page', (
    tester,
  ) async {
    h.api.details['r1'] = sampleDetail();
    h.api.enrollment = Enrollment(
      id: 'e1',
      reapEnrollmentId: 'enr_1',
      status: EnrollmentStatus.requiresAction,
      createdAt: DateTime.utc(2026, 10, 9),
      nextActionUrl: 'https://reap.example/enroll/pending',
    );
    await pumpPage(tester);
    expect(find.text('Finish adding the company card'), findsOneWidget);
    await tester.ensureVisible(find.text('Continue on Reap'));
    await tester.tap(find.text('Continue on Reap'));
    await tester.pump();
    await tester.pump();
    expect(h.api.enrollmentStarts, 0);
    expect(h.opened, ['https://reap.example/enroll/pending']);

    // Card entered on Reap: the panel polls and unlocks Confirm.
    h.api.enrollment = Enrollment(
      id: 'e1',
      reapEnrollmentId: 'enr_1',
      status: EnrollmentStatus.active,
      createdAt: DateTime.utc(2026, 10, 9),
    );
    await tester.pump(const Duration(seconds: 5));
    await tester.pumpAndSettle();
    expect(find.text('Finish adding the company card'), findsNothing);
    final confirm = tester.widget<FilledButton>(
      find.byKey(const ValueKey('confirm-order')),
    );
    expect(confirm.onPressed, isNotNull);
  });

  testWidgets('confirm rejected for no card shows the card callout', (
    tester,
  ) async {
    h.api.details['r1'] = sampleDetail();
    h.api.confirmError = const ApiException(
      statusCode: 409,
      code: 'no_active_enrollment',
      message: 'enroll a card first',
    );
    await pumpPage(tester);
    await tester.ensureVisible(find.byKey(const ValueKey('confirm-order')));
    await tester.tap(find.byKey(const ValueKey('confirm-order')));
    await tester.pumpAndSettle();
    expect(find.text('Add a company card first'), findsOneWidget);
  });

  testWidgets('upstream errors are never shown verbatim', (tester) async {
    h.api.details['r1'] = sampleDetail();
    h.api.confirmError = const ApiException(
      statusCode: 502,
      code: 'upstream_error',
      message: 'reap get enrollment: reap: 404 FOO_BAR: internal detail',
    );
    await pumpPage(tester);
    await tester.ensureVisible(find.byKey(const ValueKey('confirm-order')));
    await tester.tap(find.byKey(const ValueKey('confirm-order')));
    await tester.pumpAndSettle();
    expect(find.text('Could not confirm'), findsOneWidget);
    expect(
      find.text('Something went wrong confirming this order. Try again.'),
      findsOneWidget,
    );
    expect(find.textContaining('FOO_BAR'), findsNothing);
  });

  testWidgets('blocked lines are listed before confirm, without ids', (
    tester,
  ) async {
    h.api.details['r1'] = sampleDetail(
      extraLines: [
        {
          'id': '85513464-a558-4390-b358-cbb703df3e15',
          'request_id': 'r1',
          'position': 3,
          'description': 'boxes of tissues',
          'qty': 6,
          'urgency': 'normal',
          'policy_decision': 'REJECT',
          'reasons': [
            {
              'code': 'NO_OFFER',
              'message': 'Line 85513464-a558-4390-b358-cbb703df3e15: no available offer was found at an allowed vendor.',
            },
          ],
        },
      ],
    );
    await pumpPage(tester);
    expect(find.byKey(const ValueKey('skipped-lines')), findsOneWidget);
    expect(
      find.text('1 item can’t be ordered and will be skipped'),
      findsOneWidget,
    );
    expect(find.textContaining('85513464'), findsNothing);
    expect(find.text('No approved vendor has this in stock.'), findsOneWidget);
    expect(find.text('No offer found'), findsOneWidget);
  });

  testWidgets('approvers cannot confirm or cancel', (tester) async {
    h = Harness(role: Role.approver);
    h.api.details['r1'] = sampleDetail();
    await pumpPage(tester);
    expect(find.byKey(const ValueKey('confirm-order')), findsNothing);
    expect(
      find.text('Waiting for an office manager to confirm'),
      findsOneWidget,
    );
    expect(find.text('Cancel request'), findsNothing);
  });

  testWidgets('no cancel once a Reap payment link exists', (tester) async {
    h.api.details['r1'] = sampleDetail(
      status: 'awaiting_payment',
      payments: [samplePayment()],
    );
    await pumpPage(tester);
    expect(find.text('Cancel request'), findsNothing);
  });

  testWidgets('failed request hides open payment links', (tester) async {
    h.api.details['r1'] = sampleDetail(
      status: 'failed',
      payments: [samplePayment()],
    );
    await pumpPage(tester);
    expect(find.byKey(const ValueKey('approve-payment-pay1')), findsNothing);
    expect(
      find.text('Do not approve the remaining Reap payment'),
      findsOneWidget,
    );
  });

  testWidgets('quoted request can be cancelled by a manager', (tester) async {
    h.api.details['r1'] = sampleDetail();
    await pumpPage(tester);
    expect(find.text('Cancel request'), findsOneWidget);
  });

  testWidgets('awaiting payment opens the Reap approval URL', (tester) async {
    h.api.details['r1'] = sampleDetail(
      status: 'awaiting_payment',
      payments: [samplePayment()],
      address: sampleAddresses().first.toInput()..['id'] = 'a1',
    );
    await pumpPage(tester);

    expect(find.text('Deliver to'), findsNothing);
    expect(find.text('Awaiting approval'), findsOneWidget);
    expect(
      find.textContaining('7 Straits View', findRichText: true),
      findsOneWidget,
    );
    await tester.ensureVisible(
      find.byKey(const ValueKey('approve-payment-pay1')),
    );
    await tester.tap(find.byKey(const ValueKey('approve-payment-pay1')));
    await tester.pump();
    expect(h.opened, ['https://reap.example/approve/chk_1']);

    await tester.tap(find.byTooltip('Refresh payment status'));
    await tester.pumpAndSettle();
    expect(h.api.refreshCheckoutCalls, 1);
  });

  testWidgets('SSE events update the feed and refetch the request', (
    tester,
  ) async {
    h.api.details['r1'] = sampleDetail(
      status: 'awaiting_payment',
      payments: [samplePayment()],
    );
    await pumpPage(tester);
    final before = h.api.getRequestCalls;

    h.api.details['r1'] = sampleDetail(
      status: 'ordered',
      payments: [samplePayment(status: 'completed', finalCents: 7400)],
    );
    h.sse.add(
      sseEvent(7, SseTypes.orderCompleted, {
        'payments': [samplePayment(status: 'completed')],
      }),
    );
    await tester.pumpAndSettle();

    expect(h.api.getRequestCalls, greaterThan(before));
    expect(find.text('Order placed'), findsNWidgets(2)); // feed + callout
    expect(find.text('Paid'), findsOneWidget);
    expect(find.textContaining('ord_9'), findsOneWidget);

    // Duplicate event ids are ignored by the feed.
    h.sse.add(sseEvent(7, SseTypes.orderCompleted, {'payments': []}));
    await tester.pumpAndSettle();
    expect(find.text('Order placed'), findsNWidgets(2));
  });

  testWidgets('pending price-drift approval explains old and new totals', (
    tester,
  ) async {
    h.api.details['r1'] = sampleDetail(
      status: 'pending_approval',
      approvals: [
        {
          'id': 'ap2',
          'request_id': 'r1',
          'kind': 'price_drift',
          'status': 'pending',
          'amount_cents': 11000,
          'prev_cents': 10000,
        },
      ],
    );
    await pumpPage(tester);
    expect(find.text('The live price changed'), findsOneWidget);
    expect(find.textContaining(r'S$110.00'), findsOneWidget);
  });

  testWidgets('narrow layout renders without overflow', (tester) async {
    h.api.details['r1'] = sampleDetail();
    await pumpPage(tester, size: const Size(390, 3000));
    expect(tester.takeException(), isNull);
    expect(find.text('Best price'), findsNWidgets(2));
  });

  testWidgets('missing request shows an error with retry', (tester) async {
    await pumpPage(tester);
    expect(find.text('Request not found'), findsOneWidget);
    expect(find.text('Try again'), findsOneWidget);
  });
}
