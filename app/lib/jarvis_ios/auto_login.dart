import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api/api_client.dart';
import '../core/providers.dart';

/// The demo office manager this app signs in as.
const jarvisUserEmail = 'maya.tan@example.com';

/// Signs in with the same fake-login API the web app uses
/// (`POST /api/v1/auth/login`) and stores the session in [sessionProvider],
/// exactly like the login page. Invalidate to retry.
final autoLoginProvider = FutureProvider<void>((ref) async {
  if (ref.read(sessionProvider) != null) return;
  await ref.read(sessionProvider.notifier).login(jarvisUserEmail);
}, retry: (_, _) => null);

/// Human copy for a failed sign-in.
String describeLoginError(Object e) {
  if (e is ApiException) {
    if (e.statusCode == 0) return 'Can’t reach the Jarvis server.';
    if (e.isNotFound || e.isUnauthorized) {
      return 'No demo user $jarvisUserEmail on this server.';
    }
    return e.message;
  }
  return 'Could not sign in.';
}
