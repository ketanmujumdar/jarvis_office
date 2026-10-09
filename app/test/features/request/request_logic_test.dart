import 'package:flutter_test/flutter_test.dart';
import 'package:jarvis_office/core/api/models.dart';
import 'package:jarvis_office/core/theme/tokens.dart';
import 'package:jarvis_office/features/request/request_logic.dart';

import 'fakes.dart';

void main() {
  group('stageFor', () {
    final cases = <RequestStatus, ProgressStage?>{
      RequestStatus.parsing: ProgressStage.understand,
      RequestStatus.searching: ProgressStage.search,
      RequestStatus.quoted: ProgressStage.review,
      RequestStatus.pendingApproval: ProgressStage.approve,
      RequestStatus.approved: ProgressStage.approve,
      RequestStatus.checkingOut: ProgressStage.pay,
      RequestStatus.awaitingPayment: ProgressStage.pay,
      RequestStatus.paying: ProgressStage.pay,
      RequestStatus.ordered: ProgressStage.done,
      RequestStatus.failed: null,
      RequestStatus.rejected: null,
      RequestStatus.cancelled: null,
      RequestStatus.unknown: null,
    };
    for (final MapEntry(:key, :value) in cases.entries) {
      test('$key -> $value', () => expect(stageFor(key), value));
    }
  });

  group('activityFromEvent', () {
    final cases =
        <
          ({
            String name,
            String type,
            Json data,
            String? title,
            Tone? tone,
            String? detail,
          })
        >[
          (
            name: 'heartbeat is ignored',
            type: SseTypes.heartbeat,
            data: {},
            title: null,
            tone: null,
            detail: null,
          ),
          (
            name: 'agent message is ignored',
            type: SseTypes.agentMessage,
            data: {'content': 'hi'},
            title: null,
            tone: null,
            detail: null,
          ),
          (
            name: 'parsed items',
            type: SseTypes.lineItemsParsed,
            data: {
              'line_items': [
                {'qty': 2, 'description': 'Coffee'},
                {'qty': 1, 'description': 'Paper'},
              ],
            },
            title: 'Understood 2 items',
            tone: Tone.brand,
            detail: '2 × Coffee, 1 × Paper',
          ),
          (
            name: 'search started',
            type: SseTypes.searchStarted,
            data: {
              'vendors': ['popular.com.sg'],
            },
            title: 'Searching 1 vendor',
            tone: Tone.info,
            detail: 'popular.com.sg',
          ),
          (
            name: 'vendor result',
            type: SseTypes.searchVendorResult,
            data: {'vendor_domain': 'bettr.sg', 'offers_found': 3},
            title: 'bettr.sg: 3 offers',
            tone: Tone.info,
            detail: null,
          ),
          (
            name: 'vendor error',
            type: SseTypes.searchVendorResult,
            data: {'vendor_domain': 'bettr.sg', 'error': 'timeout'},
            title: 'bettr.sg unavailable',
            tone: Tone.warning,
            detail: 'timeout',
          ),
          (
            name: 'ranked with best',
            type: SseTypes.offersRanked,
            data: {
              'offers': [
                {
                  'merchant_name': 'Bettr',
                  'title': 'Beans',
                  'landed_cost_cents': 4200,
                },
              ],
            },
            title: 'Best offer: Bettr',
            tone: Tone.success,
            detail: r'Beans · S$42.00',
          ),
          (
            name: 'ranked empty',
            type: SseTypes.offersRanked,
            data: {'offers': []},
            title: 'No allowed vendor stocks this item',
            tone: Tone.warning,
            detail: null,
          ),
          (
            name: 'policy needs approval',
            type: SseTypes.policyEvaluated,
            data: {
              'decision': 'NEEDS_APPROVAL',
              'reasons': [
                {'code': 'over_limit', 'message': 'Over S\$500'},
              ],
            },
            title: 'Policy: Needs approval',
            tone: Tone.warning,
            detail: r'Over S$500',
          ),
          (
            name: 'status failed',
            type: SseTypes.requestStatusChanged,
            data: {'from': 'paying', 'to': 'failed', 'failure_reason': 'boom'},
            title: 'Failed',
            tone: Tone.danger,
            detail: 'boom',
          ),
          (
            name: 'price drift',
            type: SseTypes.checkoutPriceDrift,
            data: {'approved_cents': 10000, 'live_cents': 11000, 'pct': 10},
            title: 'Price changed 10.0%',
            tone: Tone.warning,
            detail: r'S$100.00 → S$110.00',
          ),
          (
            name: 'payment action',
            type: SseTypes.paymentActionRequired,
            data: {'payment': samplePayment(), 'approval_url': 'x'},
            title: 'Approve payment with Reap',
            tone: Tone.warning,
            detail: r'Common Man Coffee Roasters SG · S$75.00',
          ),
          (
            name: 'payment completed',
            type: SseTypes.paymentStatusChanged,
            data: {'payment': samplePayment(status: 'completed')},
            title: 'Common Man Coffee Roasters SG: Paid',
            tone: Tone.success,
            detail: null,
          ),
          (
            name: 'order completed',
            type: SseTypes.orderCompleted,
            data: {
              'payments': [samplePayment()],
            },
            title: 'Order placed',
            tone: Tone.success,
            detail: '1 merchant order(s) confirmed',
          ),
        ];
    for (final c in cases) {
      test(c.name, () {
        final a = activityFromEvent(sseEvent(1, c.type, c.data));
        if (c.title == null) {
          expect(a, isNull);
          return;
        }
        expect(a, isNotNull);
        expect(a!.title, c.title);
        expect(a.tone, c.tone);
        expect(a.detail, c.detail);
      });
    }
  });

  group('quoteFor', () {
    test('selected offer is best; savings vs next available offer', () {
      final d = sampleDetail();
      final q = quoteFor(d, d.lineItems.first);
      expect(q.best!.id, 'o1');
      expect(q.runnerUp!.id, 'o2'); // o3 is unavailable
      expect(q.savingsCents, 800);
      expect(q.offers.map((o) => o.id), ['o1', 'o2', 'o3']);
    });

    test('falls back to the top-ranked available offer', () {
      final d = sampleDetail();
      final q = quoteFor(d, d.lineItems[1]);
      expect(q.best!.id, 'o4');
      expect(q.runnerUp, isNull);
      expect(q.savingsCents, 0);
    });

    test('no offers gives no best', () {
      final d = sampleDetail(withOffers: false);
      final q = quoteFor(d, d.lineItems.first);
      expect(q.best, isNull);
      expect(q.savingsCents, 0);
    });

    test('totalSavings sums lines', () {
      expect(totalSavings(sampleDetail()), 800);
    });
  });

  group('canConfirm and pendingApproval', () {
    test('quoted and not rejected can confirm', () {
      expect(canConfirm(sampleDetail()), isTrue);
      expect(canConfirm(sampleDetail(decision: 'REJECT')), isFalse);
      expect(canConfirm(sampleDetail(status: 'searching')), isFalse);
    });

    test('finds the latest pending approval', () {
      final d = sampleDetail(
        status: 'pending_approval',
        approvals: [
          {
            'id': 'ap1',
            'request_id': 'r1',
            'kind': 'policy',
            'status': 'approved',
            'amount_cents': 100,
          },
          {
            'id': 'ap2',
            'request_id': 'r1',
            'kind': 'price_drift',
            'status': 'pending',
            'amount_cents': 110,
            'prev_cents': 100,
          },
        ],
      );
      expect(pendingApproval(d)!.id, 'ap2');
      expect(pendingApproval(sampleDetail()), isNull);
    });
  });

  test('eventChangesRequest skips noise', () {
    expect(eventChangesRequest(sseEvent(1, SseTypes.heartbeat, {})), isFalse);
    expect(
      eventChangesRequest(sseEvent(1, SseTypes.agentMessage, {})),
      isFalse,
    );
    expect(
      eventChangesRequest(sseEvent(1, SseTypes.requestStatusChanged, {})),
      isTrue,
    );
  });
}
