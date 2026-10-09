import 'package:flutter/material.dart';

import '../format.dart';

/// Formats integer SGD cents with tabular figures, e.g. 3550 -> S$35.50.
class MoneyText extends StatelessWidget {
  const MoneyText(
    this.cents, {
    super.key,
    this.style,
    this.strikethrough = false,
  });

  final int cents;
  final TextStyle? style;
  final bool strikethrough;

  @override
  Widget build(BuildContext context) {
    final base = style ?? DefaultTextStyle.of(context).style;
    return Text(
      Fmt.money(cents),
      style: base.copyWith(
        fontFeatures: const [FontFeature.tabularFigures()],
        decoration: strikethrough ? TextDecoration.lineThrough : null,
      ),
    );
  }
}
