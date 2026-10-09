import 'package:flutter/material.dart';

import '../../../core/api/models.dart';
import '../../../core/format.dart';
import '../../../core/theme/tokens.dart';
import '../../../core/widgets/widgets.dart';

/// Side-by-side comparison of the top three offers for one line item.
/// The best (lowest landed cost) is marked, the selected one outlined, and
/// the others show how much more they cost.
class QuoteComparison extends StatelessWidget {
  const QuoteComparison({
    super.key,
    required this.offers,
    this.selectedOfferId,
  });

  final List<Offer> offers;
  final String? selectedOfferId;

  /// Top three offers by rank (rank 0 treated as unranked, sorted by price).
  static List<Offer> topThree(List<Offer> offers) {
    final sorted = [...offers]
      ..sort((a, b) {
        final ra = a.rank <= 0 ? 1 << 20 : a.rank;
        final rb = b.rank <= 0 ? 1 << 20 : b.rank;
        final c = ra.compareTo(rb);
        return c != 0 ? c : a.landedCostCents.compareTo(b.landedCostCents);
      });
    return sorted.take(3).toList();
  }

  @override
  Widget build(BuildContext context) {
    final top = topThree(offers);
    if (top.isEmpty) {
      return Container(
        padding: const EdgeInsets.all(AppSpace.lg),
        decoration: BoxDecoration(
          color: context.jc.surfaceMuted,
          borderRadius: AppRadius.mdAll,
        ),
        child: Row(
          children: [
            Icon(Icons.search_off_rounded, color: context.jc.textMuted),
            const SizedBox(width: AppSpace.sm),
            Expanded(
              child: Text(
                'No quotes from allowed vendors.',
                style: context.tt.bodyMedium,
              ),
            ),
          ],
        ),
      );
    }
    final available = top.where((o) => o.available).toList();
    final best = available.isEmpty
        ? null
        : available.reduce(
            (a, b) => a.landedCostCents <= b.landedCostCents ? a : b,
          );

    return LayoutBuilder(
      builder: (context, c) {
        final cards = [
          for (final o in top)
            _OfferCard(
              offer: o,
              isBest: best != null && o.id == best.id,
              isSelected: o.id == selectedOfferId,
              deltaCents: best == null || o.id == best.id
                  ? null
                  : o.landedCostCents - best.landedCostCents,
            ),
        ];
        if (c.maxWidth < 560) {
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              for (final card in cards) ...[
                card,
                if (card != cards.last) const SizedBox(height: AppSpace.sm),
              ],
            ],
          );
        }
        return IntrinsicHeight(
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              for (var i = 0; i < 3; i++) ...[
                if (i > 0) const SizedBox(width: AppSpace.sm),
                Expanded(
                  child: i < cards.length ? cards[i] : const SizedBox.shrink(),
                ),
              ],
            ],
          ),
        );
      },
    );
  }
}

class _OfferCard extends StatelessWidget {
  const _OfferCard({
    required this.offer,
    required this.isBest,
    required this.isSelected,
    required this.deltaCents,
  });

  final Offer offer;
  final bool isBest;
  final bool isSelected;
  final int? deltaCents;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final borderColor = isSelected
        ? jc.brand
        : (isBest ? jc.success.withValues(alpha: 0.5) : jc.border);
    return Container(
      key: Key('offer-${offer.id}'),
      padding: const EdgeInsets.all(AppSpace.md),
      decoration: BoxDecoration(
        color: isSelected ? jc.brandSubtle : jc.surface,
        borderRadius: AppRadius.mdAll,
        border: Border.all(color: borderColor, width: isSelected ? 1.5 : 1),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Wrap(
            spacing: AppSpace.xs,
            runSpacing: AppSpace.xs,
            children: [
              if (isBest)
                const StatusChip(
                  label: 'Best price',
                  tone: Tone.success,
                  icon: Icons.star_rounded,
                ),
              if (isSelected)
                const StatusChip(
                  label: 'Selected',
                  tone: Tone.brand,
                  icon: Icons.check_rounded,
                ),
              if (!offer.available)
                const StatusChip(label: 'Unavailable', tone: Tone.danger),
              if (!isBest && !isSelected && offer.available)
                StatusChip(label: '#${offer.rank > 0 ? offer.rank : '-'}'),
            ],
          ),
          const SizedBox(height: AppSpace.sm),
          Text(
            offer.merchantName,
            style: context.tt.labelLarge?.copyWith(color: jc.textSecondary),
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
          ),
          const SizedBox(height: AppSpace.xxs),
          Text(
            [
              offer.title,
              if (offer.variantName.isNotEmpty) offer.variantName,
            ].join(' · '),
            style: context.tt.bodyMedium,
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
          ),
          const SizedBox(height: AppSpace.md),
          _kv(context, 'Unit', Fmt.money(offer.unitPriceCents)),
          _kv(
            context,
            'Shipping',
            offer.shippingCents == 0 ? 'Free' : Fmt.money(offer.shippingCents),
          ),
          if (offer.eta.isNotEmpty) _kv(context, 'Delivery', offer.eta),
          Divider(height: AppSpace.lg, color: jc.border),
          Row(
            crossAxisAlignment: CrossAxisAlignment.end,
            children: [
              Expanded(child: Text('Landed', style: context.tt.labelMedium)),
              Column(
                crossAxisAlignment: CrossAxisAlignment.end,
                children: [
                  MoneyText(
                    offer.landedCostCents,
                    style: context.tt.titleMedium?.copyWith(
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                  if (deltaCents != null && deltaCents! > 0)
                    Text(
                      '+${Fmt.money(deltaCents!)}',
                      style: context.tt.labelSmall?.copyWith(color: jc.danger),
                    ),
                ],
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _kv(BuildContext context, String k, String v) => Padding(
    padding: const EdgeInsets.only(bottom: AppSpace.xxs),
    child: Row(
      children: [
        Expanded(child: Text(k, style: context.tt.bodySmall)),
        Text(
          v,
          style: context.tt.bodySmall?.copyWith(
            color: context.jc.textPrimary,
            fontFeatures: const [FontFeature.tabularFigures()],
          ),
        ),
      ],
    ),
  );
}
