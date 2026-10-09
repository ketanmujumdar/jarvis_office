import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_riverpod/misc.dart' show Override;
import 'package:flutter_test/flutter_test.dart';
import 'package:jarvis_office/core/api/api_client.dart';
import 'package:jarvis_office/core/api/models.dart';
import 'package:jarvis_office/core/providers.dart';
import 'package:jarvis_office/features/request/request_controller.dart';
import 'package:jarvis_office/features/voice/realtime/realtime_transport.dart';

import '../../helpers.dart';

/// In-memory JarvisApi for widget and controller tests (no network).
class FakeJarvisApi extends JarvisApi {
  FakeJarvisApi() : super(baseUrl: 'http://fake.local');

  final details = <String, RequestDetail>{};
  int getRequestCalls = 0;
  final confirmCalls = <(String, String)>[];
  ApiException? confirmError;
  RequestDetail? afterConfirm;
  int refreshCheckoutCalls = 0;
  int enrollmentStarts = 0;

  /// The card enrollment. Active by default so confirm is possible; set to
  /// null (no card) or a requires-action one to test the card callout.
  Enrollment? enrollment = Enrollment(
    id: 'e0',
    reapEnrollmentId: 'enr_0',
    status: EnrollmentStatus.active,
    createdAt: DateTime.utc(2026, 10, 1),
  );
  List<Address> addressList = sampleAddresses();

  final chatCalls = <(String, String?)>[];
  ChatOutput chatReply = const ChatOutput(
    sessionId: 's1',
    reply: 'OK',
    requestIds: [],
    toolCalls: [],
  );
  ApiException? chatError;
  Completer<void>? chatGate;

  final toolCalls = <({String name, Object? args, String? callId})>[];
  Json Function(String name, Object? args) toolResult = (_, _) => {'ok': true};
  Completer<void>? toolGate;

  RealtimeSession session = RealtimeSession(
    clientSecret: 'ek_test',
    expiresAt: DateTime.utc(2030),
    model: 'gpt-realtime',
    callsUrl: 'https://example.invalid/calls',
  );
  ApiException? sessionError;
  int sessionMints = 0;

  @override
  Future<RequestDetail> getRequest(String id) async {
    getRequestCalls++;
    final d = details[id];
    if (d == null) {
      throw const ApiException(
        statusCode: 404,
        code: 'not_found',
        message: 'Request not found',
      );
    }
    return d;
  }

  @override
  Future<RequestDetail> confirmRequest(
    String id, {
    required String addressId,
  }) async {
    confirmCalls.add((id, addressId));
    if (confirmError != null) throw confirmError!;
    final next = afterConfirm ?? details[id]!;
    details[id] = next;
    return next;
  }

  @override
  Future<RequestDetail> cancelRequest(String id) async => details[id]!;

  @override
  Future<CheckoutStatus> refreshCheckout(String id) async {
    refreshCheckoutCalls++;
    final d = details[id]!;
    return CheckoutStatus(
      requestId: id,
      status: d.request.status,
      payments: d.payments,
    );
  }

  @override
  Future<List<Address>> addresses() async => addressList;

  @override
  Future<Enrollment?> currentEnrollment() async => enrollment;

  @override
  Future<Enrollment> startEnrollment() async {
    enrollmentStarts++;
    return Enrollment(
      id: 'e1',
      reapEnrollmentId: 'enr_1',
      status: EnrollmentStatus.requiresAction,
      createdAt: DateTime.utc(2026, 10, 9),
      nextActionUrl: 'https://reap.example/enroll/enr_1',
    );
  }

  @override
  Future<ChatOutput> chat(String message, {String? sessionId}) async {
    chatCalls.add((message, sessionId));
    if (chatGate != null) await chatGate!.future;
    if (chatError != null) throw chatError!;
    return chatReply;
  }

  @override
  Future<Json> executeTool(
    String name,
    Object? arguments, {
    String? callId,
    String? sessionId,
  }) async {
    toolCalls.add((name: name, args: arguments, callId: callId));
    if (toolGate != null) await toolGate!.future;
    return toolResult(name, arguments);
  }

  @override
  Future<RealtimeSession> realtimeSession() async {
    sessionMints++;
    if (sessionError != null) throw sessionError!;
    return session;
  }
}

/// Fake Realtime transport: records sent events, lets tests push server events.
class FakeTransport implements RealtimeTransport {
  final _events = StreamController<Json>.broadcast(sync: true);
  final sent = <Json>[];
  RealtimeSession? connectedWith;
  Object? connectError;
  bool muted = false;
  bool closed = false;

  void emit(Json e) => _events.add(e);

  List<String> get sentTypes => sent.map((e) => e['type'] as String).toList();

  @override
  Stream<Json> get events => _events.stream;

  @override
  Future<void> connect(RealtimeSession session) async {
    if (connectError != null) throw connectError!;
    connectedWith = session;
  }

  @override
  void send(Json event) => sent.add(event);

  @override
  void setMuted(bool m) => muted = m;

  @override
  Future<void> close() async {
    closed = true;
  }
}

List<Address> sampleAddresses() => [
  Address.fromJson({
    'id': 'a1',
    'label': 'Marina One HQ',
    'first_name': 'Maya',
    'last_name': 'Tan',
    'phone': '+65 6123 4567',
    'email': 'maya@example.com',
    'address_line1': '7 Straits View',
    'address_line2': '#20-01 Marina One East Tower',
    'city': 'Singapore',
    'postal_code': '018936',
    'country': 'SG',
    'is_default': true,
  }),
  Address.fromJson({
    'id': 'a2',
    'label': 'one-north Studio',
    'first_name': 'Daniel',
    'last_name': 'Lim',
    'phone': '+65 6234 5678',
    'email': 'daniel@example.com',
    'address_line1': '1 Fusionopolis Way',
    'city': 'Singapore',
    'postal_code': '138632',
    'country': 'SG',
    'is_default': false,
  }),
];

Json _offer(
  String id,
  String li,
  String merchant,
  int unit,
  int landed,
  int rank, {
  bool available = true,
}) => {
  'id': id,
  'line_item_id': li,
  'merchant_name': merchant,
  'reap_product_id': 'p_$id',
  'reap_variant_id': 'v_$id',
  'title': 'Product $id',
  'unit_price_cents': unit,
  'currency': 'SGD',
  'pack_size': 1,
  'shipping_cents': 500,
  'landed_cost_cents': landed,
  'available': available,
  'rank': rank,
};

/// A request with two line items and ranked offers.
RequestDetail sampleDetail({
  String id = 'r1',
  String status = 'quoted',
  String decision = 'AUTO_APPROVE',
  List<Json> payments = const [],
  List<Json> approvals = const [],
  Json? address,
  bool withOffers = true,
  List<Json> extraLines = const [],
}) => RequestDetail.fromJson({
  'request': {
    'id': id,
    'requester_id': 'u1',
    'raw_utterance': 'restock coffee and paper',
    'status': status,
    'subtotal_cents': 9000,
    'shipping_cents': 1000,
    'total_cents': 10000,
    'currency': 'SGD',
    'decision': decision,
    'created_at': '2026-10-09T08:00:00Z',
    'updated_at': '2026-10-09T08:00:00Z',
  },
  'line_items': [
    {
      'id': 'li1',
      'request_id': id,
      'position': 1,
      'catalog_item_id': 'c1',
      'description': 'Coffee beans 1kg',
      'qty': 2,
      'urgency': 'normal',
      'policy_decision': 'AUTO_APPROVE',
      'reasons': [],
      'selected_offer_id': withOffers ? 'o1' : null,
    },
    {
      'id': 'li2',
      'request_id': id,
      'position': 2,
      'description': 'Fancy notebook',
      'qty': 1,
      'urgency': 'urgent',
      'policy_decision': 'NEEDS_APPROVAL',
      'reasons': [
        {'code': 'off_list', 'message': 'Not on the approved list'},
      ],
    },
    ...extraLines,
  ],
  'offers': withOffers
      ? [
          _offer('o1', 'li1', 'Common Man Coffee Roasters SG', 3500, 7500, 1),
          _offer('o2', 'li1', 'Bettr Coffee', 3900, 8300, 2),
          _offer('o3', 'li1', 'Alchemist', 4500, 9500, 3, available: false),
          _offer('o4', 'li2', 'Popular Bookstore', 1200, 1700, 1),
        ]
      : [],
  'approvals': approvals,
  'payments': payments,
  'address': ?address,
});

Json samplePayment({
  String status = 'requires_action',
  String url = 'https://reap.example/approve/chk_1',
  int? finalCents,
}) => {
  'id': 'pay1',
  'request_id': 'r1',
  'merchant_name': 'Common Man Coffee Roasters SG',
  'items_cents': 7000,
  'shipping_cents': 500,
  'tax_cents': 0,
  'quoted_cents': 7500,
  'currency': 'SGD',
  'status': status,
  'approval_url': url,
  'reap_checkout_id': 'chk_1',
  'final_cents': finalCents,
  'reap_order_id': finalCents != null ? 'ord_9' : '',
};

class _TestSession extends SessionController {
  _TestSession(this.initial);
  final Session? initial;
  @override
  Session? build() => initial;
}

/// Test harness for widgets that use the request feature. Signed in as
/// [role] (an office manager by default).
class Harness {
  Harness({FakeJarvisApi? api, this.role = Role.manager})
    : api = api ?? FakeJarvisApi();

  final FakeJarvisApi api;
  final Role role;
  final sse = StreamController<SseEvent>.broadcast();
  final opened = <String>[];
  final transport = FakeTransport();

  List<Override> get overrides => [
    apiProvider.overrideWithValue(api),
    sessionProvider.overrideWith(
      () => _TestSession(
        Session(
          token: 'u-test',
          user: User(
            id: 'u-test',
            name: 'Test User',
            email: 'test@jarvis.example',
            role: role,
          ),
        ),
      ),
    ),
    eventStreamProvider.overrideWith((ref, id) => sse.stream),
    requestPollIntervalProvider.overrideWithValue(null),
    requestRefreshDebounceProvider.overrideWithValue(Duration.zero),
    urlOpenerProvider.overrideWithValue((u) {
      opened.add(u);
      return true;
    }),
    realtimeTransportFactoryProvider.overrideWithValue(() => transport),
  ];

  Widget wrap(Widget child) => ProviderScope(
    overrides: overrides,
    child: themed(Scaffold(body: child)),
  );

  ProviderContainer container() {
    final c = ProviderContainer(overrides: overrides);
    addTearDown(c.dispose);
    return c;
  }

  Future<void> dispose() => sse.close();
}

SseEvent sseEvent(int id, String type, Json data, {String requestId = 'r1'}) =>
    SseEvent(
      id: id,
      type: type,
      requestId: requestId,
      at: DateTime.now().toUtc(),
      data: data,
    );
