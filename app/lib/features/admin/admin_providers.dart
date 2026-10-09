import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/api/models.dart';
import '../../core/providers.dart';
import 'url_opener.dart';

/// No automatic retries: admin screens show an explicit "Try again" instead.
Duration? _noRetry(int _, Object _) => null;

final adminCatalogProvider = FutureProvider<List<CatalogItem>>(
  (ref) => ref.watch(apiProvider).adminCatalog(),
  retry: _noRetry,
);

final adminVendorsProvider = FutureProvider<List<Vendor>>(
  (ref) => ref.watch(apiProvider).adminVendors(),
  retry: _noRetry,
);

final adminPolicyProvider = FutureProvider<PolicyConfig>(
  (ref) => ref.watch(apiProvider).getPolicy(),
  retry: _noRetry,
);

final adminAddressesProvider = FutureProvider<List<Address>>((ref) async {
  final list = await ref.watch(apiProvider).adminAddresses();
  // Default first, then alphabetical by label.
  return [...list]..sort((a, b) {
    if (a.isDefault != b.isDefault) return a.isDefault ? -1 : 1;
    return a.label.toLowerCase().compareTo(b.label.toLowerCase());
  });
}, retry: _noRetry);

final systemPromptProvider = FutureProvider<SystemPrompt>(
  (ref) => ref.watch(apiProvider).getSystemPrompt(),
  retry: _noRetry,
);

/// Latest card enrollment, or null when no card has been set up.
final currentEnrollmentProvider = FutureProvider<Enrollment?>(
  (ref) => ref.watch(apiProvider).currentEnrollment(),
  retry: _noRetry,
);

/// Opens external URLs (the Reap-hosted card page). Override in tests.
final urlOpenerProvider = Provider<UrlOpener>((_) => const UrlOpener());
