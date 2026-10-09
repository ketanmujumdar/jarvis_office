import 'package:flutter/material.dart';

import '../api/models.dart';
import '../theme/tokens.dart';

/// Compact pill showing a status with a semantic tone and a leading dot.
///
/// Use the named constructors for domain statuses so colours stay consistent
/// across features.
class StatusChip extends StatelessWidget {
  const StatusChip({
    super.key,
    required this.label,
    this.tone = Tone.neutral,
    this.icon,
    this.pulse = false,
  });

  StatusChip.request(RequestStatus s, {Key? key})
    : this(key: key, label: s.label, tone: requestTone(s), pulse: s.isBusy);

  StatusChip.decision(Decision d, {Key? key})
    : this(key: key, label: d.label, tone: decisionTone(d));

  StatusChip.payment(PaymentStatus s, {Key? key})
    : this(
        key: key,
        label: s.label,
        tone: paymentTone(s),
        pulse: s == PaymentStatus.quoting || s == PaymentStatus.processing,
      );

  StatusChip.approval(ApprovalStatus s, {Key? key})
    : this(
        key: key,
        label: switch (s) {
          ApprovalStatus.pending => 'Pending',
          ApprovalStatus.approved => 'Approved',
          ApprovalStatus.rejected => 'Rejected',
          ApprovalStatus.unknown => 'Unknown',
        },
        tone: switch (s) {
          ApprovalStatus.pending => Tone.warning,
          ApprovalStatus.approved => Tone.success,
          ApprovalStatus.rejected => Tone.danger,
          ApprovalStatus.unknown => Tone.neutral,
        },
      );

  StatusChip.enrollment(EnrollmentStatus s, {Key? key})
    : this(
        key: key,
        label: switch (s) {
          EnrollmentStatus.active => 'Card ready',
          EnrollmentStatus.requiresAction => 'Card setup pending',
          EnrollmentStatus.failed => 'Card setup failed',
          EnrollmentStatus.expired => 'Card setup expired',
          EnrollmentStatus.revoked => 'Card revoked',
          EnrollmentStatus.unknown => 'No card',
        },
        tone: switch (s) {
          EnrollmentStatus.active => Tone.success,
          EnrollmentStatus.requiresAction => Tone.warning,
          EnrollmentStatus.failed ||
          EnrollmentStatus.expired ||
          EnrollmentStatus.revoked => Tone.danger,
          EnrollmentStatus.unknown => Tone.neutral,
        },
      );

  final String label;
  final Tone tone;
  final IconData? icon;

  /// Animate the dot to signal background work.
  final bool pulse;

  static Tone requestTone(RequestStatus s) => switch (s) {
    RequestStatus.parsing ||
    RequestStatus.searching ||
    RequestStatus.checkingOut ||
    RequestStatus.paying => Tone.info,
    RequestStatus.quoted || RequestStatus.approved => Tone.brand,
    RequestStatus.pendingApproval ||
    RequestStatus.awaitingPayment => Tone.warning,
    RequestStatus.ordered => Tone.success,
    RequestStatus.rejected || RequestStatus.failed => Tone.danger,
    RequestStatus.cancelled || RequestStatus.unknown => Tone.neutral,
  };

  static Tone decisionTone(Decision d) => switch (d) {
    Decision.autoApprove => Tone.success,
    Decision.needsApproval => Tone.warning,
    Decision.reject => Tone.danger,
    Decision.none => Tone.neutral,
  };

  static Tone paymentTone(PaymentStatus s) => switch (s) {
    PaymentStatus.quoting ||
    PaymentStatus.quoted ||
    PaymentStatus.processing => Tone.info,
    PaymentStatus.requiresAction => Tone.warning,
    PaymentStatus.completed => Tone.success,
    PaymentStatus.failed || PaymentStatus.expired => Tone.danger,
    PaymentStatus.unknown => Tone.neutral,
  };

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final fg = jc.fg(tone);
    return Semantics(
      label: 'Status: $label',
      child: Container(
        padding: const EdgeInsets.symmetric(
          horizontal: AppSpace.sm + 2,
          vertical: AppSpace.xs,
        ),
        decoration: BoxDecoration(
          color: jc.bg(tone),
          borderRadius: AppRadius.pillAll,
          border: Border.all(color: fg.withValues(alpha: 0.18)),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (icon != null)
              Icon(icon, size: 14, color: fg)
            else
              _Dot(color: fg, pulse: pulse),
            const SizedBox(width: AppSpace.xs + 2),
            ExcludeSemantics(
              child: Text(
                label,
                style: context.tt.labelMedium?.copyWith(color: fg),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _Dot extends StatefulWidget {
  const _Dot({required this.color, required this.pulse});
  final Color color;
  final bool pulse;

  @override
  State<_Dot> createState() => _DotState();
}

class _DotState extends State<_Dot> with SingleTickerProviderStateMixin {
  AnimationController? _c;

  @override
  void initState() {
    super.initState();
    _sync();
  }

  @override
  void didUpdateWidget(_Dot old) {
    super.didUpdateWidget(old);
    _sync();
  }

  void _sync() {
    final reduceMotion = WidgetsBinding
        .instance
        .platformDispatcher
        .accessibilityFeatures
        .disableAnimations;
    if (widget.pulse && !reduceMotion) {
      _c ??= AnimationController(
        vsync: this,
        duration: const Duration(milliseconds: 900),
      )..repeat(reverse: true);
    } else {
      _c?.dispose();
      _c = null;
    }
  }

  @override
  void dispose() {
    _c?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final dot = Container(
      width: 7,
      height: 7,
      decoration: BoxDecoration(color: widget.color, shape: BoxShape.circle),
    );
    final c = _c;
    if (c == null) return dot;
    return FadeTransition(
      opacity: Tween(begin: 0.35, end: 1.0).animate(c),
      child: dot,
    );
  }
}
