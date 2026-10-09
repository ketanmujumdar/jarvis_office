import 'package:web/web.dart' as web;

bool openUrl(String url) {
  web.window.open(url, '_blank', 'noopener');
  return true;
}
