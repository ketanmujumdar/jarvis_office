import 'dart:math' as math;
import 'dart:ui' as ui;

import 'package:flutter/material.dart';

import '../features/voice/voice_controller.dart';

/// Palette shared by the Jarvis screen.
abstract final class JarvisColors {
  static const cyan = Color(0xFF3EE7FF);
  static const blue = Color(0xFF2F6BFF);
  static const violet = Color(0xFF8B5CFF);
  static const magenta = Color(0xFFD06BFF);
  static const red = Color(0xFFFF4D6D);
  static const green = Color(0xFF3DF5A8);
  static const navy = Color(0xFF0A1330);
  static const night = Color(0xFF02040B);
}

/// Every visual parameter of the orb. The screen lerps between these so
/// state changes glide rather than snap.
@immutable
class OrbLook {
  const OrbLook({
    required this.inner,
    required this.outer,
    required this.accent,
    required this.brightness,
    required this.pulseAmp,
    required this.pulseHz,
    required this.rhythm,
    required this.ringSpeed,
    required this.swirlSpeed,
    required this.particles,
    required this.glow,
  });

  /// Core colour (centre of the sphere).
  final Color inner;

  /// Rim colour.
  final Color outer;

  /// Rings, ticks and particles.
  final Color accent;

  /// 0..1 overall intensity.
  final double brightness;

  /// Scale wobble (fraction of radius).
  final double pulseAmp;
  final double pulseHz;

  /// 0..1: how much of the pulse is the irregular "speech" rhythm.
  final double rhythm;

  /// Ring rotation, radians per second.
  final double ringSpeed;

  /// Inner swirl rotation, radians per second.
  final double swirlSpeed;

  /// 0..1 opacity of the orbiting particles.
  final double particles;

  /// 0..1 strength of the outer halo.
  final double glow;

  static OrbLook forStatus(VoiceStatus s) => switch (s) {
    VoiceStatus.idle => const OrbLook(
      inner: JarvisColors.cyan,
      outer: JarvisColors.blue,
      accent: JarvisColors.cyan,
      brightness: 0.42,
      pulseAmp: 0.035,
      pulseHz: 0.22,
      rhythm: 0,
      ringSpeed: 0.06,
      swirlSpeed: 0.25,
      particles: 0,
      glow: 0.35,
    ),
    VoiceStatus.connecting => const OrbLook(
      inner: JarvisColors.cyan,
      outer: JarvisColors.blue,
      accent: Color(0xFF7FB2FF),
      brightness: 0.62,
      pulseAmp: 0.03,
      pulseHz: 0.8,
      rhythm: 0,
      ringSpeed: 2.4,
      swirlSpeed: 1.6,
      particles: 0.15,
      glow: 0.5,
    ),
    VoiceStatus.listening => const OrbLook(
      inner: Color(0xFFB8FBFF),
      outer: JarvisColors.cyan,
      accent: JarvisColors.cyan,
      brightness: 0.8,
      pulseAmp: 0.055,
      pulseHz: 0.9,
      rhythm: 0,
      ringSpeed: 0.35,
      swirlSpeed: 0.7,
      particles: 0.1,
      glow: 0.7,
    ),
    VoiceStatus.userSpeaking => const OrbLook(
      inner: Color(0xFFDFFEFF),
      outer: JarvisColors.cyan,
      accent: JarvisColors.cyan,
      brightness: 0.92,
      pulseAmp: 0.08,
      pulseHz: 1.6,
      rhythm: 0.55,
      ringSpeed: 0.6,
      swirlSpeed: 1.1,
      particles: 0.2,
      glow: 0.85,
    ),
    VoiceStatus.thinking => const OrbLook(
      inner: Color(0xFFE2D4FF),
      outer: JarvisColors.violet,
      accent: JarvisColors.magenta,
      brightness: 0.78,
      pulseAmp: 0.03,
      pulseHz: 0.6,
      rhythm: 0,
      ringSpeed: 0.9,
      swirlSpeed: 2.4,
      particles: 1,
      glow: 0.7,
    ),
    VoiceStatus.speaking => const OrbLook(
      inner: Colors.white,
      outer: Color(0xFF52A8FF),
      accent: Color(0xFF8FE9FF),
      brightness: 1,
      pulseAmp: 0.1,
      pulseHz: 2.1,
      rhythm: 1,
      ringSpeed: 0.5,
      swirlSpeed: 1.3,
      particles: 0.35,
      glow: 1,
    ),
    VoiceStatus.error => const OrbLook(
      inner: Color(0xFFFFB3C0),
      outer: JarvisColors.red,
      accent: JarvisColors.red,
      brightness: 0.5,
      pulseAmp: 0.025,
      pulseHz: 0.3,
      rhythm: 0,
      ringSpeed: 0.04,
      swirlSpeed: 0.2,
      particles: 0,
      glow: 0.4,
    ),
  };

  static OrbLook lerp(OrbLook a, OrbLook b, double t) {
    double d(double x, double y) => x + (y - x) * t;
    return OrbLook(
      inner: Color.lerp(a.inner, b.inner, t)!,
      outer: Color.lerp(a.outer, b.outer, t)!,
      accent: Color.lerp(a.accent, b.accent, t)!,
      brightness: d(a.brightness, b.brightness),
      pulseAmp: d(a.pulseAmp, b.pulseAmp),
      pulseHz: d(a.pulseHz, b.pulseHz),
      rhythm: d(a.rhythm, b.rhythm),
      ringSpeed: d(a.ringSpeed, b.ringSpeed),
      swirlSpeed: d(a.swirlSpeed, b.swirlSpeed),
      particles: d(a.particles, b.particles),
      glow: d(a.glow, b.glow),
    );
  }
}

/// Integrated animation state (phases accumulate so speed changes never
/// make the rings jump).
class OrbMotion extends ChangeNotifier {
  OrbMotion(VoiceStatus status) : look = OrbLook.forStatus(status);

  OrbLook look;
  double time = 0;
  double pulsePhase = 0;
  double ringPhase = 0;
  double swirlPhase = 0;
  double particlePhase = 0;

  void tick(double dt, OrbLook target) {
    // Exponential smoothing: ~90% of the way in 0.6 s.
    final k = 1 - math.exp(-dt * 3.8);
    look = OrbLook.lerp(look, target, k);
    time += dt;
    pulsePhase += dt * look.pulseHz * 2 * math.pi;
    ringPhase += dt * look.ringSpeed;
    swirlPhase += dt * look.swirlSpeed;
    particlePhase += dt * (0.6 + look.particles * 1.4);
    notifyListeners();
  }

  /// Current scale factor of the sphere (1 = rest).
  double get pulse {
    final smooth = math.sin(pulsePhase);
    // Irregular, speech-like envelope from incommensurate sines.
    final speech =
        (math.sin(time * 9.1).abs() * 0.55 +
                math.sin(time * 14.3 + 1.2).abs() * 0.3 +
                math.sin(time * 3.7).abs() * 0.4) /
            1.25 *
            2 -
        1;
    final wave = smooth * (1 - look.rhythm) + speech * look.rhythm;
    return 1 + wave * look.pulseAmp;
  }
}

/// The animated Jarvis orb. Repaints from [motion] only.
class OrbPainter extends CustomPainter {
  OrbPainter(this.motion) : super(repaint: motion);

  final OrbMotion motion;

  static const _ticks = 72;

  @override
  void paint(Canvas canvas, Size size) {
    final m = motion;
    final l = m.look;
    final c = size.center(Offset.zero);
    final base = size.shortestSide / 2 / 1.85; // leaves room for rings
    final r = base * m.pulse;
    final b = l.brightness.clamp(0.0, 1.0);

    // ---- outer halo
    final haloR = r * (1.9 + 0.25 * l.glow);
    canvas.drawCircle(
      c,
      haloR,
      Paint()
        ..shader = ui.Gradient.radial(
          c,
          haloR,
          [
            l.outer.withValues(alpha: 0.42 * l.glow * b),
            l.outer.withValues(alpha: 0.12 * l.glow * b),
            l.outer.withValues(alpha: 0),
          ],
          const [0.35, 0.62, 1],
        ),
    );

    // ---- HUD rings
    _rings(canvas, c, base, l, m.ringPhase, b);

    // ---- sphere body
    final body = Rect.fromCircle(center: c, radius: r);
    canvas.drawCircle(
      c,
      r,
      Paint()
        ..shader = ui.Gradient.radial(
          c + Offset(-r * 0.18, -r * 0.22),
          r * 1.15,
          [
            Color.lerp(l.inner, Colors.white, 0.3)!.withValues(alpha: 0.95 * b),
            l.inner.withValues(alpha: 0.85 * b),
            l.outer.withValues(alpha: 0.75 * b),
            JarvisColors.violet.withValues(alpha: 0.55 * b),
            JarvisColors.navy.withValues(alpha: 0.9),
          ],
          const [0, 0.18, 0.5, 0.8, 1],
        ),
    );

    // ---- rotating inner swirl (clipped to the sphere, additive)
    canvas.save();
    canvas.clipPath(Path()..addOval(body));
    canvas.translate(c.dx, c.dy);
    for (var i = 0; i < 3; i++) {
      final dir = i.isEven ? 1.0 : -1.0;
      canvas.save();
      canvas.rotate(m.swirlPhase * dir * (1 + i * 0.35) + i * 2.1);
      final w = r * (1.5 - i * 0.22);
      final h = r * (0.55 + i * 0.12);
      final oval = Rect.fromCenter(
        center: Offset(r * 0.08 * i, 0),
        width: w,
        height: h,
      );
      final colors = [
        JarvisColors.cyan,
        JarvisColors.blue,
        JarvisColors.violet,
      ];
      canvas.drawOval(
        oval,
        Paint()
          ..blendMode = BlendMode.plus
          ..maskFilter = MaskFilter.blur(BlurStyle.normal, r * 0.09)
          ..shader = SweepGradient(
            colors: [
              colors[i].withValues(alpha: 0),
              colors[i].withValues(alpha: 0.55 * b),
              Colors.white.withValues(alpha: 0.35 * b),
              colors[(i + 1) % 3].withValues(alpha: 0.45 * b),
              colors[i].withValues(alpha: 0),
            ],
          ).createShader(oval),
      );
      canvas.restore();
    }
    // Bright core.
    canvas.drawCircle(
      Offset.zero,
      r * 0.45,
      Paint()
        ..blendMode = BlendMode.plus
        ..shader = ui.Gradient.radial(Offset.zero, r * 0.45, [
          Colors.white.withValues(alpha: 0.55 * b * b),
          l.inner.withValues(alpha: 0),
        ]),
    );
    canvas.restore();

    // ---- rim light
    canvas.drawCircle(
      c,
      r,
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.4
        ..shader = SweepGradient(
          transform: GradientRotation(m.swirlPhase * 0.5),
          colors: [
            l.accent.withValues(alpha: 0.9 * b),
            JarvisColors.violet.withValues(alpha: 0.4 * b),
            l.accent.withValues(alpha: 0.1),
            JarvisColors.blue.withValues(alpha: 0.6 * b),
            l.accent.withValues(alpha: 0.9 * b),
          ],
        ).createShader(body),
    );
    // Specular highlight.
    final spec = c + Offset(-r * 0.36, -r * 0.42);
    canvas.drawCircle(
      spec,
      r * 0.28,
      Paint()
        ..shader = ui.Gradient.radial(spec, r * 0.28, [
          Colors.white.withValues(alpha: 0.32 * b),
          Colors.white.withValues(alpha: 0),
        ]),
    );

    // ---- orbiting particles (thinking)
    if (l.particles > 0.01) {
      final p = Paint()..blendMode = BlendMode.plus;
      for (var i = 0; i < 18; i++) {
        final seed = i * 1.618;
        final orbit = base * (1.12 + (i % 4) * 0.07);
        final speed = 0.6 + (i % 5) * 0.18;
        final a = m.particlePhase * speed * (i.isEven ? 1 : -0.8) + seed * 2.4;
        final tilt = 0.35 + (i % 3) * 0.12;
        final pos = c + Offset(math.cos(a) * orbit, math.sin(a) * orbit * tilt);
        final front = math.sin(a) > 0 ? 1.0 : 0.45;
        final rad = 1.2 + (i % 3) * 0.7;
        p
          ..color = l.accent.withValues(alpha: l.particles * 0.9 * front)
          ..maskFilter = MaskFilter.blur(BlurStyle.normal, rad * 1.4);
        canvas.drawCircle(pos, rad * 1.8, p);
        p
          ..color = Colors.white.withValues(alpha: l.particles * 0.9 * front)
          ..maskFilter = null;
        canvas.drawCircle(pos, rad * 0.6, p);
      }
    }
  }

  void _rings(
    Canvas canvas,
    Offset c,
    double base,
    OrbLook l,
    double phase,
    double b,
  ) {
    final specs = <({double radius, double dir, double alpha, int every})>[
      (radius: base * 1.32, dir: 1, alpha: 0.55, every: 2),
      (radius: base * 1.55, dir: -0.6, alpha: 0.35, every: 3),
      (radius: base * 1.78, dir: 0.35, alpha: 0.22, every: 6),
    ];
    for (final s in specs) {
      final rect = Rect.fromCircle(center: c, radius: s.radius);
      final rot = phase * s.dir;
      final alpha = s.alpha * (0.45 + 0.55 * b);
      final stroke = Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = 0.9
        ..color = l.accent.withValues(alpha: alpha);
      // Three broken arcs per ring.
      for (var k = 0; k < 3; k++) {
        final start = rot + k * (2 * math.pi / 3);
        canvas.drawArc(rect, start, 2 * math.pi / 3 * 0.72, false, stroke);
      }
      // Ticks.
      final tick = Paint()
        ..strokeWidth = 1
        ..color = l.accent.withValues(alpha: alpha * 0.9);
      for (var i = 0; i < _ticks; i += s.every) {
        final a = rot * 0.5 + i * 2 * math.pi / _ticks;
        final major = i % 18 == 0;
        final len = major ? 7.0 : 3.0;
        final dir = Offset(math.cos(a), math.sin(a));
        canvas.drawLine(
          c + dir * (s.radius + 2),
          c + dir * (s.radius + 2 + len),
          tick..strokeWidth = major ? 1.6 : 0.8,
        );
      }
      // Leading glint.
      final glintA = rot + 2 * math.pi / 3 * 0.72;
      canvas.drawCircle(
        c + Offset(math.cos(glintA), math.sin(glintA)) * s.radius,
        2,
        Paint()
          ..color = Colors.white.withValues(alpha: alpha * 1.4)
          ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 2),
      );
    }
  }

  @override
  bool shouldRepaint(OrbPainter old) => old.motion != motion;
}

/// Deep-space background: navy-to-black radial wash with a twinkling
/// starfield. Repaints from [motion] so stars shimmer.
class StarfieldPainter extends CustomPainter {
  StarfieldPainter(this.motion) : super(repaint: motion);

  final OrbMotion motion;

  static final List<({double x, double y, double r, double tw, double ph})>
  _stars = () {
    final rnd = math.Random(7);
    return List.generate(
      140,
      (_) => (
        x: rnd.nextDouble(),
        y: rnd.nextDouble(),
        r: rnd.nextDouble() < 0.12
            ? 1.1 + rnd.nextDouble() * 0.6
            : 0.35 + rnd.nextDouble() * 0.55,
        tw: 0.4 + rnd.nextDouble() * 1.6,
        ph: rnd.nextDouble() * math.pi * 2,
      ),
    );
  }();

  @override
  void paint(Canvas canvas, Size size) {
    final rect = Offset.zero & size;
    final focus = Offset(size.width / 2, size.height * 0.42);
    canvas.drawRect(
      rect,
      Paint()
        ..shader = ui.Gradient.radial(
          focus,
          size.longestSide * 0.75,
          const [Color(0xFF13214A), JarvisColors.navy, JarvisColors.night],
          const [0, 0.38, 1],
        ),
    );
    // Faint tint from the orb's current colour.
    final l = motion.look;
    canvas.drawRect(
      rect,
      Paint()
        ..shader = ui.Gradient.radial(focus, size.width * 0.9, [
          l.outer.withValues(alpha: 0.12 * l.glow),
          l.outer.withValues(alpha: 0),
        ]),
    );
    final p = Paint();
    final t = motion.time;
    for (final s in _stars) {
      final twinkle = 0.55 + 0.45 * math.sin(t * s.tw + s.ph);
      p.color = Colors.white.withValues(alpha: 0.12 + 0.5 * twinkle * s.r);
      canvas.drawCircle(Offset(s.x * size.width, s.y * size.height), s.r, p);
    }
  }

  @override
  bool shouldRepaint(StarfieldPainter old) => old.motion != motion;
}
