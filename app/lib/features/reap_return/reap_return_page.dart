import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../../core/theme/tokens.dart';
import '../../core/widgets/widgets.dart';

/// Where Reap's hosted pages (card entry, payment approval) send the browser
/// back to (REAP_RETURN_URL = `<app origin>/reap-return`). Jarvis picks up the
/// new card or payment status by itself, so this page only confirms that and
/// offers a way back. It works without a session (the tab may be new).
class ReapReturnPage extends StatelessWidget {
  const ReapReturnPage({super.key, this.status});

  /// Optional `status` query parameter Reap appends (e.g. COMPLETED).
  final String? status;

  @override
  Widget build(BuildContext context) {
    final s = (status ?? '').toUpperCase();
    final failed = s == 'FAILED' || s == 'EXPIRED';
    return Scaffold(
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 480),
          child: Padding(
            padding: const EdgeInsets.all(AppSpace.xl),
            child: AppCard(
              child: EmptyState(
                icon: failed
                    ? Icons.error_outline_rounded
                    : Icons.check_circle_outline_rounded,
                tone: failed ? Tone.danger : Tone.success,
                title: failed ? 'Not completed on Reap' : 'Done on Reap',
                message: failed
                    ? 'Reap did not complete this step. Return to Jarvis to try again.'
                    : 'Jarvis updates the card and payment status by itself. '
                          'You can close this tab or go back to Jarvis.',
                action: FilledButton.icon(
                  key: const ValueKey('reap-return-back'),
                  onPressed: () => context.go('/'),
                  icon: const Icon(Icons.arrow_back_rounded, size: 18),
                  label: const Text('Back to Jarvis'),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}
