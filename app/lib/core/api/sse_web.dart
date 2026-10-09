import 'dart:async';
import 'dart:js_interop';

import 'package:web/web.dart' as web;

import 'models.dart';
import 'sse_common.dart';

SseEvent? parseFrame(String frame) => parseSseFrameCommon(frame);

Stream<SseEvent> connect(Uri uri, {String? token}) {
  web.EventSource? source;
  late StreamController<SseEvent> controller;

  void onMessage(web.Event raw) {
    final msg = raw as web.MessageEvent;
    final data = (msg.data as JSString?)?.toDart;
    if (data == null) return;
    final e = parseFrame('data: $data');
    if (e != null) controller.add(e);
  }

  controller = StreamController<SseEvent>(
    onListen: () {
      source = web.EventSource(uri.toString());
      // Named events (`event: <type>`) are not delivered to onmessage, so we
      // listen for every known type plus the default "message" type.
      final listener = onMessage.toJS;
      for (final t in _types) {
        source!.addEventListener(t, listener);
      }
      source!.onerror = ((web.Event _) {
        // EventSource reconnects by itself; surface a soft error so callers can re-fetch.
        if (source!.readyState == web.EventSource.CLOSED) {
          controller.addError(StateError('event stream closed'));
        }
      }).toJS;
    },
    onCancel: () {
      source?.close();
    },
  );
  return controller.stream;
}

const _types = [
  'message',
  SseTypes.requestCreated,
  SseTypes.requestStatusChanged,
  SseTypes.lineItemsParsed,
  SseTypes.searchStarted,
  SseTypes.searchVendorResult,
  SseTypes.offersRanked,
  SseTypes.policyEvaluated,
  SseTypes.approvalRequested,
  SseTypes.approvalDecided,
  SseTypes.checkoutQuoted,
  SseTypes.checkoutPriceDrift,
  SseTypes.paymentActionRequired,
  SseTypes.paymentStatusChanged,
  SseTypes.orderCompleted,
  SseTypes.paymentAlert,
  SseTypes.enrollmentUpdated,
  SseTypes.agentMessage,
  SseTypes.heartbeat,
];
