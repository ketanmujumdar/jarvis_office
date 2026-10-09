import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../features/admin/admin_page.dart';
import '../features/approvals/approvals_page.dart';
import '../features/auth/login_page.dart';
import '../features/orders/order_detail_page.dart';
import '../features/orders/orders_page.dart';
import '../features/reap_return/reap_return_page.dart';
import '../features/request/request_detail_page.dart';
import '../features/voice/voice_page.dart';
import 'api/models.dart';
import 'providers.dart';
import 'widgets/app_shell.dart';

/// Route paths. Feature owners add sub-routes under their branch.
abstract final class Routes {
  static const login = '/login';
  static const assistant = '/assistant';
  static String request(String id) => '/requests/$id';
  static const approvals = '/approvals';
  static String approval(String id) => '/approvals/$id';
  static const orders = '/orders';
  static const admin = '/admin';

  /// Return URL for Reap's hosted pages (REAP_RETURN_URL).
  static const reapReturn = '/reap-return';
}

/// Branch indices of the StatefulShellRoute (stable; do not reorder).
abstract final class Branches {
  static const assistant = 0;
  static const approvals = 1;
  static const orders = 2;
  static const admin = 3;
}

/// Navigation destinations visible to a role.
List<ShellDestination> destinationsFor(Role role) => [
  const ShellDestination(
    branch: Branches.assistant,
    label: 'Assistant',
    icon: Icons.graphic_eq_outlined,
    selectedIcon: Icons.graphic_eq_rounded,
  ),
  if (role.canApprove)
    const ShellDestination(
      branch: Branches.approvals,
      label: 'Approvals',
      icon: Icons.fact_check_outlined,
      selectedIcon: Icons.fact_check_rounded,
    ),
  const ShellDestination(
    branch: Branches.orders,
    label: 'Orders',
    icon: Icons.inventory_2_outlined,
    selectedIcon: Icons.inventory_2_rounded,
  ),
  if (role.canAdmin)
    const ShellDestination(
      branch: Branches.admin,
      label: 'Admin',
      icon: Icons.tune_outlined,
      selectedIcon: Icons.tune_rounded,
    ),
];

/// Where a role lands after sign-in: each role's primary task.
String homeFor(Role role) => switch (role) {
  Role.approver => Routes.approvals,
  Role.admin => Routes.admin,
  _ => Routes.assistant,
};

/// Pure redirect logic (unit-testable).
String? redirectFor({required User? user, required String location}) {
  if (location == Routes.reapReturn) return null; // works signed out (new tab)
  final atLogin = location == Routes.login;
  if (user == null) return atLogin ? null : Routes.login;
  if (atLogin || location == '/') return homeFor(user.role);
  if (location.startsWith(Routes.admin) && !user.role.canAdmin) {
    return Routes.assistant;
  }
  if (location.startsWith(Routes.approvals) && !user.role.canApprove) {
    return Routes.assistant;
  }
  return null;
}

final routerProvider = Provider<GoRouter>((ref) {
  final refresh = ValueNotifier<int>(0);
  ref.listen(sessionProvider, (_, _) => refresh.value++);
  ref.onDispose(refresh.dispose);

  return GoRouter(
    initialLocation: Routes.assistant,
    refreshListenable: refresh,
    redirect: (context, state) => redirectFor(
      // Read the source provider: derived providers can be stale inside the
      // refresh callback.
      user: ref.read(sessionProvider)?.user,
      location: state.matchedLocation,
    ),
    routes: [
      GoRoute(path: Routes.login, builder: (_, _) => const LoginPage()),
      GoRoute(
        path: Routes.reapReturn,
        builder: (_, s) =>
            ReapReturnPage(status: s.uri.queryParameters['status']),
      ),
      StatefulShellRoute.indexedStack(
        builder: (context, state, shell) => _Shell(shell: shell),
        branches: [
          StatefulShellBranch(
            routes: [
              GoRoute(
                path: Routes.assistant,
                builder: (_, _) => const VoicePage(),
              ),
              GoRoute(
                path: '/requests/:id',
                builder: (_, s) =>
                    RequestDetailPage(requestId: s.pathParameters['id']!),
              ),
            ],
          ),
          StatefulShellBranch(
            routes: [
              GoRoute(
                path: Routes.approvals,
                builder: (_, _) => const ApprovalsPage(),
                routes: [
                  GoRoute(
                    path: ':id',
                    builder: (_, s) =>
                        ApprovalDetailPage(approvalId: s.pathParameters['id']!),
                  ),
                ],
              ),
            ],
          ),
          StatefulShellBranch(
            routes: [
              GoRoute(
                path: Routes.orders,
                builder: (_, _) => const OrdersPage(),
                routes: [
                  GoRoute(
                    path: ':id',
                    builder: (_, s) =>
                        OrderDetailPage(requestId: s.pathParameters['id']!),
                  ),
                ],
              ),
            ],
          ),
          StatefulShellBranch(
            routes: [
              // Owner H: `/admin` shows the catalog tab; `/admin/<tab>` the
              // others (see AdminTab). Guarded by the `/admin` prefix check.
              GoRoute(
                path: Routes.admin,
                builder: (_, _) => const AdminPage(),
                routes: [
                  GoRoute(
                    path: ':tab',
                    redirect: (_, s) =>
                        AdminTab.values.any(
                          (t) => t.slug == s.pathParameters['tab'],
                        )
                        ? null
                        : Routes.admin,
                    builder: (_, s) => AdminPage(
                      tab: AdminTab.fromSlug(s.pathParameters['tab']),
                    ),
                  ),
                ],
              ),
            ],
          ),
        ],
      ),
    ],
  );
});

class _Shell extends ConsumerWidget {
  const _Shell({required this.shell});
  final StatefulNavigationShell shell;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final user = ref.watch(currentUserProvider);
    return AppShell(
      destinations: destinationsFor(user?.role ?? Role.manager),
      selectedBranch: shell.currentIndex,
      onSelectBranch: (b) =>
          shell.goBranch(b, initialLocation: b == shell.currentIndex),
      user: user,
      onSignOut: () => ref.read(sessionProvider.notifier).logout(),
      child: shell,
    );
  }
}
