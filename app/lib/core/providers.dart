import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'api/api_client.dart';
import 'api/models.dart';
import 'api/sse.dart';
import 'auth/session.dart';
import 'config.dart';

export 'auth/session.dart';

/// The typed API client. Reads the current session token on every request.
/// Override in tests: `apiProvider.overrideWithValue(fakeApi)`.
final apiProvider = Provider<JarvisApi>((ref) {
  return JarvisApi(
    baseUrl: AppConfig.apiBaseUrl,
    token: () => ref.read(sessionProvider)?.token,
  );
});

/// The one SSE connection for the session (all events, fanned out to every
/// listener). Browsers allow only ~6 HTTP/1.1 connections per host, so opening
/// one EventSource per request would starve the API calls. The connection opens
/// with the first listener and closes when the last one goes away.
final sseHubProvider = Provider.autoDispose<Stream<SseEvent>>((ref) {
  final api = ref.watch(apiProvider);
  final token = ref.watch(sessionProvider.select((s) => s?.token));
  StreamSubscription<SseEvent>? sub;
  late final StreamController<SseEvent> hub;
  hub = StreamController<SseEvent>.broadcast(
    onListen: () {
      sub = openEventStream(
        baseUrl: api.baseUrl,
        token: token,
      ).listen(hub.add, onError: hub.addError);
    },
    onCancel: () {
      sub?.cancel();
      sub = null;
    },
  );
  ref.onDispose(() {
    sub?.cancel();
    hub.close();
  });
  return hub.stream;
});

/// Live SSE events, optionally filtered to one request (`null` = all events).
/// Filters the shared [sseHubProvider] stream client-side; auto-disposed so
/// nothing stays subscribed after the last widget stops listening.
final eventStreamProvider = StreamProvider.autoDispose
    .family<SseEvent, String?>((ref, requestId) {
      final all = ref.watch(sseHubProvider);
      if (requestId == null) return all;
      return all.where((e) => e.requestId == requestId);
    });
