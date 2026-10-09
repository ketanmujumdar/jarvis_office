import 'package:flutter/material.dart';
import 'package:google_fonts/google_fonts.dart';

import 'tokens.dart';

/// Builds the light and dark [ThemeData] for the app.
///
/// Typography is Inter (via google_fonts). Tests set [AppTheme.useGoogleFonts]
/// to false so no network font fetch happens.
abstract final class AppTheme {
  static bool useGoogleFonts = true;

  static ThemeData light() => _build(Brightness.light, JarvisColors.light);
  static ThemeData dark() => _build(Brightness.dark, JarvisColors.dark);

  /// Tabular figures so money columns align.
  static const List<FontFeature> tabular = [FontFeature.tabularFigures()];

  static TextTheme _textTheme(JarvisColors c) {
    const base = TextTheme(
      displaySmall: TextStyle(
        fontSize: 36,
        height: 1.15,
        fontWeight: FontWeight.w700,
        letterSpacing: -0.8,
      ),
      headlineMedium: TextStyle(
        fontSize: 28,
        height: 1.2,
        fontWeight: FontWeight.w700,
        letterSpacing: -0.5,
      ),
      headlineSmall: TextStyle(
        fontSize: 22,
        height: 1.25,
        fontWeight: FontWeight.w600,
        letterSpacing: -0.3,
      ),
      titleLarge: TextStyle(
        fontSize: 18,
        height: 1.3,
        fontWeight: FontWeight.w600,
        letterSpacing: -0.2,
      ),
      titleMedium: TextStyle(
        fontSize: 15,
        height: 1.35,
        fontWeight: FontWeight.w600,
      ),
      titleSmall: TextStyle(
        fontSize: 13,
        height: 1.35,
        fontWeight: FontWeight.w600,
      ),
      bodyLarge: TextStyle(
        fontSize: 15,
        height: 1.5,
        fontWeight: FontWeight.w400,
      ),
      bodyMedium: TextStyle(
        fontSize: 14,
        height: 1.45,
        fontWeight: FontWeight.w400,
      ),
      bodySmall: TextStyle(
        fontSize: 12,
        height: 1.4,
        fontWeight: FontWeight.w400,
      ),
      labelLarge: TextStyle(
        fontSize: 14,
        height: 1.2,
        fontWeight: FontWeight.w600,
      ),
      labelMedium: TextStyle(
        fontSize: 12,
        height: 1.2,
        fontWeight: FontWeight.w600,
        letterSpacing: 0.1,
      ),
      labelSmall: TextStyle(
        fontSize: 11,
        height: 1.2,
        fontWeight: FontWeight.w600,
        letterSpacing: 0.4,
      ),
    );
    final colored = base
        .apply(bodyColor: c.textPrimary, displayColor: c.textPrimary)
        .copyWith(
          bodySmall: base.bodySmall!.copyWith(color: c.textSecondary),
          labelSmall: base.labelSmall!.copyWith(color: c.textMuted),
        );
    return useGoogleFonts ? GoogleFonts.interTextTheme(colored) : colored;
  }

  static ThemeData _build(Brightness b, JarvisColors c) {
    final scheme =
        ColorScheme.fromSeed(
          seedColor: AppPalette.brand600,
          brightness: b,
        ).copyWith(
          primary: c.brand,
          onPrimary: b == Brightness.light ? Colors.white : AppPalette.brand900,
          primaryContainer: c.brandSubtle,
          onPrimaryContainer: c.brand,
          surface: c.surface,
          onSurface: c.textPrimary,
          onSurfaceVariant: c.textSecondary,
          surfaceContainerLowest: c.background,
          surfaceContainerLow: c.surfaceMuted,
          surfaceContainer: c.surfaceMuted,
          outline: c.borderStrong,
          outlineVariant: c.border,
          error: c.danger,
        );
    final text = _textTheme(c);

    OutlineInputBorder inputBorder(Color color, [double width = 1]) =>
        OutlineInputBorder(
          borderRadius: AppRadius.mdAll,
          borderSide: BorderSide(color: color, width: width),
        );

    const buttonPadding = EdgeInsets.symmetric(
      horizontal: AppSpace.lg,
      vertical: AppSpace.md,
    );
    const buttonShape = RoundedRectangleBorder(borderRadius: AppRadius.mdAll);

    return ThemeData(
      useMaterial3: true,
      brightness: b,
      colorScheme: scheme,
      extensions: [c],
      scaffoldBackgroundColor: c.background,
      canvasColor: c.background,
      textTheme: text,
      dividerTheme: DividerThemeData(color: c.border, thickness: 1, space: 1),
      cardTheme: CardThemeData(
        color: c.surface,
        elevation: 0,
        margin: EdgeInsets.zero,
        shape: RoundedRectangleBorder(
          borderRadius: AppRadius.lgAll,
          side: BorderSide(color: c.border),
        ),
      ),
      appBarTheme: AppBarTheme(
        backgroundColor: c.surface,
        foregroundColor: c.textPrimary,
        elevation: 0,
        scrolledUnderElevation: 0,
        centerTitle: false,
        titleTextStyle: text.titleLarge,
        shape: Border(bottom: BorderSide(color: c.border)),
      ),
      navigationRailTheme: NavigationRailThemeData(
        backgroundColor: c.surface,
        indicatorColor: c.brandSubtle,
        selectedIconTheme: IconThemeData(color: c.brand),
        unselectedIconTheme: IconThemeData(color: c.textMuted),
        selectedLabelTextStyle: text.labelLarge?.copyWith(color: c.brand),
        unselectedLabelTextStyle: text.labelLarge?.copyWith(
          color: c.textSecondary,
        ),
        indicatorShape: const RoundedRectangleBorder(
          borderRadius: AppRadius.mdAll,
        ),
      ),
      navigationBarTheme: NavigationBarThemeData(
        backgroundColor: c.surface,
        indicatorColor: c.brandSubtle,
        elevation: 0,
        labelTextStyle: WidgetStatePropertyAll(text.labelSmall),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          padding: buttonPadding,
          shape: buttonShape,
          textStyle: text.labelLarge,
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          padding: buttonPadding,
          shape: buttonShape,
          textStyle: text.labelLarge,
          side: BorderSide(color: c.borderStrong),
          foregroundColor: c.textPrimary,
        ),
      ),
      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(
          padding: buttonPadding,
          shape: buttonShape,
          textStyle: text.labelLarge,
        ),
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: c.surface,
        isDense: true,
        contentPadding: const EdgeInsets.symmetric(
          horizontal: AppSpace.md,
          vertical: AppSpace.md,
        ),
        border: inputBorder(c.borderStrong),
        enabledBorder: inputBorder(c.borderStrong),
        focusedBorder: inputBorder(c.brand, 1.5),
        errorBorder: inputBorder(c.danger),
        labelStyle: text.bodyMedium?.copyWith(color: c.textSecondary),
        hintStyle: text.bodyMedium?.copyWith(color: c.textMuted),
      ),
      chipTheme: ChipThemeData(
        backgroundColor: c.surfaceMuted,
        side: BorderSide(color: c.border),
        shape: const RoundedRectangleBorder(borderRadius: AppRadius.pillAll),
        labelStyle: text.labelMedium,
      ),
      dataTableTheme: DataTableThemeData(
        headingTextStyle: text.labelMedium?.copyWith(color: c.textSecondary),
        dataTextStyle: text.bodyMedium,
        dividerThickness: 1,
        headingRowColor: WidgetStatePropertyAll(c.surfaceMuted),
      ),
      tooltipTheme: TooltipThemeData(
        decoration: BoxDecoration(
          color: AppPalette.slate900,
          borderRadius: AppRadius.smAll,
        ),
        textStyle: text.bodySmall?.copyWith(color: Colors.white),
      ),
      snackBarTheme: SnackBarThemeData(
        behavior: SnackBarBehavior.floating,
        shape: const RoundedRectangleBorder(borderRadius: AppRadius.mdAll),
        backgroundColor: b == Brightness.light
            ? AppPalette.slate900
            : AppPalette.slate100,
        contentTextStyle: text.bodyMedium?.copyWith(
          color: b == Brightness.light ? Colors.white : AppPalette.slate900,
        ),
      ),
      dialogTheme: DialogThemeData(
        backgroundColor: c.surface,
        shape: const RoundedRectangleBorder(borderRadius: AppRadius.lgAll),
      ),
      tabBarTheme: TabBarThemeData(
        labelColor: c.brand,
        unselectedLabelColor: c.textSecondary,
        indicatorColor: c.brand,
        labelStyle: text.labelLarge,
        dividerColor: c.border,
      ),
    );
  }
}
