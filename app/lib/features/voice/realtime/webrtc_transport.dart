import 'dart:async';
import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:flutter_webrtc/flutter_webrtc.dart';

import '../../../core/api/models.dart';
import 'realtime_transport.dart';

/// Default endpoint for the WebRTC SDP exchange (GA Realtime API).
const defaultRealtimeCallsUrl = 'https://api.openai.com/v1/realtime/calls';

/// OpenAI Realtime over WebRTC (flutter_webrtc).
///
/// Flow: capture the mic → create an `oai-events` data channel → create an
/// SDP offer → POST it to `calls_url` with the ephemeral client secret as the
/// bearer → apply the SDP answer → wait for the data channel to open.
/// Remote audio is played through an [RTCVideoRenderer] (an `<audio>` element
/// on web). The client secret is never logged or stored.
class WebRtcRealtimeTransport implements RealtimeTransport {
  WebRtcRealtimeTransport({Dio? dio}) : _dio = dio ?? Dio();

  final Dio _dio;
  final _events = StreamController<Json>.broadcast();
  RTCPeerConnection? _pc;
  RTCDataChannel? _dc;
  MediaStream? _mic;
  RTCVideoRenderer? _renderer;
  bool _closed = false;

  @override
  Stream<Json> get events => _events.stream;

  @override
  Future<void> connect(RealtimeSession session) async {
    final opened = Completer<void>();
    try {
      final pc = _pc = await createPeerConnection({
        'iceServers': <Map<String, dynamic>>[],
      });

      final renderer = _renderer = RTCVideoRenderer();
      await renderer.initialize();
      pc.onTrack = (event) {
        if (event.streams.isNotEmpty) renderer.srcObject = event.streams.first;
      };
      pc.onConnectionState = (state) {
        if (state == RTCPeerConnectionState.RTCPeerConnectionStateFailed ||
            state == RTCPeerConnectionState.RTCPeerConnectionStateClosed) {
          _emitClosed();
        }
      };

      final mic = _mic = await navigator.mediaDevices.getUserMedia({
        'audio': {'echoCancellation': true, 'noiseSuppression': true},
        'video': false,
      });
      for (final track in mic.getAudioTracks()) {
        await pc.addTrack(track, mic);
      }

      final dc = _dc = await pc.createDataChannel(
        'oai-events',
        RTCDataChannelInit(),
      );
      dc.onDataChannelState = (state) {
        if (state == RTCDataChannelState.RTCDataChannelOpen &&
            !opened.isCompleted) {
          opened.complete();
        } else if (state == RTCDataChannelState.RTCDataChannelClosed) {
          _emitClosed();
        }
      };
      dc.onMessage = (msg) {
        if (msg.isBinary) return;
        try {
          final decoded = jsonDecode(msg.text);
          if (decoded is Map<String, dynamic>) _events.add(decoded);
        } on FormatException {
          // Ignore malformed frames.
        }
      };

      final offer = await pc.createOffer({});
      await pc.setLocalDescription(offer);

      final url = session.callsUrl.isEmpty
          ? defaultRealtimeCallsUrl
          : session.callsUrl;
      final res = await _dio.post<String>(
        url,
        data: offer.sdp,
        options: Options(
          contentType: 'application/sdp',
          responseType: ResponseType.plain,
          headers: {'Authorization': 'Bearer ${session.clientSecret}'},
        ),
      );
      final answer = res.data;
      if (answer == null || answer.isEmpty) {
        throw const RealtimeConnectException('Empty SDP answer from OpenAI');
      }
      await pc.setRemoteDescription(RTCSessionDescription(answer, 'answer'));
      await opened.future.timeout(const Duration(seconds: 15));
    } on DioException catch (e) {
      await close();
      // Do not include the request (it carries the client secret).
      throw RealtimeConnectException(
        'OpenAI Realtime refused the connection'
        '${e.response?.statusCode != null ? ' (HTTP ${e.response!.statusCode})' : ''}',
      );
    } on TimeoutException {
      await close();
      throw const RealtimeConnectException('Timed out connecting to voice');
    } catch (e) {
      await close();
      if (e is RealtimeConnectException) rethrow;
      throw RealtimeConnectException('Could not start voice: $e');
    }
  }

  @override
  void send(Json event) {
    final dc = _dc;
    if (dc == null || dc.state != RTCDataChannelState.RTCDataChannelOpen) {
      return;
    }
    dc.send(RTCDataChannelMessage(jsonEncode(event)));
  }

  @override
  void setMuted(bool muted) {
    for (final t in _mic?.getAudioTracks() ?? const <MediaStreamTrack>[]) {
      t.enabled = !muted;
    }
  }

  void _emitClosed() {
    if (_closed) return;
    _closed = true;
    if (!_events.isClosed) _events.add({'type': transportClosedEvent});
  }

  @override
  Future<void> close() async {
    _closed = true;
    try {
      await _dc?.close();
    } catch (_) {}
    for (final t in _mic?.getTracks() ?? const <MediaStreamTrack>[]) {
      try {
        await t.stop();
      } catch (_) {}
    }
    try {
      await _mic?.dispose();
    } catch (_) {}
    try {
      await _pc?.close();
    } catch (_) {}
    try {
      _renderer?.srcObject = null;
      await _renderer?.dispose();
    } catch (_) {}
    _dc = null;
    _mic = null;
    _pc = null;
    _renderer = null;
    if (!_events.isClosed) await _events.close();
  }
}

/// A user-presentable voice connection failure (never contains secrets).
class RealtimeConnectException implements Exception {
  const RealtimeConnectException(this.message);
  final String message;
  @override
  String toString() => message;
}
