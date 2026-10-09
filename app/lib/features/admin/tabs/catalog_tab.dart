import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api/models.dart';
import '../../../core/format.dart';
import '../../../core/providers.dart';
import '../../../core/theme/tokens.dart';
import '../../../core/widgets/widgets.dart';
import '../admin_common.dart';
import '../admin_providers.dart';

/// Filters catalog items by free text (name, SKU, alias, category) and category.
List<CatalogItem> filterCatalog(
  List<CatalogItem> items, {
  String query = '',
  String? category,
}) {
  final q = query.trim().toLowerCase();
  return items.where((i) {
    if (category != null && i.category != category) return false;
    if (q.isEmpty) return true;
    return i.name.toLowerCase().contains(q) ||
        i.sku.toLowerCase().contains(q) ||
        i.category.toLowerCase().contains(q) ||
        i.aliases.any((a) => a.toLowerCase().contains(q));
  }).toList();
}

class CatalogTab extends ConsumerStatefulWidget {
  const CatalogTab({super.key});

  @override
  ConsumerState<CatalogTab> createState() => _CatalogTabState();
}

class _CatalogTabState extends ConsumerState<CatalogTab> {
  String _query = '';
  String? _category;

  Future<void> _edit(CatalogItem? item, List<CatalogItem> all) async {
    final vendors = await ref
        .read(adminVendorsProvider.future)
        .catchError((_) => <Vendor>[]);
    if (!mounted) return;
    final categories = {for (final i in all) i.category}.toList()..sort();
    final changed = await showDialog<bool>(
      context: context,
      builder: (_) => CatalogItemDialog(
        item: item,
        vendors: vendors,
        categories: categories,
      ),
    );
    if (changed == true) ref.invalidate(adminCatalogProvider);
  }

  @override
  Widget build(BuildContext context) {
    final async = ref.watch(adminCatalogProvider);
    final vendorsById = {
      for (final v in ref.watch(adminVendorsProvider).value ?? const <Vendor>[])
        v.id: v,
    };
    return AsyncValueView<List<CatalogItem>>(
      value: async,
      loading: const AdminSkeleton(rows: 8),
      onRetry: () => ref.invalidate(adminCatalogProvider),
      data: (items) {
        final categories = {for (final i in items) i.category}.toList()..sort();
        final shown = filterCatalog(items, query: _query, category: _category);
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            TabToolbar(
              leading: _Summary(items: items),
              actions: [
                FilledButton.icon(
                  onPressed: () => _edit(null, items),
                  icon: const Icon(Icons.add_rounded, size: 18),
                  label: const Text('New item'),
                ),
              ],
            ),
            const SizedBox(height: AppSpace.lg),
            Wrap(
              spacing: AppSpace.md,
              runSpacing: AppSpace.md,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                SizedBox(
                  width: 340,
                  child: AdminSearchField(
                    hint: 'Search name, SKU or alias',
                    onChanged: (v) => setState(() => _query = v),
                  ),
                ),
                _CategoryFilter(
                  categories: categories,
                  value: _category,
                  onChanged: (c) => setState(() => _category = c),
                ),
              ],
            ),
            const SizedBox(height: AppSpace.lg),
            AppCard(
              padding: EdgeInsets.zero,
              child: shown.isEmpty
                  ? const EmptyState(
                      compact: true,
                      icon: Icons.search_off_rounded,
                      title: 'No matching items',
                      message: 'Try a different search or category.',
                    )
                  : _CatalogTable(
                      items: shown,
                      vendorsById: vendorsById,
                      onEdit: (i) => _edit(i, items),
                    ),
            ),
          ],
        );
      },
    );
  }
}

class _Summary extends StatelessWidget {
  const _Summary({required this.items});
  final List<CatalogItem> items;

  @override
  Widget build(BuildContext context) {
    final active = items.where((i) => i.active).length;
    final manual = items.where((i) => !i.autoApprove).length;
    return Wrap(
      spacing: AppSpace.sm,
      runSpacing: AppSpace.sm,
      children: [
        StatusChip(label: '${items.length} items', tone: Tone.brand),
        StatusChip(label: '$active active', tone: Tone.success),
        StatusChip(label: '$manual need approval', tone: Tone.warning),
      ],
    );
  }
}

class _CategoryFilter extends StatelessWidget {
  const _CategoryFilter({
    required this.categories,
    required this.value,
    required this.onChanged,
  });
  final List<String> categories;
  final String? value;
  final ValueChanged<String?> onChanged;

  @override
  Widget build(BuildContext context) {
    return Wrap(
      spacing: AppSpace.xs,
      runSpacing: AppSpace.xs,
      children: [
        ChoiceChip(
          label: const Text('All'),
          selected: value == null,
          onSelected: (_) => onChanged(null),
        ),
        for (final c in categories)
          ChoiceChip(
            label: Text(c),
            selected: value == c,
            onSelected: (s) => onChanged(s ? c : null),
          ),
      ],
    );
  }
}

class _CatalogTable extends StatelessWidget {
  const _CatalogTable({
    required this.items,
    required this.vendorsById,
    required this.onEdit,
  });
  final List<CatalogItem> items;
  final Map<String, Vendor> vendorsById;
  final ValueChanged<CatalogItem> onEdit;

  String _vendorSummary(CatalogItem i) {
    if (i.preferredVendorIds.isEmpty) return 'None';
    final first =
        vendorsById[i.preferredVendorIds.first]?.name ??
        '${i.preferredVendorIds.length} vendor'
            '${i.preferredVendorIds.length == 1 ? '' : 's'}';
    final more = vendorsById.containsKey(i.preferredVendorIds.first)
        ? i.preferredVendorIds.length - 1
        : 0;
    return more > 0 ? '$first +$more' : first;
  }

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return LayoutBuilder(
      builder: (context, c) {
        final wide = c.maxWidth >= 860;
        final rows = <Widget>[];
        if (wide) {
          rows.add(
            Container(
              color: jc.surfaceMuted,
              padding: const EdgeInsets.symmetric(
                horizontal: AppSpace.lg,
                vertical: AppSpace.md,
              ),
              child: DefaultTextStyle.merge(
                style: context.tt.labelMedium?.copyWith(
                  color: jc.textSecondary,
                ),
                child: const Row(
                  children: [
                    Expanded(flex: 5, child: Text('ITEM')),
                    Expanded(flex: 3, child: Text('CATEGORY')),
                    Expanded(flex: 2, child: Text('DEFAULT QTY')),
                    Expanded(flex: 2, child: Text('MAX UNIT')),
                    Expanded(flex: 3, child: Text('PREFERRED VENDOR')),
                    Expanded(flex: 3, child: Text('RULES')),
                    SizedBox(width: 40),
                  ],
                ),
              ),
            ),
          );
        }
        for (var n = 0; n < items.length; n++) {
          final i = items[n];
          if (n > 0 || wide) rows.add(const Divider());
          rows.add(
            InkWell(
              key: ValueKey('catalog-row-${i.id}'),
              onTap: () => onEdit(i),
              child: Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: AppSpace.lg,
                  vertical: AppSpace.md,
                ),
                child: wide ? _wideRow(context, i) : _narrowRow(context, i),
              ),
            ),
          );
        }
        return Column(children: rows);
      },
    );
  }

  Widget _itemCell(BuildContext context, CatalogItem i) {
    final jc = context.jc;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          i.name,
          style: context.tt.titleSmall?.copyWith(
            color: i.active ? jc.textPrimary : jc.textMuted,
          ),
        ),
        const SizedBox(height: AppSpace.xxs),
        Text(
          [i.sku, if (i.aliases.isNotEmpty) i.aliases.join(', ')].join(' · '),
          style: context.tt.bodySmall?.copyWith(color: jc.textMuted),
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
      ],
    );
  }

  Widget _rules(CatalogItem i) => Wrap(
    spacing: AppSpace.xs,
    runSpacing: AppSpace.xs,
    children: [
      if (i.autoApprove)
        const Tag('Auto-approve', tone: Tone.success)
      else
        const Tag('Needs approval', tone: Tone.warning),
      if (!i.active) const Tag('Inactive'),
    ],
  );

  Widget _wideRow(BuildContext context, CatalogItem i) {
    return Row(
      children: [
        Expanded(flex: 5, child: _itemCell(context, i)),
        Expanded(
          flex: 3,
          child: Align(
            alignment: Alignment.centerLeft,
            child: Tag(i.category, tone: Tone.brand),
          ),
        ),
        Expanded(
          flex: 2,
          child: Text(
            '${i.defaultQty} ${i.unit}'.trim(),
            style: context.tt.bodyMedium,
          ),
        ),
        Expanded(flex: 2, child: MoneyText(i.maxUnitPriceCents)),
        Expanded(
          flex: 3,
          child: Text(
            _vendorSummary(i),
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: context.tt.bodyMedium,
          ),
        ),
        Expanded(flex: 3, child: _rules(i)),
        SizedBox(
          width: 40,
          child: IconButton(
            tooltip: 'Edit ${i.name}',
            icon: const Icon(Icons.edit_outlined, size: 18),
            onPressed: () => onEdit(i),
          ),
        ),
      ],
    );
  }

  Widget _narrowRow(BuildContext context, CatalogItem i) {
    return Row(
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              _itemCell(context, i),
              const SizedBox(height: AppSpace.sm),
              Wrap(
                spacing: AppSpace.xs,
                runSpacing: AppSpace.xs,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [
                  Tag(i.category, tone: Tone.brand),
                  Tag('${i.defaultQty} ${i.unit}'.trim()),
                  Tag('max ${Fmt.money(i.maxUnitPriceCents)}'),
                  _rules(i),
                ],
              ),
            ],
          ),
        ),
        IconButton(
          tooltip: 'Edit ${i.name}',
          icon: const Icon(Icons.edit_outlined, size: 18),
          onPressed: () => onEdit(i),
        ),
      ],
    );
  }
}

/// Create or edit a catalog item. Pops `true` after a save or delete.
class CatalogItemDialog extends ConsumerStatefulWidget {
  const CatalogItemDialog({
    super.key,
    required this.item,
    required this.vendors,
    required this.categories,
  });

  final CatalogItem? item;
  final List<Vendor> vendors;
  final List<String> categories;

  @override
  ConsumerState<CatalogItemDialog> createState() => _CatalogItemDialogState();
}

class _CatalogItemDialogState extends ConsumerState<CatalogItemDialog> {
  final _form = GlobalKey<FormState>();
  late final _sku = TextEditingController(text: widget.item?.sku);
  late final _name = TextEditingController(text: widget.item?.name);
  late final _aliases = TextEditingController(
    text: widget.item?.aliases.join(', '),
  );
  late final _category = TextEditingController(text: widget.item?.category);
  late final _unit = TextEditingController(text: widget.item?.unit ?? 'pack');
  late final _qty = TextEditingController(
    text: '${widget.item?.defaultQty ?? 1}',
  );
  late final _maxPrice = TextEditingController(
    text: widget.item == null
        ? ''
        : centsToInput(widget.item!.maxUnitPriceCents),
  );
  late final _search = TextEditingController(text: widget.item?.searchQuery);
  late final List<String> _vendorIds = [...?widget.item?.preferredVendorIds];
  late bool _autoApprove = widget.item?.autoApprove ?? true;
  late bool _active = widget.item?.active ?? true;
  bool _busy = false;
  String? _vendorError;

  bool get _isNew => widget.item == null;

  @override
  void dispose() {
    for (final c in [
      _sku,
      _name,
      _aliases,
      _category,
      _unit,
      _qty,
      _maxPrice,
      _search,
    ]) {
      c.dispose();
    }
    super.dispose();
  }

  Json _input() => {
    'sku': _sku.text.trim(),
    'name': _name.text.trim(),
    'aliases': _aliases.text
        .split(',')
        .map((a) => a.trim())
        .where((a) => a.isNotEmpty)
        .toList(),
    'category': _category.text.trim(),
    'unit': _unit.text.trim(),
    'default_qty': int.parse(_qty.text.trim()),
    'max_unit_price_cents': parseCents(_maxPrice.text)!,
    'preferred_vendor_ids': _vendorIds,
    'auto_approve': _autoApprove,
    'search_query': _search.text.trim(),
    'active': _active,
  };

  Future<void> _save() async {
    final ok = _form.currentState!.validate();
    setState(
      () => _vendorError = _vendorIds.isEmpty
          ? 'Pick at least one preferred vendor'
          : null,
    );
    if (!ok || _vendorIds.isEmpty) return;
    setState(() => _busy = true);
    final api = ref.read(apiProvider);
    final input = _input();
    final done = await runMutation(
      context,
      () => _isNew
          ? api.createCatalogItem(input)
          : api.updateCatalogItem(widget.item!.id, input),
      success: _isNew ? 'Item added to the catalog' : 'Item updated',
    );
    if (!mounted) return;
    setState(() => _busy = false);
    if (done) Navigator.of(context).pop(true);
  }

  Future<void> _delete() async {
    final item = widget.item!;
    final yes = await confirmAction(
      context,
      title: 'Delete ${item.name}?',
      message:
          'The agent will treat future requests for this item as off-list. '
          'Past orders keep their history.',
      confirmLabel: 'Delete',
      destructive: true,
    );
    if (!yes || !mounted) return;
    setState(() => _busy = true);
    final done = await runMutation(
      context,
      () => ref.read(apiProvider).deleteCatalogItem(item.id),
      success: 'Item deleted',
    );
    if (!mounted) return;
    setState(() => _busy = false);
    if (done) Navigator.of(context).pop(true);
  }

  List<Vendor> get _vendorChoices {
    final list =
        widget.vendors
            .where((v) => v.allowed && !_vendorIds.contains(v.id))
            .toList()
          ..sort((a, b) {
            if (a.officeRelevant != b.officeRelevant) {
              return a.officeRelevant ? -1 : 1;
            }
            return a.priority.compareTo(b.priority);
          });
    return list;
  }

  @override
  Widget build(BuildContext context) {
    final byId = {for (final v in widget.vendors) v.id: v};
    return FormDialog(
      title: _isNew ? 'New catalog item' : 'Edit catalog item',
      subtitle: _isNew
          ? 'Items the agent can match by name or alias.'
          : widget.item!.sku,
      icon: Icons.inventory_2_outlined,
      maxWidth: 680,
      body: Form(
        key: _form,
        child: FieldGrid(
          children: [
            TextFormField(
              key: const ValueKey('field-name'),
              controller: _name,
              decoration: const InputDecoration(labelText: 'Name'),
              validator: (v) => Validators.required(v, 'Name'),
            ),
            TextFormField(
              key: const ValueKey('field-sku'),
              controller: _sku,
              decoration: const InputDecoration(labelText: 'SKU'),
              validator: (v) => Validators.required(v, 'SKU'),
            ),
            FullWidth(
              child: TextFormField(
                controller: _aliases,
                decoration: const InputDecoration(
                  labelText: 'Aliases',
                  helperText: 'Comma separated, e.g. A4 paper, printer paper',
                ),
              ),
            ),
            TextFormField(
              key: const ValueKey('field-category'),
              controller: _category,
              decoration: InputDecoration(
                labelText: 'Category',
                suffixIcon: widget.categories.isEmpty
                    ? null
                    : PopupMenuButton<String>(
                        tooltip: 'Existing categories',
                        icon: const Icon(Icons.expand_more_rounded),
                        onSelected: (c) => setState(() => _category.text = c),
                        itemBuilder: (_) => [
                          for (final c in widget.categories)
                            PopupMenuItem(value: c, child: Text(c)),
                        ],
                      ),
              ),
              validator: (v) => Validators.required(v, 'Category'),
            ),
            TextFormField(
              controller: _unit,
              decoration: const InputDecoration(
                labelText: 'Unit',
                hintText: 'ream, box, pack',
              ),
            ),
            TextFormField(
              key: const ValueKey('field-qty'),
              controller: _qty,
              keyboardType: TextInputType.number,
              decoration: const InputDecoration(
                labelText: 'Default quantity',
                helperText: 'Used when someone asks for "the usual"',
              ),
              validator: Validators.positiveInt,
            ),
            TextFormField(
              key: const ValueKey('field-max-price'),
              controller: _maxPrice,
              keyboardType: const TextInputType.numberWithOptions(
                decimal: true,
              ),
              decoration: const InputDecoration(
                labelText: 'Max unit price',
                prefixText: r'S$ ',
                helperText: 'Offers above this need approval',
              ),
              validator: Validators.money,
            ),
            FullWidth(
              child: TextFormField(
                controller: _search,
                decoration: const InputDecoration(
                  labelText: 'Search query',
                  helperText: 'What the agent searches for at each vendor',
                ),
              ),
            ),
            FullWidth(
              child: _VendorPicker(
                selected: [
                  for (final id in _vendorIds)
                    byId[id] ??
                        Vendor(
                          id: id,
                          domain: id,
                          name: 'Unknown vendor',
                          reapMerchantName: '',
                          category: '',
                          country: 'SG',
                          allowed: false,
                          officeRelevant: false,
                          priority: 0,
                        ),
                ],
                choices: _vendorChoices,
                error: _vendorError,
                onAdd: (v) => setState(() {
                  _vendorIds.add(v.id);
                  _vendorError = null;
                }),
                onRemove: (v) => setState(() => _vendorIds.remove(v.id)),
              ),
            ),
            SwitchTile(
              title: 'Auto-approve',
              subtitle: 'Within limits, no approver needed',
              value: _autoApprove,
              onChanged: (v) => setState(() => _autoApprove = v),
            ),
            SwitchTile(
              title: 'Active',
              subtitle: 'Inactive items are never matched',
              value: _active,
              onChanged: (v) => setState(() => _active = v),
            ),
          ],
        ),
      ),
      actions: [
        if (!_isNew)
          TextButton.icon(
            onPressed: _busy ? null : _delete,
            style: TextButton.styleFrom(foregroundColor: context.jc.danger),
            icon: const Icon(Icons.delete_outline_rounded, size: 18),
            label: const Text('Delete'),
          ),
        TextButton(
          onPressed: _busy ? null : () => Navigator.of(context).pop(false),
          child: const Text('Cancel'),
        ),
        FilledButton(
          onPressed: _busy ? null : _save,
          child: Text(_isNew ? 'Add item' : 'Save changes'),
        ),
      ],
    );
  }
}

class _VendorPicker extends StatelessWidget {
  const _VendorPicker({
    required this.selected,
    required this.choices,
    required this.onAdd,
    required this.onRemove,
    this.error,
  });

  final List<Vendor> selected;
  final List<Vendor> choices;
  final ValueChanged<Vendor> onAdd;
  final ValueChanged<Vendor> onRemove;
  final String? error;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('Preferred vendors', style: context.tt.titleSmall),
        Text(
          'In order of preference. The agent searches these first.',
          style: context.tt.bodySmall,
        ),
        const SizedBox(height: AppSpace.sm),
        Wrap(
          spacing: AppSpace.sm,
          runSpacing: AppSpace.sm,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            for (var n = 0; n < selected.length; n++)
              InputChip(
                avatar: CircleAvatar(
                  backgroundColor: jc.brandSubtle,
                  child: Text(
                    '${n + 1}',
                    style: context.tt.labelSmall?.copyWith(color: jc.brand),
                  ),
                ),
                label: Text(selected[n].name),
                onDeleted: () => onRemove(selected[n]),
              ),
            PopupMenuButton<Vendor>(
              tooltip: 'Add vendor',
              enabled: choices.isNotEmpty,
              onSelected: onAdd,
              itemBuilder: (_) => [
                for (final v in choices)
                  PopupMenuItem(
                    value: v,
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(v.name),
                        Text(v.domain, style: context.tt.bodySmall),
                      ],
                    ),
                  ),
              ],
              child: Chip(
                avatar: Icon(Icons.add_rounded, size: 16, color: jc.brand),
                label: Text(
                  'Add vendor',
                  style: context.tt.labelMedium?.copyWith(color: jc.brand),
                ),
              ),
            ),
          ],
        ),
        if (error != null) ...[
          const SizedBox(height: AppSpace.xs),
          Text(error!, style: context.tt.bodySmall?.copyWith(color: jc.danger)),
        ],
      ],
    );
  }
}
