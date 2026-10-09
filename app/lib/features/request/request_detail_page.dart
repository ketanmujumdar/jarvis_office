import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/api/models.dart';
import '../../core/format.dart';
import '../../core/providers.dart';
import '../../core/router.dart';
import '../../core/widgets/widgets.dart';
import 'request_controller.dart';
import 'widgets/request_workspace.dart';

/// Full-page view of one purchase request: line items, quote comparison,
/// delivery address, approval state and the Reap checkout.
class RequestDetailPage extends ConsumerWidget {
  const RequestDetailPage({super.key, required this.requestId});

  final String requestId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final detail = ref.watch(requestDetailProvider(requestId));
    final d = detail.value;
    final r = d?.request;
    final role = ref.watch(currentUserProvider)?.role;
    // Reap cannot cancel a checkout: once a payment link exists the request
    // can no longer be cancelled (the API refuses it too).
    final cancellable =
        r != null &&
        (role?.canBuy ?? false) &&
        !r.status.isTerminal &&
        r.status != RequestStatus.awaitingPayment &&
        r.status != RequestStatus.paying &&
        !d!.payments.any((p) => p.reapCheckoutId.isNotEmpty);
    return PageScaffold(
      header: PageHeader(
        title: 'Request',
        subtitle: r == null
            ? 'Line items, quotes, approvals and payment.'
            : 'Created ${Fmt.dateTime(r.createdAt)} · #${_short(r.id)}',
        leading: IconButton(
          tooltip: 'Back to assistant',
          onPressed: () =>
              context.canPop() ? context.pop() : context.go(Routes.assistant),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
        actions: [
          OutlinedButton.icon(
            onPressed: () =>
                ref.read(requestDetailProvider(requestId).notifier).refresh(),
            icon: const Icon(Icons.refresh_rounded, size: 18),
            label: const Text('Refresh'),
          ),
          if (cancellable)
            TextButton.icon(
              onPressed: () => _confirmCancel(context, ref),
              icon: const Icon(Icons.close_rounded, size: 18),
              label: const Text('Cancel request'),
            ),
        ],
      ),
      children: [RequestWorkspace(requestId: requestId)],
    );
  }

  static String _short(String id) => id.length > 8 ? id.substring(0, 8) : id;

  Future<void> _confirmCancel(BuildContext context, WidgetRef ref) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Cancel this request?'),
        content: const Text('Nothing has been charged yet.'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Keep'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Cancel request'),
          ),
        ],
      ),
    );
    if (ok != true) return;
    try {
      await ref.read(requestDetailProvider(requestId).notifier).cancel();
    } catch (e) {
      if (context.mounted) {
        ScaffoldMessenger.maybeOf(context)
            ?.showSnackBar(SnackBar(content: Text('Could not cancel: $e')));
      }
    }
  }
}
