import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'features/request/request_controller.dart';
import 'features/voice/realtime/realtime_transport.dart';
import 'jarvis_ios/ios_realtime_transport.dart';
import 'jarvis_ios/ios_url_opener.dart';
import 'jarvis_ios/jarvis_screen.dart';

/// Standalone iPhone "Jarvis" app for office manager Maya Tan.
///
/// flutter run -t lib/main_jarvis.dart --dart-define=API_BASE_URL=https://...
void main() {
  WidgetsFlutterBinding.ensureInitialized();
  SystemChrome.setPreferredOrientations([DeviceOrientation.portraitUp]);
  SystemChrome.setSystemUIOverlayStyle(SystemUiOverlayStyle.light);
  runApp(
    ProviderScope(
      overrides: [
        realtimeTransportFactoryProvider.overrideWithValue(
          IosRealtimeTransport.new,
        ),
        urlOpenerProvider.overrideWithValue(openInSafari),
      ],
      child: const JarvisIosApp(),
    ),
  );
}

class JarvisIosApp extends StatelessWidget {
  const JarvisIosApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Jarvis',
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        brightness: Brightness.dark,
        useMaterial3: true,
        scaffoldBackgroundColor: const Color(0xFF02040B),
        colorScheme: ColorScheme.fromSeed(
          seedColor: const Color(0xFF3EE7FF),
          brightness: Brightness.dark,
        ),
      ),
      home: const AnnotatedRegion<SystemUiOverlayStyle>(
        value: SystemUiOverlayStyle.light,
        child: JarvisRoot(),
      ),
    );
  }
}
