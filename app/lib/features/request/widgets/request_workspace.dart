import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api/models.dart';
import '../../../core/providers.dart';
import '../../../core/theme/tokens.dart';
import '../../../core/widgets/widgets.dart';
import '../request_controller.dart';
import '../request_logic.dart';
import 'checkout_panel.dart';
import 'confirm_panel.dart';
import 'progress_tracker.dart';
import 'quote_table.dart';

/// Everything about one live request: progress, parsed items, quotes,
/// address picker, approval and payment, and the agent activity feed.
///
/// Used by the request detail page (two columns on wide screens) and embedded
/// in the assistant playground (always one column).
class RequestWorkspace extends ConsumerWidget {
  const RequestWorkspace({
    super.key,
    required this.requestId,
    this.allowTwoColumns = true,
    this.header,
  });

  final String requestId;
  final bool allowTwoColumns;

  /// Optional widget placed above the progress card (e.g. "open full view").
  final Widget? header;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final detail = ref.watch(requestDetailProvider(requestId));
    final activity = ref.watch(requestActivityProvider(requestId));
    return AsyncValueView<RequestDetail>(
      value: detail,
      onRetry: () => ref.invalidate(requestDetailProvider(requestId)),
      data: (d) => _Body(
        detail: d,
        activity: activity,
        allowTwoColumns: allowTwoColumns,
        header: header,
      ),
    );
  }
}

class _Body extends ConsumerWidget {
  const _Body({
    required this.detail,
    required this.activity,
    required this.allowTwoColumns,
    this.header,
  });

  final RequestDetail detail;
  final List<ActivityEntry> activity;
  final bool allowTwoColumns;
  final Widget? header;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // Only managers and admins confirm (the API enforces the same rule).
    final role = ref.watch(currentUserProvider)?.role;
    final canBuy = role?.canBuy ?? false;
    final r = detail.request;
    final status = r.status;
    final quotes = [...detail.lineItems]
      ..sort((a, b) => a.position.compareTo(b.position));
    final searching =
        status == RequestStatus.searching || status == RequestStatus.parsing;
    final showQuotes =
        detail.offers.isNotEmpty || status == RequestStatus.searching;
    final afterConfirm = {
      RequestStatus.pendingApproval,
      RequestStatus.approved,
      RequestStatus.checkingOut,
      RequestStatus.awaitingPayment,
      RequestStatus.paying,
      RequestStatus.ordered,
      RequestStatus.rejected,
    }.contains(status);

    final progress = AppCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  r.rawUtterance.isEmpty
                      ? 'Purchase request'
                      : '“${r.rawUtterance}”',
                  style: context.tt.titleMedium,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              const SizedBox(width: AppSpace.sm),
              StatusChip.request(status),
            ],
          ),
          const SizedBox(height: AppSpace.lg),
          ProgressTracker(status: status),
          if (status == RequestStatus.failed ||
              status == RequestStatus.cancelled) ...[
            const SizedBox(height: AppSpace.lg),
            Callout(
              tone: status == RequestStatus.failed ? Tone.danger : Tone.neutral,
              icon: status == RequestStatus.failed
                  ? Icons.error_outline_rounded
                  : Icons.cancel_outlined,
              title: status == RequestStatus.failed
                  ? 'This request failed'
                  : 'This request was cancelled',
              message: r.failureReason,
            ),
          ],
        ],
      ),
    );

    final lineItems = LineItemsCard(detail: detail);
    final quoteCards = <Widget>[
      if (showQuotes)
        for (final li in quotes)
          QuoteCard(quote: quoteFor(detail, li), searching: searching),
      if (detail.offers.isNotEmpty) TotalsCard(detail: detail),
    ];
    final main = <Widget>[lineItems, ...quoteCards];
    final side = <Widget>[
      if (canConfirm(detail) && canBuy) ConfirmPanel(detail: detail),
      if (canConfirm(detail) && !canBuy)
        const Callout(
          tone: Tone.info,
          icon: Icons.hourglass_empty_rounded,
          title: 'Waiting for an office manager to confirm',
          message: 'Only an office manager or admin can confirm and pay for an order.',
        ),
      if (afterConfirm || detail.payments.isNotEmpty)
        CheckoutPanel(detail: detail),
      SectionCard(
        title: 'Live activity',
        icon: Icons.bolt_rounded,
        trailing: status.isBusy
            ? const SizedBox(
                width: 16,
                height: 16,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            : null,
        child: ActivityFeed(entries: activity),
      ),
    ];

    List<Widget> spaced(List<Widget> ws) => [
      for (var i = 0; i < ws.length; i++) ...[
        if (i > 0) const SizedBox(height: AppSpace.lg),
        ws[i],
      ],
    ];

    return LayoutBuilder(
      builder: (context, c) {
        final two = allowTwoColumns && c.maxWidth >= 1000;
        if (!two) {
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: spaced([
              ?header,
              progress,
              // What is being bought first, then the action panels (confirm,
              // payment), then the detailed quote comparison and activity.
              lineItems,
              ...side.take(side.length - 1),
              ...quoteCards,
              side.last,
            ]),
          );
        }
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: spaced([
            ?header,
            progress,
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  flex: 7,
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: spaced(main),
                  ),
                ),
                const SizedBox(width: AppSpace.lg),
                Expanded(
                  flex: 5,
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: spaced(side),
                  ),
                ),
              ],
            ),
          ]),
        );
      },
    );
  }
}
