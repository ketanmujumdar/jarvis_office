import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/models.dart';
import '../providers.dart';

/// A fake-auth session: the token is the user id (no real security).
class Session {
  const Session({required this.token, required this.user});
  final String token;
  final User user;
}

/// Holds the signed-in user. In-memory only (a page refresh signs out);
/// the auth feature may add persistence.
class SessionController extends Notifier<Session?> {
  @override
  Session? build() => null;

  Future<void> login(String email) async {
    final res = await ref.read(apiProvider).login(email);
    state = Session(token: res.token, user: res.user);
  }

  /// For tests and restoring a persisted session.
  void setSession(Session? s) => state = s;

  void logout() => state = null;
}

final sessionProvider = NotifierProvider<SessionController, Session?>(
  SessionController.new,
);

/// Convenience: the current user or null.
final currentUserProvider = Provider<User?>(
  (ref) => ref.watch(sessionProvider)?.user,
);
