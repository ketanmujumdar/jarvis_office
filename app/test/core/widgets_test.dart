import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:jarvis_office/core/widgets/widgets.dart';

import '../helpers.dart';

void main() {
  testWidgets('AppCard renders header and body; EmptyState error retries', (
    tester,
  ) async {
    var retried = false;
    await tester.pumpWidget(
      themed(
        Scaffold(
          body: Column(
            children: [
              const AppCard(
                title: 'Line items',
                subtitle: '3 items',
                trailing: Text('S\$12.00'),
                child: Text('content'),
              ),
              const StatTile(
                label: 'Month to date',
                value: 'S\$1,240.00',
                caption: '41% of budget',
              ),
              const MoneyText(3550),
              Expanded(
                child: EmptyState.error(
                  message: 'boom',
                  onRetry: () => retried = true,
                ),
              ),
            ],
          ),
        ),
      ),
    );
    expect(find.text('Line items'), findsOneWidget);
    expect(find.text('3 items'), findsOneWidget);
    expect(find.text('content'), findsOneWidget);
    expect(find.text('MONTH TO DATE'), findsOneWidget);
    expect(find.text('S\$35.50'), findsOneWidget);
    await tester.tap(find.text('Try again'));
    expect(retried, isTrue);
  });

  testWidgets('PageHeader wraps actions under the title on narrow screens', (
    tester,
  ) async {
    setSurface(tester, const Size(400, 800));
    await tester.pumpWidget(
      themed(
        Scaffold(
          body: PageScaffold(
            header: PageHeader(
              title: 'Orders',
              subtitle: 'History',
              actions: [
                FilledButton(onPressed: () {}, child: const Text('Export')),
              ],
            ),
            children: const [Text('row')],
          ),
        ),
      ),
    );
    final title = tester.getTopLeft(find.text('Orders'));
    final action = tester.getTopLeft(find.text('Export'));
    expect(action.dy, greaterThan(title.dy));
  });
}
