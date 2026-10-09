// Typed models for docs/openapi.yaml. Hand-written; keep field names in sync
// with the backend JSON (snake_case). Money fields are integer SGD cents.

typedef Json = Map<String, dynamic>;

DateTime? _dt(Object? v) => v == null ? null : DateTime.tryParse(v as String);
DateTime _dtReq(Object? v) =>
    _dt(v) ?? DateTime.fromMillisecondsSinceEpoch(0, isUtc: true);
int _int(Object? v) => v == null ? 0 : (v as num).toInt();
int? _intOrNull(Object? v) => v == null ? null : (v as num).toInt();
String _str(Object? v) => v == null ? '' : v as String;
List<T> _list<T>(Object? v, T Function(Json) f) =>
    v == null ? <T>[] : (v as List).map((e) => f(e as Json)).toList();
List<String> _strList(Object? v) =>
    v == null ? <String>[] : (v as List).cast<String>();

T _enum<T extends Enum>(List<T> values, Object? v, T fallback) {
  for (final e in values) {
    if (_wire(e) == v) return e;
  }
  return fallback;
}

/// Enum wire value: camelCase name -> snake_case (pendingApproval -> pending_approval).
String _wire(Enum e) =>
    e.name.replaceAllMapped(RegExp('[A-Z]'), (m) => '_${m[0]!.toLowerCase()}');

// ---------------------------------------------------------------- enums

enum Role {
  manager,
  approver,
  admin,
  unknown;

  static Role fromJson(Object? v) => _enum(values, v, unknown);
  String toJson() => _wire(this);
  bool get canApprove => this == approver || this == admin;
  bool get canAdmin => this == admin;

  /// Who may create, confirm or cancel purchase requests (same rule as the API).
  bool get canBuy => this == manager || this == admin;

  /// Display title, e.g. "Office manager".
  String get title => switch (this) {
    manager => 'Office manager',
    approver => 'Approver',
    admin => 'Admin',
    unknown => 'Member',
  };

  /// Short title for tight spaces (segmented buttons, chips).
  String get shortTitle => switch (this) {
    manager => 'Manager',
    approver => 'Approver',
    admin => 'Admin',
    unknown => 'Member',
  };
}

enum RequestStatus {
  parsing,
  searching,
  quoted,
  pendingApproval,
  approved,
  checkingOut,
  awaitingPayment,
  paying,
  ordered,
  rejected,
  failed,
  cancelled,
  unknown;

  static RequestStatus fromJson(Object? v) => _enum(values, v, unknown);
  String toJson() => _wire(this);

  bool get isTerminal =>
      this == ordered ||
      this == rejected ||
      this == failed ||
      this == cancelled;

  /// True while the backend is working without user input.
  bool get isBusy =>
      this == parsing ||
      this == searching ||
      this == checkingOut ||
      this == paying;

  String get label => switch (this) {
    parsing => 'Understanding',
    searching => 'Finding prices',
    quoted => 'Ready to confirm',
    pendingApproval => 'Needs approval',
    approved => 'Approved',
    checkingOut => 'Getting live quote',
    awaitingPayment => 'Approve payment',
    paying => 'Paying',
    ordered => 'Ordered',
    rejected => 'Rejected',
    failed => 'Failed',
    cancelled => 'Cancelled',
    unknown => 'Unknown',
  };
}

enum Decision {
  autoApprove,
  needsApproval,
  reject,
  none;

  static Decision fromJson(Object? v) => switch (v) {
    'AUTO_APPROVE' => autoApprove,
    'NEEDS_APPROVAL' => needsApproval,
    'REJECT' => reject,
    _ => none,
  };

  String get label => switch (this) {
    autoApprove => 'Pre-approved',
    needsApproval => 'Needs approval',
    reject => 'Blocked',
    none => 'Pending',
  };
}

enum ApprovalStatus {
  pending,
  approved,
  rejected,
  unknown;

  static ApprovalStatus fromJson(Object? v) => _enum(values, v, unknown);
  String toJson() => _wire(this);
}

enum PaymentStatus {
  quoting,
  quoted,
  requiresAction,
  processing,
  completed,
  failed,
  expired,
  unknown;

  static PaymentStatus fromJson(Object? v) => _enum(values, v, unknown);

  String get label => switch (this) {
    quoting => 'Quoting',
    quoted => 'Quoted',
    requiresAction => 'Awaiting approval',
    processing => 'Processing',
    completed => 'Paid',
    failed => 'Failed',
    expired => 'Expired',
    unknown => 'Unknown',
  };
}

enum EnrollmentStatus {
  requiresAction,
  active,
  failed,
  expired,
  revoked,
  unknown;

  static EnrollmentStatus fromJson(Object? v) => switch (v) {
    'REQUIRES_ACTION' => requiresAction,
    'ACTIVE' => active,
    'FAILED' => failed,
    'EXPIRED' => expired,
    'REVOKED' => revoked,
    _ => unknown,
  };
}

// ---------------------------------------------------------------- auth

class User {
  const User({
    required this.id,
    required this.name,
    required this.email,
    required this.role,
  });
  final String id;
  final String name;
  final String email;
  final Role role;

  factory User.fromJson(Json j) => User(
    id: _str(j['id']),
    name: _str(j['name']),
    email: _str(j['email']),
    role: Role.fromJson(j['role']),
  );

  Json toJson() => {
    'id': id,
    'name': name,
    'email': email,
    'role': role.toJson(),
  };

  String get initials {
    final parts = name
        .trim()
        .split(RegExp(r'\s+'))
        .where((p) => p.isNotEmpty)
        .toList();
    if (parts.isEmpty) return '?';
    return (parts.first[0] + (parts.length > 1 ? parts.last[0] : ''))
        .toUpperCase();
  }
}

class LoginResponse {
  const LoginResponse({required this.token, required this.user});
  final String token;
  final User user;
  factory LoginResponse.fromJson(Json j) => LoginResponse(
    token: _str(j['token']),
    user: User.fromJson(j['user'] as Json),
  );
}

// ---------------------------------------------------------------- requests

class Reason {
  const Reason({required this.code, required this.message});
  final String code;
  final String message;
  factory Reason.fromJson(Json j) =>
      Reason(code: _str(j['code']), message: _str(j['message']));
}

class PurchaseRequest {
  const PurchaseRequest({
    required this.id,
    required this.requesterId,
    required this.rawUtterance,
    required this.status,
    required this.subtotalCents,
    required this.shippingCents,
    required this.totalCents,
    required this.currency,
    required this.decision,
    required this.createdAt,
    required this.updatedAt,
    this.addressId,
    this.failureReason = '',
    this.confirmedBy,
    this.confirmedAt,
  });

  final String id;
  final String requesterId;
  final String rawUtterance;
  final RequestStatus status;
  final String? addressId;
  final int subtotalCents;
  final int shippingCents;
  final int totalCents;
  final String currency;
  final Decision decision;
  final String failureReason;
  final String? confirmedBy;
  final DateTime? confirmedAt;
  final DateTime createdAt;
  final DateTime updatedAt;

  factory PurchaseRequest.fromJson(Json j) => PurchaseRequest(
    id: _str(j['id']),
    requesterId: _str(j['requester_id']),
    rawUtterance: _str(j['raw_utterance']),
    status: RequestStatus.fromJson(j['status']),
    addressId: j['address_id'] as String?,
    subtotalCents: _int(j['subtotal_cents']),
    shippingCents: _int(j['shipping_cents']),
    totalCents: _int(j['total_cents']),
    currency: _str(j['currency']),
    decision: Decision.fromJson(j['decision']),
    failureReason: _str(j['failure_reason']),
    confirmedBy: j['confirmed_by'] as String?,
    confirmedAt: _dt(j['confirmed_at']),
    createdAt: _dtReq(j['created_at']),
    updatedAt: _dtReq(j['updated_at']),
  );
}

class LineItem {
  const LineItem({
    required this.id,
    required this.requestId,
    required this.position,
    required this.description,
    required this.qty,
    required this.urgency,
    required this.policyDecision,
    required this.reasons,
    this.catalogItemId,
    this.selectedOfferId,
  });

  final String id;
  final String requestId;
  final int position;
  final String? catalogItemId;
  final String description;
  final int qty;
  final String urgency;
  final Decision policyDecision;
  final List<Reason> reasons;
  final String? selectedOfferId;

  bool get isOffList => catalogItemId == null;

  factory LineItem.fromJson(Json j) => LineItem(
    id: _str(j['id']),
    requestId: _str(j['request_id']),
    position: _int(j['position']),
    catalogItemId: j['catalog_item_id'] as String?,
    description: _str(j['description']),
    qty: _int(j['qty']),
    urgency: j['urgency'] as String? ?? 'normal',
    policyDecision: Decision.fromJson(j['policy_decision']),
    reasons: _list(j['reasons'], Reason.fromJson),
    selectedOfferId: j['selected_offer_id'] as String?,
  );
}

class Offer {
  const Offer({
    required this.id,
    required this.lineItemId,
    required this.merchantName,
    required this.reapProductId,
    required this.reapVariantId,
    required this.title,
    required this.unitPriceCents,
    required this.currency,
    required this.packSize,
    required this.shippingCents,
    required this.landedCostCents,
    required this.available,
    required this.rank,
    this.vendorId,
    this.variantName = '',
    this.imageUrl = '',
    this.url = '',
    this.eta = '',
  });

  final String id;
  final String lineItemId;
  final String? vendorId;
  final String merchantName;
  final String reapProductId;
  final String reapVariantId;
  final String title;
  final String variantName;
  final String imageUrl;
  final String url;
  final int unitPriceCents;
  final String currency;
  final int packSize;
  final int shippingCents;
  final int landedCostCents;
  final String eta;
  final bool available;
  final int rank;

  factory Offer.fromJson(Json j) => Offer(
    id: _str(j['id']),
    lineItemId: _str(j['line_item_id']),
    vendorId: j['vendor_id'] as String?,
    merchantName: _str(j['merchant_name']),
    reapProductId: _str(j['reap_product_id']),
    reapVariantId: _str(j['reap_variant_id']),
    title: _str(j['title']),
    variantName: _str(j['variant_name']),
    imageUrl: _str(j['image_url']),
    url: _str(j['url']),
    unitPriceCents: _int(j['unit_price_cents']),
    currency: _str(j['currency']),
    packSize: _int(j['pack_size']) == 0 ? 1 : _int(j['pack_size']),
    shippingCents: _int(j['shipping_cents']),
    landedCostCents: _int(j['landed_cost_cents']),
    eta: _str(j['eta']),
    available: j['available'] as bool? ?? true,
    rank: _int(j['rank']),
  );
}

class Approval {
  const Approval({
    required this.id,
    required this.requestId,
    required this.kind,
    required this.status,
    required this.reasons,
    required this.amountCents,
    required this.createdAt,
    this.prevCents,
    this.approverId,
    this.comment = '',
    this.decidedAt,
  });

  final String id;
  final String requestId;
  final String kind; // policy | price_drift
  final ApprovalStatus status;
  final List<Reason> reasons;
  final int amountCents;
  final int? prevCents;
  final String? approverId;
  final String comment;
  final DateTime? decidedAt;
  final DateTime createdAt;

  bool get isPriceDrift => kind == 'price_drift';

  factory Approval.fromJson(Json j) => Approval(
    id: _str(j['id']),
    requestId: _str(j['request_id']),
    kind: _str(j['kind']),
    status: ApprovalStatus.fromJson(j['status']),
    reasons: _list(j['reasons'], Reason.fromJson),
    amountCents: _int(j['amount_cents']),
    prevCents: _intOrNull(j['prev_cents']),
    approverId: j['approver_id'] as String?,
    comment: _str(j['comment']),
    decidedAt: _dt(j['decided_at']),
    createdAt: _dtReq(j['created_at']),
  );
}

class ApprovalLine {
  const ApprovalLine({
    required this.lineItem,
    required this.topOffers,
    this.catalogItem,
  });
  final LineItem lineItem;
  final CatalogItem? catalogItem;
  final List<Offer> topOffers;
  factory ApprovalLine.fromJson(Json j) => ApprovalLine(
    lineItem: LineItem.fromJson(j['line_item'] as Json),
    catalogItem: j['catalog_item'] == null
        ? null
        : CatalogItem.fromJson(j['catalog_item'] as Json),
    topOffers: _list(j['top_offers'], Offer.fromJson),
  );
}

class ApprovalView {
  const ApprovalView({
    required this.approval,
    required this.request,
    required this.requester,
    required this.lines,
  });
  final Approval approval;
  final PurchaseRequest request;
  final User requester;
  final List<ApprovalLine> lines;
  factory ApprovalView.fromJson(Json j) => ApprovalView(
    approval: Approval.fromJson(j['approval'] as Json),
    request: PurchaseRequest.fromJson(j['request'] as Json),
    requester: User.fromJson(j['requester'] as Json),
    lines: _list(j['lines'], ApprovalLine.fromJson),
  );
}

class Payment {
  const Payment({
    required this.id,
    required this.requestId,
    required this.merchantName,
    required this.itemsCents,
    required this.shippingCents,
    required this.taxCents,
    required this.quotedCents,
    required this.currency,
    required this.status,
    this.vendorId,
    this.reapQuoteId = '',
    this.reapCheckoutId = '',
    this.reapOrderId = '',
    this.finalCents,
    this.approvalUrl = '',
    this.quoteExpiresAt,
    this.error = '',
  });

  final String id;
  final String requestId;
  final String? vendorId;
  final String merchantName;
  final String reapQuoteId;
  final String reapCheckoutId;
  final String reapOrderId;
  final int itemsCents;
  final int shippingCents;
  final int taxCents;
  final int quotedCents;
  final int? finalCents;
  final String currency;
  final PaymentStatus status;
  final String approvalUrl;
  final DateTime? quoteExpiresAt;
  final String error;

  factory Payment.fromJson(Json j) => Payment(
    id: _str(j['id']),
    requestId: _str(j['request_id']),
    vendorId: j['vendor_id'] as String?,
    merchantName: _str(j['merchant_name']),
    reapQuoteId: _str(j['reap_quote_id']),
    reapCheckoutId: _str(j['reap_checkout_id']),
    reapOrderId: _str(j['reap_order_id']),
    itemsCents: _int(j['items_cents']),
    shippingCents: _int(j['shipping_cents']),
    taxCents: _int(j['tax_cents']),
    quotedCents: _int(j['quoted_cents']),
    finalCents: _intOrNull(j['final_cents']),
    currency: _str(j['currency']),
    status: PaymentStatus.fromJson(j['status']),
    approvalUrl: _str(j['approval_url']),
    quoteExpiresAt: _dt(j['quote_expires_at']),
    error: _str(j['error']),
  );
}

class CheckoutStatus {
  const CheckoutStatus({
    required this.requestId,
    required this.status,
    required this.payments,
  });
  final String requestId;
  final RequestStatus status;
  final List<Payment> payments;
  factory CheckoutStatus.fromJson(Json j) => CheckoutStatus(
    requestId: _str(j['request_id']),
    status: RequestStatus.fromJson(j['status']),
    payments: _list(j['payments'], Payment.fromJson),
  );
}

class RequestDetail {
  const RequestDetail({
    required this.request,
    required this.lineItems,
    required this.offers,
    required this.approvals,
    required this.payments,
    this.address,
  });

  final PurchaseRequest request;
  final List<LineItem> lineItems;
  final List<Offer> offers;
  final List<Approval> approvals;
  final List<Payment> payments;
  final Address? address;

  /// Offers of one line item ordered by rank (best first).
  List<Offer> offersFor(String lineItemId) =>
      offers.where((o) => o.lineItemId == lineItemId).toList()
        ..sort((a, b) => a.rank.compareTo(b.rank));

  Offer? selectedOffer(LineItem li) {
    final id = li.selectedOfferId;
    if (id == null) return null;
    for (final o in offers) {
      if (o.id == id) return o;
    }
    return null;
  }

  factory RequestDetail.fromJson(Json j) => RequestDetail(
    request: PurchaseRequest.fromJson(j['request'] as Json),
    lineItems: _list(j['line_items'], LineItem.fromJson),
    offers: _list(j['offers'], Offer.fromJson),
    approvals: _list(j['approvals'], Approval.fromJson),
    payments: _list(j['payments'], Payment.fromJson),
    address: j['address'] == null
        ? null
        : Address.fromJson(j['address'] as Json),
  );
}

class CreateRequestItem {
  const CreateRequestItem({required this.description, this.qty, this.urgency});
  final String description;
  final int? qty;
  final String? urgency;
  Json toJson() => {
    'description': description,
    if (qty != null) 'qty': qty,
    if (urgency != null) 'urgency': urgency,
  };
}

// ---------------------------------------------------------------- orders, spend, audit

class OrderSummary {
  const OrderSummary({
    required this.request,
    required this.payments,
    required this.vendors,
    required this.itemCount,
    this.requester,
    this.approver,
  });

  final PurchaseRequest request;
  final User? requester;
  final User? approver;
  final List<String> vendors;
  final int itemCount;
  final List<Payment> payments;

  factory OrderSummary.fromJson(Json j) => OrderSummary(
    request: PurchaseRequest.fromJson(j['request'] as Json),
    requester: j['requester'] == null
        ? null
        : User.fromJson(j['requester'] as Json),
    approver: j['approver'] == null
        ? null
        : User.fromJson(j['approver'] as Json),
    vendors: _strList(j['vendors']),
    itemCount: _int(j['item_count']),
    payments: _list(j['payments'], Payment.fromJson),
  );
}

class MonthSpend {
  const MonthSpend({
    required this.month,
    required this.spendCents,
    required this.orders,
  });
  final String month; // "2026-10"
  final int spendCents;
  final int orders;
  factory MonthSpend.fromJson(Json j) => MonthSpend(
    month: _str(j['month']),
    spendCents: _int(j['spend_cents']),
    orders: _int(j['orders']),
  );
}

class SpendSummary {
  const SpendSummary({
    required this.currency,
    required this.monthlyBudgetCents,
    required this.monthToDateCents,
    required this.months,
  });
  final String currency;
  final int monthlyBudgetCents;
  final int monthToDateCents;
  final List<MonthSpend> months;
  factory SpendSummary.fromJson(Json j) => SpendSummary(
    currency: _str(j['currency']),
    monthlyBudgetCents: _int(j['monthly_budget_cents']),
    monthToDateCents: _int(j['month_to_date_cents']),
    months: _list(j['months'], MonthSpend.fromJson),
  );
}

class AuditEvent {
  const AuditEvent({
    required this.id,
    required this.actorType,
    required this.type,
    required this.at,
    required this.payload,
    this.requestId,
    this.actorId = '',
  });
  final int id;
  final String? requestId;
  final String actorType; // user | agent | system
  final String actorId;
  final String type;
  final Json payload;
  final DateTime at;
  factory AuditEvent.fromJson(Json j) => AuditEvent(
    id: _int(j['id']),
    requestId: j['request_id'] as String?,
    actorType: _str(j['actor_type']),
    actorId: _str(j['actor_id']),
    type: _str(j['type']),
    payload: (j['payload'] as Json?) ?? const {},
    at: _dtReq(j['at']),
  );
}

/// One Server-Sent Event (see SseEvent in openapi.yaml for `data` shapes).
class SseEvent {
  const SseEvent({
    required this.id,
    required this.type,
    required this.at,
    required this.data,
    this.requestId,
  });
  final int id;
  final String type;
  final String? requestId;
  final DateTime at;
  final Json data;
  factory SseEvent.fromJson(Json j) => SseEvent(
    id: _int(j['id']),
    type: _str(j['type']),
    requestId: j['request_id'] as String?,
    at: _dtReq(j['at']),
    data: (j['data'] as Json?) ?? const {},
  );
}

/// SSE event type names.
abstract final class SseTypes {
  static const requestCreated = 'request.created';
  static const requestStatusChanged = 'request.status_changed';
  static const lineItemsParsed = 'line_items.parsed';
  static const searchStarted = 'search.started';
  static const searchVendorResult = 'search.vendor_result';
  static const offersRanked = 'offers.ranked';
  static const policyEvaluated = 'policy.evaluated';
  static const approvalRequested = 'approval.requested';
  static const approvalDecided = 'approval.decided';
  static const checkoutQuoted = 'checkout.quoted';
  static const checkoutPriceDrift = 'checkout.price_drift';
  static const paymentActionRequired = 'payment.action_required';
  static const paymentStatusChanged = 'payment.status_changed';
  static const orderCompleted = 'order.completed';
  static const paymentAlert = 'payment.alert';
  static const enrollmentUpdated = 'enrollment.updated';
  static const agentMessage = 'agent.message';
  static const heartbeat = 'heartbeat';
}

// ---------------------------------------------------------------- agent + realtime

class ToolCallTrace {
  const ToolCallTrace({
    required this.name,
    this.arguments,
    this.result,
    this.error = '',
  });
  final String name;
  final Object? arguments;
  final Object? result;
  final String error;
  factory ToolCallTrace.fromJson(Json j) => ToolCallTrace(
    name: _str(j['name']),
    arguments: j['arguments'],
    result: j['result'],
    error: _str(j['error']),
  );
}

class ChatOutput {
  const ChatOutput({
    required this.sessionId,
    required this.reply,
    required this.requestIds,
    required this.toolCalls,
  });
  final String sessionId;
  final String reply;
  final List<String> requestIds;
  final List<ToolCallTrace> toolCalls;
  factory ChatOutput.fromJson(Json j) => ChatOutput(
    sessionId: _str(j['session_id']),
    reply: _str(j['reply']),
    requestIds: _strList(j['request_ids']),
    toolCalls: _list(j['tool_calls'], ToolCallTrace.fromJson),
  );
}

class ChatMessage {
  const ChatMessage({
    required this.id,
    required this.sessionId,
    required this.role,
    required this.content,
    required this.createdAt,
  });
  final int id;
  final String sessionId;
  final String role; // system | user | assistant | tool
  final String content;
  final DateTime createdAt;
  factory ChatMessage.fromJson(Json j) => ChatMessage(
    id: _int(j['id']),
    sessionId: _str(j['session_id']),
    role: _str(j['role']),
    content: _str(j['content']),
    createdAt: _dtReq(j['created_at']),
  );
}

class ToolDefinition {
  const ToolDefinition({
    required this.name,
    required this.description,
    required this.parameters,
  });
  final String name;
  final String description;
  final Json parameters;
  factory ToolDefinition.fromJson(Json j) => ToolDefinition(
    name: _str(j['name']),
    description: _str(j['description']),
    parameters: (j['parameters'] as Json?) ?? const {},
  );
  Json toJson() => {
    'type': 'function',
    'name': name,
    'description': description,
    'parameters': parameters,
  };
}

class RealtimeSession {
  const RealtimeSession({
    required this.clientSecret,
    required this.expiresAt,
    required this.model,
    required this.callsUrl,
    this.voice = '',
    this.tools = const [],
  });
  final String clientSecret;
  final DateTime expiresAt;
  final String model;
  final String voice;
  final String callsUrl;
  final List<ToolDefinition> tools;
  factory RealtimeSession.fromJson(Json j) => RealtimeSession(
    clientSecret: _str(j['client_secret']),
    expiresAt: _dtReq(j['expires_at']),
    model: _str(j['model']),
    voice: _str(j['voice']),
    callsUrl: _str(j['calls_url']),
    tools: _list(j['tools'], ToolDefinition.fromJson),
  );
}

// ---------------------------------------------------------------- enrollment

class Enrollment {
  const Enrollment({
    required this.id,
    required this.reapEnrollmentId,
    required this.status,
    required this.createdAt,
    this.nextActionUrl = '',
    this.ownerEmail = '',
  });
  final String id;
  final String reapEnrollmentId;
  final EnrollmentStatus status;
  final String nextActionUrl;
  final String ownerEmail;
  final DateTime createdAt;
  bool get isActive => status == EnrollmentStatus.active;
  factory Enrollment.fromJson(Json j) => Enrollment(
    id: _str(j['id']),
    reapEnrollmentId: _str(j['reap_enrollment_id']),
    status: EnrollmentStatus.fromJson(j['status']),
    nextActionUrl: _str(j['next_action_url']),
    ownerEmail: _str(j['owner_email']),
    createdAt: _dtReq(j['created_at']),
  );
}

// ---------------------------------------------------------------- admin entities

class CatalogItem {
  const CatalogItem({
    required this.id,
    required this.sku,
    required this.name,
    required this.aliases,
    required this.category,
    required this.unit,
    required this.defaultQty,
    required this.maxUnitPriceCents,
    required this.preferredVendorIds,
    required this.autoApprove,
    required this.searchQuery,
    required this.active,
  });

  final String id;
  final String sku;
  final String name;
  final List<String> aliases;
  final String category;
  final String unit;
  final int defaultQty;
  final int maxUnitPriceCents;
  final List<String> preferredVendorIds;
  final bool autoApprove;
  final String searchQuery;
  final bool active;

  factory CatalogItem.fromJson(Json j) => CatalogItem(
    id: _str(j['id']),
    sku: _str(j['sku']),
    name: _str(j['name']),
    aliases: _strList(j['aliases']),
    category: _str(j['category']),
    unit: _str(j['unit']),
    defaultQty: _int(j['default_qty']),
    maxUnitPriceCents: _int(j['max_unit_price_cents']),
    preferredVendorIds: _strList(j['preferred_vendor_ids']),
    autoApprove: j['auto_approve'] as bool? ?? false,
    searchQuery: _str(j['search_query']),
    active: j['active'] as bool? ?? true,
  );

  /// Body for POST/PUT /admin/catalog (CatalogItemInput).
  Json toInput() => {
    'sku': sku,
    'name': name,
    'aliases': aliases,
    'category': category,
    'unit': unit,
    'default_qty': defaultQty,
    'max_unit_price_cents': maxUnitPriceCents,
    'preferred_vendor_ids': preferredVendorIds,
    'auto_approve': autoApprove,
    'search_query': searchQuery,
    'active': active,
  };
}

class Vendor {
  const Vendor({
    required this.id,
    required this.domain,
    required this.name,
    required this.reapMerchantName,
    required this.category,
    required this.country,
    required this.allowed,
    required this.officeRelevant,
    required this.priority,
    this.notes = '',
  });

  final String id;
  final String domain;
  final String name;
  final String reapMerchantName;
  final String category;
  final String country;
  final bool allowed;
  final bool officeRelevant;
  final int priority;
  final String notes;

  factory Vendor.fromJson(Json j) => Vendor(
    id: _str(j['id']),
    domain: _str(j['domain']),
    name: _str(j['name']),
    reapMerchantName: _str(j['reap_merchant_name']),
    category: _str(j['category']),
    country: _str(j['country']),
    allowed: j['allowed'] as bool? ?? false,
    officeRelevant: j['office_relevant'] as bool? ?? false,
    priority: _int(j['priority']),
    notes: _str(j['notes']),
  );

  Json toInput() => {
    'domain': domain,
    'name': name,
    'reap_merchant_name': reapMerchantName,
    'category': category,
    'country': country,
    'allowed': allowed,
    'office_relevant': officeRelevant,
    'priority': priority,
    'notes': notes,
  };
}

class PolicyConfig {
  const PolicyConfig({
    required this.currency,
    required this.perOrderLimitCents,
    required this.monthlyBudgetCents,
    required this.priceDriftPct,
    this.updatedAt,
  });
  final String currency;
  final int perOrderLimitCents;
  final int monthlyBudgetCents;
  final double priceDriftPct;
  final DateTime? updatedAt;
  factory PolicyConfig.fromJson(Json j) => PolicyConfig(
    currency: _str(j['currency']),
    perOrderLimitCents: _int(j['per_order_limit_cents']),
    monthlyBudgetCents: _int(j['monthly_budget_cents']),
    priceDriftPct: (j['price_drift_pct'] as num?)?.toDouble() ?? 0,
    updatedAt: _dt(j['updated_at']),
  );
  Json toInput() => {
    'per_order_limit_cents': perOrderLimitCents,
    'monthly_budget_cents': monthlyBudgetCents,
    'price_drift_pct': priceDriftPct,
  };
}

class Address {
  const Address({
    required this.id,
    required this.label,
    required this.firstName,
    required this.lastName,
    required this.phone,
    required this.email,
    required this.addressLine1,
    required this.city,
    required this.postalCode,
    required this.country,
    required this.isDefault,
    this.addressLine2 = '',
    this.region = '',
  });

  final String id;
  final String label;
  final String firstName;
  final String lastName;
  final String phone;
  final String email;
  final String addressLine1;
  final String addressLine2;
  final String city;
  final String region;
  final String postalCode;
  final String country;
  final bool isDefault;

  String get contactName => '$firstName $lastName'.trim();

  /// One-line summary, e.g. "7 Straits View, #20-01 Marina One East Tower, Singapore 018936".
  String get oneLine => [
    addressLine1,
    if (addressLine2.isNotEmpty) addressLine2,
    '$city $postalCode'.trim(),
  ].join(', ');

  factory Address.fromJson(Json j) => Address(
    id: _str(j['id']),
    label: _str(j['label']),
    firstName: _str(j['first_name']),
    lastName: _str(j['last_name']),
    phone: _str(j['phone']),
    email: _str(j['email']),
    addressLine1: _str(j['address_line1']),
    addressLine2: _str(j['address_line2']),
    city: _str(j['city']),
    region: _str(j['region']),
    postalCode: _str(j['postal_code']),
    country: _str(j['country']),
    isDefault: j['is_default'] as bool? ?? false,
  );

  Json toInput() => {
    'label': label,
    'first_name': firstName,
    'last_name': lastName,
    'phone': phone,
    'email': email,
    'address_line1': addressLine1,
    'address_line2': addressLine2,
    'city': city,
    'region': region,
    'postal_code': postalCode,
    'country': country,
    'is_default': isDefault,
  };
}

class SystemPrompt {
  const SystemPrompt({
    required this.key,
    required this.content,
    required this.version,
    required this.updatedAt,
    this.updatedBy = '',
  });
  final String key;
  final String content;
  final int version;
  final DateTime updatedAt;
  final String updatedBy;
  factory SystemPrompt.fromJson(Json j) => SystemPrompt(
    key: _str(j['key']),
    content: _str(j['content']),
    version: _int(j['version']),
    updatedAt: _dtReq(j['updated_at']),
    updatedBy: _str(j['updated_by']),
  );
}
