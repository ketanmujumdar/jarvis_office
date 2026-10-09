import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:jarvis_office/core/api/models.dart';
import 'package:jarvis_office/core/router.dart';
import 'package:jarvis_office/core/widgets/app_shell.dart';

import '../helpers.dart';

const _admin = User(
  id: 'u1',
  name: 'Priya Nair',
  email: 'priya@example.com',
  role: Role.admin,
);

Widget _shell({required Role role, required ValueChanged<int> onSelect}) =>
    themed(
      AppShell(
        destinations: destinationsFor(role),
        selectedBranch: Branches.assistant,
        onSelectBranch: onSelect,
        user: User(
          id: 'u',
          name: 'Maya Tan',
          email: 'm@example.com',
          role: role,
        ),
        child: const Center(child: Text('body')),
      ),
    );

void main() {
  testWidgets('narrow layout uses a bottom NavigationBar', (tester) async {
    setSurface(tester, const Size(400, 800));
    var selected = -1;
    await tester.pumpWidget(
      _shell(role: Role.manager, onSelect: (b) => selected = b),
    );
    expect(find.byType(NavigationBar), findsOneWidget);
    expect(find.byType(NavigationRail), findsNothing);
    expect(
      find.text('Approvals'),
      findsNothing,
      reason: 'managers cannot approve',
    );
    await tester.tap(find.text('Orders'));
    expect(selected, Branches.orders);
  });

  testWidgets(
    'wide layout uses an extended NavigationRail with brand and all admin tabs',
    (tester) async {
      setSurface(tester, const Size(1400, 900));
      await tester.pumpWidget(
        themed(
          AppShell(
            destinations: destinationsFor(Role.admin),
            selectedBranch: Branches.admin,
            onSelectBranch: (_) {},
            user: _admin,
            child: const Text('body'),
          ),
        ),
      );
      expect(find.byType(NavigationRail), findsOneWidget);
      expect(find.text('Jarvis Office'), findsOneWidget);
      for (final label in ['Assistant', 'Approvals', 'Orders']) {
        expect(find.text(label), findsOneWidget);
      }
      // "Admin" is both the nav item and the role title under the name.
      expect(find.text('Admin'), findsNWidgets(2));
      expect(find.text('Priya Nair'), findsOneWidget);
    },
  );

  test('destinations depend on role', () {
    List<String> labels(Role r) =>
        destinationsFor(r).map((d) => d.label).toList();
    expect(labels(Role.manager), ['Assistant', 'Orders']);
    expect(labels(Role.approver), ['Assistant', 'Approvals', 'Orders']);
    expect(labels(Role.admin), ['Assistant', 'Approvals', 'Orders', 'Admin']);
  });

  test('redirects enforce login and roles', () {
    const manager = User(id: 'm', name: 'M', email: 'm', role: Role.manager);
    expect(redirectFor(user: null, location: Routes.orders), Routes.login);
    expect(redirectFor(user: null, location: Routes.login), isNull);
    expect(
      redirectFor(user: manager, location: Routes.login),
      Routes.assistant,
    );
    expect(
      redirectFor(user: manager, location: Routes.admin),
      Routes.assistant,
    );
    expect(
      redirectFor(user: manager, location: Routes.approvals),
      Routes.assistant,
    );
    expect(redirectFor(user: manager, location: Routes.orders), isNull);
    expect(redirectFor(user: _admin, location: Routes.admin), isNull);
  });
}
