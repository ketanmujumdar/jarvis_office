import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api/api_client.dart';
import '../../../core/api/models.dart';
import '../../../core/format.dart';
import '../../../core/providers.dart';
import '../../../core/theme/tokens.dart';
import '../../../core/widgets/widgets.dart';
import '../approvals_providers.dart';
import '../reason_copy.dart';
import 'approval_queue_tile.dart';
import 'quote_comparison.dart';

/// Full review of one approval: who asked, why it needs review, the top three
/// quotes per line, and the approve / reject panel.
class ApprovalDetail extends StatelessWidget {
  const ApprovalDetail({super.key, required this.view, this.onDecided});

  final ApprovalView view;

  /// Called after a successful decision (e.g. to select the next item).
  final void Function(Approval decided)? onDecided;

  @override
  Widget build(BuildContext context) {
    final a = view.approval;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _SummaryCard(view: view),
        const SizedBox(height: AppSpace.lg),
        if (a.reasons.isNotEmpty) ...[
          _ReasonsCard(reasons: a.reasons),
          const SizedBox(height: AppSpace.lg),
        ],
        AppCard(
          title: 'Quotes',
          subtitle: 'Top three offers per item from allowed vendors',
          child: view.lines.isEmpty
              ? Text('No line items.', style: context.tt.bodyMedium)
              : Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    for (final (i, line) in view.lines.indexed) ...[
                      if (i > 0)
                        Divider(height: AppSpace.xxl, color: context.jc.border),
                      _LineSection(line: line),
                    ],
                  ],
                ),
        ),
        const SizedBox(height: AppSpace.lg),
        DecisionPanel(
          key: ValueKey('decision-${a.id}'),
          approval: a,
          onDecided: onDecided,
        ),
      ],
    );
  }
}

class _SummaryCard extends StatelessWidget {
  const _SummaryCard({required this.view});
  final ApprovalView view;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final a = view.approval;
    final prev = a.prevCents;
    return AppCard(
      highlight: a.status == ApprovalStatus.pending ? Tone.warning : null,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              UserAvatar(user: view.requester, size: 44),
              const SizedBox(width: AppSpace.md),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(view.requester.name, style: context.tt.titleLarge),
                    const SizedBox(height: AppSpace.xxs),
                    Text(
                      '${approvalKindLabel(a)} · requested '
                      '${Fmt.dateTime(a.createdAt)}',
                      style: context.tt.bodySmall,
                    ),
                  ],
                ),
              ),
              const SizedBox(width: AppSpace.md),
              Column(
                crossAxisAlignment: CrossAxisAlignment.end,
                children: [
                  Text('TOTAL', style: context.tt.labelSmall),
                  MoneyText(
                    a.amountCents,
                    style: context.tt.headlineSmall?.copyWith(
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                  if (prev != null && prev > 0) ...[
                    const SizedBox(height: AppSpace.xxs),
                    Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        MoneyText(
                          prev,
                          strikethrough: true,
                          style: context.tt.bodySmall,
                        ),
                        const SizedBox(width: AppSpace.xs),
                        Text(
                          '${a.amountCents >= prev ? '+' : ''}'
                          '${((a.amountCents - prev) * 100 / prev).toStringAsFixed(1)}%',
                          style: context.tt.labelMedium?.copyWith(
                            color: a.amountCents > prev
                                ? jc.danger
                                : jc.success,
                          ),
                        ),
                      ],
                    ),
                  ],
                ],
              ),
            ],
          ),
          if (view.request.rawUtterance.isNotEmpty) ...[
            const SizedBox(height: AppSpace.lg),
            Container(
              padding: const EdgeInsets.all(AppSpace.md),
              decoration: BoxDecoration(
                color: jc.surfaceMuted,
                borderRadius: AppRadius.mdAll,
              ),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Icon(
                    Icons.format_quote_rounded,
                    size: 18,
                    color: jc.textMuted,
                  ),
                  const SizedBox(width: AppSpace.sm),
                  Expanded(
                    child: Text(
                      view.request.rawUtterance,
                      style: context.tt.bodyMedium,
                    ),
                  ),
                ],
              ),
            ),
          ],
          const SizedBox(height: AppSpace.md),
          Wrap(
            spacing: AppSpace.sm,
            runSpacing: AppSpace.sm,
            children: [
              StatusChip.approval(a.status),
              StatusChip.request(view.request.status),
              StatusChip(
                label:
                    '${view.lines.length} '
                    '${view.lines.length == 1 ? 'item' : 'items'}',
                icon: Icons.shopping_bag_outlined,
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _ReasonsCard extends StatelessWidget {
  const _ReasonsCard({required this.reasons});
  final List<Reason> reasons;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return AppCard(
      title: 'Why this needs review',
      leading: Icon(Icons.policy_outlined, color: jc.warning),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [for (final r in reasons) ReasonRow(reason: r)],
      ),
    );
  }
}

/// One policy reason: tone-coloured icon, short title and the full message.
class ReasonRow extends StatelessWidget {
  const ReasonRow({super.key, required this.reason, this.dense = false});
  final Reason reason;
  final bool dense;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final copy = ReasonCopy.of(reason.code);
    return Padding(
      padding: EdgeInsets.symmetric(
        vertical: dense ? AppSpace.xxs : AppSpace.xs,
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            padding: const EdgeInsets.all(AppSpace.xs + 2),
            decoration: BoxDecoration(
              color: jc.bg(copy.tone),
              borderRadius: AppRadius.smAll,
            ),
            child: Icon(
              copy.icon,
              size: dense ? 14 : 16,
              color: jc.fg(copy.tone),
            ),
          ),
          const SizedBox(width: AppSpace.md),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(copy.title, style: context.tt.labelLarge),
                if (reason.message.isNotEmpty)
                  Text(reason.message, style: context.tt.bodySmall),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _LineSection extends StatelessWidget {
  const _LineSection({required this.line});
  final ApprovalLine line;

  @override
  Widget build(BuildContext context) {
    final li = line.lineItem;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    line.catalogItem?.name ?? li.description,
                    style: context.tt.titleSmall,
                  ),
                  const SizedBox(height: AppSpace.xxs),
                  Text(
                    [
                      'Qty ${li.qty}',
                      if (line.catalogItem != null) line.catalogItem!.sku,
                      if (li.urgency != 'normal') 'Urgency: ${li.urgency}',
                    ].join(' · '),
                    style: context.tt.bodySmall,
                  ),
                ],
              ),
            ),
            const SizedBox(width: AppSpace.sm),
            Wrap(
              spacing: AppSpace.xs,
              children: [
                if (li.isOffList)
                  const StatusChip(label: 'Off-list', tone: Tone.info),
                StatusChip.decision(li.policyDecision),
              ],
            ),
          ],
        ),
        if (li.reasons.isNotEmpty) ...[
          const SizedBox(height: AppSpace.sm),
          for (final r in li.reasons) ReasonRow(reason: r, dense: true),
        ],
        const SizedBox(height: AppSpace.md),
        QuoteComparison(
          offers: line.topOffers,
          selectedOfferId: li.selectedOfferId,
        ),
      ],
    );
  }
}

/// Comment box with Approve / Reject. Rejecting requires a comment so the
/// requester learns why. Shows the recorded decision once decided.
class DecisionPanel extends ConsumerStatefulWidget {
  const DecisionPanel({super.key, required this.approval, this.onDecided});
  final Approval approval;
  final void Function(Approval decided)? onDecided;

  @override
  ConsumerState<DecisionPanel> createState() => _DecisionPanelState();
}

class _DecisionPanelState extends ConsumerState<DecisionPanel> {
  final _comment = TextEditingController();
  DecisionAction? _busy;
  String? _error;

  @override
  void dispose() {
    _comment.dispose();
    super.dispose();
  }

  Future<void> _decide(DecisionAction action) async {
    if (action == DecisionAction.reject && _comment.text.trim().isEmpty) {
      setState(() => _error = 'Add a comment so the requester knows why.');
      return;
    }
    setState(() {
      _busy = action;
      _error = null;
    });
    final messenger = ScaffoldMessenger.maybeOf(context);
    try {
      final decided = await submitDecision(
        ref.read(apiProvider),
        widget.approval.id,
        action,
        _comment.text,
      );
      if (!mounted) return;
      messenger?.showSnackBar(
        SnackBar(
          content: Text(
            action == DecisionAction.approve
                ? 'Approved. Checkout will continue automatically.'
                : 'Rejected. The requester has been notified.',
          ),
        ),
      );
      ref.invalidate(approvalsProvider);
      ref.invalidate(approvalProvider(widget.approval.id));
      widget.onDecided?.call(decided);
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() => _error = e.message);
    } catch (e) {
      if (!mounted) return;
      setState(() => _error = e.toString());
    } finally {
      if (mounted) setState(() => _busy = null);
    }
  }

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final a = widget.approval;
    if (a.status != ApprovalStatus.pending) {
      final approved = a.status == ApprovalStatus.approved;
      return AppCard(
        highlight: approved ? Tone.success : Tone.danger,
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(
              approved ? Icons.check_circle_rounded : Icons.cancel_rounded,
              color: approved ? jc.success : jc.danger,
            ),
            const SizedBox(width: AppSpace.md),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    approved ? 'Approved' : 'Rejected',
                    style: context.tt.titleMedium,
                  ),
                  if (a.decidedAt != null)
                    Text(
                      'Decided ${Fmt.dateTime(a.decidedAt!)}',
                      style: context.tt.bodySmall,
                    ),
                  if (a.comment.isNotEmpty) ...[
                    const SizedBox(height: AppSpace.sm),
                    Text('"${a.comment}"', style: context.tt.bodyMedium),
                  ],
                ],
              ),
            ),
          ],
        ),
      );
    }

    final busy = _busy != null;
    return AppCard(
      title: 'Your decision',
      subtitle:
          'Approving lets checkout continue; nothing is paid until the '
          'requester approves the payment with Reap.',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          TextField(
            key: const Key('decision-comment'),
            controller: _comment,
            enabled: !busy,
            minLines: 2,
            maxLines: 4,
            decoration: InputDecoration(
              hintText: 'Comment (required to reject)',
              errorText: _error,
            ),
            onChanged: (_) {
              if (_error != null) setState(() => _error = null);
            },
          ),
          const SizedBox(height: AppSpace.md),
          Wrap(
            alignment: WrapAlignment.end,
            spacing: AppSpace.sm,
            runSpacing: AppSpace.sm,
            children: [
              OutlinedButton.icon(
                key: const Key('reject-button'),
                style: OutlinedButton.styleFrom(
                  foregroundColor: jc.danger,
                  side: BorderSide(color: jc.danger.withValues(alpha: 0.4)),
                ),
                onPressed: busy ? null : () => _decide(DecisionAction.reject),
                icon: _busy == DecisionAction.reject
                    ? const _Spinner()
                    : const Icon(Icons.close_rounded, size: 18),
                label: const Text('Reject'),
              ),
              FilledButton.icon(
                key: const Key('approve-button'),
                onPressed: busy ? null : () => _decide(DecisionAction.approve),
                icon: _busy == DecisionAction.approve
                    ? const _Spinner()
                    : const Icon(Icons.check_rounded, size: 18),
                label: const Text('Approve'),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _Spinner extends StatelessWidget {
  const _Spinner();
  @override
  Widget build(BuildContext context) => const SizedBox(
    width: 16,
    height: 16,
    child: CircularProgressIndicator(strokeWidth: 2),
  );
}
