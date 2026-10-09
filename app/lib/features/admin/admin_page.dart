import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../../core/theme/tokens.dart';
import '../../core/widgets/widgets.dart';
import 'tabs/addresses_tab.dart';
import 'tabs/card_tab.dart';
import 'tabs/catalog_tab.dart';
import 'tabs/policy_tab.dart';
import 'tabs/prompt_tab.dart';
import 'tabs/vendors_tab.dart';

/// Sections of the admin console. [slug] is the sub-route: `/admin/<slug>`.
enum AdminTab {
  catalog('catalog', 'Catalog', Icons.inventory_2_outlined),
  vendors('vendors', 'Vendors', Icons.storefront_outlined),
  policy('policy', 'Policy limits', Icons.policy_outlined),
  addresses('addresses', 'Delivery addresses', Icons.place_outlined),
  prompt('prompt', 'Agent prompt', Icons.auto_awesome_outlined),
  card('card', 'Payment card', Icons.credit_card_outlined);

  const AdminTab(this.slug, this.label, this.icon);
  final String slug;
  final String label;
  final IconData icon;

  static AdminTab fromSlug(String? slug) =>
      values.firstWhere((t) => t.slug == slug, orElse: () => AdminTab.catalog);

  String get path => '/admin/$slug';
}

/// Admin console: catalog, vendors, policy, addresses, agent prompt and card.
class AdminPage extends StatefulWidget {
  const AdminPage({super.key, this.tab = AdminTab.catalog});

  final AdminTab tab;

  @override
  State<AdminPage> createState() => _AdminPageState();
}

class _AdminPageState extends State<AdminPage> {
  late AdminTab _tab = widget.tab;

  @override
  void didUpdateWidget(AdminPage old) {
    super.didUpdateWidget(old);
    if (old.tab != widget.tab) _tab = widget.tab;
  }

  void _select(AdminTab t) {
    if (t == _tab) return;
    final router = GoRouter.maybeOf(context);
    if (router != null) {
      router.go(t.path);
    } else {
      setState(() => _tab = t);
    }
  }

  @override
  Widget build(BuildContext context) {
    return PageScaffold(
      header: const PageHeader(
        title: 'Admin',
        subtitle: 'Catalog, vendors, policy, addresses and the agent prompt.',
        actions: [
          StatusChip(
            label: 'Demo workspace',
            tone: Tone.brand,
            icon: Icons.shield_outlined,
          ),
        ],
      ),
      children: [
        _AdminTabBar(selected: _tab, onSelect: _select),
        const SizedBox(height: AppSpace.xl),
        KeyedSubtree(
          key: ValueKey(_tab),
          child: switch (_tab) {
            AdminTab.catalog => const CatalogTab(),
            AdminTab.vendors => const VendorsTab(),
            AdminTab.policy => const PolicyTab(),
            AdminTab.addresses => const AddressesTab(),
            AdminTab.prompt => const PromptTab(),
            AdminTab.card => const CardTab(),
          },
        ),
      ],
    );
  }
}

class _AdminTabBar extends StatelessWidget {
  const _AdminTabBar({required this.selected, required this.onSelect});
  final AdminTab selected;
  final ValueChanged<AdminTab> onSelect;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return Container(
      padding: const EdgeInsets.all(AppSpace.xs),
      decoration: BoxDecoration(
        color: jc.surfaceMuted,
        borderRadius: AppRadius.lgAll,
        border: Border.all(color: jc.border),
      ),
      child: LayoutBuilder(
        builder: (context, c) {
          final pills = [
            for (final t in AdminTab.values)
              _TabPill(
                tab: t,
                selected: t == selected,
                onTap: () => onSelect(t),
              ),
          ];
          // Phones: wrap the sections onto several rows so every one is visible
          // (a horizontal strip hid half of them off-screen).
          if (c.maxWidth < 600) {
            return Wrap(
              spacing: AppSpace.xs,
              runSpacing: AppSpace.xs,
              children: pills,
            );
          }
          return SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: Row(
              children: [
                for (final p in pills)
                  Padding(
                    padding: const EdgeInsets.only(right: AppSpace.xs),
                    child: p,
                  ),
              ],
            ),
          );
        },
      ),
    );
  }
}

class _TabPill extends StatelessWidget {
  const _TabPill({
    required this.tab,
    required this.selected,
    required this.onTap,
  });
  final AdminTab tab;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final fg = selected ? jc.brand : jc.textSecondary;
    return Semantics(
      selected: selected,
      button: true,
      child: Material(
        color: selected ? jc.surface : Colors.transparent,
        shape: RoundedRectangleBorder(
          borderRadius: AppRadius.mdAll,
          side: BorderSide(color: selected ? jc.border : Colors.transparent),
        ),
        child: InkWell(
          key: ValueKey('admin-tab-${tab.slug}'),
          borderRadius: AppRadius.mdAll,
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.symmetric(
              horizontal: AppSpace.md,
              vertical: AppSpace.sm + 2,
            ),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(tab.icon, size: 18, color: fg),
                const SizedBox(width: AppSpace.sm),
                Text(
                  tab.label,
                  style: context.tt.labelLarge?.copyWith(color: fg),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
