import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:jarvis_office/core/api/models.dart';
import 'package:jarvis_office/core/theme/tokens.dart';
import 'package:jarvis_office/core/widgets/status_chip.dart';

import '../helpers.dart';

void main() {
  testWidgets('request status chips show human labels', (tester) async {
    await tester.pumpWidget(
      themed(
        Scaffold(
          body: Wrap(
            children: [
              StatusChip.request(RequestStatus.quoted),
              StatusChip.request(RequestStatus.pendingApproval),
              StatusChip.request(RequestStatus.ordered),
              StatusChip.payment(PaymentStatus.requiresAction),
              StatusChip.decision(Decision.autoApprove),
              StatusChip.approval(ApprovalStatus.rejected),
              StatusChip.enrollment(EnrollmentStatus.active),
            ],
          ),
        ),
      ),
    );
    expect(find.text('Ready to confirm'), findsOneWidget);
    expect(find.text('Needs approval'), findsOneWidget);
    expect(find.text('Ordered'), findsOneWidget);
    expect(find.text('Awaiting approval'), findsOneWidget);
    expect(find.text('Pre-approved'), findsOneWidget);
    expect(find.text('Rejected'), findsOneWidget);
    expect(find.text('Card ready'), findsOneWidget);
    expect(find.bySemanticsLabel('Status: Ordered'), findsOneWidget);
  });

  testWidgets('busy statuses pulse without throwing, in dark mode too', (
    tester,
  ) async {
    await tester.pumpWidget(
      themed(
        Scaffold(body: StatusChip.request(RequestStatus.searching)),
        brightness: Brightness.dark,
      ),
    );
    await tester.pump(const Duration(milliseconds: 500));
    expect(find.text('Finding prices'), findsOneWidget);
  });

  test('tones map statuses to semantic colours', () {
    expect(StatusChip.requestTone(RequestStatus.ordered), Tone.success);
    expect(StatusChip.requestTone(RequestStatus.failed), Tone.danger);
    expect(StatusChip.requestTone(RequestStatus.awaitingPayment), Tone.warning);
    expect(StatusChip.decisionTone(Decision.reject), Tone.danger);
    expect(StatusChip.paymentTone(PaymentStatus.completed), Tone.success);
  });
}
