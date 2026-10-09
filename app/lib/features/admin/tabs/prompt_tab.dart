import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api/models.dart';
import '../../../core/format.dart';
import '../../../core/providers.dart';
import '../../../core/theme/tokens.dart';
import '../../../core/widgets/widgets.dart';
import '../admin_common.dart';
import '../admin_providers.dart';

class PromptTab extends ConsumerWidget {
  const PromptTab({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return AsyncValueView<SystemPrompt>(
      value: ref.watch(systemPromptProvider),
      loading: const AdminSkeleton(rows: 1, rowHeight: 420),
      onRetry: () => ref.invalidate(systemPromptProvider),
      data: (p) => PromptEditor(prompt: p),
    );
  }
}

/// Large editor for the agent system prompt with save, discard and reset.
class PromptEditor extends ConsumerStatefulWidget {
  const PromptEditor({super.key, required this.prompt});
  final SystemPrompt prompt;

  @override
  ConsumerState<PromptEditor> createState() => _PromptEditorState();
}

class _PromptEditorState extends ConsumerState<PromptEditor> {
  late final _text = TextEditingController(text: widget.prompt.content);
  bool _busy = false;
  bool _defaultLoaded = false;

  @override
  void initState() {
    super.initState();
    _text.addListener(() => setState(() {}));
  }

  @override
  void didUpdateWidget(PromptEditor old) {
    super.didUpdateWidget(old);
    // A new server version arrived (after save): adopt it.
    if (old.prompt.version != widget.prompt.version) {
      _text.text = widget.prompt.content;
      _defaultLoaded = false;
    }
  }

  @override
  void dispose() {
    _text.dispose();
    super.dispose();
  }

  bool get _dirty => _text.text != widget.prompt.content;
  bool get _empty => _text.text.trim().isEmpty;

  Future<void> _save() async {
    if (_busy || !_dirty || _empty) return;
    setState(() => _busy = true);
    final ok = await runMutation(
      context,
      () => ref.read(apiProvider).updateSystemPrompt(_text.text),
      success: 'Prompt saved. New agent sessions use it right away.',
    );
    if (!mounted) return;
    setState(() => _busy = false);
    if (ok) ref.invalidate(systemPromptProvider);
  }

  void _discard() => setState(() {
    _text.text = widget.prompt.content;
    _defaultLoaded = false;
  });

  Future<void> _resetToDefault() async {
    final yes = await confirmAction(
      context,
      title: 'Reset to the default prompt?',
      message:
          'This replaces the text in the editor with the default prompt that '
          'ships with Jarvis. Nothing changes until you save.',
      confirmLabel: 'Load default',
    );
    if (!yes || !mounted) return;
    setState(() => _busy = true);
    try {
      final content = await ref.read(apiProvider).defaultSystemPrompt();
      if (!mounted) return;
      if (content.trim().isEmpty) {
        showToast(context, 'The server has no default prompt.', error: true);
      } else {
        _text.text = content;
        _defaultLoaded = true;
      }
    } catch (e) {
      if (mounted) showToast(context, errorMessage(e), error: true);
    }
    if (mounted) setState(() => _busy = false);
  }

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final p = widget.prompt;
    final words = RegExp(r'\S+').allMatches(_text.text).length;
    final meta = Wrap(
      spacing: AppSpace.sm,
      runSpacing: AppSpace.sm,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: [
        StatusChip(
          key: const ValueKey('prompt-version'),
          label: 'Version ${p.version}',
          tone: Tone.brand,
          icon: Icons.history_rounded,
        ),
        Tooltip(
          message: Fmt.dateTime(p.updatedAt),
          child: Text(
            'Updated ${Fmt.relative(p.updatedAt)}'
            '${p.updatedBy.isEmpty ? '' : ' by ${p.updatedBy}'}',
            style: context.tt.bodySmall,
          ),
        ),
        if (_dirty)
          const StatusChip(label: 'Unsaved changes', tone: Tone.warning),
      ],
    );

    final editor = AppCard(
      padding: EdgeInsets.zero,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Container(
            color: jc.surfaceMuted,
            padding: const EdgeInsets.symmetric(
              horizontal: AppSpace.lg,
              vertical: AppSpace.sm + 2,
            ),
            child: Row(
              children: [
                Icon(Icons.description_outlined, size: 16, color: jc.textMuted),
                const SizedBox(width: AppSpace.sm),
                Expanded(
                  child: Text(
                    'system_prompt · ${p.key.isEmpty ? 'agent' : p.key}',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: context.tt.labelMedium?.copyWith(
                      color: jc.textSecondary,
                    ),
                  ),
                ),
                const SizedBox(width: AppSpace.sm),
                Text(
                  '$words words · ${_text.text.length} chars',
                  style: context.tt.labelSmall,
                ),
              ],
            ),
          ),
          const Divider(),
          if (_defaultLoaded)
            Container(
              color: jc.infoSubtle,
              padding: const EdgeInsets.symmetric(
                horizontal: AppSpace.lg,
                vertical: AppSpace.sm,
              ),
              child: Row(
                children: [
                  Icon(Icons.info_outline_rounded, size: 16, color: jc.info),
                  const SizedBox(width: AppSpace.sm),
                  Expanded(
                    child: Text(
                      'Default prompt loaded. Save to apply it.',
                      style: context.tt.bodySmall?.copyWith(color: jc.info),
                    ),
                  ),
                ],
              ),
            ),
          CallbackShortcuts(
            bindings: {
              const SingleActivator(LogicalKeyboardKey.keyS, control: true):
                  _save,
              const SingleActivator(LogicalKeyboardKey.keyS, meta: true): _save,
            },
            child: TextField(
              key: const ValueKey('prompt-editor'),
              controller: _text,
              minLines: 22,
              maxLines: 34,
              style: context.tt.bodyMedium?.copyWith(
                fontFamily: 'monospace',
                fontFamilyFallback: const ['Menlo', 'Consolas', 'Courier'],
                height: 1.6,
                fontSize: 13.5,
              ),
              decoration: InputDecoration(
                filled: true,
                fillColor: jc.surface,
                border: InputBorder.none,
                enabledBorder: InputBorder.none,
                focusedBorder: InputBorder.none,
                contentPadding: const EdgeInsets.all(AppSpace.lg),
                hintText: 'You are Jarvis, the office procurement assistant…',
              ),
            ),
          ),
          const Divider(),
          Padding(
            padding: const EdgeInsets.symmetric(
              horizontal: AppSpace.lg,
              vertical: AppSpace.md,
            ),
            child: Wrap(
              alignment: WrapAlignment.spaceBetween,
              crossAxisAlignment: WrapCrossAlignment.center,
              spacing: AppSpace.sm,
              runSpacing: AppSpace.sm,
              children: [
                OutlinedButton.icon(
                  onPressed: _busy ? null : _resetToDefault,
                  icon: const Icon(Icons.restart_alt_rounded, size: 18),
                  label: const Text('Reset to default'),
                ),
                Wrap(
                  spacing: AppSpace.sm,
                  children: [
                    if (_dirty)
                      TextButton(
                        onPressed: _busy ? null : _discard,
                        child: const Text('Discard'),
                      ),
                    FilledButton.icon(
                      onPressed: _busy || !_dirty || _empty ? null : _save,
                      icon: const Icon(Icons.save_outlined, size: 18),
                      label: const Text('Save prompt'),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ],
      ),
    );

    const tips = _PromptTips();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        meta,
        const SizedBox(height: AppSpace.lg),
        LayoutBuilder(
          builder: (context, c) => c.maxWidth < 980
              ? Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    editor,
                    const SizedBox(height: AppSpace.lg),
                    tips,
                  ],
                )
              : Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Expanded(child: editor),
                    const SizedBox(width: AppSpace.xl),
                    const SizedBox(width: 300, child: tips),
                  ],
                ),
        ),
      ],
    );
  }
}

class _PromptTips extends StatelessWidget {
  const _PromptTips();

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    Widget tip(IconData icon, String title, String body) => Padding(
      padding: const EdgeInsets.only(bottom: AppSpace.lg),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: 18, color: jc.brand),
          const SizedBox(width: AppSpace.sm),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title, style: context.tt.titleSmall),
                const SizedBox(height: AppSpace.xxs),
                Text(body, style: context.tt.bodySmall),
              ],
            ),
          ),
        ],
      ),
    );
    return AppCard(
      title: 'How this prompt is used',
      child: Column(
        children: [
          tip(
            Icons.forum_outlined,
            'Text and voice',
            'Both the chat agent and the realtime voice session load it at '
                'the start of every new session.',
          ),
          tip(
            Icons.place_outlined,
            'Delivery address',
            'Keep the instruction to ask which office address to use before '
                'confirming an order.',
          ),
          tip(
            Icons.lock_outline_rounded,
            'Guardrails stay in code',
            'Policy limits, approvals and payments are enforced by the '
                'server. The prompt cannot loosen them.',
          ),
          tip(
            Icons.keyboard_outlined,
            'Shortcut',
            'Press Ctrl+S or Cmd+S in the editor to save.',
          ),
        ],
      ),
    );
  }
}
