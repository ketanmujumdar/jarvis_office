import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api/models.dart';
import '../../../core/format.dart';
import '../../../core/providers.dart';
import '../../../core/theme/tokens.dart';
import '../../../core/widgets/widgets.dart';
import '../admin_common.dart';
import '../admin_providers.dart';

/// Month-to-date spend for the budget meter. Optional: errors hide the meter.
final _mtdSpendProvider = FutureProvider<SpendSummary>(
  (ref) => ref.watch(apiProvider).monthlySpend(months: 1),
  retry: (_, _) => null,
);

class PolicyTab extends ConsumerWidget {
  const PolicyTab({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return AsyncValueView<PolicyConfig>(
      value: ref.watch(adminPolicyProvider),
      loading: const AdminSkeleton(rows: 3, rowHeight: 96),
      onRetry: () => ref.invalidate(adminPolicyProvider),
      data: (p) => PolicyForm(
        // Rebuild the form when a new server version arrives.
        key: ValueKey(p.updatedAt?.toIso8601String() ?? 'policy'),
        policy: p,
      ),
    );
  }
}

class PolicyForm extends ConsumerStatefulWidget {
  const PolicyForm({super.key, required this.policy});
  final PolicyConfig policy;

  @override
  ConsumerState<PolicyForm> createState() => _PolicyFormState();
}

class _PolicyFormState extends ConsumerState<PolicyForm> {
  final _form = GlobalKey<FormState>();
  late final _perOrder = TextEditingController(
    text: centsToInput(widget.policy.perOrderLimitCents),
  );
  late final _monthly = TextEditingController(
    text: centsToInput(widget.policy.monthlyBudgetCents),
  );
  late final _drift = TextEditingController(
    text: _pct(widget.policy.priceDriftPct),
  );
  bool _busy = false;

  static String _pct(double v) =>
      v == v.roundToDouble() ? v.toStringAsFixed(0) : v.toString();

  @override
  void initState() {
    super.initState();
    for (final c in [_perOrder, _monthly, _drift]) {
      c.addListener(() => setState(() {}));
    }
  }

  @override
  void dispose() {
    _perOrder.dispose();
    _monthly.dispose();
    _drift.dispose();
    super.dispose();
  }

  bool get _dirty =>
      parseCents(_perOrder.text) != widget.policy.perOrderLimitCents ||
      parseCents(_monthly.text) != widget.policy.monthlyBudgetCents ||
      double.tryParse(_drift.text.trim()) != widget.policy.priceDriftPct;

  static String? _validateDrift(String? v) {
    final d = double.tryParse((v ?? '').trim());
    if (d == null || d < 0 || d > 100) {
      return 'Enter a percentage from 0 to 100';
    }
    return null;
  }

  String? _validateMonthly(String? v) {
    final m = Validators.money(v);
    if (m != null) return m;
    final per = parseCents(_perOrder.text);
    if (per != null && parseCents(v!)! < per) {
      return 'Monthly budget should be at least the per-order limit';
    }
    return null;
  }

  Future<void> _save() async {
    if (!_form.currentState!.validate()) return;
    setState(() => _busy = true);
    final ok = await runMutation(
      context,
      () => ref.read(apiProvider).updatePolicy({
        'per_order_limit_cents': parseCents(_perOrder.text)!,
        'monthly_budget_cents': parseCents(_monthly.text)!,
        'price_drift_pct': double.parse(_drift.text.trim()),
      }),
      success: 'Policy limits saved',
    );
    if (!mounted) return;
    setState(() => _busy = false);
    if (ok) ref.invalidate(adminPolicyProvider);
  }

  void _discard() {
    _perOrder.text = centsToInput(widget.policy.perOrderLimitCents);
    _monthly.text = centsToInput(widget.policy.monthlyBudgetCents);
    _drift.text = _pct(widget.policy.priceDriftPct);
  }

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final updated = widget.policy.updatedAt;
    final fields = Form(
      key: _form,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _LimitField(
            fieldKey: const ValueKey('policy-per-order'),
            icon: Icons.receipt_long_outlined,
            title: 'Per-order limit',
            description:
                'Orders above this total always go to an approver, even if '
                'every item is set to auto-approve.',
            child: TextFormField(
              controller: _perOrder,
              keyboardType: const TextInputType.numberWithOptions(
                decimal: true,
              ),
              decoration: const InputDecoration(prefixText: r'S$ '),
              validator: Validators.money,
            ),
          ),
          const SizedBox(height: AppSpace.md),
          _LimitField(
            fieldKey: const ValueKey('policy-monthly'),
            icon: Icons.calendar_month_outlined,
            title: 'Monthly budget',
            description:
                'Orders that would take month-to-date spend (Singapore time) '
                'past this budget need approval.',
            child: TextFormField(
              controller: _monthly,
              keyboardType: const TextInputType.numberWithOptions(
                decimal: true,
              ),
              decoration: const InputDecoration(prefixText: r'S$ '),
              validator: _validateMonthly,
            ),
          ),
          const SizedBox(height: AppSpace.md),
          _LimitField(
            fieldKey: const ValueKey('policy-drift'),
            icon: Icons.trending_up_rounded,
            title: 'Price drift tolerance',
            description:
                'At checkout, if the live Reap quote is higher than the '
                'approved total by more than this, the order goes back for '
                'approval.',
            child: TextFormField(
              controller: _drift,
              keyboardType: const TextInputType.numberWithOptions(
                decimal: true,
              ),
              decoration: const InputDecoration(suffixText: '%'),
              validator: _validateDrift,
            ),
          ),
          const SizedBox(height: AppSpace.lg),
          Row(
            children: [
              Expanded(
                child: Text(
                  updated == null
                      ? 'Not saved yet'
                      : 'Last updated ${Fmt.relative(updated)}',
                  style: context.tt.bodySmall?.copyWith(color: jc.textMuted),
                ),
              ),
              if (_dirty)
                TextButton(
                  onPressed: _busy ? null : _discard,
                  child: const Text('Discard'),
                ),
              const SizedBox(width: AppSpace.sm),
              FilledButton.icon(
                onPressed: _busy || !_dirty ? null : _save,
                icon: const Icon(Icons.save_outlined, size: 18),
                label: const Text('Save limits'),
              ),
            ],
          ),
        ],
      ),
    );
    final side = _BudgetCard(
      monthlyBudgetCents:
          parseCents(_monthly.text) ?? widget.policy.monthlyBudgetCents,
      perOrderCents:
          parseCents(_perOrder.text) ?? widget.policy.perOrderLimitCents,
    );
    return LayoutBuilder(
      builder: (context, c) {
        if (c.maxWidth < 900) {
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              side,
              const SizedBox(height: AppSpace.lg),
              fields,
            ],
          );
        }
        return Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Expanded(flex: 3, child: fields),
            const SizedBox(width: AppSpace.xl),
            Expanded(flex: 2, child: side),
          ],
        );
      },
    );
  }
}

class _LimitField extends StatelessWidget {
  const _LimitField({
    required this.fieldKey,
    required this.icon,
    required this.title,
    required this.description,
    required this.child,
  });

  final Key fieldKey;
  final IconData icon;
  final String title;
  final String description;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return AppCard(
      key: fieldKey,
      child: LayoutBuilder(
        builder: (context, c) {
          final text = Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Container(
                padding: const EdgeInsets.all(AppSpace.sm),
                decoration: BoxDecoration(
                  color: jc.brandSubtle,
                  borderRadius: AppRadius.mdAll,
                ),
                child: Icon(icon, size: 20, color: jc.brand),
              ),
              const SizedBox(width: AppSpace.md),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(title, style: context.tt.titleMedium),
                    const SizedBox(height: AppSpace.xxs),
                    Text(description, style: context.tt.bodySmall),
                  ],
                ),
              ),
            ],
          );
          if (c.maxWidth < 520) {
            return Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                text,
                const SizedBox(height: AppSpace.md),
                child,
              ],
            );
          }
          return Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(child: text),
              const SizedBox(width: AppSpace.lg),
              SizedBox(width: 180, child: child),
            ],
          );
        },
      ),
    );
  }
}

class _BudgetCard extends ConsumerWidget {
  const _BudgetCard({
    required this.monthlyBudgetCents,
    required this.perOrderCents,
  });
  final int monthlyBudgetCents;
  final int perOrderCents;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final jc = context.jc;
    final spend = ref.watch(_mtdSpendProvider).value;
    final mtd = spend?.monthToDateCents;
    final ratio = (mtd == null || monthlyBudgetCents <= 0)
        ? 0.0
        : (mtd / monthlyBudgetCents).clamp(0.0, 1.0);
    final tone = ratio >= 0.9
        ? Tone.danger
        : ratio >= 0.7
        ? Tone.warning
        : Tone.success;
    return AppCard(
      title: 'This month',
      subtitle: 'How the limits apply right now',
      leading: Icon(Icons.insights_rounded, color: jc.brand),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (mtd != null) ...[
            Wrap(
              crossAxisAlignment: WrapCrossAlignment.end,
              spacing: AppSpace.sm,
              children: [
                MoneyText(mtd, style: context.tt.headlineSmall),
                Padding(
                  padding: const EdgeInsets.only(bottom: 3),
                  child: Text(
                    'of ${Fmt.money(monthlyBudgetCents)}',
                    style: context.tt.bodySmall,
                  ),
                ),
              ],
            ),
            const SizedBox(height: AppSpace.sm),
            ClipRRect(
              borderRadius: AppRadius.pillAll,
              child: LinearProgressIndicator(
                value: ratio,
                minHeight: 8,
                color: jc.fg(tone),
                backgroundColor: jc.surfaceMuted,
              ),
            ),
            const SizedBox(height: AppSpace.xs),
            Text(
              '${Fmt.percent(mtd, monthlyBudgetCents)} of the monthly budget used',
              style: context.tt.bodySmall,
            ),
            const SizedBox(height: AppSpace.lg),
          ],
          _Rule(
            icon: Icons.check_circle_outline_rounded,
            tone: Tone.success,
            text:
                'Catalog items under their max unit price, in orders up to '
                '${Fmt.money(perOrderCents)}, are auto-approved.',
          ),
          _Rule(
            icon: Icons.pending_outlined,
            tone: Tone.warning,
            text:
                'Larger orders, off-list items and items marked "needs '
                'approval" go to an approver.',
          ),
          const _Rule(
            icon: Icons.lock_outline_rounded,
            tone: Tone.brand,
            text:
                'Limits are enforced in code, not by the AI agent. The agent '
                'cannot override them.',
          ),
        ],
      ),
    );
  }
}

class _Rule extends StatelessWidget {
  const _Rule({required this.icon, required this.tone, required this.text});
  final IconData icon;
  final Tone tone;
  final String text;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpace.sm),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: 18, color: context.jc.fg(tone)),
          const SizedBox(width: AppSpace.sm),
          Expanded(child: Text(text, style: context.tt.bodyMedium)),
        ],
      ),
    );
  }
}
