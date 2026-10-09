import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api/api_client.dart';
import '../../../core/api/models.dart';
import '../../../core/format.dart';
import '../../../core/providers.dart';
import '../../../core/theme/tokens.dart';
import '../../../core/widgets/widgets.dart';
import '../request_controller.dart';
import 'progress_tracker.dart';

/// The confirm step, shown while the request is quoted: what is being bought
/// (and what will be skipped), where it goes, and the card check.
///
/// The manager must pick a saved Singapore address before confirming. The
/// card state is known before the user acts, so a missing or unfinished Reap
/// card enrollment is shown up front and Confirm stays disabled until the card
/// is active. Card details are only ever entered on Reap's hosted page.
class ConfirmPanel extends ConsumerStatefulWidget {
  const ConfirmPanel({super.key, required this.detail});
  final RequestDetail detail;

  @override
  ConsumerState<ConfirmPanel> createState() => _ConfirmPanelState();
}

class _ConfirmPanelState extends ConsumerState<ConfirmPanel> {
  String? _selected;
  bool _choosing = false;
  bool _submitting = false;
  bool _enrolling = false;
  ApiException? _error;
  Timer? _enrollPoll;

  String get _requestId => widget.detail.request.id;

  @override
  void dispose() {
    _enrollPoll?.cancel();
    super.dispose();
  }

  Future<void> _confirm() async {
    final id = _selected;
    if (id == null) return;
    setState(() {
      _submitting = true;
      _error = null;
    });
    try {
      await ref.read(requestDetailProvider(_requestId).notifier).confirm(id);
    } on ApiException catch (e) {
      if (e.needsEnrollment) ref.invalidate(currentEnrollmentProvider);
      if (!e.needsEnrollment) {
        debugPrint('confirm failed: ${e.statusCode} ${e.code}: ${e.message}');
      }
      if (mounted) setState(() => _error = e);
    } finally {
      if (mounted) setState(() => _submitting = false);
    }
  }

  /// Opens the Reap card page: the unfinished enrollment's page when there is
  /// one, otherwise a new enrollment. Then polls until the card is active.
  Future<void> _enroll(Enrollment? current) async {
    setState(() => _enrolling = true);
    try {
      var url = '';
      if (current != null &&
          current.status == EnrollmentStatus.requiresAction &&
          current.nextActionUrl.isNotEmpty) {
        url = current.nextActionUrl;
      } else {
        final e = await ref.read(apiProvider).startEnrollment();
        url = e.nextActionUrl;
      }
      if (url.isNotEmpty) ref.read(urlOpenerProvider)(url);
      ref.invalidate(currentEnrollmentProvider);
      _startEnrollPolling();
      if (mounted) setState(() => _error = null);
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e);
    } finally {
      if (mounted) setState(() => _enrolling = false);
    }
  }

  void _startEnrollPolling() {
    _enrollPoll?.cancel();
    _enrollPoll = Timer.periodic(const Duration(seconds: 4), (_) {
      if (!mounted) return;
      ref.invalidate(currentEnrollmentProvider);
    });
  }

  @override
  Widget build(BuildContext context) {
    final addresses = ref.watch(deliveryAddressesProvider);
    final enrollment = ref.watch(currentEnrollmentProvider);
    final jc = context.jc;
    final total = widget.detail.request.totalCents;
    final card = enrollment.value;
    final cardKnown = enrollment.hasValue;
    final cardActive = card?.status == EnrollmentStatus.active;
    if (cardActive && _enrollPoll != null) {
      _enrollPoll!.cancel();
      _enrollPoll = null;
    }

    return SectionCard(
      title: 'Confirm order',
      subtitle: 'Check what is being bought and where it goes.',
      icon: Icons.local_shipping_outlined,
      highlight: Tone.brand,
      trailing: enrollment.maybeWhen(
        data: (e) =>
            StatusChip.enrollment(e?.status ?? EnrollmentStatus.unknown),
        orElse: () => null,
      ),
      child: AsyncValueView<List<Address>>(
        value: addresses,
        onRetry: () => ref.invalidate(deliveryAddressesProvider),
        data: (list) {
          if (list.isEmpty) {
            return const EmptyState(
              compact: true,
              icon: Icons.location_off_outlined,
              title: 'No delivery addresses',
              message: 'Ask an admin to add an office address.',
            );
          }
          // Preselect the default so one tap confirms the common case.
          _selected ??= list
              .firstWhere((a) => a.isDefault, orElse: () => list.first)
              .id;
          final chosen = list.firstWhere(
            (a) => a.id == _selected,
            orElse: () => list.first,
          );
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              _BasketSummary(detail: widget.detail),
              const SizedBox(height: AppSpace.lg),
              Row(
                children: [
                  Expanded(
                    child: Text('Deliver to', style: context.tt.titleSmall),
                  ),
                  if (list.length > 1)
                    TextButton(
                      key: const ValueKey('change-address'),
                      onPressed: _submitting
                          ? null
                          : () => setState(() => _choosing = !_choosing),
                      child: Text(_choosing ? 'Done' : 'Change'),
                    ),
                ],
              ),
              const SizedBox(height: AppSpace.xs),
              AnimatedSize(
                duration: const Duration(milliseconds: 180),
                alignment: Alignment.topCenter,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    for (final a in _choosing ? list : [chosen])
                      _AddressOption(
                        address: a,
                        selected: a.id == _selected,
                        onTap: _submitting
                            ? null
                            : () => setState(() {
                                _selected = a.id;
                                _choosing = false;
                              }),
                      ),
                  ],
                ),
              ),
              if (cardKnown && !cardActive) ...[
                const SizedBox(height: AppSpace.sm),
                _cardCallout(card),
              ],
              if (_error != null &&
                  !(_error!.needsEnrollment && cardKnown && !cardActive)) ...[
                const SizedBox(height: AppSpace.sm),
                _error!.needsEnrollment
                    ? _cardCallout(card)
                    : Callout(
                        tone: Tone.danger,
                        icon: Icons.error_outline_rounded,
                        title: 'Could not confirm',
                        message: friendlyConfirmError(_error!),
                      ),
              ],
              const SizedBox(height: AppSpace.md),
              Row(
                children: [
                  Expanded(
                    child: Text(
                      'Policy runs again on confirm. Payment is approved on Reap’s page.',
                      style: context.tt.bodySmall?.copyWith(
                        color: jc.textMuted,
                      ),
                    ),
                  ),
                  const SizedBox(width: AppSpace.md),
                  FilledButton.icon(
                    key: const ValueKey('confirm-order'),
                    onPressed:
                        _submitting ||
                            _selected == null ||
                            (cardKnown && !cardActive)
                        ? null
                        : _confirm,
                    icon: _submitting
                        ? const SizedBox(
                            width: 16,
                            height: 16,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Icon(Icons.check_circle_outline_rounded),
                    label: Text('Confirm ${Fmt.money(total)}'),
                  ),
                ],
              ),
            ],
          );
        },
      ),
    );
  }

  Widget _cardCallout(Enrollment? card) {
    final pending =
        card != null &&
        card.status == EnrollmentStatus.requiresAction &&
        card.nextActionUrl.isNotEmpty;
    return Callout(
      tone: Tone.warning,
      icon: Icons.credit_card_outlined,
      title: pending
          ? 'Finish adding the company card'
          : 'Add a company card first',
      message: pending
          ? 'Card entry on Reap is not finished yet. This updates by itself once it is.'
          : 'Cards are entered on Reap’s secure page. Jarvis never sees card details.',
      action: FilledButton.tonalIcon(
        key: const ValueKey('setup-card'),
        onPressed: _enrolling ? null : () => _enroll(card),
        icon: const Icon(Icons.open_in_new_rounded, size: 18),
        label: Text(pending ? 'Continue on Reap' : 'Set up card with Reap'),
      ),
    );
  }
}

/// User-facing text for a failed confirm. Upstream/internal details are never
/// shown verbatim (they are logged instead).
String friendlyConfirmError(ApiException e) => switch (e.code) {
  'validation_failed' => e.message,
  'invalid_transition' =>
    'This request changed in the meantime. Refresh and try again.',
  'forbidden' => 'Only an office manager or admin can confirm orders.',
  'network' => 'Could not reach Jarvis. Check your connection and try again.',
  _ => 'Something went wrong confirming this order. Try again.',
};

/// What will be bought, and which lines are skipped or need approval.
class _BasketSummary extends StatelessWidget {
  const _BasketSummary({required this.detail});
  final RequestDetail detail;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final lines = [...detail.lineItems]
      ..sort((a, b) => a.position.compareTo(b.position));
    final buying = lines.where((l) => l.policyDecision != Decision.reject);
    final skipped = lines.where((l) => l.policyDecision == Decision.reject);
    final review = lines.where(
      (l) => l.policyDecision == Decision.needsApproval,
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        for (final l in buying)
          Padding(
            padding: const EdgeInsets.only(bottom: AppSpace.xs),
            child: Row(
              children: [
                Icon(Icons.check_rounded, size: 16, color: jc.success),
                const SizedBox(width: AppSpace.sm),
                Expanded(
                  child: Text(
                    '${l.qty} × ${l.description}',
                    style: context.tt.bodyMedium,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                if (l.policyDecision == Decision.needsApproval)
                  const Tag('Needs approval', tone: Tone.warning),
              ],
            ),
          ),
        if (skipped.isNotEmpty) ...[
          const SizedBox(height: AppSpace.xs),
          Callout(
            key: const ValueKey('skipped-lines'),
            tone: Tone.warning,
            icon: Icons.remove_shopping_cart_outlined,
            title: skipped.length == 1
                ? '1 item can’t be ordered and will be skipped'
                : '${skipped.length} items can’t be ordered and will be skipped',
            message: skipped.map((l) => l.description).join(', '),
          ),
        ],
        if (review.isNotEmpty) ...[
          const SizedBox(height: AppSpace.xs),
          Text(
            'An approver reviews this order after you confirm.',
            style: context.tt.bodySmall?.copyWith(color: jc.textMuted),
          ),
        ],
      ],
    );
  }
}

class _AddressOption extends StatelessWidget {
  const _AddressOption({
    required this.address,
    required this.selected,
    required this.onTap,
  });

  final Address address;
  final bool selected;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpace.sm),
      child: Semantics(
        selected: selected,
        button: true,
        child: Material(
          color: selected ? jc.brandSubtle : jc.surface,
          shape: RoundedRectangleBorder(
            borderRadius: AppRadius.mdAll,
            side: BorderSide(
              color: selected ? jc.brand : jc.border,
              width: selected ? 1.5 : 1,
            ),
          ),
          child: InkWell(
            key: ValueKey('address-${address.id}'),
            borderRadius: AppRadius.mdAll,
            onTap: onTap,
            child: Padding(
              padding: const EdgeInsets.all(AppSpace.md),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Icon(
                    selected
                        ? Icons.radio_button_checked_rounded
                        : Icons.radio_button_off_rounded,
                    color: selected ? jc.brand : jc.textMuted,
                    size: 20,
                  ),
                  const SizedBox(width: AppSpace.md),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Wrap(
                          spacing: AppSpace.sm,
                          crossAxisAlignment: WrapCrossAlignment.center,
                          children: [
                            Text(
                              address.label,
                              style: context.tt.bodyMedium?.copyWith(
                                fontWeight: FontWeight.w600,
                                color: jc.textPrimary,
                              ),
                            ),
                            if (address.isDefault)
                              const Tag('Default', tone: Tone.brand),
                          ],
                        ),
                        const SizedBox(height: AppSpace.xxs),
                        Text(address.oneLine, style: context.tt.bodySmall),
                        Text(
                          'Attn: ${address.contactName} · ${address.phone}',
                          style: context.tt.labelSmall?.copyWith(
                            color: jc.textMuted,
                          ),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
