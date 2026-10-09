import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:jarvis_office/core/theme/app_theme.dart';

/// Wraps [child] in a themed MaterialApp for widget tests (no network fonts).
Widget themed(Widget child, {Brightness brightness = Brightness.light}) {
  AppTheme.useGoogleFonts = false;
  return MaterialApp(
    theme: AppTheme.light(),
    darkTheme: AppTheme.dark(),
    themeMode: brightness == Brightness.dark ? ThemeMode.dark : ThemeMode.light,
    home: child,
  );
}

/// Sets the logical test surface size for this test.
void setSurface(WidgetTester tester, Size size) {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
}
