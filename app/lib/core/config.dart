/// Build-time configuration. Override with
/// `flutter run --dart-define=API_BASE_URL=http://localhost:8080`.
class AppConfig {
  const AppConfig._();

  static const String apiBaseUrl = String.fromEnvironment(
    'API_BASE_URL',
    defaultValue: 'http://localhost:8080',
  );

  /// Currency for all money in the app. Amounts are integer cents.
  static const String currency = 'SGD';
}
