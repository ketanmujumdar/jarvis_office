import 'package:flutter/material.dart';

import '../../../core/api/models.dart';
import '../../../core/format.dart';
import '../../../core/theme/tokens.dart';
import '../../../core/widgets/widgets.dart';
import '../reason_copy.dart';

/// Avatar with the user's initials.
class UserAvatar extends StatelessWidget {
  const UserAvatar({super.key, required this.user, this.size = 36});
  final User user;
  final double size;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return Container(
      width: size,
      height: size,
      alignment: Alignment.center,
      decoration: BoxDecoration(color: jc.brandSubtle, shape: BoxShape.circle),
      child: Text(
        user.initials,
        style: context.tt.labelLarge?.copyWith(
          color: jc.brand,
          fontSize: size * 0.36,
        ),
      ),
    );
  }
}

/// Label for an approval kind.
String approvalKindLabel(Approval a) =>
    a.isPriceDrift ? 'Price change' : 'Policy exception';

/// One row in the approval queue.
class ApprovalQueueTile extends StatelessWidget {
  const ApprovalQueueTile({
    super.key,
    required this.view,
    required this.selected,
    required this.onTap,
    this.now,
  });

  final ApprovalView view;
  final bool selected;
  final VoidCallback onTap;
  final DateTime? now;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final a = view.approval;
    final reasons = a.reasons;
    final first = reasons.isEmpty ? null : reasons.first;
    final copy = first == null ? null : ReasonCopy.of(first.code);
    final pending = a.status == ApprovalStatus.pending;

    return Material(
      color: selected ? jc.brandSubtle : jc.surface,
      shape: RoundedRectangleBorder(
        borderRadius: AppRadius.mdAll,
        side: BorderSide(color: selected ? jc.brand : jc.border),
      ),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        key: Key('queue-tile-${a.id}'),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(AppSpace.md + 2),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Row(
                children: [
                  UserAvatar(user: view.requester, size: 32),
                  const SizedBox(width: AppSpace.sm + 2),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          view.requester.name,
                          style: context.tt.titleSmall,
                          overflow: TextOverflow.ellipsis,
                        ),
                        Text(
                          '${approvalKindLabel(a)} · '
                          '${Fmt.relative(a.createdAt, now: now)}',
                          style: context.tt.bodySmall,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(width: AppSpace.sm),
                  MoneyText(
                    a.amountCents,
                    style: context.tt.titleMedium?.copyWith(
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ],
              ),
              if (view.request.rawUtterance.isNotEmpty) ...[
                const SizedBox(height: AppSpace.sm),
                Text(
                  '"${view.request.rawUtterance}"',
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: context.tt.bodyMedium?.copyWith(
                    color: jc.textSecondary,
                  ),
                ),
              ],
              const SizedBox(height: AppSpace.sm + 2),
              Wrap(
                spacing: AppSpace.xs + 2,
                runSpacing: AppSpace.xs + 2,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [
                  if (!pending) StatusChip.approval(a.status),
                  if (copy != null)
                    StatusChip(
                      label: copy.title,
                      tone: copy.tone,
                      icon: copy.icon,
                    ),
                  if (reasons.length > 1)
                    Text(
                      '+${reasons.length - 1} more',
                      style: context.tt.labelSmall,
                    ),
                  Text(
                    '${view.lines.length} '
                    '${view.lines.length == 1 ? 'item' : 'items'}',
                    style: context.tt.labelSmall,
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}
