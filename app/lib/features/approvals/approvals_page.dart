import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/api/models.dart';
import '../../core/format.dart';
import '../../core/providers.dart';
import '../../core/router.dart';
import '../../core/theme/tokens.dart';
import '../../core/widgets/widgets.dart';
import 'approvals_providers.dart';
import 'widgets/approval_detail.dart';
import 'widgets/approval_queue_tile.dart';
import 'widgets/live_badge.dart';

/// Width at which the queue and the detail sit side by side.
const double _masterDetailMin = 980;

/// Refetches the queue when a relevant live event arrives.
void listenForApprovalEvents(WidgetRef ref, {String? approvalId}) {
  ref.listen(eventStreamProvider(null), (_, next) {
    if (next case AsyncData(:final value)
        when approvalEventTypes.contains(value.type)) {
      ref.invalidate(approvalsProvider);
      if (approvalId != null) ref.invalidate(approvalProvider(approvalId));
    }
  });
}

/// Approver console: a FIFO queue of exceptions with reasons, the top three
/// quotes per line, and approve / reject with a comment. Updates live.
class ApprovalsPage extends ConsumerWidget {
  const ApprovalsPage({super.key, this.now});

  /// Clock override for tests.
  final DateTime? now;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    listenForApprovalEvents(ref);
    final async = ref.watch(approvalsProvider);
    return PageScaffold(
      header: PageHeader(
        title: 'Approvals',
        subtitle: 'Review exceptions before anything is bought.',
        actions: [
          const LiveBadge(),
          IconButton.outlined(
            key: const Key('approvals-refresh'),
            tooltip: 'Refresh',
            onPressed: () => ref.invalidate(approvalsProvider),
            icon: const Icon(Icons.refresh_rounded, size: 20),
          ),
        ],
      ),
      children: [
        AsyncValueView<List<ApprovalView>>(
          value: async,
          onRetry: () => ref.invalidate(approvalsProvider),
          data: (all) => _Console(all: all, now: now),
        ),
      ],
    );
  }
}

class _Console extends ConsumerWidget {
  const _Console({required this.all, this.now});
  final List<ApprovalView> all;
  final DateTime? now;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final tab = ref.watch(queueTabProvider);
    final pending = pendingOf(all);
    final decided = decidedOf(all);
    final list = tab == QueueTab.pending ? pending : decided;
    final selectedId = ref.watch(selectedApprovalProvider);
    final selected =
        list.where((v) => v.approval.id == selectedId).firstOrNull ??
        list.firstOrNull;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _QueueStats(pending: pending, now: now),
        const SizedBox(height: AppSpace.xl),
        LayoutBuilder(
          builder: (context, c) {
            final wide = c.maxWidth >= _masterDetailMin;
            final queue = _Queue(
              tab: tab,
              pendingCount: pending.length,
              decidedCount: decided.length,
              list: list,
              selectedId: wide ? selected?.approval.id : null,
              now: now,
              onSelect: (v) {
                if (wide) {
                  ref
                      .read(selectedApprovalProvider.notifier)
                      .select(v.approval.id);
                } else {
                  context.push(Routes.approval(v.approval.id));
                }
              },
            );
            if (!wide) return queue;
            return Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                SizedBox(width: 380, child: queue),
                const SizedBox(width: AppSpace.xl),
                Expanded(
                  child: selected == null
                      ? AppCard(
                          child: EmptyState(
                            icon: Icons.inbox_outlined,
                            title: 'Nothing selected',
                            message: tab == QueueTab.pending
                                ? 'New requests that need your sign-off '
                                      'will appear here.'
                                : 'Decisions you make will appear here.',
                            compact: true,
                          ),
                        )
                      : ApprovalDetail(
                          key: ValueKey(selected.approval.id),
                          view: selected,
                          onDecided: (_) => ref
                              .read(selectedApprovalProvider.notifier)
                              .select(null),
                        ),
                ),
              ],
            );
          },
        ),
      ],
    );
  }
}

class _QueueStats extends StatelessWidget {
  const _QueueStats({required this.pending, this.now});
  final List<ApprovalView> pending;
  final DateTime? now;

  @override
  Widget build(BuildContext context) {
    final value = pending.fold<int>(0, (s, v) => s + v.approval.amountCents);
    final oldest = pending.isEmpty ? null : pending.first.approval.createdAt;
    final drift = pending.where((v) => v.approval.isPriceDrift).length;
    final tiles = [
      StatTile(
        key: const Key('stat-pending'),
        label: 'Waiting for you',
        value: '${pending.length}',
        caption: drift == 0
            ? 'Policy exceptions'
            : '$drift price ${drift == 1 ? 'change' : 'changes'}',
        icon: Icons.pending_actions_rounded,
        tone: pending.isEmpty ? Tone.success : Tone.warning,
      ),
      StatTile(
        label: 'Value on hold',
        value: Fmt.money(value),
        caption: 'Across pending requests',
        icon: Icons.account_balance_wallet_outlined,
      ),
      StatTile(
        label: 'Oldest request',
        value: oldest == null ? 'None' : Fmt.relative(oldest, now: now),
        caption: oldest == null ? 'Queue is clear' : Fmt.dateTime(oldest),
        icon: Icons.schedule_rounded,
        tone: Tone.info,
      ),
    ];
    return LayoutBuilder(
      builder: (context, c) {
        if (c.maxWidth < AppBreakpoints.compact) {
          return Column(
            children: [
              for (final t in tiles) ...[
                t,
                if (t != tiles.last) const SizedBox(height: AppSpace.md),
              ],
            ],
          );
        }
        return Row(
          children: [
            for (final t in tiles) ...[
              Expanded(child: t),
              if (t != tiles.last) const SizedBox(width: AppSpace.lg),
            ],
          ],
        );
      },
    );
  }
}

class _Queue extends ConsumerWidget {
  const _Queue({
    required this.tab,
    required this.pendingCount,
    required this.decidedCount,
    required this.list,
    required this.selectedId,
    required this.onSelect,
    this.now,
  });

  final QueueTab tab;
  final int pendingCount;
  final int decidedCount;
  final List<ApprovalView> list;
  final String? selectedId;
  final ValueChanged<ApprovalView> onSelect;
  final DateTime? now;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SegmentedButton<QueueTab>(
          showSelectedIcon: false,
          segments: [
            ButtonSegment(
              value: QueueTab.pending,
              label: Text('Pending ($pendingCount)'),
            ),
            ButtonSegment(
              value: QueueTab.decided,
              label: Text('Decided ($decidedCount)'),
            ),
          ],
          selected: {tab},
          onSelectionChanged: (s) {
            ref.read(queueTabProvider.notifier).set(s.first);
            ref.read(selectedApprovalProvider.notifier).select(null);
          },
        ),
        const SizedBox(height: AppSpace.md),
        if (list.isEmpty)
          AppCard(
            child: EmptyState(
              icon: tab == QueueTab.pending
                  ? Icons.task_alt_rounded
                  : Icons.history_rounded,
              tone: tab == QueueTab.pending ? Tone.success : Tone.brand,
              title: tab == QueueTab.pending
                  ? 'All caught up'
                  : 'No decisions yet',
              message: tab == QueueTab.pending
                  ? 'Nothing needs your approval right now.'
                  : 'Approved and rejected requests show up here.',
              compact: true,
            ),
          )
        else
          for (final v in list) ...[
            ApprovalQueueTile(
              view: v,
              selected: v.approval.id == selectedId,
              now: now,
              onTap: () => onSelect(v),
            ),
            const SizedBox(height: AppSpace.sm),
          ],
      ],
    );
  }
}

/// Narrow-screen detail route: `/approvals/:id`.
class ApprovalDetailPage extends ConsumerWidget {
  const ApprovalDetailPage({super.key, required this.approvalId});
  final String approvalId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    listenForApprovalEvents(ref, approvalId: approvalId);
    final async = ref.watch(approvalProvider(approvalId));
    return PageScaffold(
      maxWidth: 900,
      header: PageHeader(
        title: 'Review request',
        subtitle: 'Check the reasons and quotes, then decide.',
        leading: IconButton(
          tooltip: 'Back to queue',
          onPressed: () =>
              context.canPop() ? context.pop() : context.go(Routes.approvals),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
        actions: const [LiveBadge()],
      ),
      children: [
        AsyncValueView<ApprovalView>(
          value: async,
          onRetry: () => ref.invalidate(approvalProvider(approvalId)),
          data: (v) => ApprovalDetail(view: v),
        ),
      ],
    );
  }
}
