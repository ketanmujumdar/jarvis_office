import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:jarvis_office/core/api/models.dart';
import 'package:jarvis_office/core/format.dart';
import 'package:jarvis_office/core/providers.dart';

SseEvent _ev(int id, String? requestId) => SseEvent(
  id: id,
  type: SseTypes.requestStatusChanged,
  requestId: requestId,
  at: DateTime.utc(2026, 10, 9),
  data: const {},
);

void main() {
  test('per-request event streams filter the one shared connection', () async {
    var listens = 0, cancels = 0;
    final hub = StreamController<SseEvent>.broadcast(
      onListen: () => listens++,
      onCancel: () => cancels++,
    );
    final c = ProviderContainer(
      overrides: [sseHubProvider.overrideWithValue(hub.stream)],
    );
    addTearDown(c.dispose);
    addTearDown(hub.close);

    final r1 = <int>[], all = <int>[];
    final s1 = c.listen(eventStreamProvider('r1'), (_, n) {
      if (n.hasValue) r1.add(n.value!.id);
    });
    final s2 = c.listen(eventStreamProvider(null), (_, n) {
      if (n.hasValue) all.add(n.value!.id);
    });
    await Future<void>.delayed(Duration.zero);
    hub
      ..add(_ev(1, 'r1'))
      ..add(_ev(2, 'r2'))
      ..add(_ev(3, 'r1'));
    await Future<void>.delayed(Duration.zero);
    expect(r1, [1, 3]);
    expect(all, [1, 2, 3]);
    expect(listens, 1, reason: 'one connection shared by every listener');

    // Auto-dispose: when the last listener goes, the subscription is dropped.
    s1.close();
    s2.close();
    await Future<void>.delayed(const Duration(milliseconds: 10));
    expect(cancels, 1);
  });

  test('compact money formats zero without decimals', () {
    expect(Fmt.moneyCompact(0), r'S$0');
  });
}
