import 'dart:async';
import 'dart:convert';

import 'package:dio/dio.dart';

import 'models.dart';
import 'sse_common.dart';

SseEvent? parseFrame(String frame) => parseSseFrameCommon(frame);

Stream<SseEvent> connect(Uri uri, {String? token}) {
  late StreamController<SseEvent> controller;
  final cancel = CancelToken();
  controller = StreamController<SseEvent>(
    onListen: () async {
      try {
        final res = await Dio().getUri<ResponseBody>(
          uri,
          options: Options(
            responseType: ResponseType.stream,
            headers: {
              'Accept': 'text/event-stream',
              if (token != null) 'Authorization': 'Bearer $token',
            },
          ),
          cancelToken: cancel,
        );
        var buffer = '';
        await for (final chunk in res.data!.stream.cast<List<int>>().transform(
          utf8.decoder,
        )) {
          buffer += chunk.replaceAll('\r\n', '\n');
          var idx = buffer.indexOf('\n\n');
          while (idx >= 0) {
            final e = parseFrame(buffer.substring(0, idx));
            buffer = buffer.substring(idx + 2);
            if (e != null) controller.add(e);
            idx = buffer.indexOf('\n\n');
          }
        }
        await controller.close();
      } catch (e, st) {
        if (!cancel.isCancelled) controller.addError(e, st);
        await controller.close();
      }
    },
    onCancel: () => cancel.cancel('closed'),
  );
  return controller.stream;
}
