import 'package:flutter/material.dart';

import '../../core/api/api_client.dart';
import '../../core/theme/tokens.dart';

/// Shared building blocks for the admin console tabs.

/// Human-readable message for an error thrown by the API client.
String errorMessage(Object e) {
  if (e is ApiException) {
    if (e.code == 'not_implemented') {
      return 'The server does not support this yet.';
    }
    return e.message;
  }
  return e.toString();
}

/// Parses an SGD amount typed by a person ("1,250.5", "S\$35") into cents.
/// Returns null when the text is not a non-negative amount with at most 2 dp.
int? parseCents(String text) {
  final t = text.trim().replaceAll(',', '').replaceFirst(RegExp(r'^S?\$'), '');
  if (!RegExp(r'^\d+(\.\d{0,2})?$').hasMatch(t)) return null;
  final parts = t.split('.');
  final whole = int.parse(parts[0]);
  final frac = parts.length > 1 ? parts[1].padRight(2, '0') : '00';
  return whole * 100 + int.parse(frac.isEmpty ? '0' : frac);
}

/// Formats cents for an editable field (3550 -> "35.50").
String centsToInput(int cents) {
  final whole = cents ~/ 100;
  final frac = (cents % 100).toString().padLeft(2, '0');
  return '$whole.$frac';
}

/// Shows a floating snackbar. [error] tints it with the danger tone.
void showToast(BuildContext context, String message, {bool error = false}) {
  final messenger = ScaffoldMessenger.maybeOf(context);
  if (messenger == null) return;
  messenger
    ..hideCurrentSnackBar()
    ..showSnackBar(
      SnackBar(
        content: Row(
          children: [
            Icon(
              error ? Icons.error_outline_rounded : Icons.check_circle_rounded,
              size: 18,
              color: error ? context.jc.danger : context.jc.success,
            ),
            const SizedBox(width: AppSpace.sm),
            Expanded(child: Text(message)),
          ],
        ),
      ),
    );
}

/// Runs an API mutation and reports the outcome. Returns true on success.
Future<bool> runMutation(
  BuildContext context,
  Future<void> Function() action, {
  required String success,
}) async {
  try {
    await action();
    if (context.mounted) showToast(context, success);
    return true;
  } catch (e) {
    if (context.mounted) showToast(context, errorMessage(e), error: true);
    return false;
  }
}

/// Asks for confirmation. Returns true when the person confirms.
Future<bool> confirmAction(
  BuildContext context, {
  required String title,
  required String message,
  required String confirmLabel,
  bool destructive = false,
}) async {
  final ok = await showDialog<bool>(
    context: context,
    builder: (ctx) => AlertDialog(
      title: Text(title),
      content: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 420),
        child: Text(message),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(ctx).pop(false),
          child: const Text('Cancel'),
        ),
        FilledButton(
          style: destructive
              ? FilledButton.styleFrom(backgroundColor: ctx.jc.danger)
              : null,
          onPressed: () => Navigator.of(ctx).pop(true),
          child: Text(confirmLabel),
        ),
      ],
    ),
  );
  return ok ?? false;
}

/// Static loading placeholder (no infinite animation, so tests can settle).
class AdminSkeleton extends StatelessWidget {
  const AdminSkeleton({super.key, this.rows = 5, this.rowHeight = 44});
  final int rows;
  final double rowHeight;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return Semantics(
      label: 'Loading',
      child: Column(
        children: [
          for (var i = 0; i < rows; i++)
            Container(
              height: rowHeight,
              margin: const EdgeInsets.only(bottom: AppSpace.sm),
              decoration: BoxDecoration(
                color: jc.surfaceMuted,
                borderRadius: AppRadius.mdAll,
              ),
            ),
        ],
      ),
    );
  }
}

/// Consistent dialog frame for admin forms: title, subtitle, scrollable body
/// and an action row. Width adapts to the screen.
class FormDialog extends StatelessWidget {
  const FormDialog({
    super.key,
    required this.title,
    required this.body,
    required this.actions,
    this.subtitle,
    this.icon,
    this.maxWidth = 600,
  });

  final String title;
  final String? subtitle;
  final IconData? icon;
  final Widget body;
  final List<Widget> actions;
  final double maxWidth;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final size = MediaQuery.sizeOf(context);
    final narrow = size.width < AppBreakpoints.compact;
    return Dialog(
      insetPadding: EdgeInsets.all(narrow ? AppSpace.md : AppSpace.xl),
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxWidth: maxWidth,
          maxHeight: size.height * 0.9,
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(
                AppSpace.xl,
                AppSpace.xl,
                AppSpace.xl,
                AppSpace.lg,
              ),
              child: Row(
                children: [
                  if (icon != null) ...[
                    Container(
                      padding: const EdgeInsets.all(AppSpace.sm),
                      decoration: BoxDecoration(
                        color: jc.brandSubtle,
                        borderRadius: AppRadius.mdAll,
                      ),
                      child: Icon(icon, size: 20, color: jc.brand),
                    ),
                    const SizedBox(width: AppSpace.md),
                  ],
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(title, style: context.tt.titleLarge),
                        if (subtitle != null) ...[
                          const SizedBox(height: AppSpace.xxs),
                          Text(subtitle!, style: context.tt.bodySmall),
                        ],
                      ],
                    ),
                  ),
                ],
              ),
            ),
            const Divider(),
            Flexible(
              child: SingleChildScrollView(
                padding: const EdgeInsets.all(AppSpace.xl),
                child: body,
              ),
            ),
            const Divider(),
            Padding(
              padding: const EdgeInsets.symmetric(
                horizontal: AppSpace.xl,
                vertical: AppSpace.lg,
              ),
              child: Wrap(
                alignment: WrapAlignment.end,
                spacing: AppSpace.sm,
                runSpacing: AppSpace.sm,
                children: actions,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Lays out form fields two per row on wide dialogs, one per row on narrow.
class FieldGrid extends StatelessWidget {
  const FieldGrid({super.key, required this.children, this.columns = 2});
  final List<Widget> children;
  final int columns;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, c) {
        final cols = c.maxWidth < 480 ? 1 : columns;
        final w = (c.maxWidth - AppSpace.md * (cols - 1)) / cols;
        return Wrap(
          spacing: AppSpace.md,
          runSpacing: AppSpace.lg,
          children: [
            for (final child in children)
              SizedBox(
                width: child is FullWidth ? c.maxWidth : w,
                child: child,
              ),
          ],
        );
      },
    );
  }
}

/// Marks a [FieldGrid] child that spans the whole row.
class FullWidth extends StatelessWidget {
  const FullWidth({super.key, required this.child});
  final Widget child;
  @override
  Widget build(BuildContext context) => child;
}

/// A switch with a title and helper text, laid out as a bordered tile.
class SwitchTile extends StatelessWidget {
  const SwitchTile({
    super.key,
    required this.title,
    required this.value,
    required this.onChanged,
    this.subtitle,
  });

  final String title;
  final String? subtitle;
  final bool value;
  final ValueChanged<bool>? onChanged;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return Container(
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpace.md,
        vertical: AppSpace.sm,
      ),
      decoration: BoxDecoration(
        border: Border.all(color: jc.border),
        borderRadius: AppRadius.mdAll,
      ),
      child: Row(
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title, style: context.tt.titleSmall),
                if (subtitle != null)
                  Text(subtitle!, style: context.tt.bodySmall),
              ],
            ),
          ),
          Switch(value: value, onChanged: onChanged),
        ],
      ),
    );
  }
}

/// Search box used above admin tables.
class AdminSearchField extends StatelessWidget {
  const AdminSearchField({
    super.key,
    required this.hint,
    required this.onChanged,
    this.controller,
  });

  final String hint;
  final ValueChanged<String> onChanged;
  final TextEditingController? controller;

  @override
  Widget build(BuildContext context) {
    return TextField(
      controller: controller,
      onChanged: onChanged,
      decoration: InputDecoration(
        hintText: hint,
        prefixIcon: const Icon(Icons.search_rounded, size: 20),
      ),
    );
  }
}

/// Small rounded tag (aliases, categories, counts).
class Tag extends StatelessWidget {
  const Tag(this.label, {super.key, this.tone = Tone.neutral, this.icon});
  final String label;
  final Tone tone;
  final IconData? icon;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return Container(
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpace.sm,
        vertical: AppSpace.xxs + 1,
      ),
      decoration: BoxDecoration(
        color: jc.bg(tone),
        borderRadius: AppRadius.smAll,
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (icon != null) ...[
            Icon(icon, size: 12, color: jc.fg(tone)),
            const SizedBox(width: AppSpace.xs),
          ],
          Flexible(
            child: Text(
              label,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: context.tt.labelSmall?.copyWith(
                color: jc.fg(tone),
                letterSpacing: 0.1,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// Header row for a tab: count summary on the left, actions on the right.
class TabToolbar extends StatelessWidget {
  const TabToolbar({super.key, required this.leading, this.actions = const []});
  final Widget leading;
  final List<Widget> actions;

  @override
  Widget build(BuildContext context) {
    return Wrap(
      alignment: WrapAlignment.spaceBetween,
      crossAxisAlignment: WrapCrossAlignment.center,
      spacing: AppSpace.md,
      runSpacing: AppSpace.md,
      children: [
        leading,
        Wrap(spacing: AppSpace.sm, runSpacing: AppSpace.sm, children: actions),
      ],
    );
  }
}

/// Validators shared by admin forms.
abstract final class Validators {
  static String? required(String? v, [String field = 'This field']) =>
      (v == null || v.trim().isEmpty) ? '$field is required' : null;

  static String? positiveInt(String? v) {
    final n = int.tryParse(v?.trim() ?? '');
    if (n == null || n < 1) return 'Enter a whole number of at least 1';
    return null;
  }

  static String? money(String? v) =>
      parseCents(v ?? '') == null ? 'Enter an amount like 35.50' : null;

  /// E.164 as required by Reap shipping addresses.
  static String? phone(String? v) {
    final t = (v ?? '').replaceAll(' ', '');
    return RegExp(r'^\+[1-9]\d{6,14}$').hasMatch(t)
        ? null
        : 'Use international format, e.g. +6562001001';
  }

  static String? email(String? v) =>
      RegExp(r'^[^@\s]+@[^@\s]+\.[^@\s]+$').hasMatch((v ?? '').trim())
      ? null
      : 'Enter a valid email';

  /// Singapore postal codes are six digits.
  static String? sgPostal(String? v) =>
      RegExp(r'^\d{6}$').hasMatch((v ?? '').trim())
      ? null
      : 'Singapore postal codes have 6 digits';
}
