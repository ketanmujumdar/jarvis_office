import 'package:flutter/material.dart';

import '../../../core/theme/tokens.dart';
import '../voice_controller.dart';

/// Scrolling chat transcript that sticks to the latest message.
class TranscriptView extends StatefulWidget {
  const TranscriptView({super.key, required this.entries, this.empty});

  final List<TranscriptEntry> entries;

  /// Shown when there are no entries yet.
  final Widget? empty;

  @override
  State<TranscriptView> createState() => _TranscriptViewState();
}

class _TranscriptViewState extends State<TranscriptView> {
  final _scroll = ScrollController();

  @override
  void didUpdateWidget(TranscriptView old) {
    super.didUpdateWidget(old);
    if (old.entries != widget.entries) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted || !_scroll.hasClients) return;
        _scroll.animateTo(
          _scroll.position.maxScrollExtent,
          duration: const Duration(milliseconds: 200),
          curve: Curves.easeOut,
        );
      });
    }
  }

  @override
  void dispose() {
    _scroll.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (widget.entries.isEmpty && widget.empty != null) return widget.empty!;
    return ListView.builder(
      controller: _scroll,
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpace.lg,
        vertical: AppSpace.md,
      ),
      itemCount: widget.entries.length,
      itemBuilder: (context, i) => _EntryView(entry: widget.entries[i]),
    );
  }
}

class _EntryView extends StatelessWidget {
  const _EntryView({required this.entry});
  final TranscriptEntry entry;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    switch (entry.speaker) {
      case Speaker.tool:
        final tone = entry.isError
            ? Tone.danger
            : entry.partial
            ? Tone.info
            : Tone.success;
        return Padding(
          padding: const EdgeInsets.symmetric(vertical: AppSpace.xs),
          child: Row(
            children: [
              const SizedBox(width: 40),
              Flexible(
                child: Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: AppSpace.md,
                    vertical: AppSpace.xs + 2,
                  ),
                  decoration: BoxDecoration(
                    color: jc.bg(tone),
                    borderRadius: AppRadius.pillAll,
                  ),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      if (entry.partial)
                        SizedBox(
                          width: 12,
                          height: 12,
                          child: CircularProgressIndicator(
                            strokeWidth: 1.5,
                            color: jc.fg(tone),
                          ),
                        )
                      else
                        Icon(
                          entry.isError
                              ? Icons.error_outline_rounded
                              : Icons.check_circle_outline_rounded,
                          size: 14,
                          color: jc.fg(tone),
                        ),
                      const SizedBox(width: AppSpace.sm),
                      Flexible(
                        child: Text(
                          entry.text,
                          style: context.tt.labelMedium?.copyWith(
                            color: jc.fg(tone),
                          ),
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ],
          ),
        );
      case Speaker.system:
        return Padding(
          padding: const EdgeInsets.symmetric(vertical: AppSpace.sm),
          child: Text(
            entry.text,
            textAlign: TextAlign.center,
            style: context.tt.labelSmall?.copyWith(
              color: entry.isError ? jc.danger : jc.textMuted,
            ),
          ),
        );
      case Speaker.user:
      case Speaker.assistant:
        final user = entry.speaker == Speaker.user;
        final bubble = Container(
          padding: const EdgeInsets.symmetric(
            horizontal: AppSpace.md + 2,
            vertical: AppSpace.sm + 2,
          ),
          decoration: BoxDecoration(
            color: user ? jc.brand : jc.surfaceMuted,
            borderRadius: BorderRadius.only(
              topLeft: const Radius.circular(AppRadius.lg),
              topRight: const Radius.circular(AppRadius.lg),
              bottomLeft: Radius.circular(
                user ? AppRadius.lg : AppRadius.sm / 2,
              ),
              bottomRight: Radius.circular(
                user ? AppRadius.sm / 2 : AppRadius.lg,
              ),
            ),
            border: user ? null : Border.all(color: jc.border),
          ),
          child: entry.text.isEmpty && entry.partial
              ? _TypingDots(color: user ? jc.surface : jc.textMuted)
              : Text(
                  entry.text,
                  style: context.tt.bodyMedium?.copyWith(
                    color: user ? Colors.white : jc.textPrimary,
                    height: 1.4,
                  ),
                ),
        );
        final avatar = Container(
          width: 30,
          height: 30,
          alignment: Alignment.center,
          decoration: BoxDecoration(
            shape: BoxShape.circle,
            gradient: LinearGradient(
              colors: [jc.brand, jc.info],
              begin: Alignment.topLeft,
              end: Alignment.bottomRight,
            ),
          ),
          child: const Icon(
            Icons.auto_awesome_rounded,
            size: 16,
            color: Colors.white,
          ),
        );
        return Padding(
          padding: const EdgeInsets.symmetric(vertical: AppSpace.xs + 1),
          child: Row(
            mainAxisAlignment: user
                ? MainAxisAlignment.end
                : MainAxisAlignment.start,
            crossAxisAlignment: CrossAxisAlignment.end,
            children: [
              if (!user) ...[avatar, const SizedBox(width: AppSpace.sm + 2)],
              if (user) const SizedBox(width: 48),
              Flexible(
                child: Column(
                  crossAxisAlignment: user
                      ? CrossAxisAlignment.end
                      : CrossAxisAlignment.start,
                  children: [
                    bubble,
                    if (entry.voice)
                      Padding(
                        padding: const EdgeInsets.only(top: AppSpace.xxs),
                        child: Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Icon(
                              Icons.mic_none_rounded,
                              size: 11,
                              color: jc.textMuted,
                            ),
                            const SizedBox(width: 2),
                            Text(
                              'voice',
                              style: context.tt.labelSmall?.copyWith(
                                color: jc.textMuted,
                                fontSize: 10,
                              ),
                            ),
                          ],
                        ),
                      ),
                  ],
                ),
              ),
              if (!user) const SizedBox(width: 48),
            ],
          ),
        );
    }
  }
}

class _TypingDots extends StatelessWidget {
  const _TypingDots({required this.color});
  final Color color;

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        for (var i = 0; i < 3; i++)
          Container(
            width: 6,
            height: 6,
            margin: const EdgeInsets.symmetric(horizontal: 2, vertical: 6),
            decoration: BoxDecoration(
              color: color.withValues(alpha: 0.4 + i * 0.2),
              shape: BoxShape.circle,
            ),
          ),
      ],
    );
  }
}
