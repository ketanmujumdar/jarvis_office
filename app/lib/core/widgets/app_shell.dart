import 'package:flutter/material.dart';

import '../api/models.dart';
import '../theme/tokens.dart';

/// One navigation destination. [branch] is the StatefulShellRoute branch index.
class ShellDestination {
  const ShellDestination({
    required this.branch,
    required this.label,
    required this.icon,
    required this.selectedIcon,
    this.badgeCount = 0,
  });

  final int branch;
  final String label;
  final IconData icon;
  final IconData selectedIcon;
  final int badgeCount;
}

/// Responsive app frame:
/// - < 720 px: top app bar + bottom NavigationBar
/// - 720..1200 px: compact NavigationRail
/// - >= 1200 px: extended NavigationRail with labels and brand
///
/// Independent of go_router so it can be widget-tested directly.
class AppShell extends StatelessWidget {
  const AppShell({
    super.key,
    required this.destinations,
    required this.selectedBranch,
    required this.onSelectBranch,
    required this.child,
    this.user,
    this.onSignOut,
    this.statusChip,
  });

  final List<ShellDestination> destinations;
  final int selectedBranch;
  final ValueChanged<int> onSelectBranch;
  final Widget child;
  final User? user;
  final VoidCallback? onSignOut;

  /// Optional global status (e.g. card enrollment) shown in the header.
  final Widget? statusChip;

  int get _selectedIndex {
    final i = destinations.indexWhere((d) => d.branch == selectedBranch);
    return i < 0 ? 0 : i;
  }

  Widget _icon(IconData icon, int badge) =>
      badge > 0 ? Badge(label: Text('$badge'), child: Icon(icon)) : Icon(icon);

  @override
  Widget build(BuildContext context) {
    final width = MediaQuery.sizeOf(context).width;
    final jc = context.jc;

    if (width < AppBreakpoints.compact) {
      return Scaffold(
        appBar: AppBar(
          title: const _Brand(compact: true),
          actions: [
            if (statusChip != null)
              Padding(
                padding: const EdgeInsets.only(right: AppSpace.sm),
                child: statusChip,
              ),
            if (user != null) _UserMenu(user: user!, onSignOut: onSignOut),
            const SizedBox(width: AppSpace.sm),
          ],
        ),
        body: child,
        bottomNavigationBar: NavigationBar(
          selectedIndex: _selectedIndex,
          onDestinationSelected: (i) => onSelectBranch(destinations[i].branch),
          destinations: [
            for (final d in destinations)
              NavigationDestination(
                icon: _icon(d.icon, d.badgeCount),
                selectedIcon: _icon(d.selectedIcon, d.badgeCount),
                label: d.label,
              ),
          ],
        ),
      );
    }

    final extended = width >= AppBreakpoints.expanded;
    return Scaffold(
      body: Row(
        children: [
          DecoratedBox(
            decoration: BoxDecoration(
              border: Border(right: BorderSide(color: jc.border)),
            ),
            child: NavigationRail(
              extended: extended,
              minExtendedWidth: 232,
              selectedIndex: _selectedIndex,
              onDestinationSelected: (i) =>
                  onSelectBranch(destinations[i].branch),
              labelType: extended
                  ? NavigationRailLabelType.none
                  : NavigationRailLabelType.all,
              leading: Padding(
                padding: const EdgeInsets.symmetric(vertical: AppSpace.lg),
                child: _Brand(compact: !extended),
              ),
              trailing: Expanded(
                child: Align(
                  // Left-aligned with the destinations when extended.
                  alignment: extended
                      ? Alignment.bottomLeft
                      : Alignment.bottomCenter,
                  child: Padding(
                    padding: EdgeInsets.only(
                      bottom: AppSpace.lg,
                      left: extended ? AppSpace.md : 0,
                    ),
                    child: user == null
                        ? const SizedBox.shrink()
                        : _UserMenu(
                            user: user!,
                            onSignOut: onSignOut,
                            showName: extended,
                          ),
                  ),
                ),
              ),
              destinations: [
                for (final d in destinations)
                  NavigationRailDestination(
                    icon: _icon(d.icon, d.badgeCount),
                    selectedIcon: _icon(d.selectedIcon, d.badgeCount),
                    label: Text(d.label),
                  ),
              ],
            ),
          ),
          Expanded(
            child: Column(
              children: [
                if (statusChip != null)
                  Container(
                    height: 52,
                    padding: const EdgeInsets.symmetric(
                      horizontal: AppSpace.xl,
                    ),
                    decoration: BoxDecoration(
                      color: jc.surface,
                      border: Border(bottom: BorderSide(color: jc.border)),
                    ),
                    alignment: Alignment.centerRight,
                    child: statusChip,
                  ),
                Expanded(child: child),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _Brand extends StatelessWidget {
  const _Brand({required this.compact});
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final mark = Container(
      width: 34,
      height: 34,
      decoration: BoxDecoration(
        gradient: LinearGradient(
          colors: [jc.brand, jc.info],
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
        ),
        borderRadius: AppRadius.mdAll,
      ),
      child: const Icon(
        Icons.graphic_eq_rounded,
        color: Colors.white,
        size: 20,
      ),
    );
    if (compact) return mark;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        mark,
        const SizedBox(width: AppSpace.sm + 2),
        Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            Text('Jarvis Office', style: context.tt.titleMedium),
            Text('Office procurement', style: context.tt.labelSmall),
          ],
        ),
      ],
    );
  }
}

class _UserMenu extends StatelessWidget {
  const _UserMenu({required this.user, this.onSignOut, this.showName = false});
  final User user;
  final VoidCallback? onSignOut;
  final bool showName;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final avatar = CircleAvatar(
      radius: 16,
      backgroundColor: jc.brandSubtle,
      child: Text(
        user.initials,
        style: context.tt.labelMedium?.copyWith(color: jc.brand),
      ),
    );
    return PopupMenuButton<String>(
      tooltip: 'Account',
      position: PopupMenuPosition.under,
      onSelected: (v) {
        if (v == 'signout') onSignOut?.call();
      },
      itemBuilder: (_) => [
        PopupMenuItem<String>(
          enabled: false,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(user.name, style: context.tt.titleSmall),
              Text(
                '${user.email} · ${user.role.title}',
                style: context.tt.bodySmall,
              ),
            ],
          ),
        ),
        const PopupMenuDivider(),
        const PopupMenuItem<String>(value: 'signout', child: Text('Sign out')),
      ],
      child: Padding(
        padding: const EdgeInsets.all(AppSpace.xs),
        child: showName
            ? Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  avatar,
                  const SizedBox(width: AppSpace.sm),
                  Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(user.name, style: context.tt.labelLarge),
                      Text(user.role.title, style: context.tt.labelSmall),
                    ],
                  ),
                ],
              )
            : avatar,
      ),
    );
  }
}
