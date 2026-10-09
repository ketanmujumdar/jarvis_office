import 'package:flutter/material.dart';

import '../../../core/theme/tokens.dart';
import '../voice_controller.dart';

/// The big round voice button. Glows and breathes while the session is live.
class MicOrb extends StatefulWidget {
  const MicOrb({
    super.key,
    required this.status,
    required this.onPressed,
    this.size = 88,
  });

  final VoiceStatus status;
  final VoidCallback? onPressed;
  final double size;

  @override
  State<MicOrb> createState() => _MicOrbState();
}

class _MicOrbState extends State<MicOrb> with SingleTickerProviderStateMixin {
  AnimationController? _c;

  @override
  void initState() {
    super.initState();
    _sync();
  }

  @override
  void didUpdateWidget(MicOrb old) {
    super.didUpdateWidget(old);
    if (old.status != widget.status) _sync();
  }

  void _sync() {
    final reduceMotion = WidgetsBinding
        .instance
        .platformDispatcher
        .accessibilityFeatures
        .disableAnimations;
    final animate =
        !reduceMotion &&
        (widget.status.isLive || widget.status == VoiceStatus.connecting);
    if (animate) {
      _c ??= AnimationController(
        vsync: this,
        duration: const Duration(milliseconds: 1400),
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
    final jc = context.jc;
    final s = widget.status;
    final live = s.isLive;
    final tone = switch (s) {
      VoiceStatus.error => Tone.danger,
      VoiceStatus.userSpeaking => Tone.success,
      VoiceStatus.speaking => Tone.info,
      _ => Tone.brand,
    };
    final color = jc.fg(tone);
    final icon = switch (s) {
      VoiceStatus.idle || VoiceStatus.error => Icons.mic_rounded,
      VoiceStatus.connecting => Icons.more_horiz_rounded,
      VoiceStatus.speaking => Icons.graphic_eq_rounded,
      VoiceStatus.thinking => Icons.auto_awesome_rounded,
      _ => Icons.stop_rounded,
    };
    final size = widget.size;

    Widget ring(double t) => Container(
      width: size + 28 * t,
      height: size + 28 * t,
      decoration: BoxDecoration(
        shape: BoxShape.circle,
        color: color.withValues(alpha: 0.10 * (1 - t) + 0.04),
      ),
    );

    final core = Material(
      shape: const CircleBorder(),
      clipBehavior: Clip.antiAlias,
      elevation: live ? 6 : 2,
      shadowColor: color.withValues(alpha: 0.5),
      child: Ink(
        width: size,
        height: size,
        decoration: BoxDecoration(
          shape: BoxShape.circle,
          gradient: LinearGradient(
            begin: Alignment.topLeft,
            end: Alignment.bottomRight,
            colors: [Color.lerp(color, Colors.white, 0.18)!, color],
          ),
        ),
        child: InkWell(
          key: const ValueKey('mic-orb'),
          onTap: widget.onPressed,
          customBorder: const CircleBorder(),
          child: Icon(icon, color: Colors.white, size: size * 0.4),
        ),
      ),
    );

    final c = _c;
    return Semantics(
      button: true,
      label: live ? 'Stop voice session' : 'Start voice session',
      child: SizedBox(
        width: size + 32,
        height: size + 32,
        child: Stack(
          alignment: Alignment.center,
          children: [
            if (c != null)
              AnimatedBuilder(
                animation: c,
                builder: (_, _) => ring(Curves.easeInOut.transform(c.value)),
              )
            else if (live)
              ring(0.5),
            core,
          ],
        ),
      ),
    );
  }
}
