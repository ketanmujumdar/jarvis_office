import 'url_opener_stub.dart'
    if (dart.library.js_interop) 'url_opener_web.dart'
    as impl;

/// Opens [url] in a new browser tab (web) or does nothing elsewhere.
/// Returns false if the URL could not be opened.
typedef UrlOpener = bool Function(String url);

/// Platform implementation used by `urlOpenerProvider`.
bool openExternalUrl(String url) {
  final uri = Uri.tryParse(url);
  if (uri == null || !(uri.isScheme('https') || uri.isScheme('http'))) {
    return false;
  }
  return impl.open(uri.toString());
}
