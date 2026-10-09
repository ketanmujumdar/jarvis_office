import 'package:flutter/material.dart';

import '../../../core/api/models.dart';
import '../../../core/format.dart';
import '../../../core/theme/tokens.dart';
import '../../../core/widgets/widgets.dart';
import '../../approvals/reason_copy.dart';
import '../request_logic.dart';
import 'progress_tracker.dart';

/// The parsed line items with quantity, catalog match and policy decision.
class LineItemsCard extends StatelessWidget {
  const LineItemsCard({super.key, required this.detail});
  final RequestDetail detail;

  @override
  Widget build(BuildContext context) {
    final items = [...detail.lineItems]
      ..sort((a, b) => a.position.compareTo(b.position));
    final busy =
        detail.request.status == RequestStatus.parsing && items.isEmpty;
    return SectionCard(
      title: 'Line items',
      subtitle: busy
          ? 'Understanding the request…'
          : '${items.length} ${items.length == 1 ? 'item' : 'items'} parsed from “${detail.request.rawUtterance}”',
      icon: Icons.checklist_rounded,
      child: busy
          ? const _Shimmer(rows: 3)
          : Column(
              children: [
                for (var i = 0; i < items.length; i++) ...[
                  if (i > 0)
                    Divider(height: AppSpace.lg, color: context.jc.border),
                  _LineItemRow(item: items[i]),
                ],
              ],
            ),
    );
  }
}

class _LineItemRow extends StatelessWidget {
  const _LineItemRow({required this.item});
  final LineItem item;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Container(
          width: 40,
          height: 40,
          alignment: Alignment.center,
          decoration: BoxDecoration(
            color: jc.surfaceMuted,
            borderRadius: AppRadius.mdAll,
            border: Border.all(color: jc.border),
          ),
          child: Text(
            '×${item.qty}',
            style: context.tt.labelLarge?.copyWith(
              color: jc.textPrimary,
              fontFeatures: const [FontFeature.tabularFigures()],
            ),
          ),
        ),
        const SizedBox(width: AppSpace.md),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                item.description,
                style: context.tt.bodyLarge?.copyWith(
                  fontWeight: FontWeight.w600,
                  color: jc.textPrimary,
                ),
              ),
              const SizedBox(height: AppSpace.xs),
              Wrap(
                spacing: AppSpace.xs,
                runSpacing: AppSpace.xs,
                children: [
                  if (item.policyDecision != Decision.none)
                    StatusChip.decision(item.policyDecision),
                  if (item.isOffList)
                    const Tag('Off-list', tone: Tone.warning)
                  else
                    const Tag(
                      'Catalog',
                      tone: Tone.brand,
                      icon: Icons.bookmark_rounded,
                    ),
                  if (item.urgency == 'urgent')
                    const Tag(
                      'Urgent',
                      tone: Tone.danger,
                      icon: Icons.bolt_rounded,
                    ),
                ],
              ),
              for (final r in item.reasons) ...[
                const SizedBox(height: AppSpace.xs),
                _ReasonLine(reason: r, description: item.description),
              ],
            ],
          ),
        ),
      ],
    );
  }
}

/// One policy reason: a labelled tag plus a short, user-facing sentence.
class _ReasonLine extends StatelessWidget {
  const _ReasonLine({required this.reason, required this.description});
  final Reason reason;
  final String description;

  @override
  Widget build(BuildContext context) {
    final copy = ReasonCopy.of(reason.code);
    final msg = reasonSentence(reason, description);
    return Wrap(
      spacing: AppSpace.sm,
      runSpacing: AppSpace.xxs,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: [
        Tag(copy.title, tone: copy.tone, icon: copy.icon),
        if (msg.isNotEmpty) Text(msg, style: context.tt.bodySmall),
      ],
    );
  }
}

final _linePrefix = RegExp(r'^Line [0-9a-fA-F-]{8,}:\s*');

/// The reason text without internal ids or a repeat of the item name.
String reasonSentence(Reason r, String description) {
  if (r.code == 'NO_OFFER') return 'No approved vendor has this in stock.';
  var m = r.message.trim().replaceFirst(_linePrefix, '');
  final d = description.trim();
  if (d.isNotEmpty && m.toLowerCase().startsWith('${d.toLowerCase()}:')) {
    m = m.substring(d.length + 1).trim();
  }
  if (m.isNotEmpty) m = m[0].toUpperCase() + m.substring(1);
  return m;
}

/// Offer comparison table for one line item. The best offer is highlighted
/// and the saving against the next cheapest offer is called out.
class QuoteCard extends StatelessWidget {
  const QuoteCard({super.key, required this.quote, this.searching = false});

  final LineQuote quote;
  final bool searching;

  @override
  Widget build(BuildContext context) {
    final li = quote.lineItem;
    final best = quote.best;
    Widget body;
    if (quote.offers.isEmpty) {
      body = searching
          ? const _Shimmer(rows: 2)
          : const Callout(
              tone: Tone.warning,
              icon: Icons.search_off_rounded,
              title: 'No allowed vendor has this item',
              message: 'Only allow-listed merchants are searched. Try a catalog alternative.',
            );
    } else {
      body = LayoutBuilder(
        builder: (context, c) {
          final wide = c.maxWidth >= 600;
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              if (wide) const _HeaderRow(),
              for (final o in quote.offers.take(5))
                _OfferRow(
                  offer: o,
                  isBest: best != null && o.id == best.id,
                  wide: wide,
                ),
            ],
          );
        },
      );
    }
    return SectionCard(
      title: li.description,
      subtitle:
          'Qty ${li.qty} · ${quote.offers.length} '
          '${quote.offers.length == 1 ? 'offer' : 'offers'} compared',
      icon: Icons.storefront_outlined,
      trailing: quote.savingsCents > 0
          ? Tag(
              'Saves ${Fmt.money(quote.savingsCents)}',
              tone: Tone.success,
              icon: Icons.savings_outlined,
            )
          : null,
      child: body,
    );
  }
}

class _HeaderRow extends StatelessWidget {
  const _HeaderRow();

  @override
  Widget build(BuildContext context) {
    final style = context.tt.labelSmall?.copyWith(color: context.jc.textMuted);
    Widget num(String t) => SizedBox(
      width: 96,
      child: Text(t.toUpperCase(), style: style, textAlign: TextAlign.right),
    );
    return Padding(
      padding: const EdgeInsets.fromLTRB(
        AppSpace.md,
        0,
        AppSpace.md,
        AppSpace.sm,
      ),
      child: Row(
        children: [
          const SizedBox(width: 36),
          Expanded(child: Text('MERCHANT', style: style)),
          num('Unit'),
          num('Shipping'),
          num('Total'),
        ],
      ),
    );
  }
}

class _OfferRow extends StatelessWidget {
  const _OfferRow({
    required this.offer,
    required this.isBest,
    required this.wide,
  });

  final Offer offer;
  final bool isBest;
  final bool wide;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final muted = !offer.available;
    final numStyle = context.tt.bodyMedium?.copyWith(
      color: muted ? jc.textMuted : jc.textPrimary,
    );
    final title = [
      offer.title,
      if (offer.variantName.isNotEmpty) offer.variantName,
    ].join(' · ');
    final merchant = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Wrap(
          spacing: AppSpace.sm,
          runSpacing: AppSpace.xxs,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            Text(
              offer.merchantName,
              style: context.tt.bodyMedium?.copyWith(
                fontWeight: FontWeight.w600,
                color: muted ? jc.textMuted : jc.textPrimary,
              ),
            ),
            if (isBest)
              const Tag(
                'Best price',
                tone: Tone.success,
                icon: Icons.emoji_events_rounded,
              ),
            if (muted) const Tag('Unavailable'),
          ],
        ),
        const SizedBox(height: AppSpace.xxs),
        Text(
          title,
          style: context.tt.bodySmall,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
        if (offer.eta.isNotEmpty)
          Text(
            offer.eta,
            style: context.tt.labelSmall?.copyWith(color: jc.textMuted),
          ),
        if (!wide) ...[
          const SizedBox(height: AppSpace.xxs),
          Text(
            '${Fmt.money(offer.unitPriceCents)} each · '
            '${offer.shippingCents == 0 ? 'shipping TBC' : '${Fmt.money(offer.shippingCents)} shipping'}',
            style: context.tt.bodySmall,
          ),
        ],
      ],
    );
    Widget cell(Widget child) => SizedBox(
      width: 96,
      child: Align(alignment: Alignment.centerRight, child: child),
    );
    final rank = Container(
      width: 24,
      height: 24,
      alignment: Alignment.center,
      decoration: BoxDecoration(
        color: isBest ? jc.success : jc.surfaceMuted,
        shape: BoxShape.circle,
      ),
      child: Text(
        '${offer.rank == 0 ? '–' : offer.rank}',
        style: context.tt.labelSmall?.copyWith(
          color: isBest ? jc.surface : jc.textSecondary,
          fontWeight: FontWeight.w700,
        ),
      ),
    );
    return Semantics(
      label: isBest ? 'Best offer' : null,
      container: true,
      child: Container(
        key: ValueKey('offer-${offer.id}'),
        margin: const EdgeInsets.only(bottom: AppSpace.xs),
        padding: const EdgeInsets.symmetric(
          horizontal: AppSpace.md,
          vertical: AppSpace.md,
        ),
        decoration: BoxDecoration(
          color: isBest ? jc.successSubtle : null,
          borderRadius: AppRadius.mdAll,
          border: Border.all(
            color: isBest ? jc.success.withValues(alpha: 0.45) : jc.border,
          ),
        ),
        child: Row(
          children: [
            rank,
            const SizedBox(width: AppSpace.md),
            Expanded(child: merchant),
            if (wide) ...[
              cell(MoneyText(offer.unitPriceCents, style: numStyle)),
              cell(
                offer.shippingCents == 0
                    ? Text('TBC', style: context.tt.bodySmall)
                    : MoneyText(offer.shippingCents, style: numStyle),
              ),
            ],
            cell(
              MoneyText(
                offer.landedCostCents,
                style: numStyle?.copyWith(
                  fontWeight: FontWeight.w700,
                  color: isBest ? jc.success : null,
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Order totals with savings and the overall policy decision.
class TotalsCard extends StatelessWidget {
  const TotalsCard({super.key, required this.detail});
  final RequestDetail detail;

  @override
  Widget build(BuildContext context) {
    final r = detail.request;
    final jc = context.jc;
    final savings = totalSavings(detail);
    Widget line(String label, Widget value, {bool strong = false}) => Padding(
      padding: const EdgeInsets.symmetric(vertical: AppSpace.xs),
      child: Row(
        children: [
          Expanded(
            child: Text(
              label,
              style: strong
                  ? context.tt.titleMedium
                  : context.tt.bodyMedium?.copyWith(color: jc.textSecondary),
            ),
          ),
          value,
        ],
      ),
    );
    return SectionCard(
      title: 'Summary',
      icon: Icons.receipt_long_outlined,
      trailing: r.decision == Decision.none
          ? null
          : StatusChip.decision(r.decision),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          line('Subtotal', MoneyText(r.subtotalCents)),
          line(
            'Shipping (est.)',
            r.shippingCents == 0
                ? Text('Calculated at checkout', style: context.tt.bodySmall)
                : MoneyText(r.shippingCents),
          ),
          Divider(height: AppSpace.lg, color: jc.border),
          line(
            'Total',
            MoneyText(
              r.totalCents,
              style: context.tt.titleLarge?.copyWith(color: jc.textPrimary),
            ),
            strong: true,
          ),
          if (savings > 0) ...[
            const SizedBox(height: AppSpace.sm),
            Container(
              padding: const EdgeInsets.all(AppSpace.md),
              decoration: BoxDecoration(
                color: jc.successSubtle,
                borderRadius: AppRadius.mdAll,
              ),
              child: Row(
                children: [
                  Icon(Icons.savings_outlined, color: jc.success, size: 20),
                  const SizedBox(width: AppSpace.sm),
                  Expanded(
                    child: Text(
                      'You save ${Fmt.money(savings)} vs the next best offers',
                      style: context.tt.bodyMedium?.copyWith(
                        color: jc.success,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ],
        ],
      ),
    );
  }
}

/// Placeholder rows while data is loading.
class _Shimmer extends StatelessWidget {
  const _Shimmer({required this.rows});
  final int rows;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return Column(
      children: [
        for (var i = 0; i < rows; i++)
          Container(
            height: 44,
            margin: const EdgeInsets.only(bottom: AppSpace.sm),
            decoration: BoxDecoration(
              color: jc.surfaceMuted,
              borderRadius: AppRadius.mdAll,
            ),
          ),
        const LinearProgressIndicator(minHeight: 2),
      ],
    );
  }
}
