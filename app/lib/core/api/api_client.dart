import 'package:dio/dio.dart';

import 'models.dart';

/// Error from the Jarvis API (`{"error": {"code", "message"}}`) or the network.
class ApiException implements Exception {
  const ApiException({
    required this.statusCode,
    required this.code,
    required this.message,
  });

  /// HTTP status, or 0 for network/timeout errors.
  final int statusCode;

  /// Backend error code, e.g. `not_found`, `no_active_enrollment`, or `network`.
  final String code;
  final String message;

  bool get isNotFound => statusCode == 404;
  bool get isUnauthorized => statusCode == 401;
  bool get needsEnrollment =>
      code == 'no_active_enrollment' ||
      message.contains('ENROLLMENT_NOT_FOUND') ||
      message.contains('ENROLLMENT_NOT_ACTIVE');

  @override
  String toString() => 'ApiException($statusCode $code: $message)';
}

/// Typed client for docs/openapi.yaml. One method per endpoint.
class JarvisApi {
  JarvisApi({required String baseUrl, String? Function()? token, Dio? dio})
    : _token = token ?? (() => null),
      dio =
          dio ??
          Dio(
            BaseOptions(
              baseUrl: baseUrl,
              connectTimeout: const Duration(seconds: 10),
              receiveTimeout: const Duration(seconds: 60),
              contentType: 'application/json',
            ),
          ) {
    this.dio.interceptors.add(
      InterceptorsWrapper(
        onRequest: (options, handler) {
          final t = _token();
          if (t != null && t.isNotEmpty) {
            options.headers['Authorization'] = 'Bearer $t';
          }
          handler.next(options);
        },
      ),
    );
  }

  final Dio dio;
  final String? Function() _token;

  String get baseUrl => dio.options.baseUrl;

  Future<T> _call<T>(
    Future<Response<dynamic>> Function() req,
    T Function(dynamic data) parse,
  ) async {
    try {
      final res = await req();
      return parse(res.data);
    } on DioException catch (e) {
      throw _toApiException(e);
    }
  }

  static ApiException _toApiException(DioException e) {
    final res = e.response;
    if (res == null) {
      return ApiException(
        statusCode: 0,
        code: 'network',
        message: e.message ?? 'Network error',
      );
    }
    final data = res.data;
    if (data is Map && data['error'] is Map) {
      final err = data['error'] as Map;
      return ApiException(
        statusCode: res.statusCode ?? 0,
        code: err['code'] as String? ?? 'error',
        message: err['message'] as String? ?? 'Request failed',
      );
    }
    return ApiException(
      statusCode: res.statusCode ?? 0,
      code: 'http_${res.statusCode}',
      message: res.statusMessage ?? 'Request failed',
    );
  }

  List<T> _listOf<T>(dynamic data, String key, T Function(Json) f) =>
      ((data as Json)[key] as List? ?? const [])
          .map((e) => f(e as Json))
          .toList();

  // ---------------- auth ----------------
  Future<LoginResponse> login(String email) => _call(
    () => dio.post('/api/v1/auth/login', data: {'email': email}),
    (d) => LoginResponse.fromJson(d as Json),
  );
  Future<User> me() =>
      _call(() => dio.get('/api/v1/auth/me'), (d) => User.fromJson(d as Json));
  Future<List<User>> demoUsers() => _call(
    () => dio.get('/api/v1/auth/users'),
    (d) => _listOf(d, 'users', User.fromJson),
  );

  // ---------------- requests ----------------
  Future<PurchaseRequest> createRequest(
    String utterance, {
    List<CreateRequestItem>? items,
  }) => _call(
    () => dio.post(
      '/api/v1/requests',
      data: {
        'utterance': utterance,
        if (items != null) 'items': items.map((i) => i.toJson()).toList(),
      },
    ),
    (d) => PurchaseRequest.fromJson(d as Json),
  );

  Future<List<PurchaseRequest>> listRequests({
    List<RequestStatus>? statuses,
    bool? mine,
    int? limit,
    int? offset,
  }) => _call(
    () => dio.get(
      '/api/v1/requests',
      queryParameters: {
        if (statuses != null && statuses.isNotEmpty)
          'status': statuses.map((s) => s.toJson()).join(','),
        'mine': ?mine,
        'limit': ?limit,
        'offset': ?offset,
      },
    ),
    (d) => _listOf(d, 'requests', PurchaseRequest.fromJson),
  );

  Future<RequestDetail> getRequest(String id) => _call(
    () => dio.get('/api/v1/requests/$id'),
    (d) => RequestDetail.fromJson(d as Json),
  );

  Future<RequestDetail> confirmRequest(
    String id, {
    required String addressId,
  }) => _call(
    () => dio.post(
      '/api/v1/requests/$id/confirm',
      data: {'address_id': addressId},
    ),
    (d) => RequestDetail.fromJson(d as Json),
  );

  Future<RequestDetail> cancelRequest(String id) => _call(
    () => dio.post('/api/v1/requests/$id/cancel'),
    (d) => RequestDetail.fromJson(d as Json),
  );

  Future<CheckoutStatus> checkoutStatus(String id) => _call(
    () => dio.get('/api/v1/requests/$id/checkout'),
    (d) => CheckoutStatus.fromJson(d as Json),
  );

  Future<CheckoutStatus> refreshCheckout(String id) => _call(
    () => dio.post('/api/v1/requests/$id/checkout/refresh'),
    (d) => CheckoutStatus.fromJson(d as Json),
  );

  Future<List<AuditEvent>> requestAudit(String id) => _call(
    () => dio.get('/api/v1/requests/$id/audit'),
    (d) => _listOf(d, 'events', AuditEvent.fromJson),
  );

  // ---------------- approvals ----------------
  Future<List<ApprovalView>> listApprovals({
    ApprovalStatus? status,
    int? limit,
    int? offset,
  }) => _call(
    () => dio.get(
      '/api/v1/approvals',
      queryParameters: {
        if (status != null) 'status': status.toJson(),
        'limit': ?limit,
        'offset': ?offset,
      },
    ),
    (d) => _listOf(d, 'approvals', ApprovalView.fromJson),
  );

  Future<ApprovalView> getApproval(String id) => _call(
    () => dio.get('/api/v1/approvals/$id'),
    (d) => ApprovalView.fromJson(d as Json),
  );

  Future<Approval> approve(String id, {String comment = ''}) => _call(
    () => dio.post('/api/v1/approvals/$id/approve', data: {'comment': comment}),
    (d) => Approval.fromJson(d as Json),
  );

  Future<Approval> reject(String id, {String comment = ''}) => _call(
    () => dio.post('/api/v1/approvals/$id/reject', data: {'comment': comment}),
    (d) => Approval.fromJson(d as Json),
  );

  // ---------------- orders, spend, audit ----------------
  Future<List<OrderSummary>> listOrders({int? limit, int? offset}) => _call(
    () => dio.get(
      '/api/v1/orders',
      queryParameters: {'limit': ?limit, 'offset': ?offset},
    ),
    (d) => _listOf(d, 'orders', OrderSummary.fromJson),
  );

  Future<SpendSummary> monthlySpend({int months = 6}) => _call(
    () => dio.get('/api/v1/spend/monthly', queryParameters: {'months': months}),
    (d) => SpendSummary.fromJson(d as Json),
  );

  Future<List<AuditEvent>> auditLog({String? type, int? limit, int? offset}) =>
      _call(
        () => dio.get(
          '/api/v1/audit',
          queryParameters: {'type': ?type, 'limit': ?limit, 'offset': ?offset},
        ),
        (d) => _listOf(d, 'events', AuditEvent.fromJson),
      );

  // ---------------- lookups ----------------
  Future<List<CatalogItem>> catalog({String? q, String? category}) => _call(
    () => dio.get(
      '/api/v1/catalog',
      queryParameters: {
        if (q != null && q.isNotEmpty) 'q': q,
        'category': ?category,
      },
    ),
    (d) => _listOf(d, 'items', CatalogItem.fromJson),
  );

  Future<List<Address>> addresses() => _call(
    () => dio.get('/api/v1/addresses'),
    (d) => _listOf(d, 'addresses', Address.fromJson),
  );

  // ---------------- agent + realtime ----------------
  Future<ChatOutput> chat(String message, {String? sessionId}) => _call(
    () => dio.post(
      '/api/v1/agent/chat',
      data: {'message': message, 'session_id': ?sessionId},
    ),
    (d) => ChatOutput.fromJson(d as Json),
  );

  Future<List<ChatMessage>> chatHistory(String sessionId) => _call(
    () => dio.get('/api/v1/agent/sessions/$sessionId/messages'),
    (d) => _listOf(d, 'messages', ChatMessage.fromJson),
  );

  Future<List<ToolDefinition>> toolDefinitions() => _call(
    () => dio.get('/api/v1/agent/tools'),
    (d) => _listOf(d, 'tools', ToolDefinition.fromJson),
  );

  /// Executes a tool call relayed from the realtime voice session. Returns `output`.
  Future<Json> executeTool(
    String name,
    Object? arguments, {
    String? callId,
    String? sessionId,
  }) => _call(
    () => dio.post(
      '/api/v1/agent/tools/$name',
      data: {
        'arguments': arguments ?? <String, dynamic>{},
        'call_id': ?callId,
        'session_id': ?sessionId,
      },
    ),
    (d) => ((d as Json)['output'] as Json?) ?? <String, dynamic>{},
  );

  Future<RealtimeSession> realtimeSession() => _call(
    () => dio.post('/api/v1/realtime/session'),
    (d) => RealtimeSession.fromJson(d as Json),
  );

  // ---------------- enrollment ----------------
  Future<Enrollment> startEnrollment() => _call(
    () => dio.post('/api/v1/enrollments'),
    (d) => Enrollment.fromJson(d as Json),
  );

  /// Latest enrollment, or null if none exists yet.
  Future<Enrollment?> currentEnrollment() async {
    try {
      return await _call(
        () => dio.get('/api/v1/enrollments/current'),
        (d) => Enrollment.fromJson(d as Json),
      );
    } on ApiException catch (e) {
      if (e.isNotFound) return null;
      rethrow;
    }
  }

  // ---------------- admin ----------------
  Future<List<CatalogItem>> adminCatalog() => _call(
    () => dio.get('/api/v1/admin/catalog'),
    (d) => _listOf(d, 'items', CatalogItem.fromJson),
  );
  Future<CatalogItem> createCatalogItem(Json input) => _call(
    () => dio.post('/api/v1/admin/catalog', data: input),
    (d) => CatalogItem.fromJson(d as Json),
  );
  Future<CatalogItem> updateCatalogItem(String id, Json input) => _call(
    () => dio.put('/api/v1/admin/catalog/$id', data: input),
    (d) => CatalogItem.fromJson(d as Json),
  );
  Future<void> deleteCatalogItem(String id) =>
      _call(() => dio.delete('/api/v1/admin/catalog/$id'), (_) {});

  Future<List<Vendor>> adminVendors() => _call(
    () => dio.get('/api/v1/admin/vendors'),
    (d) => _listOf(d, 'vendors', Vendor.fromJson),
  );
  Future<Vendor> createVendor(Json input) => _call(
    () => dio.post('/api/v1/admin/vendors', data: input),
    (d) => Vendor.fromJson(d as Json),
  );
  Future<Vendor> updateVendor(String id, Json input) => _call(
    () => dio.put('/api/v1/admin/vendors/$id', data: input),
    (d) => Vendor.fromJson(d as Json),
  );
  Future<void> deleteVendor(String id) =>
      _call(() => dio.delete('/api/v1/admin/vendors/$id'), (_) {});

  Future<PolicyConfig> getPolicy() => _call(
    () => dio.get('/api/v1/admin/policy'),
    (d) => PolicyConfig.fromJson(d as Json),
  );
  Future<PolicyConfig> updatePolicy(Json input) => _call(
    () => dio.put('/api/v1/admin/policy', data: input),
    (d) => PolicyConfig.fromJson(d as Json),
  );

  Future<List<Address>> adminAddresses() => _call(
    () => dio.get('/api/v1/admin/addresses'),
    (d) => _listOf(d, 'addresses', Address.fromJson),
  );
  Future<Address> createAddress(Json input) => _call(
    () => dio.post('/api/v1/admin/addresses', data: input),
    (d) => Address.fromJson(d as Json),
  );
  Future<Address> updateAddress(String id, Json input) => _call(
    () => dio.put('/api/v1/admin/addresses/$id', data: input),
    (d) => Address.fromJson(d as Json),
  );
  Future<void> deleteAddress(String id) =>
      _call(() => dio.delete('/api/v1/admin/addresses/$id'), (_) {});

  Future<SystemPrompt> getSystemPrompt() => _call(
    () => dio.get('/api/v1/admin/system-prompt'),
    (d) => SystemPrompt.fromJson(d as Json),
  );
  Future<SystemPrompt> updateSystemPrompt(String content) => _call(
    () => dio.put('/api/v1/admin/system-prompt', data: {'content': content}),
    (d) => SystemPrompt.fromJson(d as Json),
  );

  /// The seeded default prompt text (not saved; PUT it to apply).
  Future<String> defaultSystemPrompt() => _call(
    () => dio.get('/api/v1/admin/system-prompt/default'),
    (d) => ((d as Json)['content'] as String?) ?? '',
  );
}
