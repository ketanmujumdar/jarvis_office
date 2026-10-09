import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api/models.dart';
import 'webrtc_transport.dart';

/// A connection to an OpenAI Realtime session.
///
/// The production implementation ([WebRtcRealtimeTransport]) streams the
/// microphone over WebRTC and exchanges JSON events on the `oai-events` data
/// channel. Tests use an in-memory fake.
abstract class RealtimeTransport {
  /// Server events (already JSON-decoded). A synthetic
  /// `{"type": "transport.closed"}` is emitted when the connection drops.
  Stream<Json> get events;

  /// Connects using the ephemeral session minted by the backend.
  Future<void> connect(RealtimeSession session);

  /// Sends one client event (e.g. `conversation.item.create`).
  void send(Json event);

  /// Mutes or unmutes the microphone track.
  void setMuted(bool muted);

  Future<void> close();
}

typedef RealtimeTransportFactory = RealtimeTransport Function();

/// Creates the transport for a new voice session. Override in tests.
final realtimeTransportFactoryProvider = Provider<RealtimeTransportFactory>(
  (ref) => WebRtcRealtimeTransport.new,
);

/// Synthetic event type emitted when the transport closes.
const transportClosedEvent = 'transport.closed';
