import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api/models.dart';
import '../../../core/providers.dart';
import '../../../core/theme/tokens.dart';
import '../../../core/widgets/widgets.dart';
import '../admin_common.dart';
import '../admin_providers.dart';

enum VendorFilter { all, allowed, blocked, office }

/// Filters and sorts vendors: priority ascending (lower = preferred), then name.
List<Vendor> filterVendors(
  List<Vendor> vendors, {
  String query = '',
  VendorFilter filter = VendorFilter.all,
}) {
  final q = query.trim().toLowerCase();
  final out = vendors.where((v) {
    final ok = switch (filter) {
      VendorFilter.all => true,
      VendorFilter.allowed => v.allowed,
      VendorFilter.blocked => !v.allowed,
      VendorFilter.office => v.officeRelevant,
    };
    if (!ok) return false;
    if (q.isEmpty) return true;
    return v.name.toLowerCase().contains(q) ||
        v.domain.toLowerCase().contains(q) ||
        v.reapMerchantName.toLowerCase().contains(q) ||
        v.category.toLowerCase().contains(q);
  }).toList();
  out.sort((a, b) {
    final p = a.priority.compareTo(b.priority);
    return p != 0 ? p : a.name.toLowerCase().compareTo(b.name.toLowerCase());
  });
  return out;
}

class VendorsTab extends ConsumerStatefulWidget {
  const VendorsTab({super.key});

  @override
  ConsumerState<VendorsTab> createState() => _VendorsTabState();
}

class _VendorsTabState extends ConsumerState<VendorsTab> {
  String _query = '';
  VendorFilter _filter = VendorFilter.all;

  /// Optimistic values shown while an update is in flight.
  final Map<String, Vendor> _pending = {};

  Future<void> _update(Vendor v, Json patch, String success) async {
    final input = {...v.toInput(), ...patch};
    setState(() => _pending[v.id] = Vendor.fromJson({'id': v.id, ...input}));
    await runMutation(
      context,
      () => ref.read(apiProvider).updateVendor(v.id, input),
      success: success,
    );
    ref.invalidate(adminVendorsProvider);
    try {
      await ref.read(adminVendorsProvider.future);
    } catch (_) {}
    if (mounted) setState(() => _pending.remove(v.id));
  }

  Future<void> _edit(Vendor v) async {
    final changed = await showDialog<bool>(
      context: context,
      builder: (_) => VendorDialog(vendor: v),
    );
    if (changed == true) ref.invalidate(adminVendorsProvider);
  }

  @override
  Widget build(BuildContext context) {
    return AsyncValueView<List<Vendor>>(
      value: ref.watch(adminVendorsProvider),
      loading: const AdminSkeleton(rows: 8, rowHeight: 56),
      onRetry: () => ref.invalidate(adminVendorsProvider),
      data: (raw) {
        final vendors = [for (final v in raw) _pending[v.id] ?? v];
        final shown = filterVendors(vendors, query: _query, filter: _filter);
        final allowed = vendors.where((v) => v.allowed).length;
        final unresolved = vendors
            .where((v) => v.allowed && v.reapMerchantName.isEmpty)
            .length;
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            _StatRow(
              tiles: [
                StatTile(
                  label: 'Allowed vendors',
                  value: '$allowed / ${vendors.length}',
                  caption: 'Only allowlisted merchants are searched',
                  icon: Icons.verified_outlined,
                  tone: Tone.success,
                ),
                StatTile(
                  label: 'Office relevant',
                  value: '${vendors.where((v) => v.officeRelevant).length}',
                  caption: 'Searched for off-list items',
                  icon: Icons.business_center_outlined,
                ),
                StatTile(
                  label: 'Unresolved',
                  value: '$unresolved',
                  caption: 'Allowed but no Reap merchant name',
                  icon: Icons.link_off_rounded,
                  tone: unresolved > 0 ? Tone.warning : Tone.neutral,
                ),
              ],
            ),
            const SizedBox(height: AppSpace.xl),
            Wrap(
              spacing: AppSpace.md,
              runSpacing: AppSpace.md,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                SizedBox(
                  width: 340,
                  child: AdminSearchField(
                    hint: 'Search vendor, domain or category',
                    onChanged: (v) => setState(() => _query = v),
                  ),
                ),
                Wrap(
                  spacing: AppSpace.xs,
                  children: [
                    for (final f in VendorFilter.values)
                      ChoiceChip(
                        label: Text(switch (f) {
                          VendorFilter.all => 'All',
                          VendorFilter.allowed => 'Allowed',
                          VendorFilter.blocked => 'Blocked',
                          VendorFilter.office => 'Office relevant',
                        }),
                        selected: _filter == f,
                        onSelected: (_) => setState(() => _filter = f),
                      ),
                  ],
                ),
              ],
            ),
            const SizedBox(height: AppSpace.lg),
            AppCard(
              padding: EdgeInsets.zero,
              child: shown.isEmpty
                  ? const EmptyState(
                      compact: true,
                      icon: Icons.storefront_outlined,
                      title: 'No vendors match',
                    )
                  : Column(
                      children: [
                        for (var n = 0; n < shown.length; n++) ...[
                          if (n > 0) const Divider(),
                          _VendorRow(
                            vendor: shown[n],
                            busy: _pending.containsKey(shown[n].id),
                            onAllowed: (a) => _update(
                              shown[n],
                              {'allowed': a},
                              a
                                  ? '${shown[n].name} allowed'
                                  : '${shown[n].name} blocked',
                            ),
                            onPriority: (p) => _update(shown[n], {
                              'priority': p,
                            }, 'Priority updated'),
                            onEdit: () => _edit(shown[n]),
                          ),
                        ],
                      ],
                    ),
            ),
          ],
        );
      },
    );
  }
}

class _StatRow extends StatelessWidget {
  const _StatRow({required this.tiles});
  final List<Widget> tiles;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, c) {
        if (c.maxWidth < AppBreakpoints.compact) {
          return Column(
            children: [
              for (final t in tiles) ...[
                t,
                const SizedBox(height: AppSpace.md),
              ],
            ],
          );
        }
        return IntrinsicHeight(
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              for (var i = 0; i < tiles.length; i++) ...[
                if (i > 0) const SizedBox(width: AppSpace.md),
                Expanded(child: tiles[i]),
              ],
            ],
          ),
        );
      },
    );
  }
}

class _VendorRow extends StatelessWidget {
  const _VendorRow({
    required this.vendor,
    required this.busy,
    required this.onAllowed,
    required this.onPriority,
    required this.onEdit,
  });

  final Vendor vendor;
  final bool busy;
  final ValueChanged<bool> onAllowed;
  final ValueChanged<int> onPriority;
  final VoidCallback onEdit;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final v = vendor;
    final info = Row(
      children: [
        CircleAvatar(
          radius: 18,
          backgroundColor: v.allowed ? jc.brandSubtle : jc.neutralSubtle,
          child: Text(
            v.name.isEmpty ? '?' : v.name[0].toUpperCase(),
            style: context.tt.titleSmall?.copyWith(
              color: v.allowed ? jc.brand : jc.textMuted,
            ),
          ),
        ),
        const SizedBox(width: AppSpace.md),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Wrap(
                spacing: AppSpace.sm,
                runSpacing: AppSpace.xs,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [
                  Text(v.name, style: context.tt.titleSmall),
                  if (v.category.isNotEmpty) Tag(v.category),
                  if (v.officeRelevant)
                    const Tag(
                      'Office',
                      tone: Tone.info,
                      icon: Icons.business_center_outlined,
                    ),
                ],
              ),
              const SizedBox(height: AppSpace.xxs),
              Text(
                v.reapMerchantName.isEmpty
                    ? '${v.domain} · not resolved on Reap'
                    : '${v.domain} · ${v.reapMerchantName}',
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: context.tt.bodySmall?.copyWith(
                  color: v.reapMerchantName.isEmpty ? jc.warning : jc.textMuted,
                ),
              ),
            ],
          ),
        ),
      ],
    );
    Widget labelled(String label, Widget child) => Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(
          label,
          style: context.tt.labelSmall?.copyWith(color: jc.textMuted),
        ),
        const SizedBox(height: AppSpace.xxs),
        child,
      ],
    );
    final controls = Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Tooltip(
          message: 'Priority: lower numbers are preferred when prices tie',
          child: labelled(
            'Priority',
            PriorityStepper(
              key: ValueKey('priority-${v.id}'),
              value: v.priority,
              enabled: !busy,
              onChanged: onPriority,
            ),
          ),
        ),
        const SizedBox(width: AppSpace.md),
        Tooltip(
          message: v.allowed ? 'Allowed' : 'Blocked',
          child: labelled(
            v.allowed ? 'Allowed' : 'Blocked',
            Switch(
              key: ValueKey('allowed-${v.id}'),
              value: v.allowed,
              onChanged: busy ? null : onAllowed,
            ),
          ),
        ),
        IconButton(
          tooltip: 'Edit ${v.name}',
          icon: const Icon(Icons.edit_outlined, size: 18),
          onPressed: onEdit,
        ),
      ],
    );
    return Padding(
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpace.lg,
        vertical: AppSpace.md,
      ),
      child: LayoutBuilder(
        builder: (context, c) => c.maxWidth < 640
            ? Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  info,
                  const SizedBox(height: AppSpace.sm),
                  Align(alignment: Alignment.centerRight, child: controls),
                ],
              )
            : Row(
                children: [
                  Expanded(child: info),
                  const SizedBox(width: AppSpace.md),
                  controls,
                ],
              ),
      ),
    );
  }
}

/// Compact "− 10 +" control. Lower numbers are preferred; clamps to 1..999.
class PriorityStepper extends StatelessWidget {
  const PriorityStepper({
    super.key,
    required this.value,
    required this.onChanged,
    this.enabled = true,
    this.step = 5,
  });

  final int value;
  final ValueChanged<int> onChanged;
  final bool enabled;
  final int step;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    Widget btn(IconData icon, String tip, int next) => SizedBox(
      width: 30,
      height: 30,
      child: IconButton(
        tooltip: tip,
        padding: EdgeInsets.zero,
        iconSize: 16,
        icon: Icon(icon),
        onPressed: enabled && next != value ? () => onChanged(next) : null,
      ),
    );
    return Tooltip(
      message: 'Priority (lower is preferred)',
      child: Container(
        decoration: BoxDecoration(
          border: Border.all(color: jc.borderStrong),
          borderRadius: AppRadius.mdAll,
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            btn(
              Icons.remove_rounded,
              'More preferred',
              (value - step).clamp(1, 999),
            ),
            SizedBox(
              width: 34,
              child: Text(
                '$value',
                textAlign: TextAlign.center,
                style: context.tt.labelLarge?.copyWith(
                  fontFeatures: const [FontFeature.tabularFigures()],
                ),
              ),
            ),
            btn(
              Icons.add_rounded,
              'Less preferred',
              (value + step).clamp(1, 999),
            ),
          ],
        ),
      ),
    );
  }
}

/// Edits a vendor's descriptive fields. Pops `true` after a save.
class VendorDialog extends ConsumerStatefulWidget {
  const VendorDialog({super.key, required this.vendor});
  final Vendor vendor;

  @override
  ConsumerState<VendorDialog> createState() => _VendorDialogState();
}

class _VendorDialogState extends ConsumerState<VendorDialog> {
  final _form = GlobalKey<FormState>();
  late final _name = TextEditingController(text: widget.vendor.name);
  late final _reapName = TextEditingController(
    text: widget.vendor.reapMerchantName,
  );
  late final _category = TextEditingController(text: widget.vendor.category);
  late final _priority = TextEditingController(
    text: '${widget.vendor.priority}',
  );
  late final _notes = TextEditingController(text: widget.vendor.notes);
  late bool _allowed = widget.vendor.allowed;
  late bool _office = widget.vendor.officeRelevant;
  bool _busy = false;

  @override
  void dispose() {
    for (final c in [_name, _reapName, _category, _priority, _notes]) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _save() async {
    if (!_form.currentState!.validate()) return;
    setState(() => _busy = true);
    final input = {
      ...widget.vendor.toInput(),
      'name': _name.text.trim(),
      'reap_merchant_name': _reapName.text.trim(),
      'category': _category.text.trim(),
      'priority': int.parse(_priority.text.trim()),
      'notes': _notes.text.trim(),
      'allowed': _allowed,
      'office_relevant': _office,
    };
    final ok = await runMutation(
      context,
      () => ref.read(apiProvider).updateVendor(widget.vendor.id, input),
      success: 'Vendor updated',
    );
    if (!mounted) return;
    setState(() => _busy = false);
    if (ok) Navigator.of(context).pop(true);
  }

  @override
  Widget build(BuildContext context) {
    return FormDialog(
      title: 'Edit vendor',
      subtitle: widget.vendor.domain,
      icon: Icons.storefront_outlined,
      body: Form(
        key: _form,
        child: FieldGrid(
          children: [
            TextFormField(
              controller: _name,
              decoration: const InputDecoration(labelText: 'Display name'),
              validator: (v) => Validators.required(v, 'Name'),
            ),
            TextFormField(
              controller: _priority,
              keyboardType: TextInputType.number,
              decoration: const InputDecoration(
                labelText: 'Priority',
                helperText: 'Lower is preferred',
              ),
              validator: Validators.positiveInt,
            ),
            FullWidth(
              child: TextFormField(
                controller: _reapName,
                decoration: const InputDecoration(
                  labelText: 'Reap merchant name',
                  helperText:
                      'Exact merchant.name returned by Reap search results',
                ),
              ),
            ),
            TextFormField(
              controller: _category,
              decoration: const InputDecoration(labelText: 'Category'),
            ),
            const SizedBox.shrink(),
            SwitchTile(
              title: 'Allowed',
              subtitle: 'The agent may buy from this merchant',
              value: _allowed,
              onChanged: (v) => setState(() => _allowed = v),
            ),
            SwitchTile(
              title: 'Office relevant',
              subtitle: 'Searched for off-list items',
              value: _office,
              onChanged: (v) => setState(() => _office = v),
            ),
            FullWidth(
              child: TextFormField(
                controller: _notes,
                minLines: 2,
                maxLines: 4,
                decoration: const InputDecoration(labelText: 'Notes'),
              ),
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: _busy ? null : () => Navigator.of(context).pop(false),
          child: const Text('Cancel'),
        ),
        FilledButton(
          onPressed: _busy ? null : _save,
          child: const Text('Save changes'),
        ),
      ],
    );
  }
}
