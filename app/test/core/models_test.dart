import 'package:flutter_test/flutter_test.dart';
import 'package:jarvis_office/core/api/models.dart';
import 'package:jarvis_office/core/api/sse.dart';
import 'package:jarvis_office/core/format.dart';

void main() {
  test('RequestDetail parses the openapi shape', () {
    final d = RequestDetail.fromJson({
      'request': {
        'id': 'r1',
        'requester_id': 'u1',
        'raw_utterance': 'printer paper',
        'status': 'pending_approval',
        'subtotal_cents': 7100,
        'shipping_cents': 0,
        'total_cents': 7100,
        'currency': 'SGD',
        'decision': 'NEEDS_APPROVAL',
        'created_at': '2026-10-09T10:00:00Z',
        'updated_at': '2026-10-09T10:00:01Z',
      },
      'line_items': [
        {
          'id': 'l1',
          'request_id': 'r1',
          'position': 0,
          'catalog_item_id': null,
          'description': 'standing desk',
          'qty': 1,
          'urgency': 'normal',
          'policy_decision': 'NEEDS_APPROVAL',
          'reasons': [
            {'code': 'OFF_LIST', 'message': 'Not on the approved list'},
          ],
          'selected_offer_id': 'o2',
        },
      ],
      'offers': [
        {
          'id': 'o1',
          'line_item_id': 'l1',
          'merchant_name': 'ErgoTune',
          'reap_product_id': 'p',
          'reap_variant_id': 'v',
          'title': 'B',
          'unit_price_cents': 59900,
          'currency': 'SGD',
          'pack_size': 1,
          'shipping_cents': 0,
          'landed_cost_cents': 59900,
          'available': true,
          'rank': 2,
        },
        {
          'id': 'o2',
          'line_item_id': 'l1',
          'merchant_name': 'ErgoTune',
          'reap_product_id': 'p',
          'reap_variant_id': 'v',
          'title': 'A',
          'unit_price_cents': 54900,
          'currency': 'SGD',
          'pack_size': 1,
          'shipping_cents': 0,
          'landed_cost_cents': 54900,
          'available': true,
          'rank': 1,
        },
      ],
      'approvals': [
        {
          'id': 'a1',
          'request_id': 'r1',
          'kind': 'price_drift',
          'status': 'pending',
          'reasons': [],
          'amount_cents': 7500,
          'prev_cents': 7100,
          'created_at': '2026-10-09T10:00:00Z',
        },
      ],
      'payments': [
        {
          'id': 'p1',
          'request_id': 'r1',
          'merchant_name': 'Popular Bookstore',
          'items_cents': 7100,
          'shipping_cents': 0,
          'tax_cents': 0,
          'quoted_cents': 7100,
          'currency': 'SGD',
          'status': 'requires_action',
          'approval_url': 'https://reap.example/approve',
        },
      ],
    });
    expect(d.request.status, RequestStatus.pendingApproval);
    expect(d.request.decision, Decision.needsApproval);
    expect(d.lineItems.single.isOffList, isTrue);
    expect(d.lineItems.single.reasons.single.code, 'OFF_LIST');
    expect(d.offersFor('l1').map((o) => o.id), ['o2', 'o1']);
    expect(d.selectedOffer(d.lineItems.single)?.title, 'A');
    expect(d.approvals.single.isPriceDrift, isTrue);
    expect(d.approvals.single.prevCents, 7100);
    expect(d.payments.single.status, PaymentStatus.requiresAction);
    expect(d.address, isNull);
  });

  test('enum wire values round-trip and unknowns are tolerated', () {
    for (final s in RequestStatus.values.where(
      (s) => s != RequestStatus.unknown,
    )) {
      expect(RequestStatus.fromJson(s.toJson()), s);
    }
    expect(RequestStatus.checkingOut.toJson(), 'checking_out');
    expect(RequestStatus.fromJson('brand_new_status'), RequestStatus.unknown);
    expect(Role.fromJson('approver').canApprove, isTrue);
    expect(EnrollmentStatus.fromJson('ACTIVE'), EnrollmentStatus.active);
  });

  test('User initials and Address one-liner', () {
    expect(
      const User(
        id: '',
        name: 'Maya Tan',
        email: '',
        role: Role.manager,
      ).initials,
      'MT',
    );
    final a = Address.fromJson({
      'id': 'a',
      'label': 'HQ',
      'first_name': 'Maya',
      'last_name': 'Tan',
      'phone': '+6562001001',
      'email': 'm@example.com',
      'address_line1': '7 Straits View',
      'address_line2': '#20-01',
      'city': 'Singapore',
      'postal_code': '018936',
      'country': 'SG',
      'is_default': true,
    });
    expect(a.oneLine, '7 Straits View, #20-01, Singapore 018936');
    expect(a.toInput()['postal_code'], '018936');
  });

  test('SSE frames parse; comments and junk are ignored', () {
    final e = parseSseFrame(
      'id: 7\nevent: request.status_changed\ndata: {"id":7,"type":"request.status_changed","request_id":"r1","at":"2026-10-09T10:00:00Z","data":{"from":"searching","to":"quoted"}}',
    );
    expect(e, isNotNull);
    expect(e!.type, SseTypes.requestStatusChanged);
    expect(e.data['to'], 'quoted');
    expect(parseSseFrame(': keep-alive'), isNull);
    expect(parseSseFrame('data: not json'), isNull);
  });

  test('money formatting uses cents', () {
    expect(Fmt.money(3550), 'S\$35.50');
    expect(Fmt.money(0), 'S\$0.00');
    expect(Fmt.monthShort('2026-10'), 'Oct');
    expect(Fmt.percent(150000, 300000), '50%');
    final now = DateTime(2026, 10, 9, 12);
    expect(
      Fmt.relative(now.subtract(const Duration(minutes: 5)), now: now),
      '5 min ago',
    );
  });
}
