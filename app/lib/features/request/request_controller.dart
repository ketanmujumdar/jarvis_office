import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/models.dart';
import '../../core/providers.dart';
import 'request_logic.dart';
import 'url_opener.dart';

/// Opens external URLs (the Reap approval and card-entry pages).
/// Tests override this to record calls.
final urlOpenerProvider = Provider<UrlOpener>((ref) => openExternalUrl);

/// How often to re-fetch a request while the backend is working or a payment
/// is pending. `null` disables polling (tests); SSE still drives refreshes.
final requestPollIntervalProvider = Provider<Duration?>(
  (ref) => const Duration(seconds: 5),
);

/// Delay used to coalesce bursts of SSE events into a single re-fetch.
final requestRefreshDebounceProvider = Provider<Duration>(
  (ref) => const Duration(milliseconds: 250),
);

/// Saved Singapore delivery addresses (default first).
final deliveryAddressesProvider = FutureProvider.autoDispose<List<Address>>((
  ref,
) async {
  final list = await ref.watch(apiProvider).addresses();
  return [...list]..sort((a, b) {
    if (a.isDefault != b.isDefault) return a.isDefault ? -1 : 1;
    return a.label.compareTo(b.label);
  });
});

/// The current user's Reap card enrollment (null when none exists).
final currentEnrollmentProvider = FutureProvider.autoDispose<Enrollment?>(
  (ref) => ref.watch(apiProvider).currentEnrollment(),
);

/// Live activity feed for one request, newest first, built from SSE events.
class RequestActivity extends Notifier<List<ActivityEntry>> {
  RequestActivity(this.requestId);
  final String requestId;
  final _seen = <int>{};

  @override
  List<ActivityEntry> build() => const [];

  void add(SseEvent e) {
    if (e.id != 0 && !_seen.add(e.id)) return;
    final entry = activityFromEvent(e);
    if (entry == null) return;
    state = [entry, ...state].take(50).toList();
  }
}

final requestActivityProvider = NotifierProvider.autoDispose
    .family<RequestActivity, List<ActivityEntry>, String>(RequestActivity.new);

/// Loads one request and keeps it fresh from SSE events and light polling.
class RequestDetailController extends AsyncNotifier<RequestDetail> {
  RequestDetailController(this.requestId);
  final String requestId;

  Timer? _poll;
  Timer? _debounce;
  bool _fetching = false;

  @override
  Future<RequestDetail> build() async {
    ref.onDispose(() {
      _poll?.cancel();
      _debounce?.cancel();
    });
    // Keep the activity feed alive for as long as this request is watched.
    ref.listen(requestActivityProvider(requestId), (_, _) {});
    ref.listen<AsyncValue<SseEvent>>(eventStreamProvider(requestId), (_, next) {
      if (next is! AsyncData<SseEvent>) return;
      final e = next.value;
      if (e.requestId != null && e.requestId != requestId) return;
      ref.read(requestActivityProvider(requestId).notifier).add(e);
      if (eventChangesRequest(e)) _scheduleRefresh();
    });
    final d = await ref.read(apiProvider).getRequest(requestId);
    _syncPolling(d.request.status);
    return d;
  }

  void _scheduleRefresh() {
    _debounce?.cancel();
    _debounce = Timer(ref.read(requestRefreshDebounceProvider), refresh);
  }

  void _syncPolling(RequestStatus s) {
    final interval = ref.read(requestPollIntervalProvider);
    final active =
        interval != null && (s.isBusy || s == RequestStatus.awaitingPayment);
    if (!active) {
      _poll?.cancel();
      _poll = null;
      return;
    }
    _poll ??= Timer.periodic(interval, (_) => refresh());
  }

  void _set(RequestDetail d) {
    if (!ref.mounted) return;
    state = AsyncData(d);
    _syncPolling(d.request.status);
  }

  /// Re-fetches the request. Errors keep the last good data.
  Future<void> refresh() async {
    if (_fetching || !ref.mounted) return;
    _fetching = true;
    try {
      _set(await ref.read(apiProvider).getRequest(requestId));
    } catch (_) {
      // Transient: the next event or poll will retry.
    } finally {
      _fetching = false;
    }
  }

  /// Confirms the quoted request for delivery to [addressId].
  /// Throws [ApiException] (e.g. `no_active_enrollment`) for the UI to show.
  Future<void> confirm(String addressId) async {
    _set(
      await ref
          .read(apiProvider)
          .confirmRequest(requestId, addressId: addressId),
    );
  }

  Future<void> cancel() async {
    _set(await ref.read(apiProvider).cancelRequest(requestId));
  }

  /// Asks the backend to poll Reap now, then re-fetches.
  Future<void> refreshCheckout() async {
    await ref.read(apiProvider).refreshCheckout(requestId);
    await refresh();
  }
}

final requestDetailProvider = AsyncNotifierProvider.autoDispose
    .family<RequestDetailController, RequestDetail, String>(
      RequestDetailController.new,
    );
