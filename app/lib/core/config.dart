/// Build-time configuration. Override with
/// `flutter run --dart-define=API_BASE_URL=http://localhost:8080`.
class AppConfig {
  const AppConfig._();

  static const String _apiBaseUrl = String.fromEnvironment(
    'API_BASE_URL',
    defaultValue: 'http://localhost:8080',
  );

  /// `--dart-define=API_BASE_URL=same-origin` calls the api on the page's own
  /// origin (demo tunnel routes /api/* to the backend).
  static String get apiBaseUrl =>
      _apiBaseUrl == 'same-origin' ? Uri.base.origin : _apiBaseUrl;

  /// Currency for all money in the app. Amounts are integer cents.
  static const String currency = 'SGD';
}
