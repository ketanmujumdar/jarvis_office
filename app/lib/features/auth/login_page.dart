import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../../core/api/models.dart';
import '../../core/providers.dart';
import '../../core/theme/tokens.dart';

/// Demo users for the fake login (`GET /api/v1/auth/users`).
final demoUsersProvider = FutureProvider<List<User>>(
  (ref) => ref.watch(apiProvider).demoUsers(),
  retry: (_, _) => null,
);

/// Display copy for each role.
extension RoleCopy on Role {
  String get blurb => switch (this) {
    Role.manager => 'Restock by voice or chat, confirm orders and track them.',
    Role.approver => 'Review orders outside policy and approve or reject them.',
    Role.admin =>
      'Everything an approver can do, plus catalog, vendors, limits, '
          'addresses and the agent prompt.',
    Role.unknown => 'Limited access.',
  };

  IconData get icon => switch (this) {
    Role.manager => Icons.record_voice_over_outlined,
    Role.approver => Icons.fact_check_outlined,
    Role.admin => Icons.admin_panel_settings_outlined,
    Role.unknown => Icons.person_outline_rounded,
  };

  Tone get tone => switch (this) {
    Role.manager => Tone.info,
    Role.approver => Tone.warning,
    Role.admin => Tone.brand,
    Role.unknown => Tone.neutral,
  };
}

/// Fake sign-in: pick a demo user (name and role). No password.
class LoginPage extends ConsumerWidget {
  const LoginPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final width = MediaQuery.sizeOf(context).width;
    final wide = width >= 1000;
    final form = Center(
      child: SingleChildScrollView(
        padding: EdgeInsets.symmetric(
          horizontal: width < AppBreakpoints.compact
              ? AppSpace.lg
              : AppSpace.xxl,
          vertical: AppSpace.xxl,
        ),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 460),
          child: const _SignInPanel(),
        ),
      ),
    );
    return Scaffold(
      body: wide
          ? Row(
              children: [
                const Expanded(flex: 5, child: _BrandPanel()),
                Expanded(flex: 6, child: form),
              ],
            )
          : form,
    );
  }
}

class _BrandPanel extends StatelessWidget {
  const _BrandPanel();

  @override
  Widget build(BuildContext context) {
    const onBrand = Colors.white;
    final soft = Colors.white.withValues(alpha: 0.72);
    Widget point(IconData icon, String title, String body) => Padding(
      padding: const EdgeInsets.only(bottom: AppSpace.xl),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            padding: const EdgeInsets.all(AppSpace.sm),
            decoration: BoxDecoration(
              color: Colors.white.withValues(alpha: 0.12),
              borderRadius: AppRadius.mdAll,
              border: Border.all(color: Colors.white.withValues(alpha: 0.18)),
            ),
            child: Icon(icon, color: onBrand, size: 20),
          ),
          const SizedBox(width: AppSpace.md),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  title,
                  style: context.tt.titleMedium?.copyWith(color: onBrand),
                ),
                const SizedBox(height: AppSpace.xxs),
                Text(body, style: context.tt.bodyMedium?.copyWith(color: soft)),
              ],
            ),
          ),
        ],
      ),
    );
    return DecoratedBox(
      decoration: const BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [
            AppPalette.brand600,
            AppPalette.brand700,
            AppPalette.brand900,
          ],
        ),
      ),
      child: Stack(
        children: [
          Positioned(
            right: -120,
            top: -80,
            child: _Halo(size: 360, opacity: 0.10),
          ),
          Positioned(
            left: -90,
            bottom: -110,
            child: _Halo(size: 300, opacity: 0.08),
          ),
          Padding(
            padding: const EdgeInsets.all(AppSpace.xxxl),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Container(
                      width: 36,
                      height: 36,
                      decoration: BoxDecoration(
                        color: Colors.white,
                        borderRadius: AppRadius.mdAll,
                      ),
                      child: const Icon(
                        Icons.graphic_eq_rounded,
                        color: AppPalette.brand600,
                        size: 20,
                      ),
                    ),
                    const SizedBox(width: AppSpace.md),
                    Text(
                      'Jarvis Office',
                      style: context.tt.titleLarge?.copyWith(color: onBrand),
                    ),
                  ],
                ),
                const Spacer(),
                Text(
                  'Your office,\nrestocked by voice.',
                  style: context.tt.displaySmall?.copyWith(color: onBrand),
                ),
                const SizedBox(height: AppSpace.md),
                Text(
                  'Say what you need. Jarvis finds it at approved Singapore '
                  'merchants, checks your policy and pays through Reap.',
                  style: context.tt.bodyLarge?.copyWith(color: soft),
                ),
                const SizedBox(height: AppSpace.xxl),
                point(
                  Icons.mic_none_rounded,
                  'Talk or type',
                  '"Two boxes of A4 paper and the usual coffee beans."',
                ),
                point(
                  Icons.policy_outlined,
                  'Policy in code',
                  'Limits and approvals are enforced by the server, not the AI.',
                ),
                point(
                  Icons.lock_outline_rounded,
                  'Pay with Reap',
                  'Card details never touch Jarvis.',
                ),
                const Spacer(),
                Text(
                  'Demo workspace · SGD · Singapore',
                  style: context.tt.labelMedium?.copyWith(color: soft),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _Halo extends StatelessWidget {
  const _Halo({required this.size, required this.opacity});
  final double size;
  final double opacity;

  @override
  Widget build(BuildContext context) => IgnorePointer(
    child: Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        shape: BoxShape.circle,
        border: Border.all(
          color: Colors.white.withValues(alpha: opacity),
          width: 48,
        ),
      ),
    ),
  );
}

class _SignInPanel extends ConsumerStatefulWidget {
  const _SignInPanel();

  @override
  ConsumerState<_SignInPanel> createState() => _SignInPanelState();
}

class _SignInPanelState extends ConsumerState<_SignInPanel> {
  Role? _role;
  String? _userId;
  bool _busy = false;
  String? _error;
  final _email = TextEditingController();

  @override
  void dispose() {
    _email.dispose();
    super.dispose();
  }

  Future<void> _login(String email) async {
    if (email.trim().isEmpty) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await ref.read(sessionProvider.notifier).login(email.trim());
      // The router redirects away from /login once the session is set.
    } catch (e) {
      if (mounted) {
        setState(
          () => _error = e is ApiException
              ? (e.isNotFound || e.isUnauthorized
                    ? 'No demo user with that email.'
                    : e.message)
              : 'Could not sign in. Is the API running?',
        );
      }
    }
    if (mounted) setState(() => _busy = false);
  }

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final users = ref.watch(demoUsersProvider);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text('Sign in', style: context.tt.headlineMedium),
        const SizedBox(height: AppSpace.xs),
        Text(
          'Demo workspace. Choose who you are; no password needed.',
          style: context.tt.bodyLarge?.copyWith(color: jc.textSecondary),
        ),
        const SizedBox(height: AppSpace.xl),
        ...users.when(
          loading: () => [const _UsersSkeleton()],
          error: (e, _) => _fallback(
            context,
            message: e is ApiException && e.statusCode == 0
                ? 'Cannot reach the Jarvis API. Start it, then try again.'
                : 'Could not load demo users.',
          ),
          data: (list) => list.isEmpty
              ? _fallback(context, message: 'No demo users are seeded yet.')
              : _picker(context, list),
        ),
        if (_error != null) ...[
          const SizedBox(height: AppSpace.md),
          _ErrorBanner(message: _error!),
        ],
      ],
    );
  }

  List<Widget> _picker(BuildContext context, List<User> users) {
    final roles = [
      for (final r in Role.values)
        if (users.any((u) => u.role == r)) r,
    ];
    final role = _role ?? roles.first;
    final forRole = users.where((u) => u.role == role).toList();
    final selected = forRole.firstWhere(
      (u) => u.id == _userId,
      orElse: () => forRole.first,
    );
    return [
      Text('Role', style: context.tt.titleSmall),
      const SizedBox(height: AppSpace.sm),
      SegmentedButton<Role>(
        key: const ValueKey('role-picker'),
        showSelectedIcon: false,
        segments: [
          for (final r in roles)
            ButtonSegment(
              value: r,
              icon: Icon(r.icon, size: 18),
              tooltip: r.title,
              label: Text(
                r.shortTitle,
                maxLines: 1,
                softWrap: false,
                overflow: TextOverflow.ellipsis,
              ),
            ),
        ],
        selected: {role},
        onSelectionChanged: (s) => setState(() {
          _role = s.first;
          _userId = null;
        }),
      ),
      const SizedBox(height: AppSpace.sm),
      Text(role.blurb, style: context.tt.bodySmall),
      const SizedBox(height: AppSpace.xl),
      Text('Person', style: context.tt.titleSmall),
      const SizedBox(height: AppSpace.sm),
      for (final u in forRole)
        Padding(
          padding: const EdgeInsets.only(bottom: AppSpace.sm),
          child: _UserOption(
            user: u,
            selected: u.id == selected.id,
            onTap: () => setState(() => _userId = u.id),
          ),
        ),
      const SizedBox(height: AppSpace.lg),
      FilledButton(
        key: const ValueKey('login-continue'),
        onPressed: _busy ? null : () => _login(selected.email),
        style: FilledButton.styleFrom(
          padding: const EdgeInsets.symmetric(vertical: AppSpace.lg),
        ),
        child: _busy
            ? const SizedBox(
                width: 18,
                height: 18,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            : Text('Continue as ${selected.name}'),
      ),
    ];
  }

  List<Widget> _fallback(BuildContext context, {required String message}) {
    return [
      _ErrorBanner(
        message: message,
        action: TextButton(
          onPressed: () => ref.invalidate(demoUsersProvider),
          child: const Text('Retry'),
        ),
      ),
      const SizedBox(height: AppSpace.xl),
      TextField(
        key: const ValueKey('login-email'),
        controller: _email,
        keyboardType: TextInputType.emailAddress,
        onSubmitted: _login,
        decoration: const InputDecoration(
          labelText: 'Work email',
          hintText: 'priya.nair@example.com',
          prefixIcon: Icon(Icons.mail_outline_rounded, size: 20),
        ),
      ),
      const SizedBox(height: AppSpace.lg),
      FilledButton(
        onPressed: _busy ? null : () => _login(_email.text),
        style: FilledButton.styleFrom(
          padding: const EdgeInsets.symmetric(vertical: AppSpace.lg),
        ),
        child: const Text('Continue'),
      ),
    ];
  }
}

class _UserOption extends StatelessWidget {
  const _UserOption({
    required this.user,
    required this.selected,
    required this.onTap,
  });
  final User user;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final tone = user.role.tone;
    return Semantics(
      selected: selected,
      button: true,
      child: Material(
        color: selected ? jc.brandSubtle : jc.surface,
        shape: RoundedRectangleBorder(
          borderRadius: AppRadius.lgAll,
          side: BorderSide(
            color: selected ? jc.brand : jc.border,
            width: selected ? 1.5 : 1,
          ),
        ),
        child: InkWell(
          key: ValueKey('user-${user.id}'),
          borderRadius: AppRadius.lgAll,
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.all(AppSpace.md),
            child: Row(
              children: [
                CircleAvatar(
                  radius: 20,
                  backgroundColor: jc.bg(tone),
                  child: Text(
                    user.initials,
                    style: context.tt.titleSmall?.copyWith(color: jc.fg(tone)),
                  ),
                ),
                const SizedBox(width: AppSpace.md),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(user.name, style: context.tt.titleSmall),
                      Text(
                        user.email,
                        style: context.tt.bodySmall,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ],
                  ),
                ),
                AnimatedOpacity(
                  duration: const Duration(milliseconds: 150),
                  opacity: selected ? 1 : 0,
                  child: Icon(Icons.check_circle_rounded, color: jc.brand),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _UsersSkeleton extends StatelessWidget {
  const _UsersSkeleton();

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    Widget box(double h) => Container(
      height: h,
      margin: const EdgeInsets.only(bottom: AppSpace.sm),
      decoration: BoxDecoration(
        color: jc.surfaceMuted,
        borderRadius: AppRadius.lgAll,
      ),
    );
    return Semantics(
      label: 'Loading demo users',
      child: Column(children: [box(44), box(64), box(64), box(52)]),
    );
  }
}

class _ErrorBanner extends StatelessWidget {
  const _ErrorBanner({required this.message, this.action});
  final String message;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return Container(
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpace.md,
        vertical: AppSpace.sm,
      ),
      decoration: BoxDecoration(
        color: jc.dangerSubtle,
        borderRadius: AppRadius.mdAll,
        border: Border.all(color: jc.danger.withValues(alpha: 0.2)),
      ),
      child: Row(
        children: [
          Icon(Icons.error_outline_rounded, size: 18, color: jc.danger),
          const SizedBox(width: AppSpace.sm),
          Expanded(
            child: Text(
              message,
              style: context.tt.bodyMedium?.copyWith(color: jc.danger),
            ),
          ),
          ?action,
        ],
      ),
    );
  }
}
