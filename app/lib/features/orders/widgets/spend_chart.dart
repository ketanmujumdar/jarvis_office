import 'dart:math' as math;

import 'package:fl_chart/fl_chart.dart';
import 'package:flutter/material.dart';

import '../../../core/api/models.dart';
import '../../../core/format.dart';
import '../../../core/theme/tokens.dart';
import '../../../core/widgets/widgets.dart';

/// Monthly completed spend as bars against a dashed budget line, with a
/// month-to-date budget meter underneath.
class SpendChartCard extends StatelessWidget {
  const SpendChartCard({super.key, required this.spend});
  final SpendSummary spend;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final months = [...spend.months]
      ..sort((a, b) => a.month.compareTo(b.month));
    final budget = spend.monthlyBudgetCents;
    final legend = Wrap(
      spacing: AppSpace.md,
      runSpacing: AppSpace.xs,
      children: [
        _Legend(color: jc.brand, label: 'Spend'),
        _Legend(color: jc.danger, label: 'Over budget'),
        _Legend(color: jc.textMuted, label: 'Budget', dashed: true),
      ],
    );
    final narrow = MediaQuery.sizeOf(context).width < 600;
    return AppCard(
      title: 'Monthly spend vs budget',
      subtitle: 'Completed orders, last ${months.length} months',
      // Below 600px the legend moves under the title so the title is not squeezed.
      trailing: narrow ? null : legend,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (narrow) ...[legend, const SizedBox(height: AppSpace.md)],
          SizedBox(
            height: 240,
            child: months.isEmpty
                ? Center(
                    child: Text(
                      'No completed spend yet.',
                      style: context.tt.bodyMedium,
                    ),
                  )
                : _Bars(months: months, budgetCents: budget),
          ),
          const SizedBox(height: AppSpace.lg),
          BudgetMeter(spentCents: spend.monthToDateCents, budgetCents: budget),
        ],
      ),
    );
  }
}

class _Legend extends StatelessWidget {
  const _Legend({
    required this.color,
    required this.label,
    this.dashed = false,
  });
  final Color color;
  final String label;
  final bool dashed;

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        dashed
            ? Row(
                children: [
                  for (var i = 0; i < 3; i++)
                    Container(
                      width: 3,
                      height: 2,
                      margin: const EdgeInsets.only(right: 2),
                      color: color,
                    ),
                ],
              )
            : Container(
                width: 10,
                height: 10,
                decoration: BoxDecoration(
                  color: color,
                  borderRadius: const BorderRadius.all(Radius.circular(3)),
                ),
              ),
        const SizedBox(width: AppSpace.xs + 2),
        Text(label, style: context.tt.labelSmall),
      ],
    );
  }
}

class _Bars extends StatelessWidget {
  const _Bars({required this.months, required this.budgetCents});
  final List<MonthSpend> months;
  final int budgetCents;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final maxSpend = months.map((m) => m.spendCents).fold(0, math.max);
    final raw = math.max(math.max(maxSpend, budgetCents), 10000) * 1.18 / 100;
    final interval = _niceInterval(raw / 4);
    // Round the top up to a whole tick so the axis never shows an odd label.
    final top = (raw / interval).ceil() * interval;
    final axisStyle = context.tt.labelSmall?.copyWith(color: jc.textMuted);

    return BarChart(
      BarChartData(
        maxY: top,
        minY: 0,
        alignment: BarChartAlignment.spaceAround,
        gridData: FlGridData(
          drawVerticalLine: false,
          horizontalInterval: interval,
          getDrawingHorizontalLine: (_) =>
              FlLine(color: jc.border, strokeWidth: 1),
        ),
        borderData: FlBorderData(show: false),
        titlesData: FlTitlesData(
          topTitles: const AxisTitles(),
          rightTitles: const AxisTitles(),
          leftTitles: AxisTitles(
            sideTitles: SideTitles(
              showTitles: true,
              reservedSize: 56,
              interval: interval,
              getTitlesWidget: (v, meta) {
                if (v == meta.max && v % interval != 0) {
                  return const SizedBox.shrink();
                }
                return SideTitleWidget(
                  meta: meta,
                  child: Text(
                    Fmt.moneyCompact((v * 100).round()),
                    style: axisStyle,
                    maxLines: 1,
                    softWrap: false,
                  ),
                );
              },
            ),
          ),
          bottomTitles: AxisTitles(
            sideTitles: SideTitles(
              showTitles: true,
              reservedSize: 28,
              getTitlesWidget: (v, meta) {
                final i = v.toInt();
                if (i < 0 || i >= months.length) return const SizedBox();
                return SideTitleWidget(
                  meta: meta,
                  child: Text(
                    Fmt.monthShort(months[i].month),
                    style: axisStyle,
                  ),
                );
              },
            ),
          ),
        ),
        extraLinesData: ExtraLinesData(
          horizontalLines: [
            if (budgetCents > 0)
              HorizontalLine(
                y: budgetCents / 100,
                color: jc.textMuted,
                strokeWidth: 1.5,
                dashArray: const [6, 4],
                label: HorizontalLineLabel(
                  show: true,
                  alignment: Alignment.topRight,
                  style: context.tt.labelSmall?.copyWith(color: jc.textMuted),
                  labelResolver: (_) =>
                      'Budget ${Fmt.moneyCompact(budgetCents)}',
                ),
              ),
          ],
        ),
        barTouchData: BarTouchData(
          touchTooltipData: BarTouchTooltipData(
            getTooltipColor: (_) => jc.textPrimary,
            tooltipBorderRadius: AppRadius.smAll,
            fitInsideHorizontally: true,
            fitInsideVertically: true,
            getTooltipItem: (group, _, _, _) {
              final m = months[group.x];
              return BarTooltipItem(
                '${Fmt.monthShort(m.month)}  ${Fmt.money(m.spendCents)}\n',
                TextStyle(
                  color: jc.surface,
                  fontWeight: FontWeight.w600,
                  fontSize: 12,
                ),
                children: [
                  TextSpan(
                    text: '${m.orders} ${m.orders == 1 ? 'order' : 'orders'}',
                    style: TextStyle(
                      color: jc.surface.withValues(alpha: 0.75),
                      fontWeight: FontWeight.w400,
                      fontSize: 11,
                    ),
                  ),
                ],
              );
            },
          ),
        ),
        barGroups: [
          for (final (i, m) in months.indexed)
            BarChartGroupData(
              x: i,
              barRods: [
                BarChartRodData(
                  toY: m.spendCents / 100,
                  width: 28,
                  color: budgetCents > 0 && m.spendCents > budgetCents
                      ? jc.danger
                      : (i == months.length - 1
                            ? jc.brand
                            : jc.brand.withValues(alpha: 0.55)),
                  borderRadius: const BorderRadius.vertical(
                    top: Radius.circular(6),
                  ),
                ),
              ],
            ),
        ],
      ),
    );
  }

  static double _niceInterval(double raw) {
    if (raw <= 0) return 1;
    final exp = math.pow(10, (math.log(raw) / math.ln10).floor()).toDouble();
    for (final m in [1, 2, 2.5, 5, 10]) {
      if (raw <= m * exp) return m * exp;
    }
    return 10 * exp;
  }
}

/// Horizontal progress bar of month-to-date spend against the budget.
class BudgetMeter extends StatelessWidget {
  const BudgetMeter({
    super.key,
    required this.spentCents,
    required this.budgetCents,
  });
  final int spentCents;
  final int budgetCents;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final ratio = budgetCents <= 0 ? 0.0 : spentCents / budgetCents;
    final tone = ratio >= 1
        ? Tone.danger
        : (ratio >= 0.8 ? Tone.warning : Tone.success);
    final left = budgetCents - spentCents;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            Text('This month', style: context.tt.labelLarge),
            const SizedBox(width: AppSpace.md),
            Expanded(
              child: Text(
                '${Fmt.money(spentCents)} of ${Fmt.money(budgetCents)}',
                textAlign: TextAlign.right,
                overflow: TextOverflow.ellipsis,
                style: context.tt.labelLarge?.copyWith(
                  fontFeatures: const [FontFeature.tabularFigures()],
                ),
              ),
            ),
          ],
        ),
        const SizedBox(height: AppSpace.sm),
        ClipRRect(
          borderRadius: AppRadius.pillAll,
          child: LinearProgressIndicator(
            value: ratio.clamp(0.0, 1.0),
            minHeight: 8,
            color: jc.fg(tone),
            backgroundColor: jc.surfaceMuted,
          ),
        ),
        const SizedBox(height: AppSpace.xs + 2),
        Text(
          left >= 0
              ? '${Fmt.money(left)} left · ${Fmt.percent(spentCents, budgetCents)} used'
              : '${Fmt.money(-left)} over budget',
          style: context.tt.bodySmall?.copyWith(
            color: left >= 0 ? jc.textSecondary : jc.danger,
          ),
        ),
      ],
    );
  }
}
