import 'dart:async';

import 'package:url_launcher/url_launcher.dart';

/// Opens an http(s) URL in Safari (outside the app), so the Reap approval
/// page behaves like it does in a normal browser. Matches the synchronous
/// `UrlOpener` signature: returns false only for URLs it will not open.
bool openInSafari(String url) {
  final uri = Uri.tryParse(url);
  if (uri == null || !(uri.isScheme('https') || uri.isScheme('http'))) {
    return false;
  }
  unawaited(
    launchUrl(uri, mode: LaunchMode.externalApplication).catchError((_) {
      return launchUrl(uri, mode: LaunchMode.inAppBrowserView);
    }),
  );
  return true;
}
