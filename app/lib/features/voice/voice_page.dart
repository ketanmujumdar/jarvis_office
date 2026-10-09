import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/router.dart';
import '../../core/theme/tokens.dart';
import '../../core/widgets/widgets.dart';
import '../request/widgets/request_workspace.dart';
import 'voice_controller.dart';
import 'widgets/mic_orb.dart';
import 'widgets/transcript_view.dart';

/// Example requests offered before the first message.
const playgroundSuggestions = [
  'Restock the usual coffee beans and A4 paper',
  'We need 6 boxes of tissues and 2 dish soaps, urgent',
  'Order 3 USB-C chargers for the meeting rooms',
];

/// The manager playground: talk (OpenAI Realtime over WebRTC) or type to the
/// procurement agent, and watch the request it creates update live.
class VoicePage extends ConsumerWidget {
  const VoicePage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(playgroundProvider);
    final ctl = ref.read(playgroundProvider.notifier);
    final header = PageHeader(
      title: 'Assistant',
      subtitle: 'Talk or type to restock the office.',
      actions: [
        if (state.transcript.isNotEmpty || state.activeRequestId != null)
          OutlinedButton.icon(
            onPressed: ctl.reset,
            icon: const Icon(Icons.add_comment_outlined, size: 18),
            label: const Text('New conversation'),
          ),
      ],
    );
    const conversation = _ConversationCard();
    final workspace = _WorkspaceColumn(state: state);
    return LayoutBuilder(
      builder: (context, c) {
        // Desktop: the page itself does not scroll. The chat stays pinned with
        // its composer visible; only the live-request column scrolls.
        final wide = c.maxWidth >= 1080 && c.maxHeight >= 560;
        if (!wide) {
          return PageScaffold(
            maxWidth: 1400,
            header: header,
            children: [
              const SizedBox(height: 600, child: conversation),
              const SizedBox(height: AppSpace.lg),
              workspace,
            ],
          );
        }
        return Padding(
          padding: const EdgeInsets.fromLTRB(
            AppSpace.xxl,
            AppSpace.xxl,
            AppSpace.xxl,
            AppSpace.lg,
          ),
          child: Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 1400),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  header,
                  const SizedBox(height: AppSpace.xl),
                  Expanded(
                    child: Row(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        const Expanded(flex: 5, child: conversation),
                        const SizedBox(width: AppSpace.lg),
                        Expanded(
                          flex: 7,
                          child: SingleChildScrollView(
                            key: const ValueKey('workspace-scroll'),
                            padding: const EdgeInsets.only(bottom: AppSpace.xl),
                            child: workspace,
                          ),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ),
        );
      },
    );
  }
}

class _ConversationCard extends ConsumerStatefulWidget {
  const _ConversationCard();

  @override
  ConsumerState<_ConversationCard> createState() => _ConversationCardState();
}

class _ConversationCardState extends ConsumerState<_ConversationCard> {
  final _input = TextEditingController();
  final _focus = FocusNode();

  @override
  void dispose() {
    _input.dispose();
    _focus.dispose();
    super.dispose();
  }

  void _send([String? text]) {
    // The composer stays enabled while a reply is pending (disabling a focused
    // field trips the web text-editing engine); block double sends here.
    if (ref.read(playgroundProvider).sending) return;
    final msg = (text ?? _input.text).trim();
    if (msg.isEmpty) return;
    _input.clear();
    ref.read(playgroundProvider.notifier).sendText(msg);
    _focus.requestFocus();
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(playgroundProvider);
    final ctl = ref.read(playgroundProvider.notifier);
    final jc = context.jc;
    final status = state.voiceStatus;
    final live = status.isLive;

    return Material(
      color: jc.surface,
      shape: RoundedRectangleBorder(
        borderRadius: AppRadius.lgAll,
        side: BorderSide(color: jc.border),
      ),
      clipBehavior: Clip.antiAlias,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          // Voice header.
          Container(
            padding: const EdgeInsets.fromLTRB(
              AppSpace.lg,
              AppSpace.lg,
              AppSpace.lg,
              AppSpace.md,
            ),
            decoration: BoxDecoration(
              gradient: LinearGradient(
                begin: Alignment.topCenter,
                end: Alignment.bottomCenter,
                colors: [jc.brandSubtle, jc.surface],
              ),
              border: Border(bottom: BorderSide(color: jc.border)),
            ),
            child: Row(
              children: [
                MicOrb(
                  status: status,
                  size: 64,
                  onPressed: status == VoiceStatus.connecting
                      ? null
                      : live
                      ? ctl.stopVoice
                      : ctl.startVoice,
                ),
                const SizedBox(width: AppSpace.md),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text('Jarvis', style: context.tt.titleLarge),
                      const SizedBox(height: AppSpace.xxs),
                      Wrap(
                        spacing: AppSpace.sm,
                        runSpacing: AppSpace.xs,
                        crossAxisAlignment: WrapCrossAlignment.center,
                        children: [
                          StatusChip(
                            key: const ValueKey('voice-status'),
                            label: status.label,
                            tone: switch (status) {
                              VoiceStatus.error => Tone.danger,
                              VoiceStatus.idle => Tone.neutral,
                              VoiceStatus.connecting => Tone.info,
                              VoiceStatus.userSpeaking => Tone.success,
                              _ => Tone.brand,
                            },
                            pulse: live || status == VoiceStatus.connecting,
                          ),
                          Text(
                            live
                                ? 'Voice on · you can still type'
                                : 'Text mode',
                            style: context.tt.bodySmall,
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
                if (live)
                  IconButton.filledTonal(
                    tooltip: state.muted ? 'Unmute' : 'Mute',
                    onPressed: ctl.toggleMute,
                    icon: Icon(
                      state.muted ? Icons.mic_off_rounded : Icons.mic_rounded,
                    ),
                  ),
              ],
            ),
          ),
          if (state.error != null)
            Container(
              color: jc.dangerSubtle,
              padding: const EdgeInsets.symmetric(
                horizontal: AppSpace.lg,
                vertical: AppSpace.sm,
              ),
              child: Row(
                children: [
                  Icon(Icons.error_outline_rounded, size: 18, color: jc.danger),
                  const SizedBox(width: AppSpace.sm),
                  Expanded(
                    child: Text(
                      '${state.error} — you can keep typing instead.',
                      style: context.tt.bodySmall?.copyWith(color: jc.danger),
                    ),
                  ),
                  IconButton(
                    tooltip: 'Dismiss',
                    visualDensity: VisualDensity.compact,
                    onPressed: ctl.clearError,
                    icon: Icon(Icons.close_rounded, size: 16, color: jc.danger),
                  ),
                ],
              ),
            ),
          Expanded(
            child: TranscriptView(
              entries: state.transcript,
              empty: _Suggestions(onPick: _send),
            ),
          ),
          if (state.sending) const LinearProgressIndicator(minHeight: 2),
          // Composer.
          Container(
            padding: const EdgeInsets.all(AppSpace.md),
            decoration: BoxDecoration(
              border: Border(top: BorderSide(color: jc.border)),
            ),
            child: Row(
              children: [
                Expanded(
                  child: TextField(
                    key: const ValueKey('composer'),
                    controller: _input,
                    focusNode: _focus,
                    textInputAction: TextInputAction.send,
                    onSubmitted: (_) => _send(),
                    minLines: 1,
                    maxLines: 4,
                    decoration: InputDecoration(
                      hintText: live ? 'Type while talking…' : 'Ask Jarvis…',
                      hintMaxLines: 1,
                      isDense: true,
                    ),
                  ),
                ),
                const SizedBox(width: AppSpace.sm),
                IconButton.filled(
                  key: const ValueKey('send'),
                  tooltip: 'Send',
                  onPressed: state.sending ? null : _send,
                  icon: const Icon(Icons.arrow_upward_rounded),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _Suggestions extends StatelessWidget {
  const _Suggestions({required this.onPick});
  final void Function(String) onPick;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return SingleChildScrollView(
      padding: const EdgeInsets.all(AppSpace.xl),
      child: Column(
        children: [
          Icon(Icons.forum_outlined, size: 36, color: jc.brand),
          const SizedBox(height: AppSpace.md),
          Text(
            'What does the office need?',
            style: context.tt.titleMedium,
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: AppSpace.xs),
          Text(
            'Tap the mic to talk, or try one of these.',
            style: context.tt.bodySmall,
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: AppSpace.lg),
          ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 420),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                for (final s in playgroundSuggestions)
                  Padding(
                    padding: const EdgeInsets.only(bottom: AppSpace.sm),
                    child: OutlinedButton.icon(
                      style: OutlinedButton.styleFrom(
                        alignment: Alignment.centerLeft,
                        padding: const EdgeInsets.symmetric(
                          horizontal: AppSpace.md,
                          vertical: AppSpace.md,
                        ),
                      ),
                      onPressed: () => onPick(s),
                      icon: Icon(Icons.bolt_rounded, size: 16, color: jc.brand),
                      label: Text(
                        s,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        textAlign: TextAlign.left,
                      ),
                    ),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _WorkspaceColumn extends ConsumerWidget {
  const _WorkspaceColumn({required this.state});
  final PlaygroundState state;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final id = state.activeRequestId;
    if (id == null) {
      return const AppCard(
        child: EmptyState(
          icon: Icons.shopping_bag_outlined,
          title: 'No request yet',
          message:
              'When you ask for supplies, the parsed items, best prices across '
              'approved vendors and checkout status appear here live.',
        ),
      );
    }
    final ids = state.requestIds;
    final header = Row(
      children: [
        Expanded(
          child: Wrap(
            spacing: AppSpace.sm,
            runSpacing: AppSpace.xs,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              Text('Live request', style: context.tt.titleSmall),
              if (ids.length > 1)
                for (var i = 0; i < ids.length; i++)
                  ChoiceChip(
                    label: Text('#${i + 1}'),
                    selected: ids[i] == id,
                    onSelected: (_) => ref
                        .read(playgroundProvider.notifier)
                        .selectRequest(ids[i]),
                  ),
            ],
          ),
        ),
        TextButton.icon(
          onPressed: () => context.push(Routes.request(id)),
          icon: const Icon(Icons.open_in_full_rounded, size: 16),
          label: const Text('Full view'),
        ),
      ],
    );
    return RequestWorkspace(
      key: ValueKey('workspace-$id'),
      requestId: id,
      allowTwoColumns: false,
      header: header,
    );
  }
}
