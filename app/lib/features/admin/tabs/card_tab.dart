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

/// Card enrollment: starts Reap's hosted card-entry page and shows its status.
/// The app never sees or stores card details.
class CardTab extends ConsumerStatefulWidget {
  const CardTab({super.key});

  @override
  ConsumerState<CardTab> createState() => _CardTabState();
}

class _CardTabState extends ConsumerState<CardTab> {
  bool _busy = false;

  /// Hosted page URL that could not be opened automatically (non-web).
  String? _manualUrl;

  Future<void> _open(String url) async {
    final opened = await ref.read(urlOpenerProvider).open(url);
    if (!mounted) return;
    setState(() => _manualUrl = opened ? null : url);
  }

  Future<void> _start() async {
    setState(() => _busy = true);
    try {
      final e = await ref.read(apiProvider).startEnrollment();
      ref.invalidate(currentEnrollmentProvider);
      if (!mounted) return;
      if (e.nextActionUrl.isNotEmpty) {
        await _open(e.nextActionUrl);
        if (mounted) {
          showToast(context, 'Enter the card on Reap\'s secure page.');
        }
      } else if (mounted) {
        showToast(context, 'Card setup started.');
      }
    } catch (e) {
      if (mounted) showToast(context, errorMessage(e), error: true);
    }
    if (mounted) setState(() => _busy = false);
  }

  Future<void> _refresh() async {
    ref.invalidate(currentEnrollmentProvider);
    try {
      final e = await ref.read(currentEnrollmentProvider.future);
      if (!mounted) return;
      showToast(
        context,
        e == null
            ? 'No card set up yet.'
            : e.isActive
            ? 'Card is ready for checkout.'
            : 'Status: ${StatusChip.enrollment(e.status).label}',
      );
    } catch (_) {
      // The error state is rendered below.
    }
  }

  @override
  Widget build(BuildContext context) {
    return AsyncValueView<Enrollment?>(
      value: ref.watch(currentEnrollmentProvider),
      loading: const AdminSkeleton(rows: 2, rowHeight: 120),
      onRetry: () => ref.invalidate(currentEnrollmentProvider),
      data: (e) {
        final status = e?.status ?? EnrollmentStatus.unknown;
        final pending = status == EnrollmentStatus.requiresAction;
        final main = AppCard(
          title: 'Company payment card',
          subtitle: 'Used by Reap to pay merchants after an order is approved',
          leading: _CardGlyph(active: e?.isActive ?? false),
          trailing: StatusChip.enrollment(
            status,
            key: const ValueKey('enrollment-status'),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              _Steps(status: e == null ? null : status),
              if (e != null) ...[
                const SizedBox(height: AppSpace.lg),
                const Divider(),
                const SizedBox(height: AppSpace.md),
                _Detail('Reap enrollment', e.reapEnrollmentId),
                if (e.ownerEmail.isNotEmpty) _Detail('Owner', e.ownerEmail),
                _Detail('Started', Fmt.dateTime(e.createdAt)),
              ],
              if (_manualUrl != null) ...[
                const SizedBox(height: AppSpace.md),
                _ManualLink(url: _manualUrl!),
              ],
              const SizedBox(height: AppSpace.lg),
              Wrap(
                spacing: AppSpace.sm,
                runSpacing: AppSpace.sm,
                children: [
                  if (pending && e!.nextActionUrl.isNotEmpty)
                    FilledButton.icon(
                      onPressed: _busy ? null : () => _open(e.nextActionUrl),
                      icon: const Icon(Icons.open_in_new_rounded, size: 18),
                      label: const Text('Continue on Reap'),
                    )
                  else
                    FilledButton.icon(
                      onPressed: _busy ? null : _start,
                      icon: const Icon(Icons.add_card_rounded, size: 18),
                      label: Text(
                        e?.isActive ?? false ? 'Replace card' : 'Set up card',
                      ),
                    ),
                  if (pending)
                    OutlinedButton(
                      onPressed: _busy ? null : _start,
                      child: const Text('Start over'),
                    ),
                  OutlinedButton.icon(
                    onPressed: _busy ? null : _refresh,
                    icon: const Icon(Icons.refresh_rounded, size: 18),
                    label: const Text('Check status'),
                  ),
                ],
              ),
            ],
          ),
        );
        const privacy = _PrivacyNote();
        return LayoutBuilder(
          builder: (context, c) => c.maxWidth < 900
              ? Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    main,
                    const SizedBox(height: AppSpace.lg),
                    privacy,
                  ],
                )
              : Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Expanded(flex: 3, child: main),
                    const SizedBox(width: AppSpace.xl),
                    const Expanded(flex: 2, child: privacy),
                  ],
                ),
        );
      },
    );
  }
}

class _CardGlyph extends StatelessWidget {
  const _CardGlyph({required this.active});
  final bool active;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return Container(
      width: 44,
      height: 44,
      decoration: BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: active
              ? [AppPalette.brand500, AppPalette.brand700]
              : [jc.surfaceMuted, jc.border],
        ),
        borderRadius: AppRadius.mdAll,
      ),
      child: Icon(
        Icons.credit_card_rounded,
        color: active ? Colors.white : jc.textMuted,
      ),
    );
  }
}

class _Steps extends StatelessWidget {
  const _Steps({required this.status});

  /// Null when no enrollment exists yet.
  final EnrollmentStatus? status;

  @override
  Widget build(BuildContext context) {
    final s = status;
    final failed =
        s == EnrollmentStatus.failed ||
        s == EnrollmentStatus.expired ||
        s == EnrollmentStatus.revoked;
    final reached = switch (s) {
      null || EnrollmentStatus.unknown => 0,
      EnrollmentStatus.requiresAction => 1,
      EnrollmentStatus.active => 3,
      _ => 1,
    };
    final steps = [
      ('Start setup', 'Jarvis asks Reap for a secure card page'),
      ('Enter card on Reap', 'Card details go to Reap only'),
      ('Ready for checkout', 'Approved orders can be paid'),
    ];
    return Column(
      children: [
        for (var i = 0; i < steps.length; i++)
          _StepRow(
            index: i + 1,
            title: steps[i].$1,
            body: steps[i].$2,
            done: i < reached,
            current: i == reached && !failed,
            failed: failed && i == 1,
            last: i == steps.length - 1,
          ),
      ],
    );
  }
}

class _StepRow extends StatelessWidget {
  const _StepRow({
    required this.index,
    required this.title,
    required this.body,
    required this.done,
    required this.current,
    required this.failed,
    required this.last,
  });

  final int index;
  final String title;
  final String body;
  final bool done;
  final bool current;
  final bool failed;
  final bool last;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final tone = failed
        ? Tone.danger
        : done
        ? Tone.success
        : current
        ? Tone.brand
        : Tone.neutral;
    return IntrinsicHeight(
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Column(
            children: [
              Container(
                width: 28,
                height: 28,
                decoration: BoxDecoration(
                  color: jc.bg(tone),
                  shape: BoxShape.circle,
                  border: Border.all(color: jc.fg(tone).withValues(alpha: 0.3)),
                ),
                alignment: Alignment.center,
                child: done
                    ? Icon(Icons.check_rounded, size: 16, color: jc.fg(tone))
                    : failed
                    ? Icon(Icons.close_rounded, size: 16, color: jc.fg(tone))
                    : Text(
                        '$index',
                        style: context.tt.labelMedium?.copyWith(
                          color: jc.fg(tone),
                        ),
                      ),
              ),
              if (!last)
                Expanded(
                  child: Container(
                    width: 2,
                    margin: const EdgeInsets.symmetric(vertical: AppSpace.xs),
                    color: done ? jc.success : jc.border,
                  ),
                ),
            ],
          ),
          const SizedBox(width: AppSpace.md),
          Expanded(
            child: Padding(
              padding: EdgeInsets.only(bottom: last ? 0 : AppSpace.lg, top: 4),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    title,
                    style: context.tt.titleSmall?.copyWith(
                      color: done || current ? jc.textPrimary : jc.textMuted,
                    ),
                  ),
                  Text(body, style: context.tt.bodySmall),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _Detail extends StatelessWidget {
  const _Detail(this.label, this.value);
  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpace.xs),
      child: Row(
        children: [
          SizedBox(width: 140, child: Text(label, style: context.tt.bodySmall)),
          Expanded(
            child: SelectableText(
              value.isEmpty ? '—' : value,
              style: context.tt.bodyMedium,
            ),
          ),
        ],
      ),
    );
  }
}

class _ManualLink extends StatelessWidget {
  const _ManualLink({required this.url});
  final String url;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return Container(
      padding: const EdgeInsets.all(AppSpace.md),
      decoration: BoxDecoration(
        color: jc.infoSubtle,
        borderRadius: AppRadius.mdAll,
      ),
      child: Row(
        children: [
          Icon(Icons.link_rounded, size: 18, color: jc.info),
          const SizedBox(width: AppSpace.sm),
          Expanded(
            child: SelectableText(
              url,
              style: context.tt.bodySmall?.copyWith(color: jc.info),
            ),
          ),
          IconButton(
            tooltip: 'Copy link',
            icon: const Icon(Icons.copy_rounded, size: 16),
            onPressed: () async {
              await Clipboard.setData(ClipboardData(text: url));
              if (context.mounted) showToast(context, 'Link copied');
            },
          ),
        ],
      ),
    );
  }
}

class _PrivacyNote extends StatelessWidget {
  const _PrivacyNote();

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return AppCard(
      title: 'Your card stays with Reap',
      leading: Icon(Icons.verified_user_outlined, color: jc.success),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'The card number is entered on a page hosted by Reap. Jarvis only '
            'keeps the enrollment reference and its status, never the card '
            'number, expiry or last four digits.',
            style: context.tt.bodyMedium,
          ),
          const SizedBox(height: AppSpace.md),
          Text(
            'Every payment is still approved on Reap\'s checkout page before '
            'money moves.',
            style: context.tt.bodySmall,
          ),
        ],
      ),
    );
  }
}
