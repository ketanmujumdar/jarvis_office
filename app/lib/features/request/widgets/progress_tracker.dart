import 'package:flutter/material.dart';

import '../../../core/api/models.dart';
import '../../../core/format.dart';
import '../../../core/theme/tokens.dart';
import '../../../core/widgets/widgets.dart';
import '../request_logic.dart';

/// Horizontal stepper: Understand → Find prices → Review → Approve → Pay → Ordered.
class ProgressTracker extends StatelessWidget {
  const ProgressTracker({super.key, required this.status});

  final RequestStatus status;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final current = stageFor(status);
    final failed = current == null;
    return LayoutBuilder(
      builder: (context, c) {
        final showLabels = c.maxWidth >= 420;
        final children = <Widget>[];
        for (final stage in ProgressStage.values) {
          final idx = stage.index;
          final done =
              current != null &&
              (idx < current.index || current == ProgressStage.done);
          final active = current == stage && current != ProgressStage.done;
          final tone = failed
              ? Tone.neutral
              : done
              ? Tone.success
              : active
              ? Tone.brand
              : Tone.neutral;
          if (idx > 0) {
            children.add(
              Expanded(
                child: Container(
                  height: 2,
                  margin: const EdgeInsets.symmetric(horizontal: AppSpace.xs),
                  decoration: BoxDecoration(
                    color: done || active ? jc.fg(Tone.success) : jc.border,
                    borderRadius: AppRadius.pillAll,
                  ),
                ),
              ),
            );
          }
          children.add(
            _StepDot(
              label: stage.label,
              index: idx + 1,
              done: done,
              active: active,
              tone: tone,
              showLabel: showLabels,
            ),
          );
        }
        return Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: children,
        );
      },
    );
  }
}

class _StepDot extends StatelessWidget {
  const _StepDot({
    required this.label,
    required this.index,
    required this.done,
    required this.active,
    required this.tone,
    required this.showLabel,
  });

  final String label;
  final int index;
  final bool done;
  final bool active;
  final Tone tone;
  final bool showLabel;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final fg = jc.fg(tone);
    final dot = AnimatedContainer(
      duration: const Duration(milliseconds: 250),
      width: 28,
      height: 28,
      decoration: BoxDecoration(
        color: done ? fg : (active ? jc.bg(tone) : jc.surfaceMuted),
        shape: BoxShape.circle,
        border: Border.all(
          color: done || active ? fg : jc.borderStrong,
          width: active ? 2 : 1,
        ),
      ),
      alignment: Alignment.center,
      child: done
          ? Icon(Icons.check_rounded, size: 16, color: jc.surface)
          : Text(
              '$index',
              style: context.tt.labelMedium?.copyWith(
                color: active ? fg : jc.textMuted,
              ),
            ),
    );
    return Semantics(
      label:
          '$label${done
              ? ', done'
              : active
              ? ', in progress'
              : ''}',
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          dot,
          if (showLabel) ...[
            const SizedBox(height: AppSpace.xs),
            ExcludeSemantics(
              child: Text(
                label,
                style: context.tt.labelSmall?.copyWith(
                  color: active ? jc.textPrimary : jc.textMuted,
                  fontWeight: active ? FontWeight.w600 : null,
                ),
              ),
            ),
          ],
        ],
      ),
    );
  }
}

/// Vertical feed of live agent progress (newest first).
class ActivityFeed extends StatelessWidget {
  const ActivityFeed({super.key, required this.entries, this.maxItems = 12});

  final List<ActivityEntry> entries;
  final int maxItems;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    if (entries.isEmpty) {
      return Padding(
        padding: const EdgeInsets.symmetric(vertical: AppSpace.sm),
        child: Row(
          children: [
            Icon(Icons.bolt_rounded, size: 18, color: jc.textMuted),
            const SizedBox(width: AppSpace.sm),
            Expanded(
              child: Text(
                'Live updates from the agents will appear here.',
                style: context.tt.bodySmall,
              ),
            ),
          ],
        ),
      );
    }
    final shown = entries.take(maxItems).toList();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        for (var i = 0; i < shown.length; i++)
          _ActivityRow(entry: shown[i], last: i == shown.length - 1),
      ],
    );
  }
}

class _ActivityRow extends StatelessWidget {
  const _ActivityRow({required this.entry, required this.last});
  final ActivityEntry entry;
  final bool last;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return IntrinsicHeight(
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Column(
            children: [
              Container(
                width: 28,
                height: 28,
                decoration: BoxDecoration(
                  color: jc.bg(entry.tone),
                  shape: BoxShape.circle,
                ),
                child: Icon(entry.icon, size: 15, color: jc.fg(entry.tone)),
              ),
              if (!last)
                Expanded(
                  child: Container(
                    width: 1.5,
                    margin: const EdgeInsets.symmetric(vertical: AppSpace.xxs),
                    color: jc.border,
                  ),
                ),
            ],
          ),
          const SizedBox(width: AppSpace.md),
          Expanded(
            child: Padding(
              padding: EdgeInsets.only(
                top: AppSpace.xs,
                bottom: last ? 0 : AppSpace.md,
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Expanded(
                        child: Text(
                          entry.title,
                          style: context.tt.bodyMedium?.copyWith(
                            fontWeight: FontWeight.w600,
                            color: jc.textPrimary,
                          ),
                        ),
                      ),
                      Text(
                        Fmt.relative(entry.at),
                        style: context.tt.labelSmall?.copyWith(
                          color: jc.textMuted,
                        ),
                      ),
                    ],
                  ),
                  if (entry.detail != null) ...[
                    const SizedBox(height: AppSpace.xxs),
                    Text(
                      entry.detail!,
                      style: context.tt.bodySmall,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ],
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// A tinted callout used for status messages (approval waiting, failure, ...).
class Callout extends StatelessWidget {
  const Callout({
    super.key,
    required this.tone,
    required this.icon,
    required this.title,
    this.message,
    this.action,
  });

  final Tone tone;
  final IconData icon;
  final String title;
  final String? message;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final fg = jc.fg(tone);
    return Container(
      padding: const EdgeInsets.all(AppSpace.lg),
      decoration: BoxDecoration(
        color: jc.bg(tone),
        borderRadius: AppRadius.mdAll,
        border: Border.all(color: fg.withValues(alpha: 0.25)),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, color: fg, size: 22),
          const SizedBox(width: AppSpace.md),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  title,
                  style: context.tt.titleSmall?.copyWith(color: jc.textPrimary),
                ),
                if (message != null && message!.isNotEmpty) ...[
                  const SizedBox(height: AppSpace.xxs),
                  Text(
                    message!,
                    style: context.tt.bodySmall?.copyWith(
                      color: jc.textSecondary,
                    ),
                  ),
                ],
                if (action != null) ...[
                  const SizedBox(height: AppSpace.md),
                  action!,
                ],
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// Small rounded label used inside tables (e.g. "Best price", "Off-list").
class Tag extends StatelessWidget {
  const Tag(this.label, {super.key, this.tone = Tone.neutral, this.icon});
  final String label;
  final Tone tone;
  final IconData? icon;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return Container(
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpace.sm,
        vertical: AppSpace.xxs,
      ),
      decoration: BoxDecoration(
        color: jc.bg(tone),
        borderRadius: AppRadius.smAll,
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (icon != null) ...[
            Icon(icon, size: 12, color: jc.fg(tone)),
            const SizedBox(width: AppSpace.xs),
          ],
          Text(
            label,
            style: context.tt.labelSmall?.copyWith(
              color: jc.fg(tone),
              fontWeight: FontWeight.w600,
            ),
          ),
        ],
      ),
    );
  }
}

/// Section card wrapper with consistent spacing for the request workspace.
class SectionCard extends StatelessWidget {
  const SectionCard({
    super.key,
    required this.title,
    required this.child,
    this.subtitle,
    this.icon,
    this.trailing,
    this.highlight,
  });

  final String title;
  final String? subtitle;
  final IconData? icon;
  final Widget? trailing;
  final Widget child;
  final Tone? highlight;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return AppCard(
      title: title,
      subtitle: subtitle,
      highlight: highlight,
      leading: icon == null
          ? null
          : Container(
              padding: const EdgeInsets.all(AppSpace.sm),
              decoration: BoxDecoration(
                color: jc.brandSubtle,
                borderRadius: AppRadius.mdAll,
              ),
              child: Icon(icon, size: 18, color: jc.brand),
            ),
      trailing: trailing,
      child: child,
    );
  }
}
