import 'package:web/web.dart' as web;

/// Opens the URL in a new tab. `noopener` keeps the Reap page isolated.
bool open(String url) {
  web.window.open(url, '_blank', 'noopener');
  return true;
}
