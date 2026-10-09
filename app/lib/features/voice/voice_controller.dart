import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/api_client.dart';
import '../../core/api/models.dart';
import '../../core/providers.dart';
import 'realtime/realtime_transport.dart';

/// Who said a transcript line.
enum Speaker { user, assistant, tool, system }

/// One line in the live transcript.
@immutable
class TranscriptEntry {
  const TranscriptEntry({
    required this.id,
    required this.speaker,
    required this.text,
    this.partial = false,
    this.voice = false,
    this.toolName,
    this.isError = false,
  });

  final String id;
  final Speaker speaker;
  final String text;

  /// Still streaming (shown with a typing indicator).
  final bool partial;

  /// Came from the voice session rather than the text agent.
  final bool voice;
  final String? toolName;
  final bool isError;

  TranscriptEntry copyWith({String? text, bool? partial, bool? isError}) =>
      TranscriptEntry(
        id: id,
        speaker: speaker,
        text: text ?? this.text,
        partial: partial ?? this.partial,
        voice: voice,
        toolName: toolName,
        isError: isError ?? this.isError,
      );
}

/// Voice connection state shown by the mic orb.
enum VoiceStatus {
  idle('Tap to talk'),
  connecting('Connecting…'),
  listening('Listening'),
  userSpeaking('Hearing you'),
  thinking('Thinking'),
  speaking('Speaking'),
  error('Voice unavailable');

  const VoiceStatus(this.label);
  final String label;

  bool get isLive =>
      this == listening ||
      this == userSpeaking ||
      this == thinking ||
      this == speaking;
}

@immutable
class PlaygroundState {
  const PlaygroundState({
    this.transcript = const [],
    this.voiceStatus = VoiceStatus.idle,
    this.muted = false,
    this.sending = false,
    this.error,
    this.activeRequestId,
    this.requestIds = const [],
    this.chatSessionId,
  });

  final List<TranscriptEntry> transcript;
  final VoiceStatus voiceStatus;
  final bool muted;

  /// A text message is in flight to the text agent.
  final bool sending;
  final String? error;

  /// The request shown in the workspace (latest created/touched).
  final String? activeRequestId;

  /// Every request touched this session, oldest first.
  final List<String> requestIds;
  final String? chatSessionId;

  PlaygroundState copyWith({
    List<TranscriptEntry>? transcript,
    VoiceStatus? voiceStatus,
    bool? muted,
    bool? sending,
    String? Function()? error,
    String? activeRequestId,
    List<String>? requestIds,
    String? chatSessionId,
  }) => PlaygroundState(
    transcript: transcript ?? this.transcript,
    voiceStatus: voiceStatus ?? this.voiceStatus,
    muted: muted ?? this.muted,
    sending: sending ?? this.sending,
    error: error != null ? error() : this.error,
    activeRequestId: activeRequestId ?? this.activeRequestId,
    requestIds: requestIds ?? this.requestIds,
    chatSessionId: chatSessionId ?? this.chatSessionId,
  );
}

/// Extracts a purchase request id from a tool result, if present.
/// Accepts `{request_id}`, `{request: {id}}` and `{request_ids: [...]}`.
@visibleForTesting
String? requestIdFromToolOutput(Object? output) {
  if (output is! Map) return null;
  final direct = output['request_id'];
  if (direct is String && direct.isNotEmpty) return direct;
  final req = output['request'];
  if (req is Map && req['id'] is String && (req['id'] as String).isNotEmpty) {
    return req['id'] as String;
  }
  final ids = output['request_ids'];
  if (ids is List && ids.isNotEmpty && ids.last is String) {
    return ids.last as String;
  }
  return null;
}

/// Short human summary of a tool call for the transcript.
@visibleForTesting
String describeTool(String name, Object? output) {
  final out = output is Map ? output : const {};
  final err = out['error'];
  if (err is String && err.isNotEmpty) return '$name failed: $err';
  return switch (name) {
    'create_order_request' => 'Created a purchase request',
    'get_request_status' => () {
      final req = out['request'];
      final status = req is Map ? req['status'] : out['status'];
      return status is String
          ? 'Checked status: ${RequestStatus.fromJson(status).label}'
          : 'Checked request status';
    }(),
    'list_addresses' => () {
      final list = out['addresses'];
      return list is List
          ? 'Looked up ${list.length} delivery addresses'
          : 'Looked up delivery addresses';
    }(),
    'confirm_order' => 'Confirmed the order',
    'cancel_request' => 'Cancelled the request',
    _ => 'Ran $name',
  };
}

/// Drives the assistant playground: the text agent fallback, the OpenAI
/// Realtime voice session (via [RealtimeTransport]) and the tool relay.
///
/// Tool calls from the voice model are executed by the backend
/// (`POST /agent/tools/{name}`); this class only relays them and returns the
/// output, so policy and checkout stay deterministic server-side.
class PlaygroundController extends Notifier<PlaygroundState> {
  RealtimeTransport? _transport;
  StreamSubscription<Json>? _sub;
  String? _voiceSessionId;
  final _seenCalls = <String>{};
  int _pendingTools = 0;
  bool _responseActive = false;
  bool _needsResponse = false;
  int _seq = 0;
  ProviderSubscription<AsyncValue<SseEvent>>? _narration;

  @override
  PlaygroundState build() {
    // Reset when the signed-in user changes.
    ref.watch(currentUserProvider.select((u) => u?.id));
    ref.onDispose(_teardown);
    _narration = null;
    return const PlaygroundState();
  }

  // ------------------------------------------------------------ narration

  /// Statuses worth telling the user about unprompted.
  static const _narratedStatuses = {
    'quoted',
    'pending_approval',
    'approved',
    'awaiting_payment',
    'ordered',
    'failed',
    'cancelled',
    'rejected',
  };

  /// Turns backend progress (SSE) for this conversation's requests into a
  /// spoken update: the event goes into the realtime conversation and Jarvis
  /// is asked to tell the user, once any current reply has finished.
  void _onServerEvent(SseEvent e) {
    final id = e.requestId;
    if (id == null || !state.requestIds.contains(id)) return;
    String? update;
    switch (e.type) {
      case 'request.status_changed':
        final to = e.data['to'] as String?;
        if (to == null || !_narratedStatuses.contains(to)) return;
        final reason = e.data['failure_reason'] as String?;
        update =
            'Request $id is now "$to"'
            '${reason != null && reason.isNotEmpty ? ' ($reason)' : ''}.';
      case 'approval.decided':
        final a = e.data['approval'];
        final status = a is Map ? a['status'] : null;
        update = 'The approver has decided on request $id: $status.';
      case 'payment.alert':
        update =
            'Payment alert on request $id: ${e.data['message'] ?? e.data['kind']}.';
      default:
        return;
    }
    _system('Update: $update');
    // A tool call in flight (e.g. get_request_status waiting on the search)
    // will return the new state itself, so don't narrate it twice.
    if (!voiceLive || _pendingTools > 0) return;
    _transport?.send({
      'type': 'conversation.item.create',
      'item': {
        'type': 'message',
        'role': 'system',
        'content': [
          {
            'type': 'input_text',
            'text':
                '[Status update] $update Call get_request_status for '
                'details and tell the user what happened in one or two short '
                'sentences, including what they need to do next, if anything.',
          },
        ],
      },
    });
    _requestResponse(force: true);
  }

  JarvisApi get _api => ref.read(apiProvider);

  String _nextId(String prefix) => '$prefix-${++_seq}';

  // ------------------------------------------------------------ transcript

  void _append(TranscriptEntry e) {
    state = state.copyWith(transcript: [...state.transcript, e]);
  }

  void _upsert(String id, TranscriptEntry Function(TranscriptEntry? old) f) {
    final list = [...state.transcript];
    final i = list.indexWhere((e) => e.id == id);
    if (i >= 0) {
      list[i] = f(list[i]);
    } else {
      list.add(f(null));
    }
    state = state.copyWith(transcript: list);
  }

  void _remove(String id) {
    state = state.copyWith(
      transcript: state.transcript.where((e) => e.id != id).toList(),
    );
  }

  void _system(String text, {bool error = false}) => _append(
    TranscriptEntry(
      id: _nextId('sys'),
      speaker: Speaker.system,
      text: text,
      isError: error,
    ),
  );

  void _trackRequest(String? id) {
    if (id == null || id.isEmpty) return;
    // Follow live progress once the conversation has a request to narrate.
    _narration ??= ref.listen<AsyncValue<SseEvent>>(eventStreamProvider(null), (
      _,
      next,
    ) {
      final e = next.value;
      if (e != null && next.hasValue) _onServerEvent(e);
    });
    state = state.copyWith(
      activeRequestId: id,
      requestIds: state.requestIds.contains(id)
          ? state.requestIds
          : [...state.requestIds, id],
    );
  }

  /// Shows [id] in the workspace (e.g. picked from the session's requests).
  void selectRequest(String id) => _trackRequest(id);

  /// Ends any voice session and clears the conversation.
  Future<void> reset() async {
    await _teardown();
    if (!ref.mounted) return;
    _seenCalls.clear();
    _pendingTools = 0;
    _responseActive = false;
    _needsResponse = false;
    state = const PlaygroundState();
  }

  void clearError() => state = state.copyWith(error: () => null);

  // ------------------------------------------------------------ text

  bool get voiceLive => state.voiceStatus.isLive;

  /// Sends a typed message. While voice is live it goes into the realtime
  /// conversation; otherwise it is answered by the backend text agent.
  Future<void> sendText(String message) async {
    final text = message.trim();
    if (text.isEmpty || state.sending) return;
    _append(
      TranscriptEntry(
        id: _nextId('user'),
        speaker: Speaker.user,
        text: text,
        voice: voiceLive,
      ),
    );

    if (voiceLive && _transport != null) {
      _transport!.send({
        'type': 'conversation.item.create',
        'item': {
          'type': 'message',
          'role': 'user',
          'content': [
            {'type': 'input_text', 'text': text},
          ],
        },
      });
      _requestResponse(force: true);
      return;
    }

    state = state.copyWith(sending: true, error: () => null);
    try {
      final out = await _api.chat(text, sessionId: state.chatSessionId);
      if (!ref.mounted) return;
      state = state.copyWith(
        chatSessionId: out.sessionId.isEmpty ? null : out.sessionId,
      );
      for (final t in out.toolCalls) {
        _append(
          TranscriptEntry(
            id: _nextId('tool'),
            speaker: Speaker.tool,
            toolName: t.name,
            text: t.error.isNotEmpty
                ? '${t.name} failed: ${t.error}'
                : describeTool(t.name, t.result),
            isError: t.error.isNotEmpty,
          ),
        );
        _trackRequest(requestIdFromToolOutput(t.result));
      }
      for (final id in out.requestIds) {
        _trackRequest(id);
      }
      if (out.reply.isNotEmpty) {
        _append(
          TranscriptEntry(
            id: _nextId('asst'),
            speaker: Speaker.assistant,
            text: out.reply,
          ),
        );
      }
    } on ApiException catch (e) {
      if (!ref.mounted) return;
      _system('The assistant could not answer: ${e.message}', error: true);
    } finally {
      if (ref.mounted) state = state.copyWith(sending: false);
    }
  }

  // ------------------------------------------------------------ voice

  /// Mints an ephemeral Realtime session on the backend and connects.
  Future<void> startVoice() async {
    if (state.voiceStatus == VoiceStatus.connecting || voiceLive) return;
    state = state.copyWith(
      voiceStatus: VoiceStatus.connecting,
      muted: false,
      error: () => null,
    );
    RealtimeTransport? transport;
    try {
      final session = await _api.realtimeSession();
      if (!ref.mounted) return;
      transport = ref.read(realtimeTransportFactoryProvider)();
      _transport = transport;
      _voiceSessionId =
          'rt-${DateTime.now().toUtc().millisecondsSinceEpoch.toRadixString(36)}';
      _seenCalls.clear();
      _pendingTools = 0;
      _responseActive = false;
      _needsResponse = false;
      _sub = transport.events.listen(
        handleServerEvent,
        onError: (Object e) => _onTransportError(e.toString()),
      );
      await transport.connect(session);
      if (!ref.mounted) return;
      // Ask for input transcription so the user's words show in the transcript.
      transport.send({
        'type': 'session.update',
        'session': {
          'type': 'realtime',
          'audio': {
            'input': {
              'transcription': {'model': 'gpt-4o-mini-transcribe'},
            },
          },
        },
      });
      state = state.copyWith(voiceStatus: VoiceStatus.listening);
      _system('Voice connected. Speak naturally; I’ll read back the order.');
    } catch (e) {
      final msg = e is ApiException ? e.message : e.toString();
      await _teardown();
      if (!ref.mounted) return;
      state = state.copyWith(voiceStatus: VoiceStatus.error, error: () => msg);
    }
  }

  Future<void> stopVoice() async {
    await _teardown();
    if (!ref.mounted) return;
    state = state.copyWith(voiceStatus: VoiceStatus.idle, muted: false);
    _system('Voice session ended.');
  }

  void toggleMute() {
    if (!voiceLive) return;
    final muted = !state.muted;
    _transport?.setMuted(muted);
    state = state.copyWith(muted: muted);
  }

  Future<void> _teardown() async {
    final sub = _sub;
    final t = _transport;
    _sub = null;
    _transport = null;
    // Not awaited: a cancelled broadcast subscription's future may complete
    // in another zone, and nothing depends on it.
    unawaited(sub?.cancel());
    await t?.close();
  }

  void _onTransportError(String message) {
    if (!ref.mounted) return;
    state = state.copyWith(
      voiceStatus: VoiceStatus.error,
      error: () => message,
    );
  }

  void _setStatus(VoiceStatus s) {
    if (state.voiceStatus != s) state = state.copyWith(voiceStatus: s);
  }

  /// Handles one OpenAI Realtime server event (GA and beta names).
  @visibleForTesting
  void handleServerEvent(Json e) {
    if (!ref.mounted) return;
    final type = e['type'] as String? ?? '';
    switch (type) {
      case 'input_audio_buffer.speech_started':
        _setStatus(VoiceStatus.userSpeaking);
      case 'input_audio_buffer.speech_stopped':
        _setStatus(VoiceStatus.thinking);
      case 'input_audio_buffer.committed':
        // Reserve the slot so the user's words appear before the reply.
        final itemId = e['item_id'] as String?;
        if (itemId != null) {
          _upsert(
            'u:$itemId',
            (old) =>
                old ??
                TranscriptEntry(
                  id: 'u:$itemId',
                  speaker: Speaker.user,
                  text: '',
                  partial: true,
                  voice: true,
                ),
          );
        }
      case 'conversation.item.input_audio_transcription.delta':
        final itemId = e['item_id'] as String? ?? '';
        final delta = e['delta'] as String? ?? '';
        _upsert(
          'u:$itemId',
          (old) =>
              (old ??
                      TranscriptEntry(
                        id: 'u:$itemId',
                        speaker: Speaker.user,
                        text: '',
                        partial: true,
                        voice: true,
                      ))
                  .copyWith(text: '${old?.text ?? ''}$delta', partial: true),
        );
      case 'conversation.item.input_audio_transcription.completed':
        final itemId = e['item_id'] as String? ?? '';
        final text = (e['transcript'] as String? ?? '').trim();
        if (text.isEmpty) {
          _remove('u:$itemId');
        } else {
          _upsert(
            'u:$itemId',
            (old) =>
                (old ??
                        TranscriptEntry(
                          id: 'u:$itemId',
                          speaker: Speaker.user,
                          text: '',
                          voice: true,
                        ))
                    .copyWith(text: text, partial: false),
          );
        }
      case 'conversation.item.input_audio_transcription.failed':
        final itemId = e['item_id'] as String? ?? '';
        _upsert(
          'u:$itemId',
          (old) => TranscriptEntry(
            id: 'u:$itemId',
            speaker: Speaker.user,
            text: '(could not transcribe)',
            voice: true,
          ),
        );
      case 'response.created':
        _responseActive = true;
        _setStatus(VoiceStatus.thinking);
      case 'response.output_audio_transcript.delta' ||
          'response.audio_transcript.delta' ||
          'response.output_text.delta' ||
          'response.text.delta':
        final itemId = e['item_id'] as String? ?? 'resp';
        final delta = e['delta'] as String? ?? '';
        _setStatus(VoiceStatus.speaking);
        _upsert(
          'a:$itemId',
          (old) =>
              (old ??
                      TranscriptEntry(
                        id: 'a:$itemId',
                        speaker: Speaker.assistant,
                        text: '',
                        partial: true,
                        voice: true,
                      ))
                  .copyWith(text: '${old?.text ?? ''}$delta', partial: true),
        );
      case 'response.output_audio_transcript.done' ||
          'response.audio_transcript.done' ||
          'response.output_text.done' ||
          'response.text.done':
        final itemId = e['item_id'] as String? ?? 'resp';
        final full = (e['transcript'] ?? e['text']) as String?;
        _upsert(
          'a:$itemId',
          (old) =>
              (old ??
                      TranscriptEntry(
                        id: 'a:$itemId',
                        speaker: Speaker.assistant,
                        text: '',
                        voice: true,
                      ))
                  .copyWith(
                    text: (full != null && full.isNotEmpty) ? full : old?.text,
                    partial: false,
                  ),
        );
      case 'response.function_call_arguments.done':
        final name = e['name'] as String?;
        if (name != null) {
          _onFunctionCall(e['call_id'] as String?, name, e['arguments']);
        }
      case 'response.output_item.done':
        final item = e['item'];
        if (item is Map && item['type'] == 'function_call') {
          _onFunctionCall(
            item['call_id'] as String?,
            item['name'] as String? ?? '',
            item['arguments'],
          );
        }
      case 'response.done':
        _responseActive = false;
        final resp = e['response'];
        final output = resp is Map ? resp['output'] : null;
        if (output is List) {
          for (final item in output) {
            if (item is Map && item['type'] == 'function_call') {
              _onFunctionCall(
                item['call_id'] as String?,
                item['name'] as String? ?? '',
                item['arguments'],
              );
            }
          }
        }
        if (_pendingTools == 0 && !_needsResponse) {
          _setStatus(VoiceStatus.listening);
        }
        _requestResponse();
      case 'output_audio_buffer.stopped':
        if (_pendingTools == 0 && !_responseActive) {
          _setStatus(VoiceStatus.listening);
        }
      case 'error':
        final err = e['error'];
        if (err is Map &&
            err['code'] == 'conversation_already_has_active_response') {
          // Our response.create raced a reply the server started itself
          // (e.g. from voice activity): retry once that reply is done.
          _responseActive = true;
          _needsResponse = true;
          return;
        }
        final msg = err is Map ? err['message'] as String? : null;
        _system(msg ?? 'The voice service reported an error.', error: true);
      case transportClosedEvent:
        _teardown();
        state = state.copyWith(voiceStatus: VoiceStatus.idle, muted: false);
        _system('Voice connection closed.');
      default:
        break;
    }
  }

  void _onFunctionCall(String? callId, String name, Object? rawArgs) {
    if (callId == null || name.isEmpty || !_seenCalls.add(callId)) return;
    _pendingTools++;
    _setStatus(VoiceStatus.thinking);
    unawaited(_runTool(callId, name, rawArgs));
  }

  Future<void> _runTool(String callId, String name, Object? rawArgs) async {
    final entryId = 't:$callId';
    _append(
      TranscriptEntry(
        id: entryId,
        speaker: Speaker.tool,
        toolName: name,
        text: 'Running $name…',
        partial: true,
        voice: true,
      ),
    );
    Object? args = rawArgs;
    if (rawArgs is String) {
      try {
        args = rawArgs.isEmpty ? <String, dynamic>{} : jsonDecode(rawArgs);
      } on FormatException {
        args = rawArgs; // the backend accepts a JSON-encoded string too
      }
    }
    Json output;
    var failed = false;
    try {
      output = await _api.executeTool(
        name,
        args,
        callId: callId,
        sessionId: _voiceSessionId,
      );
    } on ApiException catch (e) {
      output = {'error': e.message};
      failed = true;
    }
    if (!ref.mounted) return;
    _upsert(
      entryId,
      (old) => old!.copyWith(
        text: failed
            ? '$name failed: ${output['error']}'
            : describeTool(name, output),
        partial: false,
        isError: failed || output['error'] != null,
      ),
    );
    _trackRequest(requestIdFromToolOutput(output));
    _transport?.send({
      'type': 'conversation.item.create',
      'item': {
        'type': 'function_call_output',
        'call_id': callId,
        'output': jsonEncode(output),
      },
    });
    _pendingTools--;
    _needsResponse = true;
    _requestResponse();
  }

  /// Asks the model to continue once every tool output is in and no response
  /// is active (sending `response.create` mid-response is rejected).
  void _requestResponse({bool force = false}) {
    if (force) _needsResponse = true;
    if (!_needsResponse || _responseActive || _pendingTools > 0) return;
    _needsResponse = false;
    _responseActive = true;
    _transport?.send({'type': 'response.create'});
  }
}

final playgroundProvider =
    NotifierProvider<PlaygroundController, PlaygroundState>(
      PlaygroundController.new,
    );
