import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:jarvis_office/core/api/api_client.dart';
import 'package:jarvis_office/core/api/models.dart';
import 'package:jarvis_office/features/voice/voice_controller.dart';

import '../request/fakes.dart';

void main() {
  late Harness h;
  setUp(() => h = Harness());
  tearDown(() => h.dispose());

  Future<void> flush() => Future<void>.delayed(Duration.zero);

  group('requestIdFromToolOutput', () {
    final cases = <(Object?, String?)>[
      (null, null),
      ('x', null),
      ({'request_id': 'r1'}, 'r1'),
      ({'request_id': ''}, null),
      (
        {
          'request': {'id': 'r2'},
        },
        'r2',
      ),
      (
        {
          'request_ids': ['a', 'b'],
        },
        'b',
      ),
      ({'error': 'nope'}, null),
    ];
    for (final (input, want) in cases) {
      test('$input -> $want', () {
        expect(requestIdFromToolOutput(input), want);
      });
    }
  });

  group('describeTool', () {
    final cases = <(String, Object?, String)>[
      (
        'create_order_request',
        {'request_id': 'r1'},
        'Created a purchase request',
      ),
      (
        'get_request_status',
        {
          'request': {'status': 'quoted'},
        },
        'Checked status: Ready to confirm',
      ),
      (
        'list_addresses',
        {
          'addresses': [1, 2, 3],
        },
        'Looked up 3 delivery addresses',
      ),
      ('confirm_order', {}, 'Confirmed the order'),
      ('cancel_request', {}, 'Cancelled the request'),
      (
        'confirm_order',
        {'error': 'no address'},
        'confirm_order failed: no address',
      ),
      ('mystery', {}, 'Ran mystery'),
    ];
    for (final (name, out, want) in cases) {
      test('$name $out', () => expect(describeTool(name, out), want));
    }
  });

  test('text fallback uses the text agent and tracks requests', () async {
    final c = h.container();
    h.api.chatReply = ChatOutput(
      sessionId: 'chat-1',
      reply: 'I found prices for 2 items.',
      requestIds: const ['r9'],
      toolCalls: [
        ToolCallTrace.fromJson({
          'name': 'create_order_request',
          'result': {'request_id': 'r9'},
        }),
      ],
    );
    final ctl = c.read(playgroundProvider.notifier);
    await ctl.sendText('  restock coffee  ');

    final s = c.read(playgroundProvider);
    expect(h.api.chatCalls, [('restock coffee', null)]);
    expect(s.transcript.map((e) => (e.speaker, e.text)), [
      (Speaker.user, 'restock coffee'),
      (Speaker.tool, 'Created a purchase request'),
      (Speaker.assistant, 'I found prices for 2 items.'),
    ]);
    expect(s.activeRequestId, 'r9');
    expect(s.requestIds, ['r9']);
    expect(s.chatSessionId, 'chat-1');
    expect(s.sending, isFalse);

    // The session id is reused on the next turn.
    await ctl.sendText('yes, Marina One');
    expect(h.api.chatCalls.last, ('yes, Marina One', 'chat-1'));
  });

  test('text agent errors become a system line', () async {
    final c = h.container();
    h.api.chatError = const ApiException(
      statusCode: 502,
      code: 'upstream_error',
      message: 'LLM down',
    );
    await c.read(playgroundProvider.notifier).sendText('hi');
    final last = c.read(playgroundProvider).transcript.last;
    expect(last.speaker, Speaker.system);
    expect(last.isError, isTrue);
    expect(last.text, contains('LLM down'));
  });

  test('empty messages are ignored', () async {
    final c = h.container();
    await c.read(playgroundProvider.notifier).sendText('   ');
    expect(h.api.chatCalls, isEmpty);
    expect(c.read(playgroundProvider).transcript, isEmpty);
  });

  test(
    'startVoice mints a session, connects and enables transcription',
    () async {
      final c = h.container();
      await c.read(playgroundProvider.notifier).startVoice();
      final s = c.read(playgroundProvider);
      expect(h.api.sessionMints, 1);
      expect(h.transport.connectedWith?.clientSecret, 'ek_test');
      expect(s.voiceStatus, VoiceStatus.listening);
      expect(h.transport.sentTypes, ['session.update']);
    },
  );

  test('startVoice failure shows an error and stays usable for text', () async {
    final c = h.container();
    h.api.sessionError = const ApiException(
      statusCode: 502,
      code: 'upstream_error',
      message: 'OpenAI unavailable',
    );
    await c.read(playgroundProvider.notifier).startVoice();
    final s = c.read(playgroundProvider);
    expect(s.voiceStatus, VoiceStatus.error);
    expect(s.error, 'OpenAI unavailable');

    await c.read(playgroundProvider.notifier).sendText('hello');
    expect(h.api.chatCalls, hasLength(1));
  });

  test('transport connect failure closes the transport', () async {
    final c = h.container();
    h.transport.connectError = StateError('mic denied');
    await c.read(playgroundProvider.notifier).startVoice();
    expect(c.read(playgroundProvider).voiceStatus, VoiceStatus.error);
    expect(h.transport.closed, isTrue);
  });

  test('voice transcripts stream in order', () async {
    final c = h.container();
    final ctl = c.read(playgroundProvider.notifier);
    await ctl.startVoice();
    final t = h.transport;

    t.emit({'type': 'input_audio_buffer.speech_started'});
    expect(c.read(playgroundProvider).voiceStatus, VoiceStatus.userSpeaking);
    t.emit({'type': 'input_audio_buffer.committed', 'item_id': 'u1'});
    t.emit({'type': 'response.created'});
    t.emit({
      'type': 'response.output_audio_transcript.delta',
      'item_id': 'a1',
      'delta': 'Sure, ',
    });
    expect(c.read(playgroundProvider).voiceStatus, VoiceStatus.speaking);
    t.emit({
      'type': 'response.output_audio_transcript.delta',
      'item_id': 'a1',
      'delta': 'checking.',
    });
    // The user transcript arrives late but keeps its slot before the reply.
    t.emit({
      'type': 'conversation.item.input_audio_transcription.completed',
      'item_id': 'u1',
      'transcript': 'Order two coffee bags',
    });
    t.emit({
      'type': 'response.output_audio_transcript.done',
      'item_id': 'a1',
      'transcript': 'Sure, checking prices.',
    });
    t.emit({
      'type': 'response.done',
      'response': {'output': []},
    });

    final s = c.read(playgroundProvider);
    final convo = s.transcript.where((e) => e.speaker != Speaker.system);
    expect(convo.map((e) => (e.speaker, e.text, e.partial)), [
      (Speaker.user, 'Order two coffee bags', false),
      (Speaker.assistant, 'Sure, checking prices.', false),
    ]);
    expect(s.voiceStatus, VoiceStatus.listening);
    // No tool calls, so no extra response.create.
    expect(t.sentTypes, ['session.update']);
  });

  test('empty user transcription is dropped', () async {
    final c = h.container();
    await c.read(playgroundProvider.notifier).startVoice();
    h.transport.emit({'type': 'input_audio_buffer.committed', 'item_id': 'u1'});
    h.transport.emit({
      'type': 'conversation.item.input_audio_transcription.completed',
      'item_id': 'u1',
      'transcript': ' ',
    });
    expect(
      c
          .read(playgroundProvider)
          .transcript
          .where((e) => e.speaker == Speaker.user),
      isEmpty,
    );
  });

  test('tool calls are relayed to the backend and answered once', () async {
    final c = h.container();
    final ctl = c.read(playgroundProvider.notifier);
    await ctl.startVoice();
    final t = h.transport;
    h.api.toolResult = (name, args) => {
      'request_id': 'r42',
      'status': 'parsing',
    };
    final gate = h.api.toolGate = Completer<void>();

    t.emit({'type': 'response.created'});
    t.emit({
      'type': 'response.function_call_arguments.done',
      'call_id': 'call_1',
      'name': 'create_order_request',
      'arguments': '{"utterance":"two coffee bags"}',
    });
    // The same call reported again (output_item.done + response.done) is deduped.
    t.emit({
      'type': 'response.output_item.done',
      'item': {
        'type': 'function_call',
        'call_id': 'call_1',
        'name': 'create_order_request',
        'arguments': '{"utterance":"two coffee bags"}',
      },
    });
    t.emit({
      'type': 'response.done',
      'response': {
        'output': [
          {
            'type': 'function_call',
            'call_id': 'call_1',
            'name': 'create_order_request',
            'arguments': '{}',
          },
        ],
      },
    });
    expect(c.read(playgroundProvider).voiceStatus, VoiceStatus.thinking);
    final running = c.read(playgroundProvider).transcript.last;
    expect(running.speaker, Speaker.tool);
    expect(running.partial, isTrue);

    gate.complete();
    await flush();
    await flush();

    expect(h.api.toolCalls, hasLength(1));
    final call = h.api.toolCalls.single;
    expect(call.name, 'create_order_request');
    expect(call.callId, 'call_1');
    expect(call.args, {'utterance': 'two coffee bags'});

    expect(t.sentTypes, [
      'session.update',
      'conversation.item.create',
      'response.create',
    ]);
    final item = t.sent[1]['item'] as Map;
    expect(item['type'], 'function_call_output');
    expect(item['call_id'], 'call_1');
    expect(jsonDecode(item['output'] as String), {
      'request_id': 'r42',
      'status': 'parsing',
    });

    final s = c.read(playgroundProvider);
    expect(s.activeRequestId, 'r42');
    expect(s.transcript.last.text, 'Created a purchase request');
    expect(s.transcript.last.partial, isFalse);
  });

  test('response.create waits for the active response to finish', () async {
    final c = h.container();
    await c.read(playgroundProvider.notifier).startVoice();
    final t = h.transport;

    t.emit({'type': 'response.created'});
    t.emit({
      'type': 'response.function_call_arguments.done',
      'call_id': 'c1',
      'name': 'list_addresses',
      'arguments': '',
    });
    await flush();
    await flush();
    // Output is sent, but the response is still active: no response.create yet.
    expect(t.sentTypes, ['session.update', 'conversation.item.create']);
    expect(h.api.toolCalls.single.args, <String, dynamic>{});

    t.emit({'type': 'response.done', 'response': {}});
    expect(t.sentTypes.last, 'response.create');
  });

  test('tool HTTP errors are returned to the model as output.error', () async {
    final c = h.container();
    await c.read(playgroundProvider.notifier).startVoice();
    h.api.toolResult = (_, _) => throw const ApiException(
      statusCode: 404,
      code: 'not_found',
      message: 'unknown tool',
    );
    h.transport.emit({
      'type': 'response.function_call_arguments.done',
      'call_id': 'c1',
      'name': 'bogus',
      'arguments': '{}',
    });
    await flush();
    await flush();
    final out = h.transport.sent.firstWhere(
      (e) => e['type'] == 'conversation.item.create',
    );
    expect(jsonDecode((out['item'] as Map)['output'] as String), {
      'error': 'unknown tool',
    });
    expect(c.read(playgroundProvider).transcript.last.isError, isTrue);
  });

  test(
    'typing while voice is live goes into the realtime conversation',
    () async {
      final c = h.container();
      final ctl = c.read(playgroundProvider.notifier);
      await ctl.startVoice();
      await ctl.sendText('Use the one-north address');
      expect(h.api.chatCalls, isEmpty);
      expect(h.transport.sentTypes, [
        'session.update',
        'conversation.item.create',
        'response.create',
      ]);
      final content =
          ((h.transport.sent[1]['item'] as Map)['content'] as List).single
              as Map;
      expect(content, {
        'type': 'input_text',
        'text': 'Use the one-north address',
      });
    },
  );

  test('mute, stop and transport close', () async {
    final c = h.container();
    final ctl = c.read(playgroundProvider.notifier);
    await ctl.startVoice();
    ctl.toggleMute();
    expect(h.transport.muted, isTrue);
    expect(c.read(playgroundProvider).muted, isTrue);

    h.transport.emit({'type': 'transport.closed'});
    await flush();
    expect(c.read(playgroundProvider).voiceStatus, VoiceStatus.idle);
    expect(h.transport.closed, isTrue);
  });

  test('server error events are shown', () async {
    final c = h.container();
    await c.read(playgroundProvider.notifier).startVoice();
    h.transport.emit({
      'type': 'error',
      'error': {'message': 'conversation_already_has_active_response'},
    });
    final last = c.read(playgroundProvider).transcript.last;
    expect(last.isError, isTrue);
    expect(last.text, 'conversation_already_has_active_response');
  });

  test('reset clears the conversation and closes voice', () async {
    final c = h.container();
    final ctl = c.read(playgroundProvider.notifier);
    await ctl.startVoice();
    await ctl.reset();
    expect(h.transport.closed, isTrue);
    expect(c.read(playgroundProvider).transcript, isEmpty);
    expect(c.read(playgroundProvider).voiceStatus, VoiceStatus.idle);
  });

  group('narration', () {
    Map<String, dynamic> lastSystemMessage(List<Json> sent) {
      final m = sent.lastWhere(
        (e) =>
            e['type'] == 'conversation.item.create' &&
            (e['item'] as Map)['role'] == 'system',
      );
      return ((m['item'] as Map)['content'] as List).single
          as Map<String, dynamic>;
    }

    Future<void> startWithRequest(dynamic c) async {
      h.api.toolResult = (_, _) => {'request_id': 'r1', 'status': 'parsing'};
      await c.read(playgroundProvider.notifier).startVoice();
      h.transport.emit({
        'type': 'response.function_call_arguments.done',
        'call_id': 'c0',
        'name': 'create_order_request',
        'arguments': '{"utterance":"paper"}',
      });
      await flush();
      await flush();
      h.transport.emit({'type': 'response.done', 'response': {}});
      h.transport.emit({'type': 'response.created'});
      h.transport.emit({'type': 'response.done', 'response': {}});
    }

    test('a status change is spoken without being asked', () async {
      final c = h.container();
      await startWithRequest(c);
      h.sse.add(
        sseEvent(1, 'request.status_changed', {
          'from': 'searching',
          'to': 'quoted',
        }),
      );
      await flush();
      expect(lastSystemMessage(h.transport.sent)['text'], contains('"quoted"'));
      expect(h.transport.sentTypes.last, 'response.create');
    });

    test(
      'an update during a tool call is held, then sent with its output',
      () async {
        final c = h.container();
        await startWithRequest(c);
        final gate = Completer<void>();
        h.api.toolGate = gate;
        h.api.toolResult = (_, _) => {
          'request': {'id': 'r1', 'status': 'checking_out'},
        };
        h.transport.emit({
          'type': 'response.function_call_arguments.done',
          'call_id': 'c1',
          'name': 'confirm_order',
          'arguments': '{"request_id":"r1","address_id":"a1"}',
        });
        await flush();
        h.sse.add(
          sseEvent(2, 'request.status_changed', {
            'from': 'checking_out',
            'to': 'failed',
            'failure_reason': 'out of stock',
          }),
        );
        await flush();
        final before = h.transport.sent.length;
        expect(
          h.transport.sent.where(
            (e) => (e['item'] as Map?)?['role'] == 'system',
          ),
          isEmpty,
        );
        gate.complete();
        await flush();
        await flush();
        expect(h.transport.sent.length, greaterThan(before));
        expect(
          lastSystemMessage(h.transport.sent)['text'],
          contains('out of stock'),
        );
      },
    );

    test('events for other requests are ignored', () async {
      final c = h.container();
      await startWithRequest(c);
      final n = h.transport.sent.length;
      h.sse.add(
        sseEvent(3, 'request.status_changed', {
          'to': 'quoted',
        }, requestId: 'other'),
      );
      await flush();
      expect(h.transport.sent.length, n);
    });
  });
}
