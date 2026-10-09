import 'package:flutter/foundation.dart';
import 'package:flutter_webrtc/flutter_webrtc.dart';

import '../core/api/models.dart';
import '../features/voice/realtime/realtime_transport.dart';
import '../features/voice/realtime/webrtc_transport.dart';

/// iOS flavour of the Realtime transport.
///
/// The shared [WebRtcRealtimeTransport] already uses flutter_webrtc's
/// cross-platform API (no dart:html), so this wraps it and only adds the
/// native audio-session setup iOS needs: a voice-chat `playAndRecord`
/// session (echo cancellation) that routes the assistant to the loudspeaker
/// instead of the earpiece.
class IosRealtimeTransport implements RealtimeTransport {
  IosRealtimeTransport() : _inner = WebRtcRealtimeTransport();

  final WebRtcRealtimeTransport _inner;

  bool get _isIos => !kIsWeb && defaultTargetPlatform == TargetPlatform.iOS;

  @override
  Stream<Json> get events => _inner.events;

  @override
  Future<void> connect(RealtimeSession session) async {
    if (_isIos) {
      try {
        await Helper.setAppleAudioConfiguration(
          AppleAudioConfiguration(
            appleAudioCategory: AppleAudioCategory.playAndRecord,
            appleAudioCategoryOptions: const {
              AppleAudioCategoryOption.defaultToSpeaker,
              AppleAudioCategoryOption.allowBluetooth,
              AppleAudioCategoryOption.allowBluetoothA2DP,
              AppleAudioCategoryOption.allowAirPlay,
            },
            appleAudioMode: AppleAudioMode.voiceChat,
          ),
        );
      } catch (_) {
        // Non-fatal: the default session still works, just quieter.
      }
    }
    await _inner.connect(session);
    if (_isIos) {
      try {
        await Helper.setSpeakerphoneOnButPreferBluetooth();
      } catch (_) {
        try {
          await Helper.setSpeakerphoneOn(true);
        } catch (_) {}
      }
    }
  }

  @override
  void send(Json event) => _inner.send(event);

  @override
  void setMuted(bool muted) => _inner.setMuted(muted);

  @override
  Future<void> close() => _inner.close();
}
