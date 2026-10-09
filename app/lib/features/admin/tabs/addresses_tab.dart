import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api/models.dart';
import '../../../core/providers.dart';
import '../../../core/theme/tokens.dart';
import '../../../core/widgets/widgets.dart';
import '../admin_common.dart';
import '../admin_providers.dart';

class AddressesTab extends ConsumerWidget {
  const AddressesTab({super.key});

  Future<void> _edit(BuildContext context, WidgetRef ref, Address? a) async {
    final changed = await showDialog<bool>(
      context: context,
      builder: (_) => AddressDialog(address: a),
    );
    if (changed == true) ref.invalidate(adminAddressesProvider);
  }

  Future<void> _makeDefault(
    BuildContext context,
    WidgetRef ref,
    Address a,
  ) async {
    final ok = await runMutation(
      context,
      () => ref.read(apiProvider).updateAddress(a.id, {
        ...a.toInput(),
        'is_default': true,
      }),
      success: '${a.label} is now the default address',
    );
    if (ok) ref.invalidate(adminAddressesProvider);
  }

  Future<void> _delete(BuildContext context, WidgetRef ref, Address a) async {
    final yes = await confirmAction(
      context,
      title: 'Delete ${a.label}?',
      message:
          'The agent will stop offering this address. Orders already '
          'delivered there keep their history.',
      confirmLabel: 'Delete',
      destructive: true,
    );
    if (!yes || !context.mounted) return;
    final ok = await runMutation(
      context,
      () => ref.read(apiProvider).deleteAddress(a.id),
      success: 'Address deleted',
    );
    if (ok) ref.invalidate(adminAddressesProvider);
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return AsyncValueView<List<Address>>(
      value: ref.watch(adminAddressesProvider),
      loading: const AdminSkeleton(rows: 3, rowHeight: 120),
      onRetry: () => ref.invalidate(adminAddressesProvider),
      data: (list) => Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          TabToolbar(
            leading: Text(
              'At confirmation the agent asks which of these to deliver to.',
              style: context.tt.bodyMedium?.copyWith(
                color: context.jc.textSecondary,
              ),
            ),
            actions: [
              FilledButton.icon(
                onPressed: () => _edit(context, ref, null),
                icon: const Icon(Icons.add_location_alt_outlined, size: 18),
                label: const Text('Add address'),
              ),
            ],
          ),
          const SizedBox(height: AppSpace.lg),
          if (list.isEmpty)
            AppCard(
              child: EmptyState(
                icon: Icons.place_outlined,
                title: 'No delivery addresses',
                message: 'Add at least one office so orders can be shipped.',
                action: FilledButton(
                  onPressed: () => _edit(context, ref, null),
                  child: const Text('Add address'),
                ),
              ),
            )
          else
            LayoutBuilder(
              builder: (context, c) {
                final cols = c.maxWidth >= 1000
                    ? 3
                    : c.maxWidth >= 640
                    ? 2
                    : 1;
                final w = (c.maxWidth - AppSpace.lg * (cols - 1)) / cols;
                return Wrap(
                  spacing: AppSpace.lg,
                  runSpacing: AppSpace.lg,
                  children: [
                    for (final a in list)
                      SizedBox(
                        width: w,
                        child: _AddressCard(
                          address: a,
                          onEdit: () => _edit(context, ref, a),
                          onDefault: () => _makeDefault(context, ref, a),
                          onDelete: () => _delete(context, ref, a),
                        ),
                      ),
                  ],
                );
              },
            ),
        ],
      ),
    );
  }
}

class _AddressCard extends StatelessWidget {
  const _AddressCard({
    required this.address,
    required this.onEdit,
    required this.onDefault,
    required this.onDelete,
  });

  final Address address;
  final VoidCallback onEdit;
  final VoidCallback onDefault;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final a = address;
    // Every card has the same rows (the address always takes two lines) so
    // cards in a grid row line up, including their action buttons.
    final lineHeight =
        (context.tt.bodyMedium?.fontSize ?? 14) *
        (context.tt.bodyMedium?.height ?? 1.4);
    Widget line(IconData icon, String text, {int lines = 1}) => Padding(
      padding: const EdgeInsets.only(top: AppSpace.xs),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: 16, color: jc.textMuted),
          const SizedBox(width: AppSpace.sm),
          Expanded(
            child: ConstrainedBox(
              constraints: BoxConstraints(minHeight: lineHeight * lines),
              child: Text(
                text,
                style: context.tt.bodyMedium,
                maxLines: lines,
                overflow: TextOverflow.ellipsis,
              ),
            ),
          ),
        ],
      ),
    );
    return AppCard(
      key: ValueKey('address-${a.id}'),
      highlight: a.isDefault ? Tone.brand : null,
      title: a.label,
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (a.isDefault)
            const StatusChip(
              label: 'Default',
              tone: Tone.brand,
              icon: Icons.star_rounded,
            ),
          PopupMenuButton<String>(
            tooltip: 'Actions for ${a.label}',
            icon: const Icon(Icons.more_horiz_rounded),
            onSelected: (v) => switch (v) {
              'edit' => onEdit(),
              'default' => onDefault(),
              'delete' => onDelete(),
              _ => null,
            },
            itemBuilder: (_) => [
              const PopupMenuItem(value: 'edit', child: Text('Edit')),
              if (!a.isDefault)
                const PopupMenuItem(
                  value: 'default',
                  child: Text('Set as default'),
                ),
              PopupMenuItem(
                value: 'delete',
                child: Text('Delete', style: TextStyle(color: jc.danger)),
              ),
            ],
          ),
        ],
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          line(Icons.place_outlined, a.oneLine, lines: 2),
          line(Icons.person_outline_rounded, a.contactName),
          line(Icons.call_outlined, a.phone),
          line(Icons.mail_outline_rounded, a.email),
          const SizedBox(height: AppSpace.md),
          Wrap(
            spacing: AppSpace.sm,
            runSpacing: AppSpace.sm,
            children: [
              OutlinedButton(onPressed: onEdit, child: const Text('Edit')),
              TextButton(
                // Same button on every card keeps the rows aligned; disabled
                // on the current default.
                onPressed: a.isDefault ? null : onDefault,
                child: Text(a.isDefault ? 'Default address' : 'Set as default'),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

/// Create or edit a delivery address. Pops `true` after a save.
class AddressDialog extends ConsumerStatefulWidget {
  const AddressDialog({super.key, this.address});
  final Address? address;

  @override
  ConsumerState<AddressDialog> createState() => _AddressDialogState();
}

class _AddressDialogState extends ConsumerState<AddressDialog> {
  final _form = GlobalKey<FormState>();
  late final _label = TextEditingController(text: widget.address?.label);
  late final _first = TextEditingController(text: widget.address?.firstName);
  late final _last = TextEditingController(text: widget.address?.lastName);
  late final _phone = TextEditingController(
    text: widget.address?.phone ?? '+65',
  );
  late final _email = TextEditingController(text: widget.address?.email);
  late final _line1 = TextEditingController(text: widget.address?.addressLine1);
  late final _line2 = TextEditingController(text: widget.address?.addressLine2);
  late final _postal = TextEditingController(text: widget.address?.postalCode);
  late bool _isDefault = widget.address?.isDefault ?? false;
  bool _busy = false;

  bool get _isNew => widget.address == null;

  @override
  void dispose() {
    for (final c in [
      _label,
      _first,
      _last,
      _phone,
      _email,
      _line1,
      _line2,
      _postal,
    ]) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _save() async {
    if (!_form.currentState!.validate()) return;
    setState(() => _busy = true);
    final input = {
      'label': _label.text.trim(),
      'first_name': _first.text.trim(),
      'last_name': _last.text.trim(),
      'phone': _phone.text.replaceAll(' ', ''),
      'email': _email.text.trim(),
      'address_line1': _line1.text.trim(),
      'address_line2': _line2.text.trim(),
      'city': 'Singapore',
      'region': widget.address?.region ?? '',
      'postal_code': _postal.text.trim(),
      'country': 'SG',
      'is_default': _isDefault,
    };
    final api = ref.read(apiProvider);
    final ok = await runMutation(
      context,
      () => _isNew
          ? api.createAddress(input)
          : api.updateAddress(widget.address!.id, input),
      success: _isNew ? 'Address added' : 'Address updated',
    );
    if (!mounted) return;
    setState(() => _busy = false);
    if (ok) Navigator.of(context).pop(true);
  }

  @override
  Widget build(BuildContext context) {
    return FormDialog(
      title: _isNew ? 'New delivery address' : 'Edit delivery address',
      subtitle: 'Singapore only. Reap uses these details for shipping.',
      icon: Icons.place_outlined,
      body: Form(
        key: _form,
        child: FieldGrid(
          children: [
            FullWidth(
              child: TextFormField(
                key: const ValueKey('addr-label'),
                controller: _label,
                decoration: const InputDecoration(
                  labelText: 'Label',
                  hintText: 'HQ - Marina One',
                ),
                validator: (v) => Validators.required(v, 'Label'),
              ),
            ),
            FullWidth(
              child: TextFormField(
                key: const ValueKey('addr-line1'),
                controller: _line1,
                decoration: const InputDecoration(
                  labelText: 'Street address',
                  hintText: '7 Straits View',
                ),
                validator: (v) => Validators.required(v, 'Street address'),
              ),
            ),
            TextFormField(
              key: const ValueKey('addr-line2'),
              controller: _line2,
              decoration: const InputDecoration(
                labelText: 'Unit / building',
                hintText: '#20-01 Marina One East Tower',
              ),
            ),
            TextFormField(
              key: const ValueKey('addr-postal'),
              controller: _postal,
              keyboardType: TextInputType.number,
              decoration: const InputDecoration(
                labelText: 'Postal code',
                prefixText: 'Singapore ',
              ),
              validator: Validators.sgPostal,
            ),
            TextFormField(
              key: const ValueKey('addr-first'),
              controller: _first,
              decoration: const InputDecoration(
                labelText: 'Contact first name',
              ),
              validator: (v) => Validators.required(v, 'First name'),
            ),
            TextFormField(
              key: const ValueKey('addr-last'),
              controller: _last,
              decoration: const InputDecoration(labelText: 'Contact last name'),
              validator: (v) => Validators.required(v, 'Last name'),
            ),
            TextFormField(
              key: const ValueKey('addr-phone'),
              controller: _phone,
              keyboardType: TextInputType.phone,
              decoration: const InputDecoration(labelText: 'Phone'),
              validator: Validators.phone,
            ),
            TextFormField(
              key: const ValueKey('addr-email'),
              controller: _email,
              keyboardType: TextInputType.emailAddress,
              decoration: const InputDecoration(labelText: 'Email'),
              validator: Validators.email,
            ),
            FullWidth(
              child: SwitchTile(
                title: 'Default address',
                subtitle: 'Suggested first when the agent asks where to ship',
                value: _isDefault,
                onChanged: (widget.address?.isDefault ?? false)
                    ? null
                    : (v) => setState(() => _isDefault = v),
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
          child: Text(_isNew ? 'Add address' : 'Save changes'),
        ),
      ],
    );
  }
}
