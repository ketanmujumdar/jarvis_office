import 'package:flutter/material.dart';

import '../theme/tokens.dart';

/// Friendly placeholder for empty lists, errors and not-yet-built screens.
class EmptyState extends StatelessWidget {
  const EmptyState({
    super.key,
    required this.icon,
    required this.title,
    this.message,
    this.action,
    this.tone = Tone.brand,
    this.compact = false,
  });

  /// Error variant with a retry button.
  factory EmptyState.error({
    Key? key,
    required String message,
    VoidCallback? onRetry,
  }) => EmptyState(
    key: key,
    icon: Icons.error_outline_rounded,
    title: 'Something went wrong',
    message: message,
    tone: Tone.danger,
    action: onRetry == null
        ? null
        : OutlinedButton.icon(
            onPressed: onRetry,
            icon: const Icon(Icons.refresh_rounded, size: 18),
            label: const Text('Try again'),
          ),
  );

  final IconData icon;
  final String title;
  final String? message;
  final Widget? action;
  final Tone tone;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final size = compact ? 40.0 : 56.0;
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 420),
        child: Padding(
          padding: EdgeInsets.all(compact ? AppSpace.lg : AppSpace.xxl),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Container(
                width: size,
                height: size,
                decoration: BoxDecoration(
                  color: jc.bg(tone),
                  shape: BoxShape.circle,
                ),
                child: Icon(icon, color: jc.fg(tone), size: size * 0.5),
              ),
              SizedBox(height: compact ? AppSpace.md : AppSpace.lg),
              Text(
                title,
                style: compact ? context.tt.titleMedium : context.tt.titleLarge,
                textAlign: TextAlign.center,
              ),
              if (message != null) ...[
                const SizedBox(height: AppSpace.sm),
                Text(
                  message!,
                  style: context.tt.bodyMedium?.copyWith(
                    color: jc.textSecondary,
                  ),
                  textAlign: TextAlign.center,
                ),
              ],
              if (action != null) ...[
                const SizedBox(height: AppSpace.lg),
                action!,
              ],
            ],
          ),
        ),
      ),
    );
  }
}
