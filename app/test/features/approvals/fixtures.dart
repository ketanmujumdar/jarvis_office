// Shared fakes and fixtures for the approvals and orders feature tests.
import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_riverpod/misc.dart' show Override;
import 'package:go_router/go_router.dart';
import 'package:jarvis_office/core/api/api_client.dart';
import 'package:jarvis_office/core/api/models.dart';
import 'package:jarvis_office/core/providers.dart';
import 'package:jarvis_office/core/theme/app_theme.dart';

final now = DateTime.utc(2026, 10, 9, 12);

const maya = User(
  id: 'u-maya',
  name: 'Maya Tan',
  email: 'maya@example.com',
  role: Role.manager,
);
const daniel = User(
  id: 'u-daniel',
  name: 'Daniel Lim',
  email: 'daniel@example.com',
  role: Role.approver,
);

PurchaseRequest request({
  String id = 'req-1',
  RequestStatus status = RequestStatus.pendingApproval,
  Decision decision = Decision.needsApproval,
  int totalCents = 64900,
  String utterance = 'Two ergonomic chairs for the new hires',
  DateTime? createdAt,
}) => PurchaseRequest(
  id: id,
  requesterId: maya.id,
  rawUtterance: utterance,
  status: status,
  subtotalCents: totalCents - 1000,
  shippingCents: 1000,
  totalCents: totalCents,
  currency: 'SGD',
  decision: decision,
  createdAt: createdAt ?? now.subtract(const Duration(hours: 2)),
  updatedAt: createdAt ?? now.subtract(const Duration(hours: 2)),
);

Offer offer(
  String id, {
  String lineItemId = 'li-1',
  required String merchant,
  required int landed,
  int rank = 1,
  bool available = true,
}) => Offer(
  id: id,
  lineItemId: lineItemId,
  merchantName: merchant,
  reapProductId: 'p-$id',
  reapVariantId: 'v-$id',
  title: 'Ergonomic Chair',
  unitPriceCents: landed - 500,
  currency: 'SGD',
  packSize: 1,
  shippingCents: 500,
  landedCostCents: landed,
  available: available,
  rank: rank,
  eta: '3-5 days',
);

LineItem lineItem({
  String id = 'li-1',
  String requestId = 'req-1',
  String? selectedOfferId = 'o-1',
  String? catalogItemId = 'cat-1',
  List<Reason> reasons = const [],
}) => LineItem(
  id: id,
  requestId: requestId,
  position: 1,
  catalogItemId: catalogItemId,
  description: 'Ergonomic office chair',
  qty: 2,
  urgency: 'normal',
  policyDecision: Decision.needsApproval,
  reasons: reasons,
  selectedOfferId: selectedOfferId,
);

ApprovalView approvalView({
  String id = 'ap-1',
  String requestId = 'req-1',
  ApprovalStatus status = ApprovalStatus.pending,
  String kind = 'policy',
  int amountCents = 64900,
  int? prevCents,
  String comment = '',
  DateTime? createdAt,
  List<Reason>? reasons,
  String utterance = 'Two ergonomic chairs for the new hires',
}) => ApprovalView(
  approval: Approval(
    id: id,
    requestId: requestId,
    kind: kind,
    status: status,
    reasons:
        reasons ??
        const [
          Reason(
            code: 'OVER_ORDER_LIMIT',
            message: r'Total S$649.00 exceeds the S$500.00 per-order limit',
          ),
          Reason(
            code: 'NOT_AUTO_APPROVE',
            message: 'Office chairs always need sign-off',
          ),
        ],
    amountCents: amountCents,
    prevCents: prevCents,
    comment: comment,
    approverId: status == ApprovalStatus.pending ? null : daniel.id,
    decidedAt: status == ApprovalStatus.pending
        ? null
        : now.subtract(const Duration(minutes: 30)),
    createdAt: createdAt ?? now.subtract(const Duration(hours: 2)),
  ),
  request: request(
    id: requestId,
    totalCents: amountCents,
    utterance: utterance,
  ),
  requester: maya,
  lines: [
    ApprovalLine(
      lineItem: lineItem(requestId: requestId),
      topOffers: [
        offer('o-1', merchant: 'ErgoTune', landed: 32450, rank: 1),
        offer('o-2', merchant: 'Picket & Rail', landed: 35900, rank: 2),
        offer('o-3', merchant: 'Metro', landed: 41000, rank: 3),
        offer('o-4', merchant: 'Shoppy', landed: 50000, rank: 4),
      ],
    ),
  ],
);

/// In-memory fake of the Jarvis API for feature tests.
class FakeApi extends JarvisApi {
  FakeApi() : super(baseUrl: 'http://fake.test');

  List<ApprovalView> approvals = [];
  List<OrderSummary> orders = [];
  SpendSummary spend = const SpendSummary(
    currency: 'SGD',
    monthlyBudgetCents: 300000,
    monthToDateCents: 0,
    months: [],
  );
  Map<String, RequestDetail> details = {};
  Map<String, List<AuditEvent>> audits = {};
  Object? approvalsError;

  final decisions = <(String id, String action, String comment)>[];
  int listApprovalsCalls = 0;
  int listOrdersCalls = 0;

  @override
  Future<List<ApprovalView>> listApprovals({
    ApprovalStatus? status,
    int? limit,
    int? offset,
  }) async {
    listApprovalsCalls++;
    final err = approvalsError;
    if (err != null) throw err;
    return approvals
        .where((a) => status == null || a.approval.status == status)
        .toList();
  }

  @override
  Future<ApprovalView> getApproval(String id) async =>
      approvals.firstWhere((a) => a.approval.id == id);

  Future<Approval> _decide(String id, String action, String comment) async {
    decisions.add((id, action, comment));
    final i = approvals.indexWhere((a) => a.approval.id == id);
    final old = approvals[i];
    final a = old.approval;
    final decided = Approval(
      id: a.id,
      requestId: a.requestId,
      kind: a.kind,
      status: action == 'approve'
          ? ApprovalStatus.approved
          : ApprovalStatus.rejected,
      reasons: a.reasons,
      amountCents: a.amountCents,
      approverId: daniel.id,
      comment: comment,
      decidedAt: now,
      createdAt: a.createdAt,
    );
    approvals[i] = ApprovalView(
      approval: decided,
      request: old.request,
      requester: old.requester,
      lines: old.lines,
    );
    return decided;
  }

  @override
  Future<Approval> approve(String id, {String comment = ''}) =>
      _decide(id, 'approve', comment);

  @override
  Future<Approval> reject(String id, {String comment = ''}) =>
      _decide(id, 'reject', comment);

  @override
  Future<List<OrderSummary>> listOrders({int? limit, int? offset}) async {
    listOrdersCalls++;
    return orders;
  }

  @override
  Future<SpendSummary> monthlySpend({int months = 6}) async => spend;

  @override
  Future<RequestDetail> getRequest(String id) async {
    final d = details[id];
    if (d == null) {
      throw const ApiException(
        statusCode: 404,
        code: 'not_found',
        message: 'request not found',
      );
    }
    return d;
  }

  @override
  Future<List<AuditEvent>> requestAudit(String id) async =>
      audits[id] ?? const [];
}

/// A controllable live event stream plus the provider overrides for a test.
class Harness {
  Harness(this.api);
  final FakeApi api;
  final events = StreamController<SseEvent>.broadcast();

  List<Override> get overrides => [
    apiProvider.overrideWithValue(api),
    eventStreamProvider.overrideWith((ref, id) => events.stream),
  ];

  void emit(String type, {String? requestId, Json data = const {}}) =>
      events.add(
        SseEvent(id: 1, type: type, requestId: requestId, at: now, data: data),
      );

  /// Hosts [child] directly in a themed MaterialApp.
  Widget wrap(Widget child, {bool dark = false}) {
    AppTheme.useGoogleFonts = false;
    return ProviderScope(
      overrides: overrides,
      child: MaterialApp(
        theme: dark ? AppTheme.dark() : AppTheme.light(),
        home: Scaffold(body: child),
      ),
    );
  }

  /// Hosts a GoRouter so pages can navigate.
  Widget router(List<RouteBase> routes, {required String initial}) {
    AppTheme.useGoogleFonts = false;
    final r = GoRouter(initialLocation: initial, routes: routes);
    return ProviderScope(
      overrides: overrides,
      child: MaterialApp.router(theme: AppTheme.light(), routerConfig: r),
    );
  }
}
