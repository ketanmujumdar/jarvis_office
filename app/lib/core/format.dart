import 'package:intl/intl.dart';

/// Formatting helpers. Money is always integer cents of SGD.
abstract final class Fmt {
  static final NumberFormat _money = NumberFormat.currency(
    locale: 'en_SG',
    symbol: r'S$',
    decimalDigits: 2,
  );
  static final NumberFormat _compact = NumberFormat.compactCurrency(
    locale: 'en_SG',
    symbol: r'S$',
  );
  static final DateFormat _date = DateFormat('d MMM yyyy');
  static final DateFormat _dateTime = DateFormat('d MMM yyyy, HH:mm');
  static final DateFormat _month = DateFormat('MMM');

  /// 3550 -> "S$35.50".
  static String money(int cents) => _money.format(cents / 100);

  /// 350000 -> "S$3.5K" (chart axes, KPI tiles).
  static String moneyCompact(int cents) =>
      cents == 0 ? r'S$0' : _compact.format(cents / 100);

  static String date(DateTime d) => _date.format(d.toLocal());
  static String dateTime(DateTime d) => _dateTime.format(d.toLocal());

  /// "2026-10" -> "Oct".
  static String monthShort(String yyyyMm) {
    final parts = yyyyMm.split('-');
    if (parts.length != 2) return yyyyMm;
    final d = DateTime(int.parse(parts[0]), int.parse(parts[1]));
    return _month.format(d);
  }

  /// Relative time like "just now", "5 min ago", "3 h ago", else a date.
  static String relative(DateTime d, {DateTime? now}) {
    final diff = (now ?? DateTime.now()).difference(d);
    if (diff.inSeconds < 45) return 'just now';
    if (diff.inMinutes < 60) return '${diff.inMinutes} min ago';
    if (diff.inHours < 24) return '${diff.inHours} h ago';
    if (diff.inDays < 7) return '${diff.inDays} d ago';
    return date(d);
  }

  /// Percentage of [part] in [whole], clamped to 0..999.
  static String percent(int part, int whole) {
    if (whole <= 0) return '0%';
    final p = (part * 100 / whole).clamp(0, 999);
    return '${p.toStringAsFixed(p < 10 ? 1 : 0)}%';
  }
}
