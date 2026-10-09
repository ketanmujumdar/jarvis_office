import 'package:flutter/material.dart';

import '../../core/theme/tokens.dart';

/// Presentation for a policy reason code (`domain.ReasonCode`).
class ReasonCopy {
  const ReasonCopy(this.title, this.icon, this.tone);
  final String title;
  final IconData icon;
  final Tone tone;

  static ReasonCopy of(String code) => switch (code) {
    'OFF_LIST' => const ReasonCopy(
      'Not in catalog',
      Icons.playlist_add_rounded,
      Tone.info,
    ),
    'NOT_AUTO_APPROVE' => const ReasonCopy(
      'Needs sign-off',
      Icons.verified_user_outlined,
      Tone.warning,
    ),
    'OVER_UNIT_CEILING' => const ReasonCopy(
      'Above price ceiling',
      Icons.trending_up_rounded,
      Tone.warning,
    ),
    'OVER_ORDER_LIMIT' => const ReasonCopy(
      'Over order limit',
      Icons.receipt_long_outlined,
      Tone.warning,
    ),
    'OVER_MONTHLY_BUDGET' => const ReasonCopy(
      'Over monthly budget',
      Icons.account_balance_wallet_outlined,
      Tone.danger,
    ),
    'VENDOR_NOT_ALLOWED' => const ReasonCopy(
      'Vendor not allowed',
      Icons.block_rounded,
      Tone.danger,
    ),
    'NO_OFFER' => const ReasonCopy(
      'No offer found',
      Icons.search_off_rounded,
      Tone.danger,
    ),
    'PRICE_DRIFT' => const ReasonCopy(
      'Price changed',
      Icons.swap_vert_rounded,
      Tone.warning,
    ),
    'CURRENCY_MISMATCH' => const ReasonCopy(
      'Currency mismatch',
      Icons.currency_exchange_rounded,
      Tone.danger,
    ),
    _ => ReasonCopy(
      code.isEmpty ? 'Review' : _humanize(code),
      Icons.info_outline_rounded,
      Tone.neutral,
    ),
  };

  static String _humanize(String code) {
    final s = code.toLowerCase().replaceAll('_', ' ');
    return s[0].toUpperCase() + s.substring(1);
  }
}
