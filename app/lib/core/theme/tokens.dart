import 'package:flutter/material.dart';

/// Design tokens for Jarvis Office. Use these instead of literal values so the
/// light and dark themes stay consistent.
///
/// Spacing follows a 4pt scale; radii are soft; colours are a slate neutral
/// ramp with an indigo brand and four semantic tones.
abstract final class AppSpace {
  static const double xxs = 2;
  static const double xs = 4;
  static const double sm = 8;
  static const double md = 12;
  static const double lg = 16;
  static const double xl = 24;
  static const double xxl = 32;
  static const double xxxl = 48;

  /// Max width for reading-heavy page content on wide screens.
  static const double contentMaxWidth = 1200;
}

abstract final class AppRadius {
  static const double sm = 8;
  static const double md = 12;
  static const double lg = 16;
  static const double xl = 24;
  static const double pill = 999;

  static const BorderRadius smAll = BorderRadius.all(Radius.circular(sm));
  static const BorderRadius mdAll = BorderRadius.all(Radius.circular(md));
  static const BorderRadius lgAll = BorderRadius.all(Radius.circular(lg));
  static const BorderRadius pillAll = BorderRadius.all(Radius.circular(pill));
}

/// Breakpoints for responsive layout.
abstract final class AppBreakpoints {
  /// Below this: bottom navigation, single column.
  static const double compact = 720;

  /// Above this: extended navigation rail with labels.
  static const double expanded = 1200;
}

/// Raw palette. Prefer semantic access via [JarvisColors] / ColorScheme.
abstract final class AppPalette {
  // Brand (indigo).
  static const brand50 = Color(0xFFEEF2FF);
  static const brand100 = Color(0xFFE0E7FF);
  static const brand300 = Color(0xFFA5B4FC);
  static const brand400 = Color(0xFF818CF8);
  static const brand500 = Color(0xFF6366F1);
  static const brand600 = Color(0xFF4F46E5);
  static const brand700 = Color(0xFF4338CA);
  static const brand900 = Color(0xFF1E1B4B);

  // Neutrals (slate).
  static const slate0 = Color(0xFFFFFFFF);
  static const slate25 = Color(0xFFF8FAFC);
  static const slate50 = Color(0xFFF1F5F9);
  static const slate100 = Color(0xFFE2E8F0);
  static const slate200 = Color(0xFFCBD5E1);
  static const slate400 = Color(0xFF94A3B8);
  static const slate500 = Color(0xFF64748B);
  static const slate600 = Color(0xFF475569);
  static const slate700 = Color(0xFF334155);
  static const slate800 = Color(0xFF1E293B);
  static const slate850 = Color(0xFF172033);
  static const slate900 = Color(0xFF0F172A);
  static const slate950 = Color(0xFF0A0F1C);

  // Semantic.
  static const green600 = Color(0xFF059669);
  static const green400 = Color(0xFF34D399);
  static const amber600 = Color(0xFFD97706);
  static const amber400 = Color(0xFFFBBF24);
  static const red600 = Color(0xFFDC2626);
  static const red400 = Color(0xFFF87171);
  static const sky600 = Color(0xFF0284C7);
  static const sky400 = Color(0xFF38BDF8);
}

/// Semantic tone used by chips, banners and stat deltas.
enum Tone { neutral, brand, info, success, warning, danger }

/// Theme extension carrying semantic colours that Material's ColorScheme
/// does not model (success/warning/info + subtle backgrounds + borders).
@immutable
class JarvisColors extends ThemeExtension<JarvisColors> {
  const JarvisColors({
    required this.background,
    required this.surface,
    required this.surfaceMuted,
    required this.border,
    required this.borderStrong,
    required this.textPrimary,
    required this.textSecondary,
    required this.textMuted,
    required this.brand,
    required this.brandSubtle,
    required this.success,
    required this.successSubtle,
    required this.warning,
    required this.warningSubtle,
    required this.danger,
    required this.dangerSubtle,
    required this.info,
    required this.infoSubtle,
    required this.neutral,
    required this.neutralSubtle,
    required this.chartPalette,
  });

  final Color background;
  final Color surface;
  final Color surfaceMuted;
  final Color border;
  final Color borderStrong;
  final Color textPrimary;
  final Color textSecondary;
  final Color textMuted;
  final Color brand;
  final Color brandSubtle;
  final Color success;
  final Color successSubtle;
  final Color warning;
  final Color warningSubtle;
  final Color danger;
  final Color dangerSubtle;
  final Color info;
  final Color infoSubtle;
  final Color neutral;
  final Color neutralSubtle;

  /// Ordered series colours for charts.
  final List<Color> chartPalette;

  static const light = JarvisColors(
    background: AppPalette.slate25,
    surface: AppPalette.slate0,
    surfaceMuted: AppPalette.slate50,
    border: AppPalette.slate100,
    borderStrong: AppPalette.slate200,
    textPrimary: AppPalette.slate900,
    textSecondary: AppPalette.slate600,
    textMuted: AppPalette.slate500,
    brand: AppPalette.brand600,
    brandSubtle: AppPalette.brand50,
    success: AppPalette.green600,
    successSubtle: Color(0xFFECFDF5),
    warning: AppPalette.amber600,
    warningSubtle: Color(0xFFFFFBEB),
    danger: AppPalette.red600,
    dangerSubtle: Color(0xFFFEF2F2),
    info: AppPalette.sky600,
    infoSubtle: Color(0xFFF0F9FF),
    neutral: AppPalette.slate600,
    neutralSubtle: AppPalette.slate50,
    chartPalette: [
      AppPalette.brand600,
      AppPalette.sky600,
      AppPalette.green600,
      AppPalette.amber600,
      Color(0xFFDB2777),
    ],
  );

  static const dark = JarvisColors(
    background: AppPalette.slate950,
    surface: AppPalette.slate900,
    surfaceMuted: AppPalette.slate850,
    border: AppPalette.slate800,
    borderStrong: AppPalette.slate700,
    textPrimary: AppPalette.slate25,
    textSecondary: AppPalette.slate200,
    textMuted: AppPalette.slate400,
    brand: AppPalette.brand400,
    brandSubtle: Color(0xFF1E1B4B),
    success: AppPalette.green400,
    successSubtle: Color(0xFF052E26),
    warning: AppPalette.amber400,
    warningSubtle: Color(0xFF3A2A06),
    danger: AppPalette.red400,
    dangerSubtle: Color(0xFF3B1010),
    info: AppPalette.sky400,
    infoSubtle: Color(0xFF082F49),
    neutral: AppPalette.slate400,
    neutralSubtle: AppPalette.slate850,
    chartPalette: [
      AppPalette.brand400,
      AppPalette.sky400,
      AppPalette.green400,
      AppPalette.amber400,
      Color(0xFFF472B6),
    ],
  );

  /// Foreground colour for a tone.
  Color fg(Tone tone) => switch (tone) {
    Tone.neutral => neutral,
    Tone.brand => brand,
    Tone.info => info,
    Tone.success => success,
    Tone.warning => warning,
    Tone.danger => danger,
  };

  /// Subtle background colour for a tone.
  Color bg(Tone tone) => switch (tone) {
    Tone.neutral => neutralSubtle,
    Tone.brand => brandSubtle,
    Tone.info => infoSubtle,
    Tone.success => successSubtle,
    Tone.warning => warningSubtle,
    Tone.danger => dangerSubtle,
  };

  @override
  JarvisColors copyWith({Color? brand}) => JarvisColors(
    background: background,
    surface: surface,
    surfaceMuted: surfaceMuted,
    border: border,
    borderStrong: borderStrong,
    textPrimary: textPrimary,
    textSecondary: textSecondary,
    textMuted: textMuted,
    brand: brand ?? this.brand,
    brandSubtle: brandSubtle,
    success: success,
    successSubtle: successSubtle,
    warning: warning,
    warningSubtle: warningSubtle,
    danger: danger,
    dangerSubtle: dangerSubtle,
    info: info,
    infoSubtle: infoSubtle,
    neutral: neutral,
    neutralSubtle: neutralSubtle,
    chartPalette: chartPalette,
  );

  @override
  JarvisColors lerp(ThemeExtension<JarvisColors>? other, double t) {
    if (other is! JarvisColors) return this;
    Color l(Color a, Color b) => Color.lerp(a, b, t)!;
    return JarvisColors(
      background: l(background, other.background),
      surface: l(surface, other.surface),
      surfaceMuted: l(surfaceMuted, other.surfaceMuted),
      border: l(border, other.border),
      borderStrong: l(borderStrong, other.borderStrong),
      textPrimary: l(textPrimary, other.textPrimary),
      textSecondary: l(textSecondary, other.textSecondary),
      textMuted: l(textMuted, other.textMuted),
      brand: l(brand, other.brand),
      brandSubtle: l(brandSubtle, other.brandSubtle),
      success: l(success, other.success),
      successSubtle: l(successSubtle, other.successSubtle),
      warning: l(warning, other.warning),
      warningSubtle: l(warningSubtle, other.warningSubtle),
      danger: l(danger, other.danger),
      dangerSubtle: l(dangerSubtle, other.dangerSubtle),
      info: l(info, other.info),
      infoSubtle: l(infoSubtle, other.infoSubtle),
      neutral: l(neutral, other.neutral),
      neutralSubtle: l(neutralSubtle, other.neutralSubtle),
      chartPalette: t < 0.5 ? chartPalette : other.chartPalette,
    );
  }
}

/// Convenience accessors: `context.jc.success`, `context.tt.titleLarge`.
extension JarvisThemeContext on BuildContext {
  JarvisColors get jc => Theme.of(this).extension<JarvisColors>()!;
  TextTheme get tt => Theme.of(this).textTheme;
  ColorScheme get cs => Theme.of(this).colorScheme;
}
