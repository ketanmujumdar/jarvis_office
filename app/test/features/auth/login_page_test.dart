import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:jarvis_office/app.dart';
import 'package:jarvis_office/core/api/models.dart';
import 'package:jarvis_office/core/providers.dart';
import 'package:jarvis_office/core/router.dart';
import 'package:jarvis_office/core/theme/app_theme.dart';
import 'package:jarvis_office/features/admin/admin_providers.dart';
import 'package:jarvis_office/features/auth/login_page.dart';

import '../../helpers.dart';
import '../admin/fake_admin_api.dart';

Future<ProviderContainer> pumpLogin(
  WidgetTester tester,
  FakeAdminApi api, {
  Size size = const Size(1400, 1000),
}) async {
  setSurface(tester, size);
  final container = ProviderContainer(
    overrides: [apiProvider.overrideWithValue(api)],
  );
  addTearDown(container.dispose);
  await tester.pumpWidget(
    UncontrolledProviderScope(
      container: container,
      child: themed(const LoginPage()),
    ),
  );
  await tester.pumpAndSettle();
  return container;
}

/// Full app with the real router, signed in as [user] (or signed out).
Future<ProviderContainer> pumpApp(
  WidgetTester tester,
  FakeAdminApi api, {
  User? user,
}) async {
  setSurface(tester, const Size(1400, 2200));
  final container = ProviderContainer(
    overrides: [
      apiProvider.overrideWithValue(api),
      urlOpenerProvider.overrideWithValue(RecordingUrlOpener()),
    ],
  );
  addTearDown(container.dispose);
  if (user != null) {
    container
        .read(sessionProvider.notifier)
        .setSession(Session(token: user.id, user: user));
  }
  await tester.pumpWidget(
    UncontrolledProviderScope(container: container, child: const JarvisApp()),
  );
  await tester.pumpAndSettle();
  return container;
}

void main() {
  setUp(() => AppTheme.useGoogleFonts = false);

  group('login page', () {
    testWidgets('pick a role and a person, then continue', (tester) async {
      final api = FakeAdminApi();
      final c = await pumpLogin(tester, api);
      expect(find.text('Sign in'), findsOneWidget);
      expect(find.text('Your office,\nrestocked by voice.'), findsOneWidget);
      // First role (manager) is preselected.
      expect(find.text('Continue as Maya Tan'), findsOneWidget);

      await tester.tap(find.text('Admin'));
      await tester.pumpAndSettle();
      expect(find.text('Priya Nair'), findsOneWidget);
      expect(find.text('Maya Tan'), findsNothing);
      await tester.tap(find.text('Continue as Priya Nair'));
      await tester.pumpAndSettle();

      expect(api.bodies['login'], {'email': 'priya.nair@example.com'});
      final s = c.read(sessionProvider);
      expect(s?.token, 'u-priya');
      expect(s?.user.role, Role.admin);
    });

    testWidgets('selecting a person within a role', (tester) async {
      final api = FakeAdminApi()
        ..users = [
          ...FakeAdminApi().users,
          const User(
            id: 'u-ken',
            name: 'Ken Ong',
            email: 'ken@example.com',
            role: Role.manager,
          ),
        ];
      await pumpLogin(tester, api);
      await tester.tap(find.byKey(const ValueKey('user-u-ken')));
      await tester.pumpAndSettle();
      expect(find.text('Continue as Ken Ong'), findsOneWidget);
    });

    testWidgets('falls back to email when users cannot load', (tester) async {
      final api = FakeAdminApi()..usersFail = true;
      final c = await pumpLogin(tester, api);
      expect(
        find.text('Cannot reach the Jarvis API. Start it, then try again.'),
        findsOneWidget,
      );
      await tester.enterText(
        find.byKey(const ValueKey('login-email')),
        'nobody@example.com',
      );
      await tester.tap(find.text('Continue'));
      await tester.pumpAndSettle();
      expect(find.text('No demo user with that email.'), findsOneWidget);
      expect(c.read(sessionProvider), isNull);

      await tester.enterText(
        find.byKey(const ValueKey('login-email')),
        'daniel.lim@example.com',
      );
      await tester.tap(find.text('Continue'));
      await tester.pumpAndSettle();
      expect(c.read(sessionProvider)?.user.role, Role.approver);
    });

    testWidgets('phone layout hides the brand panel without overflow', (
      tester,
    ) async {
      await pumpLogin(tester, FakeAdminApi(), size: const Size(390, 900));
      expect(find.text('Your office,\nrestocked by voice.'), findsNothing);
      expect(find.text('Sign in'), findsOneWidget);
      expect(tester.takeException(), isNull);
    });
  });

  group('routing and guards', () {
    const priya = User(
      id: 'u-priya',
      name: 'Priya Nair',
      email: 'priya.nair@example.com',
      role: Role.admin,
    );
    const maya = User(
      id: 'u-maya',
      name: 'Maya Tan',
      email: 'maya.tan@example.com',
      role: Role.manager,
    );
    const daniel = User(
      id: 'u-daniel',
      name: 'Daniel Lim',
      email: 'daniel.lim@example.com',
      role: Role.approver,
    );

    final cases = <(String, User?, String, String?)>[
      ('signed out -> login', null, '/admin/prompt', Routes.login),
      (
        'manager blocked from admin tab',
        maya,
        '/admin/vendors',
        Routes.assistant,
      ),
      ('approver blocked from admin', daniel, '/admin', Routes.assistant),
      ('admin allowed on tab', priya, '/admin/card', null),
      ('admin signed in at login -> admin', priya, Routes.login, Routes.admin),
      ('manager at login -> assistant', maya, Routes.login, Routes.assistant),
      (
        'approver at login -> approvals',
        daniel,
        Routes.login,
        Routes.approvals,
      ),
      ('approver at root -> approvals', daniel, '/', Routes.approvals),
      ('reap return page works signed out', null, Routes.reapReturn, null),
    ];
    for (final (name, user, loc, want) in cases) {
      test(name, () {
        expect(redirectFor(user: user, location: loc), want);
      });
    }

    testWidgets('login through the app lands on the assistant', (tester) async {
      final api = FakeAdminApi();
      await pumpApp(tester, api);
      expect(find.text('Sign in'), findsOneWidget);
      await tester.tap(find.text('Continue as Maya Tan'));
      await tester.pumpAndSettle();
      expect(find.text('Sign in'), findsNothing);
      // Managers do not get the Admin destination.
      expect(find.text('Admin'), findsNothing);
    });

    testWidgets('admin deep link to a tab and tab navigation', (tester) async {
      final api = FakeAdminApi();
      final c = await pumpApp(tester, api, user: priya);
      c.read(routerProvider).go('/admin/prompt');
      await tester.pumpAndSettle();
      expect(find.text('Version 3'), findsOneWidget);

      await tester.tap(find.byKey(const ValueKey('admin-tab-addresses')));
      await tester.pumpAndSettle();
      expect(
        c.read(routerProvider).routeInformationProvider.value.uri.path,
        '/admin/addresses',
      );
      expect(find.text('HQ - Marina One'), findsOneWidget);

      c.read(routerProvider).go('/admin/bogus');
      await tester.pumpAndSettle();
      expect(find.text('A4 copy paper 80gsm'), findsOneWidget);
    });

    testWidgets('manager deep link to admin is redirected', (tester) async {
      final api = FakeAdminApi();
      final c = await pumpApp(tester, api, user: maya);
      c.read(routerProvider).go('/admin/policy');
      await tester.pumpAndSettle();
      expect(find.text('Policy limits'), findsNothing);
      expect(api.calls, isNot(contains('getPolicy')));
    });
  });
}
