import 'models.dart';
import 'sse_io.dart' if (dart.library.js_interop) 'sse_web.dart' as impl;

/// Opens the SSE stream `GET /api/v1/events`.
///
/// On web this uses the browser EventSource (token passed as `?token=` since
/// EventSource cannot set headers). Elsewhere it streams over HTTP. The stream
/// reconnects automatically on web; callers should re-fetch state on errors.
/// Cancel the subscription to close the connection.
Stream<SseEvent> openEventStream({
  required String baseUrl,
  String? token,
  String? requestId,
}) {
  final uri = Uri.parse('$baseUrl/api/v1/events')
      .replace(queryParameters: {'request_id': ?requestId, 'token': ?token});
  return impl.connect(uri, token: token);
}

/// Parses one SSE frame (lines up to a blank line). Returns null for comments
/// or frames without data. Exposed for tests.
SseEvent? parseSseFrame(String frame) => impl.parseFrame(frame);
