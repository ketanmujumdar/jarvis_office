import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api/models.dart';
import '../core/format.dart';
import '../features/request/request_controller.dart';
import '../features/voice/voice_controller.dart';
import 'glass.dart';
import 'orb.dart';

/// Which card (if any) the active request calls for.
sealed class _CardKind {
  const _CardKind();
  String get key;
}

class _Approve extends _CardKind {
  const _Approve(this.payment);
  final Payment payment;
  @override
  String get key => 'approve-${payment.id}';
}

class _Waiting extends _CardKind {
  const _Waiting(this.requestId);
  final String requestId;
  @override
  String get key => 'waiting-$requestId';
}

class _Ordered extends _CardKind {
  const _Ordered(this.detail);
  final RequestDetail detail;
  @override
  String get key => 'ordered-${detail.request.id}';
}

class _Failed extends _CardKind {
  const _Failed(this.request);
  final PurchaseRequest request;
  @override
  String get key => 'failed-${request.id}-${request.status.name}';
}

_CardKind? _cardFor(RequestDetail d) {
  for (final p in d.payments) {
    if (p.status == PaymentStatus.requiresAction && p.approvalUrl.isNotEmpty) {
      return _Approve(p);
    }
  }
  return switch (d.request.status) {
    RequestStatus.pendingApproval => _Waiting(d.request.id),
    RequestStatus.ordered => _Ordered(d),
    RequestStatus.failed ||
    RequestStatus.rejected ||
    RequestStatus.cancelled => _Failed(d.request),
    _ => null,
  };
}

/// Slides a contextual glass card up from the bottom, driven by the
/// conversation's active request.
class ContextCardArea extends ConsumerWidget {
  const ContextCardArea({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final id = ref.watch(playgroundProvider.select((s) => s.activeRequestId));
    final detail = id == null ? null : ref.watch(requestDetailProvider(id));
    final d = detail?.value;
    final kind = d == null ? null : _cardFor(d);

    final Widget card = switch (kind) {
      null => const SizedBox(width: double.infinity, key: ValueKey('none')),
      _Approve(:final payment) => _ApproveCard(
        key: ValueKey(kind.key),
        payment: payment,
      ),
      _Waiting() => _InfoCard(
        key: ValueKey(kind.key),
        color: const Color(0xFFFFC857),
        icon: Icons.hourglass_top_rounded,
        title: 'Waiting for finance approval',
        body: 'I’ll let you know the moment it’s decided.',
        spinner: true,
      ),
      _Ordered(:final detail) => _SuccessCard(
        key: ValueKey(kind.key),
        detail: detail,
      ),
      _Failed(:final request) => _InfoCard(
        key: ValueKey(kind.key),
        color: JarvisColors.red,
        icon: Icons.error_outline_rounded,
        title: switch (request.status) {
          RequestStatus.rejected => 'Request rejected',
          RequestStatus.cancelled => 'Request cancelled',
          _ => 'Order failed',
        },
        body: request.failureReason.isNotEmpty
            ? request.failureReason
            : request.status.label,
      ),
    };

    return AnimatedSize(
      duration: const Duration(milliseconds: 420),
      curve: Curves.easeOutCubic,
      alignment: Alignment.bottomCenter,
      child: AnimatedSwitcher(
        duration: const Duration(milliseconds: 520),
        switchInCurve: Curves.easeOutCubic,
        switchOutCurve: Curves.easeInCubic,
        layoutBuilder: (current, previous) => Stack(
          alignment: Alignment.bottomCenter,
          children: [...previous, ?current],
        ),
        transitionBuilder: (child, anim) => FadeTransition(
          opacity: anim,
          child: SlideTransition(
            position: Tween(
              begin: const Offset(0, 0.6),
              end: Offset.zero,
            ).animate(anim),
            child: child,
          ),
        ),
        child: card,
      ),
    );
  }
}

class _ApproveCard extends ConsumerStatefulWidget {
  const _ApproveCard({super.key, required this.payment});
  final Payment payment;

  @override
  ConsumerState<_ApproveCard> createState() => _ApproveCardState();
}

class _ApproveCardState extends ConsumerState<_ApproveCard>
    with SingleTickerProviderStateMixin {
  late final _glow = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1600),
  )..repeat(reverse: true);

  @override
  void initState() {
    super.initState();
    HapticFeedback.mediumImpact();
  }

  @override
  void dispose() {
    _glow.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final p = widget.payment;
    final amount = Fmt.money(p.finalCents ?? p.quotedCents);
    final merchant = p.merchantName.isEmpty ? 'merchant' : p.merchantName;
    return Glass(
      padding: const EdgeInsets.fromLTRB(18, 16, 18, 18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        mainAxisSize: MainAxisSize.min,
        children: [
          Row(
            children: [
              Icon(
                Icons.lock_outline_rounded,
                size: 15,
                color: Colors.white.withValues(alpha: 0.6),
              ),
              const SizedBox(width: 6),
              Text(
                'PAYMENT READY · TAP TO APPROVE IN REAP',
                style: TextStyle(
                  fontSize: 10.5,
                  letterSpacing: 1.6,
                  fontWeight: FontWeight.w600,
                  color: Colors.white.withValues(alpha: 0.6),
                ),
              ),
            ],
          ),
          const SizedBox(height: 12),
          AnimatedBuilder(
            animation: _glow,
            builder: (context, child) {
              final g = Curves.easeInOut.transform(_glow.value);
              return DecoratedBox(
                decoration: BoxDecoration(
                  borderRadius: BorderRadius.circular(18),
                  boxShadow: [
                    BoxShadow(
                      color: JarvisColors.cyan.withValues(
                        alpha: 0.25 + 0.3 * g,
                      ),
                      blurRadius: 18 + 16 * g,
                      spreadRadius: -2,
                    ),
                  ],
                ),
                child: child,
              );
            },
            child: Material(
              color: Colors.transparent,
              child: Ink(
                decoration: BoxDecoration(
                  borderRadius: BorderRadius.circular(18),
                  gradient: const LinearGradient(
                    colors: [JarvisColors.cyan, JarvisColors.blue],
                  ),
                ),
                child: InkWell(
                  borderRadius: BorderRadius.circular(18),
                  onTap: () {
                    HapticFeedback.heavyImpact();
                    ref.read(urlOpenerProvider)(p.approvalUrl);
                  },
                  child: Padding(
                    padding: const EdgeInsets.symmetric(
                      vertical: 17,
                      horizontal: 16,
                    ),
                    child: Row(
                      mainAxisAlignment: MainAxisAlignment.center,
                      children: [
                        const Icon(
                          Icons.verified_user_rounded,
                          color: Color(0xFF02101F),
                          size: 22,
                        ),
                        const SizedBox(width: 10),
                        Flexible(
                          child: Text(
                            'Approve $amount · $merchant',
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(
                              color: Color(0xFF02101F),
                              fontSize: 17,
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _InfoCard extends StatelessWidget {
  const _InfoCard({
    super.key,
    required this.color,
    required this.icon,
    required this.title,
    required this.body,
    this.spinner = false,
  });

  final Color color;
  final IconData icon;
  final String title;
  final String body;
  final bool spinner;

  @override
  Widget build(BuildContext context) {
    return Glass(
      tint: color,
      tintAlpha: 0.1,
      glow: color,
      padding: const EdgeInsets.all(16),
      child: Row(
        children: [
          SizedBox(
            width: 40,
            height: 40,
            child: Stack(
              alignment: Alignment.center,
              children: [
                if (spinner)
                  CircularProgressIndicator(
                    strokeWidth: 1.6,
                    color: color.withValues(alpha: 0.8),
                  ),
                Icon(icon, color: color, size: spinner ? 18 : 26),
              ],
            ),
          ),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(
                  title,
                  style: const TextStyle(
                    color: Colors.white,
                    fontSize: 16,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                const SizedBox(height: 3),
                Text(
                  body,
                  maxLines: 3,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(
                    color: Colors.white.withValues(alpha: 0.7),
                    fontSize: 13.5,
                    height: 1.3,
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

class _SuccessCard extends StatefulWidget {
  const _SuccessCard({super.key, required this.detail});
  final RequestDetail detail;

  @override
  State<_SuccessCard> createState() => _SuccessCardState();
}

class _SuccessCardState extends State<_SuccessCard>
    with SingleTickerProviderStateMixin {
  late final _c = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1100),
  )..forward();

  @override
  void initState() {
    super.initState();
    HapticFeedback.heavyImpact();
  }

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final d = widget.detail;
    final merchants = {
      for (final p in d.payments)
        if (p.merchantName.isNotEmpty) p.merchantName,
    };
    final paid = d.payments.fold<int>(
      0,
      (sum, p) => sum + (p.finalCents ?? p.quotedCents),
    );
    final total = paid > 0 ? paid : d.request.totalCents;
    final sub = [
      if (total > 0) Fmt.money(total),
      if (merchants.isNotEmpty) merchants.join(', '),
    ].join(' · ');
    return Glass(
      tint: JarvisColors.green,
      tintAlpha: 0.1,
      glow: JarvisColors.green,
      padding: const EdgeInsets.all(16),
      child: Row(
        children: [
          SizedBox(
            width: 46,
            height: 46,
            child: AnimatedBuilder(
              animation: _c,
              builder: (_, _) => CustomPaint(painter: _CheckPainter(_c.value)),
            ),
          ),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                const Text(
                  'Order placed',
                  style: TextStyle(
                    color: Colors.white,
                    fontSize: 16,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                const SizedBox(height: 3),
                Text(
                  sub.isEmpty ? 'Paid and on its way.' : '$sub · on its way',
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(
                    color: Colors.white.withValues(alpha: 0.7),
                    fontSize: 13.5,
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

/// Circle draws in, then the tick strokes through.
class _CheckPainter extends CustomPainter {
  _CheckPainter(this.t);
  final double t;

  @override
  void paint(Canvas canvas, Size size) {
    final c = size.center(Offset.zero);
    final r = size.shortestSide / 2 - 2;
    final ring = Curves.easeOutCubic.transform((t / 0.55).clamp(0.0, 1.0));
    final tick = Curves.easeOutBack.transform(
      ((t - 0.45) / 0.55).clamp(0.0, 1.0),
    );
    canvas.drawCircle(
      c,
      r,
      Paint()
        ..color = JarvisColors.green.withValues(alpha: 0.18 * ring)
        ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 6),
    );
    final stroke = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = 2.4
      ..strokeCap = StrokeCap.round
      ..color = JarvisColors.green;
    canvas.drawArc(
      Rect.fromCircle(center: c, radius: r),
      -math.pi / 2,
      2 * math.pi * ring,
      false,
      stroke,
    );
    if (tick <= 0) return;
    final path = Path()
      ..moveTo(c.dx - r * 0.42, c.dy + r * 0.02)
      ..lineTo(c.dx - r * 0.1, c.dy + r * 0.34)
      ..lineTo(c.dx + r * 0.46, c.dy - r * 0.3);
    for (final m in path.computeMetrics()) {
      canvas.drawPath(
        m.extractPath(0, m.length * tick.clamp(0.0, 1.0)),
        stroke..strokeWidth = 3,
      );
    }
  }

  @override
  bool shouldRepaint(_CheckPainter old) => old.t != t;
}
