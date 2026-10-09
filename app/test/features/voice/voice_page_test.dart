import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:jarvis_office/core/api/api_client.dart';
import 'package:jarvis_office/core/api/models.dart';
import 'package:jarvis_office/features/voice/voice_page.dart';

import '../../helpers.dart';
import '../request/fakes.dart';

void main() {
  late Harness h;
  setUp(() => h = Harness());
  tearDown(() => h.dispose());

  Future<void> pumpPage(
    WidgetTester tester, {
    Size size = const Size(1400, 1600),
  }) async {
    setSurface(tester, size);
    await tester.pumpWidget(h.wrap(const VoicePage()));
    await tester.pumpAndSettle();
  }

  testWidgets('empty state offers suggestions and no request yet', (
    tester,
  ) async {
    await pumpPage(tester);
    expect(find.text('Talk or type to restock the office.'), findsOneWidget);
    expect(find.text('What does the office need?'), findsOneWidget);
    expect(find.text(playgroundSuggestions.first), findsOneWidget);
    expect(find.text('No request yet'), findsOneWidget);
    expect(find.text('Tap to talk'), findsOneWidget);
  });

  testWidgets('typing sends to the text agent and shows the live request', (
    tester,
  ) async {
    h.api.details['r1'] = sampleDetail();
    h.api.chatReply = ChatOutput(
      sessionId: 's1',
      reply: 'Found 2 items. Which address?',
      requestIds: const ['r1'],
      toolCalls: [
        ToolCallTrace.fromJson({
          'name': 'create_order_request',
          'result': {'request_id': 'r1'},
        }),
      ],
    );
    await pumpPage(tester);

    await tester.enterText(
      find.byKey(const ValueKey('composer')),
      'restock coffee',
    );
    await tester.tap(find.byKey(const ValueKey('send')));
    await tester.pumpAndSettle();

    expect(h.api.chatCalls.single.$1, 'restock coffee');
    expect(find.text('restock coffee'), findsOneWidget);
    expect(find.text('Created a purchase request'), findsOneWidget);
    expect(find.text('Found 2 items. Which address?'), findsOneWidget);
    // Workspace: parsed items, quotes, address picker.
    expect(find.text('Live request'), findsOneWidget);
    expect(find.text('Line items'), findsOneWidget);
    expect(find.text('Best price'), findsNWidgets(2));
    expect(find.text('Deliver to'), findsOneWidget);
  });

  testWidgets('tapping a suggestion sends it', (tester) async {
    await pumpPage(tester);
    await tester.tap(find.text(playgroundSuggestions[1]));
    await tester.pumpAndSettle();
    expect(h.api.chatCalls.single.$1, playgroundSuggestions[1]);
  });

  testWidgets('mic starts a voice session and relays a tool call', (
    tester,
  ) async {
    h.api.details['r1'] = sampleDetail();
    h.api.toolResult = (_, _) => {'request_id': 'r1'};
    await pumpPage(tester);

    await tester.tap(find.byKey(const ValueKey('mic-orb')));
    await tester.pump();
    await tester.pump();
    expect(h.transport.connectedWith, isNotNull);
    expect(find.text('Listening'), findsOneWidget);
    expect(find.byTooltip('Mute'), findsOneWidget);

    h.transport.emit({
      'type': 'response.function_call_arguments.done',
      'call_id': 'c1',
      'name': 'create_order_request',
      'arguments': '{"utterance":"coffee"}',
    });
    await tester.pump();
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(h.api.toolCalls.single.callId, 'c1');
    expect(find.text('Created a purchase request'), findsOneWidget);
    expect(find.text('Live request'), findsOneWidget);

    // Stop the session (the orb animation stops too).
    await tester.tap(find.byKey(const ValueKey('mic-orb')));
    await tester.pumpAndSettle();
    expect(find.text('Tap to talk'), findsOneWidget);
    expect(h.transport.closed, isTrue);
  });

  testWidgets('voice failure shows a banner and keeps text working', (
    tester,
  ) async {
    h.api.sessionError = const ApiException(
      statusCode: 502,
      code: 'upstream_error',
      message: 'Realtime unavailable',
    );
    await pumpPage(tester);
    await tester.tap(find.byKey(const ValueKey('mic-orb')));
    await tester.pumpAndSettle();
    expect(
      find.text('Realtime unavailable — you can keep typing instead.'),
      findsOneWidget,
    );
    expect(find.text('Voice unavailable'), findsOneWidget);
    await tester.tap(find.byTooltip('Dismiss'));
    await tester.pumpAndSettle();
    expect(find.textContaining('Realtime unavailable —'), findsNothing);
  });

  testWidgets('narrow layout stacks without overflow', (tester) async {
    h.api.details['r1'] = sampleDetail();
    h.api.chatReply = const ChatOutput(
      sessionId: 's1',
      reply: 'Done',
      requestIds: ['r1'],
      toolCalls: [],
    );
    await pumpPage(tester, size: const Size(390, 3600));
    await tester.enterText(find.byKey(const ValueKey('composer')), 'coffee');
    await tester.tap(find.byKey(const ValueKey('send')));
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    expect(find.text('Line items'), findsOneWidget);
  });
}
