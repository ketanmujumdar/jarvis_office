import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/providers.dart';
import '../../../core/theme/tokens.dart';
import '../../../core/widgets/widgets.dart';

/// Shows whether the live event stream for [requestId] (null = all events) is
/// connected. Watching it also keeps the stream open while the page is shown.
class LiveBadge extends ConsumerWidget {
  const LiveBadge({super.key, this.requestId});
  final String? requestId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(eventStreamProvider(requestId));
    final offline = s.hasError && !s.isLoading;
    return Tooltip(
      message: offline
          ? 'Live updates paused. Use refresh to reload.'
          : 'Updates appear automatically.',
      child: StatusChip(
        key: const Key('live-badge'),
        label: offline ? 'Offline' : 'Live',
        tone: offline ? Tone.neutral : Tone.success,
        icon: offline ? Icons.cloud_off_rounded : Icons.bolt_rounded,
      ),
    );
  }
}
