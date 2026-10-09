import 'package:flutter/material.dart';

import '../theme/tokens.dart';

/// Bordered surface card with an optional header (title, subtitle, trailing).
class AppCard extends StatelessWidget {
  const AppCard({
    super.key,
    required this.child,
    this.title,
    this.subtitle,
    this.leading,
    this.trailing,
    this.padding = const EdgeInsets.all(AppSpace.lg),
    this.onTap,
    this.highlight,
  });

  final Widget child;
  final String? title;
  final String? subtitle;
  final Widget? leading;
  final Widget? trailing;
  final EdgeInsetsGeometry padding;
  final VoidCallback? onTap;

  /// Optional accent tone drawn as a left border (e.g. warning for items needing attention).
  final Tone? highlight;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final hasHeader = title != null || trailing != null || leading != null;
    final content = Padding(
      padding: padding,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        mainAxisSize: MainAxisSize.min,
        children: [
          if (hasHeader) ...[
            Row(
              children: [
                if (leading != null) ...[
                  leading!,
                  const SizedBox(width: AppSpace.md),
                ],
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      if (title != null)
                        Text(title!, style: context.tt.titleMedium),
                      if (subtitle != null) ...[
                        const SizedBox(height: AppSpace.xxs),
                        Text(subtitle!, style: context.tt.bodySmall),
                      ],
                    ],
                  ),
                ),
                ?trailing,
              ],
            ),
            const SizedBox(height: AppSpace.lg),
          ],
          child,
        ],
      ),
    );
    return Material(
      color: jc.surface,
      shape: RoundedRectangleBorder(
        borderRadius: AppRadius.lgAll,
        side: BorderSide(color: jc.border),
      ),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: onTap,
        child: highlight == null
            ? content
            : DecoratedBox(
                decoration: BoxDecoration(
                  border: Border(
                    left: BorderSide(color: jc.fg(highlight!), width: 3),
                  ),
                ),
                child: content,
              ),
      ),
    );
  }
}

/// KPI tile: label, big value, optional caption and tone-coloured delta.
class StatTile extends StatelessWidget {
  const StatTile({
    super.key,
    required this.label,
    required this.value,
    this.caption,
    this.icon,
    this.tone = Tone.brand,
    this.compact = false,
  });

  final String label;
  final String value;
  final String? caption;
  final IconData? icon;
  final Tone tone;

  /// Phone layout (two tiles per row): no icon, smaller value text.
  final bool compact;

  /// This tile in its [compact] form.
  StatTile asCompact() => StatTile(
    key: key,
    label: label,
    value: value,
    caption: caption,
    icon: icon,
    tone: tone,
    compact: true,
  );

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return AppCard(
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (icon != null && !compact) ...[
            Container(
              padding: const EdgeInsets.all(AppSpace.sm),
              decoration: BoxDecoration(
                color: jc.bg(tone),
                borderRadius: AppRadius.mdAll,
              ),
              child: Icon(icon, size: 20, color: jc.fg(tone)),
            ),
            const SizedBox(width: AppSpace.md),
          ],
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  label.toUpperCase(),
                  style: context.tt.labelSmall,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
                const SizedBox(height: AppSpace.xs),
                FittedBox(
                  fit: BoxFit.scaleDown,
                  alignment: Alignment.centerLeft,
                  child: Text(
                    value,
                    maxLines: 1,
                    style:
                        (compact
                                ? context.tt.titleLarge
                                : context.tt.headlineSmall)
                            ?.copyWith(
                              fontFeatures: const [
                                FontFeature.tabularFigures(),
                              ],
                            ),
                  ),
                ),
                if (caption != null) ...[
                  const SizedBox(height: AppSpace.xxs),
                  Text(
                    caption!,
                    style: context.tt.bodySmall,
                    maxLines: compact ? 2 : null,
                    overflow: compact ? TextOverflow.ellipsis : null,
                  ),
                ],
              ],
            ),
          ),
        ],
      ),
    );
  }
}
