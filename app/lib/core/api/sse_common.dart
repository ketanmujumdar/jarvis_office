import 'dart:convert';

import 'models.dart';

/// Parses one SSE frame; the `data:` payload is the full event JSON.
SseEvent? parseSseFrameCommon(String frame) {
  final data = StringBuffer();
  for (final line in const LineSplitter().convert(frame)) {
    if (line.startsWith(':')) continue;
    if (line.startsWith('data:')) {
      if (data.isNotEmpty) data.write('\n');
      data.write(line.substring(5).trimLeft());
    }
  }
  if (data.isEmpty) return null;
  try {
    return SseEvent.fromJson(jsonDecode(data.toString()) as Json);
  } on FormatException {
    return null;
  }
}
