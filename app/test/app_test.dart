import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:jarvis_office/app.dart';
import 'package:jarvis_office/core/api/models.dart';
import 'package:jarvis_office/core/providers.dart';
import 'package:jarvis_office/core/theme/app_theme.dart';

import 'helpers.dart';

void main() {
  setUp(() => AppTheme.useGoogleFonts = false);

  testWidgets('signed-out users land on the login screen', (tester) async {
    await tester.pumpWidget(const ProviderScope(child: JarvisApp()));
    await tester.pumpAndSettle();
    expect(find.text('Sign in'), findsOneWidget);
  });

  testWidgets('signed-in admin sees the shell with the assistant page', (
    tester,
  ) async {
    setSurface(tester, const Size(1400, 900));
    final container = ProviderContainer();
    addTearDown(container.dispose);
    container
        .read(sessionProvider.notifier)
        .setSession(
          const Session(
            token: 'u1',
            user: User(
              id: 'u1',
              name: 'Priya Nair',
              email: 'p@example.com',
              role: Role.admin,
            ),
          ),
        );
    await tester.pumpWidget(
      UncontrolledProviderScope(container: container, child: const JarvisApp()),
    );
    await tester.pumpAndSettle();
    expect(find.text('Talk or type to restock the office.'), findsOneWidget);
    // The role title "Admin" also shows in the account block; tap the nav item.
    await tester.tap(find.text('Admin').first);
    await tester.pumpAndSettle();
    expect(
      find.text('Catalog, vendors, policy, addresses and the agent prompt.'),
      findsOneWidget,
    );
    container.read(sessionProvider.notifier).logout();
    await tester.pumpAndSettle();
    expect(find.text('Sign in'), findsOneWidget);
  });
}
