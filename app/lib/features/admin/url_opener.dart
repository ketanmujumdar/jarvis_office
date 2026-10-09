import 'url_opener_io.dart'
    if (dart.library.js_interop) 'url_opener_web.dart'
    as impl;

/// Opens a URL in a new browser tab (web). Returns false where unsupported.
class UrlOpener {
  const UrlOpener();

  Future<bool> open(String url) async => impl.openUrl(url);
}
