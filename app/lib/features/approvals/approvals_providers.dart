import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../../core/api/models.dart';
import '../../core/providers.dart';

/// SSE event types that change the approval queue.
const approvalEventTypes = {
  SseTypes.approvalRequested,
  SseTypes.approvalDecided,
  SseTypes.requestStatusChanged,
  SseTypes.checkoutPriceDrift,
};

/// Every approval the approver can see (pending and decided), newest first.
/// Retries are disabled: errors surface immediately with a "Try again" action.
final approvalsProvider = FutureProvider.autoDispose<List<ApprovalView>>((
  ref,
) async {
  final all = await ref.watch(apiProvider).listApprovals(limit: 200);
  return [...all]
    ..sort((a, b) => b.approval.createdAt.compareTo(a.approval.createdAt));
}, retry: (_, _) => null);

/// One approval with its lines and top offers (used by the narrow-screen
/// detail route `/approvals/:id`).
final approvalProvider = FutureProvider.autoDispose
    .family<ApprovalView, String>(
      (ref, id) => ref.watch(apiProvider).getApproval(id),
      retry: (_, _) => null,
    );

/// Pending approvals, oldest first (first in, first out).
List<ApprovalView> pendingOf(List<ApprovalView> all) =>
    all.where((v) => v.approval.status == ApprovalStatus.pending).toList()
      ..sort((a, b) => a.approval.createdAt.compareTo(b.approval.createdAt));

/// Decided approvals, most recently decided first.
List<ApprovalView> decidedOf(List<ApprovalView> all) =>
    all.where((v) => v.approval.status != ApprovalStatus.pending).toList()
      ..sort(
        (a, b) => (b.approval.decidedAt ?? b.approval.createdAt).compareTo(
          a.approval.decidedAt ?? a.approval.createdAt,
        ),
      );

/// Which queue tab is visible.
enum QueueTab { pending, decided }

class QueueTabController extends Notifier<QueueTab> {
  @override
  QueueTab build() => QueueTab.pending;
  void set(QueueTab t) => state = t;
}

final queueTabProvider = NotifierProvider<QueueTabController, QueueTab>(
  QueueTabController.new,
);

/// The approval selected in the master-detail layout (null = first in list).
class SelectedApprovalController extends Notifier<String?> {
  @override
  String? build() => null;
  void select(String? id) => state = id;
}

final selectedApprovalProvider =
    NotifierProvider<SelectedApprovalController, String?>(
      SelectedApprovalController.new,
    );

/// Outcome of approving or rejecting, for the decision panel.
enum DecisionAction { approve, reject }

/// Sends a decision. Rejections require a comment (validated by the UI too).
Future<Approval> submitDecision(
  JarvisApi api,
  String approvalId,
  DecisionAction action,
  String comment,
) {
  final c = comment.trim();
  return switch (action) {
    DecisionAction.approve => api.approve(approvalId, comment: c),
    DecisionAction.reject => api.reject(approvalId, comment: c),
  };
}
