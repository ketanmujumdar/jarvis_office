import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:jarvis_office/core/api/api_client.dart';
import 'package:jarvis_office/core/api/models.dart';
import 'package:jarvis_office/features/approvals/approvals_page.dart';
import 'package:jarvis_office/features/approvals/reason_copy.dart';
import 'package:jarvis_office/features/approvals/widgets/quote_comparison.dart';

import '../../helpers.dart';
import 'fixtures.dart';

void main() {
  late FakeApi api;
  late Harness h;

  setUp(() {
    api = FakeApi()..approvals = [approvalView()];
    h = Harness(api);
  });

  Future<void> pumpWide(WidgetTester tester) async {
    setSurface(tester, const Size(1440, 1400));
    await tester.pumpWidget(h.wrap(ApprovalsPage(now: now)));
    await tester.pumpAndSettle();
  }

  testWidgets('queue shows requester, amount, reasons and queue stats', (
    tester,
  ) async {
    await pumpWide(tester);

    expect(find.text('Pending (1)'), findsOneWidget);
    expect(find.text('Decided (0)'), findsOneWidget);
    // Requester appears in the tile and the detail header.
    expect(find.text('Maya Tan'), findsNWidgets(2));
    expect(find.text(r'S$649.00'), findsWidgets);
    // Reason copy and full messages.
    expect(find.text('Over order limit'), findsWidgets);
    expect(find.text('+1 more'), findsOneWidget);
    expect(
      find.text(r'Total S$649.00 exceeds the S$500.00 per-order limit'),
      findsOneWidget,
    );
    expect(find.text('Office chairs always need sign-off'), findsOneWidget);
    expect(find.text('2 h ago'), findsWidgets);
    expect(find.text('Live'), findsOneWidget);
  });

  testWidgets('detail compares only the top three quotes', (tester) async {
    await pumpWide(tester);

    expect(find.byKey(const Key('offer-o-1')), findsOneWidget);
    expect(find.byKey(const Key('offer-o-2')), findsOneWidget);
    expect(find.byKey(const Key('offer-o-3')), findsOneWidget);
    expect(find.byKey(const Key('offer-o-4')), findsNothing);
    expect(find.text('Best price'), findsOneWidget);
    expect(find.text('Selected'), findsOneWidget);
    // Deltas vs best: 359.00-324.50 and 410.00-324.50.
    expect(find.text(r'+S$34.50'), findsOneWidget);
    expect(find.text(r'+S$85.50'), findsOneWidget);
  });

  testWidgets('approve sends the comment and clears the queue', (tester) async {
    await pumpWide(tester);

    await tester.enterText(
      find.byKey(const Key('decision-comment')),
      'OK for onboarding',
    );
    await tester.tap(find.byKey(const Key('approve-button')));
    await tester.pumpAndSettle();

    expect(api.decisions, [('ap-1', 'approve', 'OK for onboarding')]);
    expect(find.text('All caught up'), findsOneWidget);
    expect(find.text('Pending (0)'), findsOneWidget);
    expect(find.text('Decided (1)'), findsOneWidget);
    expect(
      find.text('Approved. Checkout will continue automatically.'),
      findsOneWidget,
    );
  });

  testWidgets('reject requires a comment', (tester) async {
    await pumpWide(tester);

    await tester.tap(find.byKey(const Key('reject-button')));
    await tester.pump();
    expect(
      find.text('Add a comment so the requester knows why.'),
      findsOneWidget,
    );
    expect(api.decisions, isEmpty);

    await tester.enterText(
      find.byKey(const Key('decision-comment')),
      'Use the existing stock',
    );
    await tester.tap(find.byKey(const Key('reject-button')));
    await tester.pumpAndSettle();
    expect(api.decisions, [('ap-1', 'reject', 'Use the existing stock')]);
  });

  testWidgets('API errors from a decision are shown inline', (tester) async {
    api = _FailingDecisionApi()..approvals = [approvalView()];
    h = Harness(api);
    await pumpWide(tester);

    await tester.tap(find.byKey(const Key('approve-button')));
    await tester.pumpAndSettle();
    expect(find.text('approval already decided'), findsOneWidget);
    expect(find.text('Pending (1)'), findsOneWidget);
  });

  testWidgets('decided tab shows the outcome and comment', (tester) async {
    api.approvals = [
      approvalView(
        status: ApprovalStatus.rejected,
        comment: 'Not this quarter',
      ),
    ];
    await pumpWide(tester);
    expect(find.text('All caught up'), findsOneWidget);

    await tester.tap(find.text('Decided (1)'));
    await tester.pumpAndSettle();
    expect(find.text('"Not this quarter"'), findsOneWidget);
    expect(find.byKey(const Key('approve-button')), findsNothing);
  });

  testWidgets('selecting a tile switches the detail pane', (tester) async {
    api.approvals = [
      approvalView(),
      approvalView(
        id: 'ap-2',
        requestId: 'req-2',
        kind: 'price_drift',
        amountCents: 52000,
        prevCents: 48000,
        utterance: 'Coffee beans for October',
        createdAt: now.subtract(const Duration(minutes: 20)),
        reasons: const [
          Reason(code: 'PRICE_DRIFT', message: 'Live quote is 8.3% higher'),
        ],
      ),
    ];
    await pumpWide(tester);
    // Oldest first: ap-1 is selected by default.
    expect(find.text('Office chairs always need sign-off'), findsOneWidget);

    await tester.tap(find.byKey(const Key('queue-tile-ap-2')));
    await tester.pumpAndSettle();
    expect(find.text('Live quote is 8.3% higher'), findsOneWidget);
    expect(find.text('+8.3%'), findsOneWidget);
    expect(find.text('1 price change'), findsOneWidget);
  });

  testWidgets('live approval events refresh the queue', (tester) async {
    await pumpWide(tester);
    final before = api.listApprovalsCalls;
    api.approvals.add(
      approvalView(id: 'ap-9', requestId: 'req-9', utterance: 'Printer'),
    );

    h.emit(SseTypes.heartbeat);
    await tester.pumpAndSettle();
    expect(api.listApprovalsCalls, before);

    h.emit(SseTypes.approvalRequested);
    await tester.pumpAndSettle();
    expect(api.listApprovalsCalls, before + 1);
    expect(find.text('Pending (2)'), findsOneWidget);
  });

  testWidgets('errors offer a retry', (tester) async {
    api.approvalsError = const ApiException(
      statusCode: 500,
      code: 'internal',
      message: 'database unavailable',
    );
    await pumpWide(tester);
    expect(find.text('database unavailable'), findsOneWidget);

    api.approvalsError = null;
    await tester.tap(find.text('Try again'));
    await tester.pumpAndSettle();
    expect(find.text('Pending (1)'), findsOneWidget);
  });

  testWidgets('narrow screens open the detail route', (tester) async {
    setSurface(tester, const Size(420, 1600));
    await tester.pumpWidget(
      h.router(initial: '/approvals', [
        GoRoute(
          path: '/approvals',
          builder: (_, _) => Scaffold(body: ApprovalsPage(now: now)),
          routes: [
            GoRoute(
              path: ':id',
              builder: (_, s) => Scaffold(
                body: ApprovalDetailPage(approvalId: s.pathParameters['id']!),
              ),
            ),
          ],
        ),
      ]),
    );
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('approve-button')), findsNothing);

    await tester.tap(find.byKey(const Key('queue-tile-ap-1')));
    await tester.pumpAndSettle();
    expect(find.text('Review request'), findsOneWidget);
    expect(find.byKey(const Key('approve-button')), findsOneWidget);
  });

  for (final width in [800.0, 1100.0]) {
    testWidgets('renders in dark mode at ${width.toInt()}px without overflow', (
      tester,
    ) async {
      api.approvals = [
        approvalView(),
        approvalView(
          id: 'ap-2',
          requestId: 'req-2',
          kind: 'price_drift',
          prevCents: 60000,
        ),
      ];
      setSurface(tester, Size(width, 2400));
      await tester.pumpWidget(h.wrap(ApprovalsPage(now: now), dark: true));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(find.text('Pending (2)'), findsOneWidget);
    });
  }

  group('QuoteComparison.topThree', () {
    final cases = <(String, List<Offer>, List<String>)>[
      (
        'by rank',
        [
          offer('c', merchant: 'C', landed: 100, rank: 3),
          offer('a', merchant: 'A', landed: 300, rank: 1),
          offer('d', merchant: 'D', landed: 50, rank: 4),
          offer('b', merchant: 'B', landed: 200, rank: 2),
        ],
        ['a', 'b', 'c'],
      ),
      (
        'unranked last, by price',
        [
          offer('x', merchant: 'X', landed: 900, rank: 0),
          offer('y', merchant: 'Y', landed: 100, rank: 0),
          offer('a', merchant: 'A', landed: 500, rank: 1),
        ],
        ['a', 'y', 'x'],
      ),
      ('empty', [], []),
    ];
    for (final (name, input, want) in cases) {
      test(name, () {
        expect(QuoteComparison.topThree(input).map((o) => o.id), want);
      });
    }
  });

  test('ReasonCopy covers every backend reason code', () {
    const codes = [
      'OFF_LIST',
      'NOT_AUTO_APPROVE',
      'OVER_UNIT_CEILING',
      'OVER_ORDER_LIMIT',
      'OVER_MONTHLY_BUDGET',
      'VENDOR_NOT_ALLOWED',
      'NO_OFFER',
      'PRICE_DRIFT',
      'CURRENCY_MISMATCH',
    ];
    for (final c in codes) {
      expect(
        ReasonCopy.of(c).icon,
        isNot(Icons.info_outline_rounded),
        reason: c,
      );
    }
    expect(ReasonCopy.of('SOMETHING_NEW').title, 'Something new');
  });
}

class _FailingDecisionApi extends FakeApi {
  @override
  Future<Approval> approve(String id, {String comment = ''}) async =>
      throw const ApiException(
        statusCode: 409,
        code: 'conflict',
        message: 'approval already decided',
      );
}
